package prompts

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nolint: funlen
func TestFewShotPrompt_Format(t *testing.T) {
	examplePrompt := NewPromptTemplate("{{.question}}: {{.answer}}", []string{"question", "answer"})
	t.Parallel()
	testCases := []struct {
		name             string
		examplePrompt    PromptTemplate
		examples         []map[string]string
		prefix           string
		suffix           string
		input            map[string]any
		partialInput     map[string]any
		exampleSeparator string
		templateFormat   TemplateFormat
		validateTemplate bool
		wantErr          bool
		expected         string
	}{
		{
			"prefix only", examplePrompt,
			[]map[string]string{},
			"This is a {{.foo}} test.", "",
			map[string]any{"foo": "bar"},
			nil,
			"",
			TemplateFormatGoTemplate,
			true,
			false,
			"This is a bar test.",
		},
		{
			"suffix only", examplePrompt,
			[]map[string]string{},
			"", "This is a {{.foo}} test.",
			map[string]any{"foo": "bar"},
			nil,
			"",
			TemplateFormatGoTemplate,
			true,
			false,
			"This is a bar test.",
		},
		{
			"insufficient InputVariables w err",
			examplePrompt,
			[]map[string]string{},
			"",
			"This is a {{.foo}} test.",
			map[string]any{"bar": "bar"},
			nil,
			"",
			TemplateFormatGoTemplate,
			true,
			true,
			`template: template:1:12: executing "template" at <.foo>: map has no entry for key "foo"`,
		},
		{
			"inputVariables neither Examples nor ExampleSelector w err",
			examplePrompt,
			nil,
			"",
			"",
			map[string]any{"bar": "bar"},
			nil,
			"",
			TemplateFormatGoTemplate,
			true,
			true,
			ErrNoExample.Error(),
		},
		{
			"functionality test",
			examplePrompt,
			[]map[string]string{{"question": "foo", "answer": "bar"}, {"question": "baz", "answer": "foo"}},
			"This is a test about {{.content}}.",
			"Now you try to talk about {{.new_content}}.",
			map[string]any{"content": "animals", "new_content": "party"},
			nil,
			"\n",
			TemplateFormatGoTemplate,
			true,
			false,
			"This is a test about animals.\nfoo: bar\nbaz: foo\nNow you try to talk about party.",
		},
		{
			"functionality test with partial input",
			examplePrompt,
			[]map[string]string{{"question": "foo", "answer": "bar"}, {"question": "baz", "answer": "foo"}},
			"This is a test about {{.content}}.",
			"Now you try to talk about {{.new_content}}.",
			map[string]any{"content": "animals"},
			map[string]any{"new_content": func() string { return "party" }},
			"\n",
			TemplateFormatGoTemplate,
			true,
			false,
			"This is a test about animals.\nfoo: bar\nbaz: foo\nNow you try to talk about party.",
		},
		{
			"invalid template w err",
			examplePrompt,
			[]map[string]string{{"question": "foo", "answer": "bar"}, {"question": "baz", "answer": "foo"}},
			"This is a test about {{.wrong_content}}.",
			"Now you try to talk about {{.new_content}}.",
			map[string]any{"content": "animals"},
			map[string]any{"new_content": func() string { return "party" }},
			"\n",
			TemplateFormatGoTemplate,
			true,
			true,
			"template: template:1:23: executing \"template\" at <.wrong_content>: map has no entry for key " +
				"\"wrong_content\"",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			t.Helper()
			p, err := NewFewShotPrompt(tc.examplePrompt, tc.examples, nil, tc.prefix, tc.suffix,
				getMapKeys(tc.input), tc.partialInput, tc.exampleSeparator, tc.templateFormat, tc.validateTemplate)
			if tc.wantErr {
				checkError(t, err, tc.expected)
				return
			}
			got, err := p.Format(tc.input)
			if checkError(t, err, "") {
				return
			}
			if diff := cmp.Diff(tc.expected, got); diff != "" {
				t.Errorf("unexpected prompt output (-want +got):\n%s", diff)
			}
		})
	}
}

func checkError(t *testing.T, err error, expected string) bool {
	t.Helper()
	if err != nil {
		if expected != "" && err.Error() != expected {
			t.Errorf("unexpected error: got %q, want %q", err.Error(), expected)
		}
		return true
	}
	return false
}

const (
	fewShotSuffix  = "Q: {{.q}}"
	fewShotExample = "{{.question}} -> {{.answer}}"
)

// fakeExampleSelector returns a fixed example list and records the input it
// was asked to select for.
type fakeExampleSelector struct {
	examples []map[string]string
	gotInput map[string]string
}

func (s *fakeExampleSelector) AddExample(example map[string]string) string {
	s.examples = append(s.examples, example)
	return ""
}

func (s *fakeExampleSelector) SelectExamples(input map[string]string) []map[string]string {
	s.gotInput = input
	return s.examples
}

func TestNewFewShotPromptValidation(t *testing.T) {
	t.Parallel()

	examplePrompt := NewPromptTemplate(fewShotExample, nil)
	selector := &fakeExampleSelector{}

	_, err := NewFewShotPrompt(examplePrompt, []map[string]string{}, selector, "", "", nil, nil, "",
		TemplateFormatGoTemplate, false)
	require.ErrorIs(t, err, ErrExamplesAndExampleSelectorProvided)

	_, err = NewFewShotPrompt(examplePrompt, nil, nil, "", "", nil, nil, "", TemplateFormatGoTemplate, false)
	require.ErrorIs(t, err, ErrNoExample)
}

func TestFewShotPromptWithSelector(t *testing.T) {
	t.Parallel()

	selector := &fakeExampleSelector{}
	selector.AddExample(map[string]string{
		"question": "1+1",
		"answer":   "2",
	})
	p, err := NewFewShotPrompt(NewPromptTemplate(fewShotExample, nil), nil, selector, "", fewShotSuffix,
		[]string{"q"}, nil, "", TemplateFormatGoTemplate, true)
	require.NoError(t, err)
	assert.Equal(t, []string{"q"}, p.GetInputVariables())

	pv, err := p.FormatPrompt(map[string]any{
		"q": StringPromptValue("2+2"),
	})
	require.NoError(t, err)
	assert.Equal(t, StringPromptValue("1+1 -> 2\n\nQ: 2+2"), pv)
	assert.Equal(t, map[string]string{
		"q": "2+2",
	}, selector.gotInput)
}

func TestFewShotPromptFormatErrors(t *testing.T) {
	t.Parallel()

	examples := []map[string]string{
		{
			"question": "1+1",
		},
	}
	tests := []struct {
		name    string
		prompt  *FewShotPrompt
		values  map[string]any
		wantIs  error
		wantErr string
	}{
		{
			name: "invalid partial variable",
			prompt: &FewShotPrompt{
				Examples: examples,
				PartialVariables: map[string]any{
					"bad": 1,
				},
				TemplateFormat: TemplateFormatGoTemplate,
			},
			wantIs:  ErrInvalidPartialVariableType,
			wantErr: "unexpected type for bad: invalid partial variable type",
		},
		{
			name: "non-string value",
			prompt: &FewShotPrompt{
				Examples:       examples,
				TemplateFormat: TemplateFormatGoTemplate,
			},
			values: map[string]any{
				"q": 1,
			},
			wantIs:  ErrInvalidPartialVariableType,
			wantErr: "not expected type: int: invalid partial variable type",
		},
		{
			name: "no examples",
			prompt: &FewShotPrompt{
				TemplateFormat: TemplateFormatGoTemplate,
			},
			wantIs:  ErrNoExample,
			wantErr: ErrNoExample.Error(),
		},
		{
			name: "example prompt fails",
			prompt: &FewShotPrompt{
				Examples:       examples,
				ExamplePrompt:  NewPromptTemplate(fewShotExample, nil),
				TemplateFormat: TemplateFormatGoTemplate,
			},
			wantErr: `template: template:1:19: executing "template" at <.answer>: map has no entry for key "answer"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := tc.prompt.Format(tc.values)
			require.EqualError(t, err, tc.wantErr)
			if tc.wantIs != nil {
				require.ErrorIs(t, err, tc.wantIs)
			}

			pv, err := tc.prompt.FormatPrompt(tc.values)
			require.EqualError(t, err, tc.wantErr)
			assert.Nil(t, pv)
		})
	}
}

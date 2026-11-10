package prompts

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptTemplateFormatPrompt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		template    string
		inputVars   []string
		partialVars map[string]any
		vars        map[string]any
		expected    string
		wantErr     bool
	}{
		{"empty", "", nil, nil, nil, "", false},
		{"ok-input-var", "", []string{"foobar"}, nil, nil, "", false},
		{"missing-input-var", "{{.name}}", []string{"job"}, nil, nil, "", true}, // expect a error.
		{"hello world", "hello world", nil, nil, nil, "hello world", false},
		{"basic", "hello {{.name}}", nil, nil, map[string]any{
			"name": "richard",
		}, "hello richard", false},
		{"partials", "hello {{.name}}", nil, map[string]any{
			"name": "richard",
		}, nil, "hello richard", false},
		{
			"partials w func", "{{.greeting}} {{.name}}", nil,
			map[string]any{
				"name": func() string { return "richard" },
			},
			map[string]any{
				"greeting": "hello",
			},
			"hello richard", false,
		},
		{
			"partials w err", "{{.greeting}} {{.name}} {{.message}}", nil,
			map[string]any{
				"name": func() string { return "richard" },
			},
			map[string]any{
				"greeting": "hello",
			},
			"", true, // expect an error
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := PromptTemplate{
				Template:         tc.template,
				TemplateFormat:   TemplateFormatGoTemplate,
				InputVariables:   tc.inputVars,
				PartialVariables: tc.partialVars,
			}
			fp, err := p.FormatPrompt(tc.vars)
			if (err != nil) != tc.wantErr {
				t.Errorf("PromptTemplate.FormatPrompt() error = %v, wantErr %v", err, tc.wantErr)
				return
			}
			if tc.wantErr {
				return
			}
			got := fmt.Sprint(fp)
			if cmp.Diff(tc.expected, got) != "" {
				t.Errorf("unexpected prompt output (-want +got):\n%s", cmp.Diff(tc.expected, got))
			}
		})
	}
}

func TestPromptTemplateInvalidPartial(t *testing.T) {
	t.Parallel()

	p := PromptTemplate{
		Template:       "hello",
		TemplateFormat: TemplateFormatGoTemplate,
		InputVariables: []string{"a", "b"},
		PartialVariables: map[string]any{
			"bad": 1,
		},
	}
	assert.Equal(t, []string{"a", "b"}, p.GetInputVariables())

	_, err := p.Format(nil)
	require.ErrorIs(t, err, ErrInvalidPartialVariableType)
	require.EqualError(t, err, "unexpected type for bad: invalid partial variable type")

	pv, err := p.FormatPrompt(nil)
	require.ErrorIs(t, err, ErrInvalidPartialVariableType)
	assert.Nil(t, pv)
}

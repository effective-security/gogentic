package prompts

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInterpolateGoTemplate(t *testing.T) {
	t.Parallel()

	type tests struct {
		name           string
		template       string
		templateValues map[string]any
		expected       string
		errValue       string
	}

	testCases := []tests{
		{
			name:     "Single",
			template: "Hello {{ .key }}",
			templateValues: map[string]any{
				"key": "world",
			},
			expected: "Hello world",
		},
		{
			name:     "Multiple",
			template: "Hello {{ .key1 }} and {{ .key2 }}",
			templateValues: map[string]any{
				"key1": "world",
				"key2": "universe",
			},
			expected: "Hello world and universe",
		},
		{
			name:     "Nested",
			template: "Hello {{ .key1.key2 }}",
			templateValues: map[string]any{
				"key1": map[string]any{
					"key2": "world",
				},
			},
			expected: "Hello world",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("go/template", func(t *testing.T) {
				t.Parallel()

				actual, err := interpolateGoTemplate(tc.template, tc.templateValues)
				require.NoError(t, err)
				assert.Equal(t, tc.expected, actual)
			})
			t.Run("jinja2", func(t *testing.T) {
				t.Parallel()

				actual, err := interpolateJinja2(strings.ReplaceAll(tc.template, "{{ .", "{{ "), tc.templateValues)
				require.NoError(t, err)
				assert.Equal(t, tc.expected, actual)
			})
		})
	}

	errTestCases := []tests{
		{
			name:     "ParseErrored",
			template: "Hello {{{ .key1 }}",
			expected: "",
			errValue: "template: template:1: unexpected \"{\" in command",
		},
		{
			name:     "ExecuteErrored",
			template: "Hello {{ .key1 .key2 }}",
			expected: "",
			errValue: "template: template:1:9: executing \"template\" at <.key1>: key1 is not a method but has arguments",
		},
	}

	for _, tc := range errTestCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := interpolateGoTemplate(tc.template, map[string]any{})
			require.Error(t, err)
			require.EqualError(t, err, tc.errValue)
		})
	}
}

func TestCheckValidTemplate(t *testing.T) {
	t.Parallel()

	t.Run("NoTemplateAvailable", func(t *testing.T) {
		t.Parallel()

		err := CheckValidTemplate("Hello, {test}", "unknown", []string{"test"})
		require.Error(t, err)
		require.ErrorIs(t, err, ErrInvalidTemplateFormat)
		assert.Equal(t, "got: unknown, should be one of [f-string go-template jinja2]: invalid template format", err.Error())
	})

	t.Run("TemplateErrored", func(t *testing.T) {
		t.Parallel()

		err := CheckValidTemplate("Hello, {{{ test }}", TemplateFormatGoTemplate, []string{"test"})
		require.Error(t, err)
		require.EqualError(t, err, "template: template:1: unexpected \"{\" in command")
	})

	t.Run("TemplateValid", func(t *testing.T) {
		t.Parallel()

		err := CheckValidTemplate("Hello, {{ .test }}", TemplateFormatGoTemplate, []string{"test"})
		require.NoError(t, err)
	})
}

func TestRenderTemplate(t *testing.T) {
	t.Parallel()

	t.Run("TemplateAvailable", func(t *testing.T) {
		t.Parallel()

		actual, err := RenderTemplate(
			"Hello {{ .key }}",
			TemplateFormatGoTemplate,
			map[string]any{
				"key": "world",
			},
		)
		require.NoError(t, err)
		assert.Equal(t, "Hello world", actual)
	})

	t.Run("TemplateNotAvailable", func(t *testing.T) {
		t.Parallel()

		_, err := RenderTemplate(
			"Hello {key}",
			"unknown",
			map[string]any{
				"key": "world",
			},
		)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrInvalidTemplateFormat)
	})
}

// renderTimeout bounds a jinja2 render in tests, so a lexer that loops
// forever fails the test instead of hanging it (F-010).
const renderTimeout = 5 * time.Second

func TestInterpolateJinja2Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template string
		wantErr  string
	}{
		{
			name:     "ParseErrored",
			template: "{% if %}",
			wantErr:  `failed to parse jinja2 template: failed to parse template '{% if %}': Unable to parse controlStructure "if": expected either a number, string, keyword or identifier.`,
		},
		{
			name:     "ExecuteErrored",
			template: "{{ x() }}",
			wantErr:  "failed to render jinja2 template: unable to execute template: Unable to render expression at line 1: call([], map[]): x is not callable",
		},
		{
			name:     "UnterminatedVariable",
			template: "{{ x ",
			wantErr:  `failed to parse jinja2 template: failed to parse template '{{ x ': '}}' expected here (Line: 1 Col: 6, near "")`,
		},
		{
			name:     "UnterminatedBlock",
			template: "{% if x ",
			wantErr:  `failed to parse jinja2 template: failed to parse template '{% if x ': Expected end of block "%}" (Line: 1 Col: 9, near "")`,
		},
		{
			name:     "OpenVariableOnly",
			template: "{{",
			wantErr:  `failed to parse jinja2 template: failed to parse template '{{': expected either a number, string, keyword or identifier. (Line: 1 Col: 3, near "")`,
		},
		{
			name:     "OpenBlockOnly",
			template: "{%",
			wantErr:  `failed to parse jinja2 template: failed to parse template '{%': ControlStructure '' not found (or beginning not provided) (Line: 1 Col: 3, near "")`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			type result struct {
				out string
				err error
			}
			done := make(chan result, 1)
			go func() {
				out, err := RenderTemplate(tc.template, TemplateFormatJinja2, map[string]any{
					"x": 1,
				})
				done <- result{
					out: out,
					err: err,
				}
			}()
			select {
			case res := <-done:
				require.EqualError(t, res.err, tc.wantErr)
				assert.Empty(t, res.out)
			case <-time.After(renderTimeout):
				require.FailNow(t, "jinja2 render did not return", "template %q", tc.template)
			}
		})
	}
}

func TestInterpolateJinja2Raw(t *testing.T) {
	t.Parallel()

	out, err := RenderTemplate("{% raw %}{{ {% endraw %}{{ x }}", TemplateFormatJinja2, map[string]any{
		"x": 1,
	})
	require.NoError(t, err)
	assert.Equal(t, "{{ 1", out)
}

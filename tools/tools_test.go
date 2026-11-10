package tools_test

import (
	"context"
	"testing"

	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/tools"
	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
)

// fakeTool is a minimal tools.ITool for rendering descriptions.
type fakeTool struct {
	name        string
	description string
}

func (f *fakeTool) Name() string                                 { return f.name }
func (f *fakeTool) Description() string                          { return f.description }
func (f *fakeTool) Parameters() *jsonschema.Schema               { return nil }
func (f *fakeTool) Call(context.Context, string) (string, error) { return "", nil }

func newDescriptions() tools.Descriptions {
	return tools.GetDescriptions(
		&fakeTool{
			name:        "search",
			description: "Searches the web\nfor answers",
		},
		&fakeTool{
			name:        "calc",
			description: "Evaluates math",
		},
	)
}

func TestGetDescriptions(t *testing.T) {
	t.Parallel()

	// Descriptions are collapsed to a single line.
	assert.Equal(t, tools.Descriptions{
		{
			Name:        "search",
			Description: "Searches the web for answers.",
		},
		{
			Name:        "calc",
			Description: "Evaluates math.",
		},
	}, newDescriptions())

	assert.Nil(t, tools.GetDescriptions())
}

func TestDescriptions_Render(t *testing.T) {
	t.Parallel()

	d := newDescriptions()

	expMarkdown := `- Name: search
  Description: Searches the web for answers.
- Name: calc
  Description: Evaluates math.
`
	assert.Equal(t, expMarkdown, d.ToMarkdown())
	assert.Equal(t, expMarkdown, d.Render(llmutils.RenderFormatMarkdown))

	assert.Equal(t,
		`[{"Name":"search","Description":"Searches the web for answers."},{"Name":"calc","Description":"Evaluates math."}]`,
		d.Render(llmutils.RenderFormatJSON))

	expYAML := `- Name: search
  Description: Searches the web for answers.
- Name: calc
  Description: Evaluates math.
`
	assert.Equal(t, expYAML, d.Render(llmutils.RenderFormatYAML))

	assert.Empty(t, tools.Descriptions{}.ToMarkdown())
}

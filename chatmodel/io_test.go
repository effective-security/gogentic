package chatmodel

import (
	"testing"

	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPInputRequest_ParseInput(t *testing.T) {
	t.Parallel()
	m := &MCPInputRequest{}
	raw := `{"chatID":"abc","input":"msg"}`
	err := m.ParseInput(raw)
	require.NoError(t, err)
	assert.Equal(t, "abc", m.ChatID)
	assert.Equal(t, "msg", m.Input)

	// Bad input
	err = m.ParseInput("{invalid json}")
	require.Error(t, err)
}

func TestMCPInputRequest_JSONSchemaExtend(t *testing.T) {
	t.Parallel()
	m := MCPInputRequest{}
	schema := &jsonschema.Schema{}
	m.JSONSchemaExtend(schema)
	assert.Equal(t, "MCP Input Request", schema.Title)
}

func TestInputRequest(t *testing.T) {
	t.Parallel()
	r := &InputRequest{}
	raw := `{"input":"hello"}`
	err := r.ParseInput(raw)
	require.NoError(t, err)
	assert.Equal(t, "hello", r.Input)

	// GetContent returns input
	assert.Equal(t, "hello", r.GetContent())

	// Bad input
	err = r.ParseInput("{broken}")
	require.Error(t, err)

	// NewInputRequest
	ri := NewInputRequest("bar")
	assert.Equal(t, "bar", ri.Input)
}

func TestInputRequest_JSONSchemaExtend(t *testing.T) {
	t.Parallel()
	r := InputRequest{}
	schema := &jsonschema.Schema{}
	r.JSONSchemaExtend(schema)
	assert.Equal(t, "Input Request", schema.Title)
}

func TestOutputResult(t *testing.T) {
	t.Parallel()
	r := OutputResult{Content: "foo"}
	assert.Equal(t, "foo", r.GetContent())

	nr := NewOutputResult("baz")
	assert.Equal(t, "baz", nr.Content)
}

func TestBaseClarificationResultSetters(t *testing.T) {
	t.Parallel()
	var res BaseClarificationResult
	res.SetConfidence("Medium")
	assert.Equal(t, "Medium", res.Confidence)
	res.SetClarification("Need more info")
	assert.Equal(t, "Need more info", res.Clarification)
	res.SetReasoning("Logic chain")
	assert.Equal(t, "Logic chain", res.Reasoning)
}

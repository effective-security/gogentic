package anthropic

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToAnthropicOutputConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		rf     *schema.ResponseFormat
		nonNil bool
	}{
		{
			name:   "nil response format",
			rf:     nil,
			nonNil: false,
		},
		{
			name: "wrong type",
			rf: &schema.ResponseFormat{
				Type: "text",
			},
			nonNil: false,
		},
		{
			name: "nil json schema",
			rf: &schema.ResponseFormat{
				Type:       "json_schema",
				JSONSchema: nil,
			},
			nonNil: false,
		},
		{
			name: "nil schema property",
			rf: &schema.ResponseFormat{
				Type: "json_schema",
				JSONSchema: &schema.ResponseFormatJSONSchema{
					Schema: nil,
				},
			},
			nonNil: false,
		},
		{
			name: "valid simple object schema",
			rf: &schema.ResponseFormat{
				Type: "json_schema",
				JSONSchema: &schema.ResponseFormatJSONSchema{
					Schema: &schema.ResponseFormatJSONSchemaProperty{
						Type: "object",
						Properties: map[string]*schema.ResponseFormatJSONSchemaProperty{
							"name": {Type: "string"},
							"age":  {Type: "integer"},
						},
						Required: []string{"name"},
					},
				},
			},
			nonNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := toAnthropicOutputConfig(tt.rf)
			if tt.nonNil {
				require.NotNil(t, got)
				assert.NotEmpty(t, got.Format.Schema)
				assert.Equal(t, "object", got.Format.Schema["type"])
			} else {
				assert.Nil(t, got)
			}
		})
	}
}

func TestConvertToAnthropicSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		prop     *schema.ResponseFormatJSONSchemaProperty
		wantType string
		wantKeys []string
	}{
		{
			name:     "nil",
			prop:     nil,
			wantType: "",
			wantKeys: nil,
		},
		{
			name: "object with properties",
			prop: &schema.ResponseFormatJSONSchemaProperty{
				Type: "object",
				Properties: map[string]*schema.ResponseFormatJSONSchemaProperty{
					"final_answer": {Type: "string"},
				},
				Required: []string{"final_answer"},
			},
			wantType: "object",
			wantKeys: []string{"type", "properties", "required"},
		},
		{
			name: "array with items",
			prop: &schema.ResponseFormatJSONSchemaProperty{
				Type: "array",
				Items: &schema.ResponseFormatJSONSchemaProperty{
					Type: "string",
				},
			},
			wantType: "array",
			wantKeys: []string{"type", "items"},
		},
		{
			name: "object with additionalProperties false",
			prop: &schema.ResponseFormatJSONSchemaProperty{
				Type:                 "object",
				AdditionalProperties: func() *bool { b := false; return &b }(),
			},
			wantType: "object",
			wantKeys: []string{"type", "additionalProperties"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := convertToAnthropicSchema(tt.prop)
			if tt.prop == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			if tt.wantType != "" {
				assert.Equal(t, tt.wantType, got["type"])
			}
			for _, k := range tt.wantKeys {
				assert.Contains(t, got, k)
			}
		})
	}
}

func TestStructuredOutputObjectSchema(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" || apiKey == "fakekey" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	type Input struct {
		FinalAnswer string `json:"final_answer" description:"The final answer to the question"`
	}
	responseFormat, err := schema.NewResponseFormat(reflect.TypeOf(Input{}), true)
	require.NoError(t, err)

	llm, err := New(
		WithToken(apiKey),
		WithModel("claude-sonnet-4-5"),
	)
	require.NoError(t, err)

	content := []llms.Message{
		{
			Role:  llms.RoleSystem,
			Parts: []llms.ContentPart{llms.TextContent{Text: "You are a student taking a math exam."}},
		},
		{
			Role:  llms.RoleHuman,
			Parts: []llms.ContentPart{llms.TextContent{Text: "Solve 2 + 2"}},
		},
	}

	rsp, err := llm.GenerateContent(context.Background(), content,
		llms.WithResponseFormat(responseFormat),
		llms.WithMaxTokens(256),
	)
	require.NoError(t, err)

	require.NotEmpty(t, rsp.Choices)
	c1 := rsp.Choices[0]
	assert.Regexp(t, `"final_answer"`, strings.ToLower(c1.Content))
	// Response should be valid JSON deserializable into Input
	var parsed Input
	err = json.Unmarshal([]byte(c1.Content), &parsed)
	require.NoError(t, err)
	assert.NotEmpty(t, parsed.FinalAnswer)
}

func TestStructuredOutputObjectAndArraySchema(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" || apiKey == "fakekey" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	type Input struct {
		Steps       []string `json:"steps" description:"The steps to solve the problem"`
		FinalAnswer string   `json:"final_answer" description:"The final answer to the question"`
	}
	responseFormat, err := schema.NewResponseFormat(reflect.TypeOf(Input{}), true)
	require.NoError(t, err)

	llm, err := New(
		WithToken(apiKey),
		WithModel("claude-sonnet-4-5"),
	)
	require.NoError(t, err)

	content := []llms.Message{
		{
			Role:  llms.RoleSystem,
			Parts: []llms.ContentPart{llms.TextContent{Text: "You are a student taking a math exam."}},
		},
		{
			Role:  llms.RoleHuman,
			Parts: []llms.ContentPart{llms.TextContent{Text: "Solve 2 + 2"}},
		},
	}

	rsp, err := llm.GenerateContent(context.Background(), content,
		llms.WithResponseFormat(responseFormat),
		llms.WithMaxTokens(512),
	)
	require.NoError(t, err)

	require.NotEmpty(t, rsp.Choices)
	c1 := rsp.Choices[0]
	assert.Regexp(t, `"steps"`, strings.ToLower(c1.Content))
	// Response should be valid JSON deserializable into Input
	var parsed Input
	err = json.Unmarshal([]byte(c1.Content), &parsed)
	require.NoError(t, err)
	assert.NotEmpty(t, parsed.Steps)
	assert.NotEmpty(t, parsed.FinalAnswer)
}

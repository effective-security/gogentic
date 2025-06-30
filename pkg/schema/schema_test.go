package schema_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type SearchType string

const (
	Web   SearchType = "web"
	Image SearchType = "image"
	Video SearchType = "video"
)

// Search represents a search request with various parameters.
type Search struct {
	Topic string     `json:"topic,omitempty" jsonschema:"title=Topic,description=Topic of the search\\, with coma.,example=golang"`
	Query string     `json:"query" jsonschema:"title=Query,description=Query to search for relevant content,example=what is golang"`
	Type  SearchType `json:"type"  jsonschema:"title=Type,description=Type of search,default=web,enum=web,enum=image,enum=video"`
	Args  []*KVPair  `json:"args,omitempty" jsonschema:"title=Args,description=Arguments for the search"`
	Prov  *KVPair    `json:"prov,omitempty" jsonschema:"title=Prov,description=Provider for the search"`
}

// KVPair represents a key-value pair.
type KVPair struct {
	Key   string `json:"key" jsonschema:"title=Key,description=Key of the pair"`
	Value string `json:"value" jsonschema:"title=Value,description=Value of the pair"`
}

func TestSchema(t *testing.T) {
	t.Parallel()

	t.Run("Input", func(t *testing.T) {
		t.Parallel()
		si, err := schema.New(reflect.TypeOf(chatmodel.InputRequest{}))
		require.NoError(t, err)
		exp := `{
	"properties": {
		"input": {
			"type": "string",
			"title": "Input",
			"description": "The message sent by the user to the assistant."
		}
	},
	"type": "object",
	"required": [
		"input"
	]
}`
		assert.Equal(t, exp, si.String())
		assert.Equal(t, exp, llmutils.ToJSONIndent(si.Parameters))
	})

	t.Run("Output", func(t *testing.T) {
		t.Parallel()
		so, err := schema.New(reflect.TypeOf(chatmodel.OutputResult{}))
		require.NoError(t, err)

		exp := `{
	"properties": {
		"content": {
			"type": "string",
			"title": "Response Content",
			"description": "The content returned by agent or tool."
		}
	},
	"type": "object",
	"required": [
		"content"
	]
}`
		assert.Equal(t, exp, so.String())
		assert.Equal(t, exp, llmutils.ToJSONIndent(so.Parameters))

	})

	t.Run("Search", func(t *testing.T) {
		t.Parallel()
		s, err := schema.New(reflect.TypeOf(Search{}))
		require.NoError(t, err)

		exp := `{
	"properties": {
		"topic": {
			"type": "string",
			"title": "Topic",
			"description": "Topic of the search, with coma.",
			"examples": [
				"golang"
			]
		},
		"query": {
			"type": "string",
			"title": "Query",
			"description": "Query to search for relevant content",
			"examples": [
				"what is golang"
			]
		},
		"type": {
			"type": "string",
			"enum": [
				"web",
				"image",
				"video"
			],
			"title": "Type",
			"description": "Type of search",
			"default": "web"
		},
		"args": {
			"items": {
				"properties": {
					"key": {
						"type": "string",
						"title": "Key",
						"description": "Key of the pair"
					},
					"value": {
						"type": "string",
						"title": "Value",
						"description": "Value of the pair"
					}
				},
				"type": "object",
				"required": [
					"key",
					"value"
				]
			},
			"type": "array",
			"title": "Args",
			"description": "Arguments for the search"
		},
		"prov": {
			"properties": {
				"key": {
					"type": "string",
					"title": "Key",
					"description": "Key of the pair"
				},
				"value": {
					"type": "string",
					"title": "Value",
					"description": "Value of the pair"
				}
			},
			"type": "object",
			"required": [
				"key",
				"value"
			],
			"title": "Prov",
			"description": "Provider for the search"
		}
	},
	"type": "object",
	"required": [
		"query",
		"type"
	]
}`
		assert.Equal(t, exp, s.String())
		assert.Equal(t, exp, llmutils.ToJSONIndent(s.Parameters))
	})

	t.Run("Weather", func(t *testing.T) {
		t.Parallel()

		type weatherRequest struct {
			Location string `json:"location" jsonschema:"description=City name"`
			Unit     string `json:"unit" jsonschema:"description=Unit of measurement,enum=celsius,enum=fahrenheit"`
		}

		s, err := schema.New(reflect.TypeOf(weatherRequest{}))
		require.NoError(t, err)
		exp := `{
	"properties": {
		"location": {
			"type": "string",
			"description": "City name"
		},
		"unit": {
			"type": "string",
			"enum": [
				"celsius",
				"fahrenheit"
			],
			"description": "Unit of measurement"
		}
	},
	"type": "object",
	"required": [
		"location",
		"unit"
	]
}`
		assert.Equal(t, exp, s.String())

		// unmarshal
		var sc jsonschema.Schema
		err = json.Unmarshal([]byte(exp), &sc)
		require.NoError(t, err)
		assert.Equal(t, 2, sc.Properties.Len())
	})
}

func TestSchemaFromAny(t *testing.T) {
	t.Parallel()

	sc, err := schema.FromAny(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type": "string",
			},
		},
		"required": []string{"query"},
	})
	require.NoError(t, err)

	exp := `{
	"properties": {
		"query": {
			"type": "string"
		}
	},
	"type": "object",
	"required": [
		"query"
	]
}`
	assert.Equal(t, exp, llmutils.ToJSONIndent(sc))
}

func TestSchemaNewResponseFormat(t *testing.T) {
	t.Parallel()

	t.Run("Search", func(t *testing.T) {
		t.Parallel()
		rf, err := schema.NewResponseFormat(reflect.TypeOf(Search{}), true)
		require.NoError(t, err)
		exp := `{
	"type": "json_schema",
	"json_schema": {
		"name": "Search",
		"strict": true,
		"schema": {
			"type": "object",
			"properties": {
				"args": {
					"type": "array",
					"title": "Args",
					"description": "Arguments for the search",
					"items": {
						"type": "object",
						"properties": {
							"key": {
								"type": "string",
								"title": "Key",
								"description": "Key of the pair"
							},
							"value": {
								"type": "string",
								"title": "Value",
								"description": "Value of the pair"
							}
						},
						"additionalProperties": false,
						"required": [
							"key",
							"value"
						]
					}
				},
				"prov": {
					"type": "object",
					"title": "Prov",
					"description": "Provider for the search",
					"properties": {
						"key": {
							"type": "string",
							"title": "Key",
							"description": "Key of the pair"
						},
						"value": {
							"type": "string",
							"title": "Value",
							"description": "Value of the pair"
						}
					},
					"additionalProperties": false,
					"required": [
						"key",
						"value"
					]
				},
				"query": {
					"type": "string",
					"title": "Query",
					"description": "Query to search for relevant content",
					"examples": [
						"what is golang"
					]
				},
				"topic": {
					"type": "string",
					"title": "Topic",
					"description": "Topic of the search, with coma.",
					"examples": [
						"golang"
					]
				},
				"type": {
					"type": "string",
					"title": "Type",
					"description": "Type of search",
					"enum": [
						"web",
						"image",
						"video"
					],
					"default": "web"
				}
			},
			"additionalProperties": false,
			"required": [
				"query",
				"type"
			]
		}
	}
}`
		assert.Equal(t, exp, llmutils.ToJSONIndent(rf))
	})

	t.Run("OrchestratorResult", func(t *testing.T) {
		t.Parallel()
		rf, err := schema.NewResponseFormat(reflect.TypeOf(OrchestratorResult{}), true)
		require.NoError(t, err)
		exp := `{
	"type": "json_schema",
	"json_schema": {
		"name": "OrchestratorResult",
		"strict": true,
		"schema": {
			"type": "object",
			"properties": {
				"actions": {
					"type": "array",
					"title": "Actions",
					"description": "a list of actions to execute to produce the final answer",
					"items": {
						"type": "object",
						"properties": {
							"actionId": {
								"type": "string",
								"title": "Action ID",
								"description": "unique ID for this action in this chat execution context. The last action is the original question and depends on all other actions, if any"
							},
							"assistantId": {
								"type": "string",
								"title": "Assistant ID",
								"description": "optional, an assistant ID that needs to fulfill this step"
							},
							"classification": {
								"type": "string",
								"title": "Question Classification",
								"description": "classification of the question",
								"enum": [
									"irrelevant",
									"generic",
									"domain_specific"
								]
							},
							"dependsOnActionId": {
								"type": "array",
								"title": "Depends On Actions",
								"description": "list of action IDs that must complete and provide their output before this action",
								"items": {
									"type": "string"
								}
							},
							"question": {
								"type": "string",
								"title": "Question",
								"description": "the question or sub-task for this action"
							},
							"role": {
								"type": "string",
								"title": "Role",
								"description": "role of the question",
								"enum": [
									"human",
									"ai",
									"assistant",
									"system"
								]
							}
						},
						"additionalProperties": false,
						"required": [
							"actionId",
							"classification",
							"role",
							"question"
						]
					}
				},
				"answer": {
					"type": "string",
					"title": "Final Answer",
					"description": "a final answer, if no actions are required from Agents, and you can provide the answer, or return clarification request"
				},
				"chatTitle": {
					"type": "string",
					"title": "Chat Title",
					"description": "a brief title for the chat session"
				}
			},
			"additionalProperties": false,
			"required": [
				"actions"
			]
		}
	}
}`
		assert.Equal(t, exp, llmutils.ToJSONIndent(rf))
	})
}

type Action struct {
	ActionID          string   `json:"actionId" yaml:"actionId" jsonschema:"title=Action ID,description=unique ID for this action in this chat execution context. The last action is the original question and depends on all other actions\\, if any"`
	DependsOnActionID []string `json:"dependsOnActionId,omitempty" yaml:"dependsOnActionId" jsonschema:"title=Depends On Actions,description=list of action IDs that must complete and provide their output before this action"`
	Classification    string   `json:"classification" yaml:"classification" jsonschema:"title=Question Classification,description=classification of the question,enum=irrelevant,enum=generic,enum=domain_specific"`
	Role              string   `json:"role" yaml:"role" jsonschema:"title=Role,description=role of the question,enum=human,enum=ai,enum=assistant,enum=system"`
	Question          string   `json:"question" yaml:"question" jsonschema:"title=Question,description=the question or sub-task for this action"`
	AssistantID       string   `json:"assistantId,omitempty" yaml:"assistantId" jsonschema:"title=Assistant ID,description=optional\\, an assistant ID that needs to fulfill this step"`
}

type OrchestratorResult struct {
	Answer    string   `json:"answer,omitempty" yaml:"answer" jsonschema:"title=Final Answer,description=a final answer\\, if no actions are required from Agents\\, and you can provide the answer\\, or return clarification request"`
	ChatTitle string   `json:"chatTitle,omitempty" yaml:"chatTitle" jsonschema:"title=Chat Title,description=a brief title for the chat session"`
	Actions   []Action `json:"actions" yaml:"actions" jsonschema:"title=Actions,description=a list of actions to execute to produce the final answer"`
}

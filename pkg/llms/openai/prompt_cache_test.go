package openai

import (
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/openai/internal/openaiclient"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePromptCacheRequestConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts llms.CallOptions
		want promptCacheRequestConfig
	}{
		{
			name: "policy request key and retention",
			opts: llms.CallOptions{
				PromptCachePolicy: &llms.PromptCachePolicy{
					Request: &llms.PromptCacheRequestPolicy{
						Key:       "policy-key",
						Retention: llms.PromptCacheRetentionInMemory,
					},
				},
			},
			want: promptCacheRequestConfig{
				key:          "policy-key",
				retention:    llms.PromptCacheRetentionInMemory,
				hasKey:       true,
				hasRetention: true,
			},
		},
		{
			name: "policy request key only",
			opts: llms.CallOptions{
				PromptCachePolicy: &llms.PromptCachePolicy{
					Request: &llms.PromptCacheRequestPolicy{
						Key: "policy-key",
					},
				},
			},
			want: promptCacheRequestConfig{
				key:    "policy-key",
				hasKey: true,
			},
		},
		{
			name: "policy breakpoints only ignored for openai request fields",
			opts: llms.CallOptions{
				PromptCachePolicy: &llms.PromptCachePolicy{
					Breakpoints: []llms.PromptCacheBreakpoint{
						{
							Target: llms.PromptCacheTarget{
								Kind:         llms.PromptCacheTargetMessagePart,
								MessageIndex: 0,
								PartIndex:    0,
							},
						},
					},
				},
			},
			want: promptCacheRequestConfig{},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolvePromptCacheRequestConfig(&tt.opts)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestApplyPromptCacheToChatRequest(t *testing.T) {
	t.Parallel()

	t.Run("supported provider", func(t *testing.T) {
		t.Parallel()

		req := &openaiclient.ChatRequest{}
		opts := llms.CallOptions{
			PromptCachePolicy: &llms.PromptCachePolicy{
				Request: &llms.PromptCacheRequestPolicy{
					Key:       "chat-key",
					Retention: llms.PromptCacheRetentionInMemory,
				},
			},
		}

		applyPromptCacheToChatRequest(req, openaiclient.ProviderOpenAI, &opts)
		assert.Equal(t, "chat-key", req.PromptCacheKey)
		assert.Equal(t, "in_memory", req.PromptCacheRetention)
	})

	t.Run("unsupported provider ignored", func(t *testing.T) {
		t.Parallel()

		req := &openaiclient.ChatRequest{}
		opts := llms.CallOptions{
			PromptCachePolicy: &llms.PromptCachePolicy{
				Request: &llms.PromptCacheRequestPolicy{
					Key:       "chat-key",
					Retention: llms.PromptCacheRetention24h,
				},
			},
		}

		applyPromptCacheToChatRequest(req, openaiclient.ProviderOpenRouter, &opts)
		assert.Empty(t, req.PromptCacheKey)
		assert.Empty(t, req.PromptCacheRetention)
	})
}

func TestApplyPromptCacheToResponsesRequest(t *testing.T) {
	t.Parallel()

	req := &responses.ResponseNewParams{}
	opts := llms.CallOptions{
		PromptCachePolicy: &llms.PromptCachePolicy{
			Request: &llms.PromptCacheRequestPolicy{
				Key:       "resp-key",
				Retention: llms.PromptCacheRetentionInMemory,
			},
		},
	}

	applyPromptCacheToResponsesRequest(req, openaiclient.ProviderOpenAI, &opts)

	// SDK constant ResponseNewParamsPromptCacheRetentionInMemory = "in-memory" is stale;
	// the API requires "in_memory" (underscore).
	assert.Equal(t, responses.ResponseNewParamsPromptCacheRetention("in_memory"), req.PromptCacheRetention)
	require.True(t, req.PromptCacheKey.Valid())
	assert.Equal(t, "resp-key", req.PromptCacheKey.Value)
}

func TestResolvePromptCacheRequestConfig_NilOptions(t *testing.T) {
	t.Parallel()

	assert.Equal(t, promptCacheRequestConfig{}, resolvePromptCacheRequestConfig(nil))
}

func TestPromptCacheRetentionWireValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in            llms.PromptCacheRetention
		wantChat      string
		wantResponses responses.ResponseNewParamsPromptCacheRetention
	}{
		{
			in:            llms.PromptCacheRetentionInMemory,
			wantChat:      "in_memory",
			wantResponses: "in_memory",
		},
		{
			in:            llms.PromptCacheRetention24h,
			wantChat:      "24h",
			wantResponses: responses.ResponseNewParamsPromptCacheRetention24h,
		},
		{
			in:            "7d",
			wantChat:      "7d",
			wantResponses: "7d",
		},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.wantChat, toChatPromptCacheRetention(tc.in), string(tc.in))
		assert.Equal(t, tc.wantResponses, toResponsesPromptCacheRetention(tc.in), string(tc.in))
	}
}

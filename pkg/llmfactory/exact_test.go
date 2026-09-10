package llmfactory

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExactResolution(t *testing.T) {
	cfg := &Config{
		Providers: []*ProviderConfig{{
			Name:            "A",
			Token:           "fakekey",
			DefaultModel:    "fallback",
			AvailableModels: []string{"chosen", "org/model"},
			OpenAI: OpenAIConfig{
				APIType: "OPENAI",
			},
		}, {
			Name:            "B",
			Token:           "fakekey",
			DefaultModel:    "chosen",
			AvailableModels: []string{"chosen"},
			OpenAI: OpenAIConfig{
				APIType: "OPENAI",
			},
		}},
	}
	f := New(cfg)
	ctx := context.Background()
	_, err := ResolveExact(ctx, f, "A", "missing", "tenant")
	require.ErrorIs(t, err, ErrModelNotFound)
	a, err := ResolveExact(ctx, f, "A", "chosen", "tenant")
	require.NoError(t, err)
	assert.Equal(t, "chosen", a.GetName())
	again, err := ResolveExact(ctx, f, "A", "chosen", "tenant")
	require.NoError(t, err)
	assert.Same(t, a, again)
	b, err := ResolveExact(ctx, f, "B", "chosen", "tenant")
	require.NoError(t, err)
	assert.NotSame(t, a, b)
	otherTenant, err := ResolveExact(ctx, f, "A", "chosen", "other")
	require.NoError(t, err)
	assert.Same(t, a, otherTenant, "credentials do not vary by organization; instances are shared")
	_, err = ResolveExact(ctx, f, "A", "org/model", "tenant")
	require.NoError(t, err)
	_, err = ResolveExact(ctx, f, "A", "fallback", "tenant")
	require.NoError(t, err)
	denied := f.WithModelFilter(func(_ context.Context, org, _, _ string) bool { return org != "tenant" })
	_, err = ResolveExact(ctx, denied, "A", "chosen", "tenant")
	require.ErrorIs(t, err, ErrModelNotFound)
	legacy, err := f.GetModel(ctx, ModelOptions{
		PreferredModels: []string{"missing"},
	})
	require.NoError(t, err)
	assert.Equal(t, "fallback", legacy.GetName())
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = ResolveExact(ctx, f, "A", "chosen", "tenant")
	require.ErrorIs(t, err, context.Canceled)
	cfg.Providers = append(cfg.Providers, cfg.Providers[0])
	_, err = ResolveExact(context.Background(), New(cfg), "A", "chosen", "tenant")
	require.ErrorContains(t, err, "ambiguous")
}

func TestExactConstructionRunsOutsideLock(t *testing.T) {
	cfg := &Config{
		Providers: []*ProviderConfig{{
			Name:            "one",
			Token:           "fakekey",
			AvailableModels: []string{"chosen"},
			OpenAI: OpenAIConfig{
				APIType: string(llms.ProviderOpenAI),
			},
		}},
	}
	f := New(cfg).(*factory)
	orig := NewLLM
	t.Cleanup(func() { NewLLM = orig })
	constructions := 0
	NewLLM = func(cfg *ProviderConfig, preferredModels []string, opts *Options) (llms.Model, error) {
		constructions++
		require.True(t, f.lock.TryLock(), "factory lock must not be held during SDK construction")
		f.lock.Unlock()
		return orig(cfg, preferredModels, opts)
	}
	first, err := f.GetModelExact(context.Background(), "one", "chosen", "")
	require.NoError(t, err)
	second, err := f.GetModelExact(context.Background(), "one", "chosen", "")
	require.NoError(t, err)
	assert.Same(t, first, second)
	assert.Equal(t, 1, constructions)
}

func TestExactInferenceAPIConfiguration(t *testing.T) {
	for _, api := range []string{"", "responses", "chat", "RESPONSES"} {
		t.Run(api, func(t *testing.T) {
			cfg := &Config{
				Providers: []*ProviderConfig{{
					Name:            "one",
					Token:           "fakekey",
					AvailableModels: []string{"chosen"},
					OpenAI: OpenAIConfig{
						APIType:      string(llms.ProviderOpenAI),
						InferenceAPI: api,
					},
				}},
			}
			m, err := ResolveExact(context.Background(), New(cfg), "one", "chosen", "")
			require.NoError(t, err)
			_, ok := m.(llms.InferenceModel)
			assert.True(t, ok)
		})
	}
	cfg := &Config{
		Providers: []*ProviderConfig{{
			Name:            "one",
			Token:           "fakekey",
			AvailableModels: []string{"chosen"},
			OpenAI: OpenAIConfig{
				APIType:      string(llms.ProviderOpenAI),
				InferenceAPI: "grpc",
			},
		}},
	}
	_, err := ResolveExact(context.Background(), New(cfg), "one", "chosen", "")
	require.ErrorContains(t, err, `unsupported inference_api "grpc"`)
}

func TestExactConverseConfiguration(t *testing.T) {
	// Constructing an OpenAI model with a default-only name must remain exact.
	cfg := &Config{
		Providers: []*ProviderConfig{{
			Name:         "one",
			Token:        "fakekey",
			DefaultModel: "chosen",
			OpenAI: OpenAIConfig{
				APIType: string(llms.ProviderOpenAI),
			},
		}},
	}
	m, err := ResolveExact(context.Background(), New(cfg), "one", "chosen", "")
	require.NoError(t, err)
	assert.Equal(t, "chosen", m.GetName())
}

type exactHTTPClient struct{ calls int }

func (c *exactHTTPClient) Do(r *http.Request) (*http.Response, error) {
	c.calls++
	body := `{"choices":[{"message":{"content":"hello"},"finish_reason":"stop"}]}`
	switch {
	case strings.Contains(r.URL.Path, "generateContent"):
		body = `{"candidates":[{"content":{"parts":[{"text":"hello"}]},"finishReason":"STOP"}]}`
	case strings.Contains(r.URL.Path, "responses"):
		body = `{"id":"resp","status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello","annotations":[]}]}]}`
	}
	return &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:    io.NopCloser(strings.NewReader(body)),
		Request: r,
	}, nil
}
func TestExactHTTPClientInjection(t *testing.T) {
	for _, family := range []string{"OPENAI", "GOOGLEAI"} {
		t.Run(family, func(t *testing.T) {
			client := &exactHTTPClient{}
			cfg := &Config{
				Providers: []*ProviderConfig{{
					Name:            "provider",
					Token:           "fixture",
					AvailableModels: []string{"fixture"},
					OpenAI: OpenAIConfig{
						APIType: family,
					},
				}},
			}
			m, err := ResolveExact(context.Background(), New(cfg, WithHTTPClient(client)), "provider", "fixture", "tenant")
			require.NoError(t, err)
			inference := m.(llms.InferenceModel)
			_, err = inference.Infer(context.Background(), llms.InferenceRequest{
				Model: "fixture",
				Messages: []llms.InferenceMessage{{
					Role: "user",
					Content: []llms.InferenceBlock{{
						Type: "text",
						Text: "hello",
					}},
				}},
			})
			require.NoError(t, err)
			assert.Equal(t, 1, client.calls)
		})
	}
}

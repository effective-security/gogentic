package llmfactory

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const effortFixture = "testdata/llm_effort.yaml"

// built records one model the factory asked NewLLM to construct.
type built struct {
	provider string
	model    string
	effort   llms.EffortPolicy
}

type stubModel struct {
	built
}

func (m *stubModel) GetName() string { return m.model }

func (m *stubModel) GetProviderType() llms.ProviderType { return llms.ProviderOpenAI }

func (m *stubModel) GenerateContent(context.Context, []llms.Message, ...llms.CallOption) (*llms.ContentResponse, error) {
	return nil, nil
}

// stubNewLLM replaces NewLLM for one test. The stub selects the model the way
// every provider constructor does, with ProviderConfig.FindModel, and records
// what it was asked to build.
func stubNewLLM(t *testing.T) *[]built {
	t.Helper()
	orig := NewLLM
	constructed := &[]built{}
	NewLLM = func(cfg *ProviderConfig, preferredModels []string, opts *Options) (llms.Model, error) {
		model, err := cfg.FindModel(preferredModels...)
		if err != nil {
			return nil, err
		}
		b := built{
			provider: cfg.Name,
			model:    model,
		}
		if opts != nil {
			b.effort = opts.Effort
		}
		*constructed = append(*constructed, b)
		return &stubModel{built: b}, nil
	}
	t.Cleanup(func() { NewLLM = orig })
	return constructed
}

func loadEffortFixture(t *testing.T) *Config {
	t.Helper()
	cfg, err := LoadConfig(effortFixture)
	require.NoError(t, err)
	return cfg
}

func TestFactory_EffortFromConfig(t *testing.T) {
	stubNewLLM(t)
	ctx := context.Background()

	tests := []struct {
		name         string
		opts         ModelOptions
		wantProvider string
		wantModel    string
		wantEffort   llms.EffortPolicy
	}{
		{
			name:         "effort of the available_models entry",
			opts:         ModelOptions{AssistantName: "default"},
			wantProvider: "OPENAI",
			wantModel:    "gpt-5.1",
			wantEffort: llms.EffortPolicy{
				Default: llms.ReasoningEffortMedium,
				Max:     llms.ReasoningEffortHigh,
			},
		},
		{
			name:         "unknown assistant uses the default mapping",
			opts:         ModelOptions{AssistantName: "nobody"},
			wantProvider: "OPENAI",
			wantModel:    "gpt-5.1",
			wantEffort: llms.EffortPolicy{
				Default: llms.ReasoningEffortMedium,
				Max:     llms.ReasoningEffortHigh,
			},
		},
		{
			name:         "reference without an effort on another provider",
			opts:         ModelOptions{AssistantName: "orchestrator"},
			wantProvider: "ANTHROPIC",
			wantModel:    "claude-fable-5-2",
			wantEffort: llms.EffortPolicy{
				Default: llms.ReasoningEffortHigh,
				Max:     llms.ReasoningEffortXHigh,
			},
		},
		{
			name:         "pattern reference with its own effort",
			opts:         ModelOptions{AssistantName: "summarizer"},
			wantProvider: "ANTHROPIC",
			wantModel:    "claude-sonnet-5-5",
			wantEffort: llms.EffortPolicy{
				Default: llms.ReasoningEffortLow,
				Max:     llms.ReasoningEffortXHigh,
			},
		},
		{
			name:         "provider type alias and an effort above the ceiling",
			opts:         ModelOptions{AssistantName: "planner"},
			wantProvider: "ANTHROPIC",
			wantModel:    "claude-opus-4-8",
			wantEffort: llms.EffortPolicy{
				Default: llms.ReasoningEffortMax,
				Max:     llms.ReasoningEffortXHigh,
			},
		},
		{
			name: "org override with a pattern, an effort and a denied match",
			opts: ModelOptions{
				OrgID:         "1000",
				AssistantName: "orchestrator",
			},
			wantProvider: "OPENAI",
			wantModel:    "gpt-5.6-luna",
			wantEffort: llms.EffortPolicy{
				Default: llms.ReasoningEffortXHigh,
				Max:     llms.ReasoningEffortHigh,
			},
		},
		{
			name: "org without an override for the assistant keeps the global mapping",
			opts: ModelOptions{
				OrgID:         "1000",
				AssistantName: "summarizer",
			},
			wantProvider: "ANTHROPIC",
			wantModel:    "claude-sonnet-5-5",
			wantEffort: llms.EffortPolicy{
				Default: llms.ReasoningEffortLow,
				Max:     llms.ReasoningEffortXHigh,
			},
		},
		{
			name: "preferred reference with an effort wins over the entry",
			opts: ModelOptions{
				PreferredModels: []string{"OPENAI/gpt-5.1,low"},
			},
			wantProvider: "OPENAI",
			wantModel:    "gpt-5.1",
			wantEffort: llms.EffortPolicy{
				Default: llms.ReasoningEffortLow,
				Max:     llms.ReasoningEffortHigh,
			},
		},
		{
			name: "no effort anywhere leaves only the ceiling",
			opts: ModelOptions{
				PreferredModels: []string{"gpt-5.5"},
			},
			wantProvider: "OPENAI",
			wantModel:    "gpt-5.5",
			wantEffort: llms.EffortPolicy{
				Max: llms.ReasoningEffortHigh,
			},
		},
		{
			name:         "default model with its own effort",
			opts:         ModelOptions{},
			wantProvider: "OPENAI",
			wantModel:    "gpt-5.1",
			wantEffort: llms.EffortPolicy{
				Default: llms.ReasoningEffortLow,
				Max:     llms.ReasoningEffortHigh,
			},
		},
		{
			name: "provider type uses the default model and its effort",
			opts: ModelOptions{
				ProviderType: llms.ProviderOpenAI,
			},
			wantProvider: "OPENAI",
			wantModel:    "gpt-5.1",
			wantEffort: llms.EffortPolicy{
				Default: llms.ReasoningEffortLow,
				Max:     llms.ReasoningEffortHigh,
			},
		},
		{
			name: "provider type with a pattern default",
			opts: ModelOptions{
				ProviderType: llms.ProviderAnthropic,
			},
			wantProvider: "ANTHROPIC",
			wantModel:    "claude-sonnet-5-5",
			wantEffort: llms.EffortPolicy{
				Default: llms.ReasoningEffortMedium,
				Max:     llms.ReasoningEffortXHigh,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New(loadEffortFixture(t))
			model, err := f.GetModel(ctx, tt.opts)
			require.NoError(t, err)
			got := model.(*stubModel)
			assert.Equal(t, tt.wantProvider, got.provider)
			assert.Equal(t, tt.wantModel, got.model)
			assert.Equal(t, tt.wantEffort, got.effort)
		})
	}
}

func TestFactory_ClientsSharedByEffort(t *testing.T) {
	constructed := stubNewLLM(t)
	ctx := context.Background()
	f := New(loadEffortFixture(t))

	get := func(ref string) llms.Model {
		t.Helper()
		model, err := f.GetModel(ctx, ModelOptions{
			PreferredModels: []string{ref},
		})
		require.NoError(t, err)
		return model
	}

	qualified := get("OPENAI/gpt-5.1")
	again := get("OPENAI/gpt-5.1")
	bare := get("gpt-5.1")
	sameEffort := get("gpt-5.1,medium")
	otherEffort := get("gpt-5.1,high")
	otherModel := get("gpt-5.5")

	// One client per provider, model and effort, however it was named.
	assert.Same(t, qualified, again)
	assert.Same(t, qualified, bare)
	assert.Same(t, qualified, sameEffort)
	assert.NotSame(t, qualified, otherEffort)
	assert.NotSame(t, qualified, otherModel)
	assert.Len(t, *constructed, 3)

	// A factory derived with a filter reuses the clients built so far.
	derived := f.WithModelFilter(func(context.Context, string, string, string) bool { return true })
	fromDerived, err := derived.GetModel(ctx, ModelOptions{
		PreferredModels: []string{"OPENAI/gpt-5.1"},
	})
	require.NoError(t, err)
	assert.Same(t, qualified, fromDerived)
	assert.Len(t, *constructed, 3)
}

func TestFactory_DisabledModelsAndInvalidReferences(t *testing.T) {
	stubNewLLM(t)
	ctx := context.Background()
	f := New(loadEffortFixture(t))

	tests := []struct {
		name      string
		preferred []string
		wantModel string
	}{
		{
			name:      "a denied model is skipped",
			preferred: []string{"OPENAI/gpt-5.6-sol", "OPENAI/gpt-5.5"},
			wantModel: "gpt-5.5",
		},
		{
			name:      "a pattern that only matches denied models falls back to the default",
			preferred: []string{"OPENAI/gpt-5.6-s*"},
			wantModel: "gpt-5.1",
		},
		{
			name:      "a pattern that matches nothing is skipped",
			preferred: []string{"OPENAI/gpt-9*", "OPENAI/gpt-5.5"},
			wantModel: "gpt-5.5",
		},
		{
			name:      "a reference with an unknown effort is skipped",
			preferred: []string{"gpt-5.5,hihg", "claude-opus-4-8"},
			wantModel: "claude-opus-4-8",
		},
		{
			name:      "a bare effort level is skipped",
			preferred: []string{"medium", "gpt-5.5"},
			wantModel: "gpt-5.5",
		},
		{
			name:      "a pattern entry admits an ID that is not listed by name",
			preferred: []string{"ANTHROPIC/claude-opus-5-5"},
			wantModel: "claude-opus-5-5",
		},
		{
			name:      "an unqualified pattern goes to the first provider that has a match",
			preferred: []string{"claude-*"},
			wantModel: "claude-sonnet-5-5",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, err := f.GetModel(ctx, ModelOptions{
				PreferredModels: tt.preferred,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.wantModel, model.GetName())
		})
	}
}

func TestFactory_ProviderPrefix(t *testing.T) {
	stubNewLLM(t)
	ctx := context.Background()
	cfg := &Config{
		DefaultProvider: "fallback",
		Providers: []*ProviderConfig{
			{
				Name:            "fallback",
				DefaultModel:    "fallback-model",
				AvailableModels: []string{"fallback-model"},
				OpenAI: OpenAIConfig{
					APIType: "OPENAI",
				},
			},
			{
				Name:            "google",
				AvailableModels: []string{"gemini-3-pro"},
				OpenAI: OpenAIConfig{
					APIType: "GOOGLEAI",
				},
			},
			{
				Name:            "openrouter",
				AvailableModels: []string{"openai/gpt-oss-120b", "google/gemini-3-pro"},
				OpenAI: OpenAIConfig{
					APIType: "OPENROUTER",
				},
			},
			{
				// A provider whose name is another provider type's alias.
				Name:            "CLAUDE",
				AvailableModels: []string{"gemini-3-pro"},
				OpenAI: OpenAIConfig{
					APIType: "OPENROUTER",
				},
			},
		},
	}
	f := New(cfg)

	tests := []struct {
		ref          string
		wantProvider string
		wantModel    string
	}{
		// A provider type or alias in upper case addresses providers of that type.
		{"GEMINI/gemini-3-pro", "google", "gemini-3-pro"},
		{"GOOGLEAI/gemini-3-pro", "google", "gemini-3-pro"},
		{"GOOGLE_AI/gemini-*", "google", "gemini-3-pro"},
		{"OPENROUTER/google/gemini-3-pro", "openrouter", "google/gemini-3-pro"},
		// A configured provider name wins, whatever it looks like.
		{"google/gemini-3-pro", "google", "gemini-3-pro"},
		{"CLAUDE/gemini-3-pro", "CLAUDE", "gemini-3-pro"},
		// A lower-case vendor prefix is part of the model ID.
		{"openai/gpt-oss-120b", "openrouter", "openai/gpt-oss-120b"},
		// Not a type in upper case, a type nobody is configured with, and an
		// unknown prefix: all part of a model ID that no provider offers.
		{"gemini/gemini-3-pro", "fallback", "fallback-model"},
		{"PERPLEXITY/sonar", "fallback", "fallback-model"},
		{"MISTRAL/large", "fallback", "fallback-model"},
		// Without a prefix the first provider that offers the model wins.
		{"gemini-3-pro", "google", "gemini-3-pro"},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			model, err := f.GetModel(ctx, ModelOptions{
				PreferredModels: []string{tt.ref},
			})
			require.NoError(t, err)
			got := model.(*stubModel)
			assert.Equal(t, tt.wantProvider, got.provider)
			assert.Equal(t, tt.wantModel, got.model)
		})
	}
}

// The model filter sees concrete model IDs only: never a pattern and never an
// effort suffix.
func TestFactory_FilterSeesConcreteModels(t *testing.T) {
	stubNewLLM(t)
	ctx := context.Background()

	tests := []struct {
		name      string
		ref       string
		deny      string
		wantSeen  []string
		wantModel string
	}{
		{
			name:      "exact reference with a provider and an effort",
			ref:       "ANTHROPIC/claude-opus-4-8,high",
			wantSeen:  []string{"ANTHROPIC/claude-opus-4-8", "claude-opus-4-8"},
			wantModel: "claude-opus-4-8",
		},
		{
			name:      "exact reference without a provider",
			ref:       "claude-opus-4-8,high",
			wantSeen:  []string{"claude-opus-4-8"},
			wantModel: "claude-opus-4-8",
		},
		{
			name:      "pattern with a provider",
			ref:       "ANTHROPIC/claude-sonnet-*",
			wantSeen:  []string{"ANTHROPIC/claude-sonnet-5-5", "claude-sonnet-5-5"},
			wantModel: "claude-sonnet-5-5",
		},
		{
			name:      "pattern with a provider type alias",
			ref:       "CLAUDE/claude-sonnet-*,low",
			wantSeen:  []string{"CLAUDE/claude-sonnet-5-5", "claude-sonnet-5-5"},
			wantModel: "claude-sonnet-5-5",
		},
		{
			name:      "pattern without a provider",
			ref:       "claude-sonnet-*",
			wantSeen:  []string{"claude-sonnet-5-5"},
			wantModel: "claude-sonnet-5-5",
		},
		{
			name: "a denied match gives way to the next best one",
			ref:  "ANTHROPIC/claude-sonnet-*",
			deny: "claude-sonnet-5-5",
			wantSeen: []string{
				"ANTHROPIC/claude-sonnet-5-5",
				"claude-sonnet-5-5",
				"ANTHROPIC/claude-sonnet-4-6",
				"claude-sonnet-4-6",
			},
			wantModel: "claude-sonnet-4-6",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen []string
			filter := func(_ context.Context, _, _, model string) bool {
				seen = append(seen, model)
				return model != tt.deny
			}
			f := New(loadEffortFixture(t), WithModelFilter(filter))
			model, err := f.GetModel(ctx, ModelOptions{
				PreferredModels: []string{tt.ref},
			})
			require.NoError(t, err)
			assert.Equal(t, tt.wantModel, model.GetName())
			assert.Equal(t, tt.wantSeen, seen)
		})
	}
}

// A pattern default_model is filtered like a pattern reference: a denied match
// gives way to the next best one instead of failing the resolution.
func TestFactory_DefaultPatternHonoursTheFilter(t *testing.T) {
	ctx := context.Background()
	wantEffort := llms.EffortPolicy{
		Default: llms.ReasoningEffortMedium,
	}
	newConfig := func() *Config {
		return &Config{
			DefaultProvider: "ANTHROPIC",
			Providers: []*ProviderConfig{
				{
					Name:         "ANTHROPIC",
					Token:        "fakekey",
					DefaultModel: "claude-sonnet-*,medium",
					AvailableModels: []string{
						"claude-sonnet-4-6",
						"claude-sonnet-5-5",
						"claude-opus-4-8",
					},
					OpenAI: OpenAIConfig{
						APIType: "ANTHROPIC",
					},
				},
			},
		}
	}

	tests := []struct {
		name      string
		deny      []string
		wantSeen  []string
		wantModel string
		wantErr   string
	}{
		{
			name:      "the best match is used when it is allowed",
			wantSeen:  []string{"claude-sonnet-5-5"},
			wantModel: "claude-sonnet-5-5",
		},
		{
			name:      "a denied match gives way to the next best one",
			deny:      []string{"claude-sonnet-5-5"},
			wantSeen:  []string{"claude-sonnet-5-5", "claude-sonnet-4-6"},
			wantModel: "claude-sonnet-4-6",
		},
		{
			name:     "every match denied",
			deny:     []string{"claude-sonnet-5-5", "claude-sonnet-4-6"},
			wantSeen: []string{"claude-sonnet-5-5", "claude-sonnet-4-6"},
			wantErr:  "model not available for org: claude-sonnet-*",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			constructed := stubNewLLM(t)
			var seen []string
			filter := func(_ context.Context, _, _, model string) bool {
				seen = append(seen, model)
				return !slices.Contains(tt.deny, model)
			}
			f := New(newConfig(), WithModelFilter(filter))
			model, err := f.GetModel(ctx, ModelOptions{})
			assert.Equal(t, tt.wantSeen, seen)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Empty(t, *constructed)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantModel, model.GetName())
			require.Len(t, *constructed, 1)
			assert.Equal(t, wantEffort, (*constructed)[0].effort)
		})
	}
}

func TestExact_ModelPolicy(t *testing.T) {
	stubNewLLM(t)
	ctx := context.Background()
	f := New(loadEffortFixture(t))

	t.Run("exact entry carries its effort and the ceiling", func(t *testing.T) {
		model, err := ResolveExact(ctx, f, "OPENAI", "gpt-5.1", "tenant")
		require.NoError(t, err)
		got := model.(*stubModel)
		assert.Equal(t, "gpt-5.1", got.model)
		assert.Equal(t, llms.EffortPolicy{
			Default: llms.ReasoningEffortMedium,
			Max:     llms.ReasoningEffortHigh,
		}, got.effort)
	})

	t.Run("pattern entry admits an unlisted ID", func(t *testing.T) {
		model, err := ResolveExact(ctx, f, "ANTHROPIC", "claude-opus-5-5", "tenant")
		require.NoError(t, err)
		got := model.(*stubModel)
		assert.Equal(t, "claude-opus-5-5", got.model)
		assert.Equal(t, llms.EffortPolicy{
			Max: llms.ReasoningEffortXHigh,
		}, got.effort)
	})

	t.Run("denied model is not found", func(t *testing.T) {
		_, err := ResolveExact(ctx, f, "OPENAI", "gpt-5.6-sol", "tenant")
		require.ErrorIs(t, err, ErrModelNotFound)
	})

	t.Run("exact resolution never expands a pattern or substitutes", func(t *testing.T) {
		for _, model := range []string{"claude-sonnet-*", "claude-sonnet-9", "gpt-5.5"} {
			_, err := ResolveExact(ctx, f, "ANTHROPIC", model, "tenant")
			require.ErrorIs(t, err, ErrModelNotFound, model)
		}
	})

	t.Run("the model selected by a pattern default is admitted", func(t *testing.T) {
		model, err := ResolveExact(ctx, f, "ANTHROPIC", "claude-sonnet-5-5", "tenant")
		require.NoError(t, err)
		assert.Equal(t, "claude-sonnet-5-5", model.GetName())
	})
}

// New cannot return an error, so it drops what does not parse and keeps the
// rest of the configuration usable.
func TestFactory_NewToleratesInvalidEntries(t *testing.T) {
	stubNewLLM(t)
	ctx := context.Background()
	cfg := &Config{
		Providers: []*ProviderConfig{{
			Name:            "OPENAI",
			DefaultModel:    "gpt-5.1",
			AvailableModels: []string{"gpt-5.1,hihg", "gpt-5.5,high", "medium"},
			DisabledModels:  []string{"gpt-5.1,low"},
			MaxEffort:       "ultra",
			OpenAI: OpenAIConfig{
				APIType: "OPENAI",
			},
		}},
		AssistantModels: map[string][]string{
			"planner": {"gpt-5.5,turbo", "gpt-5.5"},
		},
	}
	require.Error(t, cfg.Validate())

	f := New(cfg)
	model, err := f.GetModel(ctx, ModelOptions{AssistantName: "planner"})
	require.NoError(t, err)
	got := model.(*stubModel)
	assert.Equal(t, "gpt-5.5", got.model)
	assert.Equal(t, llms.EffortPolicy{
		Default: llms.ReasoningEffortHigh,
	}, got.effort)

	// The entry with the mistyped effort is not available by name.
	model, err = f.GetModel(ctx, ModelOptions{
		PreferredModels: []string{"gpt-5.1"},
	})
	require.NoError(t, err)
	assert.Equal(t, "gpt-5.1", model.GetName(), "reached through the default model")
	assert.Equal(t, llms.EffortPolicy{}, model.(*stubModel).effort)
}

// wireClient records request bodies and answers each provider in its own
// response format.
type wireClient struct {
	bodies []map[string]any
}

func (c *wireClient) Do(r *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	c.bodies = append(c.bodies, body)

	reply := `{"id":"resp","status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello","annotations":[]}]}]}`
	switch {
	case strings.Contains(r.URL.Path, "generateContent"):
		reply = `{"candidates":[{"content":{"parts":[{"text":"hello"}]},"finishReason":"STOP"}]}`
	case strings.Contains(r.URL.Path, "messages"):
		reply = `{"id":"msg","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:    io.NopCloser(strings.NewReader(reply)),
		Request: r,
	}, nil
}

func (c *wireClient) last(t *testing.T) map[string]any {
	t.Helper()
	require.NotEmpty(t, c.bodies)
	return c.bodies[len(c.bodies)-1]
}

// The effort written in the configuration reaches the provider request, with
// the real provider constructors.
func TestFactory_EffortReachesTheWire(t *testing.T) {
	ctx := context.Background()
	client := &wireClient{}
	cfg := &Config{
		Providers: []*ProviderConfig{
			{
				Name:            "OPENAI",
				Token:           "fixture",
				AvailableModels: []string{"gpt-5.5,xhigh", "gpt-5.1"},
				MaxEffort:       "high",
				OpenAI: OpenAIConfig{
					APIType: "OPENAI",
				},
			},
			{
				Name:            "ANTHROPIC",
				Token:           "fixture",
				AvailableModels: []string{"claude-fable-5-2"},
				OpenAI: OpenAIConfig{
					APIType: "ANTHROPIC",
				},
			},
			{
				Name:            "GOOGLE",
				Token:           "fixture",
				AvailableModels: []string{"gemini-3.6-flash,none"},
				OpenAI: OpenAIConfig{
					APIType: "GOOGLEAI",
				},
			},
		},
		AssistantModels: map[string][]string{
			"openai":    {"OPENAI/gpt-5.5"},
			"untouched": {"OPENAI/gpt-5.1"},
			"anthropic": {"ANTHROPIC/claude-fable-5-2,medium"},
			"google":    {"GEMINI/gemini-*-flash"},
		},
	}
	require.NoError(t, cfg.Validate())
	f := New(cfg, WithHTTPClient(client))

	generate := func(t *testing.T, assistant string, callOpts ...llms.CallOption) map[string]any {
		t.Helper()
		model, err := f.GetModel(ctx, ModelOptions{AssistantName: assistant})
		require.NoError(t, err)
		messages := []llms.Message{{
			Role: llms.RoleHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: "hi"},
			},
		}}
		_, err = model.GenerateContent(ctx, messages, callOpts...)
		require.NoError(t, err)
		return client.last(t)
	}

	t.Run("openai: entry effort, lowered to the ceiling", func(t *testing.T) {
		body := generate(t, "openai")
		assert.Equal(t, map[string]any{"effort": "high"}, body["reasoning"])
	})

	t.Run("openai: a call option overrides the configured effort", func(t *testing.T) {
		body := generate(t, "openai", llms.WithReasoningEffort(llms.ReasoningEffortLow))
		assert.Equal(t, map[string]any{"effort": "low"}, body["reasoning"])
	})

	t.Run("openai: a call option is lowered to the ceiling too", func(t *testing.T) {
		body := generate(t, "openai", llms.WithReasoningEffort(llms.ReasoningEffortMax))
		assert.Equal(t, map[string]any{"effort": "high"}, body["reasoning"])
	})

	t.Run("openai: no configured effort keeps the client default", func(t *testing.T) {
		body := generate(t, "untouched")
		assert.Equal(t, map[string]any{"effort": "none"}, body["reasoning"])
	})

	t.Run("anthropic: reference effort", func(t *testing.T) {
		body := generate(t, "anthropic")
		assert.Equal(t, map[string]any{"type": "adaptive"}, body["thinking"])
		assert.Equal(t, map[string]any{"effort": "medium"}, body["output_config"])
	})

	t.Run("google: entry effort through a pattern reference", func(t *testing.T) {
		body := generate(t, "google")
		cfg, ok := body["generationConfig"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, map[string]any{"thinkingLevel": "MINIMAL"}, cfg["thinkingConfig"])
	})
}

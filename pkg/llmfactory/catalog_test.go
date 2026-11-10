package llmfactory

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLister is a model lister whose list a test changes.
type fakeLister struct {
	mu     sync.Mutex
	models []llms.ModelInfo
	err    error
	calls  int
	gate   chan struct{}
}

func (l *fakeLister) ListModels(ctx context.Context) ([]llms.ModelInfo, error) {
	l.mu.Lock()
	l.calls++
	models, err, gate := slices.Clone(l.models), l.err, l.gate
	l.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return models, err
}

// serve makes the lister return the models from now on.
func (l *fakeLister) serve(models ...llms.ModelInfo) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.models, l.err = models, nil
}

// fail makes the lister return the error from now on.
func (l *fakeLister) fail(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.err = err
}

// hold makes ListModels wait until the returned function is called.
func (l *fakeLister) hold() (release func()) {
	gate := make(chan struct{})
	l.mu.Lock()
	l.gate = gate
	l.mu.Unlock()
	return func() {
		l.mu.Lock()
		l.gate = nil
		l.mu.Unlock()
		close(gate)
	}
}

// count returns how many times the list was asked for.
func (l *fakeLister) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

// stubModelLister replaces NewModelLister for one test with the listers
// given by provider name, and returns the number of listers handed out.
func stubModelLister(t *testing.T, listers map[string]*fakeLister) *int {
	t.Helper()
	orig := NewModelLister
	var (
		lock  sync.Mutex
		built int
	)
	NewModelLister = func(cfg *ProviderConfig, _ *Options) (llms.ModelLister, error) {
		lock.Lock()
		defer lock.Unlock()
		lister, ok := listers[cfg.Name]
		if !ok {
			return nil, errors.Errorf("no lister for provider %s", cfg.Name)
		}
		built++
		return lister, nil
	}
	t.Cleanup(func() { NewModelLister = orig })
	return &built
}

// fakeClock is a clock a test moves.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// useClock makes every catalog of the factory read the fake clock. It must be
// called before the factory is used.
func useClock(f Factory) *fakeClock {
	clock := &fakeClock{t: time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)}
	for _, policy := range f.(*factory).policies {
		if policy.catalog != nil {
			policy.catalog.now = clock.now
		}
	}
	return clock
}

// catalogOf returns the catalog of a provider of the factory.
func catalogOf(t *testing.T, f Factory, provider string) *modelCatalog {
	t.Helper()
	for _, policy := range f.(*factory).policies {
		if policy.cfg.Name == provider {
			require.NotNil(t, policy.catalog, "provider %s has no catalog", provider)
			return policy.catalog
		}
	}
	require.FailNow(t, "provider not found", provider)
	return nil
}

// settled waits for the fetch in flight, if any, and returns the generation
// of the list the catalog holds: 0 when it has none.
func settled(t *testing.T, c *modelCatalog) uint64 {
	t.Helper()
	var generation uint64
	require.Eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.inflight != nil {
			return false
		}
		if c.list != nil {
			generation = c.list.generation
		}
		return true
	}, 5*time.Second, time.Millisecond)
	return generation
}

func active(id string) llms.ModelInfo {
	return llms.ModelInfo{
		ID:        id,
		Lifecycle: llms.ModelLifecycleActive,
	}
}

func deprecated(id string) llms.ModelInfo {
	return llms.ModelInfo{
		ID:        id,
		Lifecycle: llms.ModelLifecycleDeprecated,
		RetiresAt: time.Date(2026, time.November, 30, 17, 0, 0, 0, time.UTC),
	}
}

func retired(id string) llms.ModelInfo {
	return llms.ModelInfo{
		ID:        id,
		Lifecycle: llms.ModelLifecycleRetired,
	}
}

// unlisted is an entry of a provider that reports no lifecycle.
func unlisted(id string) llms.ModelInfo {
	return llms.ModelInfo{
		ID: id,
	}
}

const (
	matrixProvider  = "ANTHROPIC"
	defaultProvider = "OPENAI"
	defaultModel    = "gpt-5.5"
)

// matrixConfig is the configuration of the resolution matrix in PLAN.md: an
// Anthropic provider that allows every Fable and one Opus, and denies
// previews. A second provider holds the default model, so a reference that
// selects nothing is seen to fall through to it.
func matrixConfig(discovery bool) *Config {
	anthropic := &ProviderConfig{
		Name:  matrixProvider,
		Token: "fakekey",
		AvailableModels: []string{
			"claude-fable-*",
			"claude-opus-4-8",
		},
		DisabledModels: []string{
			"*-preview",
		},
		OpenAI: OpenAIConfig{
			APIType: "ANTHROPIC",
		},
	}
	if discovery {
		anthropic.Discovery = &DiscoveryConfig{
			Enabled: true,
		}
	}
	return &Config{
		DefaultProvider: defaultProvider,
		Providers: []*ProviderConfig{
			anthropic,
			{
				Name:         defaultProvider,
				Token:        "fakekey",
				DefaultModel: defaultModel,
				OpenAI: OpenAIConfig{
					APIType: "OPENAI",
				},
			},
		},
	}
}

// resolveBoth resolves a reference with Resolve and with GetModel, checks
// that they agree, and returns the resolution.
func resolveBoth(t *testing.T, f Factory, opts ModelOptions) *Resolution {
	t.Helper()
	ctx := context.Background()
	res, err := Resolve(ctx, f, opts)
	require.NoError(t, err)
	model, err := f.GetModel(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, res.Model, model.GetName(), "Resolve and GetModel disagree")
	return res
}

// The resolution matrix of PLAN.md.
func TestCatalog_ResolutionMatrix(t *testing.T) {
	tests := []struct {
		name      string
		reference string
		// lists is what the provider lists; nil with fetchErr unset means
		// discovery is off.
		lists    []llms.ModelInfo
		fetchErr error
		// deny is a model the org filter refuses.
		deny string

		wantModel    string
		wantReason   ResolutionReason
		wantVerified bool
		// wantSkipped means the reference selects nothing and the default
		// model of the other provider is used.
		wantSkipped bool
	}{
		{
			name:         "exact",
			reference:    "claude-opus-4-8",
			lists:        []llms.ModelInfo{active("claude-opus-4-8")},
			wantModel:    "claude-opus-4-8",
			wantReason:   ReasonExact,
			wantVerified: true,
		},
		{
			name:         "exact, with a newer model of the family listed",
			reference:    "claude-fable-5-1",
			lists:        []llms.ModelInfo{active("claude-fable-5-1"), active("claude-fable-5-2")},
			wantModel:    "claude-fable-5-1",
			wantReason:   ReasonExact,
			wantVerified: true,
		},
		{
			name:         "family: the named model is retired",
			reference:    "claude-fable-5-1",
			lists:        []llms.ModelInfo{active("claude-fable-5-2"), retired("claude-fable-5-1")},
			wantModel:    "claude-fable-5-2",
			wantReason:   ReasonFamily,
			wantVerified: true,
		},
		{
			name:         "family: the named model left the list",
			reference:    "claude-fable-5-1",
			lists:        []llms.ModelInfo{active("claude-fable-5-2"), active("claude-opus-4-8")},
			wantModel:    "claude-fable-5-2",
			wantReason:   ReasonFamily,
			wantVerified: true,
		},
		{
			name:         "pattern: the newest model",
			reference:    "claude-fable-*",
			lists:        []llms.ModelInfo{deprecated("claude-fable-5-1"), active("claude-fable-5-2")},
			wantModel:    "claude-fable-5-2",
			wantReason:   ReasonPattern,
			wantVerified: true,
		},
		{
			name:         "pattern: a model in service before a newer deprecated one",
			reference:    "claude-fable-*",
			lists:        []llms.ModelInfo{deprecated("claude-fable-5-3"), active("claude-fable-5-2")},
			wantModel:    "claude-fable-5-2",
			wantReason:   ReasonPattern,
			wantVerified: true,
		},
		{
			name:         "pattern: a retired model is not selected",
			reference:    "claude-fable-*",
			lists:        []llms.ModelInfo{retired("claude-fable-5-3"), active("claude-fable-5-2")},
			wantModel:    "claude-fable-5-2",
			wantReason:   ReasonPattern,
			wantVerified: true,
		},
		{
			name:         "pattern: the deny-list wins",
			reference:    "claude-fable-*",
			lists:        []llms.ModelInfo{active("claude-fable-5-2"), active("claude-fable-6-0-preview")},
			wantModel:    "claude-fable-5-2",
			wantReason:   ReasonPattern,
			wantVerified: true,
		},
		{
			name:        "pattern: a denied model is not selected, even as the only match",
			reference:   "claude-fable-*",
			lists:       []llms.ModelInfo{active("claude-fable-6-0-preview")},
			wantSkipped: true,
		},
		{
			name:        "family: a denied model does not stand in",
			reference:   "claude-fable-5-1",
			lists:       []llms.ModelInfo{active("claude-fable-6-0-preview")},
			wantSkipped: true,
		},
		{
			name:         "family: the named model is listed but not allowed",
			reference:    "claude-opus-4-6",
			lists:        []llms.ModelInfo{active("claude-opus-4-6"), active("claude-opus-4-8")},
			wantModel:    "claude-opus-4-8",
			wantReason:   ReasonFamily,
			wantVerified: true,
		},
		{
			name:        "no allowed model in the family",
			reference:   "claude-sonnet-4-8",
			lists:       []llms.ModelInfo{active("claude-sonnet-4-8")},
			wantSkipped: true,
		},
		{
			name:       "the fetch fails and there is no earlier list",
			reference:  "claude-fable-5-1",
			fetchErr:   errors.New("provider is down"),
			wantModel:  "claude-fable-5-1",
			wantReason: ReasonExact,
		},
		{
			name:       "discovery off: an exact ID is never verified",
			reference:  "claude-fable-5-1",
			wantModel:  "claude-fable-5-1",
			wantReason: ReasonExact,
		},
		{
			name:        "discovery off: a pattern matches no listed ID",
			reference:   "claude-fable-*",
			wantSkipped: true,
		},
		{
			name:        "the filter refuses the substitute",
			reference:   "claude-fable-5-1",
			lists:       []llms.ModelInfo{active("claude-fable-5-2")},
			deny:        "claude-fable-5-2",
			wantSkipped: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubNewLLM(t)
			discovery := tt.lists != nil || tt.fetchErr != nil
			lister := &fakeLister{
				models: tt.lists,
				err:    tt.fetchErr,
			}
			stubModelLister(t, map[string]*fakeLister{
				matrixProvider: lister,
			})

			var opts []Option
			if tt.deny != "" {
				opts = append(opts, WithModelFilter(func(_ context.Context, _, _, model string) bool {
					return model != tt.deny
				}))
			}
			f := New(matrixConfig(discovery), opts...)

			res := resolveBoth(t, f, ModelOptions{
				PreferredModels: []string{tt.reference},
			})
			if tt.wantSkipped {
				assert.True(t, res.Default, "the reference was not skipped: %+v", res)
				assert.Equal(t, defaultProvider, res.Provider)
				assert.Equal(t, defaultModel, res.Model)
				return
			}
			assert.False(t, res.Default)
			assert.Equal(t, matrixProvider, res.Provider)
			assert.Equal(t, tt.reference, res.Reference)
			assert.Equal(t, tt.wantModel, res.Model)
			assert.Equal(t, tt.wantReason, res.Reason)
			assert.Equal(t, tt.wantVerified, res.Verified)

			if !discovery {
				assert.Zero(t, lister.count(), "the provider was asked for its models")
			}
		})
	}
}

// The first incident of the intent: a provider retires a model overnight. The
// reference keeps working, with the effort written on it, on the newest model
// of the family.
func TestCatalog_RetiredModelIsReplaced(t *testing.T) {
	constructed := stubNewLLM(t)
	lister := &fakeLister{}
	lister.serve(active("claude-fable-5-1"), active("claude-fable-5-2"))
	stubModelLister(t, map[string]*fakeLister{
		matrixProvider: lister,
	})

	cfg := matrixConfig(true)
	cfg.AssistantModels = map[string][]string{
		"orchestrator": {"ANTHROPIC/claude-fable-5-1,high"},
	}
	f := New(cfg)
	clock := useClock(f)
	ctx := context.Background()
	opts := ModelOptions{AssistantName: "orchestrator"}

	model, err := f.GetModel(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, "claude-fable-5-1", model.GetName())

	// The provider retires 5.1. The next refresh picks it up.
	lister.serve(retired("claude-fable-5-1"), active("claude-fable-5-2"))
	clock.advance(defaultDiscoveryRefresh + time.Second)
	_, err = f.GetModel(ctx, opts)
	require.NoError(t, err)
	require.EqualValues(t, 2, settled(t, catalogOf(t, f, matrixProvider)))

	model, err = f.GetModel(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, "claude-fable-5-2", model.GetName())

	res, err := Resolve(ctx, f, opts)
	require.NoError(t, err)
	assert.Equal(t, &Resolution{
		Provider:  matrixProvider,
		Reference: "ANTHROPIC/claude-fable-5-1,high",
		Model:     "claude-fable-5-2",
		Effort: llms.EffortPolicy{
			Default: llms.ReasoningEffortHigh,
		},
		Reason:    ReasonFamily,
		Verified:  true,
		Lifecycle: llms.ModelLifecycleActive,
	}, res)

	// Both models were built with the effort of the reference.
	require.Len(t, *constructed, 2)
	for _, built := range *constructed {
		assert.Equal(t, llms.ReasoningEffortHigh, built.effort.Default, "model %s", built.model)
	}
}

// The org filter is shown the model that stands in for the named one, first
// with the provider prefix of the reference and then bare, as for a pattern.
func TestCatalog_FilterSeesTheSubstitute(t *testing.T) {
	tests := []struct {
		name      string
		reference string
		deny      string
		wantSeen  []string
		wantModel string
	}{
		{
			name:      "with a provider",
			reference: "ANTHROPIC/claude-fable-5-1,high",
			wantSeen: []string{
				"ANTHROPIC/claude-fable-5-1",
				"ANTHROPIC/claude-fable-5-2",
				"claude-fable-5-2",
			},
			wantModel: "claude-fable-5-2",
		},
		{
			name:      "without a provider",
			reference: "claude-fable-5-1",
			wantSeen: []string{
				"claude-fable-5-1",
				"claude-fable-5-2",
			},
			wantModel: "claude-fable-5-2",
		},
		{
			name:      "the qualified substitute is refused: the next one is tried",
			reference: "ANTHROPIC/claude-fable-5-1",
			deny:      "ANTHROPIC/claude-fable-5-2",
			wantSeen: []string{
				"ANTHROPIC/claude-fable-5-1",
				"ANTHROPIC/claude-fable-5-2",
				"ANTHROPIC/claude-fable-5-0",
				"claude-fable-5-0",
			},
			wantModel: "claude-fable-5-0",
		},
		{
			name:      "the named model is refused: nothing stands in for it",
			reference: "ANTHROPIC/claude-fable-5-1",
			deny:      "ANTHROPIC/claude-fable-5-1",
			wantSeen: []string{
				"ANTHROPIC/claude-fable-5-1",
				defaultModel,
			},
			wantModel: defaultModel,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubNewLLM(t)
			lister := &fakeLister{}
			lister.serve(active("claude-fable-5-2"), active("claude-fable-5-0"))
			stubModelLister(t, map[string]*fakeLister{
				matrixProvider: lister,
			})
			var seen []string
			f := New(matrixConfig(true), WithModelFilter(func(_ context.Context, _, _, model string) bool {
				seen = append(seen, model)
				return model != tt.deny
			}))

			model, err := f.GetModel(context.Background(), ModelOptions{
				PreferredModels: []string{tt.reference},
			})
			require.NoError(t, err)
			assert.Equal(t, tt.wantModel, model.GetName())
			assert.Equal(t, tt.wantSeen, seen)
		})
	}
}

// The second incident of the intent: a Gemini model disappears from the list,
// which reports no lifecycle. A newer model of the family takes over.
func TestCatalog_MissingModelIsReplaced(t *testing.T) {
	stubNewLLM(t)
	lister := &fakeLister{}
	lister.serve(
		unlisted("gemini-3.1-pro"),
		unlisted("gemini-3-pro-preview"),
		unlisted("gemini-3.8-flash"),
		unlisted("gemini-embedding-001"),
	)
	stubModelLister(t, map[string]*fakeLister{
		"GOOGLE": lister,
	})
	cfg := &Config{
		Providers: []*ProviderConfig{
			{
				Name:  "GOOGLE",
				Token: "fakekey",
				AvailableModels: []string{
					"gemini-*",
				},
				DisabledModels: []string{
					"*-preview",
				},
				Discovery: &DiscoveryConfig{
					Enabled: true,
				},
				OpenAI: OpenAIConfig{
					APIType: "GOOGLEAI",
				},
			},
		},
	}
	f := New(cfg)

	res := resolveBoth(t, f, ModelOptions{
		PreferredModels: []string{"GOOGLE/gemini-2.5-pro,medium"},
	})
	assert.Equal(t, "gemini-3.1-pro", res.Model)
	assert.Equal(t, ReasonFamily, res.Reason)
	assert.Equal(t, llms.ReasoningEffortMedium, res.Effort.Default)
	assert.True(t, res.Verified)

	// A model of another family is not a substitute: Flash does not stand in
	// for Flash-Lite. The reference selects nothing, and this configuration
	// has no default model to fall back to.
	_, err := Resolve(context.Background(), f, ModelOptions{
		PreferredModels: []string{"GOOGLE/gemini-2.5-flash-lite"},
	})
	require.EqualError(t, err, "no default model configured")
}

// A floating ID that the provider accepts but lists only as a dated snapshot
// is served, not replaced.
func TestCatalog_FloatingIDIsServedByItsSnapshot(t *testing.T) {
	stubNewLLM(t)
	lister := &fakeLister{}
	stubModelLister(t, map[string]*fakeLister{
		matrixProvider: lister,
	})
	cfg := &Config{
		Providers: []*ProviderConfig{
			{
				Name:  matrixProvider,
				Token: "fakekey",
				AvailableModels: []string{
					"claude-haiku-4-5",
					"claude-haiku-5-5",
					"claude-sonnet-*",
				},
				Discovery: &DiscoveryConfig{
					Enabled: true,
				},
				OpenAI: OpenAIConfig{
					APIType: "ANTHROPIC",
				},
			},
		},
	}
	ctx := context.Background()

	t.Run("the snapshot is in service", func(t *testing.T) {
		lister.serve(
			active("claude-haiku-5-5"),
			active("claude-haiku-4-5-20251001"),
			deprecated("claude-sonnet-4-5-20250929"),
			active("claude-sonnet-5-5"),
		)
		f := New(cfg)
		res := resolveBoth(t, f, ModelOptions{
			PreferredModels: []string{"claude-haiku-4-5"},
		})
		assert.Equal(t, "claude-haiku-4-5", res.Model)
		assert.Equal(t, ReasonExact, res.Reason)
		assert.True(t, res.Verified)
		assert.Equal(t, llms.ModelLifecycleActive, res.Lifecycle)

		// A deprecated snapshot still serves its floating ID, and says so.
		res = resolveBoth(t, f, ModelOptions{
			PreferredModels: []string{"claude-sonnet-4-5"},
		})
		assert.Equal(t, "claude-sonnet-4-5", res.Model)
		assert.Equal(t, ReasonExact, res.Reason)
		assert.Equal(t, llms.ModelLifecycleDeprecated, res.Lifecycle)
		assert.False(t, res.RetiresAt.IsZero())

		// A pattern sees the floating ID the configuration names, too.
		res, err := Resolve(ctx, f, ModelOptions{
			PreferredModels: []string{"claude-haiku-4*"},
		})
		require.NoError(t, err)
		assert.Equal(t, "claude-haiku-4-5", res.Model)
		assert.Equal(t, ReasonPattern, res.Reason)
	})

	t.Run("the snapshot is retired", func(t *testing.T) {
		lister.serve(
			active("claude-haiku-5-5"),
			retired("claude-haiku-4-5-20251001"),
		)
		f := New(cfg)
		res := resolveBoth(t, f, ModelOptions{
			PreferredModels: []string{"claude-haiku-4-5"},
		})
		assert.Equal(t, "claude-haiku-5-5", res.Model)
		assert.Equal(t, ReasonFamily, res.Reason)
	})

	t.Run("a dated reference needs its own snapshot", func(t *testing.T) {
		lister.serve(
			active("claude-sonnet-5-5"),
			active("claude-sonnet-4-5"),
		)
		f := New(cfg)
		res := resolveBoth(t, f, ModelOptions{
			PreferredModels: []string{"claude-sonnet-4-5-20250929"},
		})
		assert.Equal(t, "claude-sonnet-5-5", res.Model)
		assert.Equal(t, ReasonFamily, res.Reason)
	})
}

// The model a reference names wins on any provider before a substitute is
// used on an earlier one.
func TestCatalog_NamedModelBeatsSubstituteOnEarlierProvider(t *testing.T) {
	stubNewLLM(t)
	first := &fakeLister{}
	first.serve(active("gpt-5.1"), active("gpt-5.5"))
	stubModelLister(t, map[string]*fakeLister{
		"FIRST": first,
	})
	cfg := &Config{
		Providers: []*ProviderConfig{
			{
				Name:            "FIRST",
				Token:           "fakekey",
				AvailableModels: []string{"gpt-5.1"},
				Discovery: &DiscoveryConfig{
					Enabled: true,
				},
				OpenAI: OpenAIConfig{
					APIType: "OPENAI",
				},
			},
			{
				Name:            "SECOND",
				Token:           "fakekey",
				AvailableModels: []string{"gpt-5.5"},
				OpenAI: OpenAIConfig{
					APIType: "OPENAI",
				},
			},
		},
	}
	f := New(cfg)

	res := resolveBoth(t, f, ModelOptions{
		PreferredModels: []string{"gpt-5.5"},
	})
	assert.Equal(t, "SECOND", res.Provider)
	assert.Equal(t, "gpt-5.5", res.Model)
	assert.Equal(t, ReasonExact, res.Reason)

	// Only when no provider has the named model is the substitute used.
	res = resolveBoth(t, f, ModelOptions{
		PreferredModels: []string{"FIRST/gpt-5.5"},
	})
	assert.Equal(t, "FIRST", res.Provider)
	assert.Equal(t, "gpt-5.1", res.Model)
	assert.Equal(t, ReasonFamily, res.Reason)
}

func TestCatalog_DefaultModel(t *testing.T) {
	ctx := context.Background()
	newConfig := func(defaultModel string) *Config {
		return &Config{
			Providers: []*ProviderConfig{
				{
					Name:         matrixProvider,
					Token:        "fakekey",
					DefaultModel: defaultModel,
					AvailableModels: []string{
						"claude-sonnet-*",
					},
					Discovery: &DiscoveryConfig{
						Enabled: true,
					},
					OpenAI: OpenAIConfig{
						APIType: "ANTHROPIC",
					},
				},
			},
		}
	}
	listed := []llms.ModelInfo{
		active("claude-sonnet-5-5"),
		active("claude-sonnet-4-6"),
		active("claude-opus-5-5"),
	}

	tests := []struct {
		name         string
		defaultModel string
		lists        []llms.ModelInfo
		deny         []string
		byType       bool
		wantModel    string
		wantReason   ResolutionReason
		wantErr      string
	}{
		{
			name:         "a default that is served needs no allow entry",
			defaultModel: "claude-opus-5-5",
			lists:        listed,
			wantModel:    "claude-opus-5-5",
			wantReason:   ReasonExact,
		},
		{
			name:         "a default the provider no longer serves gives way to its family",
			defaultModel: "claude-sonnet-4-0,low",
			lists:        listed,
			wantModel:    "claude-sonnet-5-5",
			wantReason:   ReasonFamily,
		},
		{
			name:         "the filter moves on to the next model of the family",
			defaultModel: "claude-sonnet-4-0",
			lists:        listed,
			deny:         []string{"claude-sonnet-5-5"},
			wantModel:    "claude-sonnet-4-6",
			wantReason:   ReasonFamily,
		},
		{
			name:         "a pattern default selects from the provider's list",
			defaultModel: "claude-sonnet-*",
			lists:        listed,
			wantModel:    "claude-sonnet-5-5",
			wantReason:   ReasonPattern,
		},
		{
			name:         "a default with no usable model of its family",
			defaultModel: "claude-fable-5-1",
			lists:        listed,
			wantErr:      "default model claude-fable-5-1 of provider ANTHROPIC is not available",
		},
		{
			name:         "by provider type",
			defaultModel: "claude-sonnet-4-0",
			lists:        listed,
			byType:       true,
			wantModel:    "claude-sonnet-5-5",
			wantReason:   ReasonFamily,
		},
		{
			// No client is built for a default the list says is gone.
			name:         "by provider type, with no usable model of its family",
			defaultModel: "claude-fable-5-1",
			lists:        listed,
			byType:       true,
			wantErr:      "default model claude-fable-5-1 of provider ANTHROPIC is not available",
		},
		{
			name:         "by provider type, with a pattern that matches nothing served",
			defaultModel: "claude-fable-*",
			lists:        listed,
			byType:       true,
			wantErr:      "default model claude-fable-* of provider ANTHROPIC is not available",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubNewLLM(t)
			lister := &fakeLister{models: tt.lists}
			stubModelLister(t, map[string]*fakeLister{
				matrixProvider: lister,
			})
			cfg := newConfig(tt.defaultModel)
			require.NoError(t, cfg.Validate())
			f := New(cfg, WithModelFilter(func(_ context.Context, _, _, model string) bool {
				return !slices.Contains(tt.deny, model)
			}))

			opts := ModelOptions{}
			if tt.byType {
				opts.ProviderType = llms.ProviderAnthropic
			}
			if tt.wantErr != "" {
				constructed := stubNewLLM(t)
				_, err := Resolve(ctx, f, opts)
				require.EqualError(t, err, tt.wantErr)
				model, err := f.GetModel(ctx, opts)
				require.EqualError(t, err, tt.wantErr)
				assert.Nil(t, model)
				assert.Empty(t, *constructed, "a client was built for a model that cannot be used")
				return
			}
			res := resolveBoth(t, f, opts)
			assert.True(t, res.Default)
			assert.Equal(t, tt.defaultModel, res.Reference)
			assert.Equal(t, tt.wantModel, res.Model)
			assert.Equal(t, tt.wantReason, res.Reason)
		})
	}
}

// What the provider reports about the reasoning levels of a model reaches the
// client built for it, and a change of it builds a new client.
func TestCatalog_EffortReportedByProvider(t *testing.T) {
	constructed := stubNewLLM(t)
	lowToMax := llms.NewEffortSet(
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
		llms.ReasoningEffortXHigh,
		llms.ReasoningEffortMax,
	)
	model := llms.ModelInfo{
		ID:            "openai/gpt-6-sol",
		Lifecycle:     llms.ModelLifecycleActive,
		Efforts:       lowToMax,
		DefaultEffort: llms.ReasoningEffortMedium,
	}
	lister := &fakeLister{}
	lister.serve(model)
	stubModelLister(t, map[string]*fakeLister{
		"OPENROUTER": lister,
	})
	cfg := &Config{
		Providers: []*ProviderConfig{
			{
				Name:            "OPENROUTER",
				Token:           "fakekey",
				AvailableModels: []string{"openai/*"},
				MaxEffort:       "xhigh",
				Discovery: &DiscoveryConfig{
					Enabled: true,
				},
				OpenAI: OpenAIConfig{
					APIType: "OPENROUTER",
				},
			},
		},
	}
	f := New(cfg)
	clock := useClock(f)
	ctx := context.Background()
	opts := ModelOptions{
		PreferredModels: []string{"OPENROUTER/openai/gpt-6-sol,high"},
	}

	_, err := f.GetModel(ctx, opts)
	require.NoError(t, err)
	_, err = f.GetModel(ctx, opts)
	require.NoError(t, err)
	require.Len(t, *constructed, 1, "the client is shared while the list does not change")
	assert.Equal(t, llms.EffortPolicy{
		Default:   llms.ReasoningEffortHigh,
		Max:       llms.ReasoningEffortXHigh,
		Supported: lowToMax,
		Implicit:  llms.ReasoningEffortMedium,
	}, (*constructed)[0].effort)

	// The provider changes what it reports for the model.
	model.Efforts = lowToMax.With(llms.ReasoningEffortNone)
	lister.serve(model)
	clock.advance(defaultDiscoveryRefresh + time.Second)
	_, err = f.GetModel(ctx, opts)
	require.NoError(t, err)
	require.EqualValues(t, 2, settled(t, catalogOf(t, f, "OPENROUTER")))

	_, err = f.GetModel(ctx, opts)
	require.NoError(t, err)
	require.Len(t, *constructed, 2)
	assert.Equal(t, model.Efforts, (*constructed)[1].effort.Supported)
}

func TestCatalog_FirstFetchFailureDegradesAndIsRetried(t *testing.T) {
	stubNewLLM(t)
	lister := &fakeLister{}
	lister.fail(errors.New("provider is down"))
	built := stubModelLister(t, map[string]*fakeLister{
		matrixProvider: lister,
	})
	f := New(matrixConfig(true))
	clock := useClock(f)
	opts := ModelOptions{
		PreferredModels: []string{"claude-fable-5-1"},
	}

	// Without a list the factory resolves as it does without discovery.
	res := resolveBoth(t, f, opts)
	assert.Equal(t, "claude-fable-5-1", res.Model)
	assert.False(t, res.Verified)
	assert.Equal(t, 1, lister.count(), "Resolve and GetModel share one failed fetch")

	// The provider is not asked again within the retry interval.
	lister.serve(active("claude-fable-5-2"))
	clock.advance(modelListRetryInterval - time.Second)
	res = resolveBoth(t, f, opts)
	assert.Equal(t, "claude-fable-5-1", res.Model)
	assert.Equal(t, 1, lister.count())

	// After it, the next resolution waits for the list.
	clock.advance(2 * time.Second)
	res = resolveBoth(t, f, opts)
	assert.Equal(t, "claude-fable-5-2", res.Model)
	assert.Equal(t, ReasonFamily, res.Reason)
	assert.True(t, res.Verified)
	assert.Equal(t, 2, lister.count())
	assert.Equal(t, 1, *built, "the lister is built once")
}

func TestCatalog_RefreshInBackground(t *testing.T) {
	stubNewLLM(t)
	lister := &fakeLister{}
	lister.serve(active("claude-fable-5-1"), active("claude-fable-5-2"))
	stubModelLister(t, map[string]*fakeLister{
		matrixProvider: lister,
	})
	cfg := matrixConfig(true)
	cfg.Providers[0].Discovery.Refresh = "6h"
	f := New(cfg)
	clock := useClock(f)
	catalog := catalogOf(t, f, matrixProvider)
	opts := ModelOptions{
		PreferredModels: []string{"claude-fable-*"},
	}

	res := resolveBoth(t, f, opts)
	assert.Equal(t, "claude-fable-5-2", res.Model)
	require.Equal(t, 1, lister.count())

	// A list younger than the refresh interval is not fetched again.
	lister.serve(active("claude-fable-5-2"), active("claude-fable-5-3"))
	clock.advance(6*time.Hour - time.Second)
	res = resolveBoth(t, f, opts)
	assert.Equal(t, "claude-fable-5-2", res.Model)
	require.Equal(t, 1, lister.count())

	// An older one is refreshed in the background: the resolution that
	// notices does not wait, and uses the list it has.
	release := lister.hold()
	clock.advance(2 * time.Second)
	res = resolveBoth(t, f, opts)
	assert.Equal(t, "claude-fable-5-2", res.Model)
	release()
	require.EqualValues(t, 2, settled(t, catalog))
	assert.Equal(t, 2, lister.count())

	res = resolveBoth(t, f, opts)
	assert.Equal(t, "claude-fable-5-3", res.Model)
}

func TestCatalog_StaleListServedWhileRefreshFails(t *testing.T) {
	stubNewLLM(t)
	lister := &fakeLister{}
	lister.serve(active("claude-fable-5-2"))
	stubModelLister(t, map[string]*fakeLister{
		matrixProvider: lister,
	})
	f := New(matrixConfig(true))
	clock := useClock(f)
	catalog := catalogOf(t, f, matrixProvider)
	opts := ModelOptions{
		PreferredModels: []string{"claude-fable-5-1"},
	}

	res := resolveBoth(t, f, opts)
	require.Equal(t, "claude-fable-5-2", res.Model)

	tests := []struct {
		name string
		fail func()
	}{
		{
			name: "the provider returns an error",
			fail: func() { lister.fail(errors.New("provider is down")) },
		},
		{
			name: "the provider returns an empty list",
			fail: func() { lister.serve() },
		},
	}
	wantCalls := 1
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.fail()
			clock.advance(defaultDiscoveryRefresh + time.Second)
			wantCalls++

			// The refresh fails; the list of the first fetch stays in use.
			for range 3 {
				res := resolveBoth(t, f, opts)
				assert.Equal(t, "claude-fable-5-2", res.Model)
				assert.True(t, res.Verified)
				assert.EqualValues(t, 1, settled(t, catalog))
			}
			assert.Equal(t, wantCalls, lister.count(), "a failed refresh is not repeated at once")
		})
	}
}

// An empty list on the first fetch is a failure too: it must not make every
// configured model look retired.
func TestCatalog_EmptyFirstListIsAFailure(t *testing.T) {
	stubNewLLM(t)
	lister := &fakeLister{}
	stubModelLister(t, map[string]*fakeLister{
		matrixProvider: lister,
	})
	f := New(matrixConfig(true))

	res := resolveBoth(t, f, ModelOptions{
		PreferredModels: []string{"claude-fable-5-1"},
	})
	assert.Equal(t, "claude-fable-5-1", res.Model)
	assert.Equal(t, ReasonExact, res.Reason)
	assert.False(t, res.Verified)

	err := RefreshModels(context.Background(), f)
	require.EqualError(t, err, "provider ANTHROPIC: model list is empty")
}

func TestCatalog_Refresh(t *testing.T) {
	stubNewLLM(t)
	lister := &fakeLister{}
	lister.serve(active("claude-fable-5-1"))
	stubModelLister(t, map[string]*fakeLister{
		matrixProvider: lister,
	})
	f := New(matrixConfig(true))
	ctx := context.Background()
	opts := ModelOptions{
		PreferredModels: []string{"claude-fable-*"},
	}

	// Refresh warms the catalog: only the provider with discovery is asked.
	require.NoError(t, RefreshModels(ctx, f))
	assert.Equal(t, 1, lister.count())
	res := resolveBoth(t, f, opts)
	assert.Equal(t, "claude-fable-5-1", res.Model)
	assert.Equal(t, 1, lister.count())

	// It reads a fresh list on demand, without waiting for the interval.
	lister.serve(active("claude-fable-5-1"), active("claude-fable-5-2"))
	require.NoError(t, RefreshModels(ctx, f))
	res = resolveBoth(t, f, opts)
	assert.Equal(t, "claude-fable-5-2", res.Model)

	// A failed refresh is reported and changes nothing.
	lister.fail(errors.New("provider is down"))
	err := RefreshModels(ctx, f)
	require.EqualError(t, err, "provider ANTHROPIC: list models: provider is down")
	res = resolveBoth(t, f, opts)
	assert.Equal(t, "claude-fable-5-2", res.Model)
	assert.True(t, res.Verified)

	// A factory without discovery has nothing to refresh.
	plain := New(matrixConfig(false))
	require.NoError(t, RefreshModels(ctx, plain))
}

// Every fetch keeps its own outcome, so a caller of Refresh is told about the
// fetch it waited for even when a later one has finished since.
func TestCatalog_RefreshReportsItsOwnFetch(t *testing.T) {
	stubNewLLM(t)
	lister := &fakeLister{}
	stubModelLister(t, map[string]*fakeLister{
		matrixProvider: lister,
	})
	f := New(matrixConfig(true))
	catalog := catalogOf(t, f, matrixProvider)
	ctx := context.Background()

	start := func() *modelFetch {
		catalog.mu.Lock()
		defer catalog.mu.Unlock()
		return catalog.startLocked(ctx, true)
	}

	lister.fail(errors.New("provider is down"))
	failed := start()
	<-failed.done

	lister.serve(active("claude-fable-5-2"))
	succeeded := start()
	<-succeeded.done

	// The later success does not rewrite the outcome of the earlier fetch.
	require.EqualError(t, failed.err, "provider ANTHROPIC: list models: provider is down")
	require.NoError(t, succeeded.err)

	lister.fail(errors.New("provider is down again"))
	again := start()
	<-again.done
	require.EqualError(t, again.err, "provider ANTHROPIC: list models: provider is down again")
	require.NoError(t, succeeded.err)

	// Callers that ask while a fetch is in flight join it and share its
	// outcome.
	release := lister.hold()
	first, second := start(), start()
	assert.Same(t, first, second)
	release()
	<-first.done
	require.EqualError(t, first.err, "provider ANTHROPIC: list models: provider is down again")
}

// Refreshes from many callers at once, while the provider alternates between
// failing and answering. Run with -race.
func TestCatalog_ConcurrentRefresh(t *testing.T) {
	stubNewLLM(t)
	lister := &fakeLister{}
	lister.serve(active("claude-fable-5-2"))
	stubModelLister(t, map[string]*fakeLister{
		matrixProvider: lister,
	})
	f := New(matrixConfig(true))
	ctx := context.Background()
	const (
		workers = 8
		rounds  = 25
		failure = "provider ANTHROPIC: list models: provider is down"
	)

	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range rounds {
				// A refresh reports success or the failure of the provider,
				// never anything else.
				if err := RefreshModels(ctx, f); err != nil {
					assert.EqualError(t, err, failure)
				}
			}
		})
	}
	for round := range rounds {
		if round%2 == 0 {
			lister.fail(errors.New("provider is down"))
		} else {
			lister.serve(active("claude-fable-5-2"))
		}
		_, err := f.GetModel(ctx, ModelOptions{
			PreferredModels: []string{"claude-fable-*"},
		})
		require.NoError(t, err)
	}
	wg.Wait()

	// The list of the last good fetch is in use.
	lister.serve(active("claude-fable-5-2"))
	require.NoError(t, RefreshModels(ctx, f))
	res := resolveBoth(t, f, ModelOptions{
		PreferredModels: []string{"claude-fable-*"},
	})
	assert.Equal(t, "claude-fable-5-2", res.Model)
	assert.True(t, res.Verified)
}

// Factories derived with WithModelFilter read one list per provider.
func TestCatalog_SharedByDerivedFactories(t *testing.T) {
	stubNewLLM(t)
	lister := &fakeLister{}
	lister.serve(active("claude-fable-5-1"), active("claude-fable-5-2"))
	built := stubModelLister(t, map[string]*fakeLister{
		matrixProvider: lister,
	})
	base := New(matrixConfig(true))
	tenant := base.WithModelFilter(func(_ context.Context, _, _, model string) bool {
		return model != "claude-fable-5-2"
	})
	opts := ModelOptions{
		PreferredModels: []string{"claude-fable-*"},
	}

	res := resolveBoth(t, base, opts)
	assert.Equal(t, "claude-fable-5-2", res.Model)
	res = resolveBoth(t, tenant, opts)
	assert.Equal(t, "claude-fable-5-1", res.Model)

	assert.Equal(t, 1, lister.count())
	assert.Equal(t, 1, *built)
}

// Many resolutions at once share one fetch, and stay correct while the list
// is replaced under them. Run with -race.
func TestCatalog_ConcurrentResolutions(t *testing.T) {
	stubNewLLM(t)
	lister := &fakeLister{}
	lister.serve(active("claude-fable-5-1"))
	stubModelLister(t, map[string]*fakeLister{
		matrixProvider: lister,
	})
	f := New(matrixConfig(true))
	clock := useClock(f)
	catalog := catalogOf(t, f, matrixProvider)
	ctx := context.Background()
	opts := ModelOptions{
		PreferredModels: []string{"claude-fable-*"},
	}
	const workers = 16

	// The first fetch: everybody waits for the one in flight.
	release := lister.hold()
	var wg sync.WaitGroup
	names := make([]string, workers)
	for i := range workers {
		wg.Go(func() {
			model, err := f.GetModel(ctx, opts)
			if assert.NoError(t, err) {
				names[i] = model.GetName()
			}
		})
	}
	release()
	wg.Wait()
	for _, name := range names {
		assert.Equal(t, "claude-fable-5-1", name)
	}
	assert.Equal(t, 1, lister.count())

	// Refreshes while resolutions run: every answer is one of the lists.
	stop := make(chan struct{})
	for range workers {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				model, err := f.GetModel(ctx, opts)
				if !assert.NoError(t, err) {
					return
				}
				assert.Contains(t, []string{"claude-fable-5-1", "claude-fable-5-2"}, model.GetName())
			}
		})
	}
	for round := range 20 {
		if round%2 == 0 {
			lister.serve(active("claude-fable-5-1"), active("claude-fable-5-2"))
		} else {
			lister.serve(active("claude-fable-5-1"))
		}
		clock.advance(defaultDiscoveryRefresh + time.Second)
		_, err := f.GetModel(ctx, opts)
		require.NoError(t, err)
		settled(t, catalog)
	}
	close(stop)
	wg.Wait()
	assert.Greater(t, settled(t, catalog), uint64(1))
}

// A resolution whose context ends while the first list is read does not wait
// any longer, and resolves as without a list.
func TestCatalog_CallerDoesNotWaitPastItsContext(t *testing.T) {
	stubNewLLM(t)
	lister := &fakeLister{}
	lister.serve(active("claude-fable-5-2"))
	stubModelLister(t, map[string]*fakeLister{
		matrixProvider: lister,
	})
	f := New(matrixConfig(true))
	catalog := catalogOf(t, f, matrixProvider)
	release := lister.hold()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := Resolve(ctx, f, ModelOptions{
		PreferredModels: []string{"claude-fable-5-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, "claude-fable-5-1", res.Model)
	assert.False(t, res.Verified)

	// The fetch it started goes on, and serves the next resolution.
	release()
	require.EqualValues(t, 1, settled(t, catalog))
	res = resolveBoth(t, f, ModelOptions{
		PreferredModels: []string{"claude-fable-5-1"},
	})
	assert.Equal(t, "claude-fable-5-2", res.Model)
}

// Exact resolution never substitutes: with a list, a model the provider no
// longer serves is not found.
func TestCatalog_ExactResolution(t *testing.T) {
	stubNewLLM(t)
	lister := &fakeLister{}
	lister.serve(
		active("claude-fable-5-2"),
		retired("claude-fable-5-1"),
		active("claude-opus-4-8"),
	)
	stubModelLister(t, map[string]*fakeLister{
		matrixProvider: lister,
	})
	f := New(matrixConfig(true))
	ctx := context.Background()

	model, err := ResolveExact(ctx, f, matrixProvider, "claude-fable-5-2", "")
	require.NoError(t, err)
	assert.Equal(t, "claude-fable-5-2", model.GetName())

	for _, gone := range []string{"claude-fable-5-1", "claude-fable-5-0"} {
		_, err = ResolveExact(ctx, f, matrixProvider, gone, "")
		require.Error(t, err, "model %s", gone)
		assert.True(t, errors.Is(err, ErrModelNotFound), "model %s: %v", gone, err)
	}

	// Without a list the model is admitted as before.
	lister.fail(errors.New("provider is down"))
	offline := New(matrixConfig(true))
	model, err = ResolveExact(ctx, offline, matrixProvider, "claude-fable-5-1", "")
	require.NoError(t, err)
	assert.Equal(t, "claude-fable-5-1", model.GetName())
}

// A client of exact resolution is built for the effort settings of the model
// as the list reports them now, not as they were when the model was first
// resolved.
func TestCatalog_ExactResolutionFollowsReportedEffort(t *testing.T) {
	constructed := stubNewLLM(t)
	lowToMax := llms.NewEffortSet(
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
		llms.ReasoningEffortXHigh,
		llms.ReasoningEffortMax,
	)
	const model = "claude-fable-5-2"
	lister := &fakeLister{}
	lister.fail(errors.New("provider is down"))
	stubModelLister(t, map[string]*fakeLister{
		matrixProvider: lister,
	})
	f := New(matrixConfig(true))
	clock := useClock(f)
	ctx := context.Background()

	// The list cannot be read: the client is built without reported levels.
	first, err := ResolveExact(ctx, f, matrixProvider, model, "")
	require.NoError(t, err)
	require.Len(t, *constructed, 1)
	assert.True(t, (*constructed)[0].effort.Supported.Empty())

	// The list arrives. The client built before does not know the levels of
	// the model, so another one is built.
	reported := active(model)
	reported.Efforts = lowToMax
	lister.serve(reported)
	clock.advance(modelListRetryInterval + time.Second)
	second, err := ResolveExact(ctx, f, matrixProvider, model, "")
	require.NoError(t, err)
	require.Len(t, *constructed, 2)
	assert.Equal(t, lowToMax, (*constructed)[1].effort.Supported)
	assert.NotSame(t, first, second)

	// While the list says the same, the client is shared.
	third, err := ResolveExact(ctx, f, matrixProvider, model, "")
	require.NoError(t, err)
	assert.Same(t, second, third)
	assert.Len(t, *constructed, 2)

	// The provider changes the levels: the next refresh builds a new client.
	reported.Efforts = lowToMax.With(llms.ReasoningEffortNone)
	lister.serve(reported)
	clock.advance(defaultDiscoveryRefresh + time.Second)
	_, err = ResolveExact(ctx, f, matrixProvider, model, "")
	require.NoError(t, err)
	require.EqualValues(t, 2, settled(t, catalogOf(t, f, matrixProvider)))
	fourth, err := ResolveExact(ctx, f, matrixProvider, model, "")
	require.NoError(t, err)
	assert.NotSame(t, third, fourth)
	require.Len(t, *constructed, 3)
	assert.Equal(t, reported.Efforts, (*constructed)[2].effort.Supported)
}

func TestModelList_Lookup(t *testing.T) {
	list := newModelList(1, time.Now(), []llms.ModelInfo{
		active("claude-haiku-4-5-20251001"),
		retired("claude-opus-4-20250514"),
		active("claude-opus-4-1-20250805"),
		deprecated("claude-3-7-sonnet-20250219"),
		active("gpt-4o-2024-05-13"),
		active("gpt-4o-2024-08-06"),
		active("openai/gpt-5.5:batch"),
		active("gemini-3-pro-preview"),
	})
	tests := []struct {
		id         string
		wantID     string
		wantServed bool
	}{
		// An entry with the ID itself.
		{id: "claude-haiku-4-5-20251001", wantID: "claude-haiku-4-5-20251001", wantServed: true},
		{id: "claude-opus-4-20250514", wantID: "claude-opus-4-20250514"},
		// A floating ID is described by its snapshot.
		{id: "claude-haiku-4-5", wantID: "claude-haiku-4-5-20251001", wantServed: true},
		{id: "claude-opus-4-1", wantID: "claude-opus-4-1-20250805", wantServed: true},
		{id: "claude-opus-4-0", wantID: "claude-opus-4-20250514"},
		{id: "claude-3-7-sonnet-latest", wantID: "claude-3-7-sonnet-20250219", wantServed: true},
		// The newest snapshot describes it.
		{id: "gpt-4o", wantID: "gpt-4o-2024-08-06", wantServed: true},
		// Another version, variant, route or stage is another model.
		{id: "claude-haiku-4-6"},
		{id: "claude-haiku-5"},
		{id: "openai/gpt-5.5"},
		{id: "gpt-5.5:batch"},
		{id: "gemini-3-pro"},
		// A dated ID is only described by itself.
		{id: "claude-haiku-4-5-20250101"},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			info, ok := list.lookup(tt.id)
			assert.Equal(t, tt.wantID != "", ok)
			assert.Equal(t, tt.wantID, info.ID)
			assert.Equal(t, tt.wantServed, list.serves(tt.id))
		})
	}

	var none *modelList
	_, ok := none.lookup("claude-haiku-4-5")
	assert.False(t, ok)
	assert.False(t, none.serves("claude-haiku-4-5"))
	assert.Empty(t, none.lifecycle("claude-haiku-4-5"))
}

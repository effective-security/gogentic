package router

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testModel struct {
	calls    atomic.Int32
	validate func(llms.InferenceRequest) error
	infer    func(context.Context, llms.InferenceRequest) (*llms.InferenceResponse, error)
}

func (m *testModel) ValidateInference(r llms.InferenceRequest) error {
	if m.validate != nil {
		return m.validate(r)
	}
	return nil
}

func (m *testModel) Infer(ctx context.Context, r llms.InferenceRequest) (*llms.InferenceResponse, error) {
	m.calls.Add(1)
	if m.infer != nil {
		return m.infer(ctx, r)
	}
	return textResponse("hello"), nil
}

func textResponse(text string) *llms.InferenceResponse {
	return &llms.InferenceResponse{
		Content: []llms.InferenceBlock{{
			Type: llms.BlockText,
			Text: text,
		}},
		FinishReason: llms.FinishStop,
	}
}

func request() Request {
	return Request{
		Model: "a",
		Input: llms.InferenceRequest{
			Messages: []llms.InferenceMessage{{
				Role: llms.InferenceRoleUser,
				Content: []llms.InferenceBlock{{
					Type: llms.BlockText,
					Text: "hello",
				}},
			}},
		},
	}
}

func target(id string, m *testModel) Target {
	return Target{
		ID:           id,
		BackendModel: "backend-" + id,
		Connector:    m,
		Features: Features{
			Tools:           true,
			JSONSchema:      true,
			StrictSchema:    true,
			StrictTools:     true,
			SchemaWithTools: true,
			Reasoning:       true,
		},
		Tags: map[string]string{
			"tier": id,
		},
	}
}

func kindOf(t *testing.T, err error) Kind {
	t.Helper()
	var re *Error
	require.ErrorAs(t, err, &re)
	return re.Kind
}

func TestNewRejectsInvalidCatalog(t *testing.T) {
	m := &testModel{}
	_, err := New(Config{})
	require.ErrorContains(t, err, "at least one target")
	_, err = New(Config{Targets: []Target{{ID: "x", Connector: m}}})
	require.ErrorContains(t, err, `target "x" requires a backend model`)
	_, err = New(Config{Targets: []Target{{ID: "x", BackendModel: "b"}}})
	require.ErrorContains(t, err, "requires a connector")
	_, err = New(Config{Targets: []Target{target("a", m), {ID: "b", BackendModel: "b", Connector: m, Aliases: []string{"a"}}}})
	require.ErrorContains(t, err, `duplicate model or alias "a"`)
	_, err = New(Config{Targets: []Target{target("a", m)}, Fallback: "missing"})
	require.ErrorContains(t, err, `fallback "missing"`)
	_, err = New(Config{Targets: []Target{target(FallbackRequested, m)}})
	require.ErrorContains(t, err, "reserved")
	_, err = New(Config{Targets: []Target{target("a", m)}, MinimumConfidence: 2})
	require.ErrorContains(t, err, "minimum confidence")
	var nilSelector *fakeSelector
	_, err = New(Config{Targets: []Target{target("a", m)}, Selector: nilSelector})
	require.ErrorContains(t, err, "selector is a nil implementation")
}

type fakeSelector struct{}

func (*fakeSelector) Select(context.Context, SelectionInput) (Decision, error) {
	return Decision{}, nil
}

func TestConnectorRequiresInferenceModel(t *testing.T) {
	_, err := Connector(legacyModel{})
	require.ErrorContains(t, err, `model "legacy" does not implement`)
	c, err := Connector(inferenceModel{})
	require.NoError(t, err)
	assert.NotNil(t, c)
}

type legacyModel struct{}

func (legacyModel) GetName() string                    { return "legacy" }
func (legacyModel) GetProviderType() llms.ProviderType { return llms.ProviderOpenAI }
func (legacyModel) GenerateContent(context.Context, []llms.Message, ...llms.CallOption) (*llms.ContentResponse, error) {
	return nil, nil
}

type inferenceModel struct{ legacyModel }

func (inferenceModel) ValidateInference(llms.InferenceRequest) error { return nil }
func (inferenceModel) Infer(context.Context, llms.InferenceRequest) (*llms.InferenceResponse, error) {
	return nil, nil
}

func TestExactAndSemanticRouting(t *testing.T) {
	a, b := &testModel{}, &testModel{}
	r, err := New(Config{
		Targets: []Target{target("a", a), target("b", b)},
	})
	require.NoError(t, err)
	res, err := r.Generate(context.Background(), request())
	require.NoError(t, err)
	assert.Equal(t, "a", res.Model)
	assert.Equal(t, "backend-a", res.BackendModel)
	req := request()
	req.Model = "missing"
	_, err = r.Generate(context.Background(), req)
	assert.Equal(t, KindModelNotFound, kindOf(t, err))
	assert.EqualValues(t, 1, a.calls.Load())

	r, err = New(Config{
		Targets: []Target{target("a", a), target("b", b)},
		Selector: SelectorFunc(func(_ context.Context, in SelectionInput) (Decision, error) {
			require.Len(t, in.Candidates, 2)
			assert.Equal(t, "b", in.Candidates[1].Tags["tier"])
			in.Request.Input.Messages[0].Content[0].Text = "mutated"
			return Decision{
				TargetID:       "b",
				Classification: "simple",
			}, nil
		}),
	})
	require.NoError(t, err)
	b.infer = func(_ context.Context, in llms.InferenceRequest) (*llms.InferenceResponse, error) {
		assert.Equal(t, "hello", in.Messages[0].Content[0].Text)
		assert.Equal(t, "backend-b", in.Model)
		return textResponse("ok"), nil
	}
	res, err = r.Generate(context.Background(), request())
	require.NoError(t, err)
	assert.Equal(t, "b", res.Model)
	assert.Equal(t, "simple", res.Classification)
}

func TestRequestedTargetRejectionIsReported(t *testing.T) {
	m := &testModel{
		validate: func(llms.InferenceRequest) error { return llms.UnsupportedInference("top_p") },
	}
	r, err := New(Config{Targets: []Target{target("a", m)}})
	require.NoError(t, err)
	_, err = r.Generate(context.Background(), request())
	re := AsError(err)
	assert.Equal(t, KindUnsupported, re.Kind)
	assert.Equal(t, "top_p", re.Param)
	assert.Zero(t, m.calls.Load())
}

func TestSelectorFailureAndAuthorization(t *testing.T) {
	for _, fallback := range []string{"", FallbackRequested, "a"} {
		t.Run(fallback, func(t *testing.T) {
			m := &testModel{}
			denied := target("b", m)
			denied.Allow = func(context.Context) bool { return false }
			r, err := New(Config{
				Targets:  []Target{target("a", m), denied},
				Fallback: fallback,
				Selector: SelectorFunc(func(_ context.Context, in SelectionInput) (Decision, error) {
					require.Len(t, in.Candidates, 1)
					return Decision{TargetID: "b"}, nil
				}),
			})
			require.NoError(t, err)
			_, err = r.Generate(context.Background(), request())
			if fallback == "" {
				assert.Equal(t, KindSelectionUnavailable, kindOf(t, err))
				assert.Zero(t, m.calls.Load())
			} else {
				require.NoError(t, err)
				assert.EqualValues(t, 1, m.calls.Load())
			}
		})
	}
}

func TestMinimumConfidenceAndSelectorTimeoutFallback(t *testing.T) {
	m := &testModel{}
	low := 0.2
	r, err := New(Config{
		Targets:           []Target{target("a", m)},
		MinimumConfidence: 0.5,
		Selector: SelectorFunc(func(context.Context, SelectionInput) (Decision, error) {
			return Decision{TargetID: "a", Confidence: &low}, nil
		}),
	})
	require.NoError(t, err)
	_, err = r.Generate(context.Background(), request())
	assert.Equal(t, KindSelectionUnavailable, kindOf(t, err))
	assert.ErrorContains(t, errors.Cause(AsError(err).Cause), "below configured minimum")

	r, err = New(Config{
		Targets:         []Target{target("a", m)},
		SelectorTimeout: time.Millisecond,
		Fallback:        FallbackRequested,
		Selector: SelectorFunc(func(ctx context.Context, _ SelectionInput) (Decision, error) {
			<-ctx.Done()
			return Decision{}, ctx.Err()
		}),
	})
	require.NoError(t, err)
	res, err := r.Generate(context.Background(), request())
	require.NoError(t, err)
	assert.Equal(t, "a", res.Model)
}

func TestHistoryAndSchemaValidation(t *testing.T) {
	m := &testModel{}
	r, err := New(Config{Targets: []Target{target("a", m)}})
	require.NoError(t, err)
	req := request()
	req.Input.Messages = append(req.Input.Messages, llms.InferenceMessage{
		Role: llms.InferenceRoleAssistant,
		Content: []llms.InferenceBlock{{
			Type:      llms.BlockFunctionCall,
			ID:        "call1",
			Name:      "lookup",
			Arguments: `{}`,
		}},
	})
	_, err = r.Generate(context.Background(), req)
	assert.Equal(t, KindInvalidRequest, kindOf(t, err))
	assert.Zero(t, m.calls.Load())
	req.Input.Messages = append(req.Input.Messages, llms.InferenceMessage{
		Role: llms.InferenceRoleTool,
		Content: []llms.InferenceBlock{{
			Type: llms.BlockFunctionOutput,
			ID:   "call1",
			Text: "ok",
		}},
	})
	_, err = r.Generate(context.Background(), req)
	require.NoError(t, err)
	assert.Empty(t, req.Input.Messages[2].Content[0].Name, "caller history must remain immutable")
	req.Input.Messages = append(req.Input.Messages, req.Input.Messages[2])
	_, err = r.Generate(context.Background(), req)
	assert.Equal(t, KindInvalidRequest, kindOf(t, err))
	assert.EqualValues(t, 1, m.calls.Load())

	req = request()
	req.Input.Format = &llms.InferenceFormat{
		Type:   llms.FormatJSONSchema,
		Name:   "answer",
		Strict: true,
		Schema: []byte(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}`),
	}
	_, err = r.Generate(context.Background(), req)
	assert.Equal(t, KindUpstream, kindOf(t, err))
	m.infer = func(context.Context, llms.InferenceRequest) (*llms.InferenceResponse, error) {
		return textResponse(`{"ok":true}`), nil
	}
	_, err = r.Generate(context.Background(), req)
	require.NoError(t, err)
}

func TestReasoningSourceEligibility(t *testing.T) {
	a, b := &testModel{}, &testModel{}
	r, err := New(Config{
		Targets: []Target{target("a", a), target("b", b)},
		Selector: SelectorFunc(func(_ context.Context, in SelectionInput) (Decision, error) {
			require.Len(t, in.Candidates, 1, "foreign reasoning state must exclude target b")
			return Decision{TargetID: in.Candidates[0].ID}, nil
		}),
	})
	require.NoError(t, err)
	req := request()
	req.Input.Messages = append(req.Input.Messages,
		llms.InferenceMessage{
			Role: llms.InferenceRoleAssistant,
			Content: []llms.InferenceBlock{{
				Type:   llms.BlockReasoning,
				Opaque: json.RawMessage(`{"signature":"abc"}`),
				Source: "a",
			}, {
				Type:      llms.BlockFunctionCall,
				ID:        "call1",
				Name:      "lookup",
				Arguments: `{}`,
			}},
		},
		llms.InferenceMessage{
			Role: llms.InferenceRoleTool,
			Content: []llms.InferenceBlock{{
				Type: llms.BlockFunctionOutput,
				ID:   "call1",
				Text: "ok",
			}},
		},
	)
	a.infer = func(_ context.Context, in llms.InferenceRequest) (*llms.InferenceResponse, error) {
		assert.JSONEq(t, `{"signature":"abc"}`, string(in.Messages[1].Content[0].Opaque))
		return textResponse("ok"), nil
	}
	res, err := r.Generate(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "a", res.Model)
	assert.Zero(t, b.calls.Load())

	req.Input.Messages[1].Content[0].Source = ""
	_, err = r.Generate(context.Background(), req)
	re := AsError(err)
	assert.Equal(t, KindInvalidRequest, re.Kind)
	assert.Equal(t, paramReasoning, re.Param)
}

func TestAdmissionDeadlineAndObservations(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	m := &testModel{
		infer: func(ctx context.Context, _ llms.InferenceRequest) (*llms.InferenceResponse, error) {
			close(entered)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
				return nil, errors.New("provider secret")
			}
		},
	}
	var observation Observation
	var observedCtxErr error
	r, err := New(Config{
		Targets:   []Target{target("a", m)},
		Admission: ConcurrencyLimit(1),
		Timeout:   time.Minute,
		Observe: func(ctx context.Context, o Observation) {
			if o.TargetID != "" {
				observation = o
				observedCtxErr = ctx.Err()
			}
		},
	})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { _, err := r.Generate(context.Background(), request()); done <- err }()
	<-entered
	_, err = r.Generate(context.Background(), request())
	assert.Equal(t, KindRateLimited, kindOf(t, err))
	close(release)
	err = <-done
	assert.Equal(t, KindUpstream, kindOf(t, err))
	assert.NotContains(t, err.Error(), "secret")
	assert.NotNil(t, observation.Err)
	assert.Equal(t, "backend-a", observation.BackendModel)
	assert.NoError(t, observedCtxErr, "observer must receive the caller's live context")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = r.Generate(ctx, request())
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, KindCancelled, kindOf(t, err))
}

func TestCompletedInferenceSurvivesDeadline(t *testing.T) {
	m := &testModel{
		infer: func(ctx context.Context, _ llms.InferenceRequest) (*llms.InferenceResponse, error) {
			<-ctx.Done()
			return textResponse("late but complete"), nil
		},
	}
	r, err := New(Config{Targets: []Target{target("a", m)}, Timeout: 5 * time.Millisecond})
	require.NoError(t, err)
	res, err := r.Generate(context.Background(), request())
	require.NoError(t, err)
	assert.Equal(t, "late but complete", res.Response.Content[0].Text)
}

func TestCustomAdmissionErrors(t *testing.T) {
	m := &testModel{}
	r, err := New(Config{
		Targets: []Target{target("a", m)},
		Admission: admissionFunc(func(context.Context, AdmissionRequest) (func(), error) {
			return nil, NewError(KindInvalidRequest, "budget", "budget exhausted")
		}),
	})
	require.NoError(t, err)
	_, err = r.Generate(context.Background(), request())
	re := AsError(err)
	assert.Equal(t, KindInvalidRequest, re.Kind)
	assert.Equal(t, "budget", re.Param)
	assert.Zero(t, m.calls.Load())
}

type admissionFunc func(context.Context, AdmissionRequest) (func(), error)

func (f admissionFunc) Admit(ctx context.Context, r AdmissionRequest) (func(), error) {
	return f(ctx, r)
}

func TestConcurrentIsolation(t *testing.T) {
	m := &testModel{}
	r, err := New(Config{Targets: []Target{target("a", m)}})
	require.NoError(t, err)
	req := request()
	var wg sync.WaitGroup
	for range 40 {
		wg.Go(func() { _, err := r.Generate(context.Background(), req); assert.NoError(t, err) })
	}
	wg.Wait()
	assert.EqualValues(t, 40, m.calls.Load())
}

func BenchmarkRouting(b *testing.B) {
	for _, semantic := range []bool{false, true} {
		name := "exact"
		if semantic {
			name = "selector"
		}
		b.Run(name, func(b *testing.B) {
			cfg := Config{Targets: []Target{target("a", &testModel{})}}
			if semantic {
				cfg.Selector = SelectorFunc(func(context.Context, SelectionInput) (Decision, error) {
					return Decision{TargetID: "a"}, nil
				})
			}
			r, err := New(cfg)
			require.NoError(b, err)
			req := request()
			b.ReportAllocs()
			for b.Loop() {
				if _, err := r.Generate(context.Background(), req); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestNormalizeResponse(t *testing.T) {
	yes := true
	tool := llms.InferenceTool{
		Name:       "lookup",
		Strict:     &yes,
		Parameters: []byte(`{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"],"additionalProperties":false}`),
	}
	in := llms.InferenceRequest{
		Tools:      []llms.InferenceTool{tool},
		ToolChoice: &llms.InferenceToolChoice{Mode: llms.ToolChoiceRequired},
	}
	call := llms.InferenceBlock{
		Type:      llms.BlockFunctionCall,
		ID:        "a",
		Name:      "lookup",
		Arguments: `{"n":"wrong"}`,
	}
	out := &llms.InferenceResponse{
		FinishReason: llms.FinishToolCalls,
		Content:      []llms.InferenceBlock{call},
	}
	_, err := normalizeResponse(in, out)
	require.ErrorContains(t, err, "strict tool arguments")

	out.Content[0].Arguments = `{"n":1}`
	_, err = normalizeResponse(in, out)
	require.NoError(t, err)

	out.Content = append(out.Content, out.Content[0])
	_, err = normalizeResponse(in, out)
	require.ErrorContains(t, err, "invalid returned tool call")

	out.Content = out.Content[:1]
	out.Content[0].Arguments = `{"n":`
	out.FinishReason = llms.FinishLength
	_, err = normalizeResponse(in, out)
	require.NoError(t, err, "truncated arguments are reported, not validated")

	out.Content[0].Arguments = `{"n":1}`
	out.FinishReason = llms.FinishStop
	res, err := normalizeResponse(in, out)
	require.NoError(t, err)
	assert.Equal(t, llms.FinishToolCalls, res.FinishReason, "stop with calls is normalized")

	out.Content = []llms.InferenceBlock{{
		Type: llms.BlockRefusal,
		Text: "cannot comply",
	}}
	out.FinishReason = llms.FinishStop
	_, err = normalizeResponse(in, out)
	require.NoError(t, err)

	out.Content = nil
	out.FinishReason = llms.FinishLength
	_, err = normalizeResponse(llms.InferenceRequest{}, out)
	require.NoError(t, err, "empty truncated output is accepted")
	out.FinishReason = llms.FinishStop
	_, err = normalizeResponse(llms.InferenceRequest{}, out)
	require.ErrorContains(t, err, "empty inference output")

	out = textResponse("ok")
	out.UsageKnown = true
	out.Usage = llms.Usage{
		InputTokens:  10,
		OutputTokens: 2,
		TotalTokens:  99,
	}
	res, err = normalizeResponse(llms.InferenceRequest{}, out)
	require.NoError(t, err)
	assert.False(t, res.UsageKnown, "inconsistent usage degrades to unknown")
	assert.True(t, out.UsageKnown, "connector response is not mutated")
}

func TestUsageInconsistencyIsObserved(t *testing.T) {
	m := &testModel{
		infer: func(context.Context, llms.InferenceRequest) (*llms.InferenceResponse, error) {
			out := textResponse("ok")
			out.UsageKnown = true
			out.Usage = llms.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 5}
			return out, nil
		},
	}
	var observation Observation
	r, err := New(Config{
		Targets: []Target{target("a", m)},
		Observe: func(_ context.Context, o Observation) { observation = o },
	})
	require.NoError(t, err)
	res, err := r.Generate(context.Background(), request())
	require.NoError(t, err)
	assert.False(t, res.Response.UsageKnown)
	assert.True(t, observation.UsageInconsistent)
	assert.False(t, observation.UsageKnown)
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	m := &testModel{}
	longID := strings.Repeat("i", maxModelNameBytes+1)
	tooManyTags := map[string]string{}
	for i := range maxTags + 1 {
		tooManyTags[strings.Repeat("t", i+1)] = "v"
	}
	withTarget := func(mutate func(*Target)) Config {
		tgt := target("a", m)
		mutate(&tgt)
		return Config{Targets: []Target{tgt}}
	}
	var nilAdmission *concurrencyLimit
	cases := []struct {
		name string
		cfg  Config
		err  string
	}{
		{
			name: "nil admission implementation",
			cfg: Config{
				Targets:   []Target{target("a", m)},
				Admission: nilAdmission,
			},
			err: "router: admission is a nil implementation",
		},
		{
			name: "negative timeout",
			cfg: Config{
				Targets: []Target{target("a", m)},
				Timeout: -time.Second,
			},
			err: "router: timeouts must not be negative",
		},
		{
			name: "negative selector timeout",
			cfg: Config{
				Targets:         []Target{target("a", m)},
				SelectorTimeout: -time.Second,
			},
			err: "router: timeouts must not be negative",
		},
		{
			name: "NaN minimum confidence",
			cfg: Config{
				Targets:           []Target{target("a", m)},
				MinimumConfidence: math.NaN(),
			},
			err: "router: minimum confidence must be within [0,1]",
		},
		{
			name: "negative minimum confidence",
			cfg: Config{
				Targets:           []Target{target("a", m)},
				MinimumConfidence: -0.1,
			},
			err: "router: minimum confidence must be within [0,1]",
		},
		{
			name: "empty target ID",
			cfg:  withTarget(func(t *Target) { t.ID = "" }),
			err:  "router: target ID is required",
		},
		{
			name: "oversized target ID",
			cfg:  withTarget(func(t *Target) { t.ID = longID }),
			err:  `router: target ID "` + longID + `" exceeds 256 bytes`,
		},
		{
			name: "oversized backend model",
			cfg:  withTarget(func(t *Target) { t.BackendModel = strings.Repeat("b", maxBackendNameBytes+1) }),
			err:  `router: target "a" backend model exceeds 1024 bytes`,
		},
		{
			name: "typed nil connector",
			cfg:  withTarget(func(t *Target) { t.Connector = (*testModel)(nil) }),
			err:  `router: target "a" requires a connector`,
		},
		{
			name: "negative output limit",
			cfg:  withTarget(func(t *Target) { t.MaxOutputTokens = -1 }),
			err:  `router: target "a" output limit must be within [0,1048576]`,
		},
		{
			name: "output limit too high",
			cfg:  withTarget(func(t *Target) { t.MaxOutputTokens = maxOutputTokens + 1 }),
			err:  `router: target "a" output limit must be within [0,1048576]`,
		},
		{
			name: "too many tags",
			cfg:  withTarget(func(t *Target) { t.Tags = tooManyTags }),
			err:  `router: target "a" has more than 32 tags`,
		},
		{
			name: "empty tag key",
			cfg:  withTarget(func(t *Target) { t.Tags = map[string]string{"": "v"} }),
			err:  `router: target "a" has an invalid tag ""`,
		},
		{
			name: "oversized tag value",
			cfg:  withTarget(func(t *Target) { t.Tags = map[string]string{"tier": strings.Repeat("v", maxTagValueBytes+1)} }),
			err:  `router: target "a" has an invalid tag "tier"`,
		},
		{
			name: "empty alias",
			cfg:  withTarget(func(t *Target) { t.Aliases = []string{""} }),
			err:  `router: target "a" has an empty or oversized alias`,
		},
		{
			name: "oversized alias",
			cfg:  withTarget(func(t *Target) { t.Aliases = []string{longID} }),
			err:  `router: target "a" has an empty or oversized alias`,
		},
		{
			name: "fallback names an alias",
			cfg: Config{
				Targets: []Target{{
					ID:           "a",
					Aliases:      []string{"alias-a"},
					BackendModel: "backend-a",
					Connector:    m,
				}},
				Fallback: "alias-a",
			},
			err: `router: fallback "alias-a" is not a registered target ID`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := New(tc.cfg)
			require.EqualError(t, err, tc.err)
			assert.Nil(t, r)
		})
	}
}

func TestNewIsolatesCatalog(t *testing.T) {
	m := &testModel{}
	tgt := target("a", m)
	tgt.Aliases = []string{"alias-a"}
	cfg := Config{Targets: []Target{tgt}}
	r, err := New(cfg)
	require.NoError(t, err)
	tgt.Aliases[0] = "changed"
	tgt.Tags["tier"] = "changed"

	req := request()
	req.Model = "alias-a"
	res, err := r.Generate(context.Background(), req)
	require.NoError(t, err, "aliases are copied at construction")
	assert.Equal(t, "a", res.Model)
	assert.Equal(t, defaultSelectorTimeout, r.cfg.SelectorTimeout)
	assert.Nil(t, r.cfg.Targets, "the router keeps its own target copy")
	assert.Equal(t, "a", r.targets[0].Tags["tier"])
}

func TestRequestClone(t *testing.T) {
	req := request()
	req.Metadata = map[string]string{
		"k": "v",
	}
	clone := req.Clone()
	assert.Equal(t, req, clone)
	clone.Metadata["k"] = "changed"
	clone.Input.Messages[0].Content[0].Text = "changed"
	assert.Equal(t, "v", req.Metadata["k"])
	assert.Equal(t, "hello", req.Input.Messages[0].Content[0].Text)

	req.Metadata = nil
	assert.Nil(t, req.Clone().Metadata, "absent metadata stays absent")
}

func TestConnectorRejectsNilModels(t *testing.T) {
	const want = `router: model "" does not implement llms.InferenceModel`
	c, err := Connector(nil)
	require.EqualError(t, err, want)
	assert.Nil(t, c)

	var typedNil *inferenceModel
	c, err = Connector(typedNil)
	require.EqualError(t, err, want)
	assert.Nil(t, c)
}

func TestSelectorWithoutEligibleTargets(t *testing.T) {
	m := &testModel{}
	denied := target("a", m)
	denied.Allow = func(context.Context) bool { return false }
	var selected atomic.Int32
	r, err := New(Config{
		Targets: []Target{denied},
		Selector: SelectorFunc(func(context.Context, SelectionInput) (Decision, error) {
			selected.Add(1)
			return Decision{TargetID: "a"}, nil
		}),
	})
	require.NoError(t, err)
	_, err = r.Generate(context.Background(), request())
	requireRouterError(t, err, KindSelectionUnavailable, "", "no eligible model")
	assert.Zero(t, selected.Load(), "selector is not consulted without candidates")
	assert.Zero(t, m.calls.Load())
}

func TestInvalidSelectorDecisions(t *testing.T) {
	tooHigh := 1.5
	negative := -0.5
	nan := math.NaN()
	cases := []struct {
		name     string
		decision Decision
		cause    string
	}{
		{
			name: "classification too long",
			decision: Decision{
				TargetID:       "a",
				Classification: strings.Repeat("c", maxClassificationBytes+1),
			},
			cause: "classification exceeds 128 bytes",
		},
		{
			name: "confidence above one",
			decision: Decision{
				TargetID:   "a",
				Confidence: &tooHigh,
			},
			cause: "confidence must be within [0,1]",
		},
		{
			name: "negative confidence",
			decision: Decision{
				TargetID:   "a",
				Confidence: &negative,
			},
			cause: "confidence must be within [0,1]",
		},
		{
			name: "NaN confidence",
			decision: Decision{
				TargetID:   "a",
				Confidence: &nan,
			},
			cause: "confidence must be within [0,1]",
		},
		{
			name: "alias instead of target ID",
			decision: Decision{
				TargetID: "alias-a",
			},
			cause: `selected target "alias-a" is not an eligible target ID`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &testModel{}
			tgt := target("a", m)
			tgt.Aliases = []string{"alias-a"}
			r, err := New(Config{
				Targets: []Target{tgt},
				Selector: SelectorFunc(func(context.Context, SelectionInput) (Decision, error) {
					return tc.decision, nil
				}),
			})
			require.NoError(t, err)
			_, err = r.Generate(context.Background(), request())
			re := requireRouterError(t, err, KindSelectionUnavailable, "", "model selection failed")
			require.EqualError(t, re.Cause, tc.cause)
			assert.Zero(t, m.calls.Load())
		})
	}
}

func TestCancellationDuringSelectionSkipsInference(t *testing.T) {
	m := &testModel{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := New(Config{
		Targets:  []Target{target("a", m)},
		Fallback: "a",
		Selector: SelectorFunc(func(context.Context, SelectionInput) (Decision, error) {
			cancel()
			return Decision{TargetID: "a"}, nil
		}),
	})
	require.NoError(t, err)
	_, err = r.Generate(ctx, request())
	requireRouterError(t, err, KindCancelled, "", msgCancelled)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, m.calls.Load(), "a cancelled request never reaches the connector")
}

func TestInferenceDeadlineIsClassified(t *testing.T) {
	m := &testModel{
		infer: func(ctx context.Context, _ llms.InferenceRequest) (*llms.InferenceResponse, error) {
			<-ctx.Done()
			return nil, errors.Wrap(ctx.Err(), "provider transport")
		},
	}
	r, err := New(Config{
		Targets: []Target{target("a", m)},
		Timeout: 5 * time.Millisecond,
	})
	require.NoError(t, err)
	_, err = r.Generate(context.Background(), request())
	requireRouterError(t, err, KindTimeout, "", msgDeadline)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestTargetOutputLimitIsDefaulted(t *testing.T) {
	const limit = 64
	var got []int
	m := &testModel{
		infer: func(_ context.Context, in llms.InferenceRequest) (*llms.InferenceResponse, error) {
			require.NotNil(t, in.MaxTokens)
			got = append(got, *in.MaxTokens)
			return textResponse("ok"), nil
		},
	}
	tgt := target("a", m)
	tgt.MaxOutputTokens = limit
	r, err := New(Config{Targets: []Target{tgt}})
	require.NoError(t, err)
	req := request()
	_, err = r.Generate(context.Background(), req)
	require.NoError(t, err)
	assert.Nil(t, req.Input.MaxTokens, "caller request is not mutated")

	explicit := 10
	req.Input.MaxTokens = &explicit
	_, err = r.Generate(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, []int{limit, explicit}, got)
}

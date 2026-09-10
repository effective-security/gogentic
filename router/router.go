package router

import (
	"context"
	"math"
	"reflect"
	"slices"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/google/uuid"
)

// Request contains a public model/alias and the portable inference input.
// Input.Model is ignored; only the registered target selects the backend model.
type Request struct {
	Model    string
	Input    llms.InferenceRequest
	Metadata map[string]string
}

// Clone returns a deep copy of the request.
func (r Request) Clone() Request {
	out := Request{
		Model: r.Model,
		Input: r.Input.Clone(),
	}
	if r.Metadata != nil {
		out.Metadata = make(map[string]string, len(r.Metadata))
		for k, v := range r.Metadata {
			out.Metadata[k] = v
		}
	}
	return out
}

// Target describes one permitted deployment and its connector. Features are
// operator claims about that deployment, further restricted by connector validation.
type Target struct {
	ID           string
	Aliases      []string
	BackendModel string
	Connector    llms.InferenceModel
	Features     Features
	// Tags is bounded operator metadata (tier, cost class, region, ...) exposed
	// to selectors. It is never sent to providers.
	Tags            map[string]string
	MaxOutputTokens int
	// Allow checks trusted context on every request. Nil allows all callers.
	Allow func(context.Context) bool
}

// Features describes model-specific combinations an operator has certified.
type Features struct {
	Tools           bool
	JSON            bool
	JSONSchema      bool
	StrictSchema    bool
	StrictTools     bool
	SchemaWithTools bool
	DeveloperRole   bool
	Reasoning       bool
	Seed            bool
}

// Candidate is the credential-free target descriptor exposed to selectors.
type Candidate struct {
	ID       string
	Features Features
	Tags     map[string]string
}

// SelectionInput is a private request copy with only authorized, compatible candidates.
type SelectionInput struct {
	Request    Request
	Candidates []Candidate
}

// Decision selects a registered candidate and records optional classification usage.
type Decision struct {
	TargetID       string
	Classification string
	Confidence     *float64
	Usage          llms.Usage
	UsageKnown     bool
}

// Selector chooses one candidate. Implementations must honor context cancellation.
//
//go:generate mockgen -source=router.go -destination=../mocks/mockrouter/router_mock.gen.go -package mockrouter
type Selector interface {
	// Select classifies the request and returns one eligible target ID.
	Select(context.Context, SelectionInput) (Decision, error)
}

// SelectorFunc adapts a callback to Selector.
type SelectorFunc func(context.Context, SelectionInput) (Decision, error)

// Select invokes the callback.
func (f SelectorFunc) Select(ctx context.Context, in SelectionInput) (Decision, error) {
	return f(ctx, in)
}

// AdmissionRequest is what an Admission implementation may inspect. Trusted
// identity is expected in the context, not in the request.
type AdmissionRequest struct {
	Model    string
	Metadata map[string]string
}

// Admission decides whether a request may proceed to selection and inference.
// Implementations may return a *Error to control the reported kind; any other
// error is reported as KindRateLimited. The release function is called exactly
// once when the request finishes, whether or not inference happened.
type Admission interface {
	// Admit blocks or rejects until the request may proceed.
	Admit(context.Context, AdmissionRequest) (release func(), err error)
}

// Observation reports one routing attempt without request content or credentials.
// Err retains internal diagnostics; do not serialize it into a client response.
type Observation struct {
	RequestID          string
	TargetID           string
	BackendModel       string
	Classification     string
	Duration           time.Duration
	SelectorUsage      llms.Usage
	SelectorUsageKnown bool
	Usage              llms.Usage
	UsageKnown         bool
	// UsageInconsistent is set when the provider's counters did not add up and
	// Usage was therefore reported as unknown to the caller.
	UsageInconsistent bool
	Err               error
}

// Config supplies immutable routing policy and finite operational limits.
type Config struct {
	Targets  []Target
	Selector Selector
	// SelectorTimeout bounds one selector call; zero selects 5s.
	SelectorTimeout time.Duration
	// Timeout bounds the whole request; zero imposes no router deadline.
	Timeout time.Duration
	// Admission is optional host-supplied admission control; nil admits everything.
	Admission Admission
	// Fallback is an explicit target ID, or FallbackRequested for the incoming model.
	// It applies only to selector failures; inference is never retried.
	Fallback          string
	MinimumConfidence float64
	// Observe must be concurrency-safe, return promptly, and must not panic.
	Observe func(context.Context, Observation)
}

// FallbackRequested configures Config.Fallback to use the requested model when
// the selector fails.
const FallbackRequested = "requested"

// Result identifies the selected public model and preserves ordered generation data,
// including reasoning blocks the application may store or strip.
type Result struct {
	ID             string
	CreatedAt      int64
	Model          string
	BackendModel   string
	Classification string
	Response       llms.InferenceResponse
}

// Router is safe for concurrent requests when supplied callbacks and connectors are.
type Router struct {
	cfg     Config
	targets []Target
	aliases map[string]int
}

// New validates a catalog and installs defaults. It never invokes a provider.
func New(cfg Config) (*Router, error) {
	if cfg.Selector != nil && isNil(cfg.Selector) {
		return nil, errors.New("router: selector is a nil implementation")
	}
	if cfg.Admission != nil && isNil(cfg.Admission) {
		return nil, errors.New("router: admission is a nil implementation")
	}
	if len(cfg.Targets) == 0 {
		return nil, errors.New("router: at least one target is required")
	}
	if cfg.SelectorTimeout == 0 {
		cfg.SelectorTimeout = defaultSelectorTimeout
	}
	if cfg.Timeout < 0 || cfg.SelectorTimeout < 0 {
		return nil, errors.New("router: timeouts must not be negative")
	}
	if math.IsNaN(cfg.MinimumConfidence) || cfg.MinimumConfidence < 0 || cfg.MinimumConfidence > 1 {
		return nil, errors.New("router: minimum confidence must be within [0,1]")
	}
	r := &Router{
		cfg:     cfg,
		aliases: map[string]int{},
	}
	for i, t := range cfg.Targets {
		if err := validateTarget(t); err != nil {
			return nil, err
		}
		t.Aliases = slices.Clone(t.Aliases)
		t.Tags = cloneTags(t.Tags)
		for _, name := range append([]string{t.ID}, t.Aliases...) {
			if name == "" || len(name) > maxModelNameBytes {
				return nil, errors.Errorf("router: target %q has an empty or oversized alias", t.ID)
			}
			if _, ok := r.aliases[name]; ok {
				return nil, errors.Errorf("router: duplicate model or alias %q", name)
			}
			r.aliases[name] = i
		}
		r.targets = append(r.targets, t)
	}
	if cfg.Fallback != "" && cfg.Fallback != FallbackRequested {
		i, ok := r.aliases[cfg.Fallback]
		if !ok || r.targets[i].ID != cfg.Fallback {
			return nil, errors.Errorf("router: fallback %q is not a registered target ID", cfg.Fallback)
		}
	}
	r.cfg.Targets = nil
	return r, nil
}

func validateTarget(t Target) error {
	switch {
	case t.ID == "":
		return errors.New("router: target ID is required")
	case t.ID == FallbackRequested:
		return errors.Errorf("router: target ID %q is reserved", FallbackRequested)
	case len(t.ID) > maxModelNameBytes:
		return errors.Errorf("router: target ID %q exceeds %d bytes", t.ID, maxModelNameBytes)
	case t.BackendModel == "":
		return errors.Errorf("router: target %q requires a backend model", t.ID)
	case len(t.BackendModel) > maxBackendNameBytes:
		return errors.Errorf("router: target %q backend model exceeds %d bytes", t.ID, maxBackendNameBytes)
	case isNil(t.Connector):
		return errors.Errorf("router: target %q requires a connector", t.ID)
	case t.MaxOutputTokens < 0 || t.MaxOutputTokens > maxOutputTokens:
		return errors.Errorf("router: target %q output limit must be within [0,%d]", t.ID, maxOutputTokens)
	case len(t.Tags) > maxTags:
		return errors.Errorf("router: target %q has more than %d tags", t.ID, maxTags)
	}
	for k, v := range t.Tags {
		if k == "" || len(k) > maxTagKeyBytes || len(v) > maxTagValueBytes {
			return errors.Errorf("router: target %q has an invalid tag %q", t.ID, k)
		}
	}
	return nil
}

func cloneTags(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// Connector exposes the optional lossless inference contract of a constructed
// model. Legacy-only models are rejected rather than silently losing controls.
func Connector(model llms.Model) (llms.InferenceModel, error) {
	m, ok := model.(llms.InferenceModel)
	if !ok || isNil(m) {
		return nil, errors.Errorf("router: model %q does not implement llms.InferenceModel", modelName(model))
	}
	return m, nil
}

func modelName(model llms.Model) string {
	if model == nil || isNil(model) {
		return ""
	}
	return model.GetName()
}

// Generate validates, selects and performs exactly one backend inference.
func (r *Router) Generate(ctx context.Context, req Request) (result *Result, err error) {
	start := time.Now()
	callerCtx := ctx
	observation := Observation{
		RequestID: requestIDPrefix + uuid.NewString(),
	}
	defer func() {
		observation.Duration = time.Since(start)
		observation.Err = err
		if r.cfg.Observe != nil {
			r.cfg.Observe(callerCtx, observation)
		}
	}()
	if ctx.Err() != nil {
		return nil, contextError(ctx.Err())
	}
	if r.cfg.Admission != nil {
		release, admitErr := r.cfg.Admission.Admit(ctx, AdmissionRequest{
			Model:    req.Model,
			Metadata: cloneTags(req.Metadata),
		})
		if admitErr != nil {
			return nil, admissionError(admitErr)
		}
		if release != nil {
			defer release()
		}
	}
	if r.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.cfg.Timeout)
		defer cancel()
	}
	req = req.Clone()
	if err = validateRequest(&req); err != nil {
		return nil, err
	}
	eligible := map[int]bool{}
	var candidates []Candidate
	var requestedErr error
	requestedIdx, requestedKnown := r.aliases[req.Model]
	for i, t := range r.targets {
		if t.Allow != nil && !t.Allow(ctx) {
			continue
		}
		candidateErr := supports(t, req.Input)
		if candidateErr == nil {
			candidateErr = t.Connector.ValidateInference(backendInput(req.Input, t))
		}
		if candidateErr != nil {
			if requestedKnown && requestedIdx == i {
				requestedErr = candidateErr
			}
			continue
		}
		eligible[i] = true
		candidates = append(candidates, Candidate{
			ID:       t.ID,
			Features: t.Features,
			Tags:     cloneTags(t.Tags),
		})
	}
	selected := -1
	chooseRequested := func() {
		if requestedKnown && eligible[requestedIdx] {
			selected = requestedIdx
		}
	}
	if r.cfg.Selector == nil {
		chooseRequested()
		if selected < 0 {
			if requestedErr != nil {
				return nil, translateError(requestedErr)
			}
			return nil, NewError(KindModelNotFound, paramModel, "model not found")
		}
	} else {
		if len(candidates) == 0 {
			return nil, NewError(KindSelectionUnavailable, "", "no eligible model")
		}
		selectionCtx, selectionCancel := context.WithTimeout(ctx, r.cfg.SelectorTimeout)
		d, selectionErr := r.cfg.Selector.Select(selectionCtx, SelectionInput{
			Request:    req.Clone(),
			Candidates: candidates,
		})
		if selectionCtx.Err() != nil {
			selectionErr = errors.WithMessage(selectionCtx.Err(), "selector deadline")
		}
		selectionCancel()
		observation.SelectorUsage = d.Usage
		observation.SelectorUsageKnown = d.UsageKnown
		if selectionErr == nil {
			selected, selectionErr = r.validateDecision(d, eligible)
			if selectionErr == nil {
				observation.Classification = d.Classification
			}
		}
		if selectionErr != nil {
			if r.cfg.Fallback == FallbackRequested {
				chooseRequested()
			} else if i, ok := r.aliases[r.cfg.Fallback]; ok && eligible[i] {
				selected = i
			}
			if selected < 0 {
				return nil, errors.WithStack(&Error{
					Kind:    KindSelectionUnavailable,
					Message: "model selection failed",
					Cause:   selectionErr,
				})
			}
		}
	}
	if ctx.Err() != nil {
		return nil, contextError(ctx.Err())
	}
	target := r.targets[selected]
	observation.TargetID = target.ID
	observation.BackendModel = target.BackendModel
	out, callErr := target.Connector.Infer(ctx, backendInput(req.Input, target))
	if out != nil {
		observation.Usage = out.Usage
		observation.UsageKnown = out.UsageKnown
	}
	if callErr != nil {
		if ctx.Err() != nil {
			return nil, contextError(ctx.Err())
		}
		return nil, translateError(callErr)
	}
	out, err = normalizeResponse(req.Input, out)
	if err != nil {
		return nil, errors.WithStack(&Error{
			Kind:    KindUpstream,
			Message: "invalid model response",
			Cause:   err,
		})
	}
	if !out.UsageKnown && observation.UsageKnown {
		observation.UsageInconsistent = true
		observation.UsageKnown = false
	}
	return &Result{
		ID:             observation.RequestID,
		CreatedAt:      start.Unix(),
		Model:          target.ID,
		BackendModel:   target.BackendModel,
		Classification: observation.Classification,
		Response:       *out,
	}, nil
}

// backendInput returns the connector's copy of the request with the target's
// backend model and default output limit applied.
func backendInput(in llms.InferenceRequest, t Target) llms.InferenceRequest {
	out := in.Clone()
	out.Model = t.BackendModel
	if out.MaxTokens == nil && t.MaxOutputTokens > 0 {
		v := t.MaxOutputTokens
		out.MaxTokens = &v
	}
	return out
}

func (r *Router) validateDecision(d Decision, eligible map[int]bool) (int, error) {
	if len(d.Classification) > maxClassificationBytes {
		return -1, errors.Errorf("classification exceeds %d bytes", maxClassificationBytes)
	}
	if d.Confidence != nil && (math.IsNaN(*d.Confidence) || *d.Confidence < 0 || *d.Confidence > 1) {
		return -1, errors.New("confidence must be within [0,1]")
	}
	if r.cfg.MinimumConfidence > 0 && (d.Confidence == nil || *d.Confidence < r.cfg.MinimumConfidence) {
		return -1, errors.New("confidence below configured minimum")
	}
	i, ok := r.aliases[d.TargetID]
	if !ok || !eligible[i] || r.targets[i].ID != d.TargetID {
		return -1, errors.Errorf("selected target %q is not an eligible target ID", d.TargetID)
	}
	return i, nil
}

func isNil(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return r.IsNil()
	}
	return false
}

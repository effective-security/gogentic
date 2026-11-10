package llmfactory

import (
	"context"
	"iter"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/metricskey"
	"github.com/effective-security/xlog"
)

// Keys of the events that are logged once per model list.
const (
	noteSubstituted = "substituted"
	noteSelected    = "selected"
	noteDeprecated  = "deprecated"
	noteNotListed   = "not_listed"
	noteFamilies    = "families"
)

// selection is one model a resolution may use: the outcome of matching one
// reference against one provider.
type selection struct {
	// policy is the provider that serves the model.
	policy *providerPolicy
	// list is the model list the selection was made with, or nil.
	list *modelList
	// reference is the configuration entry that selected the model, as
	// written.
	reference string
	// requested is the model or pattern the reference names, without its
	// effort.
	requested string
	// model is the ID of the selected model.
	model string
	// effort holds the effort settings for the client of the model.
	effort llms.EffortPolicy
	// reason says how the reference selected the model.
	reason ResolutionReason
	// isDefault is set for the default model of the provider.
	isDefault bool
}

// resolution describes the selection to a caller of Resolve.
func (s selection) resolution() *Resolution {
	info, verified := s.list.lookup(s.model)
	return &Resolution{
		Provider:  s.policy.cfg.Name,
		Reference: s.reference,
		Model:     s.model,
		Effort:    s.effort,
		Reason:    s.reason,
		Default:   s.isDefault,
		Verified:  verified,
		Lifecycle: info.Lifecycle,
		RetiresAt: info.RetiresAt,
	}
}

// noted returns the record of what was logged for the model list the
// selection was made with, or for the provider when there is no list.
func (s selection) noted() *onceSet {
	if s.list != nil {
		return &s.list.noted
	}
	return &s.policy.noted
}

// GetModel returns an LLM model that matches the given options.
func (f *factory) GetModel(ctx context.Context, opts ModelOptions) (llms.Model, error) {
	// A specific provider type takes precedence over name-based resolution.
	if opts.ProviderType != "" {
		return f.getModelByType(ctx, opts)
	}

	for sel, err := range f.selections(ctx, opts) {
		if err != nil {
			return nil, err
		}
		model, err := f.instance(sel.policy, sel.model, sel.effort)
		if err != nil {
			if sel.isDefault {
				return nil, err
			}
			logger.KV(xlog.ERROR,
				"reason", "NewLLM",
				"type", sel.policy.cfg.OpenAI.APIType,
				"version", sel.policy.cfg.OpenAI.APIVersion,
				"provider", sel.policy.cfg.Name,
				"model", sel.model,
				"err", err.Error(),
			)
			continue
		}
		f.note(sel)
		return model, nil
	}
	return nil, errors.New("no LLM model found")
}

// Resolve implements [Catalog].
func (f *factory) Resolve(ctx context.Context, opts ModelOptions) (*Resolution, error) {
	if opts.ProviderType != "" {
		policy, err := f.typePolicy(opts)
		if err != nil {
			return nil, err
		}
		sel, ok, err := f.typeSelection(ctx, policy)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.Errorf("provider %s has no usable default model", policy.cfg.Name)
		}
		return sel.resolution(), nil
	}

	next, stop := iter.Pull2(f.selections(ctx, opts))
	defer stop()
	sel, err, ok := next()
	if !ok {
		return nil, errors.New("no LLM model found")
	}
	if err != nil {
		return nil, err
	}
	return sel.resolution(), nil
}

// Refresh implements [Catalog].
func (f *factory) Refresh(ctx context.Context) error {
	var (
		wg   sync.WaitGroup
		lock sync.Mutex
		errs []error
	)
	for _, policy := range f.policies {
		if policy.catalog == nil {
			continue
		}
		wg.Go(func() {
			if err := policy.catalog.refreshNow(ctx); err != nil {
				lock.Lock()
				defer lock.Unlock()
				errs = append(errs, err)
			}
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

// preferredReferences expands the assistant model mapping (optionally
// per-org) into the references to try, caller-provided ones first.
func (f *factory) preferredReferences(opts ModelOptions) []string {
	if opts.AssistantName == "" {
		return opts.PreferredModels
	}
	assistantModels := f.getOrgAssistants(opts.OrgID)
	modelNames, ok := assistantModels[opts.AssistantName]
	if !ok {
		modelNames, ok = assistantModels["default"]
	}
	if !ok {
		return opts.PreferredModels
	}
	return append(slices.Clone(opts.PreferredModels), modelNames...)
}

// selections yields the models a resolution may use, best first: for each
// preferred reference in order the models it selects, then the default model
// of the default provider. It ends with an error when the default cannot be
// used, so a caller that takes the first usable selection always gets a model
// or an error.
//
// Model lists are read as providers are reached, without the factory lock.
func (f *factory) selections(ctx context.Context, opts ModelOptions) iter.Seq2[selection, error] {
	return func(yield func(selection, error) bool) {
		for _, reference := range f.preferredReferences(opts) {
			if !f.referenceSelections(ctx, opts, reference, yield) {
				return
			}
		}
		f.defaultSelections(ctx, opts, yield)
	}
}

// referenceSelections yields the models one reference selects. The providers
// the reference can address are asked in config order for the model it names
// or the models its pattern matches. The models that stand in for a named
// model that cannot be used come after all of those, so that the named model
// on a later provider wins over a substitute on an earlier one.
//
// It returns false when the caller stopped the iteration.
func (f *factory) referenceSelections(ctx context.Context, opts ModelOptions, reference string, yield func(selection, error) bool) bool {
	spec, err := parseRefSpec(reference)
	if err != nil {
		logger.KV(xlog.WARNING,
			"reason", "invalid_model_reference",
			"reference", reference,
			"err", err.Error(),
		)
		return true
	}
	scope, modelName := f.splitProvider(spec.path)
	ref := newModelMatcher(modelName)

	// An exact reference is checked against the filter as written, before
	// any provider is consulted. A pattern has no name to check yet.
	if !ref.isPattern() && !f.isModelAllowed(ctx, opts.OrgID, opts.AssistantName, spec.path) {
		return true
	}

	var substitutes []selection
	for _, policy := range f.policies {
		if !scope.includes(policy) {
			continue
		}
		if !supportsCapabilities(policy.typ, opts.RequiredCapabilities) {
			continue
		}
		list := policy.models(ctx)
		candidates := policy.candidates(ref, list)
		noteCandidates(policy, list, ref, candidates)
		for _, cand := range candidates {
			sel := selection{
				policy:    policy,
				list:      list,
				reference: reference,
				requested: modelName,
				model:     cand.model,
				effort:    policy.effortPolicy(spec.effort, cand.model, list),
				reason:    cand.reason,
			}
			if cand.reason == ReasonFamily {
				substitutes = append(substitutes, sel)
				continue
			}
			if !f.isCandidateAllowed(ctx, opts, scope, ref, spec.path, cand.model) {
				continue
			}
			if !yield(sel, nil) {
				return false
			}
		}
	}
	for _, sel := range substitutes {
		if !f.isCandidateAllowed(ctx, opts, scope, ref, spec.path, sel.model) {
			continue
		}
		if !yield(sel, nil) {
			return false
		}
	}
	return true
}

// defaultSelections yields the default model of the default provider,
// honoring capability and org filters, and then the error that explains why
// no default can be used.
func (f *factory) defaultSelections(ctx context.Context, opts ModelOptions, yield func(selection, error) bool) {
	if len(f.cfg.Providers) == 0 || f.defaultProvider == nil {
		yield(selection{}, errors.New("no providers configured"))
		return
	}
	if f.defaultProvider.DefaultModel == "" {
		yield(selection{}, errors.New("no default model configured"))
		return
	}

	policy := f.policyFor(f.defaultProvider)
	if !supportsCapabilities(policy.typ, opts.RequiredCapabilities) {
		yield(selection{}, errors.Errorf("default provider %s does not support required capabilities", f.defaultProvider.Name))
		return
	}

	list := policy.models(ctx)
	candidates, effort := policy.defaultCandidates(list)
	if len(candidates) == 0 {
		yield(selection{}, errDefaultUnavailable(f.defaultProvider))
		return
	}

	// A pattern default, or the family of a default the provider no longer
	// serves, selects several models: the best one the org filter allows is
	// used, as for a pattern in assistant_models.
	for _, cand := range candidates {
		if !f.isModelAllowed(ctx, opts.OrgID, opts.AssistantName, cand.model) {
			continue
		}
		sel := selection{
			policy:    policy,
			list:      list,
			reference: f.defaultProvider.DefaultModel,
			requested: policy.fallback.text,
			model:     cand.model,
			effort:    policy.effortPolicy(effort, cand.model, list),
			reason:    cand.reason,
			isDefault: true,
		}
		if !yield(sel, nil) {
			return
		}
	}
	yield(selection{}, errors.Errorf("model not available for org: %s", policy.fallback.text))
}

// typePolicy returns the first provider of the requested type, after
// checking that the type supports the required capabilities.
func (f *factory) typePolicy(opts ModelOptions) (*providerPolicy, error) {
	providerType := opts.ProviderType
	for _, policy := range f.policies {
		if policy.cfg.OpenAI.APIType != string(providerType) {
			continue
		}
		if !supportsCapabilities(configProviderType(policy.cfg), opts.RequiredCapabilities) {
			return nil, errors.Errorf("provider %s does not support required capabilities", policy.cfg.Name)
		}
		return policy, nil
	}
	return nil, errors.Errorf("provider not found for type: %s", providerType)
}

// errDefaultUnavailable says that the default model of a provider cannot be
// used.
func errDefaultUnavailable(cfg *ProviderConfig) error {
	return errors.Errorf("default model %s of provider %s is not available", cfg.DefaultModel, cfg.Name)
}

// typeSelection returns the default model of a provider that was asked for
// by type.
//
// ok is false when the factory cannot name a default and the provider
// constructor is left to choose, as it always was: no default_model is
// configured, or one that cannot be used. With discovery that is an error
// instead: the constructor would build a client for the configured default,
// which the list says the provider no longer serves.
func (f *factory) typeSelection(ctx context.Context, policy *providerPolicy) (sel selection, ok bool, err error) {
	list := policy.models(ctx)
	candidates, effort := policy.defaultCandidates(list)
	if len(candidates) == 0 {
		if policy.discovers() && policy.fallback != nil {
			return selection{}, false, errDefaultUnavailable(policy.cfg)
		}
		return selection{}, false, nil
	}
	best := candidates[0]
	return selection{
		policy:    policy,
		list:      list,
		reference: policy.cfg.DefaultModel,
		requested: policy.fallback.text,
		model:     best.model,
		effort:    policy.effortPolicy(effort, best.model, list),
		reason:    best.reason,
		isDefault: true,
	}, true, nil
}

// getModelByType returns a model for the requested provider type: the
// default model of the first provider of that type.
func (f *factory) getModelByType(ctx context.Context, opts ModelOptions) (llms.Model, error) {
	policy, err := f.typePolicy(opts)
	if err != nil {
		return nil, err
	}
	sel, ok, err := f.typeSelection(ctx, policy)
	if err != nil {
		return nil, err
	}
	if ok {
		model, err := f.instance(sel.policy, sel.model, sel.effort)
		if err != nil {
			return nil, err
		}
		f.note(sel)
		return model, nil
	}

	// Without a usable default the provider constructor decides, as it
	// always did.
	f.lock.Lock()
	defer f.lock.Unlock()

	providerType := opts.ProviderType
	if client, ok := f.byType[providerType]; ok {
		return client, nil
	}
	cfg := policy.cfg
	model, err := NewLLM(cfg, nil, f.options)
	if err != nil {
		return nil, err
	}

	logger.KV(xlog.DEBUG,
		"status", "created_llm",
		"type", cfg.OpenAI.APIType,
		"version", cfg.OpenAI.APIVersion,
		"name", cfg.Name)

	f.byType[providerType] = model
	return model, nil
}

// isCandidateAllowed applies the model filter to one concrete model selected
// by a reference. A model the reference did not name, because it is the match
// of a pattern or stands in for the named model, is shown to the filter
// first with the provider prefix the reference used, and then as the bare
// model ID. The named model was shown as written before the providers were
// asked, so only its bare ID is left to check.
func (f *factory) isCandidateAllowed(ctx context.Context, opts ModelOptions, scope providerScope, ref modelMatcher, path, model string) bool {
	qualified := path
	if model != ref.text {
		qualified = scope.qualify(model)
		if !f.isModelAllowed(ctx, opts.OrgID, opts.AssistantName, qualified) {
			return false
		}
	}
	return model == qualified || f.isModelAllowed(ctx, opts.OrgID, opts.AssistantName, model)
}

// instance returns the client for a model of a provider with the given effort
// settings, building it on first use. Clients are shared by every reference
// that resolves to the same provider, model and effort.
func (f *factory) instance(policy *providerPolicy, model string, effort llms.EffortPolicy) (llms.Model, error) {
	f.lock.Lock()
	defer f.lock.Unlock()

	key := instanceKey(policy.index, model, effort)
	if client, ok := f.byName[key]; ok {
		return client, nil
	}

	client, err := NewLLM(policy.cfg, []string{model}, f.optionsFor(effort))
	if err != nil {
		return nil, err
	}

	logger.KV(xlog.DEBUG,
		"status", "created_llm",
		"type", policy.cfg.OpenAI.APIType,
		"version", policy.cfg.OpenAI.APIVersion,
		"provider", policy.cfg.Name,
		"model", model,
		"effort", effort.Default.String(),
		"max_effort", effort.Max.String(),
	)

	f.byName[key] = client
	return client, nil
}

// note reports a selection that was used: a counter for every substitution,
// and a log line the first time a model list leads to it.
func (f *factory) note(sel selection) {
	provider := sel.policy.cfg.Name
	if sel.reason == ReasonFamily {
		metricskey.StatsLLMModelSubstituted.IncrCounter(1, provider, sel.requested, sel.model)
	}

	noted := sel.noted()
	pair := sel.requested + cacheKeySeparator + sel.model
	switch {
	case sel.reason == ReasonFamily:
		if noted.first(noteSubstituted + cacheKeySeparator + pair) {
			logger.KV(xlog.INFO,
				"status", "model_substituted",
				"provider", provider,
				"reference", sel.reference,
				"requested", sel.requested,
				"model", sel.model,
				"reason", string(sel.reason),
			)
		}
	case sel.reason == ReasonPattern && sel.list != nil:
		if noted.first(noteSelected + cacheKeySeparator + pair) {
			logger.KV(xlog.INFO,
				"status", "model_selected",
				"provider", provider,
				"reference", sel.reference,
				"model", sel.model,
				"reason", string(sel.reason),
			)
		}
	}

	info, ok := sel.list.lookup(sel.model)
	if ok && info.Lifecycle == llms.ModelLifecycleDeprecated && noted.first(noteDeprecated+cacheKeySeparator+sel.model) {
		logger.KV(xlog.WARNING,
			"reason", "model_deprecated",
			"provider", provider,
			"reference", sel.reference,
			"model", sel.model,
			"retires_at", formatRetirement(info.RetiresAt),
		)
	}
}

// noteCandidates warns, once per model list, about a reference that does not
// do what its author probably meant: a named model that the configuration
// allows but the provider no longer serves, and a pattern whose matches belong
// to several families, between which the order is arbitrary.
func noteCandidates(policy *providerPolicy, list *modelList, ref modelMatcher, candidates []candidate) {
	noted := &policy.noted
	if list != nil {
		noted = &list.noted
	}

	if !ref.isPattern() {
		if list == nil || !policy.allowed(ref.text) || list.serves(ref.text) {
			return
		}
		if !noted.first(noteNotListed + cacheKeySeparator + ref.text) {
			return
		}
		substitute := ""
		if len(candidates) > 0 {
			substitute = candidates[0].model
		}
		logger.KV(xlog.WARNING,
			"reason", "model_not_listed",
			"provider", policy.cfg.Name,
			"model", ref.text,
			"substitute", substitute,
		)
		return
	}

	families := make(map[string]struct{})
	for _, cand := range candidates {
		families[llms.ParseModelName(cand.model).Key()] = struct{}{}
	}
	if len(families) < 2 || !noted.first(noteFamilies+cacheKeySeparator+ref.text) {
		return
	}
	names := make([]string, 0, len(families))
	for family := range families {
		names = append(names, family)
	}
	sort.Strings(names)
	logger.KV(xlog.WARNING,
		"reason", "pattern_spans_families",
		"provider", policy.cfg.Name,
		"pattern", ref.text,
		"families", strings.Join(names, ","),
	)
}

// formatRetirement formats a retirement date for a log line.
func formatRetirement(retiresAt time.Time) string {
	if retiresAt.IsZero() {
		return ""
	}
	return retiresAt.Format(time.DateOnly)
}

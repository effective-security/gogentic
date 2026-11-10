package llmfactory

import (
	"context"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
)

// policyEntry is one available_models or default_model entry: a model ID or
// pattern, and the effort used by default for the models it admits.
type policyEntry struct {
	modelMatcher
	// effort is the level written on the entry, or
	// llms.ReasoningEffortDefault when it has none.
	effort llms.ReasoningEffort
}

// providerPolicy is the compiled model policy of one provider: which models
// it allows and denies, its default model and its effort ceiling.
type providerPolicy struct {
	// cfg is the provider configuration the policy was built from.
	cfg *ProviderConfig
	// index is the position of the provider in the configuration.
	index int
	// typ is the normalized provider type.
	typ llms.ProviderType
	// allow holds the available_models entries, in configuration order.
	allow []policyEntry
	// deny holds the disabled_models entries.
	deny []modelMatcher
	// fallback is the default_model entry, or nil when none is configured.
	fallback *policyEntry
	// maxEffort is the effort ceiling, or llms.ReasoningEffortDefault.
	maxEffort llms.ReasoningEffort
	// refresh is how long a model list read from the provider is trusted.
	// Zero means discovery is off.
	refresh time.Duration
	// catalog reads the model list of the provider. The factory sets it when
	// discovery is on; it is nil otherwise.
	catalog *modelCatalog
	// noted records what was logged about resolutions made without a list.
	noted onceSet
}

// candidate is a model a reference selects on a provider, and how.
type candidate struct {
	model  string
	reason ResolutionReason
}

// newProviderPolicy compiles the model policy of a provider. Entries that do
// not parse are left out of the policy and reported in the returned error, so
// a caller that cannot fail still gets a usable policy.
func newProviderPolicy(cfg *ProviderConfig, index int) (*providerPolicy, error) {
	policy := &providerPolicy{
		cfg:   cfg,
		index: index,
		typ:   configProviderType(cfg),
	}
	var errs []error

	for _, raw := range cfg.AvailableModels {
		spec, err := parseRefSpec(raw)
		if err != nil {
			errs = append(errs, errors.WithMessagef(err, "provider %s: available_models", cfg.Name))
			continue
		}
		policy.allow = append(policy.allow, policyEntry{
			modelMatcher: newModelMatcher(spec.path),
			effort:       spec.effort,
		})
	}

	for _, raw := range cfg.DisabledModels {
		spec, err := parseRefSpec(raw)
		if err != nil {
			errs = append(errs, errors.WithMessagef(err, "provider %s: disabled_models", cfg.Name))
			continue
		}
		if spec.effort != llms.ReasoningEffortDefault {
			errs = append(errs, errors.Errorf("provider %s: disabled_models entry %q must not carry an effort", cfg.Name, raw))
			continue
		}
		policy.deny = append(policy.deny, newModelMatcher(spec.path))
	}

	if cfg.MaxEffort != "" {
		maxEffort, err := llms.ParseReasoningEffort(cfg.MaxEffort)
		if err != nil {
			errs = append(errs, errors.WithMessagef(err, "provider %s: max_effort", cfg.Name))
		}
		policy.maxEffort = maxEffort
	}

	if cfg.Discovery != nil {
		refresh, err := discoveryRefresh(cfg)
		enabled := cfg.Discovery.Enabled
		switch {
		case err != nil:
			errs = append(errs, err)
		case enabled && !policy.typ.Supports(llms.CapabilityModels):
			errs = append(errs, errors.Errorf("provider %s: discovery is not available for api_type %q", cfg.Name, cfg.OpenAI.APIType))
		case enabled:
			policy.refresh = refresh
		}
	}

	if cfg.DefaultModel != "" {
		spec, err := parseRefSpec(cfg.DefaultModel)
		if err != nil {
			errs = append(errs, errors.WithMessagef(err, "provider %s: default_model", cfg.Name))
		} else {
			policy.fallback = &policyEntry{
				modelMatcher: newModelMatcher(spec.path),
				effort:       spec.effort,
			}
		}
	}

	return policy, errors.Join(errs...)
}

// discoveryRefresh returns how long a model list of the provider is trusted.
func discoveryRefresh(cfg *ProviderConfig) (time.Duration, error) {
	if cfg.Discovery.Refresh == "" {
		return defaultDiscoveryRefresh, nil
	}
	refresh, err := time.ParseDuration(cfg.Discovery.Refresh)
	if err != nil {
		return 0, errors.Wrapf(err, "provider %s: discovery.refresh", cfg.Name)
	}
	if refresh < minDiscoveryRefresh {
		return 0, errors.Errorf("provider %s: discovery.refresh %s is below the minimum of %s", cfg.Name, refresh, minDiscoveryRefresh)
	}
	return refresh, nil
}

// discovers reports whether the model list of the provider is read.
func (p *providerPolicy) discovers() bool {
	return p.refresh > 0
}

// models returns the model list of the provider to resolve with, or nil when
// discovery is off or no list could be read. It may wait for the first fetch,
// so it must not be called with the factory lock held.
func (p *providerPolicy) models(ctx context.Context) *modelList {
	if p.catalog == nil {
		return nil
	}
	return p.catalog.current(ctx)
}

// validate reports configuration that parses but cannot work: a default model
// that is denied, or a default pattern that selects nothing.
func (p *providerPolicy) validate() error {
	if p.fallback == nil {
		return nil
	}
	if _, _, ok := p.defaultModel(); ok {
		return nil
	}
	if p.fallback.isPattern() {
		if p.discovers() {
			// The models the pattern selects come from the provider.
			return nil
		}
		return errors.Errorf("provider %s: default_model %q matches no available_models entry", p.cfg.Name, p.fallback.text)
	}
	return errors.Errorf("provider %s: default_model %q is listed in disabled_models", p.cfg.Name, p.fallback.text)
}

// denied reports whether a disabled_models entry matches the model.
func (p *providerPolicy) denied(model string) bool {
	for _, entry := range p.deny {
		if entry.match(model) {
			return true
		}
	}
	return false
}

// allowEntry returns the first available_models entry that matches the model.
func (p *providerPolicy) allowEntry(model string) (policyEntry, bool) {
	for _, entry := range p.allow {
		if entry.match(model) {
			return entry, true
		}
	}
	return policyEntry{}, false
}

// allowed reports whether the model matches an available_models entry and no
// disabled_models entry.
func (p *providerPolicy) allowed(model string) bool {
	_, ok := p.allowEntry(model)
	return ok && !p.denied(model)
}

// literals returns the model IDs named exactly in available_models that are
// not denied. They are the models the factory knows to exist without asking
// the provider.
func (p *providerPolicy) literals() []string {
	var ids []string
	for _, entry := range p.allow {
		if !entry.isPattern() && !p.denied(entry.text) {
			ids = append(ids, entry.text)
		}
	}
	return ids
}

// known returns the models the factory knows the provider to have, and may
// use: with a list, the models it serves that available_models allows and
// disabled_models does not deny; without one, the IDs named in
// available_models. The result must not be modified.
func (p *providerPolicy) known(list *modelList) []string {
	if list == nil {
		return p.literals()
	}
	// A list belongs to one provider, so it can keep the answer.
	list.usableOnce.Do(func() {
		list.usable = p.usable(list)
	})
	return list.usable
}

// usable computes what known returns for a list.
func (p *providerPolicy) usable(list *modelList) []string {
	literals := p.literals()
	ids := make([]string, 0, len(list.models))
	seen := make(map[string]struct{}, len(list.models))
	for _, model := range list.models {
		if model.Lifecycle == llms.ModelLifecycleRetired || !p.allowed(model.ID) {
			continue
		}
		if _, dup := seen[model.ID]; dup {
			continue
		}
		seen[model.ID] = struct{}{}
		ids = append(ids, model.ID)
	}
	// A floating ID the provider accepts without listing it, such as
	// claude-haiku-4-5 for the listed claude-haiku-4-5-20251001.
	for _, id := range literals {
		if _, dup := seen[id]; !dup && list.serves(id) {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids
}

// candidates returns the models a reference selects on this provider, best
// first.
//
// A pattern selects the known models it matches. An exact ID selects itself
// when it is allowed and, with a list, still served. With a list, an exact ID
// that cannot be used gives way to the usable models of its family.
func (p *providerPolicy) candidates(ref modelMatcher, list *modelList) []candidate {
	if ref.isPattern() {
		var matches []string
		for _, id := range p.known(list) {
			if ref.match(id) {
				matches = append(matches, id)
			}
		}
		return toCandidates(rankModels(matches, list), ReasonPattern)
	}
	if p.allowed(ref.text) && (list == nil || list.serves(ref.text)) {
		return toCandidates([]string{ref.text}, ReasonExact)
	}
	return toCandidates(p.family(ref.text, list), ReasonFamily)
}

// family returns the usable models of the family of model, best first, model
// itself left out. Models are of one family when [llms.ModelName.Key] is the
// same for both: the name without version and snapshot, on the same route and
// in the same variant. Without a list nothing is known about a family, and
// nothing is returned.
func (p *providerPolicy) family(model string, list *modelList) []string {
	if list == nil {
		return nil
	}
	key := llms.ParseModelName(model).Key()
	var relatives []string
	for _, id := range p.known(list) {
		if id != model && llms.ParseModelName(id).Key() == key {
			relatives = append(relatives, id)
		}
	}
	return rankModels(relatives, list)
}

// toCandidates pairs model IDs with the reason they were selected.
func toCandidates(ids []string, reason ResolutionReason) []candidate {
	if len(ids) == 0 {
		return nil
	}
	candidates := make([]candidate, 0, len(ids))
	for _, id := range ids {
		candidates = append(candidates, candidate{
			model:  id,
			reason: reason,
		})
	}
	return candidates
}

// defaultCandidates returns the models default_model selects, best first, and
// the effort written on it. An exact default selects itself and does not need
// to be listed in available_models; a pattern selects every known model it
// matches. With a list, an exact default the provider no longer serves gives
// way to the usable models of its family. No model is returned when
// default_model is unset, denied, or selects nothing.
func (p *providerPolicy) defaultCandidates(list *modelList) (candidates []candidate, effort llms.ReasoningEffort) {
	if p.fallback == nil {
		return nil, llms.ReasoningEffortDefault
	}
	text := p.fallback.text
	usable := !p.denied(text)
	switch {
	case p.fallback.isPattern():
		candidates = p.candidates(p.fallback.modelMatcher, list)
	case usable && (list == nil || list.serves(text)):
		candidates = toCandidates([]string{text}, ReasonExact)
	case usable:
		candidates = toCandidates(p.family(text, list), ReasonFamily)
	}
	if len(candidates) == 0 {
		return nil, llms.ReasoningEffortDefault
	}
	return candidates, p.fallback.effort
}

// defaultModel returns the default model of the provider as the configuration
// alone names it, without asking the provider, and the effort written on
// default_model.
func (p *providerPolicy) defaultModel() (model string, effort llms.ReasoningEffort, ok bool) {
	candidates, effort := p.defaultCandidates(nil)
	if len(candidates) == 0 {
		return "", llms.ReasoningEffortDefault, false
	}
	return candidates[0].model, effort, true
}

// effortPolicy returns the effort settings for a model selected on this
// provider. refEffort is the level written on the reference that selected the
// model; without one, the level of the first matching available_models entry
// applies. With a list, the levels and the default level the provider reports
// for the model are added.
func (p *providerPolicy) effortPolicy(refEffort llms.ReasoningEffort, model string, list *modelList) llms.EffortPolicy {
	policy := llms.EffortPolicy{
		Default: refEffort,
		Max:     p.maxEffort,
	}
	if policy.Default == llms.ReasoningEffortDefault {
		if entry, ok := p.allowEntry(model); ok {
			policy.Default = entry.effort
		}
	}
	if info, ok := list.lookup(model); ok {
		policy.Supported = info.Efforts
		policy.Implicit = info.DefaultEffort
	}
	return policy
}

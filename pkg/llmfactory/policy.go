package llmfactory

import (
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

// candidates returns the models a reference selects on this provider, best
// first. An exact ID selects itself when it is allowed; a pattern selects the
// known models it matches.
func (p *providerPolicy) candidates(ref modelMatcher) []string {
	if !ref.isPattern() {
		if p.allowed(ref.text) {
			return []string{ref.text}
		}
		return nil
	}
	var matches []string
	for _, id := range p.literals() {
		if ref.match(id) {
			matches = append(matches, id)
		}
	}
	return rankModels(matches)
}

// defaultCandidates returns the models default_model selects, best first, and
// the effort written on it. An exact default selects itself and does not need
// to be listed in available_models; a pattern selects every known model it
// matches. No model is returned when default_model is unset, denied, or a
// pattern that matches nothing.
func (p *providerPolicy) defaultCandidates() (models []string, effort llms.ReasoningEffort) {
	if p.fallback == nil {
		return nil, llms.ReasoningEffortDefault
	}
	if p.fallback.isPattern() {
		models = p.candidates(p.fallback.modelMatcher)
	} else if !p.denied(p.fallback.text) {
		models = []string{p.fallback.text}
	}
	if len(models) == 0 {
		return nil, llms.ReasoningEffortDefault
	}
	return models, p.fallback.effort
}

// defaultModel returns the provider's default model, the best of
// [providerPolicy.defaultCandidates], and the effort written on default_model.
func (p *providerPolicy) defaultModel() (model string, effort llms.ReasoningEffort, ok bool) {
	models, effort := p.defaultCandidates()
	if len(models) == 0 {
		return "", llms.ReasoningEffortDefault, false
	}
	return models[0], effort, true
}

// effortPolicy returns the effort settings for a model selected on this
// provider. refEffort is the level written on the reference that selected the
// model; without one, the level of the first matching available_models entry
// applies.
func (p *providerPolicy) effortPolicy(refEffort llms.ReasoningEffort, model string) llms.EffortPolicy {
	policy := llms.EffortPolicy{
		Default: refEffort,
		Max:     p.maxEffort,
	}
	if policy.Default == llms.ReasoningEffortDefault {
		if entry, ok := p.allowEntry(model); ok {
			policy.Default = entry.effort
		}
	}
	return policy
}

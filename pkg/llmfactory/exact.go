package llmfactory

import (
	"context"
	"slices"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
)

// ErrModelNotFound indicates that an exact model is absent or not allowed.
var ErrModelNotFound = errors.New("model not found")

// exactCachePrefix separates exact-resolution cache entries from legacy
// preferred-name cache entries that share the same map.
const exactCachePrefix = "exact"

// ExactResolver is an optional factory extension that never falls back.
// Provider must identify one configured provider; model names may contain slashes.
//
//go:generate mockgen -source=exact.go -destination=../../mocks/mockllmfactory/exact_mock.gen.go -package mockllmfactory
type ExactResolver interface {
	// GetModelExact resolves one provider/model under current organization filtering.
	GetModelExact(ctx context.Context, provider, model, orgID string) (llms.Model, error)
}

// ResolveExact requires the optional exact-resolution contract, never GetModel fallback.
func ResolveExact(ctx context.Context, f Factory, provider, model, orgID string) (llms.Model, error) {
	resolver, ok := f.(ExactResolver)
	if !ok {
		return nil, errors.New("factory does not support exact model resolution")
	}
	return resolver.GetModelExact(ctx, provider, model, orgID)
}

// GetModelExact resolves a configured provider and model without default fallback.
// Organization filtering runs on every call before the cache is consulted, so
// constructed instances are shared across organizations. When the model list
// of the provider is read, a model the provider no longer serves is not found:
// exact resolution never substitutes another model. Instances are cached per
// provider, model and effort settings, so a model whose reported effort levels
// change gets a new one.
func (f *factory) GetModelExact(ctx context.Context, provider, model, orgID string) (llms.Model, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.WithMessage(err, "resolve exact model")
	}
	if provider == "" || model == "" || strings.ContainsAny(provider+model+orgID, "\x00") {
		return nil, errors.WithStack(ErrModelNotFound)
	}
	path := provider + "/" + model
	if !f.isModelAllowed(ctx, orgID, "", path) || !f.isModelAllowed(ctx, orgID, "", model) {
		return nil, errors.WithStack(ErrModelNotFound)
	}
	policy, err := f.exactProvider(provider, model)
	if err != nil {
		return nil, err
	}
	// The list is read without the factory lock.
	list := policy.models(ctx)
	if list != nil && !list.serves(model) {
		return nil, errors.WithStack(ErrModelNotFound)
	}
	effort := policy.effortPolicy(llms.ReasoningEffortDefault, model, list)
	key := strings.Join([]string{
		exactCachePrefix,
		provider,
		model,
		effortKey(effort),
	}, cacheKeySeparator)
	f.lock.Lock()
	cached, ok := f.byName[key]
	f.lock.Unlock()
	if ok {
		return cached, nil
	}
	// Construct outside the lock: SDK construction may load credentials.
	// The copy lists the model by name, so the constructor selects it even
	// when only the default model or a pattern admitted it.
	cfg := *policy.cfg
	cfg.AvailableModels = slices.Clone(policy.cfg.AvailableModels)
	if !slices.Contains(cfg.AvailableModels, model) {
		cfg.AvailableModels = append(cfg.AvailableModels, model)
	}
	m, err := NewLLM(&cfg, []string{model}, f.optionsFor(effort))
	if err != nil {
		return nil, errors.WithMessage(err, "construct exact model")
	}
	if m == nil || m.GetName() != model {
		return nil, errors.New("factory returned a different model")
	}
	f.lock.Lock()
	defer f.lock.Unlock()
	if existing, ok := f.byName[key]; ok {
		return existing, nil
	}
	f.byName[key] = m
	return m, nil
}

// exactProvider finds the single provider named provider and checks that its
// model policy admits model: an available_models entry or the default model
// must match it, and no disabled_models entry may. Duplicate provider names
// are a configuration error.
func (f *factory) exactProvider(provider, model string) (*providerPolicy, error) {
	f.lock.Lock()
	defer f.lock.Unlock()
	var selected *providerPolicy
	for _, policy := range f.policies {
		if policy.cfg.Name != provider {
			continue
		}
		if selected != nil {
			return nil, errors.Errorf("ambiguous provider configuration %q", provider)
		}
		selected = policy
	}
	if selected == nil || selected.denied(model) {
		return nil, errors.WithStack(ErrModelNotFound)
	}
	if _, listed := selected.allowEntry(model); !listed {
		if name, _, ok := selected.defaultModel(); !ok || name != model {
			return nil, errors.WithStack(ErrModelNotFound)
		}
	}
	return selected, nil
}

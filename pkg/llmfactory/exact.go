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
const exactCachePrefix = "exact\x00"

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
// constructed instances are shared across organizations.
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
	selected, err := f.exactProvider(provider, model)
	if err != nil {
		return nil, err
	}
	key := exactCachePrefix + provider + "\x00" + model
	f.lock.Lock()
	cached, ok := f.byName[key]
	f.lock.Unlock()
	if ok {
		return cached, nil
	}
	// Construct outside the lock: SDK construction may load credentials.
	cfg := *selected
	cfg.AvailableModels = slices.Clone(selected.AvailableModels)
	if !slices.Contains(cfg.AvailableModels, model) {
		cfg.AvailableModels = append(cfg.AvailableModels, model)
	}
	m, err := NewLLM(&cfg, []string{model}, f.options)
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

// exactProvider finds the single provider configuration named provider that
// offers model. Duplicate provider names are a configuration error.
func (f *factory) exactProvider(provider, model string) (*ProviderConfig, error) {
	f.lock.Lock()
	defer f.lock.Unlock()
	var selected *ProviderConfig
	for _, cfg := range f.cfg.Providers {
		if cfg.Name != provider {
			continue
		}
		if selected != nil {
			return nil, errors.Errorf("ambiguous provider configuration %q", provider)
		}
		selected = cfg
	}
	if selected == nil || (!slices.Contains(selected.AvailableModels, model) && selected.DefaultModel != model) {
		return nil, errors.WithStack(ErrModelNotFound)
	}
	return selected, nil
}

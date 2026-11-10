package openai

import (
	"context"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
)

var _ llms.ModelLister = (*LLM)(nil)

// ListModels implements [llms.ModelLister]. It returns the models the account
// can call on OPENAI and OPENROUTER, in the provider's order.
//
// OpenAI reports the announced shutdown date of a model; OpenRouter reports an
// expiration date, a display name and the reasoning effort levels. A model
// with such a date is [llms.ModelLifecycleDeprecated] and carries it in
// RetiresAt.
//
// The other provider types served by this package do not advertise
// [llms.CapabilityModels] and return [llms.ErrModelListNotSupported]. On Azure
// the names a request can use are deployments, which only the management API
// lists.
func (o *LLM) ListModels(ctx context.Context) ([]llms.ModelInfo, error) {
	if !o.providerType.Supports(llms.CapabilityModels) {
		return nil, errors.Wrapf(llms.ErrModelListNotSupported, "openai: provider %s", o.providerType)
	}
	models, err := o.client.ListModels(ctx)
	if err != nil {
		return nil, errors.WithMessagef(err, "openai: provider %s", o.providerType)
	}
	return models, nil
}

// NewModelLister returns a client that lists the models of an OPENAI or
// OPENROUTER account. It takes the options of [New]; no model name is needed.
// For any other provider type it returns [llms.ErrModelListNotSupported].
func NewModelLister(opts ...Option) (llms.ModelLister, error) {
	popts, client, err := newClient(opts...)
	if popts != nil && !popts.provider.Supports(llms.CapabilityModels) {
		return nil, errors.Wrapf(llms.ErrModelListNotSupported, "openai: provider %s", popts.provider)
	}
	if err != nil {
		return nil, err
	}
	return newLLM(popts, client), nil
}

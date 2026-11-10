package llmfactory

import (
	"context"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
)

// ResolutionReason says how a model reference selected a model.
type ResolutionReason string

const (
	// ReasonExact means the reference named the model.
	ReasonExact ResolutionReason = "exact"
	// ReasonPattern means the model is the best match of a pattern.
	ReasonPattern ResolutionReason = "pattern"
	// ReasonFamily means the reference named a model that cannot be used,
	// because the provider no longer serves it or the configuration does not
	// allow it, and the model is the best usable one of the same family.
	ReasonFamily ResolutionReason = "family"
)

// Resolution describes the model a resolution selects.
type Resolution struct {
	// Provider is the name of the configured provider that serves the model.
	Provider string
	// Reference is the configuration entry that selected the model, as
	// written: a preferred model, an assistant_models entry or default_model.
	Reference string
	// Model is the ID of the selected model.
	Model string
	// Effort is the reasoning effort policy the client of the model gets.
	Effort llms.EffortPolicy
	// Reason says how Reference selected Model.
	Reason ResolutionReason
	// Default is set when no preferred reference selected a model and the
	// default model of the provider was used.
	Default bool
	// Verified is set when the model list of the provider confirmed that it
	// serves Model. It is false without discovery, and until a list is read.
	Verified bool
	// Lifecycle is the lifecycle stage the provider reports for Model.
	Lifecycle llms.ModelLifecycle
	// RetiresAt is the announced end of service of Model, or zero.
	RetiresAt time.Time
}

// Catalog is an optional factory extension for the providers whose model
// list is read: it shows what a resolution selects, and reads the lists on
// demand. Assert it on a [Factory], or use [Resolve] and [RefreshModels].
//
//go:generate mockgen -source=resolution.go -destination=../../mocks/mockllmfactory/catalog_mock.gen.go -package mockllmfactory
type Catalog interface {
	// Resolve reports the model GetModel selects for the options, without
	// building a client. It answers what a pattern or a retired model means
	// right now.
	Resolve(ctx context.Context, opts ModelOptions) (*Resolution, error)
	// Refresh reads the model list of every provider with discovery now and
	// waits for them. A list that cannot be read stays as it was; the errors
	// are returned joined.
	Refresh(ctx context.Context) error
}

// Resolve reports the model the factory selects for the options, without
// building a client. It requires a factory that implements [Catalog].
func Resolve(ctx context.Context, f Factory, opts ModelOptions) (*Resolution, error) {
	catalog, ok := f.(Catalog)
	if !ok {
		return nil, errors.New("factory does not support model resolution")
	}
	return catalog.Resolve(ctx, opts)
}

// RefreshModels reads the model list of every provider with discovery now, to
// warm a factory at start-up or to pick up a provider change at once. It
// requires a factory that implements [Catalog].
func RefreshModels(ctx context.Context, f Factory) error {
	catalog, ok := f.(Catalog)
	if !ok {
		return errors.New("factory does not support model discovery")
	}
	return catalog.Refresh(ctx)
}

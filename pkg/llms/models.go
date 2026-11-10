package llms

import (
	"context"
	"time"

	"github.com/cockroachdb/errors"
)

// ErrModelListNotSupported is returned by [ModelLister.ListModels], and by the
// NewModelLister constructors, when the client talks to a provider that has no
// model list: one that does not advertise [CapabilityModels]. Test for it with
// errors.Is.
var ErrModelListNotSupported = errors.New("provider does not list models")

// ModelLifecycle is the stage of a model in its provider's catalog. Providers
// add stages over time, so a value outside the constants below is possible.
type ModelLifecycle string

const (
	// ModelLifecycleUnknown means the provider does not report a lifecycle
	// for the model.
	ModelLifecycleUnknown ModelLifecycle = ""
	// ModelLifecycleActive is a model in normal service, with no retirement
	// announced.
	ModelLifecycleActive ModelLifecycle = "active"
	// ModelLifecycleDeprecated is a model that still answers requests but is
	// scheduled for retirement.
	ModelLifecycleDeprecated ModelLifecycle = "deprecated"
	// ModelLifecycleRetired is a model that no longer answers requests.
	ModelLifecycleRetired ModelLifecycle = "retired"
)

// ModelInfo is one entry of a provider's model list, reduced to what every
// provider can be asked for. A field the provider does not report is left at
// its zero value.
type ModelInfo struct {
	// ID is the identifier to name the model with in a request.
	ID string
	// DisplayName is the human-readable name of the model.
	DisplayName string
	// Family is the model family as the provider names it, such as "opus".
	// It is empty when the provider does not report one; derive it from the
	// ID with [ParseModelName] in that case.
	Family string
	// Created is the release time of the model.
	Created time.Time
	// Lifecycle is the stage of the model in the provider's catalog.
	Lifecycle ModelLifecycle
	// RetiresAt is the announced time at which the model stops answering
	// requests. It is zero when none is announced.
	RetiresAt time.Time
	// Efforts are the reasoning effort levels the provider reports for the
	// model. The set is empty when the provider reports none, which does not
	// mean that the model has no reasoning control.
	Efforts EffortSet
	// DefaultEffort is the level the provider reports the model applies to a
	// request that names none. It is ReasoningEffortDefault when the provider
	// does not report one.
	DefaultEffort ReasoningEffort
}

// ModelLister is implemented by the provider clients that can list the models
// an account may call. Providers that advertise [CapabilityModels] implement
// it; assert it on a [Model], or build a lister that needs no model name with
// the NewModelLister function of the provider package.
type ModelLister interface {
	// ListModels returns every model the provider lists for the account, in
	// the provider's order. The list is not limited to chat models: it can
	// hold embedding, image or audio models as well.
	ListModels(ctx context.Context) ([]ModelInfo, error)
}

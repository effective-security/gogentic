package googleai

import (
	"context"
	"slices"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
)

const (
	// modelNamePrefix starts the resource name of a model in the list; a
	// request names the model without it.
	modelNamePrefix = "models/"
	// actionGenerateContent is the action a model must support to be called
	// through GenerateContent.
	actionGenerateContent = "generateContent"
)

var _ llms.ModelLister = (*GoogleAI)(nil)

// NewModelLister returns a client that lists the models of a Google AI
// account. It takes the options of [New]; the default model is not used.
func NewModelLister(ctx context.Context, opts ...Option) (llms.ModelLister, error) {
	client, err := New(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// ListModels implements [llms.ModelLister]. It returns the models that can
// generate content, in the provider's order, named as a request names them:
// without the "models/" prefix. Embedding and live-only models are left out.
//
// The API reports neither a lifecycle nor reasoning levels, so those fields
// are empty; a model that is withdrawn simply leaves the list.
func (g *GoogleAI) ListModels(ctx context.Context) ([]llms.ModelInfo, error) {
	var models []llms.ModelInfo
	for model, err := range g.client.Models.All(ctx) {
		if err != nil {
			return nil, errors.Wrap(err, "googleai: failed to list models")
		}
		if model == nil || !slices.Contains(model.SupportedActions, actionGenerateContent) {
			continue
		}
		models = append(models, llms.ModelInfo{
			ID:          strings.TrimPrefix(model.Name, modelNamePrefix),
			DisplayName: model.DisplayName,
		})
	}
	return models, nil
}

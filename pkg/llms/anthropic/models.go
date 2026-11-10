package anthropic

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
)

// modelListPageSize is the number of models asked for per request. The API
// allows up to 1000; the list is a few dozen models long.
const modelListPageSize = 100

// modelLifecycles are the lifecycle stages asked for. The API leaves retired
// models out unless they are named, and a retired model is what tells a
// caller that a configured ID is gone for good rather than mistyped.
var modelLifecycles = []string{
	string(anthropic.ModelInfoLifecycleActive),
	string(anthropic.ModelInfoLifecycleDeprecated),
	string(anthropic.ModelInfoLifecycleRetired),
}

var (
	_ llms.ModelLister = (*LLM)(nil)
	_ llms.ModelLister = (*modelLister)(nil)
)

// modelLister lists the models of an Anthropic account. Unlike [LLM] it is
// not bound to a model.
type modelLister struct {
	client *anthropic.Client
}

// NewModelLister returns a client that lists the models of an Anthropic
// account. It takes the options of [New], and needs a token but no model
// name. The Bedrock endpoint has no model list: with [WithAWSConfig] it
// returns [llms.ErrModelListNotSupported].
func NewModelLister(opts ...Option) (llms.ModelLister, error) {
	options := newOptions(opts...)
	if options.AWSCfg != nil {
		return nil, errors.Wrap(llms.ErrModelListNotSupported, "anthropic: bedrock")
	}
	if len(options.Token) == 0 {
		return nil, errors.WithStack(ErrMissingToken)
	}
	client, err := newClient(options)
	if err != nil {
		return nil, errors.Wrap(err, "anthropic: failed to create client")
	}
	return &modelLister{
		client: client,
	}, nil
}

// ListModels implements [llms.ModelLister].
func (l *modelLister) ListModels(ctx context.Context) ([]llms.ModelInfo, error) {
	return listModels(ctx, l.client)
}

// ListModels implements [llms.ModelLister]. It returns the models of the
// account in every lifecycle stage, newest first, each with the model line as
// its Family and the effort levels the API reports for it.
//
// A client built with [NewBedrock] returns [llms.ErrModelListNotSupported]:
// the Bedrock runtime endpoint has no model list.
func (o *LLM) ListModels(ctx context.Context) ([]llms.ModelInfo, error) {
	if o.Options.AWSCfg != nil {
		return nil, errors.Wrap(llms.ErrModelListNotSupported, "anthropic: bedrock")
	}
	return listModels(ctx, o.Client)
}

// listModels reads every page of the model list.
func listModels(ctx context.Context, client *anthropic.Client) ([]llms.ModelInfo, error) {
	pager := client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{
		Limit:     anthropic.Int(modelListPageSize),
		Lifecycle: modelLifecycles,
	})
	var models []llms.ModelInfo
	for pager.Next() {
		models = append(models, toModelInfo(pager.Current()))
	}
	if err := pager.Err(); err != nil {
		return nil, errors.Wrap(err, "anthropic: failed to list models")
	}
	return models, nil
}

// toModelInfo converts one entry of the model list.
func toModelInfo(entry anthropic.ModelInfo) llms.ModelInfo {
	return llms.ModelInfo{
		ID:          entry.ID,
		DisplayName: entry.DisplayName,
		Family:      string(entry.Line),
		Created:     entry.CreatedAt,
		Lifecycle:   llms.ModelLifecycle(entry.Lifecycle),
		RetiresAt:   entry.RetiresAt,
		Efforts:     effortSetOf(entry.Capabilities.Effort),
	}
}

// effortSetOf converts the effort capability of a model into the portable
// set. The API has no "none" level; the set holds the levels it names.
func effortSetOf(effort anthropic.EffortCapability) llms.EffortSet {
	if !effort.Supported {
		return llms.NewEffortSet()
	}
	support := []struct {
		level     llms.ReasoningEffort
		supported bool
	}{
		{llms.ReasoningEffortLow, effort.Low.Supported},
		{llms.ReasoningEffortMedium, effort.Medium.Supported},
		{llms.ReasoningEffortHigh, effort.High.Supported},
		{llms.ReasoningEffortXHigh, effort.Xhigh.Supported},
		{llms.ReasoningEffortMax, effort.Max.Supported},
	}
	levels := make([]llms.ReasoningEffort, 0, len(support))
	for _, s := range support {
		if s.supported {
			levels = append(levels, s.level)
		}
	}
	return llms.NewEffortSet(levels...)
}

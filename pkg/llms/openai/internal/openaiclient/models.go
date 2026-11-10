package openaiclient

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/respjson"
)

// Fields of the OpenRouter model list that the OpenAI list does not have.
const (
	// fieldName is the display name of a model.
	fieldName = "name"
	// fieldExpirationDate is the date a model is removed, or null.
	fieldExpirationDate = "expiration_date"
	// fieldReasoning describes the reasoning control of a model.
	fieldReasoning = "reasoning"
	// fieldTotalCount is the number of models the account can list.
	fieldTotalCount = "total_count"
	// fieldLinks holds the link to the next page of the list.
	fieldLinks = "links"
)

// reasoningInfo is the reasoning field of an OpenRouter model.
type reasoningInfo struct {
	SupportedEfforts []string `json:"supported_efforts"`
	DefaultEffort    string   `json:"default_effort"`
}

// pageLinks is the links field of an OpenRouter model list.
type pageLinks struct {
	Next *string `json:"next"`
}

// ListModels returns the models the provider lists for the account, in the
// provider's order. It reads the fields of the OpenAI list, and the extra ones
// of the OpenRouter list when they are present:
//
//	shutdown_date, expiration_date   a date makes the model deprecated,
//	                                 null makes it active
//	name                             the display name
//	reasoning.supported_efforts      the reasoning effort levels
//	reasoning.default_effort         the level applied when none is named
//
// A reasoning level without a portable name, such as "minimal", is left out.
//
// Neither API pages its list today. A list that says it holds more models
// than it returned is an error, because a truncated list would make models
// look retired.
func (c *Client) ListModels(ctx context.Context) ([]llms.ModelInfo, error) {
	page, err := c.sdkClient().Models.List(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list models")
	}
	if page == nil {
		return nil, nil
	}
	count := len(page.Data)
	if err := checkComplete(page.JSON.ExtraFields, count); err != nil {
		return nil, err
	}

	models := make([]llms.ModelInfo, 0, count)
	for _, entry := range page.Data {
		info, err := toModelInfo(entry)
		if err != nil {
			return nil, err
		}
		models = append(models, info)
	}
	return models, nil
}

// checkComplete reports an error when the list response names a next page or
// a total above the number of models it returned.
func checkComplete(extras map[string]respjson.Field, count int) error {
	var total int
	found, err := decodeField(extras, fieldTotalCount, &total)
	if err != nil {
		return err
	}
	if found && total > count {
		return errors.Errorf("model list is incomplete: got %d of %d models", count, total)
	}

	var links pageLinks
	if _, err := decodeField(extras, fieldLinks, &links); err != nil {
		return err
	}
	if links.Next != nil && *links.Next != "" {
		return errors.Errorf("model list is incomplete: got %d models and a next page", count)
	}
	return nil
}

// toModelInfo converts one entry of the model list.
func toModelInfo(entry openaisdk.Model) (llms.ModelInfo, error) {
	info := llms.ModelInfo{
		ID: entry.ID,
	}
	if entry.Created > 0 {
		info.Created = time.Unix(entry.Created, 0).UTC()
	}

	// OpenAI reports shutdown_date on every model, as a date or as null.
	shutdown := entry.JSON.ShutdownDate
	switch {
	case shutdown.Valid():
		info.Lifecycle = llms.ModelLifecycleDeprecated
		info.RetiresAt = entry.ShutdownDate
	case shutdown.Raw() == respjson.Null:
		info.Lifecycle = llms.ModelLifecycleActive
	}

	extras := entry.JSON.ExtraFields
	if _, err := decodeField(extras, fieldName, &info.DisplayName); err != nil {
		return info, errors.WithMessagef(err, "model %s", entry.ID)
	}

	var expiration *string
	found, err := decodeField(extras, fieldExpirationDate, &expiration)
	if err != nil {
		return info, errors.WithMessagef(err, "model %s", entry.ID)
	}
	if found && expiration == nil {
		info.Lifecycle = llms.ModelLifecycleActive
	}
	if expiration != nil {
		retiresAt, err := time.Parse(time.DateOnly, *expiration)
		if err != nil {
			return info, errors.Wrapf(err, "model %s: invalid %s", entry.ID, fieldExpirationDate)
		}
		info.Lifecycle = llms.ModelLifecycleDeprecated
		info.RetiresAt = retiresAt
	}

	var reasoning reasoningInfo
	if _, err := decodeField(extras, fieldReasoning, &reasoning); err != nil {
		return info, errors.WithMessagef(err, "model %s", entry.ID)
	}
	info.Efforts = effortSet(reasoning.SupportedEfforts)
	info.DefaultEffort = effortLevel(reasoning.DefaultEffort)
	return info, nil
}

// decodeField decodes an extra field of a response into out. found is false
// when the response did not carry the field; a null field is found and leaves
// out as it was.
func decodeField(extras map[string]respjson.Field, name string, out any) (found bool, err error) {
	field, ok := extras[name]
	if !ok {
		return false, nil
	}
	raw := field.Raw()
	if raw == respjson.Omitted {
		return false, nil
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		return true, errors.Wrapf(err, "invalid %s", name)
	}
	return true, nil
}

// effortSet converts reasoning level names into the portable set. A name this
// module does not know is left out.
func effortSet(names []string) llms.EffortSet {
	levels := make([]llms.ReasoningEffort, 0, len(names))
	for _, name := range names {
		levels = append(levels, effortLevel(name))
	}
	return llms.NewEffortSet(levels...)
}

// effortLevel converts a reasoning level name into the portable level. It
// returns llms.ReasoningEffortDefault for an empty name and for a name without
// a portable equivalent: "minimal", or a level the provider added after this
// module was written.
func effortLevel(name string) llms.ReasoningEffort {
	level, err := llms.ParseReasoningEffort(name)
	if err != nil {
		return llms.ReasoningEffortDefault
	}
	return level
}

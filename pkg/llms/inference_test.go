package llms_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	budgetMinimal = 1024
	budgetLow     = 2048
	budgetMedium  = 8192
	budgetHigh    = 24576

	testInferenceModel = "gpt-test"
	testParam          = "temperature"
	testOperation      = "call provider"
)

func TestReasoningBudget(t *testing.T) {
	tests := []struct {
		effort llms.Effort
		budget int
		ok     bool
	}{
		{effort: llms.EffortMinimal, budget: budgetMinimal, ok: true},
		{effort: llms.EffortLow, budget: budgetLow, ok: true},
		{effort: llms.EffortMedium, budget: budgetMedium, ok: true},
		{effort: llms.EffortHigh, budget: budgetHigh, ok: true},
		{effort: "", budget: 0, ok: false},
		{effort: "extreme", budget: 0, ok: false},
	}
	for _, tt := range tests {
		t.Run(string(tt.effort), func(t *testing.T) {
			budget, ok := llms.ReasoningBudget(tt.effort)
			assert.Equal(t, tt.budget, budget)
			assert.Equal(t, tt.ok, ok)
		})
	}
}

func TestEffortForBudget(t *testing.T) {
	tests := []struct {
		budget int
		effort llms.Effort
	}{
		{budget: 0, effort: llms.EffortMinimal},
		{budget: budgetMinimal, effort: llms.EffortMinimal},
		{budget: budgetMinimal + 1, effort: llms.EffortLow},
		{budget: budgetLow, effort: llms.EffortLow},
		{budget: budgetLow + 1, effort: llms.EffortMedium},
		{budget: budgetMedium, effort: llms.EffortMedium},
		{budget: budgetMedium + 1, effort: llms.EffortHigh},
		{budget: budgetHigh * 2, effort: llms.EffortHigh},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.effort, llms.EffortForBudget(tt.budget), "budget %d", tt.budget)
	}
}

func TestInferenceReasoning_EffectiveEffort(t *testing.T) {
	var nilReasoning *llms.InferenceReasoning
	effort, ok := nilReasoning.EffectiveEffort()
	assert.Equal(t, llms.Effort(""), effort)
	assert.False(t, ok)

	effort, ok = (&llms.InferenceReasoning{}).EffectiveEffort()
	assert.Equal(t, llms.Effort(""), effort)
	assert.False(t, ok)

	// An explicit effort wins over the budget.
	budget := budgetHigh
	effort, ok = (&llms.InferenceReasoning{
		Effort:       llms.EffortLow,
		BudgetTokens: &budget,
	}).EffectiveEffort()
	assert.Equal(t, llms.EffortLow, effort)
	assert.True(t, ok)

	// Only a budget: the effort is derived from it.
	effort, ok = (&llms.InferenceReasoning{
		BudgetTokens: &budget,
	}).EffectiveEffort()
	assert.Equal(t, llms.EffortHigh, effort)
	assert.True(t, ok)
}

func newFullInferenceRequest() llms.InferenceRequest {
	strict := true
	parallel := false
	temperature := 0.2
	topP := 0.9
	maxTokens := 512
	seed := int64(42)
	budget := budgetMedium
	return llms.InferenceRequest{
		Model: testInferenceModel,
		Messages: []llms.InferenceMessage{
			{
				Role: llms.InferenceRoleUser,
				Content: []llms.InferenceBlock{
					{
						Type: llms.BlockText,
						Text: "hello",
					},
				},
			},
			{
				Role: llms.InferenceRoleAssistant,
				Content: []llms.InferenceBlock{
					{
						Type:   llms.BlockReasoning,
						Opaque: json.RawMessage(`{"state":1}`),
						Source: "target-a",
					},
					{
						Type:      llms.BlockFunctionCall,
						ID:        "call-1",
						Name:      "lookup",
						Arguments: `{"q":"x"}`,
					},
				},
			},
		},
		Tools: []llms.InferenceTool{
			{
				Name:        "lookup",
				Description: "looks things up",
				Parameters:  json.RawMessage(`{"type":"object"}`),
				Strict:      &strict,
			},
		},
		ToolChoice: &llms.InferenceToolChoice{
			Mode: llms.ToolChoiceFunction,
			Name: "lookup",
		},
		ParallelToolCalls: &parallel,
		Temperature:       &temperature,
		TopP:              &topP,
		MaxTokens:         &maxTokens,
		Stop:              []string{"END"},
		Seed:              &seed,
		Format: &llms.InferenceFormat{
			Type:   llms.FormatJSONSchema,
			Name:   "answer",
			Schema: json.RawMessage(`{"type":"object"}`),
			Strict: true,
		},
		Reasoning: &llms.InferenceReasoning{
			Effort:       llms.EffortMedium,
			BudgetTokens: &budget,
		},
	}
}

func TestInferenceRequest_Clone_DeepCopy(t *testing.T) {
	orig := newFullInferenceRequest()
	want := newFullInferenceRequest()

	clone := orig.Clone()
	require.Equal(t, want, clone)

	// Mutate every slice and pointer reachable from the clone.
	clone.Messages[0].Content[0].Text = "changed"
	clone.Messages[1].Content[0].Opaque[0] = '['
	clone.Tools[0].Parameters[0] = '['
	*clone.Tools[0].Strict = false
	clone.ToolChoice.Name = "other"
	*clone.ParallelToolCalls = true
	*clone.Temperature = 1
	*clone.TopP = 0
	*clone.MaxTokens = 1
	*clone.Seed = 7
	clone.Stop[0] = "STOP"
	clone.Format.Schema[0] = '['
	clone.Format.Name = "other"
	clone.Reasoning.Effort = llms.EffortHigh
	*clone.Reasoning.BudgetTokens = 1

	assert.Equal(t, want, orig, "mutating the clone must not affect the original")
}

func TestInferenceRequest_Clone_Empty(t *testing.T) {
	clone := llms.InferenceRequest{
		Model: testInferenceModel,
	}.Clone()

	assert.Equal(t, testInferenceModel, clone.Model)
	// Messages is always allocated, even when the source had none.
	assert.NotNil(t, clone.Messages)
	assert.Empty(t, clone.Messages)
	assert.Nil(t, clone.Tools)
	assert.Nil(t, clone.ToolChoice)
	assert.Nil(t, clone.ParallelToolCalls)
	assert.Nil(t, clone.Temperature)
	assert.Nil(t, clone.TopP)
	assert.Nil(t, clone.MaxTokens)
	assert.Nil(t, clone.Seed)
	assert.Nil(t, clone.Stop)
	assert.Nil(t, clone.Format)
	assert.Nil(t, clone.Reasoning)
}

func TestInferenceResponse_Clone(t *testing.T) {
	orig := llms.InferenceResponse{
		Content: []llms.InferenceBlock{
			{
				Type:   llms.BlockReasoning,
				Opaque: json.RawMessage(`{"s":1}`),
			},
			{
				Type: llms.BlockText,
				Text: "answer",
			},
		},
		FinishReason: llms.FinishStop,
		Usage: llms.Usage{
			InputTokens:  3,
			OutputTokens: 4,
		},
		UsageKnown: true,
		UpstreamID: "resp-1",
	}

	clone := orig.Clone()
	require.Equal(t, orig, clone)

	clone.Content[0].Opaque[0] = '['
	clone.Content[1].Text = "changed"
	assert.JSONEq(t, `{"s":1}`, string(orig.Content[0].Opaque))
	assert.Equal(t, "answer", orig.Content[1].Text)

	empty := llms.InferenceResponse{}.Clone()
	assert.Nil(t, empty.Content)
}

func TestUnsupportedInference(t *testing.T) {
	err := llms.UnsupportedInference(testParam)
	require.Error(t, err)
	assert.EqualError(t, err, "unsupported inference parameter: "+testParam)

	var ie *llms.InferenceError
	require.True(t, errors.As(err, &ie))
	assert.Equal(t, http.StatusBadRequest, ie.Status)
	assert.Equal(t, testParam, ie.Param)
	assert.Equal(t, "unsupported inference parameter: "+testParam, ie.Error())
}

func TestInferenceFailure(t *testing.T) {
	cause := errors.New("upstream unavailable")
	err := llms.InferenceFailure(cause, http.StatusServiceUnavailable, testOperation)
	require.Error(t, err)
	assert.EqualError(t, err, testOperation+": upstream unavailable")
	assert.True(t, errors.Is(err, cause), "the provider failure must stay reachable through Unwrap")

	var ie *llms.InferenceError
	require.True(t, errors.As(err, &ie))
	assert.Equal(t, http.StatusServiceUnavailable, ie.Status)
	assert.Empty(t, ie.Param)
	assert.True(t, errors.Is(ie.Unwrap(), cause))
}

package llms

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/cockroachdb/errors"
)

// InferenceModel is an optional lossless, single-turn text inference contract
// implemented by providers next to the legacy Model interface. Implementations
// must honor option presence, return ordered output blocks, make exactly one
// upstream attempt, and never execute tools.
//
//go:generate mockgen -source=inference.go -destination=../../mocks/mockllms/inference_mock.gen.go -package mockllms
type InferenceModel interface {
	// ValidateInference rejects controls the connector cannot translate faithfully.
	// It must return UnsupportedInference for each such control.
	ValidateInference(InferenceRequest) error
	// Infer generates one response without executing tools or retrying.
	Infer(context.Context, InferenceRequest) (*InferenceResponse, error)
}

// InferenceRole identifies who authored a message.
type InferenceRole string

// BlockType identifies the kind of content in an InferenceBlock.
type BlockType string

// FinishReason is the normalized reason a generation ended.
type FinishReason string

// FormatType selects plain text, a JSON object, or a schema-constrained JSON output.
type FormatType string

// ToolChoiceMode controls whether and which tools the model may call.
type ToolChoiceMode string

// Effort is a provider-independent request for how much the model
// should reason before answering. Connectors map it to a native control or a
// token budget.
type Effort string

// Portable inference vocabulary shared by routers, codecs and provider adapters.
const (
	// InferenceRoleSystem identifies leading system instructions.
	InferenceRoleSystem InferenceRole = "system"
	// InferenceRoleDeveloper identifies leading developer instructions.
	InferenceRoleDeveloper InferenceRole = "developer"
	// InferenceRoleUser identifies user content.
	InferenceRoleUser InferenceRole = "user"
	// InferenceRoleAssistant identifies prior model content.
	InferenceRoleAssistant InferenceRole = "assistant"
	// InferenceRoleTool identifies application-supplied function results.
	InferenceRoleTool InferenceRole = "tool"

	// BlockText is plain text authored by the role.
	BlockText BlockType = "text"
	// BlockFunctionCall is an assistant request to run a function.
	BlockFunctionCall BlockType = "function_call"
	// BlockFunctionOutput is the application's result for one function call.
	BlockFunctionOutput BlockType = "function_call_output"
	// BlockRefusal is an explicit model refusal.
	BlockRefusal BlockType = "refusal"
	// BlockReasoning carries a reasoning summary and opaque provider state.
	BlockReasoning BlockType = "reasoning"

	// FormatText requests unconstrained text.
	FormatText FormatType = "text"
	// FormatJSONObject requests any valid JSON object.
	FormatJSONObject FormatType = "json_object"
	// FormatJSONSchema requests JSON that conforms to a caller schema.
	FormatJSONSchema FormatType = "json_schema"

	// ToolChoiceAuto lets the model decide whether to call tools.
	ToolChoiceAuto ToolChoiceMode = "auto"
	// ToolChoiceNone forbids tool calls.
	ToolChoiceNone ToolChoiceMode = "none"
	// ToolChoiceRequired requires at least one tool call.
	ToolChoiceRequired ToolChoiceMode = "required"
	// ToolChoiceFunction requires a call to the named function.
	ToolChoiceFunction ToolChoiceMode = "function"

	// FinishStop denotes a completed generation.
	FinishStop FinishReason = "stop"
	// FinishLength denotes output truncated by the token limit.
	FinishLength FinishReason = "length"
	// FinishToolCalls denotes pending tool invocations.
	FinishToolCalls FinishReason = "tool_calls"
	// FinishContentFilter denotes content blocked or refused by the provider.
	FinishContentFilter FinishReason = "content_filter"

	// EffortMinimal requests the least reasoning a model allows.
	EffortMinimal Effort = "minimal"
	// EffortLow requests light reasoning.
	EffortLow Effort = "low"
	// EffortMedium requests moderate reasoning.
	EffortMedium Effort = "medium"
	// EffortHigh requests extensive reasoning.
	EffortHigh Effort = "high"

	// InferenceFunction is the tool type accepted by every dialect.
	InferenceFunction = "function"
)

// ReasoningBudget maps an effort level to a token budget for providers that
// only expose a numeric budget. Providers with a native effort control use that.
func ReasoningBudget(effort Effort) (int, bool) {
	switch effort {
	case EffortMinimal:
		return reasoningBudgetMinimal, true
	case EffortLow:
		return reasoningBudgetLow, true
	case EffortMedium:
		return reasoningBudgetMedium, true
	case EffortHigh:
		return reasoningBudgetHigh, true
	}
	return 0, false
}

// EffortForBudget maps a token budget to the closest effort level for providers
// that only expose a discrete effort control.
func EffortForBudget(budget int) Effort {
	switch {
	case budget <= reasoningBudgetMinimal:
		return EffortMinimal
	case budget <= reasoningBudgetLow:
		return EffortLow
	case budget <= reasoningBudgetMedium:
		return EffortMedium
	}
	return EffortHigh
}

// EffectiveEffort returns the requested effort, or the effort derived from the
// budget when only a budget was supplied. ok is false when neither is present.
func (r *InferenceReasoning) EffectiveEffort() (Effort, bool) {
	if r == nil {
		return "", false
	}
	if r.Effort != "" {
		return r.Effort, true
	}
	if r.BudgetTokens != nil {
		return EffortForBudget(*r.BudgetTokens), true
	}
	return "", false
}

const (
	reasoningBudgetMinimal = 1024
	reasoningBudgetLow     = 2048
	reasoningBudgetMedium  = 8192
	reasoningBudgetHigh    = 24576
)

// InferenceRequest preserves caller-supplied generation controls and ordered
// history. Model is always the exact backend model, never a routing alias.
// Nil pointers mean "not supplied"; connectors omit the wire field.
type InferenceRequest struct {
	Model             string
	Messages          []InferenceMessage
	Tools             []InferenceTool
	ToolChoice        *InferenceToolChoice
	ParallelToolCalls *bool
	Temperature       *float64
	TopP              *float64
	MaxTokens         *int
	Stop              []string
	Seed              *int64
	Format            *InferenceFormat
	Reasoning         *InferenceReasoning
}

// InferenceMessage preserves one role and its ordered blocks.
type InferenceMessage struct {
	Role    InferenceRole
	Content []InferenceBlock
}

// InferenceBlock is one ordered unit of content. Field use by type:
//
//   - text, refusal: Text.
//   - function_call: ID, Name, Arguments (raw JSON object bytes, never re-encoded).
//   - function_call_output: ID, Text; Name is filled from the matching call.
//   - reasoning: Text (summary, may be empty), Opaque (provider state replayed
//     verbatim, may be empty) and Source (router target that produced Opaque).
type InferenceBlock struct {
	Type      BlockType
	Text      string
	ID        string
	Name      string
	Arguments string
	Opaque    json.RawMessage
	Source    string
}

// InferenceToolChoice selects a tool-calling mode; Name is set only for ToolChoiceFunction.
type InferenceToolChoice struct {
	Mode ToolChoiceMode
	Name string
}

// InferenceTool defines a function the application, not the model client, executes.
type InferenceTool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
	Strict      *bool
}

// InferenceFormat requests text, a JSON object, or a constrained JSON schema.
type InferenceFormat struct {
	Type   FormatType
	Name   string
	Schema json.RawMessage
	Strict bool
}

// InferenceReasoning requests reasoning behaviour. Effort is mapped by each
// connector; BudgetTokens, when set, is passed to providers with a token budget.
type InferenceReasoning struct {
	Effort       Effort
	BudgetTokens *int
}

// InferenceResponse contains one generation with ordered output and usage.
type InferenceResponse struct {
	Content      []InferenceBlock
	FinishReason FinishReason
	Usage        Usage
	UsageKnown   bool
	UpstreamID   string
}

// Clone returns a deep copy that shares no slices or pointers with r.
func (r InferenceRequest) Clone() InferenceRequest {
	out := r
	out.Messages = make([]InferenceMessage, len(r.Messages))
	for i, m := range r.Messages {
		out.Messages[i] = InferenceMessage{
			Role:    m.Role,
			Content: cloneBlocks(m.Content),
		}
	}
	if r.Tools != nil {
		out.Tools = make([]InferenceTool, len(r.Tools))
		for i, t := range r.Tools {
			out.Tools[i] = InferenceTool{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  slices.Clone(t.Parameters),
				Strict:      clonePtr(t.Strict),
			}
		}
	}
	if r.ToolChoice != nil {
		v := *r.ToolChoice
		out.ToolChoice = &v
	}
	out.ParallelToolCalls = clonePtr(r.ParallelToolCalls)
	out.Temperature = clonePtr(r.Temperature)
	out.TopP = clonePtr(r.TopP)
	out.MaxTokens = clonePtr(r.MaxTokens)
	out.Seed = clonePtr(r.Seed)
	out.Stop = slices.Clone(r.Stop)
	if r.Format != nil {
		f := *r.Format
		f.Schema = slices.Clone(r.Format.Schema)
		out.Format = &f
	}
	if r.Reasoning != nil {
		v := *r.Reasoning
		v.BudgetTokens = clonePtr(r.Reasoning.BudgetTokens)
		out.Reasoning = &v
	}
	return out
}

// Clone returns a deep copy of the response.
func (r InferenceResponse) Clone() InferenceResponse {
	out := r
	out.Content = cloneBlocks(r.Content)
	return out
}

func cloneBlocks(in []InferenceBlock) []InferenceBlock {
	if in == nil {
		return nil
	}
	out := make([]InferenceBlock, len(in))
	for i, b := range in {
		out[i] = b
		out[i].Opaque = slices.Clone(b.Opaque)
	}
	return out
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// InferenceError preserves diagnostics while exposing a safe classification.
// Status is the upstream HTTP status when known, or 400 for an unsupported
// control. Callers must not expose Cause text to untrusted clients.
type InferenceError struct {
	Status int
	Param  string
	Cause  error
}

// Error returns internal diagnostics.
func (e *InferenceError) Error() string { return e.Cause.Error() }

// Unwrap preserves the original provider failure for errors.Is and errors.As.
func (e *InferenceError) Unwrap() error { return e.Cause }

// UnsupportedInference reports a control the connector cannot translate.
func UnsupportedInference(param string) error {
	return errors.WithStack(&InferenceError{
		Status: http.StatusBadRequest,
		Param:  param,
		Cause:  errors.New("unsupported inference parameter: " + param),
	})
}

// InferenceFailure attaches the provider HTTP status to an external failure.
func InferenceFailure(err error, status int, operation string) error {
	return errors.WithStack(&InferenceError{
		Status: status,
		Cause:  errors.WithMessage(err, operation),
	})
}

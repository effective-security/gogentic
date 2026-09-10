package router

import (
	"encoding/json"
	"math"
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/schema"
)

var functionName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]{0,63}$`)

func validateRequest(req *Request) error {
	in := &req.Input
	messageCount := len(in.Messages)
	if len(req.Model) > maxModelNameBytes {
		return Invalid(paramModel, "model name exceeds %d bytes", maxModelNameBytes)
	}
	if messageCount == 0 || messageCount > maxMessages {
		return Invalid(paramMessages, "expected 1 to %d messages", maxMessages)
	}
	if len(in.Tools) > maxTools {
		return Invalid(paramTools, "at most %d tools are supported", maxTools)
	}
	if err := validateMetadata(req.Metadata); err != nil {
		return err
	}
	if err := validateSampling(in); err != nil {
		return err
	}
	names, err := validateTools(in)
	if err != nil {
		return err
	}
	if err := validateToolChoice(in, names); err != nil {
		return err
	}
	if err := validateFormat(in.Format); err != nil {
		return err
	}
	if err := validateReasoningConfig(in.Reasoning); err != nil {
		return err
	}
	return validateHistory(in)
}

func validateMetadata(metadata map[string]string) error {
	if len(metadata) > maxMetadataFields {
		return Invalid(paramMetadata, "at most %d metadata fields are supported", maxMetadataFields)
	}
	for k, v := range metadata {
		if k == "" || len(k) > maxMetadataKeyBytes || len(v) > maxMetadataValueBytes {
			return Invalid(paramMetadata, "metadata keys are 1-%d bytes and values at most %d bytes", maxMetadataKeyBytes, maxMetadataValueBytes)
		}
	}
	return nil
}

func validateSampling(in *llms.InferenceRequest) error {
	if in.Temperature != nil && (math.IsNaN(*in.Temperature) || *in.Temperature < 0 || *in.Temperature > maxTemperature) {
		return Invalid(paramTemperature, "temperature must be between 0 and %d", maxTemperature)
	}
	if in.TopP != nil && (math.IsNaN(*in.TopP) || *in.TopP < 0 || *in.TopP > 1) {
		return Invalid(paramTopP, "top_p must be between 0 and 1")
	}
	if in.MaxTokens != nil && (*in.MaxTokens < 1 || *in.MaxTokens > maxOutputTokens) {
		return Invalid(paramMaxTokens, "token limit must be between 1 and %d", maxOutputTokens)
	}
	if len(in.Stop) > maxStopSequences {
		return Invalid(paramStop, "at most %d stop strings are supported", maxStopSequences)
	}
	for _, s := range in.Stop {
		if len(s) == 0 || len(s) > maxStopBytes {
			return Invalid(paramStop, "stop strings are 1-%d bytes", maxStopBytes)
		}
	}
	return nil
}

func validateTools(in *llms.InferenceRequest) (map[string]bool, error) {
	names := map[string]bool{}
	for _, t := range in.Tools {
		if isReservedToolName(t.Name) || !functionName.MatchString(t.Name) || names[t.Name] {
			return nil, Invalid(paramTools, "invalid, reserved or duplicate tool name")
		}
		names[t.Name] = true
		if len(t.Description) > maxToolDescriptionBytes {
			return nil, Invalid(paramToolDesc, "description exceeds %d bytes", maxToolDescriptionBytes)
		}
		if len(t.Parameters) > maxSchemaBytes {
			return nil, Invalid(paramToolParameters, "schema exceeds %d bytes", maxSchemaBytes)
		}
		if err := schema.ValidatePortableSchema(t.Parameters, t.Strict != nil && *t.Strict); err != nil {
			return nil, errors.WithStack(&Error{
				Kind:    KindInvalidRequest,
				Param:   paramToolParameters,
				Message: "unsupported or invalid tool schema",
				Cause:   err,
			})
		}
	}
	return names, nil
}

func isReservedToolName(name string) bool {
	switch llms.ToolChoiceMode(name) {
	case llms.ToolChoiceAuto, llms.ToolChoiceNone, llms.ToolChoiceRequired, llms.ToolChoiceFunction:
		return true
	}
	return false
}

func validateToolChoice(in *llms.InferenceRequest, names map[string]bool) error {
	if (in.ToolChoice != nil || in.ParallelToolCalls != nil) && len(in.Tools) == 0 {
		return Invalid(paramTools, "tool controls require tools")
	}
	if in.ToolChoice == nil {
		return nil
	}
	switch in.ToolChoice.Mode {
	case llms.ToolChoiceAuto, llms.ToolChoiceNone, llms.ToolChoiceRequired:
		if in.ToolChoice.Name != "" {
			return Invalid(paramToolChoice, "tool name is only valid with function mode")
		}
	case llms.ToolChoiceFunction:
		if !names[in.ToolChoice.Name] {
			return Invalid(paramToolChoice, "unknown function")
		}
	default:
		return Invalid(paramToolChoice, "unsupported tool choice mode")
	}
	return nil
}

func validateFormat(f *llms.InferenceFormat) error {
	if f == nil {
		return nil
	}
	switch f.Type {
	case llms.FormatText, llms.FormatJSONObject:
		if len(f.Schema) > 0 || f.Strict || f.Name != "" {
			return Invalid(paramFormat, "schema controls require json_schema")
		}
	case llms.FormatJSONSchema:
		if !functionName.MatchString(f.Name) {
			return Invalid(paramFormat, "schema name must match [a-zA-Z_][a-zA-Z0-9_-]{0,63}")
		}
		if len(f.Schema) > maxSchemaBytes {
			return Invalid(paramFormat, "schema exceeds %d bytes", maxSchemaBytes)
		}
		if err := schema.ValidatePortableSchema(f.Schema, f.Strict); err != nil {
			return errors.WithStack(&Error{
				Kind:    KindInvalidRequest,
				Param:   paramFormat,
				Message: "unsupported or invalid output schema",
				Cause:   err,
			})
		}
	default:
		return Invalid(paramFormat, "unsupported response format")
	}
	return nil
}

func validateReasoningConfig(r *llms.InferenceReasoning) error {
	if r == nil {
		return nil
	}
	switch r.Effort {
	case "", llms.EffortMinimal, llms.EffortLow, llms.EffortMedium, llms.EffortHigh:
	default:
		return Invalid(paramReasoningCfg, "unsupported reasoning effort")
	}
	if r.BudgetTokens != nil && (*r.BudgetTokens < 0 || *r.BudgetTokens > maxOutputTokens) {
		return Invalid(paramReasoningCfg, "reasoning budget must be between 0 and %d", maxOutputTokens)
	}
	return nil
}

// validateHistory checks roles, ordering, tool-call resolution and block shapes.
// It fills function_call_output names from the matching call.
func validateHistory(in *llms.InferenceRequest) error {
	pending := map[string]string{}
	seen := map[string]bool{}
	contentBytes := 0
	ordinarySeen := false
	messageCount := len(in.Messages)
	for mi, m := range in.Messages {
		blockCount := len(m.Content)
		if blockCount == 0 || blockCount > maxBlocksPerMessage {
			return Invalid(paramContent, "expected 1 to %d content blocks", maxBlocksPerMessage)
		}
		if m.Role != llms.InferenceRoleTool && len(pending) > 0 {
			return Invalid(paramMessages, "all tool calls require a result before continuing")
		}
		switch m.Role {
		case llms.InferenceRoleSystem, llms.InferenceRoleDeveloper:
			if ordinarySeen {
				return Invalid(paramMessages, "instructions must precede conversational messages")
			}
		case llms.InferenceRoleUser, llms.InferenceRoleAssistant, llms.InferenceRoleTool:
			ordinarySeen = true
		default:
			return Invalid(paramMessagesRole, "unsupported role")
		}
		for bi, b := range m.Content {
			contentBytes += len(b.Text) + len(b.Arguments) + len(b.ID) + len(b.Name)
			if len(b.ID) > maxModelNameBytes {
				return Invalid(paramToolCallID, "tool call ID exceeds %d bytes", maxModelNameBytes)
			}
			switch b.Type {
			case llms.BlockText:
				if m.Role == llms.InferenceRoleTool || b.ID != "" || b.Name != "" || b.Arguments != "" || len(b.Opaque) > 0 {
					return Invalid(paramContent, "text blocks carry text only")
				}
			case llms.BlockRefusal:
				if m.Role != llms.InferenceRoleAssistant || b.ID != "" || b.Name != "" || b.Arguments != "" || len(b.Opaque) > 0 {
					return Invalid(paramContent, "refusal blocks belong to assistant messages and carry text only")
				}
			case llms.BlockReasoning:
				if m.Role != llms.InferenceRoleAssistant || b.Name != "" || b.Arguments != "" {
					return Invalid(paramReasoning, "reasoning blocks belong to assistant messages")
				}
				if len(b.Opaque) > maxOpaqueBytes {
					return Invalid(paramReasoning, "reasoning state exceeds %d bytes", maxOpaqueBytes)
				}
				if len(b.Opaque) > 0 && (b.Source == "" || !json.Valid(b.Opaque)) {
					return Invalid(paramReasoning, "reasoning state requires a valid source and JSON payload")
				}
				contentBytes += len(b.Opaque)
			case llms.BlockFunctionCall:
				if m.Role != llms.InferenceRoleAssistant || b.ID == "" || seen[b.ID] || !functionName.MatchString(b.Name) || b.Text != "" || len(b.Opaque) > 0 {
					return Invalid(paramToolCalls, "invalid or duplicate tool call")
				}
				if !isJSONObject(b.Arguments) {
					return Invalid(paramToolCalls+".arguments", "arguments must be a JSON object")
				}
				pending[b.ID] = b.Name
				seen[b.ID] = true
			case llms.BlockFunctionOutput:
				name, ok := pending[b.ID]
				if m.Role != llms.InferenceRoleTool || !ok || b.Arguments != "" || len(b.Opaque) > 0 {
					return Invalid(paramToolCallID, "orphan or duplicate tool result")
				}
				if b.Name != "" && b.Name != name {
					return Invalid(paramToolCallID, "tool result name mismatch")
				}
				in.Messages[mi].Content[bi].Name = name
				delete(pending, b.ID)
			default:
				return Invalid(paramContent+".type", "unsupported content block type")
			}
		}
	}
	if len(pending) > 0 {
		return Invalid(paramMessages, "missing tool results")
	}
	if !ordinarySeen {
		return Invalid(paramMessages, "conversation requires a user or assistant message")
	}
	if in.Messages[messageCount-1].Role == llms.InferenceRoleAssistant {
		return Invalid(paramMessages, "assistant prefilling is outside the portable profile")
	}
	if contentBytes > maxContentBytes {
		return Invalid(paramMessages, "content exceeds %d bytes", maxContentBytes)
	}
	return nil
}

func isJSONObject(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	return strings.HasPrefix(trimmed, "{") && json.Valid([]byte(trimmed))
}

// supports checks the operator-declared features of a target against the request.
func supports(t Target, in llms.InferenceRequest) error {
	f := t.Features
	if t.MaxOutputTokens > 0 && in.MaxTokens != nil && *in.MaxTokens > t.MaxOutputTokens {
		return Unsupported(paramMaxTokens, "model output limit exceeded")
	}
	if len(in.Tools) > 0 && !f.Tools {
		return Unsupported(paramTools, "model does not support function tools")
	}
	if in.Reasoning != nil && !f.Reasoning {
		return Unsupported(paramReasoningCfg, "model does not support reasoning controls")
	}
	if in.Seed != nil && !f.Seed {
		return Unsupported(paramSeed, "model does not support seeds")
	}
	for _, m := range in.Messages {
		if m.Role == llms.InferenceRoleDeveloper && !f.DeveloperRole {
			return Unsupported(paramMessagesRole, "model does not support the developer role")
		}
		for _, b := range m.Content {
			switch b.Type {
			case llms.BlockFunctionCall, llms.BlockFunctionOutput:
				if !f.Tools {
					return Unsupported(paramMessages, "model does not support tool history")
				}
			case llms.BlockReasoning:
				if len(b.Opaque) > 0 && b.Source != t.ID {
					return Unsupported(paramReasoning, "reasoning state was produced by a different model")
				}
			}
		}
	}
	for _, tool := range in.Tools {
		if tool.Strict != nil && *tool.Strict && !f.StrictTools {
			return Unsupported(paramToolsStrict, "model does not support strict tools")
		}
	}
	if v := in.Format; v != nil {
		if v.Type == llms.FormatJSONObject && !f.JSON {
			return Unsupported(paramFormat, "model does not support JSON mode")
		}
		if v.Type == llms.FormatJSONSchema && (!f.JSONSchema || (v.Strict && !f.StrictSchema)) {
			return Unsupported(paramFormat, "model does not support the requested schema mode")
		}
		if v.Type != llms.FormatText && len(in.Tools) > 0 && !f.SchemaWithTools {
			return Unsupported(paramFormat, "model does not support JSON output with tools")
		}
	}
	return nil
}

// normalizeResponse validates a connector response against the request contract.
// Bookkeeping disagreements are normalized; contract violations are errors.
func normalizeResponse(in llms.InferenceRequest, out *llms.InferenceResponse) (*llms.InferenceResponse, error) {
	if out == nil {
		return nil, errors.New("nil inference response")
	}
	res := out.Clone()
	if res.UsageKnown && !usageConsistent(res.Usage) {
		res.UsageKnown = false
		res.Usage = llms.Usage{}
	}
	switch res.FinishReason {
	case llms.FinishStop, llms.FinishToolCalls, llms.FinishLength, llms.FinishContentFilter:
	default:
		return nil, errors.Errorf("unknown finish reason %q", res.FinishReason)
	}
	choice := in.ToolChoice
	namedChoice := choice != nil && choice.Mode == llms.ToolChoiceFunction
	forbidCalls := choice != nil && choice.Mode == llms.ToolChoiceNone
	requireCalls := namedChoice || (choice != nil && choice.Mode == llms.ToolChoiceRequired)
	tools := map[string]llms.InferenceTool{}
	for _, t := range in.Tools {
		tools[t.Name] = t
	}
	var text strings.Builder
	calls := 0
	refusal := false
	seen := map[string]bool{}
	for _, b := range res.Content {
		switch b.Type {
		case llms.BlockText:
			text.WriteString(b.Text)
		case llms.BlockRefusal:
			refusal = true
		case llms.BlockReasoning:
			if len(b.Opaque) > 0 && !json.Valid(b.Opaque) {
				return nil, errors.New("reasoning state is not valid JSON")
			}
		case llms.BlockFunctionCall:
			calls++
			tool, ok := tools[b.Name]
			if !ok || b.ID == "" || seen[b.ID] {
				return nil, errors.Errorf("invalid returned tool call %q", b.Name)
			}
			seen[b.ID] = true
			if forbidCalls || (namedChoice && choice.Name != b.Name) {
				return nil, errors.New("model violated tool choice")
			}
			if res.FinishReason == llms.FinishLength {
				continue
			}
			if !isJSONObject(b.Arguments) {
				return nil, errors.New("returned tool arguments must be a JSON object")
			}
			if tool.Strict != nil && *tool.Strict {
				if err := schema.ValidatePortableJSON(tool.Parameters, b.Arguments); err != nil {
					return nil, errors.WithMessage(err, "strict tool arguments")
				}
			}
		default:
			return nil, errors.Errorf("unsupported output block type %q", b.Type)
		}
	}
	if in.ParallelToolCalls != nil && !*in.ParallelToolCalls && calls > 1 {
		return nil, errors.New("model violated parallel tool setting")
	}
	if calls > 0 && res.FinishReason == llms.FinishStop {
		res.FinishReason = llms.FinishToolCalls
	}
	if res.FinishReason == llms.FinishToolCalls && calls == 0 {
		return nil, errors.New("missing output tool calls")
	}
	if len(res.Content) == 0 && (res.FinishReason == llms.FinishStop || res.FinishReason == llms.FinishToolCalls) {
		return nil, errors.New("empty inference output")
	}
	if requireCalls && res.FinishReason == llms.FinishStop && !refusal {
		return nil, errors.New("model did not call a required tool")
	}
	if refusal || res.FinishReason != llms.FinishStop || in.Format == nil {
		return &res, nil
	}
	switch in.Format.Type {
	case llms.FormatJSONObject:
		if !isJSONObject(text.String()) {
			return nil, errors.New("JSON output must be an object")
		}
	case llms.FormatJSONSchema:
		if err := schema.ValidatePortableJSON(in.Format.Schema, text.String()); err != nil {
			return nil, errors.WithMessage(err, "JSON output")
		}
	}
	return &res, nil
}

func usageConsistent(u llms.Usage) bool {
	for _, n := range []uint64{u.InputTokens, u.OutputTokens, u.TotalTokens, u.CacheReadTokens, u.CacheWriteTokens, u.ReasoningTokens} {
		if n > maxAccountingTokens {
			return false
		}
	}
	return u.TotalTokens == u.InputTokens+u.OutputTokens &&
		u.CacheReadTokens+u.CacheWriteTokens <= u.InputTokens &&
		u.ReasoningTokens <= u.OutputTokens
}

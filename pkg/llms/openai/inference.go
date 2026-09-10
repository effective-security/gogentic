package openai

import (
	"context"
	"encoding/json"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/openai/internal/openaiclient"
)

// Wire field names shared by both upstream paths.
const (
	fieldModel             = "model"
	fieldStream            = "stream"
	fieldStore             = "store"
	fieldTemperature       = "temperature"
	fieldTopP              = "top_p"
	fieldSeed              = "seed"
	fieldStop              = "stop"
	fieldTools             = "tools"
	fieldToolChoice        = "tool_choice"
	fieldParallelToolCalls = "parallel_tool_calls"
	fieldType              = "type"
	fieldName              = "name"
	fieldDescription       = "description"
	fieldParameters        = "parameters"
	fieldStrict            = "strict"
	fieldSchema            = "schema"
	fieldRole              = "role"
	fieldContent           = "content"
	fieldText              = "text"
	fieldArguments         = "arguments"
	fieldFunction          = "function"
	fieldID                = "id"
	fieldRefusal           = "refusal"

	// Chat Completions specific.
	chatMaxCompletionTokens = "max_completion_tokens"
	chatReasoningEffort     = "reasoning_effort"
	chatResponseFormat      = "response_format"
	chatJSONSchema          = "json_schema"
	chatToolCalls           = "tool_calls"
	chatToolCallID          = "tool_call_id"
	chatMessages            = "messages"

	// Responses API specific.
	respInput           = "input"
	respMaxOutputTokens = "max_output_tokens"
	respTextFormat      = "format"
	respTextField       = "text"
	respReasoning       = "reasoning"
	respEffort          = "effort"
	respSummary         = "summary"
	respSummaryAuto     = "auto"
	respInclude         = "include"
	respIncludeOpaque   = "reasoning.encrypted_content"
	respCallID          = "call_id"
	respOutput          = "output"

	// Unsupported parameter names reported to callers.
	paramReasoningBudget = "reasoning.budget_tokens"
	paramContentOrder    = "messages.content.order"
)

// upstream resolves which OpenAI API the portable path uses for this model.
func (o *LLM) upstream() InferenceAPI {
	if o.inferenceAPI != InferenceAPIDefault {
		return o.inferenceAPI
	}
	if o.providerType == llms.ProviderOpenAI {
		return InferenceAPIResponses
	}
	return InferenceAPIChat
}

// ValidateInference rejects controls the selected upstream API cannot express.
// Every OpenAI-compatible provider type is accepted; only the upstream dialect
// restricts controls.
func (o *LLM) ValidateInference(r llms.InferenceRequest) error {
	if r.Reasoning != nil {
		if _, ok := r.Reasoning.EffectiveEffort(); !ok {
			return llms.UnsupportedInference(paramReasoningBudget)
		}
	}
	if o.upstream() == InferenceAPIResponses {
		if len(r.Stop) > 0 {
			return llms.UnsupportedInference(fieldStop)
		}
		if r.Seed != nil {
			return llms.UnsupportedInference(fieldSeed)
		}
		return nil
	}
	// Chat cannot represent text interleaved after function calls in one message.
	for _, m := range r.Messages {
		call := false
		for _, b := range m.Content {
			if b.Type == llms.BlockFunctionCall {
				call = true
			}
			if call && b.Type == llms.BlockText {
				return llms.UnsupportedInference(paramContentOrder)
			}
		}
	}
	return nil
}

// Infer performs one presence-preserving, non-streaming generation through the
// Responses API or Chat Completions depending on configuration.
func (o *LLM) Infer(ctx context.Context, r llms.InferenceRequest) (*llms.InferenceResponse, error) {
	if err := o.ValidateInference(r); err != nil {
		return nil, err
	}
	if o.upstream() == InferenceAPIResponses {
		return o.client.InferResponses(ctx, o.responsesBody(r))
	}
	return o.client.InferChat(ctx, o.chatBody(r))
}

func (o *LLM) chatBody(r llms.InferenceRequest) map[string]any {
	var messages []any
	for _, m := range r.Messages {
		if m.Role == llms.InferenceRoleTool {
			for _, b := range m.Content {
				messages = append(messages, map[string]any{
					fieldRole:      string(llms.InferenceRoleTool),
					chatToolCallID: b.ID,
					fieldContent:   b.Text,
				})
			}
			continue
		}
		msg := map[string]any{
			fieldRole: string(m.Role),
		}
		var content string
		var calls []any
		for _, b := range m.Content {
			switch b.Type {
			case llms.BlockText:
				content += b.Text
			case llms.BlockFunctionCall:
				calls = append(calls, map[string]any{
					fieldID:   b.ID,
					fieldType: llms.InferenceFunction,
					fieldFunction: map[string]any{
						fieldName:      b.Name,
						fieldArguments: b.Arguments,
					},
				})
			}
			// Refusal and reasoning history have no Chat slot and are dropped.
		}
		if content != "" || len(calls) == 0 {
			msg[fieldContent] = content
		} else {
			msg[fieldContent] = nil
		}
		if len(calls) > 0 {
			msg[chatToolCalls] = calls
		}
		messages = append(messages, msg)
	}
	body := map[string]any{
		fieldModel:   r.Model,
		chatMessages: messages,
		fieldStream:  false,
	}
	o.applySampling(body, r)
	if r.MaxTokens != nil {
		body[chatMaxCompletionTokens] = *r.MaxTokens
	}
	if len(r.Stop) > 0 {
		body[fieldStop] = r.Stop
	}
	if r.Seed != nil {
		body[fieldSeed] = *r.Seed
	}
	if effort, ok := r.Reasoning.EffectiveEffort(); ok {
		body[chatReasoningEffort] = string(effort)
	}
	if toolCount := len(r.Tools); toolCount > 0 {
		ts := make([]any, 0, toolCount)
		for _, t := range r.Tools {
			ts = append(ts, map[string]any{
				fieldType:     llms.InferenceFunction,
				fieldFunction: toolDefinition(t),
			})
		}
		body[fieldTools] = ts
	}
	if c := r.ToolChoice; c != nil {
		if c.Mode == llms.ToolChoiceFunction {
			body[fieldToolChoice] = map[string]any{
				fieldType: llms.InferenceFunction,
				fieldFunction: map[string]any{
					fieldName: c.Name,
				},
			}
		} else {
			body[fieldToolChoice] = string(c.Mode)
		}
	}
	if f := r.Format; f != nil {
		rf := map[string]any{
			fieldType: string(f.Type),
		}
		if f.Type == llms.FormatJSONSchema {
			rf[chatJSONSchema] = map[string]any{
				fieldName:   f.Name,
				fieldSchema: f.Schema,
				fieldStrict: f.Strict,
			}
		}
		body[chatResponseFormat] = rf
	}
	return body
}

func (o *LLM) responsesBody(r llms.InferenceRequest) map[string]any {
	var items []any
	replayedReasoning := false
	for _, m := range r.Messages {
		switch m.Role {
		case llms.InferenceRoleTool:
			for _, b := range m.Content {
				items = append(items, map[string]any{
					fieldType:  openaiclient.ResponsesItemFunctionOutput,
					respCallID: b.ID,
					respOutput: b.Text,
				})
			}
		case llms.InferenceRoleAssistant:
			var parts []any
			flush := func() {
				if len(parts) > 0 {
					items = append(items, map[string]any{
						fieldType:    openaiclient.ResponsesItemMessage,
						fieldRole:    string(llms.InferenceRoleAssistant),
						fieldContent: parts,
					})
					parts = nil
				}
			}
			for _, b := range m.Content {
				switch b.Type {
				case llms.BlockText:
					parts = append(parts, map[string]any{
						fieldType: openaiclient.ResponsesPartOutputText,
						fieldText: b.Text,
					})
				case llms.BlockRefusal:
					parts = append(parts, map[string]any{
						fieldType:    openaiclient.ResponsesPartRefusal,
						fieldRefusal: b.Text,
					})
				case llms.BlockFunctionCall:
					flush()
					items = append(items, map[string]any{
						fieldType:      openaiclient.ResponsesItemFunctionCall,
						respCallID:     b.ID,
						fieldName:      b.Name,
						fieldArguments: b.Arguments,
					})
				case llms.BlockReasoning:
					if len(b.Opaque) > 0 {
						flush()
						replayedReasoning = true
						items = append(items, json.RawMessage(b.Opaque))
					}
				}
			}
			flush()
		default:
			parts := make([]any, 0, len(m.Content))
			for _, b := range m.Content {
				if b.Type == llms.BlockText {
					parts = append(parts, map[string]any{
						fieldType: openaiclient.ResponsesPartInputText,
						fieldText: b.Text,
					})
				}
			}
			items = append(items, map[string]any{
				fieldType:    openaiclient.ResponsesItemMessage,
				fieldRole:    string(m.Role),
				fieldContent: parts,
			})
		}
	}
	body := map[string]any{
		fieldModel:  r.Model,
		respInput:   items,
		fieldStream: false,
	}
	if o.providerType == llms.ProviderOpenAI {
		body[fieldStore] = false
	}
	o.applySampling(body, r)
	if r.MaxTokens != nil {
		body[respMaxOutputTokens] = *r.MaxTokens
	}
	if toolCount := len(r.Tools); toolCount > 0 {
		ts := make([]any, 0, toolCount)
		for _, t := range r.Tools {
			def := toolDefinition(t)
			def[fieldType] = llms.InferenceFunction
			ts = append(ts, def)
		}
		body[fieldTools] = ts
	}
	if c := r.ToolChoice; c != nil {
		if c.Mode == llms.ToolChoiceFunction {
			body[fieldToolChoice] = map[string]any{
				fieldType: llms.InferenceFunction,
				fieldName: c.Name,
			}
		} else {
			body[fieldToolChoice] = string(c.Mode)
		}
	}
	if f := r.Format; f != nil {
		format := map[string]any{
			fieldType: string(f.Type),
		}
		if f.Type == llms.FormatJSONSchema {
			format[fieldName] = f.Name
			format[fieldSchema] = f.Schema
			format[fieldStrict] = f.Strict
		}
		body[respTextField] = map[string]any{
			respTextFormat: format,
		}
	}
	if r.Reasoning != nil {
		reasoning := map[string]any{
			respSummary: respSummaryAuto,
		}
		if effort, ok := r.Reasoning.EffectiveEffort(); ok {
			reasoning[respEffort] = string(effort)
		}
		body[respReasoning] = reasoning
	}
	if r.Reasoning != nil || replayedReasoning {
		body[respInclude] = []string{respIncludeOpaque}
	}
	return body
}

func (o *LLM) applySampling(body map[string]any, r llms.InferenceRequest) {
	if r.Temperature != nil {
		body[fieldTemperature] = *r.Temperature
	}
	if r.TopP != nil {
		body[fieldTopP] = *r.TopP
	}
	if r.ParallelToolCalls != nil {
		body[fieldParallelToolCalls] = *r.ParallelToolCalls
	}
}

func toolDefinition(t llms.InferenceTool) map[string]any {
	def := map[string]any{
		fieldName:        t.Name,
		fieldDescription: t.Description,
		fieldParameters:  t.Parameters,
	}
	if t.Strict != nil {
		def[fieldStrict] = *t.Strict
	}
	return def
}

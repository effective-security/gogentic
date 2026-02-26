package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/openai/internal/openaiclient"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/effective-security/x/values"
	"github.com/effective-security/xlog"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

var logger = xlog.NewPackageLogger("github.com/effective-security/gogentic", "openai")

type ChatMessage = openaiclient.ChatMessage

type LLM struct {
	client *openaiclient.Client
}

const (
	RoleSystem    = "system"
	RoleAssistant = "assistant"
	RoleUser      = "user"
	RoleFunction  = "function"
	RoleTool      = "tool"
)

var (
	_ llms.Model    = (*LLM)(nil)
	_ llms.Embedder = (*LLM)(nil)
)

// New returns a new OpenAI LLM.
func New(opts ...Option) (*LLM, error) {
	_, c, err := newClient(opts...)
	if err != nil {
		return nil, err
	}
	return &LLM{
		client: c,
	}, err
}

// GetName implements the Model interface.
func (o *LLM) GetName() string {
	return o.client.Model
}

// GetProviderType implements the Model interface.
func (o *LLM) GetProviderType() llms.ProviderType {
	return llms.ProviderOpenAI
}

// GenerateContent implements the Model interface.
func (o *LLM) GenerateContent(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.ContentResponse, error) { //nolint: lll, cyclop, goerr113, funlen
	if o.client.SupportsResponsesAPI() {
		return o.generateContentFromResponses(ctx, messages, options...)
	}
	return o.generateContentFromChat(ctx, messages, options...)
}

// GenerateContent implements the Model interface.
func (o *LLM) generateContentFromChat(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.ContentResponse, error) { //nolint: lll, cyclop, goerr113, funlen
	opts := llms.CallOptions{}
	for _, opt := range options {
		opt(&opts)
	}

	chatMsgs := make([]*ChatMessage, 0, len(messages))
	for _, mc := range messages {
		msg := &ChatMessage{MultiContent: mc.Parts}
		switch mc.Role {
		case llms.RoleSystem:
			msg.Role = RoleSystem
		case llms.RoleAI:
			msg.Role = RoleAssistant
		case llms.RoleHuman:
			msg.Role = RoleUser
		case llms.RoleGeneric:
			msg.Role = RoleUser
		case llms.RoleTool:
			msg.Role = RoleTool
			// Here we extract tool calls from the message and populate the ToolCalls field.

			// parse mc.Parts (which should have one entry of type ToolCallResponse) and populate msg.Content and msg.ToolCallID
			if len(mc.Parts) != 1 {
				return nil, errors.Errorf("expected exactly one part for role %v, got %v", mc.Role, len(mc.Parts))
			}
			switch p := mc.Parts[0].(type) {
			case llms.ToolCallResponse:
				msg.ToolCallID = p.ToolCallID
				msg.Content = p.Content
			default:
				return nil, errors.Errorf("expected part of type ToolCallResponse for role %v, got %T", mc.Role, mc.Parts[0])
			}

		default:
			return nil, errors.Errorf("role %v not supported", mc.Role)
		}

		// Here we extract tool calls from the message and populate the ToolCalls field.
		newParts, toolCalls := ExtractToolParts(msg)
		msg.MultiContent = newParts
		msg.ToolCalls = toolCallsFromToolCalls(toolCalls)

		chatMsgs = append(chatMsgs, msg)
	}
	req := &openaiclient.ChatRequest{
		Model:                  opts.Model,
		StopWords:              opts.StopWords,
		Messages:               chatMsgs,
		StreamingFunc:          opts.StreamingFunc,
		StreamingReasoningFunc: opts.StreamingReasoningFunc,
		Temperature:            opts.Temperature,
		N:                      opts.N,
		FrequencyPenalty:       opts.FrequencyPenalty,
		PresencePenalty:        opts.PresencePenalty,

		MaxCompletionTokens: opts.MaxTokens,

		ToolChoice:     opts.ToolChoice,
		Seed:           opts.Seed,
		Metadata:       opts.Metadata,
		ResponseFormat: opts.ResponseFormat,
	}
	applyPromptCacheToChatRequest(req, o.client.Provider, &opts)

	// if opts.Tools is not empty, append them to req.Tools
	for _, tool := range opts.Tools {
		t, err := toolFromTool(tool)
		if err != nil {
			return nil, errors.Wrap(err, "failed to convert llms tool to openai tool")
		}
		req.Tools = append(req.Tools, t)
	}

	// if o.client.ResponseFormat is set, use it for the request
	if o.client.ResponseFormat != nil {
		req.ResponseFormat = o.client.ResponseFormat
	}

	result, err := o.client.CreateChat(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(result.Choices) == 0 {
		return nil, ErrEmptyResponse
	}

	choices := make([]*llms.ContentChoice, len(result.Choices))
	for i, c := range result.Choices {
		choices[i] = &llms.ContentChoice{
			Content:    c.Message.Content,
			StopReason: fmt.Sprint(c.FinishReason),
			GenerationInfo: map[string]any{
				"OutputTokens":    result.Usage.CompletionTokens,
				"InputTokens":     result.Usage.PromptTokens,
				"TotalTokens":     result.Usage.TotalTokens,
				"ReasoningTokens": result.Usage.CompletionTokensDetails.ReasoningTokens,
			},
		}

		for _, tool := range c.Message.ToolCalls {
			choices[i].ToolCalls = append(choices[i].ToolCalls, llms.ToolCall{
				ID:   tool.ID,
				Type: string(tool.Type),
				FunctionCall: &llms.FunctionCall{
					Name:      tool.Function.Name,
					Arguments: tool.Function.Arguments,
				},
			})
		}
		// populate legacy single-function call field for backwards compatibility
		if len(choices[i].ToolCalls) > 0 {
			choices[i].FuncCall = choices[i].ToolCalls[0].FunctionCall
		}
	}
	response := &llms.ContentResponse{Choices: choices}
	return response, nil
}

func (o *LLM) generateContentFromResponses(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.ContentResponse, error) {
	opts := llms.CallOptions{}
	for _, opt := range options {
		opt(&opts)
	}

	logger.ContextKV(ctx, xlog.DEBUG, "messages", len(messages))

	// Build Responses API input from generic messages
	var inputItems responses.ResponseInputParam
	for _, mc := range messages {
		switch mc.Role {
		case llms.RoleSystem, llms.RoleHuman:
			role := "user"
			if mc.Role == llms.RoleSystem {
				role = "system"
			}
			var contents responses.ResponseInputMessageContentListParam
			for _, p := range mc.Parts {
				switch v := p.(type) {
				case llms.TextContent:
					contents = append(contents, responses.ResponseInputContentUnionParam{OfInputText: &responses.ResponseInputTextParam{Text: v.Text}})
				case llms.ImageURLContent:
					contents = append(contents, responses.ResponseInputContentUnionParam{OfInputImage: &responses.ResponseInputImageParam{ImageURL: param.NewOpt(v.URL), Detail: responses.ResponseInputImageDetail(v.Detail)}})
				case llms.BinaryContent:
					contents = append(contents, responses.ResponseInputContentUnionParam{OfInputFile: &responses.ResponseInputFileParam{FileData: param.NewOpt(v.String())}})
				default:
					return nil, errors.Errorf("unsupported content part type %T", p)
				}
			}
			if len(contents) == 0 {
				contents = append(contents, responses.ResponseInputContentUnionParam{OfInputText: &responses.ResponseInputTextParam{Text: ""}})
			}
			inputItems = append(inputItems, responses.ResponseInputItemUnionParam{OfInputMessage: &responses.ResponseInputItemMessageParam{Role: role, Content: contents}})

		case llms.RoleAI, llms.RoleGeneric:
			// Assistant messages must use output content types (output_text/refusal)
			var outContents []responses.ResponseOutputMessageContentUnionParam
			var fnCalls []responses.ResponseFunctionToolCallParam
			for _, p := range mc.Parts {
				switch v := p.(type) {
				case llms.TextContent:
					outContents = append(outContents, responses.ResponseOutputMessageContentUnionParam{OfOutputText: &responses.ResponseOutputTextParam{Text: v.Text}})
				case llms.ToolCall:
					if v.FunctionCall != nil {
						fnCalls = append(fnCalls, responses.ResponseFunctionToolCallParam{
							Name:      v.FunctionCall.Name,
							Arguments: v.FunctionCall.Arguments,
							CallID:    v.ID,
						})
					}
				default:
					// ignore non-text assistant parts in history
				}
			}
			if len(outContents) == 0 {
				outContents = append(outContents, responses.ResponseOutputMessageContentUnionParam{OfOutputText: &responses.ResponseOutputTextParam{Text: ""}})
			}
			inputItems = append(inputItems, responses.ResponseInputItemUnionParam{OfOutputMessage: &responses.ResponseOutputMessageParam{Content: outContents, Status: responses.ResponseOutputMessageStatusCompleted}})
			// Append function_call items so that tool outputs can reference them by call_id
			for _, fc := range fnCalls {
				fcCopy := fc
				inputItems = append(inputItems, responses.ResponseInputItemUnionParam{OfFunctionCall: &fcCopy})
			}

		case llms.RoleTool:
			if len(mc.Parts) != 1 {
				return nil, errors.Errorf("expected exactly one part for role %v, got %v", mc.Role, len(mc.Parts))
			}
			tr, ok := mc.Parts[0].(llms.ToolCallResponse)
			if !ok {
				return nil, errors.Errorf("expected ToolCallResponse for tool role, got %T", mc.Parts[0])
			}
			fco := responses.ResponseInputItemFunctionCallOutputParam{
				CallID: tr.ToolCallID,
				Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{OfString: param.NewOpt(tr.Content)},
			}
			inputItems = append(inputItems, responses.ResponseInputItemUnionParam{OfFunctionCallOutput: &fco})

		default:
			return nil, errors.Errorf("role %v not supported", mc.Role)
		}
	}

	req := &responses.ResponseNewParams{
		Model:           values.StringsCoalesce(opts.Model, o.client.Model, openaiclient.DefaultChatModel),
		Input:           responses.ResponseNewParamsInputUnion{OfInputItemList: inputItems},
		MaxOutputTokens: param.NewOpt(int64(values.NumbersCoalesce(opts.MaxTokens, openaiclient.DefaultMaxTokens))),
		Metadata:        convertMetadata(opts.Metadata),
		// not supported with gpt5
		//Temperature:     param.NewOpt(opts.Temperature),
		//TopP:            param.NewOpt(opts.TopP),
	}
	applyPromptCacheToResponsesRequest(req, o.client.Provider, &opts)

	effort := opts.ReasoningEffort
	if strings.HasPrefix(o.client.Model, "gpt-5-pro") {
		//   - The `gpt-5-pro` model defaults to (and only supports) `high` reasoning effort.
		effort = llms.ReasoningEffortHigh
	}

	// Only configure reasoning for models that support the reasoning.effort parameter.
	// Explicitly set effort on an unsupported model (e.g. gpt-4o) is silently ignored to
	// avoid sending an invalid parameter that the API will reject with a 400.
	if modelSupportsReasoning(o.client.Model) {
		switch effort {
		case llms.ReasoningEffortLow:
			req.Reasoning = shared.ReasoningParam{Effort: responses.ReasoningEffortLow}
		case llms.ReasoningEffortMedium:
			req.Reasoning = shared.ReasoningParam{Effort: responses.ReasoningEffortMedium}
		case llms.ReasoningEffortHigh:
			req.Reasoning = shared.ReasoningParam{Effort: responses.ReasoningEffortHigh}
		default:
			if strings.HasPrefix(o.client.Model, "gpt-5.1") {
				//   - `gpt-5.1` defaults to `none`, which does not perform reasoning. The supported
				//     reasoning values for `gpt-5.1` are `none`, `low`, `medium`, and `high`. Tool
				//     calls are supported for all reasoning values in gpt-5.1.
				req.Reasoning = shared.ReasoningParam{Effort: responses.ReasoningEffortNone}
			} else {
				//   - All models before `gpt-5.1` default to `medium` reasoning effort, and do not
				//     support `none`.
				//   - The `gpt-5-pro` model defaults to (and only supports) `high` reasoning effort.
				req.Reasoning = shared.ReasoningParam{Effort: responses.ReasoningEffortLow}
			}
		}
	}

	// Tool choice mapping (support simple string modes)
	if opts.ToolChoice != nil {
		if s, ok := opts.ToolChoice.(string); ok {
			req.ToolChoice = responses.ResponseNewParamsToolChoiceUnion{OfToolChoiceMode: param.NewOpt(responses.ToolChoiceOptions(s))}
		}
	}

	// Map tools to responses ToolUnionParam
	for _, tool := range opts.Tools {
		t, err := responsesToolFromTool(tool)
		if err != nil {
			return nil, errors.Wrap(err, "failed to convert llms tool to openai tool")
		}
		req.Tools = append(req.Tools, t)
	}

	// Map ResponseFormat to Responses API text.format
	if o.client.ResponseFormat != nil {
		req.Text = toResponsesText(o.client.ResponseFormat)
	}
	if opts.ResponseFormat != nil {
		req.Text = toResponsesText(opts.ResponseFormat)
	}

	var (
		result *responses.Response
		err    error
	)
	if opts.StreamingFunc != nil {
		result, err = o.client.CreateStreamingResponse(ctx, req, opts.StreamingFunc)
	} else {
		result, err = o.client.CreateResponse(ctx, req)
	}
	if err != nil {
		return nil, err
	}

	logger.ContextKV(ctx, xlog.DEBUG, "outputs", len(result.Output))

	// Build a single choice from output_text and propagate tool calls if present.
	choice := &llms.ContentChoice{
		Content:    result.OutputText(),
		StopReason: "",
		GenerationInfo: map[string]any{
			"OutputTokens":    result.Usage.OutputTokens,
			"InputTokens":     result.Usage.InputTokens,
			"CacheReadTokens": result.Usage.InputTokensDetails.CachedTokens,
			"TotalTokens":     result.Usage.TotalTokens,
			"ReasoningTokens": result.Usage.OutputTokensDetails.ReasoningTokens,
		},
		//ReasoningContent: // TODO,
	}

	// Map Responses output items into tool calls
	for _, item := range result.Output {
		if item.Type == "function_call" {
			id := item.CallID
			if id == "" {
				id = item.ID
			}
			choice.ToolCalls = append(choice.ToolCalls, llms.ToolCall{
				ID:   id,
				Type: string(openaiclient.ToolTypeFunction),
				FunctionCall: &llms.FunctionCall{
					Name:      item.Name,
					Arguments: item.Arguments,
				},
			})
		}
	}
	if len(choice.ToolCalls) > 0 {
		choice.FuncCall = choice.ToolCalls[0].FunctionCall
	}

	response := &llms.ContentResponse{Choices: []*llms.ContentChoice{choice}}
	if response.Choices[0].Content == "" && len(response.Choices[0].ToolCalls) == 0 {
		return nil, ErrEmptyResponse
	}
	return response, nil
}

// CreateEmbedding creates embeddings for the given input texts.
func (o *LLM) CreateEmbedding(ctx context.Context, inputTexts []string) ([][]float32, error) {
	embeddings, err := o.client.CreateEmbedding(ctx, &openaiclient.EmbeddingRequest{
		Input: inputTexts,
		Model: o.client.EmbeddingModel,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create openai embeddings")
	}
	if len(embeddings) == 0 {
		return nil, ErrEmptyResponse
	}
	if len(inputTexts) != len(embeddings) {
		return embeddings, ErrUnexpectedResponseLength
	}
	return embeddings, nil
}

// ExtractToolParts extracts the tool parts from a message.
func ExtractToolParts(msg *ChatMessage) ([]llms.ContentPart, []llms.ToolCall) {
	var content []llms.ContentPart
	var toolCalls []llms.ToolCall
	for _, part := range msg.MultiContent {
		switch p := part.(type) {
		case llms.TextContent:
			content = append(content, p)
		case llms.ImageURLContent:
			content = append(content, p)
		case llms.BinaryContent:
			content = append(content, p)
		case llms.ToolCall:
			toolCalls = append(toolCalls, p)
		}
	}
	return content, toolCalls
}

// toolFromTool converts an llms.Tool to a Tool.
func toolFromTool(t llms.Tool) (openaiclient.Tool, error) {
	tool := openaiclient.Tool{
		Type: openaiclient.ToolType(t.Type),
	}
	switch t.Type {
	case string(openaiclient.ToolTypeWebSearch):
		if t.WebSearchOptions != nil && len(t.WebSearchOptions.AllowedDomains) > 0 {
			tool.WebSearchOptions = &openaiclient.WebSearchOptions{
				AllowedDomains: t.WebSearchOptions.AllowedDomains,
			}
		}
	case string(openaiclient.ToolTypeFunction):
		tool.Function = &openaiclient.FunctionDefinition{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			Parameters:  t.Function.Parameters,
			Strict:      t.Function.Strict,
		}
	default:
		return openaiclient.Tool{}, errors.Errorf("tool type %v not supported", t.Type)
	}
	return tool, nil
}

// toolCallsFromToolCalls converts a slice of llms.ToolCall to a slice of ToolCall.
func toolCallsFromToolCalls(tcs []llms.ToolCall) []openaiclient.ToolCall {
	toolCalls := make([]openaiclient.ToolCall, len(tcs))
	for i, tc := range tcs {
		toolCalls[i] = toolCallFromToolCall(tc)
	}
	return toolCalls
}

// toolCallFromToolCall converts an llms.ToolCall to a ToolCall.
func toolCallFromToolCall(tc llms.ToolCall) openaiclient.ToolCall {
	return openaiclient.ToolCall{
		ID:   tc.ID,
		Type: openaiclient.ToolType(tc.Type),
		Function: openaiclient.ToolFunction{
			Name:      tc.FunctionCall.Name,
			Arguments: tc.FunctionCall.Arguments,
		},
	}
}

// responsesToolFromTool converts an llms.Tool to a Responses ToolUnionParam.
func responsesToolFromTool(t llms.Tool) (responses.ToolUnionParam, error) {
	switch t.Type {
	case string(openaiclient.ToolTypeWebSearch):
		var filters responses.WebSearchToolFiltersParam
		if t.WebSearchOptions != nil {
			filters.AllowedDomains = t.WebSearchOptions.AllowedDomains
		}
		return responses.ToolUnionParam{
			OfWebSearch: &responses.WebSearchToolParam{
				Type:    responses.WebSearchToolType(responses.WebSearchToolTypeWebSearch2025_08_26),
				Filters: filters,
			},
		}, nil
	case string(openaiclient.ToolTypeFunction):
		if t.Function == nil {
			return responses.ToolUnionParam{}, errors.Errorf("function tool missing definition")
		}
		// Convert jsonschema.Schema to map[string]any
		var paramsMap map[string]any
		if t.Function.Parameters != nil {
			b, err := json.Marshal(t.Function.Parameters)
			if err != nil {
				return responses.ToolUnionParam{}, errors.Wrap(err, "marshal function parameters")
			}
			if err := json.Unmarshal(b, &paramsMap); err != nil {
				return responses.ToolUnionParam{}, errors.Wrap(err, "unmarshal function parameters")
			}
		}
		// Strict mode requires additionalProperties: false at every object level.
		if t.Function.Strict {
			enforceStrictSchema(paramsMap)
		}
		ft := &responses.FunctionToolParam{
			Name:        t.Function.Name,
			Description: param.NewOpt(t.Function.Description),
			Parameters:  paramsMap,
			Strict:      param.NewOpt(t.Function.Strict),
		}
		return responses.ToolUnionParam{OfFunction: ft}, nil
	default:
		return responses.ToolUnionParam{}, errors.Errorf("tool type %v not supported", t.Type)
	}
}

// toResponsesText maps our schema.ResponseFormat to Responses API text config.
func toResponsesText(f *schema.ResponseFormat) responses.ResponseTextConfigParam {
	if f == nil {
		return responses.ResponseTextConfigParam{Format: responses.ResponseFormatTextConfigUnionParam{OfText: &shared.ResponseFormatTextParam{}}}
	}
	switch f.Type {
	case "json_schema":
		var schemaMap map[string]any
		if f.JSONSchema != nil {
			// Build map from our schema struct
			b, err := json.Marshal(f.JSONSchema.Schema)
			if err == nil {
				_ = json.Unmarshal(b, &schemaMap)
			}
		}
		name := "response"
		strict := false
		if f.JSONSchema != nil {
			if f.JSONSchema.Name != "" {
				name = f.JSONSchema.Name
			}
			strict = f.JSONSchema.Strict
		}
		return responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
				Name:   name,
				Schema: schemaMap,
				Strict: param.NewOpt(strict),
			}},
		}
	case "json_object":
		// Older JSON mode; treat as plain text to avoid strict schema
		fallthrough
	default:
		return responses.ResponseTextConfigParam{Format: responses.ResponseFormatTextConfigUnionParam{OfText: &shared.ResponseFormatTextParam{}}}
	}
}

// enforceStrictSchema recursively sets "additionalProperties": false on every object node
// in the schema map. OpenAI strict function calling requires this at all levels.
func enforceStrictSchema(m map[string]any) {
	if m == nil {
		return
	}
	if t, ok := m["type"]; !ok || t == "object" {
		m["additionalProperties"] = false
	}
	if props, ok := m["properties"].(map[string]any); ok {
		for _, v := range props {
			if child, ok := v.(map[string]any); ok {
				enforceStrictSchema(child)
			}
		}
	}
	// Handle array items
	if items, ok := m["items"].(map[string]any); ok {
		enforceStrictSchema(items)
	}
}

// modelSupportsReasoning returns true for models that accept the reasoning.effort parameter.
// gpt-4o and older chat models do not support it.
func modelSupportsReasoning(model string) bool {
	return strings.HasPrefix(model, "o1") ||
		strings.HasPrefix(model, "o3") ||
		strings.HasPrefix(model, "o4") ||
		strings.HasPrefix(model, "gpt-5")
}

// convertMetadata converts map[string]any to shared.Metadata (map[string]string).
func convertMetadata(in map[string]any) shared.Metadata {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = fmt.Sprint(v)
	}
	return out
}

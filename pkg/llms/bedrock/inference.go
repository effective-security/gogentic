package bedrock

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	smithydocument "github.com/aws/smithy-go/document"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
)

// Converse stop reasons and wire constants.
const (
	stopEndTurn             = "end_turn"
	stopSequence            = "stop_sequence"
	stopMaxTokens           = "max_tokens"
	stopToolUse             = "tool_use"
	stopContentFiltered     = "content_filtered"
	stopGuardrailIntervened = "guardrail_intervened"
	outputFormatJSONSchema  = "json_schema"
	thinkingKey             = "thinking"
	thinkingTypeKey         = "type"
	thinkingEnabled         = "enabled"
	thinkingBudgetKey       = "budget_tokens"
	reasoningTextKey        = "reasoningText"
	reasoningTextTextKey    = "text"
	reasoningSignatureKey   = "signature"
	redactedContentKey      = "redactedContent"
	maxConverseAttempts     = 1
	opConverse              = "bedrock converse inference"
	opDecode                = "decode bedrock inference"
	paramTemperature        = "temperature"
	paramTopP               = "top_p"
	paramConverse           = "provider.converse"
	paramParallelToolCalls  = "parallel_tool_calls"
	paramToolChoiceNone     = "tool_choice.none"
	paramJSONObject         = "response_format.json_object"
	paramDeveloperRole      = "messages.role.developer"
	paramSeed               = "seed"
	paramReasoning          = "reasoning"
	paramMaxTokens          = "max_tokens"
)

// ValidateInference verifies that Converse was explicitly enabled and controls map exactly.
func (l *LLM) ValidateInference(r llms.InferenceRequest) error {
	if !l.converse {
		return llms.UnsupportedInference(paramConverse)
	}
	if r.Temperature != nil && *r.Temperature > 1 {
		return llms.UnsupportedInference(paramTemperature)
	}
	if r.ParallelToolCalls != nil {
		return llms.UnsupportedInference(paramParallelToolCalls)
	}
	if r.ToolChoice != nil && r.ToolChoice.Mode == llms.ToolChoiceNone {
		return llms.UnsupportedInference(paramToolChoiceNone)
	}
	if r.Format != nil && r.Format.Type == llms.FormatJSONObject {
		return llms.UnsupportedInference(paramJSONObject)
	}
	if r.Seed != nil {
		return llms.UnsupportedInference(paramSeed)
	}
	for _, m := range r.Messages {
		if m.Role == llms.InferenceRoleDeveloper {
			return llms.UnsupportedInference(paramDeveloperRole)
		}
	}
	if r.Reasoning != nil {
		budget, err := reasoningBudget(r.Reasoning)
		if err != nil {
			return err
		}
		if r.Temperature != nil {
			return llms.UnsupportedInference(paramTemperature)
		}
		if r.TopP != nil {
			return llms.UnsupportedInference(paramTopP)
		}
		if r.MaxTokens != nil && *r.MaxTokens <= budget {
			return llms.UnsupportedInference(paramMaxTokens)
		}
	}
	return nil
}

func reasoningBudget(r *llms.InferenceReasoning) (int, error) {
	if r.BudgetTokens != nil {
		return *r.BudgetTokens, nil
	}
	budget, ok := llms.ReasoningBudget(r.Effort)
	if !ok {
		return 0, llms.UnsupportedInference(paramReasoning)
	}
	return budget, nil
}

// Infer performs one Converse invocation while keeping legacy InvokeModel behavior intact.
func (l *LLM) Infer(ctx context.Context, r llms.InferenceRequest) (*llms.InferenceResponse, error) {
	if err := l.ValidateInference(r); err != nil {
		return nil, err
	}
	in, err := converseInput(r)
	if err != nil {
		return nil, err
	}
	response, err := l.runtimeClient.Converse(ctx, in, func(o *bedrockruntime.Options) { o.RetryMaxAttempts = maxConverseAttempts })
	if err != nil {
		status := http.StatusBadGateway
		var apiErr *smithyhttp.ResponseError
		if errors.As(err, &apiErr) {
			status = apiErr.HTTPStatusCode()
		}
		return nil, llms.InferenceFailure(err, status, opConverse)
	}
	if response == nil {
		return nil, llms.InferenceFailure(errors.New("empty response"), http.StatusBadGateway, opDecode)
	}
	return converseOutput(response)
}

func converseInput(r llms.InferenceRequest) (*bedrockruntime.ConverseInput, error) {
	in := &bedrockruntime.ConverseInput{
		ModelId: aws.String(r.Model),
		InferenceConfig: &types.InferenceConfiguration{
			StopSequences: r.Stop,
		},
	}
	if r.Temperature != nil {
		in.InferenceConfig.Temperature = aws.Float32(float32(*r.Temperature))
	}
	if r.TopP != nil {
		in.InferenceConfig.TopP = aws.Float32(float32(*r.TopP))
	}
	if r.MaxTokens != nil {
		in.InferenceConfig.MaxTokens = aws.Int32(int32(*r.MaxTokens))
	}
	if r.Reasoning != nil {
		budget, err := reasoningBudget(r.Reasoning)
		if err != nil {
			return nil, err
		}
		in.AdditionalModelRequestFields = document.NewLazyDocument(map[string]any{
			thinkingKey: map[string]any{
				thinkingTypeKey:   thinkingEnabled,
				thinkingBudgetKey: budget,
			},
		})
	}
	for _, m := range r.Messages {
		if m.Role == llms.InferenceRoleSystem {
			for _, b := range m.Content {
				in.System = append(in.System, &types.SystemContentBlockMemberText{
					Value: b.Text,
				})
			}
			continue
		}
		role := types.ConversationRoleUser
		if m.Role == llms.InferenceRoleAssistant {
			role = types.ConversationRoleAssistant
		}
		blocks, err := converseBlocks(m.Content)
		if err != nil {
			return nil, err
		}
		// Converse requires consecutive same-role content in one turn.
		if n := len(in.Messages); n > 0 && in.Messages[n-1].Role == role {
			in.Messages[n-1].Content = append(in.Messages[n-1].Content, blocks...)
			continue
		}
		in.Messages = append(in.Messages, types.Message{
			Role:    role,
			Content: blocks,
		})
	}
	if toolCount := len(r.Tools); toolCount > 0 {
		in.ToolConfig = &types.ToolConfiguration{
			Tools: make([]types.Tool, 0, toolCount),
		}
		for _, t := range r.Tools {
			var schema any
			if err := decodeDocument(t.Parameters, &schema); err != nil {
				return nil, llms.InferenceFailure(err, http.StatusBadRequest, "decode bedrock tool schema")
			}
			in.ToolConfig.Tools = append(in.ToolConfig.Tools, &types.ToolMemberToolSpec{
				Value: types.ToolSpecification{
					Name:        aws.String(t.Name),
					Description: aws.String(t.Description),
					InputSchema: &types.ToolInputSchemaMemberJson{
						Value: document.NewLazyDocument(schema),
					},
					Strict: t.Strict,
				},
			})
		}
		if c := r.ToolChoice; c != nil {
			switch c.Mode {
			case llms.ToolChoiceAuto:
				in.ToolConfig.ToolChoice = &types.ToolChoiceMemberAuto{
					Value: types.AutoToolChoice{},
				}
			case llms.ToolChoiceRequired:
				in.ToolConfig.ToolChoice = &types.ToolChoiceMemberAny{
					Value: types.AnyToolChoice{},
				}
			case llms.ToolChoiceFunction:
				in.ToolConfig.ToolChoice = &types.ToolChoiceMemberTool{
					Value: types.SpecificToolChoice{
						Name: aws.String(c.Name),
					},
				}
			}
		}
	}
	if f := r.Format; f != nil && f.Type == llms.FormatJSONSchema {
		in.OutputConfig = &types.OutputConfig{
			TextFormat: &types.OutputFormat{
				Type: types.OutputFormatType(outputFormatJSONSchema),
				Structure: &types.OutputFormatStructureMemberJsonSchema{
					Value: types.JsonSchemaDefinition{
						Name:   aws.String(f.Name),
						Schema: aws.String(string(f.Schema)),
					},
				},
			},
		}
	}
	return in, nil
}

func converseBlocks(blocks []llms.InferenceBlock) ([]types.ContentBlock, error) {
	var out []types.ContentBlock
	for _, b := range blocks {
		switch b.Type {
		case llms.BlockText, llms.BlockRefusal:
			out = append(out, &types.ContentBlockMemberText{
				Value: b.Text,
			})
		case llms.BlockFunctionCall:
			var v any
			if err := decodeDocument([]byte(b.Arguments), &v); err != nil {
				return nil, llms.InferenceFailure(err, http.StatusBadRequest, "decode bedrock tool arguments")
			}
			out = append(out, &types.ContentBlockMemberToolUse{
				Value: types.ToolUseBlock{
					ToolUseId: aws.String(b.ID),
					Name:      aws.String(b.Name),
					Input:     document.NewLazyDocument(v),
				},
			})
		case llms.BlockFunctionOutput:
			out = append(out, &types.ContentBlockMemberToolResult{
				Value: types.ToolResultBlock{
					ToolUseId: aws.String(b.ID),
					Content: []types.ToolResultContentBlock{&types.ToolResultContentBlockMemberText{
						Value: b.Text,
					}},
				},
			})
		case llms.BlockReasoning:
			if len(b.Opaque) == 0 {
				continue
			}
			block, err := reasoningBlock(b.Opaque)
			if err != nil {
				return nil, err
			}
			out = append(out, block)
		}
	}
	return out, nil
}

// reasoningOpaque is the JSON form of one Converse reasoning content block.
type reasoningOpaque struct {
	ReasoningText *struct {
		Text      string `json:"text"`
		Signature string `json:"signature,omitempty"`
	} `json:"reasoningText,omitempty"`
	RedactedContent []byte `json:"redactedContent,omitempty"`
}

func reasoningBlock(opaque json.RawMessage) (types.ContentBlock, error) {
	var v reasoningOpaque
	if err := json.Unmarshal(opaque, &v); err != nil {
		return nil, llms.InferenceFailure(err, http.StatusBadRequest, "decode reasoning state")
	}
	switch {
	case v.ReasoningText != nil:
		text := types.ReasoningTextBlock{
			Text: aws.String(v.ReasoningText.Text),
		}
		if v.ReasoningText.Signature != "" {
			text.Signature = aws.String(v.ReasoningText.Signature)
		}
		return &types.ContentBlockMemberReasoningContent{
			Value: &types.ReasoningContentBlockMemberReasoningText{
				Value: text,
			},
		}, nil
	case len(v.RedactedContent) > 0:
		return &types.ContentBlockMemberReasoningContent{
			Value: &types.ReasoningContentBlockMemberRedactedContent{
				Value: v.RedactedContent,
			},
		}, nil
	}
	return nil, llms.InferenceFailure(errors.New("unknown reasoning state"), http.StatusBadRequest, "decode reasoning state")
}

func converseOutput(response *bedrockruntime.ConverseOutput) (*llms.InferenceResponse, error) {
	out := &llms.InferenceResponse{}
	finish, err := converseFinishReason(response.StopReason)
	if err != nil {
		return nil, err
	}
	out.FinishReason = finish
	msg, ok := response.Output.(*types.ConverseOutputMemberMessage)
	if !ok {
		return nil, llms.InferenceFailure(errors.New("missing message"), http.StatusBadGateway, opDecode)
	}
	for _, b := range msg.Value.Content {
		switch v := b.(type) {
		case *types.ContentBlockMemberText:
			out.Content = append(out.Content, llms.InferenceBlock{
				Type: llms.BlockText,
				Text: v.Value,
			})
		case *types.ContentBlockMemberToolUse:
			if v.Value.Input == nil {
				return nil, llms.InferenceFailure(errors.New("missing tool input"), http.StatusBadGateway, opDecode)
			}
			data, err := v.Value.Input.MarshalSmithyDocument()
			if err != nil {
				return nil, llms.InferenceFailure(err, http.StatusBadGateway, "encode bedrock tool input")
			}
			out.Content = append(out.Content, llms.InferenceBlock{
				Type:      llms.BlockFunctionCall,
				ID:        aws.ToString(v.Value.ToolUseId),
				Name:      aws.ToString(v.Value.Name),
				Arguments: string(data),
			})
		case *types.ContentBlockMemberReasoningContent:
			block, err := reasoningOutput(v.Value)
			if err != nil {
				return nil, err
			}
			out.Content = append(out.Content, block)
		default:
			return nil, llms.InferenceFailure(errors.New("opaque content outside portable profile"), http.StatusBadGateway, opDecode)
		}
	}
	if u := response.Usage; u != nil {
		cacheReadTokens := uint64(aws.ToInt32(u.CacheReadInputTokens))
		cacheWriteTokens := uint64(aws.ToInt32(u.CacheWriteInputTokens))
		inputTokens := uint64(aws.ToInt32(u.InputTokens)) + cacheReadTokens + cacheWriteTokens
		outputTokens := uint64(aws.ToInt32(u.OutputTokens))
		out.UsageKnown = true
		out.Usage = llms.Usage{
			InputTokens:      inputTokens,
			OutputTokens:     outputTokens,
			TotalTokens:      inputTokens + outputTokens,
			CacheReadTokens:  cacheReadTokens,
			CacheWriteTokens: cacheWriteTokens,
		}
	}
	return out, nil
}

func reasoningOutput(content types.ReasoningContentBlock) (llms.InferenceBlock, error) {
	block := llms.InferenceBlock{
		Type: llms.BlockReasoning,
	}
	var opaque reasoningOpaque
	switch v := content.(type) {
	case *types.ReasoningContentBlockMemberReasoningText:
		block.Text = aws.ToString(v.Value.Text)
		opaque.ReasoningText = &struct {
			Text      string `json:"text"`
			Signature string `json:"signature,omitempty"`
		}{
			Text:      block.Text,
			Signature: aws.ToString(v.Value.Signature),
		}
	case *types.ReasoningContentBlockMemberRedactedContent:
		opaque.RedactedContent = v.Value
	default:
		return block, llms.InferenceFailure(errors.New("unknown reasoning content"), http.StatusBadGateway, opDecode)
	}
	raw, err := json.Marshal(opaque)
	if err != nil {
		return block, llms.InferenceFailure(err, http.StatusBadGateway, "encode reasoning state")
	}
	block.Opaque = raw
	return block, nil
}

func converseFinishReason(reason types.StopReason) (llms.FinishReason, error) {
	switch string(reason) {
	case stopEndTurn, stopSequence:
		return llms.FinishStop, nil
	case stopMaxTokens:
		return llms.FinishLength, nil
	case stopToolUse:
		return llms.FinishToolCalls, nil
	case stopContentFiltered, stopGuardrailIntervened:
		return llms.FinishContentFilter, nil
	}
	return "", llms.InferenceFailure(errors.Errorf("unsupported stop reason %q", reason), http.StatusBadGateway, opDecode)
}

func decodeDocument(raw []byte, v *any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(v); err != nil {
		return errors.WithMessage(err, "decode document")
	}
	*v = smithyNumbers(*v)
	return nil
}

func smithyNumbers(v any) any {
	switch n := v.(type) {
	case json.Number:
		return smithydocument.Number(n)
	case map[string]any:
		for k, x := range n {
			n[k] = smithyNumbers(x)
		}
	case []any:
		for i, x := range n {
			n[i] = smithyNumbers(x)
		}
	}
	return v
}

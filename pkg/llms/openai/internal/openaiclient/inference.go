package openaiclient

import (
	"context"
	"net/http"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const (
	chatCompletionsPath = "chat/completions"
	responsesPath       = "responses"

	// Chat finish reasons as returned upstream.
	chatFinishStop          = "stop"
	chatFinishLength        = "length"
	chatFinishToolCalls     = "tool_calls"
	chatFinishContentFilter = "content_filter"
	chatFinishFunctionCall  = "function_call"
)

// inferenceStatus extracts the upstream HTTP status from an SDK error.
func inferenceStatus(err error) int {
	var apiErr *openaisdk.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode > 0 {
		return apiErr.StatusCode
	}
	return http.StatusBadGateway
}

// InferChat performs one non-streaming Chat Completions call with the raw body
// supplied by the connector, using the shared authenticated SDK transport and no
// retries. Legacy defaults are not changed.
func (c *Client) InferChat(ctx context.Context, body map[string]any) (*llms.InferenceResponse, error) {
	var response struct {
		ID      string `json:"id"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   *string `json:"content"`
				Refusal   *string `json:"refusal"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			Input        uint64 `json:"prompt_tokens"`
			Output       uint64 `json:"completion_tokens"`
			Total        uint64 `json:"total_tokens"`
			InputDetails struct {
				Cached uint64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			OutputDetails struct {
				Reasoning uint64 `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
	}
	err := c.sdkClient().Post(ctx, chatCompletionsPath, body, &response, option.WithMaxRetries(0))
	if err != nil {
		return nil, llms.InferenceFailure(err, inferenceStatus(err), "openai chat inference")
	}
	if len(response.Choices) != 1 {
		return nil, llms.InferenceFailure(errors.Errorf("expected one choice, got %d", len(response.Choices)), http.StatusBadGateway, "decode openai chat inference")
	}
	choice := response.Choices[0]
	out := &llms.InferenceResponse{
		UpstreamID: response.ID,
	}
	switch choice.FinishReason {
	case chatFinishStop:
		out.FinishReason = llms.FinishStop
	case chatFinishLength:
		out.FinishReason = llms.FinishLength
	case chatFinishToolCalls, chatFinishFunctionCall:
		out.FinishReason = llms.FinishToolCalls
	case chatFinishContentFilter:
		out.FinishReason = llms.FinishContentFilter
	default:
		return nil, llms.InferenceFailure(errors.Errorf("unsupported finish reason %q", choice.FinishReason), http.StatusBadGateway, "decode openai chat inference")
	}
	m := choice.Message
	if m.Content != nil && *m.Content != "" {
		out.Content = append(out.Content, llms.InferenceBlock{
			Type: llms.BlockText,
			Text: *m.Content,
		})
	}
	if m.Refusal != nil {
		out.Content = append(out.Content, llms.InferenceBlock{
			Type: llms.BlockRefusal,
			Text: *m.Refusal,
		})
	}
	for _, t := range m.ToolCalls {
		if t.Type != llms.InferenceFunction {
			return nil, llms.InferenceFailure(errors.Errorf("unexpected tool call type %q", t.Type), http.StatusBadGateway, "decode openai chat inference")
		}
		out.Content = append(out.Content, llms.InferenceBlock{
			Type:      llms.BlockFunctionCall,
			ID:        t.ID,
			Name:      t.Function.Name,
			Arguments: t.Function.Arguments,
		})
	}
	if u := response.Usage; u != nil {
		out.UsageKnown = true
		out.Usage = llms.Usage{
			InputTokens:     u.Input,
			OutputTokens:    u.Output,
			TotalTokens:     u.Total,
			CacheReadTokens: u.InputDetails.Cached,
			ReasoningTokens: u.OutputDetails.Reasoning,
		}
	}
	return out, nil
}

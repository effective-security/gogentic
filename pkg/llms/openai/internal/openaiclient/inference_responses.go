package openaiclient

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/openai/openai-go/v3/option"
)

// Responses API wire vocabulary.
const (
	ResponsesItemMessage        = "message"
	ResponsesItemFunctionCall   = "function_call"
	ResponsesItemFunctionOutput = "function_call_output"
	ResponsesItemReasoning      = "reasoning"
	ResponsesPartInputText      = "input_text"
	ResponsesPartOutputText     = "output_text"
	ResponsesPartRefusal        = "refusal"
	ResponsesPartSummaryText    = "summary_text"

	responsesStatusCompleted  = "completed"
	responsesStatusIncomplete = "incomplete"
	responsesStatusFailed     = "failed"
	responsesReasonMaxTokens  = "max_output_tokens"
	responsesReasonFilter     = "content_filter"
)

// InferResponses performs one non-streaming Responses API call with the raw body
// supplied by the connector, using the shared authenticated SDK transport and no
// retries. Reasoning items are returned with their raw JSON as opaque state.
func (c *Client) InferResponses(ctx context.Context, body map[string]any) (*llms.InferenceResponse, error) {
	var response struct {
		ID                string `json:"id"`
		Status            string `json:"status"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Output []json.RawMessage `json:"output"`
		Usage  *struct {
			Input        uint64 `json:"input_tokens"`
			Output       uint64 `json:"output_tokens"`
			Total        uint64 `json:"total_tokens"`
			InputDetails struct {
				Cached uint64 `json:"cached_tokens"`
			} `json:"input_tokens_details"`
			OutputDetails struct {
				Reasoning uint64 `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	err := c.sdkClient().Post(ctx, responsesPath, body, &response, option.WithMaxRetries(0))
	if err != nil {
		return nil, llms.InferenceFailure(err, inferenceStatus(err), "openai responses inference")
	}
	out := &llms.InferenceResponse{
		UpstreamID: response.ID,
	}
	switch response.Status {
	case responsesStatusCompleted:
		out.FinishReason = llms.FinishStop
	case responsesStatusIncomplete:
		reason := ""
		if response.IncompleteDetails != nil {
			reason = response.IncompleteDetails.Reason
		}
		switch reason {
		case responsesReasonMaxTokens:
			out.FinishReason = llms.FinishLength
		case responsesReasonFilter:
			out.FinishReason = llms.FinishContentFilter
		default:
			return nil, llms.InferenceFailure(errors.Errorf("unsupported incomplete reason %q", reason), http.StatusBadGateway, "decode openai responses inference")
		}
	case responsesStatusFailed:
		message := "response failed"
		if response.Error != nil {
			message = response.Error.Code + ": " + response.Error.Message
		}
		return nil, llms.InferenceFailure(errors.New(message), http.StatusBadGateway, "openai responses inference")
	default:
		return nil, llms.InferenceFailure(errors.Errorf("unsupported response status %q", response.Status), http.StatusBadGateway, "decode openai responses inference")
	}
	calls := 0
	for _, raw := range response.Output {
		var item struct {
			Type    string `json:"type"`
			Summary []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"summary"`
			Content []struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Refusal string `json:"refusal"`
			} `json:"content"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, llms.InferenceFailure(err, http.StatusBadGateway, "decode openai responses output item")
		}
		switch item.Type {
		case ResponsesItemReasoning:
			var summary strings.Builder
			for _, s := range item.Summary {
				summary.WriteString(s.Text)
			}
			out.Content = append(out.Content, llms.InferenceBlock{
				Type:   llms.BlockReasoning,
				Text:   summary.String(),
				Opaque: raw,
			})
		case ResponsesItemMessage:
			for _, p := range item.Content {
				switch p.Type {
				case ResponsesPartOutputText:
					out.Content = append(out.Content, llms.InferenceBlock{
						Type: llms.BlockText,
						Text: p.Text,
					})
				case ResponsesPartRefusal:
					out.Content = append(out.Content, llms.InferenceBlock{
						Type: llms.BlockRefusal,
						Text: p.Refusal,
					})
				default:
					return nil, llms.InferenceFailure(errors.Errorf("unsupported output part %q", p.Type), http.StatusBadGateway, "decode openai responses inference")
				}
			}
		case ResponsesItemFunctionCall:
			calls++
			out.Content = append(out.Content, llms.InferenceBlock{
				Type:      llms.BlockFunctionCall,
				ID:        item.CallID,
				Name:      item.Name,
				Arguments: item.Arguments,
			})
		default:
			return nil, llms.InferenceFailure(errors.Errorf("unsupported output item %q", item.Type), http.StatusBadGateway, "decode openai responses inference")
		}
	}
	if calls > 0 && out.FinishReason == llms.FinishStop {
		out.FinishReason = llms.FinishToolCalls
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

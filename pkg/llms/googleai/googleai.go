//nolint:all
package googleai

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/googleai/internal/genaiutils"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"google.golang.org/genai"
)

var (
	ErrNoContentInResponse   = errors.New("no content in generation response")
	ErrUnknownPartInResponse = errors.New("unknown part type in generation response")
	ErrInvalidMimeType       = errors.New("invalid mime type on content")
)

const (
	CITATIONS            = "citations"
	SAFETY               = "safety"
	RoleSystem           = "system"
	RoleModel            = "model"
	RoleUser             = "user"
	RoleTool             = "tool"
	ResponseMIMETypeJson = "application/json"
)

// GetName implements the Model interface.
func (g *GoogleAI) GetName() string {
	return g.opts.DefaultModel
}

// GetProviderType implements the Model interface.
func (g *GoogleAI) GetProviderType() llms.ProviderType {
	return llms.ProviderGoogleAI
}

// GenerateContent implements the [llms.Model] interface.
func (g *GoogleAI) GenerateContent(
	ctx context.Context,
	messages []llms.Message,
	options ...llms.CallOption,
) (*llms.ContentResponse, error) {
	opts := llms.CallOptions{
		Model:          g.opts.DefaultModel,
		CandidateCount: g.opts.DefaultCandidateCount,
		MaxTokens:      g.opts.DefaultMaxTokens,
		Temperature:    g.opts.DefaultTemperature,
		TopP:           g.opts.DefaultTopP,
		TopK:           g.opts.DefaultTopK,
	}
	for _, opt := range options {
		opt(&opts)
	}

	// Populate generation controls from generic llms options
	callCfg := &genai.GenerateContentConfig{
		StopSequences:   opts.StopWords,
		CandidateCount:  int32(opts.CandidateCount),
		MaxOutputTokens: int32(opts.MaxTokens),
		Temperature:     genaiutils.Float32Ptr(float32(opts.Temperature)),
		TopP:            genaiutils.Float32Ptr(float32(opts.TopP)),
		TopK:            genaiutils.Float32Ptr(float32(opts.TopK)),
		Seed:            genaiutils.Int32Ptr(int32(opts.Seed)),
	}
	switch opts.ReasoningEffort {
	case llms.ReasoningEffortLow, llms.ReasoningEffortMedium:
		callCfg.ThinkingConfig = &genai.ThinkingConfig{
			ThinkingLevel: genai.ThinkingLevelLow,
		}
	case llms.ReasoningEffortHigh:
		callCfg.ThinkingConfig = &genai.ThinkingConfig{
			ThinkingLevel: genai.ThinkingLevelHigh,
		}
	}

	callCfg.SafetySettings = []*genai.SafetySetting{
		{
			Category:  genai.HarmCategoryDangerousContent,
			Threshold: genai.HarmBlockThreshold(g.opts.HarmThreshold),
		},
		{
			Category:  genai.HarmCategoryHarassment,
			Threshold: genai.HarmBlockThreshold(g.opts.HarmThreshold),
		},
		{
			Category:  genai.HarmCategoryHateSpeech,
			Threshold: genai.HarmBlockThreshold(g.opts.HarmThreshold),
		},
		{
			Category:  genai.HarmCategorySexuallyExplicit,
			Threshold: genai.HarmBlockThreshold(g.opts.HarmThreshold),
		},
	}
	var err error
	if callCfg.Tools, err = genaiutils.ConvertTools(opts.Tools); err != nil {
		return nil, err
	}

	if !hasFunctionTools(callCfg.Tools) && opts.ResponseFormat != nil && opts.ResponseFormat.Type == "json_object" {
		callCfg.ResponseMIMEType = ResponseMIMETypeJson
		if opts.ResponseFormat.JSONSchema != nil {
			callCfg.ResponseSchema, err = genaiutils.ConvertJResponseFormatJSONSchema(opts.ResponseFormat.JSONSchema)
			if err != nil {
				return nil, err
			}
		}
	}

	response, err := g.generateFromMessages(ctx, messages, callCfg)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func hasFunctionTools(tools []*genai.Tool) bool {
	for _, tool := range tools {
		if tool.FunctionDeclarations != nil {
			return true
		}
	}
	return false
}

// convertCandidates converts a sequence of genai.Candidate to a response.
func convertCandidates(candidates []*genai.Candidate, usage *genai.GenerateContentResponseUsageMetadata) (*llms.ContentResponse, error) {
	var contentResponse llms.ContentResponse
	var toolCalls []llms.ToolCall

	for _, candidate := range candidates {
		buf := strings.Builder{}

		if candidate.Content != nil {
			for _, part := range candidate.Content.Parts {
				switch {
				case part.Text != "":
					_, err := buf.WriteString(part.Text)
					if err != nil {
						return nil, err
					}
				case part.FunctionCall != nil:
					b, err := json.Marshal(part.FunctionCall.Args)
					if err != nil {
						return nil, err
					}
					toolCall := llms.ToolCall{
						FunctionCall: &llms.FunctionCall{
							Name:      part.FunctionCall.Name,
							Arguments: string(b),
						},
					}
					toolCalls = append(toolCalls, toolCall)
				default:
					return nil, errors.Wrapf(ErrUnknownPartInResponse, "not text or tool")
				}
			}
		}

		metadata := make(map[string]any)
		metadata[CITATIONS] = candidate.CitationMetadata
		metadata[SAFETY] = candidate.SafetyRatings

		if usage != nil {
			metadata["InputTokens"] = usage.PromptTokenCount
			metadata["CacheReadTokens"] = usage.CachedContentTokenCount
			metadata["OutputTokens"] = usage.CandidatesTokenCount + usage.ToolUsePromptTokenCount + usage.ThoughtsTokenCount
			metadata["TotalTokens"] = usage.TotalTokenCount
		}

		contentResponse.Choices = append(contentResponse.Choices,
			&llms.ContentChoice{
				Content:        buf.String(),
				StopReason:     string(candidate.FinishReason),
				GenerationInfo: metadata,
				ToolCalls:      toolCalls,
			})
	}
	return &contentResponse, nil
}

// convertParts converts between a sequence of langchain parts and genai parts.
func convertParts(parts []llms.ContentPart) ([]*genai.Part, error) {
	convertedParts := make([]*genai.Part, 0, len(parts))
	for _, part := range parts {
		out := new(genai.Part)

		switch p := part.(type) {
		case llms.TextContent:
			out.Text = p.Text
		case llms.BinaryContent:
			out.InlineData = &genai.Blob{MIMEType: p.MIMEType, Data: p.Data}
		case llms.ImageURLContent:
			typ, data, err := llmutils.DownloadImageData(p.URL)
			if err != nil {
				return nil, err
			}
			out.InlineData = &genai.Blob{MIMEType: typ, Data: data}
		case llms.ToolCall:
			fc := p.FunctionCall
			var argsMap map[string]any
			if err := json.Unmarshal([]byte(fc.Arguments), &argsMap); err != nil {
				return convertedParts, err
			}
			out.FunctionCall = &genai.FunctionCall{
				Name: fc.Name,
				Args: argsMap,
			}
		case llms.ToolCallResponse:
			out.FunctionResponse = &genai.FunctionResponse{
				Name: p.Name,
				Response: map[string]any{
					"response": p.Content,
				},
			}
		}

		convertedParts = append(convertedParts, out)
	}
	return convertedParts, nil
}

// convertContent converts between a langchain MessageContent and genai content.
func convertContent(content llms.Message) (*genai.Content, error) {
	parts, err := convertParts(content.Parts)
	if err != nil {
		return nil, err
	}

	c := &genai.Content{
		Parts: parts,
	}

	switch content.Role {
	case llms.RoleSystem:
		c.Role = RoleSystem
	case llms.RoleAI:
		c.Role = RoleModel
	case llms.RoleHuman:
		c.Role = RoleUser
	case llms.RoleGeneric:
		c.Role = RoleUser
	case llms.RoleTool:
		c.Role = RoleTool
	default:
		return nil, errors.Errorf("role %v not supported", content.Role)
	}

	return c, nil
}

func (g *GoogleAI) generateFromMessages(
	ctx context.Context,
	messages []llms.Message,
	config *genai.GenerateContentConfig,
) (*llms.ContentResponse, error) {
	if config == nil {
		config = &genai.GenerateContentConfig{}
	}

	history := make([]*genai.Content, 0, len(messages))
	for _, mc := range messages {
		content, err := convertContent(mc)
		if err != nil {
			return nil, err
		}
		if mc.Role == llms.RoleSystem {
			config.SystemInstruction = content
			continue
		}
		history = append(history, content)
	}

	// When no streaming is requested, just call GenerateContent and return
	// the complete response with a list of candidates.
	resp, err := g.client.Models.GenerateContent(ctx, g.opts.DefaultModel, history, config)
	if err != nil {
		return nil, err
	}

	if len(resp.Candidates) == 0 {
		return nil, ErrNoContentInResponse
	}
	return convertCandidates(resp.Candidates, resp.UsageMetadata)
}

/*
TODO: implement streaming

// convertAndStreamFromIterator takes an iterator of GenerateContentResponse
// and produces a llms.ContentResponse reply from it, while streaming the
// resulting text into the opts-provided streaming function.
// Note that this is tricky in the face of multiple
// candidates, so this code assumes only a single candidate for now.
func convertAndStreamFromIterator(
	ctx context.Context,
	iter iter.Seq2[*genai.GenerateContentResponse, error],
	opts *llms.CallOptions,
) (*llms.ContentResponse, error) {
	candidate := &genai.Candidate{
		Content: &genai.Content{},
	}
DoStream:
	for {
		resp, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break DoStream
		}
		if err != nil {
			return nil, errors.Wrap(err, "error in stream mode")
		}

		if len(resp.Candidates) != 1 {
			return nil, errors.Errorf("expect single candidate in stream mode; got %v", len(resp.Candidates))
		}
		respCandidate := resp.Candidates[0]

		if respCandidate.Content == nil {
			break DoStream
		}
		candidate.Content.Parts = append(candidate.Content.Parts, respCandidate.Content.Parts...)
		candidate.Content.Role = respCandidate.Content.Role
		candidate.FinishReason = respCandidate.FinishReason
		candidate.SafetyRatings = respCandidate.SafetyRatings
		candidate.CitationMetadata = respCandidate.CitationMetadata
		// candidate.TokenCount += respCandidate.TokenCount

		for _, part := range respCandidate.Content.Parts {
			if text, ok := part.(genai.Text); ok {
				if opts.StreamingFunc(ctx, []byte(text)) != nil {
					break DoStream
				}
			}
		}
	}
	// mresp := iter.MergedResponse()
	return convertCandidates([]*genai.Candidate{candidate}, nil)
}
*/

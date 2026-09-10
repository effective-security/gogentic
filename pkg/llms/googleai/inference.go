package googleai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/google/uuid"
	"google.golang.org/genai"
)

const (
	// syntheticCallPrefix marks function call IDs minted by this connector when
	// Gemini returns none. Such IDs are stripped before they reach Gemini, which
	// matches responses to calls by order and name.
	syntheticCallPrefix = "gfc_"
	// thoughtSignatureKey is the JSON member carrying a Gemini thought signature
	// inside an opaque reasoning payload.
	thoughtSignatureKey = "thoughtSignature"
	// functionOutputKey wraps a string function result for Gemini.
	functionOutputKey = "output"
	// maxInferenceResponseBytes bounds a buffered upstream response.
	maxInferenceResponseBytes = 16 << 20
	oneAttempt                = int32(1)
	paramReasoning            = "reasoning"
	paramParallelToolCalls    = "parallel_tool_calls"
	paramStrictTools          = "tools.strict"
	paramDeveloperRole        = "messages.role.developer"
	paramTemperature          = "temperature"
	paramMaxTokens            = "max_tokens"
	opGoogleInference         = "googleai text inference"
	opDecodeInference         = "decode googleai inference"
	opDecodeArguments         = "decode function arguments"
)

// captureKey locates the per-request raw response holder in a context.
type captureKey struct{}

// rawCapture receives the raw upstream body for one inference.
type rawCapture struct {
	body []byte
}

// captureTransport wraps the client's transport once. It buffers a response
// only when the request context carries a rawCapture; every other request
// passes through untouched, so legacy generation is unaffected.
type captureTransport struct {
	base http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (c *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := c.base.RoundTrip(req)
	if err != nil {
		return nil, errors.WithMessage(err, "send googleai request")
	}
	holder, ok := req.Context().Value(captureKey{}).(*rawCapture)
	if !ok || holder == nil {
		return res, nil
	}
	data, readErr := io.ReadAll(io.LimitReader(res.Body, maxInferenceResponseBytes+1))
	closeErr := res.Body.Close()
	if readErr != nil {
		return nil, errors.WithMessage(readErr, "read googleai inference")
	}
	if closeErr != nil {
		return nil, errors.WithMessage(closeErr, "close googleai response")
	}
	if len(data) > maxInferenceResponseBytes {
		return nil, errors.Errorf("googleai response exceeds %d bytes", maxInferenceResponseBytes)
	}
	holder.body = data
	res.Body = io.NopCloser(bytes.NewReader(data))
	return res, nil
}

// wrapInferenceTransport rebuilds the client once with a capturing transport
// around the SDK's resolved (possibly authenticated) HTTP client.
func wrapInferenceTransport(ctx context.Context, client *genai.Client) (*genai.Client, error) {
	cfg := client.ClientConfig()
	if cfg.HTTPClient == nil {
		return client, nil
	}
	httpClient := *cfg.HTTPClient
	base := httpClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	httpClient.Transport = &captureTransport{
		base: base,
	}
	cfg.HTTPClient = &httpClient
	wrapped, err := genai.NewClient(ctx, &cfg)
	if err != nil {
		return nil, errors.WithMessage(err, "construct googleai inference client")
	}
	return wrapped, nil
}

// ValidateInference checks controls unsupported by the portable Gemini connector.
func (g *GoogleAI) ValidateInference(r llms.InferenceRequest) error {
	if r.ParallelToolCalls != nil {
		return llms.UnsupportedInference(paramParallelToolCalls)
	}
	for _, t := range r.Tools {
		if t.Strict != nil && *t.Strict {
			return llms.UnsupportedInference(paramStrictTools)
		}
	}
	for _, m := range r.Messages {
		if m.Role == llms.InferenceRoleDeveloper {
			return llms.UnsupportedInference(paramDeveloperRole)
		}
	}
	if r.Reasoning != nil {
		if _, err := reasoningBudget(r.Reasoning); err != nil {
			return err
		}
	}
	return nil
}

func reasoningBudget(r *llms.InferenceReasoning) (int32, error) {
	if r.BudgetTokens != nil {
		return int32(*r.BudgetTokens), nil
	}
	budget, ok := llms.ReasoningBudget(r.Effort)
	if !ok {
		return 0, llms.UnsupportedInference(paramReasoning)
	}
	return int32(budget), nil
}

// Infer preserves raw JSON schemas, function IDs, explicit zero sampling values
// and thought signatures.
func (g *GoogleAI) Infer(ctx context.Context, r llms.InferenceRequest) (*llms.InferenceResponse, error) {
	if err := g.ValidateInference(r); err != nil {
		return nil, err
	}
	cfg, err := g.inferenceConfig(r)
	if err != nil {
		return nil, err
	}
	history, err := inferenceHistory(r.Messages, cfg)
	if err != nil {
		return nil, err
	}
	// The SDK's map conversion rounds arbitrary JSON numbers. Restore raw request
	// fields after SDK conversion and read raw response arguments before decoding.
	cfg.HTTPOptions.ExtrasRequestProvider = func(body map[string]any) map[string]any {
		body["contents"] = history
		if len(cfg.Tools) > 0 {
			body["tools"] = cfg.Tools
		}
		if r.Format != nil && r.Format.Type == llms.FormatJSONSchema {
			generation, ok := body["generationConfig"].(map[string]any)
			if !ok {
				generation = map[string]any{}
				body["generationConfig"] = generation
			}
			generation["responseJsonSchema"] = r.Format.Schema
		}
		return body
	}
	holder := &rawCapture{}
	ctx = context.WithValue(ctx, captureKey{}, holder)
	response, err := g.client.Models.GenerateContent(ctx, r.Model, history, cfg)
	if err != nil {
		status := http.StatusBadGateway
		var apiErr genai.APIError
		if errors.As(err, &apiErr) {
			status = apiErr.Code
		}
		return nil, llms.InferenceFailure(err, status, opGoogleInference)
	}
	if response == nil || len(holder.body) == 0 {
		return nil, llms.InferenceFailure(errors.New("empty response"), http.StatusBadGateway, opDecodeInference)
	}
	return decodeInferenceResponse(holder.body)
}

func (g *GoogleAI) inferenceConfig(r llms.InferenceRequest) (*genai.GenerateContentConfig, error) {
	attempts := oneAttempt
	cfg := &genai.GenerateContentConfig{
		HTTPOptions: &genai.HTTPOptions{
			RetryOptions: &genai.HTTPRetryOptions{
				Attempts: &attempts,
			},
		},
		StopSequences: r.Stop,
	}
	if r.Temperature != nil {
		v := float32(*r.Temperature)
		cfg.Temperature = &v
	}
	if r.TopP != nil {
		v := float32(*r.TopP)
		cfg.TopP = &v
	}
	if r.MaxTokens != nil {
		cfg.MaxOutputTokens = int32(*r.MaxTokens)
	}
	if r.Seed != nil {
		v := int32(*r.Seed)
		cfg.Seed = &v
	}
	if r.Reasoning != nil {
		budget, err := reasoningBudget(r.Reasoning)
		if err != nil {
			return nil, err
		}
		cfg.ThinkingConfig = &genai.ThinkingConfig{
			IncludeThoughts: true,
			ThinkingBudget:  &budget,
		}
	}
	if f := r.Format; f != nil && f.Type != llms.FormatText {
		cfg.ResponseMIMEType = ResponseMIMETypeJson
		if f.Type == llms.FormatJSONSchema {
			cfg.ResponseJsonSchema = f.Schema
		}
	}
	if toolCount := len(r.Tools); toolCount > 0 {
		declarations := make([]*genai.FunctionDeclaration, 0, toolCount)
		for _, t := range r.Tools {
			declarations = append(declarations, &genai.FunctionDeclaration{
				Name:                 t.Name,
				Description:          t.Description,
				ParametersJsonSchema: t.Parameters,
			})
		}
		cfg.Tools = []*genai.Tool{{
			FunctionDeclarations: declarations,
		}}
	}
	if c := r.ToolChoice; c != nil {
		fc := &genai.FunctionCallingConfig{
			Mode: genai.FunctionCallingConfigModeAuto,
		}
		switch c.Mode {
		case llms.ToolChoiceNone:
			fc.Mode = genai.FunctionCallingConfigModeNone
		case llms.ToolChoiceRequired:
			fc.Mode = genai.FunctionCallingConfigModeAny
		case llms.ToolChoiceFunction:
			fc.Mode = genai.FunctionCallingConfigModeAny
			fc.AllowedFunctionNames = []string{c.Name}
		}
		cfg.ToolConfig = &genai.ToolConfig{
			FunctionCallingConfig: fc,
		}
	}
	return cfg, nil
}

// inferenceHistory converts messages to Gemini contents. System messages become
// the system instruction; consecutive same-role contents are merged so that all
// function responses for one assistant turn share a single user turn.
func inferenceHistory(messages []llms.InferenceMessage, cfg *genai.GenerateContentConfig) ([]*genai.Content, error) {
	var history []*genai.Content
	for _, m := range messages {
		parts, err := inferenceParts(m.Content)
		if err != nil {
			return nil, err
		}
		if m.Role == llms.InferenceRoleSystem {
			if cfg.SystemInstruction == nil {
				cfg.SystemInstruction = &genai.Content{}
			}
			cfg.SystemInstruction.Parts = append(cfg.SystemInstruction.Parts, parts...)
			continue
		}
		role := RoleUser
		if m.Role == llms.InferenceRoleAssistant {
			role = RoleModel
		}
		if n := len(history); n > 0 && history[n-1].Role == role {
			history[n-1].Parts = append(history[n-1].Parts, parts...)
			continue
		}
		history = append(history, &genai.Content{
			Role:  role,
			Parts: parts,
		})
	}
	return history, nil
}

// inferenceParts converts blocks to parts. A reasoning block's signature is
// attached to the next non-reasoning part of the same message.
func inferenceParts(blocks []llms.InferenceBlock) ([]*genai.Part, error) {
	var parts []*genai.Part
	var pendingSignature []byte
	for _, b := range blocks {
		if b.Type == llms.BlockReasoning {
			signature, err := opaqueSignature(b.Opaque)
			if err != nil {
				return nil, err
			}
			if len(signature) > 0 {
				pendingSignature = signature
			}
			continue
		}
		p := &genai.Part{}
		switch b.Type {
		case llms.BlockText, llms.BlockRefusal:
			p.Text = b.Text
		case llms.BlockFunctionCall:
			var args map[string]any
			decoder := json.NewDecoder(strings.NewReader(b.Arguments))
			decoder.UseNumber()
			if err := decoder.Decode(&args); err != nil {
				return nil, llms.InferenceFailure(err, http.StatusBadRequest, opDecodeArguments)
			}
			p.FunctionCall = &genai.FunctionCall{
				ID:   upstreamCallID(b.ID),
				Name: b.Name,
				Args: args,
			}
		case llms.BlockFunctionOutput:
			p.FunctionResponse = &genai.FunctionResponse{
				ID:   upstreamCallID(b.ID),
				Name: b.Name,
				Response: map[string]any{
					functionOutputKey: b.Text,
				},
			}
		default:
			continue
		}
		if pendingSignature != nil {
			p.ThoughtSignature = pendingSignature
			pendingSignature = nil
		}
		parts = append(parts, p)
	}
	return parts, nil
}

// upstreamCallID removes IDs this connector minted; Gemini never issued them.
func upstreamCallID(id string) string {
	if strings.HasPrefix(id, syntheticCallPrefix) {
		return ""
	}
	return id
}

func opaqueSignature(opaque json.RawMessage) ([]byte, error) {
	if len(opaque) == 0 {
		return nil, nil
	}
	var payload struct {
		ThoughtSignature string `json:"thoughtSignature"`
	}
	if err := json.Unmarshal(opaque, &payload); err != nil {
		return nil, llms.InferenceFailure(err, http.StatusBadRequest, "decode reasoning state")
	}
	if payload.ThoughtSignature == "" {
		return nil, nil
	}
	signature, err := base64.StdEncoding.DecodeString(payload.ThoughtSignature)
	if err != nil {
		return nil, llms.InferenceFailure(err, http.StatusBadRequest, "decode thought signature")
	}
	return signature, nil
}

// rawResponse mirrors the Gemini generateContent wire response for the fields
// the portable contract needs, keeping numbers and signatures raw.
type rawResponse struct {
	ResponseID string `json:"responseId"`
	Candidates []struct {
		FinishReason string `json:"finishReason"`
		Content      struct {
			Parts []struct {
				Text             string `json:"text"`
				Thought          bool   `json:"thought"`
				ThoughtSignature string `json:"thoughtSignature"`
				FunctionCall     *struct {
					ID   string          `json:"id"`
					Name string          `json:"name"`
					Args json.RawMessage `json:"args"`
				} `json:"functionCall"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	PromptFeedback *struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	UsageMetadata *struct {
		PromptTokenCount        uint64 `json:"promptTokenCount"`
		CandidatesTokenCount    uint64 `json:"candidatesTokenCount"`
		ThoughtsTokenCount      uint64 `json:"thoughtsTokenCount"`
		CachedContentTokenCount uint64 `json:"cachedContentTokenCount"`
		TotalTokenCount         uint64 `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func decodeInferenceResponse(raw []byte) (*llms.InferenceResponse, error) {
	var response rawResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, llms.InferenceFailure(err, http.StatusBadGateway, opDecodeInference)
	}
	out := &llms.InferenceResponse{
		UpstreamID: response.ResponseID,
	}
	if u := response.UsageMetadata; u != nil {
		out.UsageKnown = true
		out.Usage = llms.Usage{
			InputTokens:     u.PromptTokenCount,
			OutputTokens:    u.CandidatesTokenCount + u.ThoughtsTokenCount,
			ReasoningTokens: u.ThoughtsTokenCount,
			CacheReadTokens: u.CachedContentTokenCount,
			TotalTokens:     u.TotalTokenCount,
		}
	}
	if len(response.Candidates) == 0 {
		if response.PromptFeedback != nil && response.PromptFeedback.BlockReason != "" {
			out.FinishReason = llms.FinishContentFilter
			return out, nil
		}
		return nil, llms.InferenceFailure(errors.New("expected one candidate"), http.StatusBadGateway, opDecodeInference)
	}
	if len(response.Candidates) != 1 {
		return nil, llms.InferenceFailure(errors.New("expected one candidate"), http.StatusBadGateway, opDecodeInference)
	}
	c := response.Candidates[0]
	finish, err := inferenceFinishReason(genai.FinishReason(c.FinishReason))
	if err != nil {
		return nil, err
	}
	out.FinishReason = finish
	for _, p := range c.Content.Parts {
		if p.ThoughtSignature != "" || p.Thought {
			block := llms.InferenceBlock{
				Type: llms.BlockReasoning,
			}
			if p.Thought {
				block.Text = p.Text
			}
			if p.ThoughtSignature != "" {
				opaque, err := json.Marshal(map[string]string{
					thoughtSignatureKey: p.ThoughtSignature,
				})
				if err != nil {
					return nil, llms.InferenceFailure(err, http.StatusBadGateway, "encode thought signature")
				}
				block.Opaque = opaque
			}
			out.Content = append(out.Content, block)
			if p.Thought {
				continue
			}
		}
		switch {
		case p.FunctionCall != nil:
			if len(p.FunctionCall.Args) == 0 {
				return nil, llms.InferenceFailure(errors.New("missing function arguments"), http.StatusBadGateway, opDecodeInference)
			}
			id := p.FunctionCall.ID
			if id == "" {
				id = syntheticCallPrefix + uuid.NewString()
			}
			out.Content = append(out.Content, llms.InferenceBlock{
				Type:      llms.BlockFunctionCall,
				ID:        id,
				Name:      p.FunctionCall.Name,
				Arguments: string(p.FunctionCall.Args),
			})
			if out.FinishReason == llms.FinishStop {
				out.FinishReason = llms.FinishToolCalls
			}
		case p.Text != "":
			out.Content = append(out.Content, llms.InferenceBlock{
				Type: llms.BlockText,
				Text: p.Text,
			})
		default:
			return nil, llms.InferenceFailure(errors.New("unsupported content part"), http.StatusBadGateway, opDecodeInference)
		}
	}
	return out, nil
}

func inferenceFinishReason(reason genai.FinishReason) (llms.FinishReason, error) {
	switch reason {
	case genai.FinishReasonStop:
		return llms.FinishStop, nil
	case genai.FinishReasonMaxTokens:
		return llms.FinishLength, nil
	case genai.FinishReasonSafety, genai.FinishReasonRecitation, genai.FinishReasonProhibitedContent,
		genai.FinishReasonBlocklist, genai.FinishReasonSPII:
		return llms.FinishContentFilter, nil
	}
	return "", llms.InferenceFailure(errors.Errorf("unsupported finish reason %q", reason), http.StatusBadGateway, opDecodeInference)
}

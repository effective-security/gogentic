package llms

import (
	"context"
	"strings"

	"github.com/cockroachdb/errors"
)

// ProviderType is the type of provider.
type ProviderType string

const (
	// ProviderAnthropic is the type of provider for Anthropic models.
	ProviderAnthropic ProviderType = "ANTHROPIC"
	// ProviderAnthropicBedrock is the type of provider for Anthropic models on Bedrock Mantle.
	ProviderAnthropicBedrock ProviderType = "ANTHROPIC_BEDROCK"
	// ProviderAzure is the type of provider for Azure OpenAI models.
	ProviderAzure ProviderType = "AZURE"
	// ProviderAzureAD is the type of provider for Azure OpenAI models with Azure AD authentication.
	ProviderAzureAD ProviderType = "AZURE_AD"
	// ProviderBedrock is the type of provider for Bedrock Mantle models.
	ProviderBedrock ProviderType = "BEDROCK"
	// ProviderGoogleAI is the type of provider for Google AI models.
	ProviderGoogleAI ProviderType = "GOOGLEAI"
	// ProviderOpenAI is the type of provider for OpenAI models.
	ProviderOpenAI ProviderType = "OPENAI"
	// ProviderOpenAIBedrock is the type of provider for OpenAI models on Bedrock Mantle.
	ProviderOpenAIBedrock ProviderType = "OPENAI_BEDROCK"
	// ProviderOpenRouter is the type of provider for OpenRouter models.
	ProviderOpenRouter ProviderType = "OPENROUTER"
)

// ParseProviderType converts a provider type name or one of its common aliases
// into a ProviderType. Matching ignores case, so callers that must tell a
// provider prefix from a lower-case vendor prefix in a model ID (for example
// "openai/gpt-oss-120b") have to check the case themselves before calling it.
func ParseProviderType(s string) (ProviderType, error) {
	switch strings.ToUpper(s) {
	case "ANTHROPIC", "CLAUDE":
		return ProviderAnthropic, nil
	case "ANTHROPIC_BEDROCK", "BEDROCK_ANTHROPIC":
		return ProviderAnthropicBedrock, nil
	case "AZURE", "AZURE_OPENAI":
		return ProviderAzure, nil
	case "AZURE_AD":
		return ProviderAzureAD, nil
	case "BEDROCK":
		return ProviderBedrock, nil
	case "GOOGLEAI", "GOOGLE_AI", "GEMINI":
		return ProviderGoogleAI, nil
	case "OPENAI", "OPEN_AI":
		return ProviderOpenAI, nil
	case "OPENAI_BEDROCK", "BEDROCK_OPENAI":
		return ProviderOpenAIBedrock, nil
	case "OPENROUTER":
		return ProviderOpenRouter, nil
	default:
		return "", errors.Errorf("invalid provider type: %s", s)
	}
}

// Model is an interface multi-modal models implement.
type Model interface {
	// GetName returns the name of the model.
	GetName() string
	// GetProviderType returns the type of provider.
	GetProviderType() ProviderType
	// GenerateContent asks the model to generate content from a sequence of
	// messages. It's the most general interface for multi-modal LLMs that support
	// chat-like interactions.
	GenerateContent(ctx context.Context, messages []Message, options ...CallOption) (*ContentResponse, error)
}

// Embedder is an interface for models that can create embeddings.
type Embedder interface {
	// CreateEmbedding creates embeddings for the given input texts.
	CreateEmbedding(ctx context.Context, texts []string) ([][]float32, error)
}

// Capability is a bitmask indicating supported features of an LLM provider.
type Capability uint64

const (
	// Basic text or chat generation
	CapabilityText Capability = 1 << iota

	// Structured response formats
	CapabilityJSONResponse
	CapabilityJSONSchema
	CapabilityJSONSchemaStrict

	// Function/tool calling
	CapabilityFunctionCalling
	CapabilityMultiToolCalling
	CapabilityToolCallStreaming

	// Multimodal (images, audio, etc.)
	CapabilityVision
	CapabilityImageGeneration
	CapabilityAudioTranscription

	// Open weight models / self-hosted
	CapabilitySelfHosted

	// System prompt support
	CapabilitySystemPrompt

	// Web Search tool support, used by models that support web search grounding.
	CapabilityWebSearchTool

	// Prompt Caching
	CapabilityPromptCaching

	// Asynchronous batch processing via the provider's Batch API.
	// Providers that advertise this capability also implement [Batcher].
	CapabilityBatch

	// The provider can list the models an account may call.
	// Providers that advertise this capability also implement [ModelLister].
	CapabilityModels
)

var providerCapabilities = map[ProviderType]Capability{
	ProviderOpenAI: CapabilityText |
		CapabilityJSONResponse |
		CapabilityJSONSchema |
		CapabilityJSONSchemaStrict |
		CapabilityFunctionCalling |
		CapabilityMultiToolCalling |
		CapabilityToolCallStreaming |
		CapabilitySystemPrompt |
		CapabilityVision |
		CapabilityWebSearchTool |
		CapabilityPromptCaching |
		CapabilityBatch |
		CapabilityModels,

	ProviderOpenAIBedrock: CapabilityText |
		CapabilityJSONResponse |
		CapabilityJSONSchema |
		CapabilityJSONSchemaStrict |
		CapabilityFunctionCalling |
		CapabilityMultiToolCalling |
		CapabilityToolCallStreaming |
		CapabilitySystemPrompt |
		CapabilityVision |
		CapabilityPromptCaching |
		CapabilityBatch |
		CapabilityWebSearchTool,

	ProviderOpenRouter: CapabilityText |
		CapabilityJSONResponse |
		CapabilityJSONSchema |
		CapabilityJSONSchemaStrict |
		CapabilityFunctionCalling |
		CapabilityMultiToolCalling |
		CapabilityToolCallStreaming |
		CapabilitySystemPrompt |
		CapabilityVision |
		CapabilityWebSearchTool |
		CapabilityModels,

	ProviderAnthropic: CapabilityText |
		CapabilityJSONResponse |
		CapabilityJSONSchema |
		CapabilityFunctionCalling |
		CapabilityMultiToolCalling |
		CapabilitySystemPrompt |
		CapabilityWebSearchTool |
		CapabilityPromptCaching |
		CapabilityModels,

	ProviderAnthropicBedrock: CapabilityText |
		CapabilityJSONResponse |
		CapabilityJSONSchema |
		CapabilityFunctionCalling |
		CapabilityMultiToolCalling |
		CapabilitySystemPrompt,
	//CapabilityWebSearchTool |
	//CapabilityPromptCaching,

	ProviderGoogleAI: CapabilityText |
		CapabilitySystemPrompt |
		CapabilityJSONResponse |
		//CapabilityJSONSchema |
		CapabilityFunctionCalling |
		CapabilityMultiToolCalling |
		CapabilityVision |
		CapabilityWebSearchTool |
		CapabilityModels,

	// Use Bedrock with Anthropic models
	ProviderBedrock: CapabilityText |
		CapabilityJSONResponse |
		CapabilityFunctionCalling |
		CapabilityMultiToolCalling |
		CapabilitySystemPrompt,

	ProviderAzure: CapabilityText |
		CapabilityJSONResponse |
		CapabilityJSONSchema |
		CapabilityJSONSchemaStrict |
		CapabilityFunctionCalling |
		CapabilityMultiToolCalling |
		CapabilitySystemPrompt,
	//CapabilityPromptCaching,

	ProviderAzureAD: CapabilityText, // Proxy passthrough
}

// ProviderCapabilities returns the capability mask for a provider type, or
// zero for an unknown type.
func ProviderCapabilities(pt ProviderType) Capability {
	return providerCapabilities[pt]
}

// Supports reports whether the provider supports ANY of the requested
// capabilities. Passing an OR of several bits therefore does not mean "all of
// them": for an all-of check use llmfactory.ModelOptions.RequiredCapabilities,
// or compare the mask directly:
//
//	llms.ProviderCapabilities(pt)&required == required
func (p ProviderType) Supports(cap Capability) bool {
	return ProviderCapabilities(p)&cap != 0
}

// GenerateFromSinglePrompt is a convenience function for calling an LLM with
// a single string prompt, expecting a single string response. It's useful for
// simple, string-only interactions and provides a slightly more ergonomic API
// than the more general [llms.Model.GenerateContent].
func GenerateFromSinglePrompt(ctx context.Context, llm Model, prompt string, options ...CallOption) (string, error) {
	msg := Message{
		Role:  RoleHuman,
		Parts: []ContentPart{TextContent{Text: prompt}},
	}

	resp, err := llm.GenerateContent(ctx, []Message{msg}, options...)
	if err != nil {
		return "", err
	}

	choices := resp.Choices
	if len(choices) < 1 {
		return "", errors.New("empty response from model")
	}
	c1 := choices[0]
	return c1.Content, nil
}

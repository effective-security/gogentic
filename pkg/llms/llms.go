package llms

import (
	"context"

	"github.com/cockroachdb/errors"
)

// ProviderType is the type of provider.
type ProviderType string

const (
	// ProviderAnthropic is the type of provider.
	ProviderAnthropic ProviderType = "ANTHROPIC"
	// ProviderAzure is the type of provider.
	ProviderAzure ProviderType = "AZURE"
	// ProviderAzureAD is the type of provider.
	ProviderAzureAD ProviderType = "AZURE_AD"
	// ProviderBedrock is the type of provider.
	ProviderBedrock ProviderType = "BEDROCK"
	// ProviderCloudflare is the type of provider.
	ProviderCloudflare ProviderType = "CLOUDFLARE"
	// ProviderGoogleAI is the type of provider.
	ProviderGoogleAI ProviderType = "GOOGLEAI"
	// ProviderOpenAI is the type of provider.
	ProviderOpenAI ProviderType = "OPENAI"
	// ProviderPerplexity is the type of provider.
	ProviderPerplexity ProviderType = "PERPLEXITY"
)

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
		CapabilityVision,

	ProviderAnthropic: CapabilityText |
		CapabilityJSONResponse |
		CapabilityFunctionCalling |
		CapabilityMultiToolCalling |
		CapabilitySystemPrompt,

	ProviderGoogleAI: CapabilityText |
		CapabilitySystemPrompt |
		CapabilityJSONResponse |
		CapabilityFunctionCalling |
		CapabilityMultiToolCalling |
		CapabilityVision,

	// Use Bedrock with Anthropic models
	ProviderBedrock: CapabilityText |
		CapabilityJSONResponse |
		CapabilityFunctionCalling |
		CapabilityMultiToolCalling |
		CapabilitySystemPrompt,

	ProviderCloudflare: CapabilityText,

	ProviderPerplexity: CapabilityText |
		CapabilitySystemPrompt |
		CapabilityJSONResponse |
		CapabilityJSONSchema |
		CapabilityJSONSchemaStrict,

	ProviderAzure: CapabilityText |
		CapabilityJSONResponse |
		CapabilityJSONSchema |
		CapabilityJSONSchemaStrict |
		CapabilityFunctionCalling |
		CapabilityMultiToolCalling |
		CapabilitySystemPrompt,

	ProviderAzureAD: CapabilityText, // Proxy passthrough
}

func ProviderCapabilities(pt ProviderType) Capability {
	return providerCapabilities[pt]
}

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

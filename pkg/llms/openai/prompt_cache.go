package openai

import (
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/openai/internal/openaiclient"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
)

type promptCacheRequestConfig struct {
	key          string
	retention    llms.PromptCacheRetention
	hasKey       bool
	hasRetention bool
}

func supportsPromptCacheProvider(provider openaiclient.ProviderType) bool {
	return provider == openaiclient.ProviderOpenAI ||
		provider == "OPEN_AI" ||
		provider == openaiclient.ProviderAzure ||
		provider == openaiclient.ProviderAzureAD
}

func resolvePromptCacheRequestConfig(opts *llms.CallOptions) promptCacheRequestConfig {
	if opts == nil {
		return promptCacheRequestConfig{}
	}

	if opts.PromptCachePolicy == nil || opts.PromptCachePolicy.Request == nil {
		return promptCacheRequestConfig{}
	}

	cfg := promptCacheRequestConfig{}
	if opts.PromptCachePolicy.Request.Key != "" {
		cfg.key = opts.PromptCachePolicy.Request.Key
		cfg.hasKey = true
	}
	if opts.PromptCachePolicy.Request.Retention != "" {
		cfg.retention = opts.PromptCachePolicy.Request.Retention
		cfg.hasRetention = true
	}
	return cfg
}

func applyPromptCacheToChatRequest(req *openaiclient.ChatRequest, provider openaiclient.ProviderType, opts *llms.CallOptions) {
	if req == nil || !supportsPromptCacheProvider(provider) {
		return
	}

	cfg := resolvePromptCacheRequestConfig(opts)
	if cfg.hasKey {
		req.PromptCacheKey = cfg.key
	}
	if cfg.hasRetention {
		req.PromptCacheRetention = toChatPromptCacheRetention(cfg.retention)
	}
}

// toChatPromptCacheRetention maps the internal PromptCacheRetention constant to
// the wire value expected by the OpenAI Chat Completions API.
// The API accepts "in_memory" (underscore) and "24h"; the internal constant uses "in-memory" (hyphen).
func toChatPromptCacheRetention(retention llms.PromptCacheRetention) string {
	switch retention {
	case llms.PromptCacheRetentionInMemory:
		return "in_memory"
	default:
		return string(retention)
	}
}

func applyPromptCacheToResponsesRequest(req *responses.ResponseNewParams, provider openaiclient.ProviderType, opts *llms.CallOptions) {
	if req == nil || !supportsPromptCacheProvider(provider) {
		return
	}

	cfg := resolvePromptCacheRequestConfig(opts)
	if cfg.hasRetention {
		req.PromptCacheRetention = toResponsesPromptCacheRetention(cfg.retention)
	}
	if cfg.hasKey {
		req.PromptCacheKey = param.NewOpt(cfg.key)
	}
}

// toResponsesPromptCacheRetention maps the internal retention constant to the wire value
// required by the OpenAI Responses API.
// NOTE: The SDK constant ResponseNewParamsPromptCacheRetentionInMemory is "in-memory" (hyphen),
// but the API requires "in_memory" (underscore). We use the correct wire value directly until
// the SDK is updated.
func toResponsesPromptCacheRetention(retention llms.PromptCacheRetention) responses.ResponseNewParamsPromptCacheRetention {
	switch retention {
	case llms.PromptCacheRetentionInMemory:
		return "in_memory"
	case llms.PromptCacheRetention24h:
		return responses.ResponseNewParamsPromptCacheRetention24h
	default:
		return responses.ResponseNewParamsPromptCacheRetention(retention)
	}
}

// Package llmfactory constructs provider models from configuration, resolves
// assistant→model preferences (with optional per-organization overrides), and
// supports capability filtering and per-org model restrictions.
//
// It is the only place in the module that knows how to build a provider
// client, so application code and assistants stay provider-agnostic.
//
// # 1. Configuration
//
// Providers, defaults and assistant mappings are declared in YAML. String
// values support env:// and file:// expansion, so tokens stay out of the file.
//
//	default_provider: OPENAI
//	providers:
//	  - name: OPENAI
//	    token: env://OPENAI_API_KEY
//	    default_model: gpt-5.1
//	    available_models: [gpt-5.1, gpt-5.1-mini]
//	    open_ai:
//	      api_type: OPENAI
//	  - name: ANTHROPIC
//	    token: env://ANTHROPIC_API_KEY
//	    default_model: claude-sonnet-4-8
//	    available_models: [claude-opus-4-8, claude-sonnet-4-8]
//	    open_ai:
//	      api_type: ANTHROPIC
//	assistant_models:
//	  default: [OPENAI/gpt-5.1]
//	  coder:   [OPENAI/gpt-5.1, ANTHROPIC/claude-opus-4-8]
//	orgs_override:
//	  "1000":
//	    assistant_models:
//	      default: [OPENAI/gpt-5.1-mini]
//
// open_ai.api_type selects the provider family and therefore the capability
// set: OPENAI|AZURE|AZURE_AD|OPENROUTER|PERPLEXITY|OPENAI_BEDROCK|ANTHROPIC|
// ANTHROPIC_BEDROCK|BEDROCK|GOOGLEAI|CLOUDFLARE.
//
// # 2. Load and resolve a model
//
//	fac, err := llmfactory.Load("llm.yaml")
//	if err != nil {
//	    return err
//	}
//
//	// Resolve the model configured for the "coder" assistant, falling back to
//	// assistant_models.default and then to the default provider's default_model.
//	model, err := fac.GetModel(ctx, llmfactory.ModelOptions{AssistantName: "coder"})
//
// assistants.Assistant calls GetModel with its own Name() and the org ID from
// the chatmodel.ChatContext, so naming an assistant is what binds it to an
// assistant_models entry.
//
// # 3. Require capabilities
//
// RequiredCapabilities is an all-of mask: only providers whose type supports
// every requested capability are considered. This is stricter than
// llms.ProviderType.Supports, which reports whether any requested bit is set.
//
//	model, err := fac.GetModel(ctx, llmfactory.ModelOptions{
//	    AssistantName: "coder",
//	    RequiredCapabilities: llms.CapabilityJSONSchema |
//	        llms.CapabilityFunctionCalling,
//	})
//
// # 4. Restrict models per organization
//
// A [ModelFilterFunc] is consulted for every candidate, including the
// default-model fallback. Use it for quota, entitlements or cost policy.
//
//	filter := func(ctx context.Context, orgID, assistantName, modelName string) bool {
//	    if orgID == "free-tier" && strings.Contains(modelName, "opus") {
//	        return false // block expensive models for this org
//	    }
//	    return true
//	}
//
//	fac := llmfactory.New(cfg, llmfactory.WithModelFilter(filter))
//	// or derive a restricted factory that shares the client caches:
//	tenantFactory := fac.WithModelFilter(filter)
//
// The assistantName argument is normalized to "default" when empty, and
// modelName may arrive bare or provider-qualified as "PROVIDER/model".
//
// See Documentation/llm-factory.md for the full resolution rules, the skills
// section, and testing hooks such as the overridable [NewLLM] variable.
package llmfactory

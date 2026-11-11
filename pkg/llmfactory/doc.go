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
// set: OPENAI|AZURE|AZURE_AD|OPENROUTER|OPENAI_BEDROCK|ANTHROPIC|
// ANTHROPIC_BEDROCK|BEDROCK|GOOGLEAI.
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
// modelName may arrive bare or provider-qualified as "PROVIDER/model". It is
// always a concrete model ID, never a pattern, and carries no effort suffix.
//
// # 5. Effort, patterns and the deny-list
//
// A model reference is [PROVIDER/]MODEL[,EFFORT]. The effort after the comma
// (none, low, medium, high, xhigh or max) is used when a call does not set one
// with WithReasoningEffort. "*" and "?" turn a model name into a pattern.
//
//	providers:
//	  - name: ANTHROPIC
//	    token: env://ANTHROPIC_API_KEY
//	    default_model: claude-sonnet-4-8,medium
//	    available_models:
//	      - claude-opus-4-8,high
//	      - claude-sonnet-4-8
//	      - claude-haiku-*,low
//	    disabled_models:
//	      - "*-preview"
//	    max_effort: xhigh
//	    open_ai:
//	      api_type: ANTHROPIC
//	assistant_models:
//	  coder:
//	    - ANTHROPIC/claude-sonnet-*,high
//
// available_models stays the allow-list, disabled_models wins over it, and
// max_effort caps every call to the provider. A pattern in assistant_models
// selects the best match among the models listed by name in available_models.
// Each level is sent as the nearest one the selected model accepts.
//
// [LoadConfig] validates the references and effort levels. [New] cannot fail,
// so for a Config built in code call [Config.Validate] first.
//
// # 6. Model discovery
//
// Providers retire models and ship new ones. With a discovery section the
// factory reads the model list from the provider (OPENAI, OPENROUTER,
// ANTHROPIC and GOOGLEAI), so that a pattern selects models nobody listed and
// a model the provider no longer serves gives way to the newest allowed model
// of its family. Without the section nothing changes.
//
//	providers:
//	  - name: ANTHROPIC
//	    token: env://ANTHROPIC_API_KEY
//	    default_model: claude-sonnet-*,medium
//	    available_models:
//	      - claude-fable-*,high
//	      - claude-sonnet-*
//	    discovery:
//	      enabled: true
//	      refresh: 6h
//	    open_ai:
//	      api_type: ANTHROPIC
//	assistant_models:
//	  coder:
//	    - ANTHROPIC/claude-fable-5-1
//
// When the provider retires claude-fable-5-1, "coder" resolves to the newest
// model that claude-fable-* allows. available_models stays the only grant of
// access. The list is fetched on first use and refreshed in the background; a
// failed fetch keeps the last list and never fails GetModel.
//
// [Resolve] reports what a reference selects right now, without building a
// client, and [RefreshModels] reads the lists on demand:
//
//	res, err := llmfactory.Resolve(ctx, fac, llmfactory.ModelOptions{AssistantName: "coder"})
//	if err != nil {
//	    return err
//	}
//	// res.Model "claude-fable-5-2", res.Reason llmfactory.ReasonFamily
//
// See Documentation/llm-factory.md for the full resolution rules, the effort
// mapping per provider, the skills section, and testing hooks such as the
// overridable [NewLLM] and [NewModelLister] variables.
package llmfactory

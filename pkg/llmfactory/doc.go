// Package llmfactory constructs provider models from configuration, resolves
// assistant→model preferences (with optional per‑org overrides), and supports
// capability filtering and per‑org model restrictions.
//
// Quick start
//
//  1. YAML configuration (providers, defaults, assistant mappings)
//
//     providers:
//     - name: openai
//     token: ${OPENAI_API_KEY}
//     available_models: ["gpt-4o-mini", "gpt-4o"]
//     open_ai:
//     api_type: OPENAI
//     - name: anthropic
//     token: ${ANTHROPIC_API_KEY}
//     available_models: ["claude-3-5-sonnet-20240620", "claude-3-5-haiku-latest"]
//     open_ai:
//     api_type: ANTHROPIC
//     default_provider: openai
//     assistant_models:
//     default: ["openai/gpt-4o-mini"]
//     coder:   ["openai/gpt-4o", "anthropic/claude-3-5-sonnet-20240620"]
//
//  2. Load and get a model
//
//     fac, _ := llmfactory.Load("config.yaml")
//     // Resolve model for the "coder" assistant, falling back to defaults
//     llm, err := fac.GetModel(ctx, llmfactory.ModelOptions{AssistantName: "coder"})
//     _ = llm; _ = err
//
//  3. Enforce capabilities and per‑org restrictions
//
//     // Only providers that support JSON Schema + Tool Calls will be considered
//     llm, _ = fac.GetModel(ctx, llmfactory.ModelOptions{
//     AssistantName:       "coder",
//     RequiredCapabilities: llms.CapabilityJSONSchema | llms.CapabilityToolCall,
//     })
//
//     // Install a per‑org model filter (e.g., quotas)
//     fac = fac.WithModelFilter(func(ctx context.Context, orgID, model string) bool {
//     if orgID == "free-tier" && strings.Contains(model, "gpt-4o") {
//     return false // block expensive models for this org
//     }
//     return true
//     })
//     llm, _ = fac.GetModel(ctx, llmfactory.ModelOptions{AssistantName: "coder", OrgID: "free-tier"})
package llmfactory

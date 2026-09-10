# LLM Factory

`pkg/llmfactory` turns YAML configuration into `llms.Model` instances. It is
the only place in the repo that knows about provider construction, so
application code and assistants stay provider-agnostic.

Source: [`pkg/llmfactory/`](../pkg/llmfactory) —
`config.go` (types + loading), `factory.go` (resolution + provider
construction), `options.go` (HTTP/AWS hooks, model filter).

## Configuration

```yaml
---
default_provider: OPENAI

providers:
  # The first provider is the default when default_provider is not set.
  - name: OPENAI
    token: env://OPENAI_API_KEY
    default_model: gpt-5
    available_models:
      - gpt-5.1
      - gpt-5.1-mini
    open_ai:
      api_type: OPENAI

  - name: ANTHROPIC
    token: env://ANTHROPIC_API_KEY
    default_model: claude-sonnet-4-8
    available_models:
      - claude-opus-4-8
      - claude-sonnet-4-8
    open_ai:
      api_type: ANTHROPIC

  - name: AZURE
    token: env://AZURE_OPENAI_API_KEY
    default_model: gpt-5.1
    available_models:
      - gpt-5.1
    open_ai:
      api_type: AZURE
      base_url: env://AZURE_OPENAI_URL
      api_version: "2025-03-01-preview"

  - name: BEDROCK
    default_model: us.anthropic.claude-opus-4-20250514-v1:0
    available_models:
      - us.anthropic.claude-opus-4-20250514-v1:0
    open_ai:
      api_type: BEDROCK

# Which models each assistant prefers, in order.
assistant_models:
  default:
    - OPENAI/gpt-5.1
  orchestrator:
    - claude-opus-4-8
    - claude-sonnet-4-8

# Per-organization overrides, merged over assistant_models.
orgs_override:
  "1000":
    assistant_models:
      default:
        - OPENAI/gpt-5.1-mini

# Optional Agent Skills configuration (see skills.md).
skills:
  enable_default_skills: false
  enable_standard_paths: false
  paths:
    - ./skills
```

Load it:

```go
fac, err := llmfactory.Load("llm.yaml")   // LoadConfig + New
if err != nil {
    return err
}
```

or split the steps to inject options:

```go
cfg, err := llmfactory.LoadConfig("llm.yaml")
if err != nil {
    return err
}
fac := llmfactory.New(cfg,
    llmfactory.WithHTTPClient(myInstrumentedClient),
    llmfactory.WithAWSConfigFactory(loadAWSConfig),
)
```

`LoadConfig("")` returns an empty config rather than an error, which is
convenient for tests. Values are expanded by
`configloader.UnmarshalAndExpand`, so `env://VAR` and `file://path` work for
any string field — keep tokens out of the YAML.

### `api_type` values

`open_ai.api_type` selects the provider family. It is the single most important
field, because it decides both the client implementation and the
[capability set](llms.md#capabilities).

| `api_type` | Implementation | Notes |
|------------|----------------|-------|
| `OPENAI` (or `OPEN_AI`) | `pkg/llms/openai` | Set `open_ai.organization` / `open_ai.project` if needed |
| `AZURE`, `AZURE_AD` | `pkg/llms/openai` | Requires `base_url` and `api_version` |
| `OPENROUTER` | `pkg/llms/openai` | Defaults `base_url` to `https://openrouter.ai/api/v1`; `headers` are forwarded |
| `PERPLEXITY` | `pkg/llms/openai` | Set `base_url: https://api.perplexity.ai` |
| `OPENAI_BEDROCK` | `pkg/llms/openai` | OpenAI-compatible models on Bedrock; uses `WithAWSConfigFactory` |
| `ANTHROPIC` | `pkg/llms/anthropic` | |
| `ANTHROPIC_BEDROCK` | `pkg/llms/anthropic` (Bedrock mode) | Uses `WithAWSConfigFactory` |
| `BEDROCK` | `pkg/llms/bedrock` | Native Bedrock runtime API |
| `GOOGLEAI` | `pkg/llms/googleai` | |
| `CLOUDFLARE` | `pkg/llms/cloudflare` | `base_url` maps to the server URL |

An unknown `api_type` fails at model construction with
`unsupported provider type: X`.

### `available_models` vs `default_model`

- `available_models` is an allow-list. A requested model that is not listed for
  a provider is skipped, even if the provider would accept it.
- `default_model` is the fallback used when nothing preferred matches.
- `ProviderConfig.FindModel(models...)` implements this: first match in
  `available_models` wins, else `default_model`, else an error.

## Model resolution

```go
model, err := fac.GetModel(ctx, llmfactory.ModelOptions{
    OrgID:                "1000",
    AssistantName:        "orchestrator",
    PreferredModels:      []string{"claude-opus-4-8"},
    RequiredCapabilities: llms.CapabilityJSONSchema | llms.CapabilityFunctionCalling,
})
```

Resolution order:

1. **`ProviderType` set** → pick the provider with that `api_type`, ignore
   everything else. Returns an error if it does not support
   `RequiredCapabilities`. This is the "give me Anthropic, whatever the config
   prefers" escape hatch, mostly used in tests.
2. Otherwise, build the preferred list: `PreferredModels` first, then the
   `assistant_models[AssistantName]` entries (falling back to
   `assistant_models["default"]`), using the org's overridden map when `OrgID`
   matches an `orgs_override` entry.
3. Try each preferred entry in order. An entry is skipped when the model filter
   rejects it, when its provider does not support all `RequiredCapabilities`,
   or when the model is not in that provider's `available_models`.
4. If nothing matched, fall back to the default provider's `default_model` —
   still subject to the capability check and the model filter.

Model references may be provider-qualified: `PROVIDER/model`. The prefix is
treated as a provider name **only** when it exactly matches a configured
provider, so slash-bearing model IDs like
`us.anthropic.claude-opus-4-20250514-v1:0` or `openai/gpt-oss-120b` survive
unqualified.

Resolved models are cached on the factory by name and by type, so repeated
`GetModel` calls reuse one client.

### How assistants use it

`Assistant.Run` calls `GetModel` with the assistant's own name and the org from
the `ChatContext`:

```go
fac.GetModel(ctx, llmfactory.ModelOptions{OrgID: orgID, AssistantName: a.name})
```

So `Assistant.WithName("orchestrator")` is what binds it to the
`assistant_models.orchestrator` entry. Two consequences worth remembering:

- Renaming an assistant silently changes which model it gets (it falls back to
  `default`).
- `assistants.WithModel(model)` bypasses the factory entirely — useful for
  tests and for pinning one assistant to one model.

## Capabilities

`RequiredCapabilities` is an **AND** mask: the provider type must support every
requested capability. This is stricter than `llms.ProviderType.Supports`, which
returns true if *any* bit matches — see [LLM Providers](llms.md#capabilities).

```go
// Only providers that can do strict JSON Schema *and* parallel tool calls.
model, err := fac.GetModel(ctx, llmfactory.ModelOptions{
    AssistantName: "extractor",
    RequiredCapabilities: llms.CapabilityJSONSchemaStrict |
        llms.CapabilityMultiToolCalling,
})
```

Use this when your assistant genuinely cannot work otherwise; the alternative
is a runtime failure deep in the loop.

## Restricting models per organization

Install a `ModelFilterFunc` to enforce quota, entitlements, or cost policy. It
is consulted for every candidate, including the default-model fallback.

```go
// ModelFilterFunc reports whether the model may be used.
// orgID, assistantName and modelName may be empty / provider-qualified.
filter := func(ctx context.Context, orgID, assistantName, modelName string) bool {
    if orgID == "free-tier" && strings.Contains(modelName, "opus") {
        return false
    }
    return quota.Allow(ctx, orgID, modelName)
}

fac := llmfactory.New(cfg, llmfactory.WithModelFilter(filter))
```

`Factory.WithModelFilter` returns a *new* factory with the filter applied,
sharing the already-built client caches:

```go
tenantFactory := baseFactory.WithModelFilter(tenantFilter)
```

Note that `assistantName` is normalized to `"default"` when empty, and
`modelName` may arrive either bare or as `PROVIDER/model`; a filter that
matches on substrings handles both.

## Skills

When `skills` is present in the config, `New` builds a `skills.Loader` and
exposes it:

```go
list := fac.Skills("orchestrator", "security")  // agent name + required tags
agent = agent.WithSkills(list)
```

A skills-loading failure is logged, not fatal — `Skills` then returns `nil` and
assistants run without skills. See [Skills](skills.md).

## Testing

`llmfactory.NewLLM` is a package-level variable pointing at `CreateLLM`.
Override it to return a mock without touching the config plumbing:

```go
orig := llmfactory.NewLLM
llmfactory.NewLLM = func(cfg *llmfactory.ProviderConfig, preferred []string, opts *llmfactory.Options) (llms.Model, error) {
    return mockModel, nil
}
t.Cleanup(func() { llmfactory.NewLLM = orig })
```

For assistant-level tests, prefer `mocks/mockllmfactory` (generated by
`make generate`) or `assistants.WithModel(mockModel)`.

## Exact router resolution

`ResolveExact(ctx, factory, providerName, modelName, orgID)` requires the optional
`ExactResolver` interface. It returns `ErrModelNotFound` for absent/denied targets,
checks org filters before cached results, and never falls back. Duplicate provider
names are errors. The existing `GetModel` preference/default behavior is unchanged.
Constructed models are cached per provider and model; the org filter runs on every
call, so two organizations allowed to use the same deployment share one instance.

Router-related provider options under `open_ai`:

| Key | Provider type | Effect |
|-----|---------------|--------|
| `converse: true` | `BEDROCK` | Enables the portable `Infer` method through Bedrock Converse; legacy `GenerateContent` still uses InvokeModel. |
| `inference_api: responses\|chat` | `OPENAI` and compatible types | Selects the upstream API used by the portable `Infer` method. Empty means Responses for `OPENAI` and Chat Completions for other types. |

Adapt the resolved model with `router.Connector(model)`; see [router.md](router.md).

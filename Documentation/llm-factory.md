# LLM Factory

`pkg/llmfactory` turns YAML configuration into `llms.Model` instances. It is
the only place in the repo that knows about provider construction, so
application code and assistants stay provider-agnostic.

Source: [`pkg/llmfactory/`](../pkg/llmfactory) —
`config.go` (types, loading, validation), `factory.go` (provider
construction), `resolve.go` (resolution), `modelref.go` (model references and
patterns), `policy.go` (per-provider allow/deny/effort policy), `rank.go`
(ordering of matches), `catalog.go` (model lists read from providers),
`lister.go` (the client that reads them), `resolution.go` (`Catalog`,
`Resolution`), `options.go` (HTTP/AWS hooks, model filter).

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
    # An effort after a comma is the default for that model.
    default_model: claude-sonnet-4-8,medium
    available_models:
      - claude-opus-4-8,high
      - claude-sonnet-4-8
      # A pattern admits every model it matches.
      - claude-haiku-*,low
    # The deny-list wins over available_models and default_model.
    disabled_models:
      - "*-preview"
    # No call to this provider goes above this effort.
    max_effort: xhigh
    # Read the model list from the provider (see Model discovery).
    discovery:
      enabled: true
      refresh: 6h
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
    - ANTHROPIC/claude-sonnet-*,high

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

### Model references

A *model reference* is the string used wherever a model is named:

```text
[PROVIDER/]MODEL[,EFFORT]
```

| Part | Meaning |
|------|---------|
| `PROVIDER/` | Optional. A configured provider `name`, matched exactly. Otherwise an **upper-case** provider type or alias accepted by `llms.ParseProviderType` (`ANTHROPIC`, `CLAUDE`, `GEMINI`, …), which addresses every configured provider of that type, in config order. Any other prefix is part of the model ID. |
| `MODEL` | A model ID, or a pattern: `*` matches any run of characters and `?` exactly one. Every other character is literal, including `.` and `/`. |
| `,EFFORT` | Optional default [reasoning effort](#reasoning-effort): `none`, `low`, `medium`, `high`, `xhigh` or `max`. Case-insensitive, surrounding spaces ignored. Any other text after the last comma is an error. |

| Reference | Provider | Model | Default effort |
|-----------|----------|-------|----------------|
| `gpt-5.1` | any that offers it | `gpt-5.1` | — |
| `AZURE/gpt-5.1,medium` | the provider named `AZURE`, else providers of type `AZURE` | `gpt-5.1` | `medium` |
| `ANTHROPIC/claude-opus-*,high` | the provider named `ANTHROPIC`, else providers of type `ANTHROPIC` | best match | `high` |
| `GEMINI/gemini-*-flash` | providers of type `GOOGLEAI` | best match | — |
| `openrouter/anthropic/claude-sonnet-4` | the provider named `openrouter` | `anthropic/claude-sonnet-4` | — |
| `openai/gpt-oss-120b` | any; a lower-case prefix is never a type | `openai/gpt-oss-120b` | — |

Where each part is accepted:

| Place | Provider prefix | Pattern | Effort suffix |
|-------|-----------------|---------|---------------|
| `assistant_models` (global and per org), `ModelOptions.PreferredModels` | yes | yes | Default for that assistant or call |
| `available_models` | no | yes | Default for matching models when the selecting reference has none |
| `default_model` | no | yes | Default when the default model is used |
| `disabled_models` | no | yes | not allowed |

Two YAML details matter:

- In a flow sequence the comma separates items, so `[OPENAI/gpt-5.1,medium]` is
  two entries. Use block style, or quote: `["OPENAI/gpt-5.1,medium"]`. Loading
  rejects an entry that is only an effort level and says why.
- A pattern that starts with `*` must be quoted, because YAML reads a leading
  `*` as an alias: `"*-preview"`.

### Model policy of a provider

| Key | Meaning |
|-----|---------|
| `available_models` | Allow-list of IDs and patterns. A model can be selected by name only when an entry matches it. It is the single place that grants access. |
| `disabled_models` | Deny-list of IDs and patterns. Wins over `available_models` and `default_model`. |
| `default_model` | The fallback used when nothing preferred matches. An exact ID does not need to be listed in `available_models`. A pattern selects the best known model it matches. |
| `max_effort` | Highest reasoning effort any call to this provider may use. Empty means no ceiling. |
| `discovery` | Read the model list from the provider. Off unless present and enabled; see [Model discovery](#model-discovery). |

Without `discovery` the factory does not ask a provider which models exist.
The models it *knows* are the exact IDs listed in `available_models`, minus
the denied ones. That decides what a reference selects:

| Reference | Selects |
|-----------|---------|
| Exact ID | That ID, when an `available_models` entry matches it (an exact entry or a pattern) and no `disabled_models` entry does. It is not checked against the provider. |
| Pattern | The known models the pattern matches, best first. A pattern never selects an ID that is only admitted by another pattern. |

"Best first" orders matches by: generally available before `preview`, `beta`
and `exp`; higher version; an undated ID before a dated snapshot, and a newer
snapshot before an older one; fewer name parts; then alphabetically. A pattern
that spans model families, such as `gpt-5*`, is therefore ordered by version
alone — name the family you mean, as in `gpt-*-mini`.

`ProviderConfig.FindModel(models...)` applies the same policy to concrete
names: the first allowed name wins, else the default model, else an error.

### Validation

`LoadConfig` calls `Config.Validate` and fails, naming every problem, on:

- an effort that is not a level, in any reference or in `max_effort`;
- an entry that is only an effort level (the YAML flow-sequence mistake);
- an effort on a `disabled_models` entry;
- a `default_model` that is also disabled, or a `default_model` pattern that
  matches no exact `available_models` entry (with `discovery` the pattern is
  accepted: its models come from the provider);
- `discovery` enabled for a provider type that has no model list, or a
  `discovery.refresh` that is not a duration of at least one minute.

`New` cannot return an error. Given a `Config` built in code, it logs the
entries that do not parse and leaves them out; call `Validate` yourself to
fail fast.

## Model resolution

```go
model, err := fac.GetModel(ctx, llmfactory.ModelOptions{
    OrgID:                "1000",
    AssistantName:        "orchestrator",
    PreferredModels:      []string{"claude-opus-4-8,high"},
    RequiredCapabilities: llms.CapabilityJSONSchema | llms.CapabilityFunctionCalling,
})
```

Resolution order:

1. **`ProviderType` set** → pick the provider with that `api_type` and use its
   default model, ignoring everything else, the model filter included. Returns
   an error if it does not support `RequiredCapabilities`. This is the "give me
   Anthropic, whatever the config prefers" escape hatch, mostly used in tests.
   With [discovery](#model-discovery), it is an error when the provider no
   longer serves the default model and no allowed model of its family can
   stand in.
2. Otherwise, build the preferred list: `PreferredModels` first, then the
   `assistant_models[AssistantName]` entries (falling back to
   `assistant_models["default"]`), using the org's overridden map when `OrgID`
   matches an `orgs_override` entry.
3. Try each reference in order, on each provider it can address, in config
   order. A provider is skipped when it does not support all
   `RequiredCapabilities`. The first model the reference
   [selects](#model-policy-of-a-provider) and the model filter accepts wins.
   A reference that does not parse is logged and skipped. With
   [discovery](#model-discovery), a model that stands in for a named one is
   tried only after every provider was asked for the named model.
4. If nothing matched, fall back to the default provider's default model —
   still subject to the capability check and the model filter. When
   `default_model` is a pattern, the best match the filter accepts is used.

Resolved models are cached on the factory per provider, model and effort
settings, so two assistants that resolve to the same model at the same effort
share one client, however they named it.

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
  tests and for pinning one assistant to one model. The configured effort and
  ceiling do not apply to such a model.

## Model discovery

Providers retire models and ship new ones. Without discovery the factory knows
only the IDs written in `available_models`: a retired model fails at call time
until someone edits the file, and a pattern cannot select a model nobody
listed. With discovery the factory reads the model list from the provider and
resolves against it.

```yaml
providers:
  - name: ANTHROPIC
    token: env://ANTHROPIC_API_KEY
    default_model: claude-sonnet-*,medium
    available_models:
      - claude-fable-*,high
      - claude-opus-*
      - claude-sonnet-*
    disabled_models:
      - "*-preview"
    discovery:
      enabled: true
      refresh: 6h
    open_ai:
      api_type: ANTHROPIC

assistant_models:
  orchestrator:
    - ANTHROPIC/claude-fable-5-1,high
```

| Key | Meaning |
|-----|---------|
| `discovery.enabled` | Read the model list from the provider. Accepted for the `api_type` values that advertise `llms.CapabilityModels`: `OPENAI`, `OPENROUTER`, `ANTHROPIC` and `GOOGLEAI`. On any other type it is a load error. |
| `discovery.refresh` | How long a fetched list is trusted. A duration string; default `1h`, minimum `1m`. |

A file without the section resolves exactly as before.

### What a list changes

With a list, the models the factory *knows* are the ones the provider serves,
not the IDs written in `available_models`. A model is *usable* when the
provider serves it, an `available_models` entry matches it, no
`disabled_models` entry does, and it is not retired. `available_models` stays
the only grant of access: discovery never widens it.

| Reference | Selects |
|-----------|---------|
| Exact ID | That ID, when it is usable. Otherwise the usable models of its family, best first. |
| Pattern | The usable models the pattern matches, best first. |
| Exact `default_model` | That ID, when the provider serves it and it is not denied; it still needs no `available_models` entry. Otherwise the usable models of its family, best first. |

"Best first" is the order described under
[Model policy](#model-policy-of-a-provider), with a model in normal service
ahead of a deprecated one.

**Served.** The provider serves a model when its list holds the ID and does
not mark it retired. A model that is not in the list is gone: providers drop
models from their lists rather than flag them (Anthropic returns no retired
model, even when asked for them). One rule keeps working IDs working: a
floating ID is served when the list holds a dated snapshot of it. Anthropic
accepts `claude-haiku-4-5` and lists only `claude-haiku-4-5-20251001`; the
reference is used as written.

**Family.** The family of a model is its ID without version and snapshot, as
`llms.ParseModelName` computes it. Two models are of one family when
`ModelName.Key()` is the same for both, which also holds the route and the
variant:

| ID | Family | Version |
|----|--------|---------|
| `claude-fable-5-1` | `claude-fable` | 5.1 |
| `claude-3-5-sonnet-20241022` | `claude-sonnet` | 3.5 |
| `gpt-5.1-mini` | `gpt-mini` | 5.1 |
| `gpt-5.6-luna` | `gpt-luna` | 5.6 |
| `gemini-3-pro-preview` | `gemini-pro` | 3 (preview) |
| `global.anthropic.claude-sonnet-4-6` | `claude-sonnet`, prefix `global.anthropic.` | 4.6 |
| `openai/gpt-5.5:batch` | `gpt`, prefix `openai/`, variant `batch` | 5.5 |
| `gpt-4o-mini` | `gpt-4o-mini` | none |

An ID the rule cannot split keeps its whole name as the family, so it matches
nothing else. The family comes from the ID alone, never from what the provider
says about the model.

**Family fallback** is how a retired model keeps working: an exact ID that
cannot be used gives way to the best usable model of its family.
`claude-fable-5-1` gives way to the newest `claude-fable` model, and
`gemini-2.5-pro` to the newest `gemini-pro` model, but never to a Flash model.
It only picks a usable model, so `available_models` decides how far it may go:
`claude-fable-*` allows any Fable, and a list of exact IDs allows only those.
The effort written on the reference still applies; without one, the effort of
the `available_models` entry that allows the substitute does.

The fallback also applies to an exact ID that the provider serves but the
configuration does not allow: `claude-opus-4-6` resolves to `claude-opus-4-8`
when only the latter is in `available_models`.

A named model wins over a substitute. Every provider a reference can address
is asked for the named model first, in config order; only when none has it are
the substitutes tried.

### Reading the list

- **First use.** The list is fetched on the first resolution that reaches the
  provider, outside the factory lock. That resolution waits for it, for at most
  10 seconds or until its context ends. `New` still does no I/O and cannot
  fail.
- **Refresh.** Once a list is older than `refresh`, the next resolution starts
  a fetch in the background and keeps using the list it has until the new one
  arrives.
- **Failure.** The last good list stays in use. With no list at all, the
  provider resolves as it does without discovery until a fetch succeeds. A
  failed fetch is retried at most once a minute. An empty list counts as a
  failure, so it cannot make every model look retired. A discovery outage
  never fails `GetModel`.
- **Sharing.** Factories derived with `WithModelFilter` share one list per
  provider. One fetch runs at a time per provider.

What each provider reports (see
[Providers → Listing models](llms.md#listing-models)):

| `api_type` | Lifecycle | Effort levels |
|------------|-----------|---------------|
| `ANTHROPIC` | `active` and `deprecated`, with the retirement date | Per model |
| `OPENAI` | `deprecated` with the shutdown date, once announced | — |
| `OPENROUTER` | `deprecated` with the expiration date, once set | Per model where reported, and the level it applies by default |
| `GOOGLEAI` | — | — |

Where the provider reports the effort levels of a model, they replace the
provider's built-in table for the client of that model; see
[Reasoning effort](#reasoning-effort).

### Seeing what a reference resolves to

`Resolve` reports what `GetModel` selects, without building a client:

```go
res, err := llmfactory.Resolve(ctx, fac, llmfactory.ModelOptions{
    AssistantName: "orchestrator",
})
if err != nil {
    return err
}
fmt.Printf("%s -> %s/%s (%s)\n", res.Reference, res.Provider, res.Model, res.Reason)
// ANTHROPIC/claude-fable-5-1,high -> ANTHROPIC/claude-fable-5-2 (family)
```

| `Resolution` field | Meaning |
|--------------------|---------|
| `Provider`, `Model` | The provider name and the model ID selected |
| `Reference` | The configuration entry that selected it, as written |
| `Reason` | `ReasonExact`, `ReasonPattern`, or `ReasonFamily` for a substitute |
| `Default` | Set when no preferred reference selected a model and `default_model` was used |
| `Effort` | The effort settings the client gets |
| `Verified` | Set when the provider's list confirmed the model. False without discovery and until a list is read. |
| `Lifecycle`, `RetiresAt` | What the provider reports for the model |

`RefreshModels(ctx, fac)` reads every list now and waits for them: call it at
start-up to pay for the first fetch before traffic arrives, or to pick up a
provider change at once. Lists that cannot be read stay as they were, and the
errors are returned.

Both functions need a factory that implements the optional `Catalog`
interface, as the one returned by `New` does.

### Logs and metrics

Each event is logged once per fetched list, not once per call:

| Level | Event | When |
|-------|-------|------|
| `INFO` | `model_list_loaded` | A list was read |
| `ERROR` | `model_list_failed` | A list could not be read; `stale` says whether an older one is in use |
| `INFO` | `model_substituted` | A reference resolved to another model of the family |
| `INFO` | `model_selected` | A pattern resolved to a model of the provider's list |
| `WARN` | `model_not_listed` | A model the configuration names and allows is not served by the provider |
| `WARN` | `model_deprecated` | A resolution used a deprecated model; carries the retirement date |
| `WARN` | `pattern_spans_families` | A pattern matches models of several families, between which the order is by version alone |

Two counters are in `pkg/metricskey`: `stats_llm_model_substituted` (tags
`provider`, `requested`, `model`), incremented on every resolution that uses a
substitute, and `stats_llm_model_list_failed` (tag `provider`). See
[Observability](observability.md#metrics).

## Reasoning effort

Levels are ordered `none < low < medium < high < xhigh < max`. `none` means the
least reasoning the model allows and `max` the most.

The requested level is the first of these that is set:

1. The call option: `assistants.WithReasoningEffort` or
   `llms.WithReasoningEffort`. `ReasoningEffortDefault` counts as not set; pass
   `ReasoningEffortNone` to ask for the least reasoning explicitly.
2. The effort suffix on the reference that selected the model.
3. The effort suffix on the first `available_models` entry that matches the
   model.

If none is set, the provider sends what it sent before effort could be
configured (see the table below). Otherwise the level is lowered to
`max_effort` if it exceeds it, and then snapped to the model: the level itself
when the model supports it, else the nearest supported level below, else the
nearest above.

| Requested | `max_effort` | Model supports | Sent |
|-----------|--------------|----------------|------|
| `xhigh` | — | `low`, `medium`, `high` | `high` |
| `xhigh` | `high` | `low` … `max` | `high` |
| `none` | — | `low`, `medium`, `high` | `low` |
| `medium` | — | `high` only | `high` |
| not set, config says `,medium` | — | `low` … `high` | `medium` |
| not set, no config | — | anything | The provider's own default |

`max_effort` also caps a model's own default: where sending nothing would let
the model reason above the ceiling, the ceiling is sent instead. This holds
wherever the client sends nothing on its own, including Chat Completions and
model IDs with a vendor prefix; the provider tables record the level each
model applies by default. A ceiling never raises the level: when the model's
lowest level is not below its own default, nothing is sent.

Which levels a model supports comes from a table in each provider package,
keyed by model family and version (`llms.ParseModelName`), not by name prefix.
A model newer than the table is treated like the newest generation the table
knows, or as supporting `low`, `medium` and `high`.

With [discovery](#model-discovery), what the provider reports for a model
replaces the table for the client of that model: the levels Anthropic and
OpenRouter list, and the default level OpenRouter lists, which is what
`max_effort` is checked against when a call requests none. A model new to the
table then gets its real levels: `openai/gpt-6-sol` on OpenRouter takes `max`
with discovery and is cut to `high` without.

| Providers | Requested level is sent as | With no level requested |
|-----------|----------------------------|-------------------------|
| `OPENAI`, `AZURE`, `AZURE_AD`, `OPENROUTER`, `OPENAI_BEDROCK` | `reasoning.effort` on the Responses API, `reasoning_effort` on Chat Completions. `none` is sent as `none` or `minimal`, whichever the model has, else as its lowest level. | Responses API: `low`; `none` on `gpt-5.1`; `medium` on the pro tier from 5.2; `high` on `gpt-5-pro` and `gpt-5.6-sol`; nothing for IDs with a vendor prefix and for generations after 5.6. Chat Completions: nothing. |
| `ANTHROPIC`, `ANTHROPIC_BEDROCK` | From 4.6: adaptive thinking plus `output_config.effort`. Before 4.6: a thinking token budget, with `max_tokens` raised above it when needed; Opus 4.5 also gets the effort. `none` sends the lowest effort and turns thinking off in the form the model accepts: no thinking parameter before 5.0, `disabled` on Opus 5, Sonnet 5 and Haiku 5.5, `between_tools` on Sonnet 5.5. Opus 5.5, Fable and Mythos always think, so `none` only lowers their effort. | `output_config.effort: low` on 4.6 to 4.9; nothing on older and on 5.x models. |
| `GOOGLEAI` | From Gemini 3: `thinkingLevel`. Gemini 2.5, which rejects a level: a `thinkingBudget` of 1024, 8192 or 24576 tokens for `low`, `medium` and `high`. `none` is sent as `MINIMAL` or a zero budget where the model has it, else as its lowest level. Models before 2.5 get nothing. The model is the one the client was built with; a model named by `llms.WithModel` is not sent to the API. | Nothing. |
| `BEDROCK`, `CLOUDFLARE`, and models without a reasoning control | Nothing; effort is ignored. | Nothing. |

The settings are applied inside the provider client
(`openai.WithEffortPolicy`, `anthropic.WithEffortPolicy`,
`googleai.WithEffortPolicy`), so they cover every `GenerateContent` call on a
model the factory returned, with or without an assistant.

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

Note that `assistantName` is normalized to `"default"` when empty. The filter
always sees concrete model IDs — never a pattern or an effort suffix — first as
`PROVIDER/model` when the reference named a provider, then as the bare `model`;
a filter that matches on substrings handles both. When it denies one match of a
pattern, the next best match is tried; this includes a pattern `default_model`.
A model that stands in for a retired one is shown to the filter the same way,
after the named model was shown as written; a filter that refuses the named
model stops the substitution.

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

The factory passes one concrete model ID in `preferred` (none for a provider
without a usable default model), and the model's effort settings in
`opts.Effort`. `cfg.FindModel(preferred...)` returns that ID.

`llmfactory.NewModelLister` is the same hook for discovery: it points at
`CreateModelLister` and is called once per provider, on the first fetch.
Return a fake `llms.ModelLister` (or `mockllms.MockModelLister`) to resolve
against a list of your own:

```go
orig := llmfactory.NewModelLister
llmfactory.NewModelLister = func(cfg *llmfactory.ProviderConfig, opts *llmfactory.Options) (llms.ModelLister, error) {
    return fakeLister, nil
}
t.Cleanup(func() { llmfactory.NewModelLister = orig })
```

For assistant-level tests, prefer `mocks/mockllmfactory` (generated by
`make generate`) or `assistants.WithModel(mockModel)`.

## Exact router resolution

`ResolveExact(ctx, factory, providerName, modelName, orgID)` requires the optional
`ExactResolver` interface. It returns `ErrModelNotFound` for absent/denied targets,
checks org filters before cached results, and never falls back. Duplicate provider
names are errors. The existing `GetModel` preference/default behavior is unchanged.
Constructed models are cached per provider, model and effort settings; the org
filter runs on every call, so two organizations allowed to use the same deployment
share one instance.

A model is admitted when an `available_models` entry (an ID or a pattern) or the
provider's default model matches it and no `disabled_models` entry does. The name
is taken literally: a pattern is never expanded and no other model is substituted.
With [discovery](#model-discovery), a model the provider no longer serves is
`ErrModelNotFound`. The model carries the effort of its `available_models` entry
and the provider's `max_effort`.

Router-related provider options under `open_ai`:

| Key | Provider type | Effect |
|-----|---------------|--------|
| `converse: true` | `BEDROCK` | Enables the portable `Infer` method through Bedrock Converse; legacy `GenerateContent` still uses InvokeModel. |
| `inference_api: responses\|chat` | `OPENAI` and compatible types | Selects the upstream API used by the portable `Infer` method. Empty means Responses for `OPENAI` and Chat Completions for other types. |

Adapt the resolved model with `router.Connector(model)`; see [router.md](router.md).

## Known limits

- `AZURE`, `AZURE_AD`, `BEDROCK`, `ANTHROPIC_BEDROCK`, `OPENAI_BEDROCK`,
  `PERPLEXITY` and `CLOUDFLARE` have no discovery, and so no family fallback:
  there a pattern selects among the exact IDs listed in `available_models`.
- A model retired between two refreshes fails until the next refresh. `refresh`
  bounds that window; `RefreshModels` closes it on demand.
- With discovery, an ID the provider accepts but does not list is treated as
  gone, apart from a floating ID with a listed snapshot. `Resolve` shows what
  such a reference selects, and `model_not_listed` is logged for it.
- The factory does not know prices. "Cheaper" is expressed by which family a
  pattern names, not chosen automatically.
- On Azure the effort table is keyed by deployment name, so it only works when
  deployments are named after their model.
- `max_effort` and the configured default apply to `GenerateContent`. The
  router's portable `Infer` path has its own reasoning controls
  (`llms.Effort`) and is not affected by them.
- The Anthropic client does not replay thinking blocks, so inside a tool loop a
  model reasons without its earlier thinking.

## Decisions

| Decision | Why | Rejected |
|----------|-----|----------|
| Extend the existing keys; detect features by presence | Every existing file keeps working and features are adopted one line at a time | A `version` field, or a parallel `models:` section that duplicates `available_models` |
| `available_models` stays the only grant of access; substitution picks among allowed models | Keeps the original intent of an explicit allow-list. Writing a pattern is the operator's consent to newer models. | Substituting any same-family model the provider lists |
| Effort is enforced inside the provider client | The ceiling cannot be bypassed, direct `GenerateContent` callers are covered, and optional interfaces such as `llms.InferenceModel` and `llms.Batcher` stay visible | A wrapping `llms.Model`, which breaks type assertions such as the one in `router.Connector`; or returning the effort for assistants to apply, which misses direct callers |
| Supported levels come from a family-and-version table, and later from provider data | Name checks break on every new model: a substring check for `-4-6` to `-4-9` treated every Claude 5.x model as older than 4.6 | Prefix and substring lists |
| With no level requested, each provider sends what it always sent | An existing deployment must not change cost or quality by upgrading the library | Applying one default level to every model |
| Patterns are globs | Model IDs are full of dots, which an unescaped regex over-matches, and RE2 has no negative lookahead, so a deny-list is needed either way. The `re:` prefix stays reserved. | Regular expressions |
| Effort is a suffix on the string | Org overrides merge as `map[string][]string`, and adding an effort is a one-line edit | Object entries with `model:` and `effort:` keys |
| A provider-type prefix must be upper-case | `llms.ParseProviderType` ignores case, which would turn `openai/gpt-oss-120b` into provider `OPENAI` | Case-insensitive type prefixes |
| Fallback picks the newest usable model of the family, in the same order as a pattern match | One ordering to learn, shared by patterns and fallback | Nearest-version successor: a second ordering, and it drifts again at the next retirement |
| Family fallback needs `discovery` | A configured list cannot tell a retired model from a mistyped one, and existing files must not change behaviour | Fallback among configured IDs |
| Lists are fetched lazily and refreshed on read | `New` stays free of I/O and errors, and the factory needs no `Close` | A background ticker |
| A model that is not listed is gone, except a floating ID with a listed snapshot | Providers drop retired models from their lists instead of flagging them. The exception covers the IDs Anthropic accepts but lists only by date, which would otherwise be replaced while they still work. | Waiting for an explicit `retired` flag, which the Anthropic API was seen never to send |
| The named model on any provider wins over a substitute | A reference such as `gpt-5.5` must reach the provider that has it, not an older model on a provider listed earlier | Resolving provider by provider |
| The family comes from the ID, not from provider metadata | One rule for every provider, and never wider than the name: Anthropic's `line` would put every Opus variant in one family | Comparing the family the provider reports |
| An empty list is a failed fetch | No provider serves no model, and resolving against an empty list would make every configured model look retired | Trusting the list |

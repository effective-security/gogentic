# RELEASE NOTES v1.0

Version 1.0 makes the LLM factory cope with two things that used to need a
code change or a config edit on short notice: providers retiring and renaming
models, and reasoning effort that differs from model to model.

An existing `llm.yaml` and existing code keep working. Every new feature is
switched on by adding a key or a suffix to the configuration; a file without
them resolves the same models as 0.21. The few changes you get without asking
are listed under [Changes to check when upgrading](#changes-to-check-when-upgrading).

The router is not part of this release: it has its own reasoning controls and
is not affected by the effort settings described here.

## Summary

| Change | What you get | What to do |
|--------|--------------|------------|
| [Reasoning effort in configuration](#reasoning-effort-in-configuration) | A default effort per model or per assistant, and a ceiling per provider, without code | Add `,EFFORT` to model names and `max_effort` to providers |
| [Effort sent correctly for every model](#effort-is-sent-correctly-for-every-model) | Requests that name a level are no longer rejected or silently downgraded | Nothing |
| [Model patterns and a deny-list](#model-patterns-and-a-deny-list) | `claude-opus-*` instead of a list of versions; `disabled_models` to exclude | Replace enumerated models with patterns where you want that |
| [Model discovery and family fallback](#model-discovery-and-family-fallback) | A retired model is replaced by the newest allowed model of its family; patterns see models nobody listed | Add a `discovery` section to the provider |
| [Seeing what a reference resolves to](#seeing-what-a-reference-resolves-to) | `Resolve` and `RefreshModels` | Optional calls at start-up and in diagnostics |
| [Model lists as an API](#model-lists-as-an-api) | `llms.ModelLister` on OpenAI, OpenRouter, Anthropic and Google AI | Optional |
| [Configuration validated at load](#configuration-is-validated-at-load) | A mistyped entry fails at load instead of being skipped at run time | Call `Config.Validate` for a config built in code |
| [Logs and metrics](#logs-and-metrics) | Substitutions and failed list fetches are visible | Alert on two new counters |

## What changed

### Reasoning effort in configuration

A model name can carry a default reasoning effort after a comma, and a provider
can set a ceiling:

```yaml
providers:
  - name: ANTHROPIC
    token: env://ANTHROPIC_API_KEY
    default_model: claude-sonnet-4-8,medium
    available_models:
      - claude-opus-4-8,high
      - claude-sonnet-4-8
    max_effort: xhigh
    open_ai:
      api_type: ANTHROPIC

assistant_models:
  orchestrator:
    - ANTHROPIC/claude-opus-4-8,max
```

The levels are `none`, `low`, `medium`, `high`, `xhigh` and `max`. The level
used for a call is the first of: the call option
(`assistants.WithReasoningEffort`, `llms.WithReasoningEffort`), the suffix on
the reference that selected the model, the suffix on the `available_models`
entry that matches it. It is then lowered to `max_effort` and sent as the
nearest level the model accepts. In the sample, `orchestrator` runs Opus 4.8 at
`xhigh`: its `max` is above the ceiling.

`max_effort` also caps a model that reasons above the ceiling when nothing is
requested, and it never raises a level.

`llms.ReasoningEffort` gained `ReasoningEffortXHigh` and `ReasoningEffortMax`.
`ReasoningEffortNone` now means "the least reasoning the model allows" on every
provider.

Details: [LLM Factory → Reasoning effort](llm-factory.md#reasoning-effort).

### Effort is sent correctly for every model

Which levels a model accepts now comes from a table keyed by model family and
version in each provider package, in place of name prefixes. That fixes
requests that named a level in 0.21:

| Provider | 0.21 | 1.0 |
|----------|------|-----|
| OpenAI and compatible | `none` was sent as `low` (as `none` on `gpt-5.1` only). A level was dropped for model IDs with a vendor prefix, such as `openai/gpt-5.1`, and never sent on Chat Completions. The level was looked up for the client's model even when the call named another. | Each level is sent as the nearest one the model has: `none` or `minimal` where it exists, `xhigh` and `max` on the models that take them. Sent for prefixed IDs and on Chat Completions too, for the model named in the request. |
| Anthropic | A level became a thinking budget of 1000, 5000 or 10000 tokens. 1000 is below the API minimum, the other two did not fit the default `max_tokens`, and Claude 4.7 and later reject any budget. Claude 5.x was treated as older than 4.6. A response format reset the effort to `low`. | Claude 4.6 and later get adaptive thinking plus `output_config.effort`; older models get a valid budget, with `max_tokens` raised above it. `none` turns thinking off in the form each model accepts. The effort is kept next to a response format. |
| Google AI | `medium` was sent as `LOW`, and `none` sent nothing. Gemini 2.5 was sent a thinking level, which it rejects. | `MEDIUM` where the model has it; `none` is the model's lowest setting; Gemini 2.5 gets a thinking budget. |

A request that names no level, with none configured, sends what it sent in
0.21, with the one exception listed under
[Changes to check when upgrading](#changes-to-check-when-upgrading).

### Model patterns and a deny-list

`*` and `?` turn a model name into a pattern, in `available_models`,
`default_model`, `assistant_models` and `ModelOptions.PreferredModels`.
`disabled_models` is a deny-list that wins over everything else.

```yaml
providers:
  - name: ANTHROPIC
    token: env://ANTHROPIC_API_KEY
    default_model: claude-sonnet-4-8
    available_models:
      - claude-opus-4-8
      - claude-sonnet-4-8
      - claude-haiku-*,low
    disabled_models:
      - "*-preview"
    open_ai:
      api_type: ANTHROPIC

assistant_models:
  summarizer:
    - ANTHROPIC/claude-sonnet-*
```

A pattern in `assistant_models` selects the best match: generally available
before preview, then the highest version. Without discovery it chooses among
the models listed by name in `available_models`; with discovery, among the
models the provider serves.

A reference can also name a provider by type in upper case, so
`ANTHROPIC/claude-opus-*` or `GEMINI/gemini-*-flash` reaches every configured
provider of that type.

Details: [LLM Factory → Model references](llm-factory.md#model-references).

### Model discovery and family fallback

With a `discovery` section, the factory reads the model list from the provider
and resolves against it:

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

- **A retired model keeps working.** When the provider stops serving
  `claude-fable-5-1`, `orchestrator` resolves to the newest model that
  `claude-fable-*` allows, and keeps its `high` effort. Before, every call
  failed until someone edited the file.
- **A pattern follows the provider.** `claude-opus-*` selects the newest Opus
  the provider serves, including one released after the file was written.
- **`available_models` stays the only grant of access.** A substitute is always
  a model the configuration allows, of the same family: a Flash model never
  stands in for a Pro model. A list of exact IDs allows only those IDs.
- **It cannot take the factory down.** The list is fetched on first use and
  refreshed in the background. If a fetch fails, the last list stays in use;
  with no list at all the provider resolves as it does without discovery.
- **The provider's own effort levels are used** where it reports them
  (Anthropic and OpenRouter), so a model newer than the built-in table gets its
  real levels.

Discovery is available for `api_type` `OPENAI`, `OPENROUTER`, `ANTHROPIC` and
`GOOGLEAI`. It is a load error on the other types.

Details: [LLM Factory → Model discovery](llm-factory.md#model-discovery).

### Seeing what a reference resolves to

```go
res, err := llmfactory.Resolve(ctx, fac, llmfactory.ModelOptions{
    AssistantName: "orchestrator",
})
if err != nil {
    return err
}
// res.Model     "claude-fable-5-2"
// res.Reason    llmfactory.ReasonFamily: it stands in for claude-fable-5-1
// res.Verified  true: the provider's list confirmed the model
```

`Resolve` reports the model `GetModel` selects, without building a client.
`RefreshModels` reads every list now and waits for them. Both work on the
factory returned by `llmfactory.New`, through the new optional `Catalog`
interface.

### Model lists as an API

Clients of providers that can list their models implement `llms.ModelLister`.
Each provider package has a `NewModelLister` that needs no model name:

```go
lister, err := anthropic.NewModelLister(anthropic.WithToken(token))
if err != nil {
    return err
}
models, err := lister.ListModels(ctx)
```

`llms.ModelInfo` carries the ID, display name, lifecycle (`active`,
`deprecated`, `retired`), retirement date and reasoning levels, as far as the
provider reports them. `llms.ParseModelName` splits a model ID into family,
version, snapshot and variant.

Details: [LLM Providers → Listing models](llms.md#listing-models).

### Configuration is validated at load

`LoadConfig` now fails, naming every problem, on an effort that is not a
level, an entry that is only an effort level, a `default_model` that is also
disabled, and `discovery` on a provider type without a model list. In 0.21 a
reference that did not match was skipped silently at run time.

### Logs and metrics

Two counters were added to `pkg/metricskey` and to `metricskey.Metrics`:

| Metric | Tags | Meaning |
|--------|------|---------|
| `stats_llm_model_substituted` | `provider`, `requested`, `model` | A resolution used another model of the family than the one configured |
| `stats_llm_model_list_failed` | `provider` | A fetch of a provider's model list failed |

The factory logs `model_substituted`, `model_deprecated` (with the retirement
date), `model_not_listed` and `model_list_failed`, each once per fetched list.

Details: [LLM Factory → Logs and metrics](llm-factory.md#logs-and-metrics).

## What to update to benefit

None of this is required. Each step is independent.

### In `llm.yaml`

1. **Move effort out of code.** Where code passes
   `assistants.WithReasoningEffort`, write the level on the model instead:
   `claude-opus-4-8,high` in `available_models`, or
   `ANTHROPIC/claude-opus-4-8,high` in `assistant_models` for one assistant. A
   call option still overrides it.
2. **Set a ceiling.** `max_effort: high` on a provider bounds what any call to
   it can spend on reasoning, whatever code or configuration asks for.
3. **Turn on discovery** for `OPENAI`, `OPENROUTER`, `ANTHROPIC` and `GOOGLEAI`
   providers.
4. **Allow families, not versions.** A retired model can only be replaced by a
   model `available_models` allows. Write `claude-fable-*` where you accept any
   Fable, and keep exact IDs where you do not.
5. **Exclude what you do not want** with `disabled_models`, such as
   `"*-preview"`.

Before and after, for one provider:

```yaml
# 0.21
providers:
  - name: ANTHROPIC
    token: env://ANTHROPIC_API_KEY
    default_model: claude-sonnet-4-8
    available_models:
      - claude-fable-5-1
      - claude-sonnet-4-8
    open_ai:
      api_type: ANTHROPIC
assistant_models:
  orchestrator:
    - ANTHROPIC/claude-fable-5-1
```

```yaml
# 1.0
providers:
  - name: ANTHROPIC
    token: env://ANTHROPIC_API_KEY
    default_model: claude-sonnet-*,medium
    available_models:
      - claude-fable-*,high
      - claude-sonnet-*
    disabled_models:
      - "*-preview"
    max_effort: xhigh
    discovery:
      enabled: true
    open_ai:
      api_type: ANTHROPIC
assistant_models:
  orchestrator:
    - ANTHROPIC/claude-fable-5-1
```

The reference in `assistant_models` did not change. It now means "Fable 5.1
while Anthropic serves it, then the newest Fable".

Two YAML details: in a flow sequence the comma separates items, so write
`["OPENAI/gpt-5.1,medium"]` or use block style; and quote a pattern that starts
with `*`.

### In Go code

1. **Warm the model lists at start-up**, so the first request does not wait for
   them:

   ```go
   if err := llmfactory.RefreshModels(ctx, fac); err != nil {
       log.Printf("model discovery: %v", err) // not fatal: resolution degrades
   }
   ```

2. **Validate a configuration built in code.** `New` cannot fail and drops the
   entries that do not parse; `LoadConfig` validates for you.

   ```go
   if err := cfg.Validate(); err != nil {
       return err
   }
   fac := llmfactory.New(cfg)
   ```

3. **Use the new levels** where you set effort in code:
   `llms.ReasoningEffortXHigh`, `llms.ReasoningEffortMax`, and
   `llms.ReasoningEffortNone` for the least a model allows.
4. **Review your `ModelFilterFunc`.** With patterns and discovery it is called
   with models the reference did not name. It always receives a concrete model
   ID, first as `PROVIDER/model` when the reference named a provider, then
   bare. A filter that refuses the named model also stops its substitute.
5. **Register and alert on the new counters.** If you register
   `metricskey.Metrics`, they are included. `stats_llm_model_substituted` above
   zero means a configured model was retired: update the reference or accept
   the substitute.
6. **If you build provider clients yourself**, pass the same settings with
   `openai.WithEffortPolicy`, `anthropic.WithEffortPolicy` or
   `googleai.WithEffortPolicy`.

### In tests

`llmfactory.NewModelLister` is an overridable variable like `NewLLM`: return a
fake `llms.ModelLister` to resolve against a list of your own. Regenerated
mocks add `mockllms.MockModelLister` and `mockllmfactory.MockCatalog`.

## Changes to check when upgrading

These apply without any change to your configuration.

| Change | Who is affected | What to do |
|--------|-----------------|------------|
| The `ReasoningEffort` constants are typed | Code that assigns one to an `int` without converting. The numeric values did not change. | Use the `llms.ReasoningEffort` type |
| `LoadConfig` validates | A file with a comma in a model reference followed by anything but an effort level now fails to load | Fix the entry the error names |
| `*` and `?` in a model name are wildcards | A model ID that contains one of them literally | None known; no provider uses them |
| A requested effort level is sent differently | Calls that set `WithReasoningEffort`, see [the table above](#effort-is-sent-correctly-for-every-model). Anthropic calls with a level now think, and cost accordingly; in 0.21 Claude 4.7 and later rejected them. | Check cost on Anthropic; set `max_effort` to bound it |
| `gpt-5.2-pro`, `gpt-5.4-pro` and `gpt-5.5-pro` get `medium` when no level is requested | They got `low` in 0.21, which these models do not accept | Nothing |
| Anthropic `max_tokens` defaults to 8192 | Anthropic calls that set no maximum. It was 4096. Claude 3 Haiku and Opus, which are deprecated, stop at 4096 output tokens and reject the new default. | Pass `llms.WithMaxTokens` to keep a lower limit, and always for Claude 3 Haiku and Opus |
| Claude 5.x gets the current web search and web fetch tools | Calls with web tools on Fable, Mythos, Opus 5.x and Sonnet 5.x, which were sent the legacy tool versions | Nothing |
| An upper-case provider type is a provider prefix | A reference such as `OPENAI/gpt-5.1` when no provider is named `OPENAI`: it now reaches the providers of that type. A lower-case prefix such as `openai/` stays part of the model ID. | Nothing |

The `llmfactory.Factory` interface did not change, so your own implementations
and mocks of it still compile.

## Limits

- Discovery is not available for `AZURE`, `AZURE_AD`, `BEDROCK`,
  `ANTHROPIC_BEDROCK`, `OPENAI_BEDROCK`, `PERPLEXITY` and `CLOUDFLARE`.
  Patterns there select among the models listed by name, and a retired model
  is not replaced.
- With discovery, a model the provider accepts but does not list is treated as
  gone. One case is handled: an ID without a date, such as `claude-haiku-4-5`,
  is served when the provider lists a dated snapshot of it. Use `Resolve` to
  check a reference you are unsure of.
- A model retired between two refreshes fails until the next refresh. `refresh`
  bounds that window (default one hour, minimum one minute).
- The built-in effort tables go up to GPT-5.6, Claude 5.5 and Gemini 3.8. A
  newer Claude model is treated like the newest generation the table knows.
  Any other newer model, GPT-6 for example, is taken to accept `low`, `medium`
  and `high`, so `xhigh` and `max` are sent as `high` for it. With discovery,
  Anthropic and OpenRouter report the real levels.
- The effort default and `max_effort` apply to `GenerateContent`, which is what
  assistants use. The router's `Infer` path has its own reasoning controls.

## Upgrade checklist

- [ ] Build. Fix any use of a `ReasoningEffort` constant as an `int`.
- [ ] Load your `llm.yaml` with `LoadConfig` and fix what it reports.
- [ ] Read [Changes to check when upgrading](#changes-to-check-when-upgrading),
      in particular the Anthropic `max_tokens` default and the cost of requested
      effort on Anthropic.
- [ ] Add `,EFFORT` suffixes and `max_effort`.
- [ ] Add `discovery` to the providers that support it, and widen
      `available_models` to the families you accept.
- [ ] Call `RefreshModels` at start-up, and use `Resolve` to confirm what each
      assistant resolves to.
- [ ] Alert on `stats_llm_model_substituted` and `stats_llm_model_list_failed`.

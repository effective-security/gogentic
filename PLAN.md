# PLAN: dynamic models, aliasing and effort in the LLM factory

Reference and design: [Documentation/llm-factory.md](Documentation/llm-factory.md).
This file tracks the implementation order and what is left.

## Status

| Phase | Delivers | Status |
|-------|----------|--------|
| 0 | The `pkg/llms` change that was in flight, finished | done |
| 1 | Effort vocabulary: parse, order, snap, policy | done |
| 2 | Correct effort mapping in OpenAI, Anthropic, Google | done |
| 3 | References, patterns, `disabled_models`, `max_effort`, config effort | done |
| 4 | `ModelLister` on four providers | not started |
| 5 | Catalog, discovery, family fallback, `Catalog` interface | not started |
| 6 | Documentation for phases 4 and 5 | not started |
| 7 | Effort settings on the portable `Infer` path | not started |

Phases 0–3 solve problems 2 and 3 of the intent (effort mapping, effort in
configuration) with no network code. Phases 4–5 solve problem 1 (provider
model churn).

## Ground rules

- **Compatibility is tested, not assumed.** `pkg/llmfactory/factory_test.go`
  and `pkg/llmfactory/exact_test.go` pass unmodified at the end of every phase.
  A phase that needs to edit one of them has changed behaviour and must say so.
- **One phase, one pull request.** Each phase leaves the tree shippable.
- **Repository rules apply**: `github.com/cockroachdb/errors` with wrapping,
  constants instead of repeated literals, `assert`/`require` tests, docs for
  every exported symbol, and `make generate && make fmt && make test && make lint`
  before each merge.
- **Run tests through `make`.** The Makefile exports fake provider keys, so the
  integration tests skip. A bare `go test` in a shell that has real keys calls
  the live APIs.

## Done: phases 0–3

### What was built

- `pkg/llms`: `CapabilityModels` (renamed from `CapacityModels`),
  `ParseProviderType` with `AZURE_AD` and a doc comment, typed
  `ReasoningEffort` constants with `ReasoningEffortMax`, `ParseReasoningEffort`,
  `EffortSet`, `EffortPolicy` (`effort.go`), and `ParseModelName`
  (`modelname.go`).
- `pkg/llms/{openai,anthropic,googleai}`: an `efforts.go` with the levels each
  model family and version accepts, and `WithEffortPolicy`.
- `pkg/llmfactory`: model references with an effort suffix and patterns
  (`modelref.go`), the per-provider policy with `disabled_models` and
  `max_effort` (`policy.go`), ordering of pattern matches (`rank.go`),
  `Config.Validate`, and resolution that hands each client its effort settings.

### Compatibility that was checked

- `factory_test.go` and `exact_test.go` pass without a change.
- For each provider, a test written against the old code pins what is sent
  when no level is requested (`TestReasoningEffort_Unchanged`). It passed
  before the refactoring and passes after it, so a deployment that sets no
  effort sends the same requests. The one exception is the OpenAI pro tier
  from 5.2, listed below.

### Behaviour that changed on purpose

All of these are requests that named a level, or a model the old name checks
got wrong.

| Provider | Before | Now |
|----------|--------|-----|
| OpenAI | `xhigh` and `max` fell through to `low` or `none` | Sent as the nearest level the model has |
| OpenAI | `none` was sent as `low`, except on `gpt-5.1` | Sent as `none` or `minimal`, whichever the model has |
| OpenAI | A requested level was dropped for IDs with a vendor prefix (`openai.gpt-5.6-luna`, `openai/gpt-5.1`) and for generations after GPT-5 | Sent |
| OpenAI | The level was looked up for the client's model, even when the call named another one | Looked up for the model in the request |
| OpenAI | Chat Completions never sent an effort | Sends a requested or configured level, and the ceiling when the model's own default is above it |
| OpenAI | The pro tier from 5.2 (`gpt-5.2-pro`, `gpt-5.4-pro`, `gpt-5.5-pro`) got `low` when no level was requested. These models accept `medium`, `high` and `xhigh` only. | `medium` |
| Anthropic | A requested level became a thinking budget of 1000, 5000 or 10000 tokens. 1000 is below the API minimum, 5000 and 10000 exceed the default `max_tokens`, and models from 4.7 on reject any budget. | From 4.6: adaptive thinking plus `output_config.effort`. Before 4.6: a budget of 1024, 5000 or 10000, with `max_tokens` raised above it. |
| Anthropic | Every 5.x model was treated as older than 4.6: legacy web tool version, no way to send an effort | Fable, Mythos, and Opus and Sonnet from 4.6 on get the newer web tool; 5.x models accept every level |
| Anthropic | A response format reset the output effort to `low` | The resolved effort is kept next to the format |
| Anthropic | `none` could not be requested | Lowest effort, and thinking turned off where the model allows it: `disabled` on Opus 5, Sonnet 5 and Haiku 5.5, `between_tools` on Sonnet 5.5 |
| Google | `medium` was sent as `LOW`; `none`, `xhigh` and `max` sent nothing | `MEDIUM` where the model has it; `none` is the model's lowest level; `xhigh` and `max` are `HIGH` |
| Google | A requested level was sent as `thinkingLevel` to every model. Gemini 2.5 rejects it. | Gemini 2.5 gets a `thinkingBudget` of 1024, 8192 or 24576 tokens, and 0 for `none` on Flash and Flash-Lite; models before 2.5 get nothing |

### Where the implementation differs from the plan

- **`EffortPolicy.Resolve` takes a third argument**, the level the model
  applies when a request names none. Without it the ceiling could not cap a
  model that reasons at `high` when nothing is sent (Gemini 3, Claude 5.x).
- **`EffortPolicy.Supported` is not there yet.** Nothing can fill it before
  discovery exists; it arrives with phase 5.
- **Model ID parsing lives in `pkg/llms`** (`ParseModelName`), not in the
  factory. The provider effort tables need it too, and phase 5 reuses it for
  family fallback.
- **`ProviderConfig.Discovery` was not added.** A key that is accepted and does
  nothing would mislead; it arrives with phase 5.
- **A `default_model` pattern is allowed** when it matches an ID listed in
  `available_models`. The design only allowed it with discovery.
- **Characterization tests cover the no-level case only.** For Anthropic the
  old requested-level requests were invalid, so there was nothing to keep.
- **Claude 5.x keeps sending no effort when none is requested.** Models 4.6 to
  4.9 get `output_config.effort: low` in that case, and 5.x did not because of
  the name check. Extending `low` to 5.x would have lowered quality for
  existing deployments, so the old default was kept. See open question 1.

## Phase 4 — Model listing

- [ ] `pkg/llms/models.go`: `ModelInfo`, `ModelLifecycle`, `ModelLister`.
- [ ] `openai` (`OPENAI`, `OPENROUTER`): list through the SDK client the
      package already builds lazily; map `shutdown_date` to `deprecated`.
- [ ] `anthropic` (direct API only): `Models.ListAutoPaging` with all three
      lifecycle stages, so a retired model still carries its `line`; map
      `capabilities.effort` to `EffortSet` and `line` to `Family`. Return an
      error in Bedrock mode.
- [ ] `googleai`: `Models.All`, keeping entries whose `SupportedActions`
      include `generateContent`; strip the `models/` prefix.
- [ ] `NewModelLister(opts...)` in each package, so a list can be fetched
      without naming a model (`anthropic.New` requires one).
- [ ] Each lister is a method on the provider client too, so
      `model.(llms.ModelLister)` works where the capability is advertised.

Tests: recorded list responses served over `httptest`, including pagination, an
empty list, an HTTP error, and for Anthropic one model in each lifecycle stage.
One opt-in integration test per provider behind the existing key check.

Done when: the four listers return normalized `ModelInfo` and no factory code
depends on them yet.

## Phase 5 — Catalog, discovery and family fallback

- [ ] `config.go`: `Discovery` (`enabled`, `refresh`); `Validate` rejects it on
      a provider type without `CapabilityModels`.
- [ ] `catalog.go`: per-provider catalog with its own lock; first fetch
      synchronous with a timeout; refresh on read in the background; last good
      list kept on failure; retry no more than once a minute; one fetch at a
      time per provider. No call is made while `factory.lock` is held.
- [ ] `NewModelLister` package variable beside `NewLLM`, selecting the lister
      from `open_ai.api_type`.
- [ ] `policy.go`: `candidates` reads the catalog when there is one; family
      fallback for an exact ID that is not usable, compared with
      `llms.ModelName.Key`; retired models excluded; `active` before
      `deprecated` in `rankModels`.
- [ ] `llms.EffortPolicy.Supported`, filled from the catalog and preferred by
      the provider over its table for the client's own model.
- [ ] `Catalog` interface (`Resolve`, `Refresh`) and `Resolution`, implemented
      by the factory and asserted like `ExactResolver`.
- [ ] `pkg/metricskey`: a substitution counter and a failed-fetch counter.
      Logging per the design's "Observability" section, once per catalog
      generation rather than once per call.
- [ ] `make generate` for the new interface mock.

Tests, all with a fake lister through `NewModelLister`: the matrix below; plus
refresh after expiry, stale list served during a failing refresh, first fetch
failure degrading to today's behaviour, concurrent `GetModel` during refresh
under `-race`, and a derived factory sharing the catalog.

Done when: the two incidents in the intent are reproduced as tests and pass —
`claude-fable-5-1` retired with `claude-fable-5-2` listed, and `gemini-2.5-pro`
gone with a newer `gemini-pro` listed.

### Resolution test matrix

Provider `ANTHROPIC`, `available_models: [claude-fable-*, claude-opus-4-8]`,
`disabled_models: ["*-preview"]`, discovery on unless stated.

| Reference | Provider lists | Expected | Reason |
|-----------|----------------|----------|--------|
| `claude-opus-4-8` | opus-4-8 | opus-4-8 | exact |
| `claude-fable-5-1` | fable-5-1, fable-5-2 | fable-5-1 | exact |
| `claude-fable-5-1` | fable-5-2 (5-1 retired) | fable-5-2 | family |
| `claude-fable-*` | fable-5-1 (deprecated), fable-5-2 | fable-5-2 | pattern |
| `claude-fable-*` | fable-5-2, fable-6-0-preview | fable-5-2 | pattern, deny-list |
| `claude-opus-4-6` | opus-4-6, opus-4-8 | opus-4-8 | family: 4-6 is listed but not allowed |
| `claude-sonnet-4-8` | sonnet-4-8 | skipped | no allowed model in the family |
| `claude-fable-5-1` | fetch fails, no earlier list | fable-5-1 | exact, unverified |
| `claude-fable-5-1`, discovery off | — | fable-5-1 | exact; allowed by `claude-fable-*`, never verified |
| `claude-fable-*`, discovery off | — | skipped | no listed ID matches the pattern |
| `claude-fable-5-1`, filter denies 5-2 | fable-5-2 | skipped | filter |

The two "discovery off" rows already hold today.

## Phase 6 — Documentation for phases 4 and 5

- [ ] `Documentation/llm-factory.md`: move the design section into the
      reference sections and delete the status notes. Keep the decisions table.
- [ ] `pkg/llmfactory/doc.go`, `README.md`: a sample with `discovery`.
- [ ] `Documentation/llms.md`: `ModelLister`, `ModelInfo`.
- [ ] `Documentation/codemap.md`, `Documentation/observability.md`: new files,
      symbols and counters.
- [ ] `ROADMAP.md`: tick the model routing items.

## Phase 7 — Effort settings on the `Infer` path

The router calls `Infer`, which uses `llms.Effort` (`minimal` to `high`) and its
own mapping, so neither the configured default nor `max_effort` applies there.

- [ ] Convert between `llms.Effort` and `llms.ReasoningEffort` in one place and
      apply the client's `EffortPolicy` in each provider's `Infer`.
- [ ] `anthropic/inference.go` turns an effort into `budget_tokens`
      (`thinkingBudget`). Models from 4.7 on reject a budget, so reasoning
      through the router fails on every current Claude model. Use the profile
      in `anthropic/efforts.go` instead.
- [ ] Decide whether to keep two effort vocabularies at all.

## Found while implementing, not changed

These are outside phases 0–3 and were left as they are.

- **Anthropic sampling parameters.** `GenerateContent` forwards `temperature`
  and `top_p` when a caller sets them. Claude models from 4.7 on reject
  sampling parameters with a 400.
- **Anthropic integration tests** fail when run with a real key, with or
  without this work: `TestIntegrationLegacyModels/claude-sonnet-4-0` (the model
  was retired on 2026-06-15), `TestIntegrationPromptCaching` and
  `TestIntegrationMaxTokens` (both read `GenerationInfo` keys the client does
  not set). `claude-sonnet-4-5`, used by other tests, retires on 2026-11-30.
- **MCP data races.** `mcp/internal/protocol` and `mcp/transport/stdio` fail
  under `go test -race` on the commit this work started from.
- **`anthropic.LLM.GetProviderType`** returns `ANTHROPIC` in Bedrock mode. The
  factory decides from `open_ai.api_type`, so resolution is unaffected.

## Risks for phases 4–5

| Risk | Mitigation |
|------|------------|
| A silent substitution changes cost or behaviour | Substitution only among allowed models; `INFO` log and counter on every swap; `Catalog.Resolve` shows the current answer |
| The family rule mis-groups an unusual ID | The rule only ever narrows: an ID it cannot split is its own family. `pkg/llms/modelname_test.go` covers real IDs from all providers |
| A provider list contains non-chat models | Nothing is usable unless `available_models` admits it; Google is filtered by supported action |
| Discovery adds latency or a failure mode to `GetModel` | Fetch outside the lock with a timeout; stale-while-refresh; failure degrades to today's behaviour |
| A wrong effort table rejects requests on a new model | Unknown models get `low`/`medium`/`high` or the newest known generation; with discovery, Anthropic levels come from the API |

## Open questions

1. **Should Claude 5.x get `output_config.effort: low` when no level is
   requested**, as 4.6 to 4.9 do? Today 5.x runs at the API default (`high`, or
   `medium` on Opus 5.5 and Haiku 5.5). Either way, an effort written in the
   configuration now decides. One line in `anthropic/efforts.go`.
2. **Should `gpt-5.6-sol` accept levels below `high`?** The old code forced it
   to `high` and the table keeps that, adding `xhigh` and `max`. The OpenAI
   model page lists `none` to `max` for it.
3. **Default `discovery.refresh` of one hour?** (Phase 5.)
4. **A per-organization `max_effort` under `orgs_override`?** Left out; an org
   can already set its own default effort on its references.

Decided: the capability is named `CapabilityModels`; patterns are globs, with
the `re:` prefix reserved; fallback picks the newest allowed model of the
family and needs discovery; `ReasoningEffortMax` exists and there is no
`minimal` level.

## Out of scope

- Price-aware selection. No list API used here reports prices consistently.
- Discovery for Azure deployments and Bedrock, which need management-plane
  clients this module does not depend on.
- Refreshing the catalog in reaction to a provider "model not found" error.
- Object-form config entries (`model:` / `effort:`).

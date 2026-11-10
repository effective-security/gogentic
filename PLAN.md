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
| 4 | `ModelLister` on four providers | done |
| 5 | Catalog, discovery, family fallback, `Catalog` interface | done |
| 6 | Documentation for phase 5 | done |
| 7 | Effort settings on the portable `Infer` path | dropped: out of scope |

Phases 0–3 solve problems 2 and 3 of the intent (effort mapping, effort in
configuration) with no network code. Phases 4–5 solve problem 1 (provider
model churn): phase 4 reads the lists, phase 5 makes the factory use them.
The plan is complete. Phase 7 was dropped: the router has its own logic.

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
  the live APIs. To call only the free list endpoints:
  `go test ./pkg/llms/... -run TestIntegrationListModels -v` and
  `go test ./pkg/llmfactory/ -run 'TestIntegrationDiscovery$' -v`.

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

## Done: phase 4 — model listing

### What was built

- `pkg/llms/models.go`: `ModelInfo`, `ModelLifecycle`, `ModelLister`,
  `ErrModelListNotSupported`.
- `pkg/llms/openai` (`OPENAI`, `OPENROUTER`): `LLM.ListModels` and
  `NewModelLister`, through the SDK client the package already builds lazily
  (`internal/openaiclient/models.go`).
- `pkg/llms/anthropic` (direct API): `LLM.ListModels` and `NewModelLister`,
  asking for all three lifecycle stages, with the model line as `Family` and
  `capabilities.effort` as `Efforts`.
- `pkg/llms/googleai`: `GoogleAI.ListModels` and `NewModelLister`, keeping the
  models that support `generateContent` and removing the `models/` prefix.
- Every client that advertises `CapabilityModels` implements `ModelLister`;
  the others return `ErrModelListNotSupported` without making a request.
- `mocks/mockllms`: `MockModelLister`.
- No factory code depends on any of this yet.

### How it was checked

- **Against the live APIs** on 2026-11-10, list endpoints only:

  | Provider | Result |
  |----------|--------|
  | Anthropic | 15 models: 14 active, 1 deprecated (`claude-sonnet-4-5-20250929`, retires 2026-11-30) |
  | OpenAI | 98 models, 48 with an announced shutdown date |
  | Google AI | 47 of 61 listed models support `generateContent` |
  | OpenRouter | 465 models, 220 report their effort levels. The list is public, so it was read with a placeholder token. |

- **Offline, against lists recorded from those APIs** (`testdata/models_*.json`
  in each provider package): two-page lists for Anthropic and Google, each
  lifecycle stage, an empty list, an HTTP error on the first and on a later
  page, a truncated OpenRouter list, and the provider types that have no list.
  The API returned no retired model, so that one entry of the Anthropic
  recording is written by hand.
- **The tests fail when the code is wrong.** Eight deliberate breakages were
  each caught: retired models not requested, the 4.6 effort table without
  `max`, `disabled` thinking for Sonnet 5.5, non-chat Google models kept, the
  `models/` prefix kept, a truncated list accepted, a shutdown date ignored,
  and an unsupported provider not refused.
- **The Anthropic effort table of phase 2 agrees with the API** for all 15
  models the API returned: effort levels, thinking mode, and the way thinking
  is turned off (`anthropic/efforts_recorded_test.go`). This includes `max` on
  Sonnet 4.6.

### Where the implementation differs from the plan

- **`googleai.NewModelLister` takes a context**, like `googleai.New`.
- **OpenRouter reports more than planned**: an expiration date, mapped like
  the OpenAI shutdown date, and the reasoning levels of each model, mapped to
  `Efforts`. Its `minimal` level has no portable name and is left out (it was
  reported as `none` until phase 5).
- **OpenAI models without a shutdown date are `active`**, not unknown: the API
  sends `shutdown_date: null` for them. A list without that field (an
  OpenAI-compatible server) leaves the lifecycle empty.
- **A truncated OpenAI or OpenRouter list is an error.** Neither API pages its
  list today. OpenRouter states a total and a next link; if either says there
  is more than was returned, `ListModels` fails rather than let the missing
  models look retired.
- **`ErrModelListNotSupported`** is a sentinel in `pkg/llms`, so phase 5 can
  tell "this provider has no list" from a failed fetch.
- **`ModelName.Variant`** was added. The cross-check below showed that a
  router variant such as `openai/gpt-5.5:batch` was not parsed, so those IDs
  got no effort at all. The variant is now split off and is part of
  `ModelName.Key`, so family fallback will not move between variants.
- **The Makefile sets `OPENROUTER_API_KEY=fakekey`**, like the other keys, so
  `make test` never runs the OpenRouter list test live.

### Found by checking the OpenAI effort table against OpenRouter

OpenRouter publishes the reasoning levels of every model it routes. For the 98
`openai/*` models, 51 matched the table in `openai/efforts.go`. The
differences that matter, none of them changed here because OpenRouter is not
the first-party source:

- **GPT-6** (`gpt-6-luna`, `gpt-6-sol`, `gpt-6-astra`, `gpt-6.1-terra`,
  `gpt-6.1-sol`): OpenRouter lists `low` to `max`. The table treats an unknown
  generation as `low`, `medium`, `high`, so `xhigh` and `max` are sent as
  `high`. Nothing is rejected. Since phase 5 an OpenRouter provider with
  discovery uses the levels the list reports; a direct OpenAI provider still
  cuts them to `high`.
- **`gpt-5.1-codex`, `gpt-5.1-codex-mini`, `gpt-5.1-codex-max`**: OpenRouter
  lists no `none` level, and `xhigh` on `codex-max`. The client sends `none`
  to every `gpt-5.1*` model when no level is requested, as it did before
  phase 2. The OpenAI model pages do not state the levels of these models.
- **OpenRouter's default is `medium` for `openai/gpt-5.1` to `5.4`**, where
  the OpenAI API defaults to `none`. `max_effort` on an OpenRouter provider
  therefore does not cap a call that requests no level on those models.
  Solved in phase 5 for a provider with discovery: `reasoning.default_effort`
  is read from the list.
- `openai/gpt-5.2-chat` and the `gpt-5-image` models report no levels; the
  table gives them the levels of their generation. `openai/gpt-oss-*` report
  `low`, `medium`, `high`; the table gives them none.

## Done: phases 5 and 6 — discovery and family fallback

### What was built

- `config.go`: `ProviderConfig.Discovery` (`enabled`, `refresh`). `Validate`
  rejects it on a provider type without `CapabilityModels`, and a refresh that
  is not a duration of at least one minute.
- `catalog.go`: one model list per provider with its own lock. The first
  fetch is waited for, for at most 10 seconds; later ones run in the
  background; a failed one keeps the last good list and is retried at most
  once a minute; one fetch runs at a time. Nothing is fetched while the
  factory lock is held.
- `lister.go`: `CreateModelLister` and the `NewModelLister` variable, beside
  `NewLLM`.
- `policy.go`: `known`, `candidates` and `defaultCandidates` read the list;
  `family` is the fallback for an exact ID that cannot be used; retired models
  are excluded; `rankModels` puts a deprecated model last.
- `resolve.go`: resolution as a sequence of selections, shared by `GetModel`
  and `Resolve`.
- `resolution.go`: the `Catalog` interface (`Resolve`, `Refresh`),
  `Resolution`, and the `Resolve` and `RefreshModels` helpers.
- `llms.EffortPolicy.Supported` and `Implicit`, and `llms.ModelInfo.DefaultEffort`:
  what a provider reports for a model replaces the provider's table for the
  client of that model.
- `pkg/metricskey`: `stats_llm_model_substituted` and
  `stats_llm_model_list_failed`. Log events once per fetched list.
- `mocks/mockllmfactory`: `MockCatalog`.
- Documentation: the design section of `Documentation/llm-factory.md` became
  the "Model discovery" reference; `llms.md`, `codemap.md`,
  `observability.md`, `README.md`, `ROADMAP.md` and `pkg/llmfactory/doc.go`
  follow.

### How it was checked

- **The two incidents of the intent are tests.**
  `TestCatalog_RetiredModelIsReplaced`: `claude-fable-5-1` is retired between
  two refreshes and the assistant moves to `claude-fable-5-2`, keeping its
  effort. `TestCatalog_MissingModelIsReplaced`: `gemini-2.5-pro` leaves the
  list and a newer `gemini-pro` takes over, while a Flash model does not stand
  in for Flash-Lite.
- **The resolution matrix below is `TestCatalog_ResolutionMatrix`**, each row
  checked through both `Resolve` and `GetModel`.
- **Against the live APIs** on 2026-11-10 (`TestIntegrationDiscovery`, list
  endpoints only):

  | Reference | Resolved to | Reason |
  |-----------|-------------|--------|
  | `ANTHROPIC/claude-sonnet-4-20250514` (retired) | `claude-sonnet-5-5` | family |
  | `ANTHROPIC/claude-3-5-haiku-20241022` (retired) | `claude-haiku-5-5` | family |
  | `ANTHROPIC/claude-opus-*,high` | `claude-opus-5-5` | pattern |
  | `ANTHROPIC/claude-haiku-4-5` (listed only by date) | `claude-haiku-4-5` | exact |
  | `OPENAI/gpt-4.5-preview` (retired) | `gpt-5.5` | family |
  | `OPENAI/gpt-5*-mini` | `gpt-5.4-mini` | pattern |
  | `GOOGLE/gemini-1.5-pro` (retired) | `gemini-2.5-pro` | family |
  | `GOOGLE/gemini-1.5-flash` (retired) | `gemini-3.8-flash` | family |
  | `OPENROUTER/anthropic/claude-3-opus` (retired) | `anthropic/claude-opus-5.5` | family |

- **Before and after, live** (`TestIntegrationRetiredModelStillAnswers`, one
  billed Haiku request): a direct call to `claude-3-5-haiku-20241022` returns
  `404 not_found_error`; the same reference through a factory with discovery
  is answered by `claude-haiku-5-5`.
- **End to end offline** (`TestFactory_DiscoveryReachesTheWire`): the real
  listers and provider clients over the lists recorded in phase 4. The model a
  reference resolves to is the one named in the provider request.
- **The tests fail when the code is wrong.** Twenty-two deliberate breakages
  were each caught, among them: a retired model still served, the snapshot
  rule removed, an empty list accepted, a substitute preferred to the named
  model, a failed refresh dropping the list, the filter not shown a
  substitute, and reported effort levels ignored. Two of them were missed at
  first and led to new tests (`TestCatalog_FilterSeesTheSubstitute` and two
  deny-list rows of the matrix).
- **Concurrency**: `TestCatalog_ConcurrentResolutions` under `-race`.
- `factory_test.go` and `exact_test.go` pass without a change.

### Where the implementation differs from the design

- **A model that is not listed is gone.** The design relied on the `retired`
  stage; Anthropic returns no retired model even when asked.
- **A floating ID is served by its dated snapshot.** Anthropic accepts
  `claude-haiku-4-5` and lists only `claude-haiku-4-5-20251001`. Without this
  rule the first one, which works, would have been replaced by Haiku 5.5.
- **The named model on any provider wins over a substitute.** As designed, an
  unqualified `gpt-5.5` would have resolved to an older model on an earlier
  provider that only allows `gpt-5.1`, instead of reaching the provider that
  has it.
- **The family is the parsed ID only.** The design compared the family the
  provider reports when both models had one. Anthropic's `line` is wider than
  the name, and one rule for every provider is easier to predict.
- **An empty list is a failed fetch**, so an outage that returns nothing
  cannot make every model look retired.
- **`Resolution` says more**: `Default`, `Verified`, `Lifecycle` and
  `RetiresAt`. `Reason` is `exact`, `pattern` or `family`; a default model is
  flagged by `Default` rather than by a fourth reason, so a substituted
  default is still visible.
- **`EffortPolicy.Implicit` and `ModelInfo.DefaultEffort`** were added, so
  that `max_effort` caps a call that requests no level on OpenRouter, where
  the default is `medium` for models whose OpenAI default is `none`.
- **OpenRouter's `minimal` level is no longer reported as `none`.** It has no
  portable name. The OpenAI client keeps sending `minimal` for the portable
  `none` on the models its table knows to have it.
- **By provider type**, a provider with a usable default model now shares the
  client cache of name-based resolution; the `byType` cache is left for a
  provider without a default.
- **Each log event is written once per fetched list**; the substitution
  counter counts every resolution.
- **A default refresh of one hour** (open question 3).

### Resolution test matrix

Provider `ANTHROPIC`, `available_models: [claude-fable-*, claude-opus-4-8]`,
`disabled_models: ["*-preview"]`, discovery on unless stated.

| Reference | Provider lists | Expected | Reason |
|-----------|----------------|----------|--------|
| `claude-opus-4-8` | opus-4-8 | opus-4-8 | exact |
| `claude-fable-5-1` | fable-5-1, fable-5-2 | fable-5-1 | exact |
| `claude-fable-5-1` | fable-5-2 (5-1 retired) | fable-5-2 | family |
| `claude-fable-5-1` | fable-5-2 (5-1 not listed) | fable-5-2 | family |
| `claude-fable-*` | fable-5-1 (deprecated), fable-5-2 | fable-5-2 | pattern |
| `claude-fable-*` | fable-5-3 (deprecated), fable-5-2 | fable-5-2 | pattern: in service before deprecated |
| `claude-fable-*` | fable-5-3 (retired), fable-5-2 | fable-5-2 | pattern: retired excluded |
| `claude-fable-*` | fable-5-2, fable-6-0-preview | fable-5-2 | pattern, deny-list |
| `claude-fable-*` | fable-6-0-preview | skipped | deny-list |
| `claude-fable-5-1` | fable-6-0-preview | skipped | deny-list |
| `claude-opus-4-6` | opus-4-6, opus-4-8 | opus-4-8 | family: 4-6 is listed but not allowed |
| `claude-sonnet-4-8` | sonnet-4-8 | skipped | no allowed model in the family |
| `claude-fable-5-1` | fetch fails, no earlier list | fable-5-1 | exact, unverified |
| `claude-fable-5-1`, discovery off | — | fable-5-1 | exact; allowed by `claude-fable-*`, never verified |
| `claude-fable-*`, discovery off | — | skipped | no listed ID matches the pattern |
| `claude-fable-5-1`, filter denies 5-2 | fable-5-2 | skipped | filter |

## Found while implementing, not changed

These are outside phases 0–3 and were left as they are.

- **Router reasoning on Claude.** `anthropic/inference.go` turns an effort
  into `budget_tokens` on the portable `Infer` path. Claude models from 4.7 on
  reject a budget. The router is out of scope here and has its own logic.
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

## Risks of discovery, and what covers them

| Risk | Mitigation |
|------|------------|
| A silent substitution changes cost or behaviour | Substitution only among allowed models; `INFO` log and counter on every swap; `Resolve` shows the current answer |
| A model the provider accepts but does not list is replaced | A floating ID is served by its listed snapshot; `model_not_listed` is logged; discovery is opt-in per provider |
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
3. ~~Default `discovery.refresh` of one hour?~~ Decided: one hour, minimum
   one minute.
4. **A per-organization `max_effort` under `orgs_override`?** Left out; an org
   can already set its own default effort on its references.

Decided: the capability is named `CapabilityModels`; patterns are globs, with
the `re:` prefix reserved; fallback picks the newest allowed model of the
family and needs discovery; `ReasoningEffortMax` exists and there is no
`minimal` level.

## Out of scope

- The router and its portable `Infer` path. The router has its own logic for
  reasoning (`llms.Effort`), so the effort default and `max_effort` of the
  factory apply to `GenerateContent` only. This was phase 7 of the plan.
- Price-aware selection. No list API used here reports prices consistently.
- Discovery for Azure deployments and Bedrock, which need management-plane
  clients this module does not depend on.
- Refreshing the catalog in reaction to a provider "model not found" error.
- Object-form config entries (`model:` / `effort:`).

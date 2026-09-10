# Semantic inference router

`router` performs one non-streaming text inference against a registered target.
It validates a portable request, selects a target exactly or through a semantic
selector, runs the target's connector, normalizes the result and returns typed
errors. It serves no HTTP. Codecs under `router/dialect` translate the public
OpenAI Chat Completions, OpenAI Responses and Anthropic Messages shapes to and
from the portable types; the host application owns the transport, authentication,
quotas, budgets and logging. The full specification is
[router/DESIGN.md](../router/DESIGN.md).

## Host handler pattern

```text
authenticate → read bounded body → dialect Decode* → router.Generate → Encode* / EncodeError → write
```

`router/dialect/openai/example_test.go` and `router/dialect/anthropic/example_test.go`
contain compiling handlers. In short:

```go
raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
req, err := openaidialect.DecodeChat(raw, dialect.DecodeOptions{})
res, err := rt.Generate(r.Context(), req)          // ctx carries trusted identity
if err != nil {
    status, body := openaidialect.EncodeError(err)  // sanitized envelope, never provider text
    w.WriteHeader(status); w.Write(body); return
}
body, err := openaidialect.EncodeChat(res, dialect.EncodeOptions{})
```

`dialect.HTTPStatus(kind)` gives the default status per error kind; hosts may map
kinds differently. `router.AsError(err).Cause` retains diagnostics for logs.

## Construction

Build providers with their normal options, adapt each with `router.Connector`,
and register targets. Features are operator claims; the connector's own
`ValidateInference` still applies on top of them.

```go
model, err := openai.New(openai.WithModel("gpt-5"), openai.WithToken(token))
connector, err := router.Connector(model)
rt, err := router.New(router.Config{
    Targets: []router.Target{{
        ID:           "chat",
        Aliases:      []string{"default"},
        BackendModel: "gpt-5",
        Connector:    connector,
        Features:     router.Features{Tools: true, JSONSchema: true, StrictSchema: true, StrictTools: true, SchemaWithTools: true, Reasoning: true, Seed: true},
        Tags:         map[string]string{"tier": "standard"},
        Allow:        func(ctx context.Context) bool { return tenantFrom(ctx).MayUse("chat") },
    }},
    Admission: router.ConcurrencyLimit(64), // optional; nil = host handles admission
})
```

From the factory: `llmfactory.ResolveExact(ctx, factory, providerName, modelID, orgID)`
returns the configured model without preferred-model fallback (`ErrModelNotFound`
otherwise); pass it to `router.Connector`. `Target.Allow` must still check trusted
context on every request; nil means the target is visible to all callers.

Bedrock's portable path requires `bedrock.WithConverse()` or `open_ai.converse: true`.
OpenAI's portable path uses the Responses API upstream for `OPENAI` and Chat
Completions for other OpenAI-compatible types; override with
`openai.WithInferenceAPI` or `open_ai.inference_api`.

## Selection, admission and accounting

Without a selector, `Request.Model` must be a target ID or alias; unknown or
invisible names return `KindModelNotFound`. A `Selector` receives a read-only copy
of the request and the eligible `Candidate`s (ID, `Features`, `Tags`) and returns a
`Decision` with a candidate ID, optional classification (≤128 bytes), confidence in
[0,1] and optional selector usage. Selector failures return
`KindSelectionUnavailable` unless `Config.Fallback` names `router.FallbackRequested`
or a target ID; fallbacks are revalidated against the same eligible set.

`Config.Admission` is an optional interface: `Admit(ctx, AdmissionRequest)` returns
a release function or an error. A returned `*router.Error` is reported as-is
(a budget layer can return `KindInvalidRequest` with its own message); any other
error becomes `KindRateLimited`. `router.ConcurrencyLimit(n)` is a convenience
implementation. `Config.Timeout` is optional; zero imposes no router deadline.

`Config.Observe` receives target and backend model IDs, classification, duration,
selector usage, backend usage and the internal error cause, with the caller's live
context. It never receives request text. When a provider's counters do not add up,
the result reports `UsageKnown=false` and the observation sets `UsageInconsistent`.

Usage normalization: `InputTokens` includes cache reads and writes once;
`ReasoningTokens` is a subset of `OutputTokens`. Anthropic and Bedrock report
uncached input separately and the connectors add the cache counters. Codecs
re-derive dialect-native shapes (Anthropic `input_tokens` excludes cache). No token
or cost estimates are fabricated.

## Reasoning passthrough

Connectors return reasoning as `BlockReasoning` blocks: `Text` holds the provider's
summary or thinking text (may be empty) and `Opaque` holds the state the provider
needs back (Responses reasoning item, Anthropic `thinking` block with signature,
Gemini `thoughtSignature`, Bedrock `reasoningContent`). The Go `Result` always
contains them so applications can store or audit them.

On output, codecs seal `Opaque` together with the producing target ID into one
string placed where each dialect has an opaque slot: Responses
`reasoning.encrypted_content`, Anthropic `thinking.signature` /
`redacted_thinking.data`, Chat `reasoning_details[].data`. Clients replay these
verbatim. On input the codec restores `Opaque` and `Source`; the router only keeps
a target eligible when every replayed state was produced by that target, so a
selector cannot forward one provider's state to another. Applications that want
cross-target continuations drop reasoning blocks. `EncodeOptions.OmitReasoning`
removes reasoning from the public response while keeping it in the Go result.

Reasoning controls (`reasoning_effort`, `reasoning.effort`, Anthropic `thinking`)
map to `InferenceReasoning{Effort, BudgetTokens}`. Connectors use a native effort
when the provider has one, otherwise `llms.ReasoningBudget(effort)`. A target must
declare `Features.Reasoning` to accept them.

## Ingress strictness

`dialect.DecodeOptions{Strict:false}` (default) ignores unknown fields at every
level and treats `null` as omitted, so SDK-shaped replays (assistant messages with
`refusal: null`, `annotations: []`, Responses `output_text` parts with `logprobs`)
decode. Known fields whose value is unsupported and not the provider default are
rejected in both modes with an error naming the field. `Strict:true` additionally
rejects unknown fields, duplicate keys and nulls.

## Field ledgers

### OpenAI Chat Completions (`router/dialect/openai`)

`DecodeChat` / `EncodeChat` / `EncodeError`.

| Field | Handling |
|-------|----------|
| `model` | → `Request.Model` |
| `messages[]` roles system, developer, user, assistant, tool | content string or `[{type:text}]`; assistant parts may be `{type:refusal}` |
| assistant `tool_calls[]` (`type:function`) | → function call blocks |
| assistant `refusal` (string; null accepted) | → refusal block |
| assistant `reasoning_details[]` (`reasoning.encrypted`, `reasoning.summary`, `reasoning.text`) | → reasoning blocks; `data` must be router-issued |
| tool `tool_call_id` + content | → one function output block |
| `tools[].function{name,description,parameters,strict}` | → tools (missing parameters → empty object schema) |
| `tool_choice` `auto`/`none`/`required` or `{type:function,function{name}}` | → tool choice |
| `parallel_tool_calls`, `temperature`, `top_p`, `seed` | pointer fields; explicit 0 preserved |
| `max_tokens` xor `max_completion_tokens` | → `MaxTokens`; both → invalid_request |
| `stop` string or array | → `Stop` |
| `response_format` text / json_object / json_schema{name,schema,strict} | → output format |
| `reasoning_effort` minimal/low/medium/high | → `Reasoning.Effort` |
| `metadata`, `user`/`safety_identifier` | → `Request.Metadata` (`user` key) |
| Defaults accepted | `n:1`, `stream:false`, `store:false`, `logprobs:false`, `top_logprobs:0`, `logit_bias:{}`, penalties 0, `modalities:["text"]`, `service_tier` auto/default, `verbosity:"medium"` |
| Rejected (`unsupported`, param named) | `n≠1`, `stream:true`, `stream_options`, `store:true`, `logprobs:true`, `top_logprobs>0`, non-empty `logit_bias`, nonzero penalties, other `modalities`, `audio`, `prediction`, `functions`, `function_call`, `web_search_options`, other `service_tier`/`verbosity`/`reasoning_effort`, non-function tools or choices, non-text content parts |
| Ignored in lenient, unknown in strict | `prompt_cache_key`, `prompt_cache_retention`, any unlisted field |

Response: `chat.completion` with a `chatcmpl_` ID, one choice, `refusal` null or
string, `tool_calls` when present, `reasoning_details` unless omitted, `usage`
only when known. Encoded output decodes back in strict mode.

### OpenAI Responses (`router/dialect/openai`)

`DecodeResponses` / `EncodeResponses` / `EncodeError`.

| Field | Handling |
|-------|----------|
| `model`, `instructions` | → `Request.Model`; leading system message |
| `input` string | → one user text message |
| `input[]` `message` items (system/developer/user `input_text`; assistant `output_text`/`refusal`) | → messages; consecutive assistant items merge into one turn |
| `function_call{call_id,name,arguments}` (`id`, `status` ignored) | → assistant function call blocks |
| `function_call_output{call_id, output string or input_text parts}` | → tool message; consecutive outputs merge into one turn |
| `reasoning{summary[], encrypted_content}` | → reasoning block on the assistant turn; `encrypted_content` must be router-issued |
| `tools[]` flat function, `tool_choice` string or `{type:function,name}` | → tools and tool choice |
| `parallel_tool_calls`, `temperature`, `top_p`, `max_output_tokens` | pointer fields |
| `text.format` (inline json_schema), `text.verbosity:"medium"` | → output format |
| `reasoning{effort}` (`summary` ignored) | → `Reasoning.Effort` |
| `include:["reasoning.encrypted_content"]`, `store:false`, `stream:false`, `background:false`, `truncation:"disabled"`, `service_tier` auto/default | accepted |
| `metadata`, `user`/`safety_identifier` | → `Request.Metadata` |
| Rejected (`unsupported`) | `stream:true`, `store:true`, `background:true`, `truncation:"auto"`, `previous_response_id`, `conversation`, `prompt`, `max_tool_calls`, `top_logprobs>0`, other `include`, other `service_tier`/`verbosity`/`effort`, `item_reference`, built-in tools, non-text parts |
| `invalid_request` | `role:"tool"` message items (use `function_call_output`), missing `input`, tampered `encrypted_content` |

Response: `response` object with ordered `reasoning`, `message` and
`function_call` items, `status` completed or incomplete with
`incomplete_details` (`max_output_tokens`, `content_filter`), `usage` null when
unknown, `store:false`, and echoes of the accepted request controls. Errors use
`{"error":{"message","type","param","code"}}`.


### Anthropic Messages (`router/dialect/anthropic`)

`DecodeMessages` / `EncodeMessages` / `EncodeError`.

| Field | Handling |
|-------|----------|
| `model` | → `Request.Model` |
| `max_tokens` | required by the dialect (400 `max_tokens` if absent) → `MaxTokens` |
| `system` | string or `[{type:text,text}]` → leading system message |
| `messages[].role` | `user`, `assistant` only |
| `messages[].content` | string or blocks: `text`; assistant `tool_use{id,name,input}` → function call with raw input bytes; user `tool_result{tool_use_id, content string or text blocks}` → one tool message per user turn holding all results, followed by a user message for any text; assistant `thinking{thinking,signature}` / `redacted_thinking{data}` → reasoning block (signature/data must be router-issued) |
| `tools[]` | custom tools only: `name`, `description`, `input_schema` (default empty object schema), `strict`; any other `type` → unsupported |
| `tool_choice` | `auto`, `any` (→ required), `tool{name}` (→ function), `none`; `disable_parallel_tool_use` → `ParallelToolCalls` |
| `temperature`, `top_p`, `stop_sequences` | → same |
| `thinking` | `enabled{budget_tokens}` → `Reasoning{BudgetTokens}`; `adaptive` → `Effort: medium`; `disabled` → none |
| `output_config.format` | `json_schema{schema, name?}` → strict JSON schema output (name defaults to `output`) |
| `output_config.effort` | low/medium/high → `Reasoning.Effort` when `thinking` is absent |
| `metadata.user_id` | → `Metadata["user"]` |
| `stream` | false accepted; true → unsupported |
| `service_tier` | `auto`, `standard_only` accepted; others → unsupported |
| `top_k`, `container`, `context_management`, non-empty `mcp_servers`/`betas`, server tools, non-text blocks | → unsupported, naming the path |
| `cache_control`, `tool_result.is_error:true` | lenient: ignored; strict: unsupported |
| unknown keys, nulls, duplicate keys | lenient: ignored; strict: 400 naming the path |

Response: `message` with `thinking`/`redacted_thinking` (sealed state in
`signature`/`data`), `text`, `tool_use` blocks; `stop_reason` `end_turn`,
`max_tokens`, `tool_use` or `refusal`; `stop_sequence` always null; usage with
Anthropic-native `input_tokens` (cache excluded) and zeros when usage is unknown.
Refusal blocks render as text with `stop_reason: refusal`. Error envelope
`{"type":"error","error":{"type","message"}}` with `invalid_request_error`,
`not_found_error`, `rate_limit_error` or `api_error`.


## Connector restrictions

| Connector | Upstream | Supports | Rejects (`unsupported`, param) |
|-----------|----------|----------|--------------------------------|
| OpenAI, Responses path (default for `OPENAI`) | `POST /responses`, `store:false`, `stream:false` | all roles, refusal history, function calls/outputs, reasoning items replayed verbatim, tools + strict, tool choice, parallel control, temperature, top_p, max_output_tokens, text/json_object/json_schema (+strict), `reasoning{effort, summary:auto}` with `include:[reasoning.encrypted_content]` | `stop`, `seed`, `reasoning.budget_tokens` |
| OpenAI, Chat path (default for Azure, AzureAD, OpenRouter, Perplexity, OpenAI-on-Bedrock, Cloudflare) | `POST /chat/completions`, `stream:false` | same via Chat shapes, `max_completion_tokens`, `stop`, `seed`, `reasoning_effort`; reasoning and refusal history are dropped and no reasoning blocks are returned | `reasoning.budget_tokens`, text after a function call in one assistant turn (`messages.content.order`) |
| Anthropic | `POST /v1/messages` | system blocks, merged same-role turns (parallel results in one user turn), tools + strict, tool choice incl. `disable_parallel_tool_use`, temperature ≤ 1 or top_p, stop sequences, `output_config.format` json_schema, `thinking` from budget or effort (max_tokens defaults to 4096 + budget), thinking/redacted_thinking passthrough | `seed`, developer role, json_object, temperature > 1, temperature with top_p; with thinking: temperature, top_p, forced tool choice, max_tokens ≤ budget, unresolvable effort |
| GoogleAI | `generateContent` | system instruction, tools, tool choice auto/none/required/function, temperature, top_p, max_tokens, stop, seed, json_object, raw json_schema, `thinkingConfig` from budget or effort, thought and thoughtSignature passthrough, merged same-role turns | `parallel_tool_calls`, `tools.strict`, developer role, unresolvable reasoning effort |
| Bedrock Converse (opt-in) | `Converse` | system, tools (+strict), tool choice auto/required/function, temperature ≤ 1, top_p, max_tokens, stop, json_schema, `thinking` via `additionalModelRequestFields`, reasoningContent passthrough | Converse not enabled, `seed`, `parallel_tool_calls`, `tool_choice: none`, json_object, developer role, temperature > 1; with thinking: temperature, top_p, max_tokens ≤ budget |

Finish reasons and usage follow the normalization tables in
[DESIGN.md](../router/DESIGN.md#6-normalization-tables). Gemini function calls
without a provider ID get a synthetic `gfc_` ID that is stripped on replay.


Accepted JSON Schema keywords are `type`, `title`, `description`, `enum`,
`properties`, `required`, boolean `additionalProperties`, and `items`. The root is
an object; arrays need an items schema; every node needs a type; numbers are
limited to 128 characters with exponent magnitude at most 308. Other keywords are
rejected, never stripped. Strict objects require every property and
`additionalProperties:false`. Tool names are at most 64 ASCII characters matching
`[a-zA-Z_][a-zA-Z0-9_-]*`; `auto`, `none`, `required` and `function` are reserved.

## Result validation

Contract violations return `KindUpstream`: unknown block types or tool names,
duplicate call IDs, calls under `tool_choice: none` or a different named function,
non-object arguments, strict tool arguments or completed JSON output violating the
schema, empty output with finish `stop`/`tool_calls`, and a required tool not
called without a refusal. Bookkeeping differences are normalized: `stop` with tool
calls becomes `tool_calls`, empty output with `length`/`content_filter` is accepted,
and inconsistent usage becomes unknown.

## Tool continuation

Chat: replay the assistant `tool_calls` and one `tool` message per call ID.
Responses: replay the `function_call` items and one `function_call_output` per call.
Anthropic: replay the `tool_use` blocks and one `tool_result` block per ID in the
next user message. Every pending call must be resolved exactly once before the next
non-tool message; duplicates and orphans fail before inference. Connectors deliver
parallel results in one provider turn where the provider requires it.

## Limits and errors

Core limits: 1024 messages, 256 blocks per message, 128 tools, 2 MiB text and
arguments, 256 KiB opaque state per block, 64 KiB per schema, 16 schema levels,
32 JSON levels, 4 stop strings, 16 metadata pairs, 32 target tags, output limit
1–1,048,576 tokens. `Target.MaxOutputTokens` caps explicit values and is the
default when the request omits one. Selector deadline defaults to 5s.

| Kind | Meaning | Default HTTP |
|------|---------|--------------|
| `invalid_request` | malformed or out-of-bounds input | 400 |
| `unsupported` | well-formed control the selected model cannot honor | 400 |
| `model_not_found` | unknown, unregistered or invisible model | 404 |
| `rate_limited` | admission rejection or upstream rate limit | 429 |
| `selection_unavailable` | selector failure without recovery | 503 |
| `upstream_error` | provider failure or invalid provider response | 502 |
| `timeout` | deadline exhausted before a response | 504 |
| `cancelled` | caller cancellation | 499 (never written) |
| `internal_error` | unexpected failure | 500 |

Error messages never contain provider text; `AsError(err).Cause` does.

## Validation status

Offline verification on 2026-09-10 (fixtures, not live certification):

- `router/conformance_test.go`: 3 dialects × 5 connector configurations
  (OpenAI Responses, OpenAI Chat, Anthropic, GoogleAI, Bedrock Converse) × 9 modes
  (text, tool, parallel continuation, schema, schema + tools, reasoning round
  trip, refusal, length, usage absent) through real SDK serialization and mocked
  transports; foreign reasoning state refused per connector; upstream 429
  sanitized per dialect.
- Per-package unit tests for the router core, both codec packages (including
  OpenAI Go SDK and Anthropic Go SDK client round trips, strict/lenient modes and
  fuzzing), every connector, the exact factory resolver and the schema validator.
- `make generate`, `make fmt`, `make test` and `make lint` pass.

Live smoke (`GOGENTIC_ROUTER_LIVE=1`, `GOGENTIC_ROUTER_CONFIG`, and
`GOGENTIC_ROUTER_<FAMILY>_PROVIDER` / `_MODEL` for OPENAI, ANTHROPIC, GOOGLEAI,
BEDROCK; `go test ./router -run TestLiveSmoke -v`) exercises text, a tool call and
a strict schema through all three dialects per configured deployment. It has not
been run for this revision; no live-provider feature is certified until it is.


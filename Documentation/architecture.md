# Architecture

gogentic is a library, not a framework: there is no runtime to start and no
global registry. You compose a small number of interfaces and call them.

## Layering

```
        ┌──────────────────────────────────────────────────────────────┐
        │  your application                                            │
        └──────────────────────────────────────────────────────────────┘
                  │                          │                  │
                  ▼                          ▼                  ▼
        ┌───────────────────┐      ┌──────────────────┐  ┌──────────────┐
        │  assistants       │◀────▶│  tools           │  │  mcp         │
        │  (agent loop)     │      │  (ITool)         │  │ server/client│
        └───────────────────┘      └──────────────────┘  └──────────────┘
             │      │    │                 │
             │      │    └── skills ───────┘
             │      │
             │      └── callbacks ── observability, scratchpad, metrics
             │
             ├── pkg/prompts ──── system prompt rendering
             ├── encoding + pkg/schema ── structured output / JSON schema
             ├── store ────────── message history (memory, Redis)
             ├── chatmodel ────── ChatContext, I/O types, OutputParser
             │
             ▼
        ┌───────────────────────────────────────────────┐
        │  pkg/llmfactory  → resolves config to a Model │
        └───────────────────────────────────────────────┘
                              │
                              ▼
        ┌───────────────────────────────────────────────┐
        │  pkg/llms  (Model interface, messages, opts)  │
        │  openai · anthropic · bedrock · googleai ·    │
        │  cloudflare  (+ Azure/OpenRouter/Perplexity   │
        │               via the openai client)          │
        └───────────────────────────────────────────────┘
```

Dependencies point downwards only. `pkg/llms` knows nothing about assistants;
`assistants` knows nothing about MCP transports.

## Package map

| Package                                                   | Responsibility                                                                                                                     |
| --------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `assistants`                                              | The agent loop: system prompt → LLM → tool calls → typed result. Also `AssistantTool`, which turns an assistant into a tool.       |
| `tools`                                                   | The `ITool` contract every tool implements, plus typed and MCP-aware variants.                                                     |
| `tools/tavily`                                            | Reference tool implementation (Tavily web search).                                                                                 |
| `skills`                                                  | Agent Skills (<https://agentskills.io>): filesystem/tar discovery of `SKILL.md`, and the `activate_skill` tool.                    |
| `callbacks`                                               | Implementations of the assistant/tool callback interfaces: printer, package logger, fanout, scratchpad.                            |
| `chatmodel`                                               | `ChatContext` (user/org/chat/run identity), input & output value types, `OutputParser[T]`, sentinel parse errors.                  |
| `encoding`                                                | Turns a Go type into prompt "format instructions" and parses model output back into it. JSON / YAML / TOML / plain-text back ends. |
| `store`                                                   | Message history: in-memory and Redis, keyed by tenant + chat.                                                                      |
| `mcp`                                                     | MCP server and client.                                                                                                             |
| `mcp/transport/...`                                       | `stdio`, `httptransport` (HTTP + SSE endpoint), `sse` (server push), `localtransport` (in-process).                                |
| `pkg/llms`                                                | `Model` interface, message/content types, `CallOption`s, provider capability table, prompt caching and batch contracts.            |
| `pkg/llms/{openai,anthropic,bedrock,googleai,cloudflare}` | Provider implementations.                                                                                                          |
| `pkg/llmfactory`                                          | Config → `llms.Model`. Assistant→model mapping, per-org overrides, capability filtering, model allow/deny filter.                  |
| `pkg/prompts`                                             | Prompt templates (Go template, Jinja2, f-string), chat prompts, few-shot.                                                          |
| `pkg/schema`                                              | Go type → JSON Schema, and JSON Schema → provider response format.                                                                 |
| `pkg/llmutils`                                            | Output cleaning (`CleanJSON`), rendering, message inspection, size counting.                                                       |
| `pkg/metricskey`                                          | Metric descriptors emitted by the assistant loop.                                                                                  |
| `mocks/...`                                               | Generated mocks (`make generate`).                                                                                                 |

## Anatomy of one assistant run

`Assistant.Run` is the single place where everything comes together. The steps
below are what actually happens, in order — see
[`assistants/assistant.go`](../assistants/assistant.go).

```
Run(ctx, input, &output)
│
├─ 1. cfg := a.cfg.Apply(input.Options...)          per-call config copy
├─ 2. chatCtx := chatmodel.GetChatContext(ctx)      required; else ErrInvalidChatContext
├─ 3. if cfg.Model == nil:
│        llmFactory.GetModel(ctx, {OrgID, AssistantName})
│        derive ResponseFormat from the output type + provider capabilities
├─ 4. callback.OnAssistantStart
│
├─ 5. ── attempt loop (at most 2 attempts) ─────────────────────────────┐
│    │                                                                  │
│    │  build message history:                                          │
│    │      system prompt (template + skills catalog + output schema)   │
│    │      + store.Messages(ctx)                                       │
│    │      + few-shot examples                                         │
│    │      + user input                                                │
│    │      + input.Messages                                            │
│    │                                                                  │
│    │  ── LLM loop ────────────────────────────────────────────────┐   │
│    │     guard MaxMessages / MaxLength                            │   │
│    │     Model.GenerateContent(...)                               │   │
│    │     if no choices → retry (up to DefaultMaxRetries)          │   │
│    │     executeToolCalls(...)  ← tools run in parallel           │   │
│    │     if no tool was called → break                            │   │
│    │     guard MaxToolCalls                                       │   │
│    │  ────────────────────────────────────────────────────────────┘   │
│    │                                                                  │
│    │  OutputParser.Parse(choices[0].Content)  (only if output != nil) │
│    │  on ErrFailedUnmarshalOutput → retry once, tools disabled ───────┘
│    │
├─ 6. append run messages to the store (unless SkipMessageHistory)
└─ 7. callback.OnAssistantEnd → *Response{Choices, Messages, Model, Usage}
```

Key consequences of this design:

- **`ChatContext` is mandatory.** Every run needs a user/chat identity; the
  store and the metrics are keyed by it. See [Memory](memory.md).
- **The system prompt is assembled, not fixed.** Skills catalog and output
  schema instructions are appended to your template. See
  [Assistants → System prompt assembly](assistants.md#system-prompt-assembly).
- **Tool calls in one LLM turn run concurrently** and their responses are
  re-ordered to match the order the model requested them. Tools must be
  goroutine-safe.
- **A tool failure is not a run failure.** The error is returned to the model
  as a JSON `{"error": ...}` tool response so it can recover. See
  [Tools → Error contract](tools.md#error-contract).
- **Structured-output failures get one free retry** with tools disabled and a
  nudge to answer in JSON.

## Where state lives

| State                       | Lives in                                         | Lifetime                                      |
| --------------------------- | ------------------------------------------------ | --------------------------------------------- |
| User / org / chat / run IDs | `chatmodel.ChatContext` on the `context.Context` | one request (chat ID persists across runs)    |
| Conversation history        | `store.MessageStore`                             | as long as the store                          |
| Per-run message list        | `Response.Messages`                              | returned to the caller, appended to the store |
| Assistant configuration     | `assistants.Config` inside the `Assistant`       | assistant lifetime                            |
| Per-call overrides          | `CallInput.Options` → a copied `Config`          | one call                                      |
| Cached provider clients     | `llmfactory` (`byName`, `byType` maps)           | factory lifetime                              |
| Loaded skills               | `skills.Loader` inside the factory               | factory lifetime                              |

## Concurrency notes

- `Assistant` is safe to share across goroutines for calls: `Run` copies the
  config per call and does not mutate the assistant, except for the lazily
  built skills prompt.
- The `WithXxx` builder methods (`WithTools`, `WithSkills`, `WithName`, ...)
  mutate the assistant and are intended for construction time only.
- `llmfactory` guards its client caches with a mutex.
- `store.NewMemoryStore` is internally locked per tenant.

# gogentic Roadmap

Where the library stands today, and what is still missing. See
[Documentation/](Documentation/README.md) for how the existing pieces work.

## ✅ Current capabilities

- Agent loop with parallel tool calling, per-run limits, empty-response and
  output-parse retries — [Assistants](Documentation/assistants.md)
- Typed, schema-validated outputs; provider-native response formats with a
  prompt-instruction fallback — [Structured Output](Documentation/structured-output.md)
- Tool system with JSON Schema parameters generated from Go types, and a
  recoverable tool-error contract — [Tools](Documentation/tools.md)
- Multi-agent delegation: any assistant can be wrapped as a tool, with
  callback propagation and usage roll-up — [Orchestration](Documentation/orchestration.md)
- Multi-provider factory (OpenAI, Anthropic, Azure, Bedrock, Google AI,
  OpenRouter, Perplexity, Cloudflare) with per-assistant and per-organization
  model routing, capability filtering and a model allow/deny hook —
  [LLM Factory](Documentation/llm-factory.md)
- Provider-native prompt caching and asynchronous Batch APIs —
  [LLM Providers](Documentation/llms.md)
- Agent Skills (`SKILL.md`) with progressive disclosure and folder/tar
  discovery — [Skills](Documentation/skills.md)
- MCP server and client over stdio, HTTP, SSE and in-process transports —
  [MCP](Documentation/mcp.md)
- Message history in memory or Redis, multi-tenant by org + user + chat —
  [Memory](Documentation/memory.md)
- Prompt templates (Go template, Jinja2, f-string), chat prompts, few-shot —
  [Prompts](Documentation/prompts.md)
- Callbacks, per-run transcripts and stats, and metric descriptors for tokens,
  bytes, tool outcomes and latencies — [Observability](Documentation/observability.md)
- Generated mocks for assistants, tools, models and the factory

## ⚠️ Key areas for enhancement

### Orchestration

Delegation works today through `assistants.AssistantTool`, driven by the
supervisor's model. Still missing:

- [ ] A first-class router/supervisor helper, so intent-based dispatch does not
      have to be hand-written per application
- [ ] Response aggregation across several assistants invoked for one request
- [ ] Declarative, multi-step workflows (planner / task graph) rather than
      model-driven chaining only

### Conversation & session management

- [ ] A `SessionManager` abstraction for session lifecycle and authentication
- [ ] Session timeouts, reconnection and multi-device support
- [ ] History compaction (summarize-and-replace) as a reusable primitive rather
      than application code

### Retrieval

- [ ] Vector store / retriever interfaces and a RAG-oriented prompter
      (`Embedder` exists on providers that support embeddings, but there is no
      retrieval layer above it)

### Observability

- [ ] OpenTelemetry tracing spans across assistant, LLM and tool boundaries
      (metric descriptors already exist in `pkg/metricskey`)

### Reliability & safety

- [ ] Retry/backoff policy for transient provider errors, above the current
      empty-response and parse retries
- [ ] Rate limiting and input-validation middleware
- [ ] Enforcement of a skill's `allowed-tools` (currently metadata only)

## 🛠️ How to contribute

- Read [AGENTS.md](AGENTS.md) for coding, error-handling and testing conventions
- Read [Documentation/codemap.md](Documentation/codemap.md) to find the right
  file, and its invariants list before changing the agent loop
- Run `make generate && make test && make lint` before opening a PR

Prioritize based on your use case — open an issue if you want help designing
any of the above.

# gogentic Documentation

Reference documentation for the `github.com/effective-security/gogentic` module.

Each document is self-contained: reading the document for an area should be
enough to use that area correctly without reading the implementation.

## Reading order

If you are new to the repo, read in this order:

| # | Document | What it covers |
|---|----------|----------------|
| 1 | [Architecture](architecture.md) | Package map, layering, the anatomy of one assistant run |
| 2 | [LLM Factory](llm-factory.md) | Provider configuration, model resolution, capabilities, per-org overrides |
| 3 | [Assistants](assistants.md) | Building agents, typed output, options, the tool-calling loop, limits and retries |
| 4 | [Tools](tools.md) | Implementing tools, JSON schema for parameters, error contract |
| 5 | [Orchestration](orchestration.md) | Multi-agent flows: assistants as tools, delegation, usage aggregation |
| 6 | [Structured Output](structured-output.md) | `encoding` + `pkg/schema`: format instructions, response formats, parsers |
| 7 | [Prompts](prompts.md) | Prompt templates, chat prompts, few-shot, template engines |
| 8 | [Memory and Chat Context](memory.md) | `ChatContext`, message stores (memory/Redis), multi-tenancy |
| 9 | [Skills](skills.md) | Agent Skills: discovery, progressive disclosure, `activate_skill` |
| 10 | [MCP](mcp.md) | Model Context Protocol server/client and transports |
| 11 | [LLM Providers](llms.md) | `llms.Model`, messages, call options, prompt caching, batch API |
| 12 | [Observability](observability.md) | Callbacks, scratchpad run reports, metrics |

## Quick reference

| I want to... | Go to |
|--------------|-------|
| Run a single agent with typed JSON output | [Assistants → Quick start](assistants.md#quick-start) |
| Give an agent a custom tool | [Tools → Implementing a tool](tools.md#implementing-a-tool) |
| Have one agent call another | [Orchestration → Assistant as a tool](orchestration.md#assistant-as-a-tool) |
| Configure OpenAI / Anthropic / Bedrock / Azure | [LLM Factory → Configuration](llm-factory.md#configuration) |
| Pick a model per assistant, or per organization | [LLM Factory → Model resolution](llm-factory.md#model-resolution) |
| Persist chat history | [Memory → Message stores](memory.md#message-stores) |
| Expose my agent over MCP | [MCP → Exposing assistants and tools](mcp.md#exposing-assistants-and-tools) |
| Load markdown skills from disk | [Skills](skills.md) |
| See token usage and per-run stats | [Observability → Scratchpad](observability.md#scratchpad) |
| Understand why my output fails to parse | [Structured Output → Failure modes](structured-output.md#failure-modes) |

## For AI agents

[`codemap.md`](codemap.md) is a machine-oriented index: package purposes,
the file that owns each concept, and the entry-point symbols. Read it before
grepping the tree.

## Conventions used in the samples

Samples elide error handling with `//` comments where it is not the point.
Every sample assumes these imports resolve from the module root:

```go
import (
    "github.com/effective-security/gogentic/assistants"
    "github.com/effective-security/gogentic/callbacks"
    "github.com/effective-security/gogentic/chatmodel"
    "github.com/effective-security/gogentic/encoding"
    "github.com/effective-security/gogentic/mcp"
    "github.com/effective-security/gogentic/pkg/llmfactory"
    "github.com/effective-security/gogentic/pkg/llms"
    "github.com/effective-security/gogentic/pkg/prompts"
    "github.com/effective-security/gogentic/skills"
    "github.com/effective-security/gogentic/store"
    "github.com/effective-security/gogentic/tools"
)
```

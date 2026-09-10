# gogentic

Agentic LLM applications in Go.

gogentic is a library for building LLM agents: assistants that call tools, hand
work to other assistants, produce typed results, and run against any of a dozen
providers behind one interface. It is used in production and its API is stable.

It began as a fork of [langchaingo](https://github.com/tmc/langchaingo), and has
since diverged around stronger tool/assistant abstractions, schema generation,
Agent Skills, MCP, and multi-tenant model routing.

```sh
go get github.com/effective-security/gogentic
```

## What it gives you

| Capability | Package | Details |
|------------|---------|---------|
| Agent loop with parallel tool calling, limits and retries | `assistants` | [Assistants](Documentation/assistants.md) |
| Typed, schema-validated results | `encoding`, `pkg/schema` | [Structured Output](Documentation/structured-output.md) |
| Tools with generated JSON Schema parameters | `tools` | [Tools](Documentation/tools.md) |
| Multi-agent delegation (an assistant *is* a tool) | `assistants` | [Orchestration](Documentation/orchestration.md) |
| One config for OpenAI, Anthropic, Azure, Bedrock, Google AI, OpenRouter, Perplexity, Cloudflare | `pkg/llmfactory`, `pkg/llms` | [LLM Factory](Documentation/llm-factory.md), [LLM Providers](Documentation/llms.md) |
| Per-assistant and per-organization model routing, capability filtering, quota hooks | `pkg/llmfactory` | [Model resolution](Documentation/llm-factory.md#model-resolution) |
| Agent Skills (`SKILL.md`) with progressive disclosure | `skills` | [Skills](Documentation/skills.md) |
| MCP server and client, with stdio / HTTP / SSE / in-process transports | `mcp` | [MCP](Documentation/mcp.md) |
| Chat history in memory or Redis, multi-tenant | `store`, `chatmodel` | [Memory](Documentation/memory.md) |
| Prompt templates: Go template, Jinja2, f-string, few-shot | `pkg/prompts` | [Prompts](Documentation/prompts.md) |
| Callbacks, per-run transcripts and stats, Prometheus-ready metrics | `callbacks`, `pkg/metricskey` | [Observability](Documentation/observability.md) |
| Provider-native prompt caching and Batch APIs | `pkg/llms` | [Prompt caching](Documentation/llms.md#prompt-caching), [Batch](Documentation/llms.md#batch-api) |

## Quick start

**1. Describe your providers** in `llm.yaml`:

```yaml
default_provider: OPENAI
providers:
  - name: OPENAI
    token: env://OPENAI_API_KEY
    default_model: gpt-5.1
    available_models: [gpt-5.1, gpt-5.1-mini]
    open_ai:
      api_type: OPENAI
assistant_models:
  default: [OPENAI/gpt-5.1]
  researcher: [OPENAI/gpt-5.1]
```

**2. Define the shape of the answer** and run an assistant:

```go
package main

import (
    "context"
    "fmt"

    "github.com/effective-security/gogentic/assistants"
    "github.com/effective-security/gogentic/chatmodel"
    "github.com/effective-security/gogentic/pkg/llmfactory"
    "github.com/effective-security/gogentic/pkg/llmutils"
    "github.com/effective-security/gogentic/pkg/prompts"
    "github.com/effective-security/gogentic/store"
)

// The output type: jsonschema tags are the model's instructions.
type Answer struct {
    Text    string   `json:"text"    jsonschema:"required,title=Text,description=The answer."`
    Sources []string `json:"sources" jsonschema:"title=Sources,description=URLs backing the answer."`
}

func (a Answer) GetContent() string { return llmutils.ToJSON(a) }

func main() {
    fac, err := llmfactory.Load("llm.yaml")
    if err != nil {
        panic(err)
    }

    agent := assistants.NewAssistant[Answer](fac,
        prompts.NewPromptTemplate("You are a concise research assistant.", nil),
        assistants.WithMessageStore(store.NewMemoryStore()),
    ).
        WithName("researcher"). // binds to assistant_models.researcher
        WithDescription("Answers factual questions and cites sources.")

    // Every run needs a ChatContext: user/org/chat/run identity.
    chatCtx := chatmodel.NewChatContext("user-123", "", nil)
    ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

    var out Answer
    resp, err := agent.Run(ctx, &assistants.CallInput{
        Input: "What is the capital of the largest country in Europe?",
    }, &out)
    if err != nil {
        panic(err)
    }

    fmt.Println(out.Text)
    fmt.Println("tokens:", resp.Usage.ModelUsage)
}
```

## Add a tool

A tool is a name, a description, a JSON Schema, and a `string → string` call.
Keep the real logic in a typed `Run` and let `Call` adapt the model's JSON:

```go
type Request struct {
    City string `json:"city" jsonschema:"required,title=City,description=City name."`
}

type Result struct {
    TempC float64 `json:"temp_c"`
}

func (r *Result) GetContent() string { return llmutils.ToJSON(r) }

type Tool struct{ params *jsonschema.Schema }

func New() (*Tool, error) {
    sc, err := schema.New(reflect.TypeOf(Request{}))
    if err != nil {
        return nil, errors.Wrap(err, "failed to create schema")
    }
    return &Tool{params: sc.Parameters}, nil
}

func (t *Tool) Name() string                   { return "get_weather" }
func (t *Tool) Description() string            { return "Current temperature for a city." }
func (t *Tool) Parameters() *jsonschema.Schema { return t.params }

func (t *Tool) Run(ctx context.Context, req *Request) (*Result, error) {
    return &Result{TempC: 21.5}, nil
}

func (t *Tool) Call(ctx context.Context, input string) (string, error) {
    var req Request
    if err := json.Unmarshal(llmutils.CleanJSON([]byte(input)), &req); err != nil {
        return "", errors.WithStack(chatmodel.ErrFailedUnmarshalInput)
    }
    out, err := t.Run(ctx, &req)
    if err != nil {
        return "", err
    }
    return out.GetContent(), nil
}
```

```go
agent = agent.WithTools(weatherTool)
```

Tools requested in the same LLM turn run concurrently, and a tool error is
handed back to the model rather than aborting the run.
See [Tools](Documentation/tools.md).

## Orchestrate several agents

Wrap an assistant as a tool and give it to a supervisor. Context, callbacks and
call options flow down; token usage rolls up.

```go
researchTool, err := assistants.NewAssistantTool[chatmodel.InputRequest](researcher)
if err != nil {
    return err
}

supervisor := assistants.NewAssistant[chatmodel.OutputResult](fac, supervisorPrompt,
    assistants.WithCallback(callbacks.NewScratchpad(callbacks.ModeVerbose)),
    assistants.WithMaxToolCalls(8),
).
    WithName("supervisor").
    WithTools(researchTool, coderTool)
```

See [Orchestration](Documentation/orchestration.md).

## Expose it over MCP

```go
tr := stdio.NewStdioServerTransport()
srv := mcp.NewServer(tr, mcp.WithName("my-agents"), mcp.WithVersion("1.0.0"))

if err := srv.Serve(); err != nil {
    return err
}
if err := researcher.RegisterMCP(srv); err != nil { // as an MCP prompt
    return err
}
if err := weatherTool.RegisterMCP(srv); err != nil { // as an MCP tool
    return err
}
if err := tr.Start(ctx); err != nil {
    return err
}
```

See [MCP](Documentation/mcp.md).

## Load skills from disk

Drop `SKILL.md` files in a directory; only their names and descriptions enter
the prompt, and the model loads full instructions on demand via
`activate_skill`.

```yaml
skills:
  enable_default_skills: true
  paths: [./skills]
```

```go
agent = agent.WithSkills(fac.Skills("researcher"))
```

See [Skills](Documentation/skills.md).

## Documentation

Full documentation is in [`Documentation/`](Documentation/README.md):

- [Architecture](Documentation/architecture.md) — package map and the anatomy of one run
- [Assistants](Documentation/assistants.md) · [Tools](Documentation/tools.md) · [Orchestration](Documentation/orchestration.md)
- [LLM Factory](Documentation/llm-factory.md) · [LLM Providers](Documentation/llms.md)
- [Structured Output](Documentation/structured-output.md) · [Prompts](Documentation/prompts.md)
- [Memory and Chat Context](Documentation/memory.md) · [Skills](Documentation/skills.md) · [MCP](Documentation/mcp.md)
- [Observability](Documentation/observability.md)
- [Code Map](Documentation/codemap.md) — navigation index, also for AI agents

Package-level godoc is available for every package:
`go doc github.com/effective-security/gogentic/assistants`.

## Contributing

```sh
make generate   # regenerate mocks for changed interfaces
make test       # run the suite
make lint       # final check
```

Read [AGENTS.md](AGENTS.md) for error handling, testing and documentation
conventions, and [Documentation/codemap.md](Documentation/codemap.md#invariants-to-preserve-when-editing)
for the invariants the agent loop depends on.

See [ROADMAP.md](ROADMAP.md) for planned work.

## License

[Apache 2.0](LICENSE)

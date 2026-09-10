# Tools

A tool is a function the model can call. gogentic's contract is deliberately
small: name, description, JSON Schema for the arguments, and a
`string → string` call.

Source: [`tools/tools.go`](../tools/tools.go).
Reference implementation: [`tools/tavily/tavily.go`](../tools/tavily/tavily.go).

## The contract

```go
type ITool interface {
    // Name is what the model calls. Keep it stable: history references it.
    Name() string
    // Description is injected into the prompt. Must fit the model's limits.
    Description() string
    // Parameters is the JSON Schema of the arguments object.
    Parameters() *jsonschema.Schema
    // Call receives the raw arguments JSON and returns the model-visible result.
    Call(ctx context.Context, input string) (string, error)
}
```

Three layered variants build on it:

| Interface | Adds | Use when |
|-----------|------|----------|
| `tools.Tool[I, O]` | `Run(ctx, *I) (*O, error)` | You want typed Go access alongside the LLM-facing `Call` |
| `tools.IMCPTool` | `RegisterMCP(registrator)` | The tool should also be reachable over MCP |
| `tools.MCPTool[I]` | `RunMCP(ctx, *I) (*mcp.ToolResponse, error)` | MCP callers need rich content (images, resources) |

## Implementing a tool

The shape below is what `tools/tavily` uses and is the recommended pattern:
derive the parameter schema from the request struct with `pkg/schema`, keep
`Run` typed, and make `Call` a thin JSON adapter.

```go
package weather

const ToolName = "get_weather"

// Request is the arguments object. jsonschema tags become the model's
// instructions, so write them for the model, not for Go developers.
type Request struct {
    City string `json:"city" jsonschema:"required,title=City,description=City name, e.g. Paris."`
    Unit string `json:"unit" jsonschema:"title=Unit,description=Temperature unit,enum=C,enum=F,default=C"`
}

// Result is the tool output. GetContent() is what the model sees.
type Result struct {
    City  string  `json:"city"`
    TempC float64 `json:"temp_c"`
}

func (r *Result) GetContent() string { return llmutils.ToJSON(r) }

type Tool struct {
    funcParams *jsonschema.Schema
    client     *http.Client
}

var _ tools.Tool[Request, Result] = (*Tool)(nil)

func New(client *http.Client) (*Tool, error) {
    sc, err := schema.New(reflect.TypeOf(Request{}))
    if err != nil {
        return nil, errors.Wrap(err, "failed to create schema")
    }
    return &Tool{funcParams: sc.Parameters, client: client}, nil
}

func (t *Tool) Name() string        { return ToolName }
func (t *Tool) Description() string {
    return "Returns the current temperature for a city."
}
func (t *Tool) Parameters() *jsonschema.Schema { return t.funcParams }

// Run holds the real logic and is directly testable.
func (t *Tool) Run(ctx context.Context, req *Request) (*Result, error) {
    if req.City == "" {
        return nil, errors.New("invalid request: empty city")
    }
    // ... call the upstream API, wrap its errors ...
    return &Result{City: req.City, TempC: 21.5}, nil
}

// Call adapts the LLM's raw JSON to Run.
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

Attach it:

```go
tool, err := weather.New(http.DefaultClient)
if err != nil {
    return err
}
agent = agent.WithTools(tool)
```

### Why `llmutils.CleanJSON`

Models wrap JSON in prose or code fences. `llmutils.CleanJSON` strips anything
before the first `{`/`[` and after the matching close, and removes backtick
fences. Always run raw tool arguments through it before `json.Unmarshal`.

## Error contract

This is the part most easily got wrong. Three distinct outcomes:

| Return | What the assistant does |
|--------|-------------------------|
| `(result, nil)` | Result becomes the `RoleTool` message content |
| `("", chatmodel.ErrFailedUnmarshalInput)` | The model is told to fix the JSON against the schema and may retry — **not** counted as a failure |
| `("", someOtherError)` | The error is serialized as `{"error": "..."}` into the `RoleTool` message; the loop continues so the model can recover |

So a failing tool never aborts a run by itself. If a failure genuinely must
stop the flow, surface that in your application after `Run` returns, or return
a result that instructs the model to stop.

Every requested `tool_call_id` always receives exactly one `RoleTool` message —
providers reject histories where one is missing, and the loop guarantees this
even for panics-turned-errors and missing results.

Follow the repo's error conventions ([AGENTS.md](../AGENTS.md)): wrap upstream
errors with `errors.WithMessage` / `errors.Wrapf`, and keep LLM-facing error
text sanitized but specific.

## Concurrency

All tool calls the model requests in one turn are executed in parallel
goroutines. Tools must be safe for concurrent use: no mutable shared state
without locking, no per-call fields on the tool struct.

## Naming rules

- Tools are keyed **lower-cased** internally. `GetWeather` and `get_weather`
  collide; the first registration wins.
- The name `web_search` is reserved: registering a tool with that name attaches
  the provider-native web search tool instead of your function, and it is
  dropped for providers without `CapabilityWebSearchTool`.
- Keep names snake_case and stable — they appear in stored message history.

## Provider-native tools

Some capabilities are implemented by the provider, not by you. Pass them
through as raw `llms.Tool` values:

```go
agent := assistants.NewAssistant[Answer](fac, sysPrompt,
    assistants.WithTools(
        llms.Tool{Type: "web_search"},
        llms.Tool{Type: "google_search"},
    ),
)
```

`llms.Tool.WebSearchOptions` narrows web search by domain where the provider
supports it. See [LLM Providers → Capabilities](llms.md#capabilities).

## Exposing a tool over MCP

Implement `RegisterMCP` and hand the typed handler to the registrator; the MCP
server derives the tool's input schema from the handler signature.

```go
func (t *Tool) RegisterMCP(reg tools.McpServerRegistrator) error {
    return reg.RegisterTool(t.Name(), t.Description(), t.RunMCP)
}

func (t *Tool) RunMCP(ctx context.Context, req *Request) (*mcp.ToolResponse, error) {
    res, err := t.Run(ctx, req)
    if err != nil {
        return nil, err
    }
    return mcp.NewToolResponse(mcp.NewTextContent(res.GetContent())), nil
}
```

See [MCP](mcp.md).

## Describing tools in prompts

```go
catalog := tools.GetDescriptions(toolA, toolB).Render(llmutils.RenderFormatMarkdown)
```

Useful for router prompts that must choose an agent based on what tools it has.

## Testing tools

Test `Run` directly with typed input, and test `Call` for the JSON contract —
particularly that malformed input yields `chatmodel.ErrFailedUnmarshalInput`:

```go
_, err := tool.Call(ctx, `{"city":`)
require.Error(t, err)
assert.True(t, errors.Is(err, chatmodel.ErrFailedUnmarshalInput))
```

For assistant-level tests, use the generated mocks in
[`mocks/mocktools`](../mocks/mocktools) and
[`mocks/mockllms`](../mocks/mockllms); regenerate with `make generate`.

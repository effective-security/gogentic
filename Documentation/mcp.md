# MCP (Model Context Protocol)

The `mcp` package implements both sides of MCP: a **server** that exposes your
tools, prompts and resources, and a **client** that consumes someone else's.
Transports are pluggable.

Sources: [`mcp/`](../mcp) — `server.go`, `client.go`, `content_api.go`,
`tool_api.go`, `prompt_api.go`, `resource_api.go`;
[`mcp/transport/`](../mcp/transport).

## Server

```go
tr := stdio.NewStdioServerTransport()

srv := mcp.NewServer(tr,
    mcp.WithName("gogentic-tools"),
    mcp.WithVersion("1.0.0"),
    mcp.WithInstructions("Tools for incident triage."),
)

// Register handlers *after* NewServer and around Serve.
if err := srv.RegisterTool("get_weather", "Current temperature for a city.",
    func(ctx context.Context, args WeatherArgs) (*mcp.ToolResponse, error) {
        return mcp.NewToolResponse(mcp.NewTextContent("21C")), nil
    }); err != nil {
    return err
}

// Serve wires the JSON-RPC handlers and connects the transport.
if err := srv.Serve(); err != nil {
    return err
}

// Blocking transports (stdio, HTTP) are driven by Start.
if err := tr.Start(ctx); err != nil {
    return err
}
```

`Serve()` is idempotent-hostile: calling it twice returns
`server is already running`. It registers the handlers for `ping`,
`initialize`, `tools/list`, `tools/call`, `prompts/list`, `prompts/get`,
`resources/list`, `resources/templates/list` and `resources/read`.

Server options: `WithName`, `WithVersion`, `WithInstructions`,
`WithPaginationLimit(n)`, `WithProtocol(p)`.

### Handler signatures

The server reflects your handler to derive the JSON Schema, so the signature is
part of the contract.

**Tools** — one or two arguments; with two, the first must be
`context.Context`. Returns `(*mcp.ToolResponse, error)`.

```go
func(ctx context.Context, args T) (*mcp.ToolResponse, error)
func(args T) (*mcp.ToolResponse, error)
```

`T` may be any struct; `jsonschema` tags describe it to the caller.

**Prompts** — same argument rules, returns `(*mcp.PromptResponse, error)`.
`T` must be a struct whose fields are all `string` or `*string` (MCP prompt
arguments are strings by protocol).

```go
func(ctx context.Context, args chatmodel.MCPInputRequest) (*mcp.PromptResponse, error)
```

**Resources** — no arguments beyond an optional context, returns
`(*mcp.ResourceResponse, error)`.

A mismatched signature is rejected by `RegisterTool` / `RegisterPrompt` /
`RegisterResource` with a descriptive error, not at call time.

### Registration API

```go
srv.RegisterTool(name, description, handler)
srv.RegisterPrompt(name, description, handler)
srv.RegisterResource(uri, name, description, mimeType, handler)
srv.RegisterResourceTemplate(uriTemplate, name, description, mimeType)

srv.CheckToolRegistered(name)      // and CheckPromptRegistered / CheckResourceRegistered
srv.DeregisterTool(name)           // and DeregisterPrompt / DeregisterResource
```

Registration and deregistration emit the corresponding
`notifications/*/list_changed` notification, so connected clients refresh —
this is why registering after `Serve()` is fine.

### Building responses

```go
mcp.NewToolResponse(
    mcp.NewTextContent("plain text"),
    mcp.NewImageContent(base64Data, "image/png"),
    mcp.NewTextResourceContent("file:///report.md", md, "text/markdown"),
    mcp.NewBlobResourceContent("file:///report.pdf", base64Pdf, "application/pdf"),
).
    WithStructuredContent(myStruct).       // structured payload alongside the content
    WithMeta(map[string]any{"took": "1s"})

mcp.NewPromptResponse("Answers concisely.",
    mcp.NewPromptMessage(mcp.NewTextContent("Hi!"), mcp.RoleAssistant))

mcp.NewResourceResponse(
    mcp.NewTextEmbeddedResource("file:///notes.txt", "hello", "text/plain"))
```

`Content.WithAnnotations` attaches audience/priority hints.

An error returned by a tool handler becomes a **tool error result**, not a
JSON-RPC protocol error, so the calling model can see and react to it.

## Exposing assistants and tools

Both `assistants` and `tools` define a narrow registrator interface, so neither
package depends on the MCP server type:

```go
// tools.McpServerRegistrator
RegisterTool(name string, description string, handler any) error

// assistants.McpServerRegistrator
RegisterPrompt(name string, description string, handler any) error
```

`*mcp.Server` satisfies both.

**An assistant as an MCP prompt.** `*Assistant[O]` implements `IMCPAssistant`:

```go
if err := myAssistant.RegisterMCP(srv); err != nil {
    return err
}
```

This registers a prompt named `Assistant.Name()` taking
`chatmodel.MCPInputRequest`:

```go
type MCPInputRequest struct {
    ChatID string `json:"chatID"`
    Input  string `json:"input"`
}
```

`CallMCP` applies the supplied `chatID` to the `ChatContext` on the context
(via `chatmodel.SetChatID`), runs the assistant **without** output parsing, and
returns each choice as a `RoleAssistant` prompt message with the assistant's
description as the response description.

The context still needs a `ChatContext` — put one on it in your transport
layer or HTTP middleware, otherwise the run fails with
`ErrInvalidChatContext`.

**A tool over MCP.** Implement `tools.IMCPTool` (see
[Tools](tools.md#exposing-a-tool-over-mcp)):

```go
if err := myTool.RegisterMCP(srv); err != nil {
    return err
}
```

`assistants.AssistantTool` also implements it, so a sub-agent can be published
as an MCP *tool* rather than a prompt.

## Client

```go
tr := httptransport.NewHTTPClientTransport("/mcp").
    WithBaseURL("https://example.com").
    WithHeader("Authorization", "Bearer "+token)

cli := mcp.NewClientWithInfo(tr, mcp.ClientInfo{
    Name:    "my-app",
    Version: "1.0.0",
})

if _, err := cli.Initialize(ctx); err != nil {
    return err
}

toolsResp, err := cli.ListTools(ctx, nil) // nil cursor => first page
res, err := cli.CallTool(ctx, "get_weather", map[string]any{"city": "Paris"})

promptsResp, err := cli.ListPrompts(ctx, nil)
prompt, err := cli.GetPrompt(ctx, "researcher", map[string]any{"input": "hello"})

resourcesResp, err := cli.ListResources(ctx, nil)
resource, err := cli.ReadResource(ctx, "file:///notes.txt")

err = cli.Ping(ctx)
caps := cli.GetCapabilities() // populated by Initialize
```

`Initialize` must be called before anything else — the other methods rely on
the negotiated capabilities. Paginated calls take a `*string` cursor; pass the
`NextCursor` from the previous response to continue.

## Transports

All transports implement `transport.Transport`:

```go
type Transport interface {
    Start(ctx context.Context) error
    Send(ctx context.Context, message *BaseJsonRpcMessage) error
    Close() error
    SetCloseHandler(func())
    SetErrorHandler(func(error))
    SetMessageHandler(func(ctx context.Context, message *BaseJsonRpcMessage))
}
```

| Package | Constructor | Use for |
|---------|-------------|---------|
| `mcp/transport/stdio` | `stdio.NewStdioServerTransport()` / `NewStdioServerTransportWithIO(in, out)` | A server launched as a subprocess by an MCP host. `WithIO` is what tests use |
| `mcp/transport/httptransport` | `httptransport.NewHTTPTransport("/mcp").WithAddr(":8080")` | Stateless HTTP server transport; `Start` calls `ListenAndServe`. Also implements `http.Handler`, so it can be mounted on your own mux |
| `mcp/transport/httptransport` | `httptransport.NewHTTPClientTransport("/mcp")` | HTTP client transport; `WithBaseURL`, `WithHeader`, `WithClient` |
| `mcp/transport/sse` | `sse.NewSSEServerTransport(endpoint, w)` | Server-push over Server-Sent Events on an existing `http.ResponseWriter`; `HandlePostMessage(r)` feeds client messages back in |
| `mcp/transport/localtransport` | `localtransport.New()` | In-process server transport. `HandleMessage(ctx, body)` injects a raw JSON-RPC message and returns the reply — ideal for tests and embedding |
| `mcp/transport/localtransport` | `localtransport.NewLocalClientTransport(handler)` | In-process client transport that calls a `Handler` (`HandleMCP(ctx, *McpProxyRequest)`), for proxying MCP over your own RPC |

### In-process end to end

Useful in tests and when an MCP server should be reachable without a socket:

```go
tr := localtransport.New()
srv := mcp.NewServer(tr, mcp.WithName("embedded"))
if err := srv.Serve(); err != nil {
    return err
}
if err := myTool.RegisterMCP(srv); err != nil {
    return err
}

// Drive it directly with a JSON-RPC frame.
reply, err := tr.HandleMessage(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
```

To get a full client/server pair in one process, wrap the transport in a
`localtransport.Handler` and connect `NewLocalClientTransport` to it.

### Mounting the HTTP transport on your own server

```go
tr := httptransport.NewHTTPTransport("/mcp")
srv := mcp.NewServer(tr)
if err := srv.Serve(); err != nil {
    return err
}

mux := http.NewServeMux()
mux.Handle("/mcp", tr)               // instead of tr.Start(ctx)
http.ListenAndServe(":8080", mux)    // your own server, middleware, TLS
```

This is the recommended shape for production: `tr.Start(ctx)` spins up its own
`http.Server` with no middleware, no TLS and a fixed address.

## Where to put ChatContext

MCP handlers receive whatever context the transport hands them. Assistant runs
require a `ChatContext`, so inject one at the edge:

```go
mux.Handle("/mcp", withChatContext(tr))

func withChatContext(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        userID := userFromAuth(r)
        chatCtx := chatmodel.NewChatContext(userID, "", nil)
        chatCtx.SetOrgID(orgFromAuth(r))
        next.ServeHTTP(w, r.WithContext(chatmodel.WithChatContext(r.Context(), chatCtx)))
    })
}
```

An MCP client that supplies `chatID` in `MCPInputRequest` then continues an
existing conversation, because `CallMCP` overrides the generated chat ID with
it.

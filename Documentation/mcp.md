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

`WithPaginationLimit(n)` pages `tools/list`, `prompts/list`, `resources/list`
and `resources/templates/list`, which are ordered by tool name, prompt name,
resource URI and URI template respectively. Every full page carries a
`nextCursor`; following cursors until none is returned yields every item
exactly once. When the item count is a multiple of `n`, the last page is
empty. A non-positive `n` disables pagination.

### Handler signatures

The server reflects your handler to derive the JSON Schema, so the signature is
part of the contract.

**Tools** — an optional `context.Context` first, then at most one argument
value `T`. Returns `(*mcp.ToolResponse, error)`.

```go
func(ctx context.Context, args T) (*mcp.ToolResponse, error)
func(args T) (*mcp.ToolResponse, error)
func(ctx context.Context) (*mcp.ToolResponse, error)
func() (*mcp.ToolResponse, error)
```

`T` may be any struct; `jsonschema` tags describe it to the caller.

**Prompts** — same argument rules, returns `(*mcp.PromptResponse, error)`.
`T` must be a struct whose fields are all `string` or `*string` (MCP prompt
arguments are strings by protocol). A prompt without `T` takes no arguments.

```go
func(ctx context.Context, args chatmodel.MCPInputRequest) (*mcp.PromptResponse, error)
```

**Resources** — no arguments beyond an optional context, returns
`(*mcp.ResourceResponse, error)`.

A mismatched signature is rejected by `RegisterTool` / `RegisterPrompt` /
`RegisterResource` with a descriptive error, not at call time: argument count
and types, both result types (`*mcp.ToolResponse`, `*mcp.PromptResponse` or
`*mcp.ResourceResponse`, then `error`), a variadic function, a nil function
(including a typed nil, such as an unset handler field), and a handler that is
not a function at all, for example
`handler must return *mcp.ResourceResponse, got string`. A `T` that contains
itself, such as `struct{ Children []T }`, is rejected too, with an error
wrapping `schema.ErrRecursiveType`, as the schema reflector inlines every type.

A request without `arguments`, or with `"arguments": null`, calls the handler
with the zero value of `T`. A tool without `T` is listed with the input schema
`{"type":"object","properties":{}}`, a prompt without `T` with no arguments,
and both ignore the arguments they are sent.

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

Content goes on the wire in the MCP schema shape, with `annotations` as a
nested object:

```json
{"type":"text","text":"plain text"}
{"type":"image","data":"<base64>","mimeType":"image/png"}
{"type":"resource","resource":{"uri":"file:///report.md","mimeType":"text/markdown","text":"..."}}
{"type":"resource","resource":{"uri":"file:///report.pdf","mimeType":"application/pdf","blob":"<base64>"}}
```

The client decodes the same three types. Content with a missing required
field (`text`; `data` or `mimeType`; `resource`, `resource.uri`, or both
`resource.text` and `resource.blob`) or of another type fails the call with an
error such as `invalid image content: missing required field "mimeType"`.

### Errors

An error returned by a tool handler becomes a **tool error result**, not a
JSON-RPC protocol error, so the calling model can see and react to it: the
error text is the content and `isError` is set. `Client.CallTool` returns it
with `ToolResponse.IsError` true and a nil error. A handler can report a tool
error with its own content by setting `IsError` on the response it returns.
Likewise a prompt handler error comes back as a prompt response described as
`error`, and a resource handler error as a text resource holding the message,
at the URI that was read.

The same applies when the handler cannot produce a response: arguments that
do not decode (`failed to unmarshal arguments: ...`), a handler that panics
(`internal error: tool panicked`; the panic value, which can hold internal
data, and the stack go only to the server log, with the tool name), and
a handler that returns neither a response nor an error
(`tool handler returned neither a response nor an error`). A faulty handler
fails its own request; it does not crash the server.

A request the server cannot route is a **JSON-RPC error**: an unknown tool,
prompt or resource is reported as `unknown tool: <name>`,
`unknown prompt: <name>` or `unknown resource: <uri>`, and so are malformed
request params and list cursors. The client returns these as errors, for example
`failed to call tool: RPC error -32000: unknown tool: <name>`.

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
as an MCP _tool_ rather than a prompt.

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
// nil arguments leave "arguments" out of the request
res, err = cli.CallTool(ctx, "list_cities", nil)

promptsResp, err := cli.ListPrompts(ctx, nil)
prompt, err := cli.GetPrompt(ctx, "researcher", map[string]any{"input": "hello"})

resourcesResp, err := cli.ListResources(ctx, nil)
resource, err := cli.ReadResource(ctx, "file:///notes.txt")

err = cli.Ping(ctx)
caps := cli.GetCapabilities() // populated by Initialize
```

`Initialize` must be called before anything else — the other methods rely on
the negotiated capabilities. Paginated calls take a `*string` cursor; pass the
`NextCursor` from the previous response to continue, and stop when it is nil
(see `WithPaginationLimit` above).

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

| Package                        | Constructor                                                                  | Use for                                                                                                                                      |
| ------------------------------ | ---------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `mcp/transport/stdio`          | `stdio.NewStdioServerTransport()` / `NewStdioServerTransportWithIO(in, out)` | A server launched as a subprocess by an MCP host. `WithIO` is what tests use                                                                 |
| `mcp/transport/httptransport`  | `httptransport.NewHTTPTransport("/mcp").WithAddr(":8080")`                   | Stateless HTTP server transport; `Start` calls `ListenAndServe`. Also implements `http.Handler`, so it can be mounted on your own mux        |
| `mcp/transport/httptransport`  | `httptransport.NewHTTPClientTransport("/mcp")`                               | HTTP client transport; `WithBaseURL`, `WithHeader`, `WithClient`                                                                             |
| `mcp/transport/sse`            | `sse.NewSSEServerTransport(endpoint, w)`                                     | Server-push over Server-Sent Events on an existing `http.ResponseWriter`; `HandlePostMessage(r)` feeds client messages back in               |
| `mcp/transport/localtransport` | `localtransport.New()`                                                       | In-process server transport. `HandleMessage(ctx, body)` injects a raw JSON-RPC message and returns the reply — ideal for tests and embedding |
| `mcp/transport/localtransport` | `localtransport.NewLocalClientTransport(handler)`                            | In-process client transport that calls a `Handler` (`HandleMCP(ctx, *McpProxyRequest)`), for proxying MCP over your own RPC                  |

The client transports classify a reply body with `transport.ParseMessage`:
`method` with `id` is a request, `method` alone a notification, `error` an
error response and `result` a response. Anything else fails `Send` with
`received invalid response: ...` instead of reaching the client as an error.

The stdio and SSE server transports run the close handler once per started
connection, after releasing their lock, so the handler may call back into the
transport; closing a transport that was never started does nothing.

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

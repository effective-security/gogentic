// Package mcp implements the Model Context Protocol client and server, with
// pluggable transports (stdio, HTTP, SSE, in-process). Use it to expose
// assistants as MCP prompts and tools, or to consume MCP servers.
//
// # Server
//
// Create a server over a transport, register handlers, then call
// [Server.Serve] to wire the JSON-RPC methods and connect the transport.
// Registration may happen before or after Serve — each registration emits the
// corresponding notifications/*/list_changed notification.
//
//	tr := stdio.NewStdioServerTransport()
//	srv := mcp.NewServer(tr,
//	    mcp.WithName("gogentic-tools"),
//	    mcp.WithVersion("1.0.0"),
//	)
//
//	// A tool handler takes an optional context plus a struct, and returns
//	// (*mcp.ToolResponse, error). The input JSON schema is derived from the
//	// struct by reflection.
//	type WeatherArgs struct {
//	    City string `json:"city" jsonschema:"required,description=City name"`
//	}
//	err := srv.RegisterTool("get_weather", "Current temperature for a city.",
//	    func(ctx context.Context, args WeatherArgs) (*mcp.ToolResponse, error) {
//	        return mcp.NewToolResponse(mcp.NewTextContent("21C")), nil
//	    })
//	if err != nil {
//	    return err
//	}
//
//	if err := srv.Serve(); err != nil {
//	    return err
//	}
//	// Blocking transports are driven by Start.
//	if err := tr.Start(ctx); err != nil {
//	    return err
//	}
//
// An assistant registers itself as an MCP prompt via
// assistants.IMCPAssistant.RegisterMCP, and a tool as an MCP tool via
// tools.IMCPTool.RegisterMCP; *[Server] satisfies both registrator interfaces.
//
// Prompt handlers must accept a struct whose fields are all string or *string,
// because MCP prompt arguments are strings by protocol. An error returned by a
// tool handler becomes a tool error result rather than a JSON-RPC protocol
// error, so the calling model can see and react to it.
//
// # Client
//
// [Client.Initialize] must be called before any other method; the negotiated
// capabilities are then available from [Client.GetCapabilities].
//
//	cli := mcp.NewClientWithInfo(
//	    httptransport.NewHTTPClientTransport("/mcp").WithBaseURL("https://example.com"),
//	    mcp.ClientInfo{Name: "my-app", Version: "1.0.0"})
//
//	if _, err := cli.Initialize(ctx); err != nil {
//	    return err
//	}
//	list, err := cli.ListPrompts(ctx, nil) // nil cursor => first page
//	res, err := cli.CallTool(ctx, "get_weather", map[string]any{"city": "Paris"})
//
// See Documentation/mcp.md for transports, response construction and how to
// supply a chatmodel.ChatContext to assistant-backed handlers.
package mcp

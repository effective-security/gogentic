// Package mcp implements the Model Context Protocol client/server and common
// transports (stdio, HTTP/SSE, local). Use it to expose assistants as MCP
// prompts and tools, or to consume MCP servers from clients.
//
// # Quick examples
//
// Server: register an assistant prompt
//
//	// srv := mcp.NewServer(...transport...)
//	// err := srv.RegisterPrompt("helpful-assistant", "Answers concisely.", func(ctx context.Context, in chatmodel.MCPInputRequest) (*mcp.PromptResponse, error) {
//	//   // produce messages via assistants package
//	//   return mcp.NewPromptResponse("ok", mcp.NewPromptMessage(mcp.NewTextContent("Hi!"), mcp.RoleAssistant)), nil
//	// })
//	// srv.Start(ctx)
//
// Client: connect and list prompts
//
//	// cli := mcp.NewClient(transport.NewStdioTransport(os.Stdin, os.Stdout))
//	// _, _ = cli.Initialize(ctx)
//	// prompts, _ := cli.ListPrompts(ctx, nil)
//	// _ = prompts
package mcp

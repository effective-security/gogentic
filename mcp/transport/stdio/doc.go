// Package stdio implements the MCP stdio transport, used by servers that are
// launched as a subprocess by an MCP host and exchange newline-delimited
// JSON-RPC frames over standard input and output.
//
//	tr := stdio.NewStdioServerTransport()
//	srv := mcp.NewServer(tr)
//	if err := srv.Serve(); err != nil {
//	    return err
//	}
//	// Start blocks, reading frames until the context is cancelled or stdin closes.
//	if err := tr.Start(ctx); err != nil {
//	    return err
//	}
//
// Use [NewStdioServerTransportWithIO] to read and write arbitrary streams,
// which is how the transport is driven in tests.
package stdio

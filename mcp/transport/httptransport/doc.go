// Package httptransport implements stateless HTTP transports for MCP: a server
// transport that accepts JSON-RPC requests on an endpoint, and a client
// transport that posts them.
//
// # Server
//
// [HTTPTransport] implements both transport.Transport and http.Handler.
// Prefer mounting it on your own server so it inherits your middleware, TLS
// and address:
//
//	tr := httptransport.NewHTTPTransport("/mcp")
//	srv := mcp.NewServer(tr)
//	if err := srv.Serve(); err != nil {
//	    return err
//	}
//
//	mux := http.NewServeMux()
//	mux.Handle("/mcp", tr)
//
// [HTTPTransport.Start] is the standalone alternative: it creates its own
// http.Server on the address set by [HTTPTransport.WithAddr] (":8080" by
// default) and blocks in ListenAndServe.
//
// # Client
//
//	tr := httptransport.NewHTTPClientTransport("/mcp").
//	    WithBaseURL("https://example.com").
//	    WithHeader("Authorization", "Bearer "+token)
//
//	cli := mcp.NewClient(tr)
//	if _, err := cli.Initialize(ctx); err != nil {
//	    return err
//	}
//
// Supply a custom HTTP client with [HTTPClientTransport.WithClient] to add
// timeouts, retries or instrumentation.
package httptransport

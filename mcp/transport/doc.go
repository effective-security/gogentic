// Package transport defines the minimal interfaces and shared types for MCP
// transports used by the client and server. Concrete implementations live in
// subpackages (stdio, httptransport, sse, localtransport).
//
// Typical usage is indirect through mcp.Client and mcp.Server, which accept
// a transport.Transport.
package transport

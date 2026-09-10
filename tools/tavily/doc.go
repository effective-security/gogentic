// Package tavily implements a web search tool backed by the Tavily API
// (https://docs.tavily.com), and serves as the reference implementation of the
// tools.ITool contract.
//
// The tool is registered with an assistant like any other:
//
//	websearch, err := tavily.New() // reads TAVILY_API_KEY
//	if err != nil {
//	    return err
//	}
//	agent = agent.WithTools(websearch)
//
// Use [NewWithAPIKey] to supply the key directly. Search behavior is tuned with
// [Tool.WithSearchOpts]; the defaults are a basic search of at most 5 general
// results with an LLM-generated answer included.
//
// It also implements tools.MCPTool, so the same instance can be published over
// MCP with [Tool.RegisterMCP].
//
// Note that the tool's default name is [ToolName] ("tavily_web_search"), not
// "web_search" — the latter is reserved by the assistants package for
// provider-native web search grounding.
package tavily

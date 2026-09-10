// Package tools defines the contract every tool implements so an LLM agent can
// call it: a name, a description injected into the prompt, a JSON Schema for
// the arguments, and a string-to-string call.
//
// # Implementing a tool
//
// The recommended shape keeps the real logic in a typed Run and makes Call a
// thin JSON adapter, which also satisfies [Tool]:
//
//	type Request struct {
//	    City string `json:"city" jsonschema:"required,title=City,description=City name."`
//	}
//
//	func New() (*Tool, error) {
//	    sc, err := schema.New(reflect.TypeOf(Request{}))
//	    if err != nil {
//	        return nil, errors.Wrap(err, "failed to create schema")
//	    }
//	    return &Tool{funcParams: sc.Parameters}, nil
//	}
//
//	func (t *Tool) Name() string                   { return "get_weather" }
//	func (t *Tool) Description() string            { return "Current temperature for a city." }
//	func (t *Tool) Parameters() *jsonschema.Schema { return t.funcParams }
//
//	func (t *Tool) Run(ctx context.Context, req *Request) (*Result, error) { ... }
//
//	func (t *Tool) Call(ctx context.Context, input string) (string, error) {
//	    var req Request
//	    if err := json.Unmarshal(llmutils.CleanJSON([]byte(input)), &req); err != nil {
//	        return "", errors.WithStack(chatmodel.ErrFailedUnmarshalInput)
//	    }
//	    out, err := t.Run(ctx, &req)
//	    if err != nil {
//	        return "", err
//	    }
//	    return out.GetContent(), nil
//	}
//
// Always run raw arguments through llmutils.CleanJSON: models wrap JSON in
// prose or code fences.
//
// # Error contract
//
// Returning chatmodel.ErrFailedUnmarshalInput tells the assistant to ask the
// model to fix its arguments against the schema; it is not counted as a
// failure. Any other error is serialized into the tool response as
// {"error": "..."} so the model can recover, and the run continues. A failing
// tool therefore never aborts a run by itself.
//
// # Concurrency
//
// All tools requested in one LLM turn are executed in parallel goroutines, so
// an implementation must be safe for concurrent use.
//
// # Naming
//
// Tools are registered under their lower-cased name, so names that differ only
// in case collide. The name "web_search" is reserved: registering a tool with
// that name attaches the provider-native web search tool instead of the
// function. Keep names stable, because they appear in stored message history.
//
// # MCP
//
// Implement [IMCPTool] (and usually [MCPTool]) to publish the same tool over
// the Model Context Protocol; *mcp.Server satisfies [McpServerRegistrator].
//
// See tools/tavily for a complete reference implementation, and
// Documentation/tools.md for details.
package tools

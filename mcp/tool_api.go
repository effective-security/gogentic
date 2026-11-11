package mcp

// This is a union type of all the different ToolResponse that can be sent back to the client.
// We allow creation through constructors only to make sure that the ToolResponse is valid.
type ToolResponse struct {
	// This result property is reserved by the protocol to allow clients and servers
	// to attach additional metadata to their responses.
	Meta map[string]any `json:"_meta,omitempty" yaml:"_meta,omitempty" mapstructure:"_meta,omitempty"`

	Content []*Content `json:"content" yaml:"content" mapstructure:"content"`
	// NEW in MCP v2025-06-18
	StructuredContent any `json:"structuredContent,omitempty" yaml:"structuredContent,omitempty" mapstructure:"structuredContent,omitempty"`

	// IsError reports that the tool call ended in a tool error, so that a
	// caller can tell an error result from a successful one; the content then
	// describes the error. Client.CallTool sets it from the server's isError
	// flag. The server sends the flag when the handler returns an error, and
	// a handler may set it on its own result to report an error with custom
	// content.
	IsError bool `json:"isError,omitempty" yaml:"isError,omitempty" mapstructure:"isError,omitempty"`
}

func NewToolResponse(content ...*Content) *ToolResponse {
	return &ToolResponse{
		Content: content,
	}
}

func (t *ToolResponse) WithStructuredContent(resp any) *ToolResponse {
	t.StructuredContent = resp
	return t
}

func (t *ToolResponse) WithMeta(meta map[string]any) *ToolResponse {
	t.Meta = meta
	return t
}

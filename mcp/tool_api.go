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

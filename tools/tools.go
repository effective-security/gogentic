package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/effective-security/gogentic/mcp"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/x/format"
	"github.com/invopop/jsonschema"
)

//go:generate mockgen -source=tools.go -destination=../mocks/mocktools/assistants_mock.gen.go  -package mocktools

type McpServerRegistrator interface {
	RegisterTool(name string, description string, handler any) error
}

// ITool is a tool for the llm agent to interact with different applications.
type ITool interface {
	// Name returns the name of the Tool.
	Name() string
	// Description returns the description of the tool, to be used in the prompt.
	// Should not exceed LLM model limit.
	Description() string
	// Parameters returns the parameters definition of the function, to be used in the prompt.
	Parameters() *jsonschema.Schema

	// Call executes the tool with the given input and returns the result.
	// If the tool fails to parse the input, it should return ErrFailedUnmarshalInput error.
	Call(context.Context, string) (string, error)
}

// Callback receives tool execution events. Tool calls run in parallel
// goroutines, so implementations must be safe for concurrent use.
type Callback interface {
	OnToolStart(ctx context.Context, tool ITool, assistantName, input string)
	OnToolEnd(ctx context.Context, tool ITool, assistantName, input string, output string)
	OnToolError(ctx context.Context, tool ITool, assistantName, input string, err error)
}

// Tool is an ITool with a typed Run, so the same implementation is usable from
// Go code and from an LLM.
type Tool[I any, O any] interface {
	ITool
	Run(context.Context, *I) (*O, error)
}

// IMCPTool is an interface that extends ITool to include functionality for
// registering the tool with an MCP server.
// The RegisterMCP method allows the tool to be registered with a given
// MCP Server.
type IMCPTool interface {
	ITool
	RegisterMCP(registrator McpServerRegistrator) error
}

// MCPTool is an IMCPTool whose MCP handler returns rich content such as images
// or embedded resources.
type MCPTool[I any] interface {
	IMCPTool
	RunMCP(context.Context, *I) (*mcp.ToolResponse, error)
}

// Description is a compact, prompt-friendly summary of a tool.
type Description struct {
	Name        string `json:"Name" yaml:"Name"`
	Description string `json:"Description" yaml:"Description"`
}

// Descriptions is a renderable collection of tool descriptions.
type Descriptions []Description

// ToMarkdown renders the descriptions as a markdown list, with each
// description collapsed to a single line.
func (d Descriptions) ToMarkdown() string {
	var ts strings.Builder
	for _, tool := range d {
		_, _ = fmt.Fprintf(&ts, "- Name: %s\n", tool.Name)
		ts.WriteString("  Description: ")
		ts.WriteString(format.TextOneLine(tool.Description))
		ts.WriteString("\n")
	}
	return ts.String()
}

// Render renders the descriptions in the requested format; markdown uses
// ToMarkdown, other formats delegate to llmutils.RenderToString.
func (d Descriptions) Render(format llmutils.RenderFormat) string {
	if format == llmutils.RenderFormatMarkdown {
		return d.ToMarkdown()
	}
	return llmutils.RenderToString(format, d)
}

// GetDescriptions returns the name and description of each tool, for embedding
// a tool catalog in a prompt.
func GetDescriptions(list ...ITool) Descriptions {
	var d Descriptions
	for _, tool := range list {
		d = append(d, Description{
			Name:        tool.Name(),
			Description: format.TextOneLine(tool.Description()),
		})
	}
	return d
}

package tools

import (
	"context"
	"fmt"
	"strings"

	mcp "github.com/metoro-io/mcp-golang"
	"github.com/tmc/langchaingo/llms"
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
	Parameters() any

	// Call executes the tool with the given input and returns the result.
	Call(context.Context, string) (string, error)
}

type Callback interface {
	OnToolStart(context.Context, ITool, string)
	OnToolEnd(context.Context, ITool, string, string)
	OnToolError(context.Context, ITool, string, error)
	OnToolLLMCall(context.Context, ITool, []llms.MessageContent)
}

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

type MCPTool[I any] interface {
	IMCPTool
	RunMCP(context.Context, *I) (*mcp.ToolResponse, error)
}

func GetDescriptions(list ...ITool) string {
	var ts strings.Builder
	for _, item := range list {
		ts.WriteString(fmt.Sprintf("- `%s`: %s\n", item.Name(), item.Description()))
	}
	return ts.String()
}

package tools

import (
	"context"
)

// ITool is a tool for the llm agent to interact with different applications.
type ITool interface {
	// Name returns the name of the Tool.
	Name() string
	// Description returns the description of the tool, to be used in the prompt.
	// Should not exceed LLM model limit.
	Description() string
	// Parameters returns the parameters definition of the function, to be used in the prompt.
	Parameters() any

	Call(context.Context, string) (string, error)
}

type Callback interface {
	OnToolStart(context.Context, ITool, string)
	OnToolEnd(context.Context, ITool, string, string)
	OnToolError(context.Context, ITool, string, error)
}

type Tool[I any, O any] interface {
	ITool
	Run(context.Context, *I) (*O, error)
}

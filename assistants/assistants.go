package assistants

import (
	"context"
	"fmt"
	"strings"

	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/tools"
	"github.com/effective-security/xlog"
	mcp "github.com/metoro-io/mcp-golang"
	"github.com/tmc/langchaingo/llms"
)

var logger = xlog.NewPackageLogger("github.com/effective-security/gogentic", "assistants")

//go:generate mockgen -destination=../mocks/mockllms/llm_mock.gen.go -package mockllms github.com/tmc/langchaingo/llms  Model
//go:generate mockgen -source=assistants.go -destination=../mocks/mockassitants/assistants_mock.gen.go  -package mockassitants

type McpServerRegistrator interface {
	RegisterPrompt(name string, description string, handler any) error
}

type IAssistant interface {
	// Name returns the name of the Assistant.
	Name() string
	// Description returns the description of the Assistant, to be used in the prompt of other Assistants or LLMs.
	// Should not exceed LLM model limit.
	Description() string
	// FormatPrompter returns the format prompter for the Assistant.
	FormatPrompt(values map[string]any) (llms.PromptValue, error)
	GetPromptInputVariables() []string

	// // LastRunMessages returns all messages from the run, including the system prompt and tools
	// LastRunMessages() []llms.MessageContent

	Call(ctx context.Context, input string, promptInputs map[string]any, options ...Option) (*llms.ContentResponse, error)
}

// IAssistantTool provides an interface for tools that use underlying the Assistants.
type IAssistantTool interface {
	tools.ITool
	// CallAssistant allows the tool to call the assistant with the given input and options.
	CallAssistant(ctx context.Context, input string, options ...Option) (string, error)
}

type ProvidePromptInputsFunc func(input string) (map[string]any, error)

type HasCallback interface {
	GetCallback() Callback
}

type TypeableAssistant[O chatmodel.ContentProvider] interface {
	IAssistant
	HasCallback
	// Run executes the assistant with the given input and prompt inputs.
	// Do not use this method directly, use the Run function instead.
	Run(ctx context.Context, input string, promptInputs map[string]any, optionalOutputType *O, options ...Option) (*llms.ContentResponse, error)
}

type Callback interface {
	tools.Callback
	OnAssistantStart(ctx context.Context, agent IAssistant, input string)
	OnAssistantEnd(ctx context.Context, agent IAssistant, input string, resp *llms.ContentResponse)
	OnAssistantError(ctx context.Context, agent IAssistant, input string, err error)
	OnAssistantLLMCall(ctx context.Context, agent IAssistant, payload []llms.MessageContent)
}

// IMCPAssistant is an interface that extends IAssistant to include functionality for
// registering the assistant with an MCP server.
// The RegisterMCP method allows the assistant to be registered with a given
// MCP Server.
type IMCPAssistant interface {
	IAssistant
	RegisterMCP(registrator McpServerRegistrator) error
	CallMCP(context.Context, chatmodel.MCPInputRequest) (*mcp.PromptResponse, error)
}

func GetDescriptions(list ...IAssistant) string {
	var ts strings.Builder
	for _, item := range list {
		ts.WriteString(fmt.Sprintf("- `%s`: %s\n", item.Name(), item.Description()))
	}
	return ts.String()
}

func MapAssistants(list ...IAssistant) map[string]IAssistant {
	if len(list) == 0 {
		return nil
	}
	m := make(map[string]IAssistant, len(list))
	for _, a := range list {
		m[a.Name()] = a
	}
	return m
}

// Run executes the assistant with the given input and prompt inputs.
func Run[O chatmodel.ContentProvider](
	ctx context.Context,
	assistant TypeableAssistant[O],
	input string,
	promptInputs map[string]any,
	optionalOutputType *O,
	options ...Option,
) (*llms.ContentResponse, error) {
	var callback Callback
	if cb, ok := assistant.(HasCallback); ok {
		callback = cb.GetCallback()
	}

	if callback != nil {
		callback.OnAssistantStart(ctx, assistant, input)
	}

	apiResp, err := assistant.Run(ctx, input, promptInputs, optionalOutputType, options...)
	if err != nil {
		if callback != nil {
			callback.OnAssistantError(ctx, assistant, input, err)
		}
		return nil, err
	}

	if callback != nil {
		callback.OnAssistantEnd(ctx, assistant, input, apiResp)
	}
	return apiResp, nil
}

// Call executes a generic assistant without typed output.
func Call(
	ctx context.Context,
	assistant IAssistant,
	input string,
	promptInputs map[string]any,
	options ...Option,
) (*llms.ContentResponse, error) {
	var callback Callback
	if cb, ok := assistant.(HasCallback); ok {
		callback = cb.GetCallback()
	}

	if callback != nil {
		callback.OnAssistantStart(ctx, assistant, input)
	}

	apiResp, err := assistant.Call(ctx, input, promptInputs, options...)
	if err != nil {
		if callback != nil {
			callback.OnAssistantError(ctx, assistant, input, err)
		}
		return nil, err
	}

	if callback != nil {
		callback.OnAssistantEnd(ctx, assistant, input, apiResp)
	}
	return apiResp, nil
}

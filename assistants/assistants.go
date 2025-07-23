package assistants

import (
	"context"

	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/mcp"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/tools"
	"github.com/effective-security/x/format"
	"github.com/effective-security/xlog"
)

var logger = xlog.NewPackageLogger("github.com/effective-security/gogentic", "assistants")

//go:generate mockgen -destination=../mocks/mockllms/llm_mock.gen.go -package mockllms github.com/effective-security/gogentic/pkg/llms  Model
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
	// GetTools returns the tools that the Assistant can use.
	GetTools() []tools.ITool

	// FormatPrompter returns the format prompter for the Assistant.
	FormatPrompt(values map[string]any) (llms.PromptValue, error)
	// GetPromptInputVariables returns the input variables for the prompt.
	GetPromptInputVariables() []string

	// Call executes the assistant with the given input and prompt inputs.
	// If the assistant fails to parse the input, it should return ErrFailedUnmarshalInput error.
	Call(ctx context.Context, input *CallInput) (*llms.ContentResponse, error)

	// LastRunMessages returns the messages added to the message history from the last run.
	LastRunMessages() []llms.Message
}

type CallInput struct {
	// Input is the input to the assistant.
	Input string
	// PromptInputs is prompt inputs to be rendered in the system prompt.
	PromptInputs map[string]any
	// Options is additional options to be passed to the assistant on run.
	Options []Option
	// Messages is additional content to be sent to the LLM.
	Messages []llms.Message
}

// IAssistantTool provides an interface for tools that use underlying the Assistants.
type IAssistantTool interface {
	tools.ITool
	// CallAssistant allows the tool to call the assistant with the given input and options.
	// If the assistant fails to parse the input, it should return ErrFailedUnmarshalInput error.
	CallAssistant(ctx context.Context, input string, options ...Option) (string, error)
}

type ProvidePromptInputsFunc func(ctx context.Context, input string) (map[string]any, error)

type TypeableAssistant[O chatmodel.ContentProvider] interface {
	IAssistant
	// Run executes the assistant with the given input and prompt inputs.
	// Do not use this method directly, use the Run function instead.
	// If the assistant fails to parse the input, it should return ErrFailedUnmarshalInput error.
	Run(ctx context.Context, input *CallInput, optionalOutputType *O) (*llms.ContentResponse, error)
}

type Callback interface {
	tools.Callback
	OnAssistantStart(ctx context.Context, a IAssistant, input string)
	OnAssistantEnd(ctx context.Context, a IAssistant, input string, resp *llms.ContentResponse, messages []llms.Message)
	OnAssistantError(ctx context.Context, a IAssistant, input string, err error, messages []llms.Message)
	OnAssistantLLMCallStart(ctx context.Context, a IAssistant, llm llms.Model, payload []llms.Message)
	OnAssistantLLMCallEnd(ctx context.Context, a IAssistant, llm llms.Model, resp *llms.ContentResponse)
	OnAssistantLLMParseError(ctx context.Context, a IAssistant, input string, response string, err error)
	OnToolNotFound(ctx context.Context, a IAssistant, tool string)
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
	var d assistantsDescription
	for _, item := range list {
		ad := assistantDescription{
			Name:        item.Name(),
			Description: format.TextOneLine(item.Description()),
		}
		d.Assistants = append(d.Assistants, ad)
	}

	return llmutils.BackticksJSON(llmutils.ToJSONIndent(d))
}

type toolDescription struct {
	Name        string `json:"Name" yaml:"Name"`
	Description string `json:"Description" yaml:"Description"`
}

type assistantDescription struct {
	Name        string            `json:"Name" yaml:"Name"`
	Description string            `json:"Description" yaml:"Description"`
	Tools       []toolDescription `json:"Tools,omitempty" yaml:"Tools,omitempty"`
}

type assistantsDescription struct {
	Assistants []assistantDescription `json:"Assistants" yaml:"Assistants"`
}

func GetDescriptionsWithTools(list ...IAssistant) string {
	var d assistantsDescription
	for _, item := range list {
		ad := assistantDescription{
			Name:        item.Name(),
			Description: format.TextOneLine(item.Description()),
		}
		for _, t := range item.GetTools() {
			ad.Tools = append(ad.Tools, toolDescription{
				Name:        t.Name(),
				Description: format.TextOneLine(t.Description()),
			})
		}
		d.Assistants = append(d.Assistants, ad)
	}

	return llmutils.BackticksJSON(llmutils.ToJSONIndent(d))
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

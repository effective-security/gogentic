package assistants

import (
	"context"
	"fmt"
	"strings"

	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/mcp"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/skills"
	"github.com/effective-security/gogentic/tools"
	"github.com/effective-security/x/format"
	"github.com/effective-security/xlog"
)

var logger = xlog.NewPackageLogger("github.com/effective-security/gogentic", "assistants")

//go:generate mockgen -destination=../mocks/mockllms/llm_mock.gen.go -package mockllms github.com/effective-security/gogentic/pkg/llms  Model,Batcher
//go:generate mockgen -source=assistants.go -destination=../mocks/mockassitants/assistants_mock.gen.go  -package mockassitants

// McpServerRegistrator registers assistant prompts with an MCP server.
type McpServerRegistrator interface {
	RegisterPrompt(name string, description string, handler any) error
}

// IAssistant is the interface every assistant implements. It is what tools,
// callbacks and orchestrating assistants depend on, so a hand-written flow can
// be substituted for *Assistant anywhere.
type IAssistant interface {
	// Name returns the name of the Assistant.
	Name() string
	// Description returns the description of the Assistant, to be used in the prompt of other Assistants or LLMs.
	// Should not exceed LLM model limit.
	Description() string
	// GetTools returns the tools that the Assistant can use.
	GetTools() []tools.ITool
	// GetSkills returns the skills that the Assistant can activate.
	GetSkills() skills.Skills

	// FormatPrompt renders the Assistant's system prompt with the given values.
	FormatPrompt(values map[string]any) (llms.PromptValue, error)
	// GetPromptInputVariables returns the input variables for the prompt.
	GetPromptInputVariables() []string

	// Call executes the assistant with the given input and prompt inputs.
	// If the assistant fails to parse the input, it should return ErrFailedUnmarshalInput error.
	Call(ctx context.Context, input *CallInput) (*Response, error)
}

// Response is the response returned by the assistant.
type Response struct {
	// Choices is the choices returned by the GenerateContent call.
	Choices []*llms.ContentChoice
	// Messages is the messages that are created from the run and added to the Message History Store.
	Messages []llms.Message
	// Model is the model that was used to generate the response.
	Model string
	// Usage is the usage stats for the response.
	Usage llms.UsageStats
}

// GetUsage returns the usage stats, or nil for a nil Response. Safe to call on
// the result of a failed call.
func (c *Response) GetUsage() *llms.UsageStats {
	if c == nil {
		return nil
	}
	return &c.Usage
}

// CallInput is the per-call input to an assistant: the user turn plus optional
// prompt values, config overrides, extra messages and progress reporting.
type CallInput struct {
	// Input is the input to the assistant.
	Input string
	// PromptInputs is prompt inputs to be rendered in the system prompt.
	PromptInputs map[string]any
	// Options is additional options to be passed to the assistant on run.
	Options []Option
	// Messages is additional content to be sent to the LLM.
	Messages []llms.Message
	// Args is additional arguments to be passed to the assistant on run.
	// This can be used by assistants that implement IAssistant and have a custom implementation of Run.
	Args map[string]string

	// OnProgress is the progress callback, that can be used to report generic progress,
	// in addition to the callback provided in the Options.
	OnProgress OnProgressFunc
}

// GetArg returns the argument with the given key.
func (c *CallInput) GetArg(key string) string {
	if c == nil || c.Args == nil || key == "" {
		return ""
	}
	return c.Args[key]
}

// OnProgressFunc is a callback for reporting generic progress updates.
type OnProgressFunc func(ctx context.Context, a IAssistant, title, message string)

// IAssistantTool provides an interface for tools that use underlying the Assistants.
type IAssistantTool interface {
	tools.ITool
	// CallAssistant allows the tool to call the assistant with the given input and options.
	// If the assistant fails to parse the input, it should return ErrFailedUnmarshalInput error.
	CallAssistant(ctx context.Context, input string, options ...Option) (string, *llms.UsageStats, error)
}

// ProvidePromptInputsFunc is a function that provides prompt inputs for the assistant.
type ProvidePromptInputsFunc func(ctx context.Context, input string) (map[string]any, error)

// ProvideSkillsPromptFunc is a function that provides a prompt for the skills.
type ProvideSkillsPromptFunc func(ctx context.Context, skillList skills.Skills) (string, error)

// TypeableAssistant is an IAssistant with a known output type. Run populates
// the supplied output value; passing nil skips output parsing entirely.
type TypeableAssistant[O chatmodel.ContentProvider] interface {
	IAssistant
	// Run executes the assistant with the given input and prompt inputs.
	// If the assistant fails to parse the input, it should return ErrFailedUnmarshalInput error.
	Run(ctx context.Context, input *CallInput, optionalOutputType *O) (*Response, error)
}

// Callback receives every assistant, LLM and tool event of a run. The handler
// is propagated into nested assistants invoked through AssistantTool, and tool
// events are delivered from parallel goroutines, so implementations must be
// safe for concurrent use. See the callbacks package for implementations.
type Callback interface {
	tools.Callback
	OnAssistantStart(ctx context.Context, a IAssistant, input string)
	OnAssistantEnd(ctx context.Context, a IAssistant, input string, resp *Response, messageHistory llms.Messages)
	OnAssistantError(ctx context.Context, a IAssistant, input string, err error, messageHistory llms.Messages)
	OnAssistantLLMCallStart(ctx context.Context, a IAssistant, llm llms.Model, payload llms.Messages)
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

// Description is a compact, prompt-friendly summary of an assistant, optionally
// including its tools. Use it to tell a supervising assistant what specialists
// are available.
type Description struct {
	Name        string              `json:"Name" yaml:"Name"`
	Description string              `json:"Description" yaml:"Description"`
	Tools       []tools.Description `json:"Tools,omitempty" yaml:"Tools,omitempty"`
}

// Descriptions is a renderable collection of assistant descriptions.
type Descriptions []Description

// ToMarkdown renders the descriptions as a markdown list, with each
// description collapsed to a single line.
func (d Descriptions) ToMarkdown() string {
	var ts strings.Builder
	for _, assis := range d {
		_, _ = fmt.Fprintf(&ts, "- Name: %s\n", assis.Name)
		ts.WriteString("  Description: ")
		ts.WriteString(format.TextOneLine(assis.Description))
		ts.WriteString("\n")
		if len(assis.Tools) > 0 {
			ts.WriteString("  Tools:\n")
			for _, tool := range assis.Tools {
				_, _ = fmt.Fprintf(&ts, "    - Name: %s\n", tool.Name)
				ts.WriteString("      Description: ")
				ts.WriteString(format.TextOneLine(tool.Description))
				ts.WriteString("\n")
			}
		}
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

// GetDescriptions returns the name and description of each assistant, without
// their tools.
func GetDescriptions(list ...IAssistant) Descriptions {
	var d Descriptions
	for _, item := range list {
		ad := Description{
			Name:        item.Name(),
			Description: format.TextOneLine(item.Description()),
		}
		d = append(d, ad)
	}

	return d
}

// GetDescriptionsWithTools returns the name and description of each assistant
// together with its tools, for prompts where the tool set informs the routing
// decision.
func GetDescriptionsWithTools(list ...IAssistant) Descriptions {
	var d Descriptions
	for _, item := range list {
		ad := Description{
			Name:        item.Name(),
			Description: format.TextOneLine(item.Description()),
		}
		for _, t := range item.GetTools() {
			ad.Tools = append(ad.Tools, tools.Description{
				Name:        t.Name(),
				Description: format.TextOneLine(t.Description()),
			})
		}
		d = append(d, ad)
	}

	return d
}

// MapAssistants indexes the assistants by Name for dispatching by name in
// code-driven routing. Returns nil for an empty list.
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

// String returns the response content, joining multiple choices with a blank
// line.
func (r *Response) String() string {
	if r == nil {
		return ""
	}
	if len(r.Choices) == 1 {
		return r.Choices[0].Content
	}
	var b strings.Builder
	for idx, choice := range r.Choices {
		if idx > 0 {
			b.WriteString("\n")
		}
		b.WriteString(strings.TrimSpace(choice.Content))
		b.WriteString("\n")
	}
	return b.String()
}

// NewResponse builds a Response with a single choice holding the stringified
// value. Useful for assistants that implement IAssistant themselves.
func NewResponse(val any) *Response {
	// Create a new ContentResponse with the given idRes1
	return &Response{
		Choices: []*llms.ContentChoice{
			{
				Content: llmutils.Stringify(val),
			},
		},
	}
}

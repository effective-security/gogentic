package assistants

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/mcp"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/effective-security/gogentic/tools"
	"github.com/invopop/jsonschema"
)

// TypeableAssistantTool is an assistant exposed as a tool, callable both
// locally by another assistant and remotely over MCP.
type TypeableAssistantTool[I any, O any] interface {
	IAssistantTool
	tools.IMCPTool
	CallAssistant(ctx context.Context, input string, options ...Option) (string, *llms.UsageStats, error)
}

// AssistantTool adapts a TypeableAssistant to the tools.ITool contract, so one
// assistant can delegate to another. I is the input type the calling model must
// produce and defines the tool's JSON schema; O is the wrapped assistant's
// output type.
type AssistantTool[I chatmodel.ContentProvider, O chatmodel.ContentProvider] struct {
	assistant   TypeableAssistant[O]
	name        string
	description string
	funcParams  *jsonschema.Schema
}

// NewAssistantTool wraps an assistant as a tool. The tool's name and
// description default to the assistant's, so naming the assistant well is what
// makes delegation work; override them by type-asserting the result to
// *AssistantTool and calling WithName / WithDescription.
//
// O is inferred from the assistant, so only I is normally specified:
//
//	tool, err := assistants.NewAssistantTool[chatmodel.InputRequest](researcher)
func NewAssistantTool[I chatmodel.ContentProvider, O chatmodel.ContentProvider](assistant TypeableAssistant[O]) (TypeableAssistantTool[I, O], error) {
	var def I
	sc, err := schema.New(reflect.TypeOf(def))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create schema")
	}
	t := &AssistantTool[I, O]{
		assistant:   assistant,
		name:        assistant.Name(),
		description: assistant.Description(),
		funcParams:  sc.Parameters,
	}
	return t, nil
}

// WithName sets the name of the tool, when used in a prompt of another Agents or LLMs.
func (a *AssistantTool[I, O]) WithName(name string) *AssistantTool[I, O] {
	a.name = name
	return a
}

// WithDescription sets the description of the tool, to be used in the prompt of other Agents or LLMs.
func (a *AssistantTool[I, O]) WithDescription(description string) *AssistantTool[I, O] {
	a.description = description
	return a
}

// Name returns the tool name, defaulting to the wrapped assistant's name.
func (t *AssistantTool[I, O]) Name() string {
	return t.name
}

// Description returns the tool description, defaulting to the wrapped
// assistant's description.
func (t *AssistantTool[I, O]) Description() string {
	return t.description
}

// Parameters returns the JSON schema derived from the input type I.
func (t *AssistantTool[I, O]) Parameters() *jsonschema.Schema {
	return t.funcParams
}

// Call implements tools.ITool by delegating to CallAssistant and discarding the
// usage stats.
func (t *AssistantTool[I, O]) Call(ctx context.Context, input string) (string, error) {
	res, _, err := t.CallAssistant(ctx, input)
	return res, err
}

// CallAssistant parses the raw arguments into I, runs the wrapped assistant and
// returns the stringified result together with the usage stats, which the
// calling assistant adds to its own.
//
// Malformed arguments yield chatmodel.ErrFailedUnmarshalInput, which the
// calling assistant turns into a request to the model to fix its JSON. If the
// run fails and O implements chatmodel.IBaseResult, the error is recorded as a
// clarification on the result instead of being returned, so the calling model
// can recover.
func (t *AssistantTool[I, O]) CallAssistant(ctx context.Context, input string, options ...Option) (string, *llms.UsageStats, error) {
	var tin I
	if parser, ok := (any)(&tin).(chatmodel.InputParser); ok {
		if err := parser.ParseInput(input); err != nil {
			return "", nil, errors.WithStack(chatmodel.ErrFailedUnmarshalInput)
		}
	} else {
		// Validate the input against the function parameters
		if err := json.Unmarshal(llmutils.CleanJSON([]byte(input)), &tin); err != nil {
			return "", nil, errors.WithStack(chatmodel.ErrFailedUnmarshalInput)
		}
	}

	var res O
	resp, err := t.assistant.Run(ctx, &CallInput{
		Input:   tin.GetContent(),
		Options: options,
	}, &res)
	if err != nil {
		if val, ok := (any)(&res).(chatmodel.IBaseResult); ok {
			val.SetClarification(llmutils.AddComment("tool", t.Name(), "error", err.Error()))
		} else {
			return "", nil, err
		}
	}

	// resp is nil when Run returns an error, even if we recover the error
	// into the result above as a clarification.
	var usage *llms.UsageStats
	if resp != nil {
		u := resp.Usage
		usage = &u
	}

	return chatmodel.Stringify(res), usage, nil
}

// RegisterMCP registers the wrapped assistant as an MCP tool.
func (t *AssistantTool[I, O]) RegisterMCP(registrator tools.McpServerRegistrator) error {
	return registrator.RegisterTool(t.name, t.description, t.RunMCP)
}

// RunMCP handles an MCP tool call by running the wrapped assistant and
// returning its content as text.
func (t *AssistantTool[I, O]) RunMCP(ctx context.Context, req *I) (*mcp.ToolResponse, error) {
	input := chatmodel.Stringify(req)

	var res O
	_, err := t.assistant.Run(ctx, &CallInput{
		Input: input,
	}, &res)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResponse(mcp.NewTextContent(res.GetContent())), nil
}

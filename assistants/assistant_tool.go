package assistants

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/effective-security/gogentic/tools"
	mcp "github.com/metoro-io/mcp-golang"
)

type TypeableAssistantTool[I any, O any] interface {
	IAssistantTool
	tools.IMCPTool
	CallAssistant(ctx context.Context, input string, options ...Option) (string, error)
}

type AssistantTool[I chatmodel.ContentProvider, O chatmodel.ContentProvider] struct {
	assistant   TypeableAssistant[O]
	name        string
	description string
	funcParams  any
}

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
func (t *AssistantTool[I, O]) Name() string {
	return t.name
}

func (t *AssistantTool[I, O]) Description() string {
	return t.description
}

func (t *AssistantTool[I, O]) Parameters() any {
	return t.funcParams
}

func (t *AssistantTool[I, O]) Call(ctx context.Context, input string) (string, error) {
	return t.CallAssistant(ctx, input)
}

func (t *AssistantTool[I, O]) CallAssistant(ctx context.Context, input string, options ...Option) (string, error) {
	var tin I
	if parser, ok := (any)(&tin).(chatmodel.InputParser); ok {
		if err := parser.ParseInput(input); err != nil {
			return "", errors.WithStack(chatmodel.ErrFailedUnmarshalInput)
		}
	} else {
		// Validate the input against the function parameters
		if err := json.Unmarshal(llmutils.CleanJSON([]byte(input)), &tin); err != nil {
			return "", errors.WithStack(chatmodel.ErrFailedUnmarshalInput)
		}
	}

	var res O
	_, err := t.assistant.Run(ctx, &CallInput{
		Input:   tin.GetContent(),
		Options: options,
	}, &res)
	if err != nil {
		if val, ok := (any)(&res).(chatmodel.IBaseResult); ok {
			val.SetClarification(llmutils.AddComment("tool", t.Name(), "error", err.Error()))
		} else {
			return "", err
		}
	}

	return chatmodel.Stringify(res), nil
}

func (t *AssistantTool[I, O]) RegisterMCP(registrator tools.McpServerRegistrator) error {
	return registrator.RegisterTool(t.name, t.description, t.RunMCP)
}

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

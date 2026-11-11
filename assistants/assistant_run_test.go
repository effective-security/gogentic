package assistants_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/encoding"
	"github.com/effective-security/gogentic/mocks/mockassitants"
	"github.com/effective-security/gogentic/mocks/mockllmfactory"
	"github.com/effective-security/gogentic/mocks/mockllms"
	"github.com/effective-security/gogentic/mocks/mocktools"
	"github.com/effective-security/gogentic/pkg/llmfactory"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/pkg/prompts"
	"github.com/effective-security/gogentic/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// Tests of Assistant.Run: model selection, history, limits, output parsing and tool calls.

func Test_Assistant_Run_EdgeCases(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})

	mockFactory := mockllmfactory.NewMockFactory(ctrl)
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).Times(1)
	mockLLM.EXPECT().GetName().Return("gpt-4o").AnyTimes()
	assistant := assistants.NewAssistant[chatmodel.OutputResult](mockFactory, systemPrompt, assistants.WithModel(mockLLM))

	// LLM returns no choices
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(&llms.ContentResponse{Choices: []*llms.ContentChoice{}}, nil).AnyTimes()
	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)
	_, err := assistant.Run(ctx, &assistants.CallInput{Input: "input"}, nil)
	assert.Error(t, err)

	// OutputParser returns error
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(&llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "bad json"}}}, nil).AnyTimes()
	outputParser, _ := encoding.NewTypedOutputParser[chatmodel.OutputResult](chatmodel.OutputResult{}, encoding.ModeJSONSchema)
	assistant = assistant.WithOutputParser(outputParser)
	_, err = assistant.Run(ctx, &assistants.CallInput{Input: "input"}, new(chatmodel.OutputResult))
	assert.Error(t, err)
}

func Test_Assistant_Run_ToolCallEdgeCases(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})

	mockFactory := mockllmfactory.NewMockFactory(ctrl)
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).AnyTimes()
	mockLLM.EXPECT().GetName().Return("gpt-4o").AnyTimes()
	assistant := assistants.NewAssistant[chatmodel.OutputResult](mockFactory, systemPrompt, assistants.WithModel(mockLLM))

	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	// Tool not found case
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(&llms.ContentResponse{
		Choices: []*llms.ContentChoice{{
			ToolCalls: []llms.ToolCall{{
				ID: "1", Type: "function", FunctionCall: &llms.FunctionCall{Name: "not_found", Arguments: "{}"},
			}},
		}},
	}, nil).Times(1)
	// Final response after tool error
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(&llms.ContentResponse{
		Choices: []*llms.ContentChoice{{
			Content: "I apologize, but I couldn't find the requested tool.",
		}},
	}, nil).Times(1)
	_, err := assistant.Run(ctx, &assistants.CallInput{Input: "input"}, nil)
	assert.NoError(t, err)

	// Tool returns error case: error is wrapped as structured JSON tool content
	// so the LLM can reason about the failure instead of receiving raw text.
	toolErr := errors.New(`db query failed: column "id" not found`)
	mockTool := mocktools.NewMockTool[any, any](ctrl)
	mockTool.EXPECT().Name().Return("err_tool").Times(1)
	mockTool.EXPECT().Description().Return("desc").Times(1)
	mockTool.EXPECT().Parameters().Return(nil).Times(1)
	mockTool.EXPECT().Call(gomock.Any(), gomock.Any()).Return("", toolErr).Times(1)
	assistant = assistant.WithTools(mockTool)
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(&llms.ContentResponse{
		Choices: []*llms.ContentChoice{{
			ToolCalls: []llms.ToolCall{{
				ID: "2", Type: "function", FunctionCall: &llms.FunctionCall{Name: "err_tool", Arguments: "{}"},
			}},
		}},
	}, nil).Times(1)
	// Final response after tool error — assert the tool message content format
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, messages []llms.Message, _ ...llms.CallOption) (*llms.ContentResponse, error) {
			var toolResp *llms.ToolCallResponse
			for _, msg := range messages {
				if msg.Role != llms.RoleTool {
					continue
				}
				require.Len(t, msg.Parts, 1)
				tr, ok := msg.Parts[0].(llms.ToolCallResponse)
				require.True(t, ok)
				toolResp = &tr
			}
			require.NotNil(t, toolResp, "expected a RoleTool message with the error payload")
			assert.Equal(t, "2", toolResp.ToolCallID)
			assert.Equal(t, "err_tool", toolResp.Name)
			// Must be valid JSON with an "error" field (quotes in the message escaped)
			assert.JSONEq(t,
				`{"error":"tool err_tool call failed: db query failed: column \"id\" not found"}`,
				toolResp.Content,
			)
			return &llms.ContentResponse{
				Choices: []*llms.ContentChoice{{
					Content: "I encountered an error while trying to use the tool.",
				}},
			}, nil
		},
	).Times(1)
	resp, err := assistant.Run(ctx, &assistants.CallInput{Input: "input"}, nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	// Also surface the structured error in the returned message history
	var foundErrContent bool
	for _, msg := range resp.Messages {
		if msg.Role != llms.RoleTool {
			continue
		}
		tr, ok := msg.Parts[0].(llms.ToolCallResponse)
		require.True(t, ok)
		assert.JSONEq(t,
			`{"error":"tool err_tool call failed: db query failed: column \"id\" not found"}`,
			tr.Content,
		)
		foundErrContent = true
	}
	assert.True(t, foundErrContent, "expected structured error tool response in Messages")

	// Tool returns success case
	mockTool = mocktools.NewMockTool[any, any](ctrl)
	mockTool.EXPECT().Name().Return("success_tool").Times(1)
	mockTool.EXPECT().Description().Return("desc").Times(1)
	mockTool.EXPECT().Parameters().Return(nil).Times(1)
	mockTool.EXPECT().Call(gomock.Any(), gomock.Any()).Return("tool result", nil).Times(1)
	assistant = assistant.WithTools(mockTool)
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(&llms.ContentResponse{
		Choices: []*llms.ContentChoice{{
			ToolCalls: []llms.ToolCall{{
				ID: "3", Type: "function", FunctionCall: &llms.FunctionCall{Name: "success_tool", Arguments: "{}"},
			}},
		}},
	}, nil).Times(1)
	// Final response after successful tool execution
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(&llms.ContentResponse{
		Choices: []*llms.ContentChoice{{
			Content: "Based on the tool result, here is my response.",
		}},
	}, nil).Times(1)
	_, err = assistant.Run(ctx, &assistants.CallInput{Input: "input"}, nil)
	assert.NoError(t, err)
}

func Test_Assistant_Run_RequiresChatContext(t *testing.T) {
	ctrl := gomock.NewController(t)
	a := newRunAssistant(newRunModel(ctrl, llms.ProviderOpenAI))

	resp, err := a.Run(context.Background(), &assistants.CallInput{Input: runUserInput}, nil)
	assert.Nil(t, resp)
	assert.True(t, errors.Is(err, chatmodel.ErrInvalidChatContext))
}

func Test_Assistant_Run_NoModelNoFactory(t *testing.T) {
	a := assistants.NewAssistant[chatmodel.OutputResult](nil, prompts.NewPromptTemplate(runSystemPrompt, nil))

	resp, err := a.Run(newRunCtx(), &assistants.CallInput{Input: runUserInput}, nil)
	assert.Nil(t, resp)
	assert.EqualError(t, err, "no llm factory or model provided")
}

func Test_Assistant_Run_ModelFromFactory(t *testing.T) {
	ctrl := gomock.NewController(t)
	factory := mockllmfactory.NewMockFactory(ctrl)

	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	chatCtx.SetOrgID("org1")
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)
	modelOpts := llmfactory.ModelOptions{
		OrgID:         "org1",
		AssistantName: runAssistantName,
	}

	a := assistants.NewAssistant[chatmodel.OutputResult](factory, prompts.NewPromptTemplate(runSystemPrompt, nil),
		assistants.WithMode(encoding.ModeJSONSchema)).
		WithName(runAssistantName)

	factory.EXPECT().GetModel(gomock.Any(), modelOpts).Return(nil, errRun).Times(1)
	resp, err := a.Run(ctx, &assistants.CallInput{Input: runUserInput}, nil)
	assert.Nil(t, resp)
	assert.EqualError(t, err, "unable to get LLM model for assistant RunAssistant: boom")
	assert.True(t, errors.Is(err, errRun))

	model := newRunModel(ctrl, llms.ProviderOpenAI)
	factory.EXPECT().GetModel(gomock.Any(), modelOpts).Return(model, nil).Times(1)
	model.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ []llms.Message, opts ...llms.CallOption) (*llms.ContentResponse, error) {
			var callOpts llms.CallOptions
			for _, opt := range opts {
				opt(&callOpts)
			}
			// The resolved model name and its response format are applied.
			assert.Equal(t, runModelName, callOpts.Model)
			if assert.NotNil(t, callOpts.ResponseFormat) {
				assert.Equal(t, "json_schema", callOpts.ResponseFormat.Type)
			}
			return textResponse(runAnswerJSON), nil
		}).Times(1)

	var out chatmodel.OutputResult
	resp, err = a.Run(ctx, &assistants.CallInput{Input: runUserInput}, &out)
	require.NoError(t, err)
	assert.Equal(t, runModelName, resp.Model)
	assert.Equal(t, runAnswer, out.Content)

	// The resolved model is per call and does not leak into the assistant.
	cfg := a.GetCallConfig()
	assert.Nil(t, cfg.Model)
	assert.Empty(t, cfg.ModelName)
	assert.Nil(t, cfg.ResponseFormat)
}

func Test_Assistant_Run_InvalidChatID(t *testing.T) {
	ctrl := gomock.NewController(t)
	a := newRunAssistant(newRunModel(ctrl, llms.ProviderOpenAI))

	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	chatCtx.SetChatID("")
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	resp, err := a.Run(ctx, &assistants.CallInput{Input: runUserInput}, nil)
	assert.Nil(t, resp)
	assert.EqualError(t, err, "invalid chat ID")
}

func Test_Assistant_Run_SystemPromptError(t *testing.T) {
	ctrl := gomock.NewController(t)
	model := newRunModel(ctrl, llms.ProviderOpenAI)
	a := assistants.NewAssistant[chatmodel.OutputResult](nil,
		prompts.NewPromptTemplate("Hello {{.name}}", []string{"name"}),
		assistants.WithModel(model))
	a.WithPromptInputProvider(func(context.Context, string) (map[string]any, error) {
		return nil, errRun
	})

	resp, err := a.Run(newRunCtx(), &assistants.CallInput{Input: runUserInput}, nil)
	assert.Nil(t, resp)
	assert.EqualError(t, err, "failed to format system prompt: failed to get prompt inputs: boom")
	assert.True(t, errors.Is(err, errRun))
}

func Test_Assistant_Run_History(t *testing.T) {
	ctrl := gomock.NewController(t)
	model := newRunModel(ctrl, llms.ProviderOpenAI)

	example := chatmodel.FewShotExample{
		Prompt:     "What is the capital of Spain?",
		Completion: "Madrid",
	}
	extra := llms.MessageFromTextParts(llms.RoleHuman, "Answer briefly.")
	a := newRunAssistant(model, assistants.WithExamples(chatmodel.FewShotExamples{example}))
	a.WithInputParser(func(input string) (string, error) {
		return "parsed: " + input, nil
	})

	expSent := []string{
		"system: " + runSystemPrompt,
		"human: " + llmutils.AddComment("assistant", "example", "question", example.Prompt),
		"ai: " + llmutils.AddComment("assistant", "example", "answer", example.Completion),
		"human: parsed: " + runUserInput,
		"human: Answer briefly.",
	}
	model.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, msgs []llms.Message, _ ...llms.CallOption) (*llms.ContentResponse, error) {
			assert.Equal(t, expSent, messageTexts(msgs))
			for _, m := range msgs {
				// Every message is attributed to the assistant run.
				if assert.NotNil(t, m.Source) {
					assert.Equal(t, runAssistantName, m.Source.Name)
				}
			}
			return textResponse(runAnswer), nil
		}).Times(1)

	resp, err := a.Run(newRunCtx(), &assistants.CallInput{
		Input:    runUserInput,
		Messages: []llms.Message{extra},
	}, nil)
	require.NoError(t, err)
	// Only the run's own messages are returned, not the examples.
	assert.Equal(t, []string{
		"human: parsed: " + runUserInput,
		"human: Answer briefly.",
		"ai: " + runAnswer,
	}, messageTexts(resp.Messages))
}

func Test_Assistant_Run_InputParserError(t *testing.T) {
	ctrl := gomock.NewController(t)
	a := newRunAssistant(newRunModel(ctrl, llms.ProviderOpenAI))
	a.WithInputParser(func(string) (string, error) {
		return "", errRun
	})

	resp, err := a.Run(newRunCtx(), &assistants.CallInput{Input: runUserInput}, nil)
	assert.Nil(t, resp)
	assert.EqualError(t, err, "failed to parse input: boom")
}

func Test_Assistant_Run_Generic(t *testing.T) {
	ctrl := gomock.NewController(t)
	model := newRunModel(ctrl, llms.ProviderOpenAI)
	a := newRunAssistant(model, assistants.WithGeneric(true))

	question := llmutils.AddComment("assistant", runAssistantName, "question", runUserInput)
	model.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, msgs []llms.Message, _ ...llms.CallOption) (*llms.ContentResponse, error) {
			assert.Equal(t, []string{
				"system: " + runSystemPrompt,
				"generic: " + question,
			}, messageTexts(msgs))
			return textResponse(runAnswer), nil
		}).Times(1)

	resp, err := a.Run(newRunCtx(), &assistants.CallInput{Input: runUserInput}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"generic: " + question,
		"generic: " + llmutils.AddComment("assistant", runAssistantName, "observation", runAnswer),
	}, messageTexts(resp.Messages))
}

func Test_Assistant_Run_FunctionCallingUnsupported(t *testing.T) {
	ctrl := gomock.NewController(t)
	a := newRunAssistant(newRunModel(ctrl, noCapsProvider)).WithTools(newLookupTool(ctrl))

	resp, err := a.Run(newRunCtx(), &assistants.CallInput{Input: runUserInput}, nil)
	assert.Nil(t, resp)
	assert.EqualError(t, err, "assistant RunAssistant: the NO_CAPABILITIES provider does not support function calling")
}

func Test_Assistant_Run_Limits(t *testing.T) {
	t.Run("messages", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		// The system prompt alone reaches the limit.
		a := newRunAssistant(newRunModel(ctrl, llms.ProviderOpenAI), assistants.WithMaxMessages(1))

		resp, err := a.Run(newRunCtx(), &assistants.CallInput{Input: runUserInput}, nil)
		assert.Nil(t, resp)
		assert.EqualError(t, err, "assistant RunAssistant: the messages count exceeded limit")
	})

	t.Run("content size", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		a := newRunAssistant(newRunModel(ctrl, llms.ProviderOpenAI), assistants.WithMaxLength(len(runSystemPrompt)))

		resp, err := a.Run(newRunCtx(), &assistants.CallInput{Input: runUserInput}, nil)
		assert.Nil(t, resp)
		assert.EqualError(t, err, "assistant RunAssistant: the content size exceeded limit")
	})

	t.Run("tool calls", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		model := newRunModel(ctrl, llms.ProviderOpenAI)
		tool := newLookupTool(ctrl)
		tool.EXPECT().Call(gomock.Any(), runToolArgs).Return(runToolResult, nil).Times(1)
		model.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(toolCallResponse(functionCall("call-1", runToolName)), nil).Times(1)

		a := newRunAssistant(model, assistants.WithMaxToolCalls(1)).WithTools(tool)
		resp, err := a.Run(newRunCtx(), &assistants.CallInput{Input: runUserInput}, nil)
		assert.Nil(t, resp)
		assert.EqualError(t, err, "assistant RunAssistant: the tool calls limit is exceeded")
	})

	t.Run("tools not found", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		model := newRunModel(ctrl, llms.ProviderOpenAI)
		a := newRunAssistant(model).WithTools(newLookupTool(ctrl))

		missing := []string{"missing_1", "missing_2", "missing_3", "missing_4"}
		callback := mockassitants.NewMockCallback(ctrl)
		callback.EXPECT().OnAssistantStart(gomock.Any(), a, runUserInput).Times(1)
		callback.EXPECT().OnAssistantLLMCallStart(gomock.Any(), a, model, gomock.Any()).Times(1)
		callback.EXPECT().OnAssistantLLMCallEnd(gomock.Any(), a, model, gomock.Any()).Times(1)
		var calls []llms.ToolCall
		for i, name := range missing {
			calls = append(calls, functionCall(fmt.Sprintf("call-%d", i), name))
			callback.EXPECT().OnToolNotFound(gomock.Any(), a, name).Times(1)
		}
		callback.EXPECT().OnAssistantError(gomock.Any(), a, runUserInput, gomock.Any(), gomock.Any()).Times(1)

		model.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(toolCallResponse(calls...), nil).Times(1)

		resp, err := a.Run(newRunCtx(), &assistants.CallInput{
			Input:   runUserInput,
			Options: []assistants.Option{assistants.WithCallback(callback)},
		}, nil)
		assert.Nil(t, resp)
		assert.EqualError(t, err, "assistant RunAssistant: the number of not found tools is exceeded")
	})
}

func Test_Assistant_Run_MultipleChoices(t *testing.T) {
	ctrl := gomock.NewController(t)
	model := newRunModel(ctrl, llms.ProviderOpenAI)
	model.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(textResponse("  first \n", "second"), nil).Times(1)

	resp, err := newRunAssistant(model).Run(newRunCtx(), &assistants.CallInput{Input: runUserInput}, nil)
	require.NoError(t, err)
	require.Len(t, resp.Choices, 2)
	// The choices are trimmed and joined into one AI message.
	assert.Equal(t, []string{
		"human: " + runUserInput,
		"ai: first\n\nsecond",
	}, messageTexts(resp.Messages))
}

func Test_Assistant_Run_RetryOnUnparsableOutput(t *testing.T) {
	ctrl := gomock.NewController(t)
	model := newRunModel(ctrl, llms.ProviderOpenAI)
	memStore := store.NewMemoryStore()
	a := newRunAssistant(model, assistants.WithMessageStore(memStore))

	callback := mockassitants.NewMockCallback(ctrl)
	callback.EXPECT().OnAssistantStart(gomock.Any(), a, runUserInput).Times(1)
	callback.EXPECT().OnAssistantLLMCallStart(gomock.Any(), a, model, gomock.Any()).Times(2)
	callback.EXPECT().OnAssistantLLMCallEnd(gomock.Any(), a, model, gomock.Any()).Times(2)
	callback.EXPECT().OnAssistantLLMParseError(gomock.Any(), a, runUserInput, runUnparsable, isUnmarshalOutputErr).Times(1)
	callback.EXPECT().OnAssistantError(gomock.Any(), a, runUserInput, isUnmarshalOutputErr, gomock.Any()).Times(1)
	callback.EXPECT().OnAssistantEnd(gomock.Any(), a, gomock.Any(), gomock.Any(), gomock.Any()).Times(1)

	gomock.InOrder(
		model.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(textResponse(runUnparsable), nil),
		model.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, msgs []llms.Message, _ ...llms.CallOption) (*llms.ContentResponse, error) {
				// The failed attempt was stored, so the retry still has the
				// question and the unparsable answer.
				assert.Equal(t, []string{
					"system: " + runSystemPrompt,
					"human: " + runUserInput,
					"ai: " + runUnparsable,
					"human: " + runRetryInput,
				}, messageTexts(msgs))
				return textResponse(runAnswerJSON), nil
			}),
	)

	ctx := newRunCtx()
	var out chatmodel.OutputResult
	resp, err := a.Run(ctx, &assistants.CallInput{
		Input:   runUserInput,
		Options: []assistants.Option{assistants.WithCallback(callback)},
	}, &out)
	require.NoError(t, err)
	assert.Equal(t, runAnswer, out.Content)
	// The run's messages include the failed attempt, and the parsed
	// content, not the raw JSON, is recorded.
	assert.Equal(t, []string{
		"human: " + runUserInput,
		"ai: " + runUnparsable,
		"human: " + runRetryInput,
		"ai: " + runAnswer,
	}, messageTexts(resp.Messages))
	assert.Equal(t, []string{
		"human: " + runUserInput,
		"ai: " + runUnparsable,
		"human: " + runRetryInput,
		"ai: " + runAnswer,
	}, messageTexts(memStore.Messages(ctx)))
}

// F-011: without a store the retry still sends the question and the
// unparsable answer, sends the same function and native tools as the first
// attempt, does not run the input parser on the retry prompt, and leaves the
// caller's input unchanged.
func Test_Assistant_Run_RetryWithoutStore(t *testing.T) {
	const (
		parsedPrefix = "parsed: "
		extraInput   = "extra context"
		webSearch    = "web_search"
	)
	ctrl := gomock.NewController(t)
	model := newRunModel(ctrl, llms.ProviderOpenAI)
	a := newRunAssistant(model, assistants.WithTools(llms.Tool{Type: webSearch})).
		WithTools(newLookupTool(ctrl))
	a.WithInputParser(func(in string) (string, error) {
		return parsedPrefix + in, nil
	})

	sentTools := func(opts []llms.CallOption) []string {
		var callOpts llms.CallOptions
		for _, opt := range opts {
			opt(&callOpts)
		}
		var names []string
		for _, tool := range callOpts.Tools {
			name := tool.Type
			if tool.Function != nil {
				name += ":" + tool.Function.Name
			}
			names = append(names, name)
		}
		return names
	}
	wantTools := []string{
		webSearch,
		llms.ToolTypeFunction + ":" + runToolName,
	}

	gomock.InOrder(
		model.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ []llms.Message, opts ...llms.CallOption) (*llms.ContentResponse, error) {
				assert.Equal(t, wantTools, sentTools(opts))
				return textResponse(runUnparsable), nil
			}),
		model.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, msgs []llms.Message, opts ...llms.CallOption) (*llms.ContentResponse, error) {
				assert.Equal(t, []string{
					"system: " + runSystemPrompt,
					"human: " + parsedPrefix + runUserInput,
					"human: " + extraInput,
					"ai: " + runUnparsable,
					"human: " + runRetryInput,
				}, messageTexts(msgs))
				assert.Equal(t, wantTools, sentTools(opts), "the retry sends the same tools")
				return textResponse(runAnswerJSON), nil
			}),
	)

	input := &assistants.CallInput{
		Input: runUserInput,
		Messages: []llms.Message{
			llms.MessageFromTextParts(llms.RoleHuman, extraInput),
		},
	}
	var out chatmodel.OutputResult
	resp, err := a.Run(newRunCtx(), input, &out)
	require.NoError(t, err)
	assert.Equal(t, runAnswer, out.Content)
	assert.Equal(t, []string{
		"human: " + parsedPrefix + runUserInput,
		"human: " + extraInput,
		"ai: " + runUnparsable,
		"human: " + runRetryInput,
		"ai: " + runAnswer,
	}, messageTexts(resp.Messages))
	assert.Equal(t, runUserInput, input.Input, "the caller's input is not modified")
	assert.Equal(t, []string{"human: " + extraInput}, messageTexts(input.Messages))
}

// The retry continues the failed attempt's full history, so the tool
// exchange is resent even when SkipToolHistory leaves it out of the
// response and the store.
func Test_Assistant_Run_RetryKeepsToolExchange(t *testing.T) {
	const callID = "call_1"
	toolExchange := []string{
		"ai: ToolCall: " + callID + " (" + runToolName + "), input: " + runToolArgs,
		"tool: response " + callID + " " + runToolName + ": " + runToolResult,
	}
	for _, withStore := range []bool{false, true} {
		t.Run(fmt.Sprintf("store=%t", withStore), func(t *testing.T) {
			ctrl := gomock.NewController(t)
			model := newRunModel(ctrl, llms.ProviderOpenAI)
			opts := []assistants.Option{
				assistants.WithSkipToolHistory(true),
			}
			if withStore {
				opts = append(opts, assistants.WithMessageStore(store.NewMemoryStore()))
			}
			tool := newLookupTool(ctrl)
			tool.EXPECT().Call(gomock.Any(), runToolArgs).Return(runToolResult, nil).Times(1)
			a := newRunAssistant(model, opts...).WithTools(tool)

			wantRetryHistory := append([]string{
				"system: " + runSystemPrompt,
				"human: " + runUserInput,
			}, toolExchange...)
			wantRetryHistory = append(wantRetryHistory,
				"ai: "+runUnparsable,
				"human: "+runRetryInput,
			)
			gomock.InOrder(
				model.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(toolCallResponse(functionCall(callID, runToolName)), nil),
				model.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(textResponse(runUnparsable), nil),
				model.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, msgs []llms.Message, _ ...llms.CallOption) (*llms.ContentResponse, error) {
						assert.Equal(t, wantRetryHistory, messageTexts(msgs))
						return textResponse(runAnswerJSON), nil
					}),
			)

			var out chatmodel.OutputResult
			resp, err := a.Run(newRunCtx(), &assistants.CallInput{Input: runUserInput}, &out)
			require.NoError(t, err)
			assert.Equal(t, runAnswer, out.Content)
			assert.Equal(t, []string{
				"human: " + runUserInput,
				"ai: " + runUnparsable,
				"human: " + runRetryInput,
				"ai: " + runAnswer,
			}, messageTexts(resp.Messages), "SkipToolHistory still leaves the tool exchange out of the response")
		})
	}
}

func Test_Assistant_Run_UnparsableOutputAfterRetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	model := newRunModel(ctrl, llms.ProviderOpenAI)
	a := newRunAssistant(model)

	callback := mockassitants.NewMockCallback(ctrl)
	callback.EXPECT().OnAssistantStart(gomock.Any(), a, runUserInput).Times(1)
	callback.EXPECT().OnAssistantLLMCallStart(gomock.Any(), a, model, gomock.Any()).Times(2)
	callback.EXPECT().OnAssistantLLMCallEnd(gomock.Any(), a, model, gomock.Any()).Times(2)
	callback.EXPECT().OnAssistantLLMParseError(gomock.Any(), a, gomock.Any(), runUnparsable, isUnmarshalOutputErr).Times(2)
	callback.EXPECT().OnAssistantError(gomock.Any(), a, gomock.Any(), isUnmarshalOutputErr, gomock.Any()).Times(2)

	model.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(textResponse(runUnparsable), nil).Times(2)

	var out chatmodel.OutputResult
	resp, err := a.Run(newRunCtx(), &assistants.CallInput{
		Input:   runUserInput,
		Options: []assistants.Option{assistants.WithCallback(callback)},
	}, &out)
	assert.Nil(t, resp)
	assert.True(t, errors.Is(err, chatmodel.ErrFailedUnmarshalOutput), "err: %v", err)
	assert.Empty(t, out.Content)
}

func Test_Assistant_Run_ToolCallWithoutID(t *testing.T) {
	ctrl := gomock.NewController(t)
	model := newRunModel(ctrl, llms.ProviderOpenAI)
	tool := newLookupTool(ctrl)
	tool.EXPECT().Call(gomock.Any(), runToolArgs).Return(runToolResult, nil).Times(2)

	// The model omits the tool call ID: one is derived from the tool name and
	// the call's position, and both history entries carry it.
	expToolHistory := []string{
		"human: " + runUserInput,
		"ai: ToolCall: lookup_0 (lookup), input: {}",
		"tool: response lookup_0 lookup: " + runToolResult,
	}
	runOnce := func(opts ...assistants.Option) *assistants.Response {
		gomock.InOrder(
			model.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(toolCallResponse(functionCall("", runToolName)), nil),
			model.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, msgs []llms.Message, _ ...llms.CallOption) (*llms.ContentResponse, error) {
					// The model always sees the tool exchange.
					assert.Equal(t, append([]string{"system: " + runSystemPrompt}, expToolHistory...), messageTexts(msgs))
					return textResponse(runAnswer), nil
				}),
		)
		resp, err := newRunAssistant(model, opts...).WithTools(tool).
			Run(newRunCtx(), &assistants.CallInput{Input: runUserInput}, nil)
		require.NoError(t, err)
		return resp
	}

	resp := runOnce()
	assert.Equal(t, append(expToolHistory, "ai: "+runAnswer), messageTexts(resp.Messages))

	// SkipToolHistory keeps the tool exchange out of the returned messages.
	resp = runOnce(assistants.WithSkipToolHistory(true))
	assert.Equal(t, []string{
		"human: " + runUserInput,
		"ai: " + runAnswer,
	}, messageTexts(resp.Messages))
}

// Test_Assistant_Run_ToolCallWithoutFunctionCall runs a provider-native tool
// call, which has no FunctionCall, through the loop. It used to panic while
// deriving the missing ID and copying the call into the history (F-038).
func Test_Assistant_Run_ToolCallWithoutFunctionCall(t *testing.T) {
	ctrl := gomock.NewController(t)
	model := newRunModel(ctrl, llms.ProviderOpenAI)
	tool := newLookupTool(ctrl)

	const nativeTool = "web_search"
	expToolHistory := []string{
		"human: " + runUserInput,
		"ai: ToolCall: web_search_0 (web_search), input: ",
		"tool: response web_search_0 web_search: Tool `web_search` not found. Please check the tool name and try again with exact match. Available tools: " + runToolName,
	}
	gomock.InOrder(
		model.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(toolCallResponse(llms.ToolCall{
				Type: nativeTool,
			}), nil),
		model.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, msgs []llms.Message, _ ...llms.CallOption) (*llms.ContentResponse, error) {
				assert.Equal(t, append([]string{"system: " + runSystemPrompt}, expToolHistory...), messageTexts(msgs))
				return textResponse(runAnswer), nil
			}),
	)

	resp, err := newRunAssistant(model).WithTools(tool).
		Run(newRunCtx(), &assistants.CallInput{Input: runUserInput}, nil)
	require.NoError(t, err)
	assert.Equal(t, append(expToolHistory, "ai: "+runAnswer), messageTexts(resp.Messages))
}

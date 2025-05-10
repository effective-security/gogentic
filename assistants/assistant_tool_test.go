package assistants_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/encoding"
	"github.com/effective-security/gogentic/mocks/mockllms"
	"github.com/effective-security/gogentic/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/prompts"
	"go.uber.org/mock/gomock"
)

func Test_AssistantTool(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})

	calls := 0
	// Create a mock LLM
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
			calls++
			return &llms.ContentResponse{
				Choices: []*llms.ContentChoice{
					{
						Content: fmt.Sprintf("This is a test answer %d.", calls),
					},
				},
			}, nil
		}).Times(2)

	memstore := store.NewMemoryStore()

	var buf strings.Builder
	acfg := []assistants.Option{
		assistants.WithMode(encoding.ModePlainText),
		assistants.WithJSONMode(false),
		assistants.WithMessageStore(memstore),
		assistants.WithCallback(assistants.NewPrinterCallback(&buf)),
	}

	ag := assistants.NewAssistant[chatmodel.String](mockLLM, systemPrompt, acfg...)

	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	sysPrompt, err := ag.GetSystemPrompt("", nil)
	require.NoError(t, err)
	expPrompt := `You are helpful and friendly AI assistant.`
	assert.Equal(t, expPrompt, sysPrompt)

	apiResp, err := ag.Call(ctx, "What is a capital of largest country in Europe?", nil)
	require.NoError(t, err)
	assert.NotEmpty(t, apiResp.Choices)

	history := memstore.Messages(ctx)
	assert.NotEmpty(t, history)
	exp := `Human: What is a capital of largest country in Europe?
AI: This is a test answer 1.`
	chat, err := llms.GetBufferString(history, "Human", "AI")
	require.NoError(t, err)
	assert.Equal(t, exp, chat)

	tool, err := assistants.NewAssistantTool[chatmodel.String, chatmodel.String](ag)
	require.NoError(t, err)

	tres, err := tool.CallAssistant(ctx, "What is a capital of largest country in Europe?", assistants.WithMessageStore(memstore))
	require.NoError(t, err)
	assert.Equal(t, "This is a test answer 2.", tres)
}

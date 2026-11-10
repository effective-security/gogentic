package bedrock_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/bedrock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setUpTest() (*bedrockruntime.Client, error) {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, err
	}
	client := bedrockruntime.NewFromConfig(cfg)
	return client, nil
}

func TestAmazonOutput(t *testing.T) {
	t.Parallel()

	if os.Getenv("TEST_AWS") != "true" {
		t.Skip("Skipping test, requires AWS access")
	}

	client, err := setUpTest()
	if err != nil {
		t.Fatal(err)
	}
	llm, err := bedrock.New(bedrock.WithClient(client))
	if err != nil {
		t.Fatal(err)
	}

	msgs := []llms.Message{
		llms.MessageFromTextParts(llms.RoleSystem, "You know all about AI."),
		llms.MessageFromTextParts(llms.RoleHuman, "Explain AI in 10 words or less."),
	}

	// All the test models.
	models := []string{
		bedrock.ModelAi21J2MidV1,
		bedrock.ModelAi21J2UltraV1,
		bedrock.ModelAmazonTitanTextLiteV1,
		bedrock.ModelAmazonTitanTextExpressV1,
		bedrock.ModelAnthropicClaudeV3Sonnet,
		bedrock.ModelAnthropicClaudeV3Haiku,
		bedrock.ModelAnthropicClaudeV21,
		bedrock.ModelAnthropicClaudeV2,
		bedrock.ModelAnthropicClaudeInstantV1,
		bedrock.ModelCohereCommandTextV14,
		bedrock.ModelCohereCommandLightTextV14,
		bedrock.ModelMetaLlama213bChatV1,
		bedrock.ModelMetaLlama270bChatV1,
		bedrock.ModelMetaLlama38bInstructV1,
		bedrock.ModelMetaLlama370bInstructV1,
	}

	ctx := context.Background()

	for _, model := range models {
		t.Logf("Model output for %s:-", model)

		resp, err := llm.GenerateContent(ctx, msgs, llms.WithModel(model), llms.WithMaxTokens(512))
		if err != nil {
			t.Fatal(err)
		}
		for i, choice := range resp.Choices {
			t.Logf("Choice %d: %s", i, choice.Content)
		}
	}
}

const (
	fakeRegion      = "us-east-1"
	fakeAccessKey   = "AKIDFAKE"
	fakeSecretKey   = "fake-secret"
	claudeModel     = "anthropic.claude-3-haiku-20240307-v1:0"
	invokePathFmt   = "/model/%s/invoke"
	claudeResponse  = `{"type":"message","role":"assistant","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":1}}`
	missingProfile  = "gogentic-missing-profile"
	envConfigFile   = "AWS_CONFIG_FILE"
	envCredsFile    = "AWS_SHARED_CREDENTIALS_FILE"
	envRegion       = "AWS_REGION"
	envProfile      = "AWS_PROFILE"
	unsupportedPart = "unsupported message type"
)

// httpDoer adapts a function to the aws.HTTPClient interface.
type httpDoer func(*http.Request) (*http.Response, error)

func (f httpDoer) Do(r *http.Request) (*http.Response, error) { return f(r) }

// recorder fakes the Bedrock runtime HTTP API: it records request paths and
// bodies and always answers with response.
type recorder struct {
	t        *testing.T
	response string
	paths    []string
	bodies   []string
}

func (rec *recorder) config() aws.Config {
	return aws.Config{
		Region:           fakeRegion,
		Credentials:      credentials.NewStaticCredentialsProvider(fakeAccessKey, fakeSecretKey, ""),
		RetryMaxAttempts: 1,
		HTTPClient: httpDoer(func(r *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(r.Body)
			require.NoError(rec.t, err)
			rec.paths = append(rec.paths, r.URL.Path)
			rec.bodies = append(rec.bodies, string(body))
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body:    io.NopCloser(strings.NewReader(rec.response)),
				Request: r,
			}, nil
		}),
	}
}

func (rec *recorder) client() *bedrockruntime.Client {
	return bedrockruntime.NewFromConfig(rec.config())
}

// isolateAWSConfig points the shared AWS config at empty files so tests never
// read the developer's profiles.
func isolateAWSConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(envConfigFile, filepath.Join(dir, "config"))
	t.Setenv(envCredsFile, filepath.Join(dir, "credentials"))
	t.Setenv(envRegion, fakeRegion)
}

func TestNewWithConfigGeneratesContent(t *testing.T) {
	rec := &recorder{t: t, response: claudeResponse}
	cfg := rec.config()
	llm, err := bedrock.New(bedrock.WithConfig(&cfg), bedrock.WithModel(claudeModel))
	require.NoError(t, err)
	assert.Equal(t, claudeModel, llm.GetName())
	assert.Equal(t, llms.ProviderBedrock, llm.GetProviderType())

	resp, err := llm.GenerateContent(context.Background(), []llms.Message{
		llms.MessageFromTextParts(llms.RoleSystem, "be brief"),
		{
			Role: llms.RoleHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{
					Text: "what is this?",
				},
				llms.BinaryContent{
					MIMEType: "image/png",
					Data:     []byte("PNG"),
				},
			},
		},
		{
			Role: llms.RoleAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{
					ID:   "call_1",
					Type: "function",
					FunctionCall: &llms.FunctionCall{
						Name:      "lookup",
						Arguments: `{"q":"x"}`,
					},
				},
			},
		},
		{
			Role: llms.RoleTool,
			Parts: []llms.ContentPart{
				llms.ToolCallResponse{
					ToolCallID: "call_1",
					Name:       "lookup",
					Content:    "result",
				},
			},
		},
	}, llms.WithMaxTokens(100))
	require.NoError(t, err)
	require.Len(t, resp.Choices, 1)
	assert.Equal(t, "done", resp.Choices[0].Content)
	assert.Equal(t, llms.Usage{
		InputTokens:  3,
		OutputTokens: 1,
		TotalTokens:  4,
	}, resp.Choices[0].Usage)

	require.Len(t, rec.paths, 1)
	assert.Equal(t, fmt.Sprintf(invokePathFmt, claudeModel), rec.paths[0])
	assert.JSONEq(t, `{
		"anthropic_version": "bedrock-2023-05-31",
		"max_tokens": 100,
		"system": "be brief",
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "what is this?"},
				{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "UE5H"}}
			]},
			{"role": "assistant", "content": [
				{"type": "tool_use", "id": "call_1", "name": "lookup", "input": {"q": "x"}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "call_1", "content": "result"}
			]}
		]
	}`, rec.bodies[0])
}

func TestGenerateContentCallModelOverridesDefault(t *testing.T) {
	rec := &recorder{t: t, response: `{"generation":"hi","prompt_token_count":1,"generation_token_count":1,"stop_reason":"stop"}`}
	llm, err := bedrock.New(bedrock.WithClient(rec.client()))
	require.NoError(t, err)
	assert.Equal(t, bedrock.ModelAmazonTitanTextLiteV1, llm.GetName(), "titan lite is the default model")

	resp, err := llm.GenerateContent(context.Background(), []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "hello"),
	}, llms.WithModel(bedrock.ModelMetaLlama38bInstructV1))
	require.NoError(t, err)
	require.Len(t, resp.Choices, 1)
	assert.Equal(t, "hi", resp.Choices[0].Content)
	require.Len(t, rec.paths, 1)
	assert.Equal(t, fmt.Sprintf(invokePathFmt, bedrock.ModelMetaLlama38bInstructV1), rec.paths[0])
	assert.JSONEq(t, `{"prompt":"\nhuman: hello\nAI: ","max_gen_len":512}`, rec.bodies[0])
}

func TestGenerateContentErrors(t *testing.T) {
	rec := &recorder{t: t, response: claudeResponse}
	llm, err := bedrock.New(bedrock.WithClient(rec.client()), bedrock.WithModel(claudeModel))
	require.NoError(t, err)

	resp, err := llm.GenerateContent(context.Background(), []llms.Message{
		{
			Role: llms.RoleHuman,
			Parts: []llms.ContentPart{
				llms.ImageURLContent{
					URL: "https://example.com/cat.png",
				},
			},
		},
	})
	require.EqualError(t, err, unsupportedPart)
	assert.Nil(t, resp)

	resp, err = llm.GenerateContent(context.Background(), []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "hello"),
	}, llms.WithModel("mistral.mistral-7b-instruct-v0:2"))
	require.EqualError(t, err, "bedrock: unsupported provider")
	assert.Nil(t, resp)
	assert.Empty(t, rec.paths, "no request is sent for rejected input")
}

func TestCreateEmbedding(t *testing.T) {
	rec := &recorder{t: t, response: `{"embedding":[0.25,0.5],"inputTextTokenCount":2}`}
	llm, err := bedrock.New(bedrock.WithClient(rec.client()), bedrock.WithModel(bedrock.ModelAmazonTitanEmbedTextV1))
	require.NoError(t, err)

	embeddings, err := llm.CreateEmbedding(context.Background(), []string{"hello"})
	require.NoError(t, err)
	assert.Equal(t, [][]float32{
		{0.25, 0.5},
	}, embeddings)
	require.Len(t, rec.paths, 1)
	assert.Equal(t, fmt.Sprintf(invokePathFmt, bedrock.ModelAmazonTitanEmbedTextV1), rec.paths[0])
	assert.JSONEq(t, `{"inputText":"hello"}`, rec.bodies[0])
}

func TestNewLoadsDefaultConfig(t *testing.T) {
	isolateAWSConfig(t)
	llm, err := bedrock.New()
	require.NoError(t, err)
	assert.Equal(t, bedrock.ModelAmazonTitanTextLiteV1, llm.GetName())
	assert.Equal(t, llms.ProviderBedrock, llm.GetProviderType())
}

func TestNewDefaultConfigError(t *testing.T) {
	isolateAWSConfig(t)
	t.Setenv(envProfile, missingProfile)
	llm, err := bedrock.New()
	require.ErrorContains(t, err, "failed to get shared config profile, "+missingProfile)
	assert.Nil(t, llm)
}

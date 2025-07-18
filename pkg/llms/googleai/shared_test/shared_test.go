// nolint
package shared_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/googleai"
	"github.com/effective-security/gogentic/pkg/llms/googleai/vertex"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newGoogleAIClient(t *testing.T, opts ...googleai.Option) *googleai.GoogleAI {
	t.Helper()

	genaiKey := os.Getenv("GENAI_API_KEY")
	if genaiKey == "" {
		t.Skip("GENAI_API_KEY not set")
		return nil
	}

	opts = append(opts, googleai.WithAPIKey(genaiKey))
	llm, err := googleai.New(context.Background(), opts...)
	require.NoError(t, err)
	return llm
}

func newVertexClient(t *testing.T, opts ...googleai.Option) *vertex.Vertex {
	t.Helper()

	// If credentials are set, use them. Otherwise, look for project env var.
	if creds := os.Getenv("VERTEX_CREDENTIALS"); creds != "" {
		opts = append(opts, googleai.WithCredentialsFile(creds))
	} else {
		project := os.Getenv("VERTEX_PROJECT")
		if project == "" {
			t.Skip("VERTEX_PROJECT not set")
			return nil
		}
		location := os.Getenv("VERTEX_LOCATION")
		if location == "" {
			location = "us-central1"
		}

		opts = append(opts,
			googleai.WithCloudProject(project),
			googleai.WithCloudLocation(location),
		)
	}

	llm, err := vertex.New(context.Background(), opts...)
	require.NoError(t, err)
	return llm
}

// funcName obtains the name of the given function value, without a package
// prefix.
func funcName(f any) string {
	fullName := runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
	parts := strings.Split(fullName, ".")
	return parts[len(parts)-1]
}

// testConfigs is a list of all test functions in this file to run with both
// client types, and their client configurations.
type testConfig struct {
	testFunc func(*testing.T, llms.Model)
	opts     []googleai.Option
}

var testConfigs = []testConfig{
	{testMultiContentText, nil},
	{testGenerateFromSinglePrompt, nil},
	{testMultiContentTextChatSequence, nil},
	{testMultiContentWithSystemMessage, nil},
	{testMultiContentImageLink, nil},
	{testMultiContentImageBinary, nil},
	//{testEmbeddings, nil},
	{testCandidateCountSetting, nil},
	{testMaxTokensSetting, nil},
	{testTools, nil},
	{testToolsWithInterfaceRequired, nil},
	{
		testMultiContentText,
		[]googleai.Option{googleai.WithHarmThreshold(googleai.HarmBlockMediumAndAbove)},
	},
	{testWithStreaming, nil},
	{testWithHTTPClient, getHTTPTestClientOptions()},
}

func TestGoogleAIShared(t *testing.T) {
	for _, c := range testConfigs {
		t.Run(fmt.Sprintf("%s-googleai", funcName(c.testFunc)), func(t *testing.T) {
			llm := newGoogleAIClient(t, c.opts...)
			c.testFunc(t, llm)
		})
	}
}

func TestVertexShared(t *testing.T) {
	for _, c := range testConfigs {
		t.Run(fmt.Sprintf("%s-vertex", funcName(c.testFunc)), func(t *testing.T) {
			llm := newVertexClient(t, c.opts...)
			c.testFunc(t, llm)
		})
	}
}

func testMultiContentText(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	content := []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "I'm a pomeranian", "What kind of mammal am I?"),
	}

	rsp, err := llm.GenerateContent(context.Background(), content)
	require.NoError(t, err)

	assert.NotEmpty(t, rsp.Choices)
	c1 := rsp.Choices[0]
	assert.Regexp(t, "(?i)dog|carnivo|canid|canine", c1.Content)
	assert.Contains(t, c1.GenerationInfo, "output_tokens")
	assert.NotZero(t, c1.GenerationInfo["output_tokens"])
}

func testMultiContentTextUsingTextParts(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	content := llms.MessageFromTextParts(
		llms.RoleHuman,
		"I'm a pomeranian",
		"What kind of mammal am I?",
	)

	rsp, err := llm.GenerateContent(context.Background(), []llms.Message{content})
	require.NoError(t, err)

	assert.NotEmpty(t, rsp.Choices)
	c1 := rsp.Choices[0]
	assert.Regexp(t, "(?i)dog|canid|canine", c1.Content)
}

func testGenerateFromSinglePrompt(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	prompt := "name all the planets in the solar system"
	rsp, err := llms.GenerateFromSinglePrompt(context.Background(), llm, prompt)
	require.NoError(t, err)

	assert.Regexp(t, "(?i)jupiter", rsp)
}

func testMultiContentTextChatSequence(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	content := []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "Name some countries"),
		llms.MessageFromTextParts(llms.RoleAI, "Spain and Lesotho"),
		llms.MessageFromTextParts(llms.RoleHuman, "Which if these is larger?"),
	}

	rsp, err := llm.GenerateContent(context.Background(), content, llms.WithModel("gemini-1.5-flash"))
	require.NoError(t, err)

	assert.NotEmpty(t, rsp.Choices)
	c1 := rsp.Choices[0]
	assert.Regexp(t, "(?i)spain.*larger", c1.Content)
}

func testMultiContentWithSystemMessage(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	content := []llms.Message{
		llms.MessageFromTextParts(llms.RoleSystem, "You are a Spanish teacher; answer in Spanish"),
		llms.MessageFromTextParts(llms.RoleHuman, "Name the 5 most common fruits"),
	}

	rsp, err := llm.GenerateContent(context.Background(), content, llms.WithModel("gemini-1.5-flash"))
	require.NoError(t, err)

	assert.NotEmpty(t, rsp.Choices)
	c1 := rsp.Choices[0]
	checkMatch(t, c1.Content, "(manzana|naranja)")
}

func testMultiContentImageLink(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	parts := []llms.ContentPart{
		llms.ImageURLPart(
			"https://github.com/tmc/langchaingo/blob/main/docs/static/img/parrot-icon.png?raw=true",
		),
		llms.TextPart("describe this image in detail"),
	}
	content := []llms.Message{
		{
			Role:  llms.RoleHuman,
			Parts: parts,
		},
	}

	rsp, err := llm.GenerateContent(
		context.Background(),
		content,
		llms.WithModel("gemini-pro-vision"),
	)
	require.NoError(t, err)

	assert.NotEmpty(t, rsp.Choices)
	c1 := rsp.Choices[0]
	checkMatch(t, c1.Content, "parrot")
}

func testMultiContentImageBinary(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	b, err := os.ReadFile(filepath.Join("testdata", "parrot-icon.png"))
	if err != nil {
		t.Fatal(err)
	}

	parts := []llms.ContentPart{
		llms.BinaryPart("image/png", b),
		llms.TextPart("what does this image show? please use detail"),
	}
	content := []llms.Message{
		{
			Role:  llms.RoleHuman,
			Parts: parts,
		},
	}

	rsp, err := llm.GenerateContent(
		context.Background(),
		content,
		llms.WithModel("gemini-pro-vision"),
	)
	require.NoError(t, err)

	assert.NotEmpty(t, rsp.Choices)
	c1 := rsp.Choices[0]
	checkMatch(t, c1.Content, "parrot")
}

// func testEmbeddings(t *testing.T, llm llms.Model) {
// 	t.Helper()
// 	t.Parallel()

// 	texts := []string{"foo", "parrot", "foo"}
// 	emb := llm.(embeddings.EmbedderClient)
// 	res, err := emb.CreateEmbedding(context.Background(), texts)
// 	require.NoError(t, err)

// 	assert.Equal(t, len(texts), len(res))
// 	assert.NotEmpty(t, res[0])
// 	assert.NotEmpty(t, res[1])
// 	assert.Equal(t, res[0], res[2])
// }

func testCandidateCountSetting(t *testing.T, llm llms.Model) {
	t.Helper()

	content := []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "Name five countries in Africa"),
	}

	{
		rsp, err := llm.GenerateContent(context.Background(), content,
			llms.WithCandidateCount(1), llms.WithTemperature(1))
		require.NoError(t, err)

		assert.Len(t, rsp.Choices, 1)
	}

	// TODO: test multiple candidates when the backend supports it
}

func testWithStreaming(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	content := llms.MessageFromTextParts(
		llms.RoleHuman,
		"I'm a pomeranian",
		"Tell me more about my taxonomy",
	)

	var sb strings.Builder
	rsp, err := llm.GenerateContent(
		context.Background(),
		[]llms.Message{content},
		llms.WithStreamingFunc(func(ctx context.Context, chunk []byte) error {
			sb.Write(chunk)
			return nil
		}))

	require.NoError(t, err)

	assert.NotEmpty(t, rsp.Choices)
	c1 := rsp.Choices[0]
	checkMatch(t, c1.Content, "(dog|canid)")
	checkMatch(t, sb.String(), "(dog|canid)")
}

func testTools(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	type Input struct {
		Location string `json:"location"`
	}
	sc, err := schema.New(reflect.TypeOf(Input{}))
	require.NoError(t, err)

	availableTools := []llms.Tool{
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "getCurrentWeather",
				Description: "Get the current weather in a given location",
				Parameters:  sc.Parameters,
			},
		},
	}

	content := []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "What is the weather like in Chicago?"),
	}
	resp, err := llm.GenerateContent(
		context.Background(),
		content,
		llms.WithTools(availableTools))
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Choices)

	c1 := resp.Choices[0]

	// Update chat history with assistant's response, with its tool calls.
	assistantResp := llms.Message{
		Role: llms.RoleAI,
	}
	for _, tc := range c1.ToolCalls {
		assistantResp.Parts = append(assistantResp.Parts, tc)
	}
	content = append(content, assistantResp)

	// "Execute" tool calls by calling requested function
	for _, tc := range c1.ToolCalls {
		switch tc.FunctionCall.Name {
		case "getCurrentWeather":
			var args struct {
				Location string `json:"location"`
			}
			if err := json.Unmarshal([]byte(tc.FunctionCall.Arguments), &args); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(args.Location, "Chicago") {
				toolResponse := llms.Message{
					Role: llms.RoleTool,
					Parts: []llms.ContentPart{
						llms.ToolCallResponse{
							Name:    tc.FunctionCall.Name,
							Content: "64 and sunny",
						},
					},
				}
				content = append(content, toolResponse)
			}
		default:
			t.Errorf("got unexpected function call: %v", tc.FunctionCall.Name)
		}
	}

	resp, err = llm.GenerateContent(context.Background(), content, llms.WithTools(availableTools))
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Choices)

	c1 = resp.Choices[0]
	checkMatch(t, c1.Content, "(64 and sunny|64 degrees)")
	assert.Contains(t, resp.Choices[0].GenerationInfo, "output_tokens")
	assert.NotZero(t, resp.Choices[0].GenerationInfo["output_tokens"])
}

func testToolsWithInterfaceRequired(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	type Input struct {
		Location string `json:"location"`
	}
	sc, err := schema.New(reflect.TypeOf(Input{}))
	require.NoError(t, err)

	availableTools := []llms.Tool{
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "getCurrentWeather",
				Description: "Get the current weather in a given location",
				Parameters:  sc.Parameters,
			},
		},
	}

	content := []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "What is the weather like in Chicago?"),
	}
	resp, err := llm.GenerateContent(
		context.Background(),
		content,
		llms.WithTools(availableTools))
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Choices)

	c1 := resp.Choices[0]
	assert.Contains(t, c1.GenerationInfo, "output_tokens")
	assert.NotZero(t, c1.GenerationInfo["output_tokens"])

	// Update chat history with assistant's response, with its tool calls.
	assistantResp := llms.Message{
		Role: llms.RoleAI,
	}
	for _, tc := range c1.ToolCalls {
		assistantResp.Parts = append(assistantResp.Parts, tc)
	}
	content = append(content, assistantResp)

	// "Execute" tool calls by calling requested function
	for _, tc := range c1.ToolCalls {
		switch tc.FunctionCall.Name {
		case "getCurrentWeather":
			var args struct {
				Location string `json:"location"`
			}
			if err := json.Unmarshal([]byte(tc.FunctionCall.Arguments), &args); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(args.Location, "Chicago") {
				toolResponse := llms.Message{
					Role: llms.RoleTool,
					Parts: []llms.ContentPart{
						llms.ToolCallResponse{
							Name:    tc.FunctionCall.Name,
							Content: "64 and sunny",
						},
					},
				}
				content = append(content, toolResponse)
			}
		default:
			t.Errorf("got unexpected function call: %v", tc.FunctionCall.Name)
		}
	}

	resp, err = llm.GenerateContent(context.Background(), content, llms.WithTools(availableTools))
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Choices)

	c1 = resp.Choices[0]
	checkMatch(t, c1.Content, "(64 and sunny|64 degrees)")
	assert.Contains(t, resp.Choices[0].GenerationInfo, "output_tokens")
	assert.NotZero(t, resp.Choices[0].GenerationInfo["output_tokens"])
}

func testMaxTokensSetting(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	parts := []llms.ContentPart{
		llms.TextPart("I'm a pomeranian"),
		llms.TextPart("Describe my taxonomy, health and care"),
	}
	content := []llms.Message{
		{
			Role:  llms.RoleHuman,
			Parts: parts,
		},
	}

	// First, try this with a very low MaxTokens setting for such a query; expect
	// a stop reason that max of tokens was reached.
	{
		rsp, err := llm.GenerateContent(context.Background(), content,
			llms.WithMaxTokens(24))
		require.NoError(t, err)

		assert.NotEmpty(t, rsp.Choices)
		c1 := rsp.Choices[0]
		// TODO: Google genai models are returning "FinishReasonStop" instead of "MaxTokens".
		assert.Regexp(t, "(?i)(MaxTokens|FinishReasonStop)", c1.StopReason)
	}

	// Now, try it again with a much larger MaxTokens setting and expect to
	// finish successfully and generate a response.
	{
		rsp, err := llm.GenerateContent(context.Background(), content,
			llms.WithMaxTokens(2048))
		require.NoError(t, err)

		assert.NotEmpty(t, rsp.Choices)
		c1 := rsp.Choices[0]
		checkMatch(t, c1.StopReason, "stop")
		checkMatch(t, c1.Content, "(dog|breed|canid|canine)")
	}
}

func testWithHTTPClient(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	resp, err := llm.GenerateContent(
		context.TODO(),
		[]llms.Message{llms.MessageFromTextParts(llms.RoleHuman, "testing")},
	)
	require.NoError(t, err)
	require.EqualValues(t, "test-ok", resp.Choices[0].Content)
}

func getHTTPTestClientOptions() []googleai.Option {
	client := &http.Client{Transport: &testRequestInterceptor{}}
	return []googleai.Option{googleai.WithRest(), googleai.WithHTTPClient(client)}
}

type testRequestInterceptor struct{}

func (i *testRequestInterceptor) RoundTrip(req *http.Request) (*http.Response, error) {
	defer req.Body.Close()
	content := `{
					"candidates": [{
						"content": {
							"parts": [{"text": "test-ok"}]
						},
						"finishReason": "STOP"
					}],
					"usageMetadata": {
						"promptTokenCount": 7,
						"candidatesTokenCount": 7,
						"totalTokenCount": 14
					}
				}`

	resp := &http.Response{
		StatusCode: http.StatusOK, Request: req,
		Body:   io.NopCloser(bytes.NewBufferString(content)),
		Header: http.Header{},
	}
	resp.Header.Set("Content-Type", "application/json")
	return resp, nil
}

func showJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(b)
}

// checkMatch is a testing helper that checks `got` for regexp matches vs.
// `wants`. Each of `wants` has to match.
func checkMatch(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		re, err := regexp.Compile("(?i:" + want + ")")
		if err != nil {
			t.Fatal(err)
		}
		if !re.MatchString(got) {
			t.Errorf("\ngot %q\nwanted to match %q", got, want)
		}
	}
}

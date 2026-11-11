package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/mcp/internal/protocol"
	"github.com/effective-security/gogentic/mcp/internal/testingutils"
	"github.com/effective-security/gogentic/mcp/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerListChangedNotifications(t *testing.T) {
	mockTransport := testingutils.NewMockTransport()
	server := NewServer(mockTransport)
	err := server.Serve()
	require.NoError(t, err)

	// Test tool registration notification
	type TestToolArgs struct {
		Message string `json:"message" jsonschema:"required,description=A test message"`
	}
	err = server.RegisterTool("test-tool", "Test tool", func(args TestToolArgs) (*ToolResponse, error) {
		return NewToolResponse(), nil
	})
	require.NoError(t, err)

	messages := mockTransport.GetMessages()
	require.Len(t, messages, 1)
	assert.Equal(t, "notifications/tools/list_changed", messages[0].JsonRpcNotification.Method)

	// Test tool deregistration notification
	mockTransport = testingutils.NewMockTransport()
	server = NewServer(mockTransport)
	err = server.Serve()
	require.NoError(t, err)
	err = server.RegisterTool("test-tool", "Test tool", func(args TestToolArgs) (*ToolResponse, error) {
		return NewToolResponse(), nil
	})
	require.NoError(t, err)
	err = server.DeregisterTool("test-tool")
	require.NoError(t, err)
	messages = mockTransport.GetMessages()
	require.Len(t, messages, 2, "Expected 2 messages after tool registration and deregistration")
	assert.Equal(t, "notifications/tools/list_changed", messages[1].JsonRpcNotification.Method)

	// Test prompt registration notification
	type TestPromptArgs struct {
		Query string `json:"query" jsonschema:"required,description=A test query"`
	}
	mockTransport = testingutils.NewMockTransport()
	server = NewServer(mockTransport)
	err = server.Serve()
	require.NoError(t, err)
	err = server.RegisterPrompt("test-prompt", "Test prompt", func(ctx context.Context, args TestPromptArgs) (*PromptResponse, error) {
		return NewPromptResponse("test", NewPromptMessage(NewTextContent("test"), RoleUser)), nil
	})
	require.NoError(t, err)
	messages = mockTransport.GetMessages()
	require.Len(t, messages, 1, "Expected 1 message after prompt registration")
	assert.Equal(t, "notifications/prompts/list_changed", messages[0].JsonRpcNotification.Method)

	// Test prompt deregistration notification
	mockTransport = testingutils.NewMockTransport()
	server = NewServer(mockTransport)
	err = server.Serve()
	require.NoError(t, err)
	err = server.RegisterPrompt("test-prompt", "Test prompt", func(ctx context.Context, args TestPromptArgs) (*PromptResponse, error) {
		return NewPromptResponse("test", NewPromptMessage(NewTextContent("test"), RoleUser)), nil
	})
	require.NoError(t, err)
	err = server.DeregisterPrompt("test-prompt")
	require.NoError(t, err)
	messages = mockTransport.GetMessages()
	require.Len(t, messages, 2, "Expected 2 messages after prompt registration and deregistration")
	assert.Equal(t, "notifications/prompts/list_changed", messages[1].JsonRpcNotification.Method)

	// Test resource registration notification
	mockTransport = testingutils.NewMockTransport()
	server = NewServer(mockTransport)
	err = server.Serve()
	require.NoError(t, err)
	err = server.RegisterResource("test://resource", "test-resource", "Test resource", "text/plain", func() (*ResourceResponse, error) {
		return NewResourceResponse(NewTextEmbeddedResource("test://resource", "test content", "text/plain")), nil
	})
	require.NoError(t, err)
	messages = mockTransport.GetMessages()
	require.Len(t, messages, 1, "Expected 1 message after resource registration")
	assert.Equal(t, "notifications/resources/list_changed", messages[0].JsonRpcNotification.Method)

	// Test resource deregistration notification
	mockTransport = testingutils.NewMockTransport()
	server = NewServer(mockTransport)
	err = server.Serve()
	require.NoError(t, err)
	err = server.RegisterResource("test://resource", "test-resource", "Test resource", "text/plain", func() (*ResourceResponse, error) {
		return NewResourceResponse(NewTextEmbeddedResource("test://resource", "test content", "text/plain")), nil
	})
	require.NoError(t, err)
	err = server.DeregisterResource("test://resource")
	require.NoError(t, err)
	messages = mockTransport.GetMessages()
	require.Len(t, messages, 2, "Expected 2 messages after resource registration and deregistration")
	assert.Equal(t, "notifications/resources/list_changed", messages[1].JsonRpcNotification.Method)
}

func TestHandleListToolCall(t *testing.T) {
	mockTransport := testingutils.NewMockTransport()
	server := NewServer(mockTransport)
	err := server.Serve()
	require.NoError(t, err)

	type testToolArgs struct {
		Message string `json:"message" jsonschema:"required,description=A test message"`
	}

	// Register a tool
	err = server.RegisterTool("test-tool", "Test tool", func(args testToolArgs) (*ToolResponse, error) {
		c1 := &Content{
			Type: ContentTypeText,
			TextContent: &TextContent{
				Text: "test",
			},
		}
		return NewToolResponse(c1), nil
	})
	require.NoError(t, err)

	// Test tool call
	resp, err := server.handleToolCalls(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{"name":"invalid"}`),
	}, protocol.RequestHandlerExtra{})
	assert.EqualError(t, err, "unknown tool: invalid")

	resp, err = server.handleToolCalls(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{"name":"test-tool"}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	toolErr, ok := resp.(*toolResponseSent)
	require.True(t, ok, "Expected ToolResponse")
	assert.NoError(t, toolErr.Error)

	resp, err = server.handleToolCalls(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{"name":"test-tool", "arguments":{}}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	toolResp, ok := resp.(*toolResponseSent)
	require.True(t, ok, "Expected ToolResponse")
	require.NoError(t, toolResp.Error)

	resp, err = server.handleToolCalls(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{"name":"test-tool", "arguments":{invalid json}}`),
	}, protocol.RequestHandlerExtra{})
	assert.EqualError(t, err, "failed to unmarshal arguments: invalid character 'i' looking for beginning of object key string")
}

const (
	listChangedTools     = "notifications/tools/list_changed"
	listChangedPrompts   = "notifications/prompts/list_changed"
	listChangedResources = "notifications/resources/list_changed"
	templateURI          = "file:///{path}"
	missingResourceURI   = "file:///docs/missing.txt"
	panicResourceURI     = "file:///docs/panic.txt"
	emptyResourceURI     = "file:///docs/empty.txt"
)

type ctxKey struct{}

// startFailTransport is a mock transport whose Start always fails.
type startFailTransport struct {
	*testingutils.MockTransport
	err error
}

func (s *startFailTransport) Start(context.Context) error {
	return s.err
}

func newServedServer(t *testing.T, opts ...ServerOptions) (*Server, *testingutils.MockTransport) {
	t.Helper()
	tr := testingutils.NewMockTransport()
	srv := NewServer(tr, opts...)
	require.NoError(t, srv.Serve())
	return srv, tr
}

func TestServerOptions(t *testing.T) {
	p := protocol.NewProtocol(nil)
	srv := NewServer(testingutils.NewMockTransport(),
		WithName(e2eServerName),
		WithVersion(e2eServerVersion),
		WithInstructions(e2eInstructions),
		WithPaginationLimit(7),
		WithProtocol(p),
	)
	assert.Equal(t, e2eServerName, srv.serverName)
	assert.Equal(t, e2eServerVersion, srv.serverVersion)
	require.NotNil(t, srv.serverInstructions)
	assert.Equal(t, e2eInstructions, *srv.serverInstructions)
	require.NotNil(t, srv.paginationLimit)
	assert.Equal(t, 7, *srv.paginationLimit)
	assert.Same(t, p, srv.protocol)

	def := NewServer(testingutils.NewMockTransport())
	assert.Empty(t, def.serverName)
	assert.Empty(t, def.serverVersion)
	assert.Nil(t, def.serverInstructions)
	assert.Nil(t, def.paginationLimit)
	assert.NotNil(t, def.protocol)
}

func TestServe_Errors(t *testing.T) {
	t.Run("already running", func(t *testing.T) {
		srv, _ := newServedServer(t)
		assert.EqualError(t, srv.Serve(), "server is already running")
	})

	t.Run("transport start fails", func(t *testing.T) {
		tr := &startFailTransport{
			MockTransport: testingutils.NewMockTransport(),
			err:           errors.New(errBoom),
		}
		srv := NewServer(tr)
		assert.EqualError(t, srv.Serve(), errBoom)
		assert.False(t, srv.isRunning)

		// Registration on a server that is not running must not notify.
		require.NoError(t, srv.RegisterTool(echoTool, echoToolDesc, func(args echoArgs) (*ToolResponse, error) {
			return NewToolResponse(), nil
		}))
		assert.Empty(t, tr.GetMessages())
	})
}

func TestServerRegistry(t *testing.T) {
	srv := NewServer(testingutils.NewMockTransport())

	require.NoError(t, srv.RegisterTool(echoTool, echoToolDesc, func(args echoArgs) (*ToolResponse, error) {
		return NewToolResponse(), nil
	}))
	require.NoError(t, srv.RegisterPrompt(greetPrompt, greetPromptDesc, func(args greetArgs) (*PromptResponse, error) {
		return NewPromptResponse("d"), nil
	}))
	require.NoError(t, srv.RegisterResource(textResourceURI, "readme", "Readme", mimeTextPlain, func() (*ResourceResponse, error) {
		return NewResourceResponse(), nil
	}))
	require.NoError(t, srv.RegisterResourceTemplate(templateURI, "files", "Files", mimeTextPlain))

	assert.True(t, srv.CheckToolRegistered(echoTool))
	assert.False(t, srv.CheckToolRegistered(weatherTool))
	assert.True(t, srv.CheckPromptRegistered(greetPrompt))
	assert.False(t, srv.CheckPromptRegistered(failPrompt))
	assert.True(t, srv.CheckResourceRegistered(textResourceURI))
	assert.False(t, srv.CheckResourceRegistered(blobResourceURI))
	assert.True(t, srv.CheckResourceTemplateRegistered(templateURI))
	assert.False(t, srv.CheckResourceTemplateRegistered("other://{x}"))

	require.NoError(t, srv.DeregisterTool(echoTool))
	require.NoError(t, srv.DeregisterPrompt(greetPrompt))
	require.NoError(t, srv.DeregisterResource(textResourceURI))
	require.NoError(t, srv.DeregisterResourceTemplate(templateURI))

	assert.False(t, srv.CheckToolRegistered(echoTool))
	assert.False(t, srv.CheckPromptRegistered(greetPrompt))
	assert.False(t, srv.CheckResourceRegistered(textResourceURI))
	assert.False(t, srv.CheckResourceTemplateRegistered(templateURI))
}

func TestResourceTemplateNotifications(t *testing.T) {
	srv, tr := newServedServer(t)
	require.NoError(t, srv.RegisterResourceTemplate(templateURI, "files", "Files", mimeTextPlain))
	require.NoError(t, srv.DeregisterResourceTemplate(templateURI))

	msgs := tr.GetMessages()
	require.Len(t, msgs, 2)
	for _, m := range msgs {
		require.Equal(t, transport.BaseMessageTypeJSONRPCNotificationType, m.Type)
		assert.Equal(t, listChangedResources, m.JsonRpcNotification.Method)
	}
}

func TestHandleInitializeAndPing(t *testing.T) {
	srv, _ := newServedServer(t,
		WithName(e2eServerName),
		WithVersion(e2eServerVersion),
	)
	resp, err := srv.handleInitialize(context.Background(), &transport.BaseJSONRPCRequest{}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	initResp, ok := resp.(InitializeResponse)
	require.True(t, ok)
	assert.Equal(t, e2eProtocolVer, initResp.ProtocolVersion)
	assert.Equal(t, implementation{Name: e2eServerName, Version: e2eServerVersion}, initResp.ServerInfo)
	assert.Nil(t, initResp.Instructions)
	assert.Nil(t, initResp.Meta)

	b, err := json.Marshal(initResp.Capabilities)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"tools":{"listChanged":false},
		"prompts":{"listChanged":false},
		"resources":{"listChanged":false}
	}`, string(b))

	pong, err := srv.handlePing(context.Background(), &transport.BaseJSONRPCRequest{}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{}, pong)
}

func TestResponseSent_MarshalJSON(t *testing.T) {
	t.Run("tool success", func(t *testing.T) {
		b, err := json.Marshal(newToolResponseSent(NewToolResponse(NewTextContent("ok")).
			WithStructuredContent(map[string]any{
				"a": 1,
			}).
			WithMeta(map[string]any{
				"k": "v",
			})))
		require.NoError(t, err)
		assert.JSONEq(t, `{
			"_meta":{"k":"v"},
			"content":[{"type":"text","text":"ok"}],
			"structuredContent":{"a":1}
		}`, string(b))
	})

	t.Run("tool error", func(t *testing.T) {
		b, err := json.Marshal(newToolResponseSentError(errors.New(errBoom)))
		require.NoError(t, err)
		assert.JSONEq(t, `{"content":[{"type":"text","text":"boom"}],"isError":true}`, string(b))
	})

	t.Run("prompt success", func(t *testing.T) {
		b, err := json.Marshal(newPromptResponseSent(NewPromptResponse("d", NewPromptMessage(NewTextContent("m"), RoleAssistant))))
		require.NoError(t, err)
		assert.JSONEq(t, `{"description":"d","messages":[{"role":"assistant","content":{"type":"text","text":"m"}}]}`, string(b))
	})

	t.Run("prompt error", func(t *testing.T) {
		b, err := json.Marshal(newPromptResponseSentError(errors.New(errBoom)))
		require.NoError(t, err)
		assert.JSONEq(t, `{"description":"error","messages":[{"role":"user","content":{"type":"text","text":"boom"}}]}`, string(b))
	})

	t.Run("resource success", func(t *testing.T) {
		b, err := json.Marshal(newResourceResponseSent(NewResourceResponse(NewBlobEmbeddedResource("u", "b", mimeImagePNG))))
		require.NoError(t, err)
		assert.JSONEq(t, `{"contents":[{"uri":"u","blob":"b","mimeType":"image/png"}]}`, string(b))
	})

	t.Run("resource error", func(t *testing.T) {
		b, err := json.Marshal(&resourceResponseSent{
			Uri:   "u",
			Error: errors.New(errBoom),
		})
		require.NoError(t, err)
		assert.JSONEq(t, `{"contents":[{"uri":"u","text":"boom","mimeType":"text/plain"}]}`, string(b))
	})
}

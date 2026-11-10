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

func TestHandleListToolsPagination(t *testing.T) {
	mockTransport := testingutils.NewMockTransport()
	server := NewServer(mockTransport)
	err := server.Serve()
	require.NoError(t, err)

	// Register tools in a non alphabetical order
	toolNames := []string{"b-tool", "a-tool", "c-tool", "e-tool", "d-tool"}
	type testToolArgs struct {
		Message string `json:"message" jsonschema:"required,description=A test message"`
	}
	for _, name := range toolNames {
		err = server.RegisterTool(name, "Test tool "+name, func(args testToolArgs) (*ToolResponse, error) {
			return NewToolResponse(), nil
		})
		require.NoError(t, err)
	}

	// Set pagination limit to 2 items per page
	limit := 2
	server.paginationLimit = &limit

	// Test first page (no cursor)
	resp, err := server.handleListTools(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	toolsResp, ok := resp.(ToolsResponse)
	require.True(t, ok, "Expected tools.ToolsResponse")

	// Verify first page
	require.Len(t, toolsResp.Tools, 2, "Expected 2 tools on first page")
	assert.Equal(t, "a-tool", toolsResp.Tools[0].Name)
	assert.Equal(t, "b-tool", toolsResp.Tools[1].Name)
	require.NotNil(t, toolsResp.NextCursor, "Expected next cursor for first page")

	// Test second page
	resp, err = server.handleListTools(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{"cursor":"` + *toolsResp.NextCursor + `"}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	toolsResp, ok = resp.(ToolsResponse)
	require.True(t, ok, "Expected tools.ToolsResponse")

	// Verify second page
	require.Len(t, toolsResp.Tools, 2, "Expected 2 tools on second page")
	assert.Equal(t, "c-tool", toolsResp.Tools[0].Name)
	assert.Equal(t, "d-tool", toolsResp.Tools[1].Name)
	require.NotNil(t, toolsResp.NextCursor, "Expected next cursor for second page")

	// Test last page
	resp, err = server.handleListTools(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{"cursor":"` + *toolsResp.NextCursor + `"}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	toolsResp, ok = resp.(ToolsResponse)
	require.True(t, ok, "Expected tools.ToolsResponse")

	// Verify last page
	require.Len(t, toolsResp.Tools, 1, "Expected 1 tool on last page")
	assert.Equal(t, "e-tool", toolsResp.Tools[0].Name)
	assert.Nil(t, toolsResp.NextCursor, "Expected no next cursor for last page")

	// Test invalid cursor
	_, err = server.handleListTools(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{"cursor":"invalid-cursor"}`),
	}, protocol.RequestHandlerExtra{})
	assert.Error(t, err, "Expected error for invalid cursor")

	// Test without pagination (should return all tools)
	server.paginationLimit = nil
	resp, err = server.handleListTools(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	toolsResp, ok = resp.(ToolsResponse)
	require.True(t, ok, "Expected ToolsResponse")

	assert.Len(t, toolsResp.Tools, 5, "Expected 5 tools without pagination")
	assert.Nil(t, toolsResp.NextCursor, "Expected no next cursor when pagination is disabled")
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

func TestHandleToolCallRecoversFromPanic(t *testing.T) {
	mockTransport := testingutils.NewMockTransport()
	server := NewServer(mockTransport)
	err := server.Serve()
	require.NoError(t, err)

	type args struct {
		Message string `json:"message" jsonschema:"required"`
	}

	err = server.RegisterTool("panic-tool", "Tool that panics", func(args args) (*ToolResponse, error) {
		panic("tool exploded")
	})
	require.NoError(t, err)

	resp, err := server.handleToolCalls(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{"name":"panic-tool","arguments":{"message":"boom"}}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	toolResp, ok := resp.(*toolResponseSent)
	require.True(t, ok, "Expected ToolResponse")
	require.Error(t, toolResp.Error)
	assert.Contains(t, toolResp.Error.Error(), "internal error")
}

func TestHandleListPromptsPagination(t *testing.T) {
	mockTransport := testingutils.NewMockTransport()
	server := NewServer(mockTransport)
	err := server.Serve()
	require.NoError(t, err)

	// Register prompts in a non alphabetical order
	promptNames := []string{"b-prompt", "a-prompt", "c-prompt", "e-prompt", "d-prompt"}
	type testPromptArgs struct {
		Message string `json:"message" jsonschema:"required,description=A test message"`
	}
	for _, name := range promptNames {
		err = server.RegisterPrompt(name, "Test prompt "+name, func(args testPromptArgs) (*PromptResponse, error) {
			return NewPromptResponse("test", NewPromptMessage(NewTextContent("test"), RoleUser)), nil
		})
		require.NoError(t, err)
	}

	// Set pagination limit to 2 items per page
	limit := 2
	server.paginationLimit = &limit

	// Test first page (no cursor)
	resp, err := server.handleListPrompts(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	promptsResp, ok := resp.(ListPromptsResponse)
	require.True(t, ok, "Expected listPromptsResult")

	// Verify first page
	require.Len(t, promptsResp.Prompts, 2, "Expected 2 prompts on first page")
	assert.Equal(t, "a-prompt", promptsResp.Prompts[0].Name)
	assert.Equal(t, "b-prompt", promptsResp.Prompts[1].Name)
	require.NotNil(t, promptsResp.NextCursor, "Expected next cursor for first page")

	// Test second page
	resp, err = server.handleListPrompts(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{"cursor":"` + *promptsResp.NextCursor + `"}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	promptsResp, ok = resp.(ListPromptsResponse)
	require.True(t, ok, "Expected listPromptsResult")

	// Verify second page
	require.Len(t, promptsResp.Prompts, 2, "Expected 2 prompts on second page")
	assert.Equal(t, "c-prompt", promptsResp.Prompts[0].Name)
	assert.Equal(t, "d-prompt", promptsResp.Prompts[1].Name)
	require.NotNil(t, promptsResp.NextCursor, "Expected next cursor for second page")

	// Test last page
	resp, err = server.handleListPrompts(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{"cursor":"` + *promptsResp.NextCursor + `"}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	promptsResp, ok = resp.(ListPromptsResponse)
	require.True(t, ok, "Expected listPromptsResult")

	// Verify last page
	require.Len(t, promptsResp.Prompts, 1, "Expected 1 prompt on last page")
	assert.Equal(t, "e-prompt", promptsResp.Prompts[0].Name)
	assert.Nil(t, promptsResp.NextCursor, "Expected no next cursor for last page")

	// Test invalid cursor
	_, err = server.handleListPrompts(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{"cursor":"invalid-cursor"}`),
	}, protocol.RequestHandlerExtra{})
	assert.Error(t, err, "Expected error for invalid cursor")

	// Test without pagination (should return all prompts)
	server.paginationLimit = nil
	resp, err = server.handleListPrompts(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	promptsResp, ok = resp.(ListPromptsResponse)
	require.True(t, ok, "Expected listPromptsResult")

	assert.Len(t, promptsResp.Prompts, 5, "Expected 5 prompts without pagination")
	assert.Nil(t, promptsResp.NextCursor, "Expected no next cursor when pagination is disabled")
}

func TestHandleListResourcesNoParams(t *testing.T) {
	mockTransport := testingutils.NewMockTransport()
	server := NewServer(mockTransport)
	err := server.Serve()
	require.NoError(t, err)

	// Register resources
	resourceURIs := []string{"b://resource", "a://resource"}
	for _, uri := range resourceURIs {
		err = server.RegisterResource(uri, "resource-"+uri, "Test resource "+uri, "text/plain", func() (*ResourceResponse, error) {
			return NewResourceResponse(NewTextEmbeddedResource(uri, "test content", "text/plain")), nil
		})
		require.NoError(t, err)
	}

	// Test with no Params defined
	resp, err := server.handleListResources(context.Background(), &transport.BaseJSONRPCRequest{}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	resourcesResp, ok := resp.(ListResourcesResponse)
	require.True(t, ok, "Expected ListResourcesResponse")

	// Verify empty resources list
	assert.Len(t, resourcesResp.Resources, len(resourceURIs), "Unexpected resources count")
}

func TestHandleListResourcesPagination(t *testing.T) {
	mockTransport := testingutils.NewMockTransport()
	server := NewServer(mockTransport)
	err := server.Serve()
	require.NoError(t, err)

	// Register resources in a non alphabetical order
	resourceURIs := []string{"b://resource", "a://resource", "c://resource", "e://resource", "d://resource"}
	for _, uri := range resourceURIs {
		err = server.RegisterResource(uri, "resource-"+uri, "Test resource "+uri, "text/plain", func() (*ResourceResponse, error) {
			return NewResourceResponse(NewTextEmbeddedResource(uri, "test content", "text/plain")), nil
		})
		require.NoError(t, err)
	}

	// Set pagination limit to 2 items per page
	limit := 2
	server.paginationLimit = &limit

	// Test first page (no cursor)
	resp, err := server.handleListResources(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	resourcesResp, ok := resp.(ListResourcesResponse)
	require.True(t, ok, "Expected listResourcesResult")

	// Verify first page
	require.Len(t, resourcesResp.Resources, 2, "Expected 2 resources on first page")
	assert.Equal(t, "a://resource", resourcesResp.Resources[0].Uri)
	assert.Equal(t, "b://resource", resourcesResp.Resources[1].Uri)
	require.NotNil(t, resourcesResp.NextCursor, "Expected next cursor for first page")

	// Test second page
	resp, err = server.handleListResources(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{"cursor":"` + *resourcesResp.NextCursor + `"}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	resourcesResp, ok = resp.(ListResourcesResponse)
	require.True(t, ok, "Expected listResourcesResult")

	// Verify second page
	require.Len(t, resourcesResp.Resources, 2, "Expected 2 resources on second page")
	assert.Equal(t, "c://resource", resourcesResp.Resources[0].Uri)
	assert.Equal(t, "d://resource", resourcesResp.Resources[1].Uri)
	require.NotNil(t, resourcesResp.NextCursor, "Expected next cursor for second page")

	// Test last page
	resp, err = server.handleListResources(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{"cursor":"` + *resourcesResp.NextCursor + `"}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	resourcesResp, ok = resp.(ListResourcesResponse)
	require.True(t, ok, "Expected listResourcesResult")

	// Verify last page
	require.Len(t, resourcesResp.Resources, 1, "Expected 1 resource on last page")
	assert.Equal(t, "e://resource", resourcesResp.Resources[0].Uri)
	assert.Nil(t, resourcesResp.NextCursor, "Expected no next cursor for last page")

	// Test invalid cursor
	_, err = server.handleListResources(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{"cursor":"invalid-cursor"}`),
	}, protocol.RequestHandlerExtra{})
	assert.Error(t, err, "Expected error for invalid cursor")

	// Test without pagination (should return all resources)
	server.paginationLimit = nil
	resp, err = server.handleListResources(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	resourcesResp, ok = resp.(ListResourcesResponse)
	require.True(t, ok, "Expected listResourcesResult")

	assert.Len(t, resourcesResp.Resources, 5, "Expected 5 resources without pagination")
	assert.Nil(t, resourcesResp.NextCursor, "Expected no next cursor when pagination is disabled")
}

func TestHandleListResourceTemplatesPagination(t *testing.T) {
	mockTransport := testingutils.NewMockTransport()
	server := NewServer(mockTransport)
	err := server.Serve()
	require.NoError(t, err)

	// Register templates in a non alphabetical order
	templateURIs := []string{
		"b://{param}/resource",
		"a://{param}/resource",
		"c://{param}/resource",
		"e://{param}/resource",
		"d://{param}/resource",
	}
	for _, uri := range templateURIs {
		err = server.RegisterResourceTemplate(
			uri,
			"template-"+uri,
			"Test template "+uri,
			"text/plain",
		)
		require.NoError(t, err)
	}

	// Set pagination limit to 2 items per page
	limit := 2
	server.paginationLimit = &limit

	// Test first page (no cursor)
	resp, err := server.handleListResourceTemplates(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	templatesResp, ok := resp.(ListResourceTemplatesResponse)
	require.True(t, ok, "Expected ListResourceTemplatesResponse")

	// Verify first page
	require.Len(t, templatesResp.Templates, 2, "Expected 2 templates on first page")
	assert.Equal(t, "a://{param}/resource", templatesResp.Templates[0].UriTemplate)
	assert.Equal(t, "b://{param}/resource", templatesResp.Templates[1].UriTemplate)
	require.NotNil(t, templatesResp.NextCursor, "Expected next cursor for first page")

	// Test second page
	resp, err = server.handleListResourceTemplates(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{"cursor":"` + *templatesResp.NextCursor + `"}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	templatesResp, ok = resp.(ListResourceTemplatesResponse)
	require.True(t, ok, "Expected ListResourceTemplatesResponse")

	// Verify second page
	require.Len(t, templatesResp.Templates, 2, "Expected 2 templates on second page")
	assert.Equal(t, "c://{param}/resource", templatesResp.Templates[0].UriTemplate)
	assert.Equal(t, "d://{param}/resource", templatesResp.Templates[1].UriTemplate)
	require.NotNil(t, templatesResp.NextCursor, "Expected next cursor for second page")

	// Test last page
	resp, err = server.handleListResourceTemplates(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{"cursor":"` + *templatesResp.NextCursor + `"}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	templatesResp, ok = resp.(ListResourceTemplatesResponse)
	require.True(t, ok, "Expected ListResourceTemplatesResponse")

	// Verify last page
	require.Len(t, templatesResp.Templates, 1, "Expected 1 template on last page")
	assert.Equal(t, "e://{param}/resource", templatesResp.Templates[0].UriTemplate)
	assert.Nil(t, templatesResp.NextCursor, "Expected no next cursor for last page")

	// Test invalid cursor
	_, err = server.handleListResourceTemplates(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{"cursor":"invalid-cursor"}`),
	}, protocol.RequestHandlerExtra{})
	assert.Error(t, err, "Expected error for invalid cursor")

	// Test without pagination (should return all templates)
	server.paginationLimit = nil
	resp, err = server.handleListResourceTemplates(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)

	templatesResp, ok = resp.(ListResourceTemplatesResponse)
	require.True(t, ok, "Expected ListResourceTemplatesResponse")

	assert.Len(t, templatesResp.Templates, 5, "Expected 5 templates without pagination")
	assert.Nil(t, templatesResp.NextCursor, "Expected no next cursor when pagination is disabled")
}

const (
	listChangedTools     = "notifications/tools/list_changed"
	listChangedPrompts   = "notifications/prompts/list_changed"
	listChangedResources = "notifications/resources/list_changed"
	templateURI          = "file:///{path}"
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

func TestRegisterTool_Validation(t *testing.T) {
	tcs := []struct {
		name    string
		handler any
		wantErr string
	}{
		{
			name:    "no arguments",
			handler: func() (*ToolResponse, error) { return nil, nil },
			wantErr: "handler must take exactly one or two arguments, got 0",
		},
		{
			name:    "three arguments",
			handler: func(context.Context, echoArgs, int) (*ToolResponse, error) { return nil, nil },
			wantErr: "handler must take exactly one or two arguments, got 3",
		},
		{
			name:    "single return value",
			handler: func(echoArgs) *ToolResponse { return nil },
			wantErr: "handler must return exactly two values, got 1",
		},
		{
			name:    "first of two arguments is not a context",
			handler: func(string, echoArgs) (*ToolResponse, error) { return nil, nil },
			wantErr: "when a handler has 2 arguments, handler must take context.Context as the first argument, got string",
		},
		{
			name:    "wrong response type",
			handler: func(echoArgs) (string, error) { return "", nil },
			wantErr: "handler must return *tools.ToolResponse, got string",
		},
		{
			name:    "wrong error type",
			handler: func(echoArgs) (*ToolResponse, string) { return nil, "" },
			wantErr: "handler must return error, got string",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			srv, tr := newServedServer(t)
			assert.EqualError(t, srv.RegisterTool(echoTool, echoToolDesc, tc.handler), tc.wantErr)
			assert.False(t, srv.CheckToolRegistered(echoTool))
			assert.Empty(t, tr.GetMessages(), "a rejected tool must not trigger a notification")
		})
	}
}

func TestRegisterResource_Validation(t *testing.T) {
	tcs := []struct {
		name    string
		handler any
		wantErr string
	}{
		{
			name:    "two arguments",
			handler: func(context.Context, string) (*ResourceResponse, error) { return nil, nil },
			wantErr: "handler must take no or one arguments, got 2",
		},
		{
			name:    "argument is not a context",
			handler: func(string) (*ResourceResponse, error) { return nil, nil },
			wantErr: "when a handler has 1 argument, it must be context.Context, got string",
		},
		{
			name:    "single return value",
			handler: func() *ResourceResponse { return nil },
			wantErr: "handler must return exactly two values, got 1",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			srv, tr := newServedServer(t)
			assert.EqualError(t, srv.RegisterResource(textResourceURI, "n", "d", mimeTextPlain, tc.handler), tc.wantErr)
			assert.False(t, srv.CheckResourceRegistered(textResourceURI))
			assert.Empty(t, tr.GetMessages())
		})
	}
}

func TestRegisterPrompt_Validation(t *testing.T) {
	type intArgs struct {
		Count int `json:"count"`
	}
	tcs := []struct {
		name    string
		handler any
		wantErr string
	}{
		{
			name:    "no arguments",
			handler: func() (*PromptResponse, error) { return nil, nil },
			wantErr: "handler must take one or two arguments, got 0",
		},
		{
			name:    "first of two arguments is not a context",
			handler: func(string, greetArgs) (*PromptResponse, error) { return nil, nil },
			wantErr: "when a handler has 2 arguments, the first argument must be context.Context, got string",
		},
		{
			name:    "argument is not a struct",
			handler: func(string) (*PromptResponse, error) { return nil, nil },
			wantErr: "argument must be a struct",
		},
		{
			name:    "non string field",
			handler: func(intArgs) (*PromptResponse, error) { return nil, nil },
			wantErr: "all fields of the struct must be of type string or *string, found int",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			srv, tr := newServedServer(t)
			assert.EqualError(t, srv.RegisterPrompt(greetPrompt, greetPromptDesc, tc.handler), tc.wantErr)
			assert.False(t, srv.CheckPromptRegistered(greetPrompt))
			assert.Empty(t, tr.GetMessages())
		})
	}
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

func TestHandleList_ParamErrors(t *testing.T) {
	srv, _ := newServedServer(t)
	badParams := &transport.BaseJSONRPCRequest{
		Params: []byte(`{bad`),
	}
	const wantErr = "failed to unmarshal arguments: invalid character 'b' looking for beginning of object key string"

	_, err := srv.handleListTools(context.Background(), badParams, protocol.RequestHandlerExtra{})
	assert.EqualError(t, err, wantErr)
	_, err = srv.handleListPrompts(context.Background(), badParams, protocol.RequestHandlerExtra{})
	assert.EqualError(t, err, wantErr)
	_, err = srv.handleListResources(context.Background(), badParams, protocol.RequestHandlerExtra{})
	assert.EqualError(t, err, wantErr)
	_, err = srv.handleListResourceTemplates(context.Background(), badParams, protocol.RequestHandlerExtra{})
	assert.EqualError(t, err, wantErr)
	_, err = srv.handleToolCalls(context.Background(), badParams, protocol.RequestHandlerExtra{})
	assert.EqualError(t, err, wantErr)
	_, err = srv.handlePromptCalls(context.Background(), badParams, protocol.RequestHandlerExtra{})
	assert.EqualError(t, err, wantErr)
	_, err = srv.handleResourceCalls(context.Background(), badParams, protocol.RequestHandlerExtra{})
	assert.EqualError(t, err, wantErr)
}

func TestHandleList_NoParams(t *testing.T) {
	srv, _ := newServedServer(t)
	require.NoError(t, srv.RegisterTool(echoTool, echoToolDesc, func(args echoArgs) (*ToolResponse, error) {
		return NewToolResponse(), nil
	}))
	require.NoError(t, srv.RegisterResourceTemplate(templateURI, "files", "Files", mimeTextPlain))
	noParams := &transport.BaseJSONRPCRequest{}

	toolsResp, err := srv.handleListTools(context.Background(), noParams, protocol.RequestHandlerExtra{})
	require.NoError(t, err)
	tools, ok := toolsResp.(ToolsResponse)
	require.True(t, ok)
	require.Len(t, tools.Tools, 1)
	assert.Equal(t, echoTool, tools.Tools[0].Name)
	assert.Nil(t, tools.NextCursor)

	tplResp, err := srv.handleListResourceTemplates(context.Background(), noParams, protocol.RequestHandlerExtra{})
	require.NoError(t, err)
	tpls, ok := tplResp.(ListResourceTemplatesResponse)
	require.True(t, ok)
	require.Len(t, tpls.Templates, 1)
	tpl := tpls.Templates[0]
	assert.Equal(t, templateURI, tpl.UriTemplate)
	assert.Equal(t, "files", tpl.Name)
	require.NotNil(t, tpl.Description)
	assert.Equal(t, "Files", *tpl.Description)
	require.NotNil(t, tpl.MimeType)
	assert.Equal(t, mimeTextPlain, *tpl.MimeType)
	assert.Nil(t, tpls.NextCursor)

	b, err := json.Marshal(tpls)
	require.NoError(t, err)
	assert.JSONEq(t, `{"resourceTemplates":[{"uriTemplate":"file:///{path}","name":"files","description":"Files","mimeType":"text/plain"}]}`, string(b))
}

func TestHandleListTools_CursorPastEnd(t *testing.T) {
	srv, _ := newServedServer(t, WithPaginationLimit(1))
	require.NoError(t, srv.RegisterTool(echoTool, echoToolDesc, func(args echoArgs) (*ToolResponse, error) {
		return NewToolResponse(), nil
	}))

	first, err := srv.handleListTools(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)
	firstPage := first.(ToolsResponse)
	require.Len(t, firstPage.Tools, 1)
	require.NotNil(t, firstPage.NextCursor, "a full page must carry a cursor")

	last, err := srv.handleListTools(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(`{"cursor":"` + *firstPage.NextCursor + `"}`),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)
	lastPage := last.(ToolsResponse)
	assert.Empty(t, lastPage.Tools)
	assert.NotNil(t, lastPage.Tools, "tools must encode as an empty array, not null")
	assert.Nil(t, lastPage.NextCursor)
}

func callTool(t *testing.T, srv *Server, ctx context.Context, params string) *toolResponseSent {
	t.Helper()
	resp, err := srv.handleToolCalls(ctx, &transport.BaseJSONRPCRequest{
		Params: []byte(params),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)
	sent, ok := resp.(*toolResponseSent)
	require.True(t, ok, "expected *toolResponseSent, got %T", resp)
	return sent
}

func TestHandleToolCalls_HandlerBehaviors(t *testing.T) {
	srv, _ := newServedServer(t)

	require.NoError(t, srv.RegisterTool("ctx-tool", "reads context", func(ctx context.Context, args echoArgs) (*ToolResponse, error) {
		v, _ := ctx.Value(ctxKey{}).(string)
		return NewToolResponse(NewTextContent(v + ":" + args.Message)), nil
	}))
	require.NoError(t, srv.RegisterTool(failingTool, failingToolDesc, func(args echoArgs) (*ToolResponse, error) {
		return nil, errors.New(errBoom)
	}))
	require.NoError(t, srv.RegisterTool("panic-err", "panics with error", func(args echoArgs) (*ToolResponse, error) {
		panic(errors.New("kaboom"))
	}))

	t.Run("context is forwarded", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), ctxKey{}, "v1")
		sent := callTool(t, srv, ctx, `{"name":"ctx-tool","arguments":{"message":"hi"}}`)
		require.NoError(t, sent.Error)
		require.Len(t, sent.Response.Content, 1)
		assert.Equal(t, "v1:hi", sent.Response.Content[0].TextContent.Text)
	})

	t.Run("arguments of wrong shape", func(t *testing.T) {
		sent := callTool(t, srv, context.Background(), `{"name":"ctx-tool","arguments":"not-an-object"}`)
		assert.Nil(t, sent.Response)
		assert.EqualError(t, sent.Error, "failed to unmarshal arguments: json: cannot unmarshal string into Go value of type mcp.echoArgs")
	})

	t.Run("handler error", func(t *testing.T) {
		sent := callTool(t, srv, context.Background(), `{"name":"failing","arguments":{"message":"x"}}`)
		assert.Nil(t, sent.Response)
		assert.EqualError(t, sent.Error, "failed to handle tool call: boom")
	})

	t.Run("panic with error value", func(t *testing.T) {
		sent := callTool(t, srv, context.Background(), `{"name":"panic-err","arguments":{"message":"x"}}`)
		assert.Nil(t, sent.Response)
		assert.EqualError(t, sent.Error, "internal error: tool panicked: kaboom")
	})
}

func getPrompt(t *testing.T, srv *Server, params string) *promptResponseSent {
	t.Helper()
	resp, err := srv.handlePromptCalls(context.Background(), &transport.BaseJSONRPCRequest{
		Params: []byte(params),
	}, protocol.RequestHandlerExtra{})
	require.NoError(t, err)
	sent, ok := resp.(*promptResponseSent)
	require.True(t, ok, "expected *promptResponseSent, got %T", resp)
	return sent
}

func TestHandlePromptCalls(t *testing.T) {
	srv, _ := newServedServer(t)

	require.NoError(t, srv.RegisterPrompt(greetPrompt, greetPromptDesc, func(args greetArgs) (*PromptResponse, error) {
		return NewPromptResponse("greeting", NewPromptMessage(NewTextContent("Hello, "+args.Name), RoleUser)), nil
	}))
	require.NoError(t, srv.RegisterPrompt(failPrompt, failPromptDesc, func(ctx context.Context, args greetArgs) (*PromptResponse, error) {
		return nil, errors.New(errBoom)
	}))
	// Return arity is not validated at registration time, only at call time.
	require.NoError(t, srv.RegisterPrompt("one-result", "returns one value", func(args greetArgs) *PromptResponse {
		return nil
	}))

	t.Run("success", func(t *testing.T) {
		sent := getPrompt(t, srv, `{"name":"greet","arguments":{"name":"Ann"}}`)
		require.NoError(t, sent.Error)
		require.NotNil(t, sent.Response)
		require.NotNil(t, sent.Response.Description)
		assert.Equal(t, "greeting", *sent.Response.Description)
		require.Len(t, sent.Response.Messages, 1)
		assert.Equal(t, RoleUser, sent.Response.Messages[0].Role)
		assert.Equal(t, "Hello, Ann", sent.Response.Messages[0].Content.TextContent.Text)
	})

	t.Run("handler error", func(t *testing.T) {
		sent := getPrompt(t, srv, `{"name":"fail_prompt","arguments":{"name":"Ann"}}`)
		assert.Nil(t, sent.Response)
		assert.EqualError(t, sent.Error, errBoom)
	})

	t.Run("arguments of wrong type", func(t *testing.T) {
		sent := getPrompt(t, srv, `{"name":"greet","arguments":{"name":5}}`)
		assert.Nil(t, sent.Response)
		assert.EqualError(t, sent.Error, "failed to unmarshal arguments: json: cannot unmarshal number into Go struct field .name of type string")
	})

	t.Run("handler with wrong arity", func(t *testing.T) {
		sent := getPrompt(t, srv, `{"name":"one-result","arguments":{"name":"Ann"}}`)
		assert.Nil(t, sent.Response)
		assert.EqualError(t, sent.Error, "handler must return exactly two values, got 1")
	})
}

func TestHandleResourceCalls(t *testing.T) {
	srv, _ := newServedServer(t)

	require.NoError(t, srv.RegisterResource(textResourceURI, "readme", "Readme", mimeTextPlain, func(ctx context.Context) (*ResourceResponse, error) {
		v, _ := ctx.Value(ctxKey{}).(string)
		return NewResourceResponse(NewTextEmbeddedResource(textResourceURI, v, mimeTextPlain)), nil
	}))
	require.NoError(t, srv.RegisterResource(failResourceURI, "broken", "Broken", mimeTextPlain, func() (*ResourceResponse, error) {
		return nil, errors.New(errBoom)
	}))

	read := func(ctx context.Context, uri string) *resourceResponseSent {
		resp, err := srv.handleResourceCalls(ctx, &transport.BaseJSONRPCRequest{
			Params: []byte(`{"uri":"` + uri + `"}`),
		}, protocol.RequestHandlerExtra{})
		require.NoError(t, err)
		sent, ok := resp.(*resourceResponseSent)
		require.True(t, ok, "expected *resourceResponseSent, got %T", resp)
		return sent
	}

	ctx := context.WithValue(context.Background(), ctxKey{}, "from-ctx")
	ok := read(ctx, textResourceURI)
	require.NoError(t, ok.Error)
	require.NotNil(t, ok.Response)
	require.Len(t, ok.Response.Contents, 1)
	assert.Equal(t, "from-ctx", ok.Response.Contents[0].TextResourceContents.Text)

	failed := read(context.Background(), failResourceURI)
	assert.Nil(t, failed.Response)
	assert.EqualError(t, failed.Error, errBoom)
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

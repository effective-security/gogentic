package mcp

import (
	"context"
	"testing"

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

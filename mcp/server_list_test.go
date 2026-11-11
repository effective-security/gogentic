package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/effective-security/gogentic/mcp/internal/protocol"
	"github.com/effective-security/gogentic/mcp/internal/testingutils"
	"github.com/effective-security/gogentic/mcp/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests of the list methods and their pagination.

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

// TestHandleListPrompts_Concurrent lists prompts from many goroutines; the
// race detector reports the listing writing into the shared prompt schemas
// (F-033).
func TestHandleListPrompts_Concurrent(t *testing.T) {
	srv, _ := newServedServer(t)
	require.NoError(t, srv.RegisterPrompt(greetPrompt, greetPromptDesc, func(args greetArgs) (*PromptResponse, error) {
		return nil, nil
	}))

	// The schema is complete at registration, so a listing has nothing to
	// write into it. This fails without -race on the old code, which filled
	// in the name and description on every listing.
	registered, ok := srv.prompts.Load(greetPrompt)
	require.True(t, ok)
	assert.Equal(t, greetPrompt, registered.PromptInputSchema.Name)
	require.NotNil(t, registered.PromptInputSchema.Description)
	assert.Equal(t, greetPromptDesc, *registered.PromptInputSchema.Description)

	const listers = 8
	var wg sync.WaitGroup
	for range listers {
		wg.Go(func() {
			resp, err := srv.handleListPrompts(context.Background(), &transport.BaseJSONRPCRequest{}, protocol.RequestHandlerExtra{})
			assert.NoError(t, err)
			list, ok := resp.(ListPromptsResponse)
			if assert.True(t, ok) && assert.Len(t, list.Prompts, 1) {
				assert.Equal(t, greetPrompt, list.Prompts[0].Name)
				if assert.NotNil(t, list.Prompts[0].Description) {
					assert.Equal(t, greetPromptDesc, *list.Prompts[0].Description)
				}
			}
		})
	}
	wg.Wait()
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

// listKind adapts one */list handler for table-driven pagination tests.
type listKind struct {
	name     string
	register func(t *testing.T, srv *Server, key string)
	list     func(srv *Server, req *transport.BaseJSONRPCRequest) (keys []string, next *string, err error)
}

func listKinds() []listKind {
	extra := protocol.RequestHandlerExtra{}
	return []listKind{
		{
			name: "tools",
			register: func(t *testing.T, srv *Server, key string) {
				require.NoError(t, srv.RegisterTool(key, echoToolDesc, func(args echoArgs) (*ToolResponse, error) {
					return NewToolResponse(), nil
				}))
			},
			list: func(srv *Server, req *transport.BaseJSONRPCRequest) ([]string, *string, error) {
				resp, err := srv.handleListTools(context.Background(), req, extra)
				if err != nil {
					return nil, nil, err
				}
				page := resp.(ToolsResponse)
				var keys []string
				for _, item := range page.Tools {
					keys = append(keys, item.Name)
				}
				return keys, page.NextCursor, nil
			},
		},
		{
			name: "prompts",
			register: func(t *testing.T, srv *Server, key string) {
				require.NoError(t, srv.RegisterPrompt(key, greetPromptDesc, func(args greetArgs) (*PromptResponse, error) {
					return NewPromptResponse(key), nil
				}))
			},
			list: func(srv *Server, req *transport.BaseJSONRPCRequest) ([]string, *string, error) {
				resp, err := srv.handleListPrompts(context.Background(), req, extra)
				if err != nil {
					return nil, nil, err
				}
				page := resp.(ListPromptsResponse)
				var keys []string
				for _, item := range page.Prompts {
					keys = append(keys, item.Name)
				}
				return keys, page.NextCursor, nil
			},
		},
		{
			name: "resources",
			register: func(t *testing.T, srv *Server, key string) {
				require.NoError(t, srv.RegisterResource(key, key, key, mimeTextPlain, func() (*ResourceResponse, error) {
					return NewResourceResponse(), nil
				}))
			},
			list: func(srv *Server, req *transport.BaseJSONRPCRequest) ([]string, *string, error) {
				resp, err := srv.handleListResources(context.Background(), req, extra)
				if err != nil {
					return nil, nil, err
				}
				page := resp.(ListResourcesResponse)
				var keys []string
				for _, item := range page.Resources {
					keys = append(keys, item.Uri)
				}
				return keys, page.NextCursor, nil
			},
		},
		{
			name: "resource templates",
			register: func(t *testing.T, srv *Server, key string) {
				require.NoError(t, srv.RegisterResourceTemplate(key, key, key, mimeTextPlain))
			},
			list: func(srv *Server, req *transport.BaseJSONRPCRequest) ([]string, *string, error) {
				resp, err := srv.handleListResourceTemplates(context.Background(), req, extra)
				if err != nil {
					return nil, nil, err
				}
				page := resp.(ListResourceTemplatesResponse)
				var keys []string
				for _, item := range page.Templates {
					keys = append(keys, item.UriTemplate)
				}
				return keys, page.NextCursor, nil
			},
		},
	}
}

// listRequest builds a */list request for the given cursor; nil asks for the first page.
func listRequest(t *testing.T, cursor *string) *transport.BaseJSONRPCRequest {
	t.Helper()
	params, err := json.Marshal(map[string]any{
		"cursor": cursor,
	})
	require.NoError(t, err)
	return &transport.BaseJSONRPCRequest{
		Params: params,
	}
}

// Following nextCursor until it is absent must visit every item exactly once
// and stop, whether or not the page limit divides the item count.
func TestHandleList_PaginationTerminates(t *testing.T) {
	// Registered out of order; listings are ordered by key.
	keys := []string{"c://item", "a://item", "d://item", "b://item"}
	sorted := []string{"a://item", "b://item", "c://item", "d://item"}

	tcs := []struct {
		limit     int
		wantPages []int
	}{
		{
			limit:     1,
			wantPages: []int{1, 1, 1, 1, 0},
		},
		{
			limit:     2,
			wantPages: []int{2, 2, 0},
		},
		{
			limit:     3,
			wantPages: []int{3, 1},
		},
		{
			limit:     4,
			wantPages: []int{4, 0},
		},
		{
			limit:     5,
			wantPages: []int{4},
		},
		{
			// A non-positive limit disables pagination.
			limit:     0,
			wantPages: []int{4},
		},
	}
	for _, kind := range listKinds() {
		for _, tc := range tcs {
			t.Run(fmt.Sprintf("%s limit %d", kind.name, tc.limit), func(t *testing.T) {
				srv, _ := newServedServer(t, WithPaginationLimit(tc.limit))
				for _, key := range keys {
					kind.register(t, srv, key)
				}

				var (
					seen   []string
					pages  []int
					cursor *string
				)
				// Bounded so that a cursor loop fails instead of hanging.
				for range len(keys) + 2 {
					page, next, err := kind.list(srv, listRequest(t, cursor))
					require.NoError(t, err)
					seen = append(seen, page...)
					pages = append(pages, len(page))
					if next == nil {
						break
					}
					cursor = next
				}
				assert.Equal(t, tc.wantPages, pages)
				assert.Equal(t, sorted, seen)
			})
		}
	}
}

func TestHandleList_CursorPastLastItem(t *testing.T) {
	pastEnd := base64.StdEncoding.EncodeToString([]byte("z://after-everything"))
	for _, kind := range listKinds() {
		t.Run(kind.name, func(t *testing.T) {
			srv, _ := newServedServer(t, WithPaginationLimit(2))
			kind.register(t, srv, "a://item")

			page, next, err := kind.list(srv, listRequest(t, &pastEnd))
			require.NoError(t, err)
			assert.Empty(t, page, "a cursor past the last item must not restart the listing")
			assert.Nil(t, next)

			badCursor := "not base64!"
			_, _, err = kind.list(srv, listRequest(t, &badCursor))
			assert.EqualError(t, err, "failed to decode cursor: illegal base64 data at input byte 3")
		})
	}
}

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/mcp/transport"
	"github.com/effective-security/gogentic/mcp/transport/httptransport"
	"github.com/effective-security/gogentic/mcp/transport/localtransport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	e2eServerName    = "e2e-server"
	e2eServerVersion = "1.2.3"
	e2eInstructions  = "use the weather tool"
	e2eClientName    = "e2e-client"
	e2eClientVersion = "0.0.1"
	e2eProtocolVer   = "2025-06-18"

	weatherTool     = "get_weather"
	weatherToolDesc = "Current temperature for a city."
	echoTool        = "echo"
	echoToolDesc    = "Echoes the message back."
	failingTool     = "failing"
	failingToolDesc = "Always fails."

	greetPrompt     = "greet"
	greetPromptDesc = "Greets a person."
	failPrompt      = "fail_prompt"
	failPromptDesc  = "Always fails."

	textResourceURI  = "file:///docs/readme.txt"
	blobResourceURI  = "file:///docs/logo.png"
	failResourceURI  = "file:///docs/broken.txt"
	textResourceBody = "hello resource"
	blobResourceBody = "aGVsbG8="
	mimeTextPlain    = "text/plain"
	mimeImagePNG     = "image/png"

	cityParis = "Paris"
	errBoom   = "boom"
	errNope   = "nope"

	missingTool     = "missing"
	mediaTool       = "media"
	mediaToolDesc   = "Returns content of every kind."
	e2eHTTPEndpoint = "/mcp"

	// rpcErrorDeadline bounds calls that must fail fast with a server error;
	// a reply the client cannot match would surface as a deadline instead.
	rpcErrorDeadline = 5 * time.Second

	initResultJSON = `{"protocolVersion":"2025-06-18","capabilities":{"tools":{"listChanged":false}},"serverInfo":{"name":"s","version":"1"}}`
)

// localBridge plays the role of an in-process MCP proxy: it forwards the
// client transport's payloads to a server-side localtransport.Transport and
// returns the server's reply as the proxy response body.
type localBridge struct {
	server *localtransport.Transport
}

func (b *localBridge) HandleMCP(ctx context.Context, req *localtransport.McpProxyRequest) (*localtransport.McpProxyResponse, error) {
	msg, err := b.server.HandleMessage(ctx, req.Body)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to handle message")
	}
	resp := &localtransport.McpProxyResponse{
		Status: http.StatusOK,
	}
	if msg == nil {
		return resp, nil
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to marshal message")
	}
	resp.Type = msg.Type
	resp.Body = body
	return resp, nil
}

type weatherArgs struct {
	City string `json:"city" jsonschema:"required,description=City name"`
}

type echoArgs struct {
	Message string `json:"message" jsonschema:"required,description=Message to echo"`
}

type greetArgs struct {
	Name  string  `json:"name" jsonschema:"required,description=Who to greet"`
	Title *string `json:"title,omitempty" jsonschema:"description=Optional title"`
}

// newE2EServer builds a server with tools, prompts and resources registered
// and serving over an in-process local transport.
func newE2EServer(t *testing.T, opts ...ServerOptions) (*Server, *localtransport.Transport) {
	t.Helper()
	srvTransport := localtransport.New()
	return newE2EServerOn(t, srvTransport, opts...), srvTransport
}

// newE2EServerOn builds a server with tools, prompts and resources registered
// and serving over srvTransport.
func newE2EServerOn(t *testing.T, srvTransport transport.Transport, opts ...ServerOptions) *Server {
	t.Helper()

	allOpts := append([]ServerOptions{
		WithName(e2eServerName),
		WithVersion(e2eServerVersion),
		WithInstructions(e2eInstructions),
	}, opts...)
	srv := NewServer(srvTransport, allOpts...)

	require.NoError(t, srv.RegisterTool(weatherTool, weatherToolDesc,
		func(ctx context.Context, args weatherArgs) (*ToolResponse, error) {
			if ctx == nil {
				return nil, errors.New("missing context")
			}
			return NewToolResponse(NewTextContent("21C in " + args.City)).
				WithStructuredContent(map[string]any{
					"city":    args.City,
					"celsius": 21,
				}).
				WithMeta(map[string]any{
					"source": "test",
				}), nil
		}))
	require.NoError(t, srv.RegisterTool(echoTool, echoToolDesc,
		func(args echoArgs) (*ToolResponse, error) {
			return NewToolResponse(NewTextContent(args.Message)), nil
		}))
	require.NoError(t, srv.RegisterTool(failingTool, failingToolDesc,
		func(args echoArgs) (*ToolResponse, error) {
			return nil, errors.New(errBoom)
		}))

	require.NoError(t, srv.RegisterPrompt(greetPrompt, greetPromptDesc,
		func(ctx context.Context, args greetArgs) (*PromptResponse, error) {
			name := args.Name
			if args.Title != nil {
				name = *args.Title + " " + name
			}
			return NewPromptResponse("greeting",
				NewPromptMessage(NewTextContent("Hello, "+name), RoleUser),
				NewPromptMessage(NewTextContent("Hi!"), RoleAssistant),
			), nil
		}))
	require.NoError(t, srv.RegisterPrompt(failPrompt, failPromptDesc,
		func(args greetArgs) (*PromptResponse, error) {
			return nil, errors.New(errBoom)
		}))

	require.NoError(t, srv.RegisterResource(textResourceURI, "readme", "Readme file", mimeTextPlain,
		func() (*ResourceResponse, error) {
			return NewResourceResponse(NewTextEmbeddedResource(textResourceURI, textResourceBody, mimeTextPlain)), nil
		}))
	require.NoError(t, srv.RegisterResource(blobResourceURI, "logo", "Logo image", mimeImagePNG,
		func(ctx context.Context) (*ResourceResponse, error) {
			return NewResourceResponse(NewBlobEmbeddedResource(blobResourceURI, blobResourceBody, mimeImagePNG)), nil
		}))
	require.NoError(t, srv.RegisterResource(failResourceURI, "broken", "Broken file", mimeTextPlain,
		func(ctx context.Context) (*ResourceResponse, error) {
			return nil, errors.New(errBoom)
		}))

	require.NoError(t, srv.Serve())
	return srv
}

func newE2EClient(t *testing.T, srvTransport *localtransport.Transport) *Client {
	t.Helper()
	cliTransport := localtransport.NewLocalClientTransport(&localBridge{server: srvTransport})
	return NewClientWithInfo(cliTransport, ClientInfo{
		Name:    e2eClientName,
		Version: e2eClientVersion,
	})
}

// mountedHTTPTransport is an HTTPTransport mounted on the test's own
// httptest server, the way an application mounts it on its mux: Start is a
// no-op, so Serve does not open a listener of its own.
type mountedHTTPTransport struct {
	*httptransport.HTTPTransport
}

func (mountedHTTPTransport) Start(context.Context) error {
	return nil
}

// newE2EHTTPClient serves the e2e server over httptransport behind an
// httptest server and returns a client connected to it over HTTP.
func newE2EHTTPClient(t *testing.T, opts ...ServerOptions) *Client {
	t.Helper()
	srvTransport := httptransport.NewHTTPTransport(e2eHTTPEndpoint)
	newE2EServerOn(t, mountedHTTPTransport{
		HTTPTransport: srvTransport,
	}, opts...)

	ts := httptest.NewServer(srvTransport)
	t.Cleanup(ts.Close)

	cliTransport := httptransport.NewHTTPClientTransport(e2eHTTPEndpoint).WithBaseURL(ts.URL)
	return NewClientWithInfo(cliTransport, ClientInfo{
		Name:    e2eClientName,
		Version: e2eClientVersion,
	})
}

func TestClientServerE2E(t *testing.T) {
	ctx := context.Background()
	_, srvTransport := newE2EServer(t)
	cli := newE2EClient(t, srvTransport)

	assert.Nil(t, cli.GetCapabilities(), "capabilities must be nil before Initialize")

	initResp, err := cli.Initialize(ctx)
	require.NoError(t, err)
	assert.Equal(t, e2eProtocolVer, initResp.ProtocolVersion)
	assert.Equal(t, e2eServerName, initResp.ServerInfo.Name)
	assert.Equal(t, e2eServerVersion, initResp.ServerInfo.Version)
	require.NotNil(t, initResp.Instructions)
	assert.Equal(t, e2eInstructions, *initResp.Instructions)

	caps := cli.GetCapabilities()
	require.NotNil(t, caps)
	assert.Equal(t, &initResp.Capabilities, caps)
	require.NotNil(t, caps.Tools)
	require.NotNil(t, caps.Tools.ListChanged)
	assert.False(t, *caps.Tools.ListChanged)
	require.NotNil(t, caps.Prompts)
	require.NotNil(t, caps.Prompts.ListChanged)
	assert.False(t, *caps.Prompts.ListChanged)
	require.NotNil(t, caps.Resources)
	require.NotNil(t, caps.Resources.ListChanged)
	assert.False(t, *caps.Resources.ListChanged)
	assert.Nil(t, caps.Resources.Subscribe)

	_, err = cli.Initialize(ctx)
	assert.EqualError(t, err, "client already initialized")

	t.Run("ping", func(t *testing.T) {
		require.NoError(t, cli.Ping(ctx))
	})

	t.Run("list tools", func(t *testing.T) {
		resp, err := cli.ListTools(ctx, nil)
		require.NoError(t, err)
		assert.Nil(t, resp.NextCursor)
		require.Len(t, resp.Tools, 3)

		assert.Equal(t, echoTool, resp.Tools[0].Name)
		assert.Equal(t, failingTool, resp.Tools[1].Name)
		assert.Equal(t, weatherTool, resp.Tools[2].Name)
		require.NotNil(t, resp.Tools[2].Description)
		assert.Equal(t, weatherToolDesc, *resp.Tools[2].Description)

		schema, ok := resp.Tools[2].InputSchema.(map[string]any)
		require.True(t, ok, "input schema must decode as an object")
		assert.Equal(t, "object", schema["type"])
		assert.Equal(t, []any{"city"}, schema["required"])
		props, ok := schema["properties"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, map[string]any{
			"type":        "string",
			"description": "City name",
		}, props["city"])
	})

	t.Run("call tool with context", func(t *testing.T) {
		resp, err := cli.CallTool(ctx, weatherTool, weatherArgs{City: cityParis})
		require.NoError(t, err)
		require.Len(t, resp.Content, 1)
		assert.Equal(t, ContentTypeText, resp.Content[0].Type)
		require.NotNil(t, resp.Content[0].TextContent)
		assert.Equal(t, "21C in Paris", resp.Content[0].TextContent.Text)
		assert.Equal(t, map[string]any{
			"city":    cityParis,
			"celsius": float64(21),
		}, resp.StructuredContent)
		assert.Equal(t, map[string]any{
			"source": "test",
		}, resp.Meta)
	})

	t.Run("call tool without context", func(t *testing.T) {
		resp, err := cli.CallTool(ctx, echoTool, map[string]any{
			"message": "ping-pong",
		})
		require.NoError(t, err)
		require.Len(t, resp.Content, 1)
		assert.Equal(t, "ping-pong", resp.Content[0].TextContent.Text)
		assert.Nil(t, resp.StructuredContent)
		assert.Nil(t, resp.Meta)
	})

	t.Run("call tool with nil arguments", func(t *testing.T) {
		resp, err := cli.CallTool(ctx, echoTool, nil)
		require.NoError(t, err)
		require.Len(t, resp.Content, 1)
		assert.Equal(t, "", resp.Content[0].TextContent.Text)
	})

	t.Run("call tool returning error", func(t *testing.T) {
		// Tool errors are not protocol errors: they come back as content.
		resp, err := cli.CallTool(ctx, failingTool, echoArgs{Message: "x"})
		require.NoError(t, err)
		require.Len(t, resp.Content, 1)
		assert.Equal(t, "failed to handle tool call: boom", resp.Content[0].TextContent.Text)
	})

	t.Run("list prompts", func(t *testing.T) {
		resp, err := cli.ListPrompts(ctx, nil)
		require.NoError(t, err)
		assert.Nil(t, resp.NextCursor)
		require.Len(t, resp.Prompts, 2)

		assert.Equal(t, failPrompt, resp.Prompts[0].Name)
		greet := resp.Prompts[1]
		assert.Equal(t, greetPrompt, greet.Name)
		require.NotNil(t, greet.Description)
		assert.Equal(t, greetPromptDesc, *greet.Description)
		require.Len(t, greet.Arguments, 2)

		assert.Equal(t, "name", greet.Arguments[0].Name)
		require.NotNil(t, greet.Arguments[0].Description)
		assert.Equal(t, "Who to greet", *greet.Arguments[0].Description)
		require.NotNil(t, greet.Arguments[0].Required)
		assert.True(t, *greet.Arguments[0].Required)

		assert.Equal(t, "title", greet.Arguments[1].Name)
		require.NotNil(t, greet.Arguments[1].Description)
		assert.Equal(t, "Optional title", *greet.Arguments[1].Description)
		assert.Nil(t, greet.Arguments[1].Required, "omitempty field must not be required")
	})

	t.Run("get prompt", func(t *testing.T) {
		title := "Dr."
		resp, err := cli.GetPrompt(ctx, greetPrompt, greetArgs{
			Name:  "Who",
			Title: &title,
		})
		require.NoError(t, err)
		require.NotNil(t, resp.Description)
		assert.Equal(t, "greeting", *resp.Description)
		require.Len(t, resp.Messages, 2)
		assert.Equal(t, RoleUser, resp.Messages[0].Role)
		assert.Equal(t, "Hello, Dr. Who", resp.Messages[0].Content.TextContent.Text)
		assert.Equal(t, RoleAssistant, resp.Messages[1].Role)
		assert.Equal(t, "Hi!", resp.Messages[1].Content.TextContent.Text)
	})

	t.Run("get prompt with nil arguments", func(t *testing.T) {
		resp, err := cli.GetPrompt(ctx, greetPrompt, nil)
		require.NoError(t, err)
		require.Len(t, resp.Messages, 2)
		assert.Equal(t, "Hello, ", resp.Messages[0].Content.TextContent.Text)
	})

	t.Run("get prompt returning error", func(t *testing.T) {
		resp, err := cli.GetPrompt(ctx, failPrompt, greetArgs{Name: "x"})
		require.NoError(t, err)
		require.NotNil(t, resp.Description)
		assert.Equal(t, "error", *resp.Description)
		require.Len(t, resp.Messages, 1)
		assert.Equal(t, RoleUser, resp.Messages[0].Role)
		assert.Equal(t, errBoom, resp.Messages[0].Content.TextContent.Text)
	})

	t.Run("list resources", func(t *testing.T) {
		resp, err := cli.ListResources(ctx, nil)
		require.NoError(t, err)
		assert.Nil(t, resp.NextCursor)
		require.Len(t, resp.Resources, 3)

		// Ordered by URI.
		assert.Equal(t, failResourceURI, resp.Resources[0].Uri)
		assert.Equal(t, blobResourceURI, resp.Resources[1].Uri)
		readme := resp.Resources[2]
		assert.Equal(t, textResourceURI, readme.Uri)
		assert.Equal(t, "readme", readme.Name)
		require.NotNil(t, readme.Description)
		assert.Equal(t, "Readme file", *readme.Description)
		require.NotNil(t, readme.MimeType)
		assert.Equal(t, mimeTextPlain, *readme.MimeType)
		assert.Nil(t, readme.Annotations)
	})

	t.Run("read text resource", func(t *testing.T) {
		resp, err := cli.ReadResource(ctx, textResourceURI)
		require.NoError(t, err)
		require.Len(t, resp.Contents, 1)
		got := resp.Contents[0]
		assert.Equal(t, embeddedResourceTypeText, got.EmbeddedResourceType)
		assert.Nil(t, got.BlobResourceContents)
		require.NotNil(t, got.TextResourceContents)
		assert.Equal(t, textResourceURI, got.TextResourceContents.Uri)
		assert.Equal(t, textResourceBody, got.TextResourceContents.Text)
		require.NotNil(t, got.TextResourceContents.MimeType)
		assert.Equal(t, mimeTextPlain, *got.TextResourceContents.MimeType)
	})

	t.Run("read blob resource", func(t *testing.T) {
		resp, err := cli.ReadResource(ctx, blobResourceURI)
		require.NoError(t, err)
		require.Len(t, resp.Contents, 1)
		got := resp.Contents[0]
		assert.Equal(t, embeddedResourceTypeBlob, got.EmbeddedResourceType)
		assert.Nil(t, got.TextResourceContents)
		require.NotNil(t, got.BlobResourceContents)
		assert.Equal(t, blobResourceURI, got.BlobResourceContents.Uri)
		assert.Equal(t, blobResourceBody, got.BlobResourceContents.Blob)
		require.NotNil(t, got.BlobResourceContents.MimeType)
		assert.Equal(t, mimeImagePNG, *got.BlobResourceContents.MimeType)
	})

	t.Run("read resource returning error", func(t *testing.T) {
		resp, err := cli.ReadResource(ctx, failResourceURI)
		require.NoError(t, err)
		require.Len(t, resp.Contents, 1)
		require.NotNil(t, resp.Contents[0].TextResourceContents)
		assert.Equal(t, errBoom, resp.Contents[0].TextResourceContents.Text)
		assert.Equal(t, failResourceURI, resp.Contents[0].TextResourceContents.Uri)
	})
}

func TestClientServerE2E_Pagination(t *testing.T) {
	ctx := context.Background()
	_, srvTransport := newE2EServer(t, WithPaginationLimit(2))
	cli := newE2EClient(t, srvTransport)
	_, err := cli.Initialize(ctx)
	require.NoError(t, err)

	first, err := cli.ListTools(ctx, nil)
	require.NoError(t, err)
	require.Len(t, first.Tools, 2)
	assert.Equal(t, echoTool, first.Tools[0].Name)
	assert.Equal(t, failingTool, first.Tools[1].Name)
	require.NotNil(t, first.NextCursor)

	second, err := cli.ListTools(ctx, first.NextCursor)
	require.NoError(t, err)
	require.Len(t, second.Tools, 1)
	assert.Equal(t, weatherTool, second.Tools[0].Name)
	assert.Nil(t, second.NextCursor)

	prompts, err := cli.ListPrompts(ctx, nil)
	require.NoError(t, err)
	require.Len(t, prompts.Prompts, 2)
	assert.Equal(t, failPrompt, prompts.Prompts[0].Name)
	assert.Equal(t, greetPrompt, prompts.Prompts[1].Name)
	require.NotNil(t, prompts.NextCursor, "a full page must carry a cursor")

	// Two prompts with a limit of two: the cursor of the full page leads to an
	// empty last page instead of back to the first one.
	noMorePrompts, err := cli.ListPrompts(ctx, prompts.NextCursor)
	require.NoError(t, err)
	assert.Empty(t, noMorePrompts.Prompts)
	assert.Nil(t, noMorePrompts.NextCursor)

	resources, err := cli.ListResources(ctx, nil)
	require.NoError(t, err)
	require.Len(t, resources.Resources, 2)
	require.NotNil(t, resources.NextCursor)

	rest, err := cli.ListResources(ctx, resources.NextCursor)
	require.NoError(t, err)
	require.Len(t, rest.Resources, 1)
	assert.Equal(t, textResourceURI, rest.Resources[0].Uri)
	assert.Nil(t, rest.NextCursor)
}

// Unknown tools, prompts and resources are JSON-RPC errors. The transports
// must hand them back under the caller's request ID, otherwise the client
// cannot match the reply and waits until its deadline.
func TestClientServerE2E_RPCErrors(t *testing.T) {
	clients := []struct {
		name      string
		newClient func(t *testing.T) *Client
	}{
		{
			name: "local transport",
			newClient: func(t *testing.T) *Client {
				_, srvTransport := newE2EServer(t)
				return newE2EClient(t, srvTransport)
			},
		},
		{
			name: "http transport",
			newClient: func(t *testing.T) *Client {
				return newE2EHTTPClient(t)
			},
		},
	}
	calls := []struct {
		name    string
		call    func(ctx context.Context, cli *Client) error
		wantErr string
	}{
		{
			name: "unknown tool",
			call: func(ctx context.Context, cli *Client) error {
				_, err := cli.CallTool(ctx, missingTool, map[string]any{})
				return err
			},
			wantErr: "failed to call tool: RPC error -32000: unknown tool: missing",
		},
		{
			name: "unknown prompt",
			call: func(ctx context.Context, cli *Client) error {
				_, err := cli.GetPrompt(ctx, errNope, greetArgs{Name: "x"})
				return err
			},
			wantErr: "failed to get prompt: RPC error -32000: unknown prompt: nope",
		},
		{
			name: "unknown resource",
			call: func(ctx context.Context, cli *Client) error {
				_, err := cli.ReadResource(ctx, missingResourceURI)
				return err
			},
			wantErr: "failed to read resource: RPC error -32000: unknown resource: file:///docs/missing.txt",
		},
	}
	for _, cc := range clients {
		t.Run(cc.name, func(t *testing.T) {
			cli := cc.newClient(t)
			_, err := cli.Initialize(context.Background())
			require.NoError(t, err)

			for _, call := range calls {
				t.Run(call.name, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), rpcErrorDeadline)
					defer cancel()

					err := call.call(ctx, cli)
					assert.EqualError(t, err, call.wantErr)
					assert.NotErrorIs(t, err, context.DeadlineExceeded)
				})
			}

			// The connection is still usable after the errors.
			resp, err := cli.CallTool(context.Background(), echoTool, echoArgs{Message: "still here"})
			require.NoError(t, err)
			require.Len(t, resp.Content, 1)
			assert.Equal(t, "still here", resp.Content[0].TextContent.Text)
		})
	}
}

// Every kind of content a tool returns reaches the client intact, annotations
// included.
func TestClientServerE2E_ContentRoundTrip(t *testing.T) {
	priority := 0.25
	userOnly := Annotations{
		Audience: []Role{RoleUser},
		Priority: &priority,
	}
	assistantOnly := Annotations{
		Audience: []Role{RoleAssistant},
	}
	newContent := func() []*Content {
		return []*Content{
			NewTextContent(textResourceBody).WithAnnotations(userOnly),
			NewImageContent(blobResourceBody, mimeImagePNG).WithAnnotations(assistantOnly),
			NewTextResourceContent(textResourceURI, textResourceBody, mimeTextPlain),
			NewBlobResourceContent(blobResourceURI, blobResourceBody, mimeImagePNG).WithAnnotations(userOnly),
		}
	}

	srv, srvTransport := newE2EServer(t)
	require.NoError(t, srv.RegisterTool(mediaTool, mediaToolDesc,
		func(args echoArgs) (*ToolResponse, error) {
			return NewToolResponse(newContent()...), nil
		}))
	cli := newE2EClient(t, srvTransport)
	_, err := cli.Initialize(context.Background())
	require.NoError(t, err)

	resp, err := cli.CallTool(context.Background(), mediaTool, echoArgs{Message: "x"})
	require.NoError(t, err)
	assert.Equal(t, newContent(), resp.Content)
	assert.Nil(t, resp.StructuredContent)
	assert.Nil(t, resp.Meta)
}

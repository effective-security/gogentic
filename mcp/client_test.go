package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
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

// scriptedTransport is a client-side transport that answers requests with
// canned replies, so that the client's error handling can be exercised.
type scriptedTransport struct {
	mu        sync.Mutex
	startErr  error
	sendErr   func(msg *transport.BaseJsonRpcMessage) error
	reply     func(req *transport.BaseJSONRPCRequest) *transport.BaseJsonRpcMessage
	onMessage func(ctx context.Context, msg *transport.BaseJsonRpcMessage)
	sent      []*transport.BaseJsonRpcMessage
}

func (s *scriptedTransport) Start(_ context.Context) error {
	return s.startErr
}

func (s *scriptedTransport) Send(ctx context.Context, msg *transport.BaseJsonRpcMessage) error {
	s.mu.Lock()
	s.sent = append(s.sent, msg)
	sendErr := s.sendErr
	reply := s.reply
	handler := s.onMessage
	s.mu.Unlock()

	if sendErr != nil {
		if err := sendErr(msg); err != nil {
			return err
		}
	}
	if msg.Type != transport.BaseMessageTypeJSONRPCRequestType || reply == nil || handler == nil {
		return nil
	}
	if r := reply(msg.JsonRpcRequest); r != nil {
		handler(ctx, r)
	}
	return nil
}

func (s *scriptedTransport) Close() error                { return nil }
func (s *scriptedTransport) SetCloseHandler(func())      {}
func (s *scriptedTransport) SetErrorHandler(func(error)) {}
func (s *scriptedTransport) SetMessageHandler(h func(context.Context, *transport.BaseJsonRpcMessage)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onMessage = h
}

func (s *scriptedTransport) lastRequest() *transport.BaseJSONRPCRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.sent) - 1; i >= 0; i-- {
		if s.sent[i].Type == transport.BaseMessageTypeJSONRPCRequestType {
			return s.sent[i].JsonRpcRequest
		}
	}
	return nil
}

func (s *scriptedTransport) setReply(reply func(req *transport.BaseJSONRPCRequest) *transport.BaseJsonRpcMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reply = reply
}

func (s *scriptedTransport) setSendErr(fn func(msg *transport.BaseJsonRpcMessage) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendErr = fn
}

func resultReply(req *transport.BaseJSONRPCRequest, result string) *transport.BaseJsonRpcMessage {
	return transport.NewBaseMessageResponse(&transport.BaseJSONRPCResponse{
		Jsonrpc: "2.0",
		Id:      req.Id,
		Result:  json.RawMessage(result),
	})
}

func errorReply(req *transport.BaseJSONRPCRequest, code int, message string) *transport.BaseJsonRpcMessage {
	return transport.NewBaseMessageError(&transport.BaseJSONRPCError{
		Jsonrpc: "2.0",
		Id:      req.Id,
		Error: transport.BaseJSONRPCErrorInner{
			Code:    code,
			Message: message,
		},
	})
}

func initOnlyReply(req *transport.BaseJSONRPCRequest) *transport.BaseJsonRpcMessage {
	if req.Method == "initialize" {
		return resultReply(req, initResultJSON)
	}
	return nil
}

func failOnMethod(method string) func(msg *transport.BaseJsonRpcMessage) error {
	return func(msg *transport.BaseJsonRpcMessage) error {
		if msg.Type == transport.BaseMessageTypeJSONRPCRequestType && msg.JsonRpcRequest.Method == method {
			return errors.New(errBoom)
		}
		if msg.Type == transport.BaseMessageTypeJSONRPCNotificationType && msg.JsonRpcNotification.Method == method {
			return errors.New(errBoom)
		}
		return nil
	}
}

func TestClient_Initialize(t *testing.T) {
	ctx := context.Background()

	t.Run("sends client info and initialized notification", func(t *testing.T) {
		tr := &scriptedTransport{reply: initOnlyReply}
		cli := NewClientWithInfo(tr, ClientInfo{
			Name:    e2eClientName,
			Version: e2eClientVersion,
		})
		resp, err := cli.Initialize(ctx)
		require.NoError(t, err)
		assert.Equal(t, e2eProtocolVer, resp.ProtocolVersion)
		assert.Equal(t, "s", resp.ServerInfo.Name)

		require.Len(t, tr.sent, 2)
		req := tr.sent[0]
		require.Equal(t, transport.BaseMessageTypeJSONRPCRequestType, req.Type)
		assert.Equal(t, "initialize", req.JsonRpcRequest.Method)
		assert.JSONEq(t, `{"protocolVersion":"1.0","capabilities":{},"clientInfo":{"name":"e2e-client","version":"0.0.1"}}`,
			string(req.JsonRpcRequest.Params))

		notif := tr.sent[1]
		require.Equal(t, transport.BaseMessageTypeJSONRPCNotificationType, notif.Type)
		assert.Equal(t, "notifications/initialized", notif.JsonRpcNotification.Method)
	})

	t.Run("NewClient sends empty client info", func(t *testing.T) {
		tr := &scriptedTransport{reply: initOnlyReply}
		cli := NewClient(tr)
		_, err := cli.Initialize(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, tr.sent)
		assert.JSONEq(t, `{"protocolVersion":"1.0","capabilities":{},"clientInfo":{"name":"","version":""}}`,
			string(tr.sent[0].JsonRpcRequest.Params))
	})

	tcs := []struct {
		name    string
		tr      *scriptedTransport
		wantErr string
	}{
		{
			name: "transport start fails",
			tr: &scriptedTransport{
				startErr: errors.New(errBoom),
			},
			wantErr: "failed to connect transport: boom",
		},
		{
			name: "request send fails",
			tr: &scriptedTransport{
				sendErr: failOnMethod("initialize"),
			},
			wantErr: "failed to initialize: failed to send request: boom",
		},
		{
			name: "server returns rpc error",
			tr: &scriptedTransport{
				reply: func(req *transport.BaseJSONRPCRequest) *transport.BaseJsonRpcMessage {
					return errorReply(req, -32000, errNope)
				},
			},
			wantErr: "failed to initialize: RPC error -32000: nope",
		},
		{
			name: "server returns malformed result",
			tr: &scriptedTransport{
				reply: func(req *transport.BaseJSONRPCRequest) *transport.BaseJsonRpcMessage {
					return resultReply(req, `"not-an-object"`)
				},
			},
			wantErr: "failed to unmarshal initialize response: json: cannot unmarshal string into Go value of type map[string]interface {}",
		},
		{
			name: "server result misses required field",
			tr: &scriptedTransport{
				reply: func(req *transport.BaseJSONRPCRequest) *transport.BaseJsonRpcMessage {
					return resultReply(req, `{"protocolVersion":"x","serverInfo":{"name":"a","version":"b"}}`)
				},
			},
			wantErr: "failed to unmarshal initialize response: field capabilities in initializeResult: required",
		},
		{
			name: "initialized notification fails",
			tr: &scriptedTransport{
				reply:   initOnlyReply,
				sendErr: failOnMethod("notifications/initialized"),
			},
			wantErr: "failed to send initialized notification: boom",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			cli := NewClient(tc.tr)
			resp, err := cli.Initialize(ctx)
			assert.EqualError(t, err, tc.wantErr)
			assert.Nil(t, resp)
		})
	}
}

// clientCall describes one client RPC method for table-driven error tests.
type clientCall struct {
	name       string
	method     string
	params     string
	wrapMsg    string
	unmarshal  string
	call       func(ctx context.Context, c *Client) error
	skipResult bool
}

func clientCalls() []clientCall {
	cursor := "Y3Vyc29y"
	return []clientCall{
		{
			name:      "ListTools",
			method:    "tools/list",
			params:    `{"cursor":"Y3Vyc29y"}`,
			wrapMsg:   "failed to list tools",
			unmarshal: "failed to unmarshal tools response: json: cannot unmarshal string into Go value of type mcp.ToolsResponse",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListTools(ctx, &cursor)
				return err
			},
		},
		{
			name:      "CallTool",
			method:    "tools/call",
			params:    `{"name":"get_weather","arguments":{"city":"Paris"}}`,
			wrapMsg:   "failed to call tool",
			unmarshal: "failed to unmarshal tool response: json: cannot unmarshal string into Go value of type mcp.ToolResponse",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.CallTool(ctx, weatherTool, weatherArgs{City: cityParis})
				return err
			},
		},
		{
			name:      "ListPrompts",
			method:    "prompts/list",
			params:    `{"cursor":null}`,
			wrapMsg:   "failed to list prompts",
			unmarshal: "failed to unmarshal prompts response: json: cannot unmarshal string into Go value of type mcp.ListPromptsResponse",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListPrompts(ctx, nil)
				return err
			},
		},
		{
			name:      "GetPrompt",
			method:    "prompts/get",
			params:    `{"name":"greet","arguments":{"name":"Bob"}}`,
			wrapMsg:   "failed to get prompt",
			unmarshal: "failed to unmarshal prompt response: json: cannot unmarshal string into Go value of type mcp.PromptResponse",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetPrompt(ctx, greetPrompt, greetArgs{Name: "Bob"})
				return err
			},
		},
		{
			name:      "ListResources",
			method:    "resources/list",
			params:    `{"cursor":null}`,
			wrapMsg:   "failed to list resources",
			unmarshal: "failed to unmarshal resources response: json: cannot unmarshal string into Go value of type mcp.ListResourcesResponse",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListResources(ctx, nil)
				return err
			},
		},
		{
			name:      "ReadResource",
			method:    "resources/read",
			params:    `{"uri":"file:///docs/readme.txt"}`,
			wrapMsg:   "failed to read resource",
			unmarshal: "failed to unmarshal resource response: json: cannot unmarshal string into Go value of type mcp.ResourceResponse",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ReadResource(ctx, textResourceURI)
				return err
			},
		},
		{
			name:       "Ping",
			method:     "ping",
			params:     `null`,
			wrapMsg:    "failed to ping server",
			skipResult: true,
			call: func(ctx context.Context, c *Client) error {
				return c.Ping(ctx)
			},
		},
	}
}

func newInitializedScriptedClient(t *testing.T) (*Client, *scriptedTransport) {
	t.Helper()
	tr := &scriptedTransport{reply: initOnlyReply}
	cli := NewClient(tr)
	_, err := cli.Initialize(context.Background())
	require.NoError(t, err)
	return cli, tr
}

func TestClient_MethodErrors(t *testing.T) {
	ctx := context.Background()

	for _, cc := range clientCalls() {
		t.Run(cc.name, func(t *testing.T) {
			t.Run("not initialized", func(t *testing.T) {
				tr := &scriptedTransport{}
				cli := NewClient(tr)
				assert.EqualError(t, cc.call(ctx, cli), "client not initialized")
				assert.Empty(t, tr.sent, "nothing must be sent before Initialize")
			})

			t.Run("send fails", func(t *testing.T) {
				cli, tr := newInitializedScriptedClient(t)
				tr.setSendErr(failOnMethod(cc.method))
				assert.EqualError(t, cc.call(ctx, cli), cc.wrapMsg+": failed to send request: boom")
			})

			t.Run("rpc error", func(t *testing.T) {
				cli, tr := newInitializedScriptedClient(t)
				tr.setReply(func(req *transport.BaseJSONRPCRequest) *transport.BaseJsonRpcMessage {
					return errorReply(req, -32601, errNope)
				})
				assert.EqualError(t, cc.call(ctx, cli), cc.wrapMsg+": RPC error -32601: nope")

				req := tr.lastRequest()
				require.NotNil(t, req)
				assert.Equal(t, cc.method, req.Method)
				assert.JSONEq(t, cc.params, string(req.Params))
			})

			t.Run("malformed result", func(t *testing.T) {
				cli, tr := newInitializedScriptedClient(t)
				tr.setReply(func(req *transport.BaseJSONRPCRequest) *transport.BaseJsonRpcMessage {
					return resultReply(req, `"unexpected"`)
				})
				err := cc.call(ctx, cli)
				if cc.skipResult {
					// Ping ignores the result payload.
					assert.NoError(t, err)
					return
				}
				assert.EqualError(t, err, cc.unmarshal)
			})
		})
	}
}

// TestClient_NilArgumentsAreOmitted checks that nil arguments leave the
// optional "arguments" field out of the request instead of sending null.
func TestClient_NilArgumentsAreOmitted(t *testing.T) {
	ctx := context.Background()
	tr := &scriptedTransport{
		reply: func(req *transport.BaseJSONRPCRequest) *transport.BaseJsonRpcMessage {
			switch req.Method {
			case "tools/call":
				return resultReply(req, `{"content":[]}`)
			case "prompts/get":
				return resultReply(req, `{"messages":[]}`)
			}
			return initOnlyReply(req)
		},
	}
	cli := NewClient(tr)
	_, err := cli.Initialize(ctx)
	require.NoError(t, err)

	lastParams := func(method string) string {
		t.Helper()
		tr.mu.Lock()
		defer tr.mu.Unlock()
		for i := len(tr.sent) - 1; i >= 0; i-- {
			if req := tr.sent[i].JsonRpcRequest; req != nil && req.Method == method {
				return string(req.Params)
			}
		}
		require.Failf(t, "request not sent", "method %s", method)
		return ""
	}

	_, err = cli.CallTool(ctx, echoTool, nil)
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"echo"}`, lastParams("tools/call"))

	_, err = cli.GetPrompt(ctx, greetPrompt, nil)
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"greet"}`, lastParams("prompts/get"))

	var nilMap map[string]any
	_, err = cli.CallTool(ctx, echoTool, nilMap)
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"echo"}`, lastParams("tools/call"))

	var nilArgs *greetArgs
	_, err = cli.GetPrompt(ctx, greetPrompt, nilArgs)
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"greet"}`, lastParams("prompts/get"))

	_, err = cli.CallTool(ctx, echoTool, map[string]any{})
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"echo","arguments":{}}`, lastParams("tools/call"))
}

func TestClient_ArgumentMarshalErrors(t *testing.T) {
	ctx := context.Background()
	cli, tr := newInitializedScriptedClient(t)
	sentBefore := len(tr.sent)

	_, err := cli.CallTool(ctx, weatherTool, make(chan int))
	assert.EqualError(t, err, "failed to marshal arguments: json: unsupported type: chan int")

	_, err = cli.GetPrompt(ctx, greetPrompt, make(chan int))
	assert.EqualError(t, err, "failed to marshal arguments: json: unsupported type: chan int")

	assert.Len(t, tr.sent, sentBefore, "nothing must be sent when arguments cannot be marshaled")
}

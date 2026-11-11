package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/mcp/internal/protocol"
	"github.com/effective-security/gogentic/mcp/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests of handler parsing at registration and of handler calls.

func TestRegisterTool_Validation(t *testing.T) {
	tcs := []struct {
		name    string
		handler any
		wantErr string
	}{
		{
			name:    "three arguments",
			handler: func(context.Context, echoArgs, int) (*ToolResponse, error) { return nil, nil },
			wantErr: "handler must take an optional context.Context and at most one argument, got func(context.Context, mcp.echoArgs, int) (*mcp.ToolResponse, error)",
		},
		{
			name:    "single return value",
			handler: func(echoArgs) *ToolResponse { return nil },
			wantErr: "handler must return exactly two values, got 1",
		},
		{
			name:    "first of two arguments is not a context",
			handler: func(string, echoArgs) (*ToolResponse, error) { return nil, nil },
			wantErr: "handler must take an optional context.Context and at most one argument, got func(string, mcp.echoArgs) (*mcp.ToolResponse, error)",
		},
		{
			name:    "wrong response type",
			handler: func(echoArgs) (string, error) { return "", nil },
			wantErr: "handler must return *mcp.ToolResponse, got string",
		},
		{
			name:    "response is not a pointer",
			handler: func(echoArgs) (ToolResponse, error) { return ToolResponse{}, nil },
			wantErr: "handler must return *mcp.ToolResponse, got mcp.ToolResponse",
		},
		{
			name:    "wrong error type",
			handler: func(echoArgs) (*ToolResponse, string) { return nil, "" },
			wantErr: "handler must return error, got string",
		},
		{
			name:    "nil handler",
			handler: nil,
			wantErr: "handler must be a non-nil function, got <nil>",
		},
		{
			// Used to register, then fail every call with a recovered panic.
			name:    "typed nil handler",
			handler: (func(echoArgs) (*ToolResponse, error))(nil),
			wantErr: "handler must be a non-nil function, got func(mcp.echoArgs) (*mcp.ToolResponse, error)",
		},
		{
			name:    "not a function",
			handler: "echo",
			wantErr: "handler must be a non-nil function, got string",
		},
		{
			name:    "variadic",
			handler: func(...echoArgs) (*ToolResponse, error) { return nil, nil },
			wantErr: "handler must not be variadic, got func(...mcp.echoArgs) (*mcp.ToolResponse, error)",
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
			wantErr: "resource handler must take no arguments besides context.Context, got func(context.Context, string) (*mcp.ResourceResponse, error)",
		},
		{
			name:    "argument is not a context",
			handler: func(string) (*ResourceResponse, error) { return nil, nil },
			wantErr: "resource handler must take no arguments besides context.Context, got func(string) (*mcp.ResourceResponse, error)",
		},
		{
			name:    "three arguments",
			handler: func(context.Context, string, int) (*ResourceResponse, error) { return nil, nil },
			wantErr: "handler must take an optional context.Context and at most one argument, got func(context.Context, string, int) (*mcp.ResourceResponse, error)",
		},
		{
			name:    "single return value",
			handler: func() *ResourceResponse { return nil },
			wantErr: "handler must return exactly two values, got 1",
		},
		{
			// F-028: this handler used to register and crash resources/read.
			name:    "wrong response type",
			handler: func() (string, error) { return "x", nil },
			wantErr: "handler must return *mcp.ResourceResponse, got string",
		},
		{
			name:    "wrong error type",
			handler: func(context.Context) (*ResourceResponse, string) { return nil, "" },
			wantErr: "handler must return error, got string",
		},
		{
			name:    "nil handler",
			handler: nil,
			wantErr: "handler must be a non-nil function, got <nil>",
		},
		{
			name:    "typed nil handler",
			handler: (func() (*ResourceResponse, error))(nil),
			wantErr: "handler must be a non-nil function, got func() (*mcp.ResourceResponse, error)",
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
			name:    "first of two arguments is not a context",
			handler: func(string, greetArgs) (*PromptResponse, error) { return nil, nil },
			wantErr: "handler must take an optional context.Context and at most one argument, got func(string, mcp.greetArgs) (*mcp.PromptResponse, error)",
		},
		{
			name:    "argument is not a struct",
			handler: func(string) (*PromptResponse, error) { return nil, nil },
			wantErr: "prompt arguments must be a struct, got string",
		},
		{
			name:    "non string field",
			handler: func(intArgs) (*PromptResponse, error) { return nil, nil },
			wantErr: "all fields of the struct must be of type string or *string, found int",
		},
		{
			name:    "single return value",
			handler: func(greetArgs) *PromptResponse { return nil },
			wantErr: "handler must return exactly two values, got 1",
		},
		{
			// F-028: this handler used to register and crash prompts/get.
			name:    "wrong response type",
			handler: func(greetArgs) (string, error) { return "x", nil },
			wantErr: "handler must return *mcp.PromptResponse, got string",
		},
		{
			name:    "wrong error type",
			handler: func(context.Context, greetArgs) (*PromptResponse, bool) { return nil, false },
			wantErr: "handler must return error, got bool",
		},
		{
			name:    "not a function",
			handler: 42,
			wantErr: "handler must be a non-nil function, got int",
		},
		{
			name:    "typed nil handler",
			handler: (func(greetArgs) (*PromptResponse, error))(nil),
			wantErr: "handler must be a non-nil function, got func(mcp.greetArgs) (*mcp.PromptResponse, error)",
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
	require.NoError(t, srv.RegisterTool("panic-str", "panics with a string", func(args echoArgs) (*ToolResponse, error) {
		panic("kaboom")
	}))
	require.NoError(t, srv.RegisterTool("no-response", "returns nothing", func(args echoArgs) (*ToolResponse, error) {
		return nil, nil
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

	// The panic value can hold internal data: only the server log gets it.
	for _, name := range []string{"panic-err", "panic-str"} {
		t.Run(name, func(t *testing.T) {
			sent := callTool(t, srv, context.Background(), `{"name":"`+name+`","arguments":{"message":"x"}}`)
			assert.Nil(t, sent.Response)
			assert.EqualError(t, sent.Error, "internal error: tool panicked")
			b, err := json.Marshal(sent)
			require.NoError(t, err)
			assert.JSONEq(t, `{"content":[{"type":"text","text":"internal error: tool panicked"}],"isError":true}`, string(b))
		})
	}

	t.Run("neither response nor error", func(t *testing.T) {
		sent := callTool(t, srv, context.Background(), `{"name":"no-response","arguments":{"message":"x"}}`)
		assert.Nil(t, sent.Response)
		assert.EqualError(t, sent.Error, "tool handler returned neither a response nor an error")
		// Used to dereference the nil response while marshaling.
		b, err := json.Marshal(sent)
		require.NoError(t, err)
		assert.JSONEq(t, `{"content":[{"type":"text","text":"tool handler returned neither a response nor an error"}],"isError":true}`, string(b))
	})

	// F-029: absent and null arguments give the handler zero-value arguments.
	for _, params := range []string{
		`{"name":"ctx-tool"}`,
		`{"name":"ctx-tool","arguments":null}`,
	} {
		t.Run("arguments "+params, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), ctxKey{}, "v1")
			sent := callTool(t, srv, ctx, params)
			require.NoError(t, sent.Error)
			require.Len(t, sent.Response.Content, 1)
			assert.Equal(t, "v1:", sent.Response.Content[0].TextContent.Text)
		})
	}
}

func TestHandleToolCalls_HandlerWithoutArguments(t *testing.T) {
	tcs := []struct {
		name    string
		handler any
		want    string
	}{
		{
			name: "context only",
			handler: func(ctx context.Context) (*ToolResponse, error) {
				v, _ := ctx.Value(ctxKey{}).(string)
				return NewToolResponse(NewTextContent(v)), nil
			},
			want: "v1",
		},
		{
			// Used to be rejected, although a tool may take no input.
			name: "no parameters",
			handler: func() (*ToolResponse, error) {
				return NewToolResponse(NewTextContent("now")), nil
			},
			want: "now",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newServedServer(t)
			require.NoError(t, srv.RegisterTool(echoTool, echoToolDesc, tc.handler))

			tl, ok := srv.tools.Load(echoTool)
			require.True(t, ok)
			b, err := json.Marshal(tl.ToolInputSchema)
			require.NoError(t, err)
			assert.JSONEq(t, `{"type":"object","properties":{}}`, string(b))

			ctx := context.WithValue(context.Background(), ctxKey{}, "v1")
			sent := callTool(t, srv, ctx, `{"name":"`+echoTool+`","arguments":{"ignored":true}}`)
			require.NoError(t, sent.Error)
			require.Len(t, sent.Response.Content, 1)
			assert.Equal(t, tc.want, sent.Response.Content[0].TextContent.Text)
		})
	}
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
	require.NoError(t, srv.RegisterPrompt("panic_prompt", "panics", func(args greetArgs) (*PromptResponse, error) {
		panic("kaboom")
	}))
	require.NoError(t, srv.RegisterPrompt("empty_prompt", "returns nothing", func(args greetArgs) (*PromptResponse, error) {
		return nil, nil
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
		assert.EqualError(t, sent.Error, "failed to unmarshal arguments: json: cannot unmarshal number into Go struct field greetArgs.name of type string")
	})

	// F-029: absent arguments failed with "unexpected end of JSON input" and
	// null arguments with "arguments must be a struct".
	for _, params := range []string{
		`{"name":"greet"}`,
		`{"name":"greet","arguments":null}`,
	} {
		t.Run("arguments "+params, func(t *testing.T) {
			sent := getPrompt(t, srv, params)
			require.NoError(t, sent.Error)
			require.NotNil(t, sent.Response)
			require.Len(t, sent.Response.Messages, 1)
			assert.Equal(t, "Hello, ", sent.Response.Messages[0].Content.TextContent.Text)
		})
	}

	t.Run("panic", func(t *testing.T) {
		sent := getPrompt(t, srv, `{"name":"panic_prompt","arguments":{"name":"Ann"}}`)
		assert.Nil(t, sent.Response)
		assert.EqualError(t, sent.Error, "internal error: prompt panicked")
	})

	t.Run("neither response nor error", func(t *testing.T) {
		sent := getPrompt(t, srv, `{"name":"empty_prompt","arguments":{"name":"Ann"}}`)
		assert.Nil(t, sent.Response)
		assert.EqualError(t, sent.Error, "prompt handler returned neither a response nor an error")
	})

	t.Run("unknown prompt", func(t *testing.T) {
		resp, err := srv.handlePromptCalls(context.Background(), &transport.BaseJSONRPCRequest{
			Method: "prompts/get",
			Params: []byte(`{"name":"nope","arguments":{"name":"Ann"}}`),
		}, protocol.RequestHandlerExtra{})
		assert.EqualError(t, err, "unknown prompt: nope")
		assert.Nil(t, resp)
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
	require.NoError(t, srv.RegisterResource(panicResourceURI, "panics", "Panics", mimeTextPlain, func() (*ResourceResponse, error) {
		panic("kaboom")
	}))
	require.NoError(t, srv.RegisterResource(emptyResourceURI, "empty", "Empty", mimeTextPlain, func() (*ResourceResponse, error) {
		return nil, nil
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
	// F-048: the error resource carries the URI that was read.
	b, err := json.Marshal(failed)
	require.NoError(t, err)
	assert.JSONEq(t, `{"contents":[{"uri":"`+failResourceURI+`","mimeType":"text/plain","text":"boom"}]}`, string(b))

	panicked := read(context.Background(), panicResourceURI)
	assert.Nil(t, panicked.Response)
	assert.Equal(t, panicResourceURI, panicked.Uri)
	assert.EqualError(t, panicked.Error, "internal error: resource panicked")

	empty := read(context.Background(), emptyResourceURI)
	assert.Nil(t, empty.Response)
	assert.Equal(t, emptyResourceURI, empty.Uri)
	assert.EqualError(t, empty.Error, "resource handler returned neither a response nor an error")

	resp, err := srv.handleResourceCalls(context.Background(), &transport.BaseJSONRPCRequest{
		Method: "resources/read",
		Params: []byte(`{"uri":"` + missingResourceURI + `"}`),
	}, protocol.RequestHandlerExtra{})
	assert.EqualError(t, err, "unknown resource: "+missingResourceURI)
	assert.Nil(t, resp)
}

// A prompt may take no arguments, as MCP allows; it used to be rejected.
func TestHandlePromptCalls_HandlerWithoutArguments(t *testing.T) {
	tcs := []struct {
		name    string
		handler any
	}{
		{
			name: "no parameters",
			handler: func() (*PromptResponse, error) {
				return NewPromptResponse("fixed", NewPromptMessage(NewTextContent("Hello"), RoleUser)), nil
			},
		},
		{
			name: "context only",
			handler: func(context.Context) (*PromptResponse, error) {
				return NewPromptResponse("fixed", NewPromptMessage(NewTextContent("Hello"), RoleUser)), nil
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newServedServer(t)
			require.NoError(t, srv.RegisterPrompt(greetPrompt, greetPromptDesc, tc.handler))

			p, ok := srv.prompts.Load(greetPrompt)
			require.True(t, ok)
			b, err := json.Marshal(p.PromptInputSchema)
			require.NoError(t, err)
			assert.JSONEq(t, `{"name":"greet","description":"Greets a person."}`, string(b))

			sent := getPrompt(t, srv, `{"name":"greet","arguments":{"ignored":"x"}}`)
			require.NoError(t, sent.Error)
			require.Len(t, sent.Response.Messages, 1)
			assert.Equal(t, "Hello", sent.Response.Messages[0].Content.TextContent.Text)
		})
	}
}

func TestNewPromptSchema(t *testing.T) {
	type args struct {
		Topic string  `json:"topic" jsonschema:"required,description=What to write about"`
		Tone  *string `json:"tone,omitempty"`
	}
	ps := newPromptSchema(greetPrompt, greetPromptDesc, reflect.TypeFor[args]())
	b, err := json.Marshal(ps)
	require.NoError(t, err)
	// An argument without a description has no "description" key.
	assert.JSONEq(t, `{
		"name": "greet",
		"description": "Greets a person.",
		"arguments": [
			{"name": "topic", "description": "What to write about", "required": true},
			{"name": "tone"}
		]
	}`, string(b))
}

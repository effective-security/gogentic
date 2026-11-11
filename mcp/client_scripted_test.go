package mcp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/mcp/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests of the client against a scripted transport: initialization, request failures and argument encoding.

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

func (s *scriptedTransport) Close() error { return nil }

func (s *scriptedTransport) SetCloseHandler(func()) {}

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

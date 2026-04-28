package httptransport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/effective-security/gogentic/mcp/transport"
	"github.com/effective-security/gogentic/mcp/transport/httptransport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func marshalRequest(t *testing.T, method string, id int, params any) []byte {
	t.Helper()
	p, _ := json.Marshal(params)
	req := transport.BaseJSONRPCRequest{
		Jsonrpc: "2.0",
		Method:  method,
		Id:      transport.RequestId(id),
		Params:  p,
	}
	b, err := json.Marshal(req)
	require.NoError(t, err)
	return b
}

func marshalNotification(t *testing.T, method string, params any) []byte {
	t.Helper()
	p, _ := json.Marshal(params)
	n := transport.BaseJSONRPCNotification{
		Jsonrpc: "2.0",
		Method:  method,
		Params:  p,
	}
	b, err := json.Marshal(n)
	require.NoError(t, err)
	return b
}

// ---------------------------------------------------------------------------
// HTTPTransport (server side) tests
// ---------------------------------------------------------------------------

func TestHTTPTransport_New(t *testing.T) {
	tr := httptransport.NewHTTPTransport("/mcp")
	assert.NotNil(t, tr)
}

func TestHTTPTransport_HandleRequest_NotPost(t *testing.T) {
	tr := httptransport.NewHTTPTransport("/mcp")
	tr.SetMessageHandler(func(_ context.Context, _ *transport.BaseJsonRpcMessage) {})

	server := httptest.NewServer(tr)
	defer server.Close()

	resp, err := http.Get(server.URL + "/mcp")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestHTTPTransport_HandleRequest_Notification_Returns202(t *testing.T) {
	tr := httptransport.NewHTTPTransport("/mcp")

	var receivedMsg *transport.BaseJsonRpcMessage
	tr.SetMessageHandler(func(_ context.Context, msg *transport.BaseJsonRpcMessage) {
		receivedMsg = msg
	})

	server := httptest.NewServer(tr)
	defer server.Close()

	body := marshalNotification(t, "notifications/initialized", nil)
	resp, err := http.Post(server.URL+"/mcp", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	respBody, _ := io.ReadAll(resp.Body)
	assert.Empty(t, respBody)

	require.NotNil(t, receivedMsg)
	assert.Equal(t, transport.BaseMessageTypeJSONRPCNotificationType, receivedMsg.Type)
	assert.Equal(t, "notifications/initialized", receivedMsg.JsonRpcNotification.Method)
}

func TestHTTPTransport_HandleRequest_NotificationWithParams(t *testing.T) {
	tr := httptransport.NewHTTPTransport("/mcp")

	var receivedMsg *transport.BaseJsonRpcMessage
	tr.SetMessageHandler(func(_ context.Context, msg *transport.BaseJsonRpcMessage) {
		receivedMsg = msg
	})

	server := httptest.NewServer(tr)
	defer server.Close()

	body := marshalNotification(t, "notifications/cancelled", map[string]any{"requestId": 1, "reason": "timeout"})
	resp, err := http.Post(server.URL+"/mcp", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	require.NotNil(t, receivedMsg)
	assert.Equal(t, transport.BaseMessageTypeJSONRPCNotificationType, receivedMsg.Type)
	assert.NotEmpty(t, receivedMsg.JsonRpcNotification.Params)
}

func TestHTTPTransport_HandleRequest_RequestResponseRoundtrip(t *testing.T) {
	tr := httptransport.NewHTTPTransport("/mcp")

	tr.SetMessageHandler(func(ctx context.Context, msg *transport.BaseJsonRpcMessage) {
		if msg.Type != transport.BaseMessageTypeJSONRPCRequestType {
			return
		}
		// Send must be called from a separate goroutine — the protocol layer always
		// dispatches handlers asynchronously, and handleMessage blocks on the receive
		// until Send delivers the response.
		go func() {
			result, _ := json.Marshal(map[string]string{"echo": msg.JsonRpcRequest.Method})
			_ = tr.Send(ctx, &transport.BaseJsonRpcMessage{
				Type: transport.BaseMessageTypeJSONRPCResponseType,
				JsonRpcResponse: &transport.BaseJSONRPCResponse{
					Jsonrpc: "2.0",
					Id:      msg.JsonRpcRequest.Id,
					Result:  result,
				},
			})
		}()
	})

	server := httptest.NewServer(tr)
	defer server.Close()

	body := marshalRequest(t, "tools/list", 42, nil)
	resp, err := http.Post(server.URL+"/mcp", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var rpcResp transport.BaseJSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	assert.Equal(t, transport.RequestId(42), rpcResp.Id)

	var result map[string]string
	require.NoError(t, json.Unmarshal(rpcResp.Result, &result))
	assert.Equal(t, "tools/list", result["echo"])
}

func TestHTTPTransport_HandleRequest_OriginalIDRestored(t *testing.T) {
	// The server internally remaps request IDs to an atomic counter for correlation;
	// the original client-supplied ID must be restored in the response.
	tr := httptransport.NewHTTPTransport("/mcp")

	tr.SetMessageHandler(func(ctx context.Context, msg *transport.BaseJsonRpcMessage) {
		if msg.Type != transport.BaseMessageTypeJSONRPCRequestType {
			return
		}
		go func() {
			_ = tr.Send(ctx, &transport.BaseJsonRpcMessage{
				Type: transport.BaseMessageTypeJSONRPCResponseType,
				JsonRpcResponse: &transport.BaseJSONRPCResponse{
					Jsonrpc: "2.0",
					Id:      msg.JsonRpcRequest.Id, // use the remapped (internal) id
					Result:  []byte(`{}`),
				},
			})
		}()
	})

	server := httptest.NewServer(tr)
	defer server.Close()

	const clientID = 999
	body := marshalRequest(t, "ping", clientID, nil)
	resp, err := http.Post(server.URL+"/mcp", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	var rpcResp transport.BaseJSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	assert.Equal(t, transport.RequestId(clientID), rpcResp.Id)
}

func TestHTTPTransport_HandleRequest_InvalidJSON(t *testing.T) {
	tr := httptransport.NewHTTPTransport("/mcp")

	server := httptest.NewServer(tr)
	defer server.Close()

	resp, err := http.Post(server.URL+"/mcp", "application/json", bytes.NewReader([]byte("not json")))
	require.NoError(t, err)
	defer resp.Body.Close()
	// Unrecognised body: no channel is created, so we get 202 (treated as no-op)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
}

func TestHTTPTransport_Send_NoChannelReturnsError(t *testing.T) {
	tr := httptransport.NewHTTPTransport("/mcp")
	err := tr.Send(context.Background(), &transport.BaseJsonRpcMessage{
		Type: transport.BaseMessageTypeJSONRPCResponseType,
		JsonRpcResponse: &transport.BaseJSONRPCResponse{
			Jsonrpc: "2.0",
			Id:      transport.RequestId(9999),
			Result:  []byte(`{}`),
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no response channel found")
}

func TestHTTPTransport_Send_NotificationIsNoOp(t *testing.T) {
	tr := httptransport.NewHTTPTransport("/mcp")
	err := tr.Send(context.Background(), &transport.BaseJsonRpcMessage{
		Type: transport.BaseMessageTypeJSONRPCNotificationType,
		JsonRpcNotification: &transport.BaseJSONRPCNotification{
			Jsonrpc: "2.0",
			Method:  "notifications/tools/list_changed",
		},
	})
	assert.NoError(t, err)
}

func TestHTTPTransport_Close(t *testing.T) {
	tr := httptransport.NewHTTPTransport("/mcp")

	closeCalled := false
	tr.SetCloseHandler(func() { closeCalled = true })

	// Close without a running server should not panic
	require.NoError(t, tr.Close())
	assert.True(t, closeCalled)
}

func TestHTTPTransport_SetErrorHandler(t *testing.T) {
	tr := httptransport.NewHTTPTransport("/mcp")
	var gotErr error
	tr.SetErrorHandler(func(e error) { gotErr = e })

	// Trigger an error by calling Send with an unknown key
	_ = tr.Send(context.Background(), &transport.BaseJsonRpcMessage{
		Type: transport.BaseMessageTypeJSONRPCResponseType,
		JsonRpcResponse: &transport.BaseJSONRPCResponse{
			Id: transport.RequestId(1),
		},
	})
	// Error is returned directly from Send, not through the error handler
	assert.Nil(t, gotErr)
}

func TestHTTPTransport_AllMCPNotificationsReturn202(t *testing.T) {
	notifications := []string{
		"notifications/initialized",
		"notifications/cancelled",
		"notifications/tools/list_changed",
		"notifications/prompts/list_changed",
		"notifications/resources/list_changed",
	}

	for _, method := range notifications {
		t.Run(method, func(t *testing.T) {
			tr := httptransport.NewHTTPTransport("/mcp")
			tr.SetMessageHandler(func(_ context.Context, _ *transport.BaseJsonRpcMessage) {})

			server := httptest.NewServer(tr)
			defer server.Close()

			body := marshalNotification(t, method, nil)
			resp, err := http.Post(server.URL+"/mcp", "application/json", bytes.NewReader(body))
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusAccepted, resp.StatusCode)
		})
	}
}

// ---------------------------------------------------------------------------
// HTTPClientTransport (client side) tests
// ---------------------------------------------------------------------------

func TestHTTPClientTransport_New(t *testing.T) {
	tr := httptransport.NewHTTPClientTransport("/mcp")
	assert.NotNil(t, tr)
}

func TestHTTPClientTransport_Start_IsNoOp(t *testing.T) {
	tr := httptransport.NewHTTPClientTransport("/mcp")
	err := tr.Start(context.Background())
	assert.NoError(t, err)
}

func TestHTTPClientTransport_Send_Request_ReceivesResponse(t *testing.T) {
	// Fake server that returns a JSON-RPC response for any POST
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req transport.BaseJSONRPCRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))

		result, _ := json.Marshal(map[string]string{"status": "ok"})
		resp := transport.BaseJSONRPCResponse{
			Jsonrpc: "2.0",
			Id:      req.Id,
			Result:  result,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tr := httptransport.NewHTTPClientTransport("/mcp").WithBaseURL(srv.URL)

	var receivedMsg *transport.BaseJsonRpcMessage
	tr.SetMessageHandler(func(_ context.Context, msg *transport.BaseJsonRpcMessage) {
		receivedMsg = msg
	})

	err := tr.Send(context.Background(), &transport.BaseJsonRpcMessage{
		Type: transport.BaseMessageTypeJSONRPCRequestType,
		JsonRpcRequest: &transport.BaseJSONRPCRequest{
			Jsonrpc: "2.0",
			Method:  "tools/list",
			Id:      transport.RequestId(1),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, receivedMsg)
	assert.Equal(t, transport.BaseMessageTypeJSONRPCResponseType, receivedMsg.Type)
	assert.Equal(t, transport.RequestId(1), receivedMsg.JsonRpcResponse.Id)
}

func TestHTTPClientTransport_Send_Notification_Receives202(t *testing.T) {
	// Server that returns 202 for notifications (correct server behaviour after our fix)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	tr := httptransport.NewHTTPClientTransport("/mcp").WithBaseURL(srv.URL)

	var receivedMsg *transport.BaseJsonRpcMessage
	tr.SetMessageHandler(func(_ context.Context, msg *transport.BaseJsonRpcMessage) {
		receivedMsg = msg
	})

	err := tr.Send(context.Background(), &transport.BaseJsonRpcMessage{
		Type: transport.BaseMessageTypeJSONRPCNotificationType,
		JsonRpcNotification: &transport.BaseJSONRPCNotification{
			Jsonrpc: "2.0",
			Method:  "notifications/initialized",
		},
	})
	require.NoError(t, err)
	// 202 Accepted → client should not dispatch any message
	assert.Nil(t, receivedMsg)
}

func TestHTTPClientTransport_Send_Notification_Receives204(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	tr := httptransport.NewHTTPClientTransport("/mcp").WithBaseURL(srv.URL)

	err := tr.Send(context.Background(), &transport.BaseJsonRpcMessage{
		Type: transport.BaseMessageTypeJSONRPCNotificationType,
		JsonRpcNotification: &transport.BaseJSONRPCNotification{
			Jsonrpc: "2.0",
			Method:  "notifications/initialized",
		},
	})
	assert.NoError(t, err)
}

func TestHTTPClientTransport_Send_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	tr := httptransport.NewHTTPClientTransport("/mcp").WithBaseURL(srv.URL)
	err := tr.Send(context.Background(), &transport.BaseJsonRpcMessage{
		Type: transport.BaseMessageTypeJSONRPCRequestType,
		JsonRpcRequest: &transport.BaseJSONRPCRequest{
			Jsonrpc: "2.0",
			Method:  "ping",
			Id:      transport.RequestId(1),
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestHTTPClientTransport_Send_ReceivesErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		errResp := transport.BaseJSONRPCError{
			Jsonrpc: "2.0",
			Id:      transport.RequestId(1),
			Error: transport.BaseJSONRPCErrorInner{
				Code:    -32601,
				Message: "method not found",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(errResp)
	}))
	defer srv.Close()

	tr := httptransport.NewHTTPClientTransport("/mcp").WithBaseURL(srv.URL)

	var receivedMsg *transport.BaseJsonRpcMessage
	tr.SetMessageHandler(func(_ context.Context, msg *transport.BaseJsonRpcMessage) {
		receivedMsg = msg
	})

	err := tr.Send(context.Background(), &transport.BaseJsonRpcMessage{
		Type: transport.BaseMessageTypeJSONRPCRequestType,
		JsonRpcRequest: &transport.BaseJSONRPCRequest{
			Jsonrpc: "2.0",
			Method:  "unknown/method",
			Id:      transport.RequestId(1),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, receivedMsg)
	assert.Equal(t, transport.BaseMessageTypeJSONRPCErrorType, receivedMsg.Type)
	assert.Equal(t, -32601, receivedMsg.JsonRpcError.Error.Code)
	assert.Equal(t, "method not found", receivedMsg.JsonRpcError.Error.Message)
}

func TestHTTPClientTransport_Send_EmptyResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write nothing
	}))
	defer srv.Close()

	tr := httptransport.NewHTTPClientTransport("/mcp").WithBaseURL(srv.URL)

	var receivedMsg *transport.BaseJsonRpcMessage
	tr.SetMessageHandler(func(_ context.Context, msg *transport.BaseJsonRpcMessage) {
		receivedMsg = msg
	})

	err := tr.Send(context.Background(), &transport.BaseJsonRpcMessage{
		Type: transport.BaseMessageTypeJSONRPCRequestType,
		JsonRpcRequest: &transport.BaseJSONRPCRequest{
			Jsonrpc: "2.0",
			Method:  "ping",
			Id:      transport.RequestId(1),
		},
	})
	require.NoError(t, err)
	assert.Nil(t, receivedMsg)
}

func TestHTTPClientTransport_WithCustomClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, _ := json.Marshal(map[string]string{"ok": "true"})
		resp := transport.BaseJSONRPCResponse{Jsonrpc: "2.0", Id: 1, Result: result}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tr := httptransport.NewHTTPClientTransport("/mcp").
		WithBaseURL(srv.URL).
		WithClient(srv.Client()) // inject test client

	err := tr.Send(context.Background(), &transport.BaseJsonRpcMessage{
		Type: transport.BaseMessageTypeJSONRPCRequestType,
		JsonRpcRequest: &transport.BaseJSONRPCRequest{
			Jsonrpc: "2.0",
			Method:  "ping",
			Id:      transport.RequestId(1),
		},
	})
	assert.NoError(t, err)
}

func TestHTTPClientTransport_WithHeaders(t *testing.T) {
	var receivedHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("X-Api-Key")
		result, _ := json.Marshal(map[string]string{})
		resp := transport.BaseJSONRPCResponse{Jsonrpc: "2.0", Id: 1, Result: result}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tr := httptransport.NewHTTPClientTransport("/mcp").
		WithBaseURL(srv.URL).
		WithHeader("X-Api-Key", "secret")

	_ = tr.Send(context.Background(), &transport.BaseJsonRpcMessage{
		Type:           transport.BaseMessageTypeJSONRPCRequestType,
		JsonRpcRequest: &transport.BaseJSONRPCRequest{Jsonrpc: "2.0", Method: "ping", Id: 1},
	})
	assert.Equal(t, "secret", receivedHeader)
}

func TestHTTPClientTransport_Close(t *testing.T) {
	tr := httptransport.NewHTTPClientTransport("/mcp")

	closeCalled := false
	tr.SetCloseHandler(func() { closeCalled = true })

	require.NoError(t, tr.Close())
	assert.True(t, closeCalled)
}

// ---------------------------------------------------------------------------
// Integration: server + client paired together
// ---------------------------------------------------------------------------

func TestHTTPTransport_Integration_RequestResponse(t *testing.T) {
	// Wire up a real HTTPTransport server and an HTTPClientTransport client.
	serverTransport := httptransport.NewHTTPTransport("/mcp")

	serverTransport.SetMessageHandler(func(ctx context.Context, msg *transport.BaseJsonRpcMessage) {
		if msg.Type != transport.BaseMessageTypeJSONRPCRequestType {
			return
		}
		go func() {
			result, _ := json.Marshal(map[string]string{"reply": "pong"})
			_ = serverTransport.Send(ctx, &transport.BaseJsonRpcMessage{
				Type: transport.BaseMessageTypeJSONRPCResponseType,
				JsonRpcResponse: &transport.BaseJSONRPCResponse{
					Jsonrpc: "2.0",
					Id:      msg.JsonRpcRequest.Id,
					Result:  result,
				},
			})
		}()
	})

	srv := httptest.NewServer(serverTransport)
	defer srv.Close()

	clientTransport := httptransport.NewHTTPClientTransport("/mcp").WithBaseURL(srv.URL)
	require.NoError(t, clientTransport.Start(context.Background()))

	var clientReceived *transport.BaseJsonRpcMessage
	clientTransport.SetMessageHandler(func(_ context.Context, msg *transport.BaseJsonRpcMessage) {
		clientReceived = msg
	})

	err := clientTransport.Send(context.Background(), &transport.BaseJsonRpcMessage{
		Type: transport.BaseMessageTypeJSONRPCRequestType,
		JsonRpcRequest: &transport.BaseJSONRPCRequest{
			Jsonrpc: "2.0",
			Method:  "ping",
			Id:      transport.RequestId(7),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, clientReceived)
	assert.Equal(t, transport.BaseMessageTypeJSONRPCResponseType, clientReceived.Type)
	assert.Equal(t, transport.RequestId(7), clientReceived.JsonRpcResponse.Id)

	var result map[string]string
	require.NoError(t, json.Unmarshal(clientReceived.JsonRpcResponse.Result, &result))
	assert.Equal(t, "pong", result["reply"])
}

func TestHTTPTransport_Integration_NotificationFlow(t *testing.T) {
	// After an initialize exchange, the client sends notifications/initialized.
	// The server must accept it (202) without crashing.
	serverTransport := httptransport.NewHTTPTransport("/mcp")

	var notificationReceived bool
	serverTransport.SetMessageHandler(func(ctx context.Context, msg *transport.BaseJsonRpcMessage) {
		switch msg.Type {
		case transport.BaseMessageTypeJSONRPCRequestType:
			go func() {
				result, _ := json.Marshal(map[string]any{
					"protocolVersion": "2025-06-18",
					"capabilities":    map[string]any{},
					"serverInfo":      map[string]string{"name": "test", "version": "1"},
				})
				_ = serverTransport.Send(ctx, &transport.BaseJsonRpcMessage{
					Type: transport.BaseMessageTypeJSONRPCResponseType,
					JsonRpcResponse: &transport.BaseJSONRPCResponse{
						Jsonrpc: "2.0",
						Id:      msg.JsonRpcRequest.Id,
						Result:  result,
					},
				})
			}()
		case transport.BaseMessageTypeJSONRPCNotificationType:
			if msg.JsonRpcNotification.Method == "notifications/initialized" {
				notificationReceived = true
			}
		}
	})

	srv := httptest.NewServer(serverTransport)
	defer srv.Close()

	clientTransport := httptransport.NewHTTPClientTransport("/mcp").WithBaseURL(srv.URL)
	clientTransport.SetMessageHandler(func(_ context.Context, _ *transport.BaseJsonRpcMessage) {})

	// Step 1: initialize request
	err := clientTransport.Send(context.Background(), &transport.BaseJsonRpcMessage{
		Type: transport.BaseMessageTypeJSONRPCRequestType,
		JsonRpcRequest: &transport.BaseJSONRPCRequest{
			Jsonrpc: "2.0",
			Method:  "initialize",
			Id:      transport.RequestId(1),
		},
	})
	require.NoError(t, err)

	// Step 2: notifications/initialized — must not error
	err = clientTransport.Send(context.Background(), &transport.BaseJsonRpcMessage{
		Type: transport.BaseMessageTypeJSONRPCNotificationType,
		JsonRpcNotification: &transport.BaseJSONRPCNotification{
			Jsonrpc: "2.0",
			Method:  "notifications/initialized",
		},
	})
	require.NoError(t, err)

	// Give the server handler goroutine a moment to process
	time.Sleep(5 * time.Millisecond)
	assert.True(t, notificationReceived)
}

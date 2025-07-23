package localtransport_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/effective-security/gogentic/mcp/transport"
	"github.com/effective-security/gogentic/mcp/transport/localtransport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockHandler implements the Handler interface for testing
type mockHandler struct {
	handleFunc func(ctx context.Context, req *localtransport.McpProxyRequest) (*localtransport.McpProxyResponse, error)
}

func (m *mockHandler) HandleMCP(ctx context.Context, req *localtransport.McpProxyRequest) (*localtransport.McpProxyResponse, error) {
	if m.handleFunc != nil {
		return m.handleFunc(ctx, req)
	}
	return &localtransport.McpProxyResponse{
		Status: http.StatusOK,
		Body:   []byte(`{"jsonrpc":"2.0","result":{"status":"ok"},"id":1}`),
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}, nil
}

func TestNewLocalClientTransport(t *testing.T) {
	handler := &mockHandler{}
	client := localtransport.NewLocalClientTransport(handler)

	assert.NotNil(t, client)
	// We can't directly access unexported fields, but we can test the behavior
	// by calling methods that use them
}

func TestLocalMcpClientTransport_WithHeader(t *testing.T) {
	handler := &mockHandler{}
	client := localtransport.NewLocalClientTransport(handler)

	// Test chaining
	result := client.WithHeader("Authorization", "Bearer token")
	assert.Equal(t, client, result)

	// Test header was added by sending a message and checking if it's passed to handler
	handler.handleFunc = func(ctx context.Context, req *localtransport.McpProxyRequest) (*localtransport.McpProxyResponse, error) {
		assert.Equal(t, "Bearer token", req.Headers["Authorization"])
		return &localtransport.McpProxyResponse{
			Status:  http.StatusOK,
			Body:    []byte(`{"jsonrpc":"2.0","result":{"status":"ok"},"id":1}`),
			Headers: map[string]string{},
		}, nil
	}

	message := &transport.BaseJsonRpcMessage{
		Type: transport.BaseMessageTypeJSONRPCRequestType,
		JsonRpcRequest: &transport.BaseJSONRPCRequest{
			Jsonrpc: "2.0",
			Method:  "test_method",
			Id:      transport.RequestId(1),
		},
	}

	err := client.Send(context.Background(), message)
	assert.NoError(t, err)

	// Test multiple headers
	client.WithHeader("Content-Type", "application/json")
	handler.handleFunc = func(ctx context.Context, req *localtransport.McpProxyRequest) (*localtransport.McpProxyResponse, error) {
		assert.Equal(t, "Bearer token", req.Headers["Authorization"])
		assert.Equal(t, "application/json", req.Headers["Content-Type"])
		return &localtransport.McpProxyResponse{
			Status:  http.StatusOK,
			Body:    []byte(`{"jsonrpc":"2.0","result":{"status":"ok"},"id":1}`),
			Headers: map[string]string{},
		}, nil
	}

	err = client.Send(context.Background(), message)
	assert.NoError(t, err)
}

func TestLocalMcpClientTransport_Start(t *testing.T) {
	handler := &mockHandler{}
	client := localtransport.NewLocalClientTransport(handler)

	ctx := context.Background()
	err := client.Start(ctx)
	assert.NoError(t, err)
}

func TestLocalMcpClientTransport_Send(t *testing.T) {
	t.Run("successful send with response", func(t *testing.T) {
		handler := &mockHandler{
			handleFunc: func(ctx context.Context, req *localtransport.McpProxyRequest) (*localtransport.McpProxyResponse, error) {
				// Verify request structure
				assert.NotNil(t, req.Body)
				assert.Equal(t, "Bearer token", req.Headers["Authorization"])

				// Return a valid JSON-RPC response
				return &localtransport.McpProxyResponse{
					Status: http.StatusOK,
					Body:   []byte(`{"jsonrpc":"2.0","result":{"status":"ok"},"id":1}`),
					Headers: map[string]string{
						"Content-Type": "application/json",
					},
				}, nil
			},
		}

		client := localtransport.NewLocalClientTransport(handler)
		client.WithHeader("Authorization", "Bearer token")

		message := &transport.BaseJsonRpcMessage{
			Type: transport.BaseMessageTypeJSONRPCRequestType,
			JsonRpcRequest: &transport.BaseJSONRPCRequest{
				Jsonrpc: "2.0",
				Method:  "test_method",
				Id:      transport.RequestId(1),
			},
		}

		err := client.Send(context.Background(), message)
		assert.NoError(t, err)
	})

	t.Run("successful send with error response", func(t *testing.T) {
		handler := &mockHandler{
			handleFunc: func(ctx context.Context, req *localtransport.McpProxyRequest) (*localtransport.McpProxyResponse, error) {
				return &localtransport.McpProxyResponse{
					Status: http.StatusOK,
					Body:   []byte(`{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found"},"id":1}`),
					Headers: map[string]string{
						"Content-Type": "application/json",
					},
				}, nil
			},
		}

		client := localtransport.NewLocalClientTransport(handler)
		var receivedMessage *transport.BaseJsonRpcMessage
		client.SetMessageHandler(func(ctx context.Context, msg *transport.BaseJsonRpcMessage) {
			receivedMessage = msg
		})

		message := &transport.BaseJsonRpcMessage{
			Type: transport.BaseMessageTypeJSONRPCRequestType,
			JsonRpcRequest: &transport.BaseJSONRPCRequest{
				Jsonrpc: "2.0",
				Method:  "test_method",
				Id:      transport.RequestId(1),
			},
		}

		err := client.Send(context.Background(), message)
		assert.NoError(t, err)
		assert.NotNil(t, receivedMessage)
		assert.Equal(t, transport.BaseMessageTypeJSONRPCErrorType, receivedMessage.Type)
	})

	t.Run("successful send with empty response body", func(t *testing.T) {
		handler := &mockHandler{
			handleFunc: func(ctx context.Context, req *localtransport.McpProxyRequest) (*localtransport.McpProxyResponse, error) {
				return &localtransport.McpProxyResponse{
					Status:  http.StatusOK,
					Body:    []byte{},
					Headers: map[string]string{},
				}, nil
			},
		}

		client := localtransport.NewLocalClientTransport(handler)

		message := &transport.BaseJsonRpcMessage{
			Type: transport.BaseMessageTypeJSONRPCRequestType,
			JsonRpcRequest: &transport.BaseJSONRPCRequest{
				Jsonrpc: "2.0",
				Method:  "test_method",
				Id:      transport.RequestId(1),
			},
		}

		err := client.Send(context.Background(), message)
		assert.NoError(t, err)
	})

	t.Run("handler returns error", func(t *testing.T) {
		handler := &mockHandler{
			handleFunc: func(ctx context.Context, req *localtransport.McpProxyRequest) (*localtransport.McpProxyResponse, error) {
				return nil, assert.AnError
			},
		}

		client := localtransport.NewLocalClientTransport(handler)

		message := &transport.BaseJsonRpcMessage{
			Type: transport.BaseMessageTypeJSONRPCRequestType,
			JsonRpcRequest: &transport.BaseJSONRPCRequest{
				Jsonrpc: "2.0",
				Method:  "test_method",
				Id:      transport.RequestId(1),
			},
		}

		err := client.Send(context.Background(), message)
		require.Error(t, err)
		assert.Equal(t, assert.AnError, err)
	})

	t.Run("handler returns non-OK status", func(t *testing.T) {
		handler := &mockHandler{
			handleFunc: func(ctx context.Context, req *localtransport.McpProxyRequest) (*localtransport.McpProxyResponse, error) {
				return &localtransport.McpProxyResponse{
					Status:  http.StatusInternalServerError,
					Body:    []byte(`{"error":"internal server error"}`),
					Headers: map[string]string{},
				}, nil
			},
		}

		client := localtransport.NewLocalClientTransport(handler)

		message := &transport.BaseJsonRpcMessage{
			Type: transport.BaseMessageTypeJSONRPCRequestType,
			JsonRpcRequest: &transport.BaseJSONRPCRequest{
				Jsonrpc: "2.0",
				Method:  "test_method",
				Id:      transport.RequestId(1),
			},
		}

		err := client.Send(context.Background(), message)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "server returned error: 500")
	})
}

func TestLocalMcpClientTransport_Close(t *testing.T) {
	t.Run("close with handler", func(t *testing.T) {
		handler := &mockHandler{}
		client := localtransport.NewLocalClientTransport(handler)
		closeCalled := false

		client.SetCloseHandler(func() {
			closeCalled = true
		})

		err := client.Close()
		assert.NoError(t, err)
		assert.True(t, closeCalled)
	})

	t.Run("close without handler", func(t *testing.T) {
		handler := &mockHandler{}
		client := localtransport.NewLocalClientTransport(handler)

		err := client.Close()
		assert.NoError(t, err)
	})

	t.Run("close multiple times", func(t *testing.T) {
		handler := &mockHandler{}
		client := localtransport.NewLocalClientTransport(handler)
		closeCount := 0

		client.SetCloseHandler(func() {
			closeCount++
		})

		err := client.Close()
		assert.NoError(t, err)
		assert.Equal(t, 1, closeCount)

		err = client.Close()
		assert.NoError(t, err)
		assert.Equal(t, 2, closeCount)
	})
}

func TestLocalMcpClientTransport_SetCloseHandler(t *testing.T) {
	handler := &mockHandler{}
	client := localtransport.NewLocalClientTransport(handler)

	handlerCalled := false
	handlerFunc := func() {
		handlerCalled = true
	}

	client.SetCloseHandler(handlerFunc)

	// Test the handler by calling Close
	err := client.Close()
	assert.NoError(t, err)
	assert.True(t, handlerCalled)
}

func TestLocalMcpClientTransport_SetErrorHandler(t *testing.T) {
	handler := &mockHandler{}
	client := localtransport.NewLocalClientTransport(handler)

	handlerFunc := func(err error) {
		// Error handler implementation
	}

	client.SetErrorHandler(handlerFunc)

	// Test that the method doesn't panic
	assert.NotPanics(t, func() {
		client.SetErrorHandler(handlerFunc)
	})
}

func TestLocalMcpClientTransport_SetMessageHandler(t *testing.T) {
	handler := &mockHandler{}
	client := localtransport.NewLocalClientTransport(handler)

	var receivedMessage *transport.BaseJsonRpcMessage
	var receivedContext context.Context
	handlerFunc := func(ctx context.Context, message *transport.BaseJsonRpcMessage) {
		receivedContext = ctx
		receivedMessage = message
	}

	client.SetMessageHandler(handlerFunc)

	// Test the handler by sending a message that triggers a response
	handler.handleFunc = func(ctx context.Context, req *localtransport.McpProxyRequest) (*localtransport.McpProxyResponse, error) {
		return &localtransport.McpProxyResponse{
			Status: http.StatusOK,
			Body:   []byte(`{"jsonrpc":"2.0","result":{"status":"ok"},"id":1}`),
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
		}, nil
	}

	testCtx := context.Background()
	message := &transport.BaseJsonRpcMessage{
		Type: transport.BaseMessageTypeJSONRPCRequestType,
		JsonRpcRequest: &transport.BaseJSONRPCRequest{
			Jsonrpc: "2.0",
			Method:  "test_method",
			Id:      transport.RequestId(1),
		},
	}

	err := client.Send(testCtx, message)
	assert.NoError(t, err)

	// Verify the message handler was called
	assert.NotNil(t, receivedMessage)
	assert.Equal(t, testCtx, receivedContext)
	assert.Equal(t, transport.BaseMessageTypeJSONRPCResponseType, receivedMessage.Type)
}

func TestLocalMcpClientTransport_Concurrency(t *testing.T) {
	handler := &mockHandler{
		handleFunc: func(ctx context.Context, req *localtransport.McpProxyRequest) (*localtransport.McpProxyResponse, error) {
			return &localtransport.McpProxyResponse{
				Status: http.StatusOK,
				Body:   []byte(`{"jsonrpc":"2.0","result":{"status":"ok"},"id":1}`),
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
			}, nil
		},
	}

	client := localtransport.NewLocalClientTransport(handler)

	const numGoroutines = 10
	results := make(chan error, numGoroutines)

	// Start multiple goroutines sending messages concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			message := &transport.BaseJsonRpcMessage{
				Type: transport.BaseMessageTypeJSONRPCRequestType,
				JsonRpcRequest: &transport.BaseJSONRPCRequest{
					Jsonrpc: "2.0",
					Method:  "test_method",
					Id:      transport.RequestId(id),
				},
			}

			err := client.Send(context.Background(), message)
			results <- err
		}(i)
	}

	// Collect results
	for i := 0; i < numGoroutines; i++ {
		err := <-results
		assert.NoError(t, err)
	}
}

func TestLocalMcpClientTransport_MessageHandlerConcurrency(t *testing.T) {
	handler := &mockHandler{
		handleFunc: func(ctx context.Context, req *localtransport.McpProxyRequest) (*localtransport.McpProxyResponse, error) {
			return &localtransport.McpProxyResponse{
				Status: http.StatusOK,
				Body:   []byte(`{"jsonrpc":"2.0","result":{"status":"ok"},"id":1}`),
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
			}, nil
		},
	}

	client := localtransport.NewLocalClientTransport(handler)

	messageCount := 0
	var mu sync.Mutex
	handlerFunc := func(ctx context.Context, message *transport.BaseJsonRpcMessage) {
		mu.Lock()
		messageCount++
		mu.Unlock()
	}

	client.SetMessageHandler(handlerFunc)

	const numGoroutines = 10
	results := make(chan error, numGoroutines)

	// Start multiple goroutines sending messages concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			message := &transport.BaseJsonRpcMessage{
				Type: transport.BaseMessageTypeJSONRPCRequestType,
				JsonRpcRequest: &transport.BaseJSONRPCRequest{
					Jsonrpc: "2.0",
					Method:  "test_method",
					Id:      transport.RequestId(id),
				},
			}

			err := client.Send(context.Background(), message)
			results <- err
		}(i)
	}

	// Collect results
	for i := 0; i < numGoroutines; i++ {
		err := <-results
		assert.NoError(t, err)
	}

	// Verify all messages were handled
	assert.Equal(t, numGoroutines, messageCount)
}

func TestLocalMcpClientTransport_Headers(t *testing.T) {
	handler := &mockHandler{
		handleFunc: func(ctx context.Context, req *localtransport.McpProxyRequest) (*localtransport.McpProxyResponse, error) {
			// Verify headers are passed correctly
			assert.Equal(t, "Bearer token", req.Headers["Authorization"])
			assert.Equal(t, "application/json", req.Headers["Content-Type"])
			assert.Equal(t, "custom-value", req.Headers["X-Custom-Header"])

			return &localtransport.McpProxyResponse{
				Status: http.StatusOK,
				Body:   []byte(`{"jsonrpc":"2.0","result":{"status":"ok"},"id":1}`),
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
			}, nil
		},
	}

	client := localtransport.NewLocalClientTransport(handler)
	client.WithHeader("Authorization", "Bearer token")
	client.WithHeader("Content-Type", "application/json")
	client.WithHeader("X-Custom-Header", "custom-value")

	message := &transport.BaseJsonRpcMessage{
		Type: transport.BaseMessageTypeJSONRPCRequestType,
		JsonRpcRequest: &transport.BaseJSONRPCRequest{
			Jsonrpc: "2.0",
			Method:  "test_method",
			Id:      transport.RequestId(1),
		},
	}

	err := client.Send(context.Background(), message)
	assert.NoError(t, err)
}

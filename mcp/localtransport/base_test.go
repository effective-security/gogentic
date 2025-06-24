package localtransport_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/effective-security/gogentic/mcp/localtransport"
	"github.com/metoro-io/mcp-golang/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBase(t *testing.T) {
	base := localtransport.NewBase()

	assert.NotNil(t, base)
	// We can't directly access unexported fields, but we can test the behavior
	// by calling methods that use them
}

func TestBase_Send(t *testing.T) {
	t.Run("successful send", func(t *testing.T) {
		base := localtransport.NewBase()

		// Create a response channel by handling a message first
		request := transport.BaseJSONRPCRequest{
			Jsonrpc: "2.0",
			Method:  "test_method",
			Id:      transport.RequestId(123),
		}

		requestBody, err := json.Marshal(request)
		require.NoError(t, err)

		// Start a goroutine to send a response
		go func() {
			time.Sleep(10 * time.Millisecond)
			resultData, _ := json.Marshal(map[string]any{"status": "ok"})
			response := &transport.BaseJsonRpcMessage{
				Type: transport.BaseMessageTypeJSONRPCResponseType,
				JsonRpcResponse: &transport.BaseJSONRPCResponse{
					Jsonrpc: "2.0",
					Id:      transport.RequestId(1), // This will be the atomic counter value
					Result:  resultData,
				},
			}
			_ = base.Send(context.Background(), response)
		}()

		// Handle the message to create the response channel
		_, err = base.HandleMessage(context.Background(), requestBody)
		assert.NoError(t, err)
	})

	t.Run("send with non-existent key", func(t *testing.T) {
		base := localtransport.NewBase()

		resultData, _ := json.Marshal(map[string]any{"status": "ok"})
		message := &transport.BaseJsonRpcMessage{
			Type: transport.BaseMessageTypeJSONRPCResponseType,
			JsonRpcResponse: &transport.BaseJSONRPCResponse{
				Jsonrpc: "2.0",
				Id:      transport.RequestId(999),
				Result:  resultData,
			},
		}

		err := base.Send(context.Background(), message)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no response channel found for key: 999")
	})

	t.Run("send with nil message", func(t *testing.T) {
		base := localtransport.NewBase()

		// This should panic due to nil pointer dereference
		assert.Panics(t, func() {
			_ = base.Send(context.Background(), nil)
		})
	})
}

func TestBase_Close(t *testing.T) {
	t.Run("close with handler", func(t *testing.T) {
		base := localtransport.NewBase()
		closeCalled := false

		base.SetCloseHandler(func() {
			closeCalled = true
		})

		err := base.Close()
		assert.NoError(t, err)
		assert.True(t, closeCalled)
	})

	t.Run("close without handler", func(t *testing.T) {
		base := localtransport.NewBase()

		err := base.Close()
		assert.NoError(t, err)
	})

	t.Run("close multiple times", func(t *testing.T) {
		base := localtransport.NewBase()
		closeCount := 0

		base.SetCloseHandler(func() {
			closeCount++
		})

		err := base.Close()
		assert.NoError(t, err)
		assert.Equal(t, 1, closeCount)

		err = base.Close()
		assert.NoError(t, err)
		assert.Equal(t, 2, closeCount)
	})
}

func TestBase_SetCloseHandler(t *testing.T) {
	base := localtransport.NewBase()

	handlerCalled := false
	handler := func() {
		handlerCalled = true
	}

	base.SetCloseHandler(handler)

	// Test the handler by calling Close
	err := base.Close()
	assert.NoError(t, err)
	assert.True(t, handlerCalled)
}

func TestBase_SetErrorHandler(t *testing.T) {
	base := localtransport.NewBase()

	handler := func(err error) {
		// Error handler implementation
	}

	base.SetErrorHandler(handler)

	// Test the handler by calling it directly (if we had access)
	// For now, we just verify the method doesn't panic
	assert.NotPanics(t, func() {
		base.SetErrorHandler(handler)
	})
}

func TestBase_SetMessageHandler(t *testing.T) {
	base := localtransport.NewBase()

	var receivedMessage *transport.BaseJsonRpcMessage
	var receivedContext context.Context
	handler := func(ctx context.Context, message *transport.BaseJsonRpcMessage) {
		receivedContext = ctx
		receivedMessage = message
	}

	base.SetMessageHandler(handler)

	// Test the handler by processing a message
	testCtx := context.Background()

	// Create a JSON-RPC request
	request := transport.BaseJSONRPCRequest{
		Jsonrpc: "2.0",
		Method:  "test_method",
		Id:      transport.RequestId(123),
	}

	requestBody, err := json.Marshal(request)
	require.NoError(t, err)

	// Start a goroutine to send a response
	go func() {
		time.Sleep(10 * time.Millisecond)
		resultData, _ := json.Marshal(map[string]any{"result": "success"})
		response := &transport.BaseJsonRpcMessage{
			Type: transport.BaseMessageTypeJSONRPCResponseType,
			JsonRpcResponse: &transport.BaseJSONRPCResponse{
				Jsonrpc: "2.0",
				Id:      transport.RequestId(1),
				Result:  resultData,
			},
		}
		_ = base.Send(context.Background(), response)
	}()

	// Handle the message
	_, err = base.HandleMessage(testCtx, requestBody)
	assert.NoError(t, err)

	// Verify the message handler was called
	assert.NotNil(t, receivedMessage)
	assert.Equal(t, testCtx, receivedContext)
	assert.Equal(t, transport.BaseMessageTypeJSONRPCRequestType, receivedMessage.Type)
	assert.Equal(t, "test_method", receivedMessage.JsonRpcRequest.Method)
}

func TestBase_HandleMessage(t *testing.T) {
	t.Run("handle JSON-RPC request", func(t *testing.T) {
		base := localtransport.NewBase()
		var receivedMessage *transport.BaseJsonRpcMessage
		base.SetMessageHandler(func(ctx context.Context, message *transport.BaseJsonRpcMessage) {
			receivedMessage = message
		})

		paramsData, _ := json.Marshal(map[string]any{"param": "value"})
		request := transport.BaseJSONRPCRequest{
			Jsonrpc: "2.0",
			Method:  "test_method",
			Id:      transport.RequestId(123),
			Params:  paramsData,
		}
		requestBody, err := json.Marshal(request)
		require.NoError(t, err)

		// Start the response goroutine first
		done := make(chan bool, 1)
		go func() {
			// Wait a bit to ensure HandleMessage sets up the channel
			time.Sleep(5 * time.Millisecond)
			// The atomic counter will be 1 for the first call
			resultData, _ := json.Marshal(map[string]any{"result": "success"})
			response := &transport.BaseJsonRpcMessage{
				Type: transport.BaseMessageTypeJSONRPCResponseType,
				JsonRpcResponse: &transport.BaseJSONRPCResponse{
					Jsonrpc: "2.0",
					Id:      transport.RequestId(1), // This is the atomic counter key
					Result:  resultData,
				},
			}
			_ = base.Send(context.Background(), response)
			done <- true
		}()

		// Now call HandleMessage
		response, err := base.HandleMessage(context.Background(), requestBody)
		require.NoError(t, err)
		require.NotNil(t, response)
		// The response ID should be restored to the original request ID
		assert.Equal(t, transport.RequestId(123), response.JsonRpcResponse.Id)
		// Verify the message handler was called
		require.NotNil(t, receivedMessage)
		assert.Equal(t, transport.BaseMessageTypeJSONRPCRequestType, receivedMessage.Type)
		assert.Equal(t, "test_method", receivedMessage.JsonRpcRequest.Method)
		// The message handler receives the request with the atomic counter ID, not the original
		assert.Equal(t, transport.RequestId(1), receivedMessage.JsonRpcRequest.Id)

		// Wait for the goroutine to complete
		select {
		case <-done:
			// Success
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for response goroutine")
		}
	})

	t.Run("handle JSON-RPC notification", func(t *testing.T) {
		base := localtransport.NewBase()
		var receivedMessage *transport.BaseJsonRpcMessage
		base.SetMessageHandler(func(ctx context.Context, message *transport.BaseJsonRpcMessage) {
			receivedMessage = message
		})
		paramsData, _ := json.Marshal(map[string]any{"param": "value"})
		notification := transport.BaseJSONRPCNotification{
			Jsonrpc: "2.0",
			Method:  "test_notification",
			Params:  paramsData,
		}
		notificationBody, err := json.Marshal(notification)
		require.NoError(t, err)
		response, err := base.HandleMessage(context.Background(), notificationBody)
		require.NoError(t, err)
		require.NotNil(t, response)
		assert.Equal(t, transport.BaseMessageTypeJSONRPCResponseType, response.Type)
		require.NotNil(t, receivedMessage)
		assert.Equal(t, transport.BaseMessageTypeJSONRPCNotificationType, receivedMessage.Type)
		assert.Equal(t, "test_notification", receivedMessage.JsonRpcNotification.Method)
	})

	t.Run("handle JSON-RPC response", func(t *testing.T) {
		base := localtransport.NewBase()
		var receivedMessage *transport.BaseJsonRpcMessage
		base.SetMessageHandler(func(ctx context.Context, message *transport.BaseJsonRpcMessage) {
			receivedMessage = message
		})
		resultData, _ := json.Marshal(map[string]any{"result": "success"})
		response := transport.BaseJSONRPCResponse{
			Jsonrpc: "2.0",
			Id:      transport.RequestId(456),
			Result:  resultData,
		}
		responseBody, err := json.Marshal(response)
		require.NoError(t, err)

		result, err := base.HandleMessage(context.Background(), responseBody)
		require.NoError(t, err)
		require.NotNil(t, result)
		// The message handler is not called for responses (based on the code logic)
		// So receivedMessage should be nil
		assert.Nil(t, receivedMessage)
	})

	t.Run("handle JSON-RPC error", func(t *testing.T) {
		base := localtransport.NewBase()
		var receivedMessage *transport.BaseJsonRpcMessage
		base.SetMessageHandler(func(ctx context.Context, message *transport.BaseJsonRpcMessage) {
			receivedMessage = message
		})
		errorResponse := transport.BaseJSONRPCError{
			Jsonrpc: "2.0",
			Id:      transport.RequestId(789),
			Error: transport.BaseJSONRPCErrorInner{
				Code:    -32601,
				Message: "Method not found",
			},
		}
		errorBody, err := json.Marshal(errorResponse)
		require.NoError(t, err)

		result, err := base.HandleMessage(context.Background(), errorBody)
		require.NoError(t, err)
		require.NotNil(t, result)
		// The message handler is not called for errors (based on the code logic)
		// So receivedMessage should be nil
		assert.Nil(t, receivedMessage)
	})

	t.Run("handle invalid JSON", func(t *testing.T) {
		base := localtransport.NewBase()

		// Handle invalid JSON - this should not hang because it's not a request
		_, err := base.HandleMessage(context.Background(), []byte("invalid json"))
		// The method doesn't return an error for invalid JSON, it just doesn't deserialize
		assert.NoError(t, err)
	})

	t.Run("handle empty body", func(t *testing.T) {
		base := localtransport.NewBase()

		// Handle empty body - this should not hang because it's not a request
		_, err := base.HandleMessage(context.Background(), []byte{})
		// The method doesn't return an error for empty body, it just doesn't deserialize
		assert.NoError(t, err)
	})

	t.Run("handle without message handler", func(t *testing.T) {
		base := localtransport.NewBase()

		// Create a JSON-RPC request
		request := transport.BaseJSONRPCRequest{
			Jsonrpc: "2.0",
			Method:  "test_method",
			Id:      transport.RequestId(123),
		}

		requestBody, err := json.Marshal(request)
		require.NoError(t, err)

		// Start a goroutine to send a response
		done := make(chan bool, 1)
		go func() {
			time.Sleep(5 * time.Millisecond)
			resultData, _ := json.Marshal(map[string]any{"result": "success"})
			response := &transport.BaseJsonRpcMessage{
				Type: transport.BaseMessageTypeJSONRPCResponseType,
				JsonRpcResponse: &transport.BaseJSONRPCResponse{
					Jsonrpc: "2.0",
					Id:      transport.RequestId(1),
					Result:  resultData,
				},
			}
			_ = base.Send(context.Background(), response)
			done <- true
		}()

		// Handle the message without setting a message handler
		response, err := base.HandleMessage(context.Background(), requestBody)
		assert.NoError(t, err)
		assert.NotNil(t, response)

		select {
		case <-done:
			// Success
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for response goroutine")
		}
	})

	t.Run("handle with context cancellation", func(t *testing.T) {
		base := localtransport.NewBase()

		// Create a JSON-RPC request
		request := transport.BaseJSONRPCRequest{
			Jsonrpc: "2.0",
			Method:  "test_method",
			Id:      transport.RequestId(123),
		}

		requestBody, err := json.Marshal(request)
		require.NoError(t, err)

		// Create a context that will be cancelled
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// Start a goroutine to send a response
		done := make(chan bool, 1)
		go func() {
			time.Sleep(5 * time.Millisecond)
			resultData, _ := json.Marshal(map[string]any{"result": "success"})
			response := &transport.BaseJsonRpcMessage{
				Type: transport.BaseMessageTypeJSONRPCResponseType,
				JsonRpcResponse: &transport.BaseJSONRPCResponse{
					Jsonrpc: "2.0",
					Id:      transport.RequestId(1),
					Result:  resultData,
				},
			}
			_ = base.Send(context.Background(), response)
			done <- true
		}()

		// Handle the message with cancelled context
		_, _ = base.HandleMessage(ctx, requestBody)
		// The error might be due to context cancellation or channel operations
		// We don't assert on the specific error type as it depends on timing

		select {
		case <-done:
			// Success
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for response goroutine")
		}
	})
}

func TestBase_Concurrency(t *testing.T) {
	t.Run("concurrent message handling", func(t *testing.T) {
		base := localtransport.NewBase()

		const numGoroutines = 10
		results := make(chan error, numGoroutines)

		// Start multiple goroutines handling messages concurrently
		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				request := transport.BaseJSONRPCRequest{
					Jsonrpc: "2.0",
					Method:  "test_method",
					Id:      transport.RequestId(id),
				}

				requestBody, err := json.Marshal(request)
				if err != nil {
					results <- err
					return
				}

				// Send a response for this request
				go func() {
					time.Sleep(5 * time.Millisecond)
					resultData, _ := json.Marshal(map[string]any{"id": id})
					response := &transport.BaseJsonRpcMessage{
						Type: transport.BaseMessageTypeJSONRPCResponseType,
						JsonRpcResponse: &transport.BaseJSONRPCResponse{
							Jsonrpc: "2.0",
							Id:      transport.RequestId(int64(id + 1)), // atomic counter starts at 1
							Result:  resultData,
						},
					}
					_ = base.Send(context.Background(), response)
				}()

				_, err = base.HandleMessage(context.Background(), requestBody)
				results <- err
			}(i)
		}

		// Collect results
		for i := 0; i < numGoroutines; i++ {
			err := <-results
			assert.NoError(t, err)
		}
	})

	t.Run("concurrent handler setting", func(t *testing.T) {
		base := localtransport.NewBase()

		const numGoroutines = 10
		done := make(chan bool, numGoroutines)

		// Start multiple goroutines setting handlers concurrently
		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				base.SetMessageHandler(func(ctx context.Context, message *transport.BaseJsonRpcMessage) {
					// Handler implementation
				})
				base.SetErrorHandler(func(err error) {
					// Error handler implementation
				})
				base.SetCloseHandler(func() {
					// Close handler implementation
				})
				done <- true
			}(i)
		}

		// Wait for all goroutines to complete
		for i := 0; i < numGoroutines; i++ {
			<-done
		}

		// Verify handlers are set by testing their functionality
		closeCalled := false
		base.SetCloseHandler(func() {
			closeCalled = true
		})
		base.Close()
		assert.True(t, closeCalled)
	})
}

func TestBase_AtomicCounter(t *testing.T) {
	base := localtransport.NewBase()

	// Handle a message to increment the counter
	request := transport.BaseJSONRPCRequest{
		Jsonrpc: "2.0",
		Method:  "test_method",
		Id:      transport.RequestId(123),
	}

	requestBody, err := json.Marshal(request)
	require.NoError(t, err)

	// Start a goroutine to send a response
	go func() {
		time.Sleep(10 * time.Millisecond)
		resultData, _ := json.Marshal(map[string]any{"result": "success"})
		response := &transport.BaseJsonRpcMessage{
			Type: transport.BaseMessageTypeJSONRPCResponseType,
			JsonRpcResponse: &transport.BaseJSONRPCResponse{
				Jsonrpc: "2.0",
				Id:      transport.RequestId(1),
				Result:  resultData,
			},
		}
		_ = base.Send(context.Background(), response)
	}()

	_, err = base.HandleMessage(context.Background(), requestBody)
	assert.NoError(t, err)

	// We can't directly access the atomic counter, but we can verify
	// that subsequent messages get different IDs by handling another message
	request2 := transport.BaseJSONRPCRequest{
		Jsonrpc: "2.0",
		Method:  "test_method2",
		Id:      transport.RequestId(456),
	}

	requestBody2, err := json.Marshal(request2)
	require.NoError(t, err)

	go func() {
		time.Sleep(10 * time.Millisecond)
		resultData, _ := json.Marshal(map[string]any{"result": "success"})
		response := &transport.BaseJsonRpcMessage{
			Type: transport.BaseMessageTypeJSONRPCResponseType,
			JsonRpcResponse: &transport.BaseJSONRPCResponse{
				Jsonrpc: "2.0",
				Id:      transport.RequestId(2), // Should be the next atomic counter value
				Result:  resultData,
			},
		}
		_ = base.Send(context.Background(), response)
	}()

	_, err = base.HandleMessage(context.Background(), requestBody2)
	assert.NoError(t, err)
}

package localtransport_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/effective-security/gogentic/mcp/transport"
	"github.com/effective-security/gogentic/mcp/transport/localtransport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	transport := localtransport.New()

	assert.NotNil(t, transport)
}

func TestTransport_Start(t *testing.T) {
	transport := localtransport.New()
	ctx := context.Background()

	// Start should always return nil (does nothing in stateless local transport)
	err := transport.Start(ctx)
	assert.NoError(t, err)

	// Test with cancelled context
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err = transport.Start(cancelledCtx)
	assert.NoError(t, err)
}

func TestTransport_Close(t *testing.T) {
	t.Run("close with handler", func(t *testing.T) {
		transport := localtransport.New()
		closeCalled := false

		transport.SetCloseHandler(func() {
			closeCalled = true
		})

		err := transport.Close()
		assert.NoError(t, err)
		assert.True(t, closeCalled)
	})

	t.Run("close without handler", func(t *testing.T) {
		transport := localtransport.New()

		err := transport.Close()
		assert.NoError(t, err)
	})

	t.Run("close multiple times", func(t *testing.T) {
		transport := localtransport.New()
		closeCount := 0

		transport.SetCloseHandler(func() {
			closeCount++
		})

		err := transport.Close()
		assert.NoError(t, err)
		assert.Equal(t, 1, closeCount)

		err = transport.Close()
		assert.NoError(t, err)
		assert.Equal(t, 2, closeCount)
	})

	t.Run("close with nil handler", func(t *testing.T) {
		transport := localtransport.New()
		transport.SetCloseHandler(nil)

		err := transport.Close()
		assert.NoError(t, err)
	})
}

func TestTransport_SetCloseHandler(t *testing.T) {
	t.Run("set close handler", func(t *testing.T) {
		transport := localtransport.New()
		handlerCalled := false
		handler := func() {
			handlerCalled = true
		}

		transport.SetCloseHandler(handler)

		// Test the handler by calling Close
		err := transport.Close()
		assert.NoError(t, err)
		assert.True(t, handlerCalled)
	})

	t.Run("set nil close handler", func(t *testing.T) {
		transport := localtransport.New()
		transport.SetCloseHandler(nil)

		// Should not panic
		assert.NotPanics(t, func() {
			err := transport.Close()
			assert.NoError(t, err)
		})
	})

	t.Run("concurrent close handler setting", func(t *testing.T) {
		transport := localtransport.New()
		var wg sync.WaitGroup
		handlerCount := 0
		var mu sync.Mutex

		// Set handlers concurrently
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				transport.SetCloseHandler(func() {
					mu.Lock()
					handlerCount++
					mu.Unlock()
				})
			}()
		}

		wg.Wait()

		// Call close to verify handler works
		err := transport.Close()
		assert.NoError(t, err)
		assert.Equal(t, 1, handlerCount)
	})
}

func TestTransport_SetErrorHandler(t *testing.T) {
	t.Run("set error handler", func(t *testing.T) {
		transport := localtransport.New()
		handler := func(err error) {
			// Error handler implementation
		}

		transport.SetErrorHandler(handler)

		// Verify the method doesn't panic
		assert.NotPanics(t, func() {
			transport.SetErrorHandler(handler)
		})
	})

	t.Run("set nil error handler", func(t *testing.T) {
		transport := localtransport.New()
		transport.SetErrorHandler(nil)

		// Should not panic
		assert.NotPanics(t, func() {
			transport.SetErrorHandler(nil)
		})
	})

	t.Run("concurrent error handler setting", func(t *testing.T) {
		transport := localtransport.New()
		var wg sync.WaitGroup

		// Set handlers concurrently
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				handler := func(err error) {
					// Error handler implementation
				}
				transport.SetErrorHandler(handler)
			}()
		}

		wg.Wait()

		// Verify no panics occurred
		assert.NotPanics(t, func() {
			transport.SetErrorHandler(func(err error) {})
		})
	})
}

func TestTransport_Concurrency(t *testing.T) {
	t.Run("concurrent handler operations", func(t *testing.T) {
		transport := localtransport.New()
		var wg sync.WaitGroup

		// Test concurrent setting of different handlers
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				transport.SetCloseHandler(func() {})
			}
		}()

		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				transport.SetErrorHandler(func(err error) {})
			}
		}()

		wg.Wait()

		// Verify no panics occurred
		assert.NotPanics(t, func() {
			_ = transport.Close()
		})
	})
}

func TestTransport_Integration(t *testing.T) {
	t.Run("full lifecycle", func(t *testing.T) {
		transport := localtransport.New()
		closeCalled := false

		// Set up handlers
		transport.SetCloseHandler(func() {
			closeCalled = true
		})

		transport.SetErrorHandler(func(err error) {
			// Error handler implementation
		})

		// Start the transport
		err := transport.Start(context.Background())
		assert.NoError(t, err)

		// Close the transport
		err = transport.Close()
		assert.NoError(t, err)
		assert.True(t, closeCalled)
	})
}

func TestTransport_ThreadSafety(t *testing.T) {
	t.Run("thread safety of handler setters", func(t *testing.T) {
		transport := localtransport.New()
		var wg sync.WaitGroup

		// Test that setting handlers from multiple goroutines doesn't cause race conditions
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				transport.SetCloseHandler(func() {})
				transport.SetErrorHandler(func(err error) {})
			}()
		}

		wg.Wait()

		// Verify the transport is still functional
		assert.NotPanics(t, func() {
			_ = transport.Close()
		})
	})
}

func TestTransport_MultipleInstances(t *testing.T) {
	t.Run("multiple transport instances", func(t *testing.T) {
		// Test that multiple transport instances work independently
		transport1 := localtransport.New()
		transport2 := localtransport.New()

		close1Called := false
		close2Called := false

		transport1.SetCloseHandler(func() {
			close1Called = true
		})

		transport2.SetCloseHandler(func() {
			close2Called = true
		})

		// Start both transports
		err1 := transport1.Start(context.Background())
		err2 := transport2.Start(context.Background())
		assert.NoError(t, err1)
		assert.NoError(t, err2)

		// Close both transports
		err1 = transport1.Close()
		err2 = transport2.Close()
		assert.NoError(t, err1)
		assert.NoError(t, err2)

		assert.True(t, close1Called)
		assert.True(t, close2Called)
	})
}

func TestTransport_HandleMessage(t *testing.T) {
	t.Run("handle JSON-RPC request", func(t *testing.T) {
		tr := localtransport.New()
		var receivedMessage *transport.BaseJsonRpcMessage
		tr.SetMessageHandler(func(ctx context.Context, message *transport.BaseJsonRpcMessage) {
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
			_ = tr.Send(context.Background(), response)
			done <- true
		}()

		// Now call HandleMessage
		response, err := tr.HandleMessage(context.Background(), requestBody)
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
		tr := localtransport.New()
		var receivedMessage *transport.BaseJsonRpcMessage
		tr.SetMessageHandler(func(ctx context.Context, message *transport.BaseJsonRpcMessage) {
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
		response, err := tr.HandleMessage(context.Background(), notificationBody)
		require.NoError(t, err)
		require.NotNil(t, response)
		assert.Equal(t, transport.BaseMessageTypeJSONRPCResponseType, response.Type)
		require.NotNil(t, receivedMessage)
		assert.Equal(t, transport.BaseMessageTypeJSONRPCNotificationType, receivedMessage.Type)
		assert.Equal(t, "test_notification", receivedMessage.JsonRpcNotification.Method)
	})

	t.Run("handle JSON-RPC response", func(t *testing.T) {
		tr := localtransport.New()
		var receivedMessage *transport.BaseJsonRpcMessage
		tr.SetMessageHandler(func(ctx context.Context, message *transport.BaseJsonRpcMessage) {
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

		result, err := tr.HandleMessage(context.Background(), responseBody)
		require.NoError(t, err)
		require.NotNil(t, result)
		// The message handler is not called for responses (based on the code logic)
		// So receivedMessage should be nil
		assert.Nil(t, receivedMessage)
	})

	t.Run("handle JSON-RPC error", func(t *testing.T) {
		tr := localtransport.New()
		var receivedMessage *transport.BaseJsonRpcMessage
		tr.SetMessageHandler(func(ctx context.Context, message *transport.BaseJsonRpcMessage) {
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

		result, err := tr.HandleMessage(context.Background(), errorBody)
		require.NoError(t, err)
		require.NotNil(t, result)
		// The message handler is not called for errors (based on the code logic)
		// So receivedMessage should be nil
		assert.Nil(t, receivedMessage)
	})

	t.Run("handle invalid JSON", func(t *testing.T) {
		tr := localtransport.New()

		// Handle invalid JSON - this should not hang because it's not a request
		_, err := tr.HandleMessage(context.Background(), []byte("invalid json"))
		// The method doesn't return an error for invalid JSON, it just doesn't deserialize
		assert.NoError(t, err)
	})

	t.Run("handle empty body", func(t *testing.T) {
		tr := localtransport.New()

		// Handle empty body - this should not hang because it's not a request
		_, err := tr.HandleMessage(context.Background(), []byte{})
		// The method doesn't return an error for empty body, it just doesn't deserialize
		assert.NoError(t, err)
	})

	t.Run("handle without message handler", func(t *testing.T) {
		tr := localtransport.New()

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
			_ = tr.Send(context.Background(), response)
			done <- true
		}()

		// Handle the message without setting a message handler
		response, err := tr.HandleMessage(context.Background(), requestBody)
		assert.NoError(t, err)
		assert.NotNil(t, response)

		select {
		case <-done:
			// Success
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for response goroutine")
		}
	})
}

func TestTransport_Send(t *testing.T) {
	t.Run("successful send", func(t *testing.T) {
		tr := localtransport.New()

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
			_ = tr.Send(context.Background(), response)
		}()

		// Handle the message to create the response channel
		_, err = tr.HandleMessage(context.Background(), requestBody)
		assert.NoError(t, err)
	})

	t.Run("send with non-existent key", func(t *testing.T) {
		tr := localtransport.New()

		resultData, _ := json.Marshal(map[string]any{"status": "ok"})
		message := &transport.BaseJsonRpcMessage{
			Type: transport.BaseMessageTypeJSONRPCResponseType,
			JsonRpcResponse: &transport.BaseJSONRPCResponse{
				Jsonrpc: "2.0",
				Id:      transport.RequestId(999),
				Result:  resultData,
			},
		}

		err := tr.Send(context.Background(), message)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no response channel found for key: 999")
	})
}

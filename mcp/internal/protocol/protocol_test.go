package protocol

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/mcp/internal/testingutils"
	"github.com/effective-security/gogentic/mcp/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProtocol_Connect verifies the basic connection functionality of the Protocol.
// This is a critical test as connection establishment is required for all other operations.
// It ensures that:
// 1. The protocol can successfully connect to a transport
// 2. The message handler is properly registered with the transport
// 3. The protocol is ready to send and receive messages after connection
func TestProtocol_Connect(t *testing.T) {
	p := NewProtocol(nil)
	transport := testingutils.NewMockTransport()

	if err := p.Connect(transport); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if !transport.IsStarted() {
		t.Error("Transport was not started")
	}
}

// TestProtocol_Close tests the proper cleanup of resources when closing the protocol.
// Proper cleanup is essential to prevent resource leaks and ensure graceful shutdown.
// It verifies:
// 1. All handlers are properly deregistered
// 2. The transport is closed
// 3. No messages can be sent after closing
// 4. Multiple closes are handled safely
func TestProtocol_Close(t *testing.T) {
	p := NewProtocol(nil)
	transport := testingutils.NewMockTransport()

	if err := p.Connect(transport); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	closeCalled := false
	p.OnClose = func() {
		closeCalled = true
	}

	if err := p.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if !transport.IsClosed() {
		t.Error("Transport was not closed")
	}

	if !closeCalled {
		t.Error("OnClose callback was not called")
	}
}

// TestProtocol_Request tests the core request-response functionality of the protocol.
// This is the most important test as it covers the primary use case of the protocol.
// It includes subtests for:
// 1. Successful request/response with proper correlation
// 2. Request timeout handling
// 3. Request cancellation via context
// These scenarios ensure the protocol can handle both successful and error cases
// while maintaining proper message correlation and resource cleanup.
func TestProtocol_Request(t *testing.T) {
	p := NewProtocol(nil)
	p.SetRequestHandler("test_method", func(ctx context.Context, req *transport.BaseJSONRPCRequest, extra RequestHandlerExtra) (transport.JsonRpcBody, error) {
		return transport.NewBaseMessageResponse(&transport.BaseJSONRPCResponse{
			Jsonrpc: "2.0",
			Id:      req.Id,
			Result:  json.RawMessage(`{"result": "test result"}`),
		}), nil
	})
	tr := testingutils.NewMockTransport()

	if err := p.Connect(tr); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	// Test request timeout
	t.Run("Request timeout", func(t *testing.T) {
		ctx := context.Background()
		opts := &RequestOptions{
			Timeout: 50 * time.Millisecond,
		}

		_, err := p.Request(ctx, "test_method", nil, opts)
		if err == nil {
			t.Fatal("Expected timeout error, got nil")
		}
	})

	// Test request cancellation
	t.Run("Request cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		_, err := p.Request(ctx, "test_method", nil, nil)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Expected context.Canceled error, got %v", err)
		}
	})
}

// TestProtocol_Notification tests the handling of one-way notifications.
// Notifications are important for events that don't require responses.
// The test verifies:
// 1. Notifications can be sent successfully
// 2. The transport receives the correct notification format
// 3. No response handling is attempted for notifications
func TestProtocol_Notification(t *testing.T) {
	p := NewProtocol(nil)
	tr := testingutils.NewMockTransport()

	if err := p.Connect(tr); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// Send notification
	if err := p.Notification("test_notification", map[string]string{"key": "value"}); err != nil {
		t.Fatalf("Notification failed: %v", err)
	}

	// Check if notification was sent
	msgs := tr.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(msgs))
	}

	notification := msgs[0]
	if notification.Type != transport.BaseMessageTypeJSONRPCNotificationType {
		t.Fatal("Message is not a notification")
	}

	if notification.JsonRpcNotification.Method != "test_notification" {
		t.Errorf("Expected method 'test_notification', got %v", notification.JsonRpcNotification.Method)
	}
}

// TestProtocol_RequestHandler tests the registration and invocation of request handlers.
// Request handlers are crucial for servers implementing RPC methods.
// It ensures:
// 1. Handlers can be registered for specific methods
// 2. Handlers receive the correct request parameters
// 3. Handler responses are properly sent back to clients
// 4. Handler errors are properly propagated
func TestProtocol_RequestHandler(t *testing.T) {
	p := NewProtocol(nil)
	tr := testingutils.NewMockTransport()
	require.NoError(t, p.Connect(tr))

	// Register request handler
	gotParams := make(chan string, 1)
	p.SetRequestHandler("test_method", func(ctx context.Context, req *transport.BaseJSONRPCRequest, extra RequestHandlerExtra) (transport.JsonRpcBody, error) {
		gotParams <- string(req.Params)
		return map[string]any{"result": "handler result"}, nil
	})

	// Simulate incoming request
	tr.SimulateMessage(transport.NewBaseMessageRequest(&transport.BaseJSONRPCRequest{
		Jsonrpc: "2.0",
		Method:  "test_method",
		Params:  json.RawMessage(`{"param": "value"}`),
	}))

	assert.JSONEq(t, `{"param": "value"}`, recv(t, gotParams))

	// Check response
	msgs := waitForMessages(t, tr, 1)
	require.Len(t, msgs, 1)
	response := msgs[0]
	require.Equal(t, transport.BaseMessageTypeJSONRPCResponseType, response.Type)
	assert.Equal(t, `{"result":"handler result"}`, string(response.JsonRpcResponse.Result))
}

func TestProtocol_RequestHandler_ErrorResponse(t *testing.T) {
	p := NewProtocol(nil)
	tr := testingutils.NewMockTransport()
	require.NoError(t, p.Connect(tr))

	// Register request handler
	p.SetRequestHandler("test_method", func(ctx context.Context, req *transport.BaseJSONRPCRequest, extra RequestHandlerExtra) (transport.JsonRpcBody, error) {
		return nil, errors.New("sample error")
	})

	// Simulate incoming request
	tr.SimulateMessage(transport.NewBaseMessageRequest(&transport.BaseJSONRPCRequest{
		Jsonrpc: "2.0",
		Method:  "test_method",
		Params:  json.RawMessage(`{"param": "value"}`),
	}))

	// Check response
	msgs := waitForMessages(t, tr, 1)
	require.Len(t, msgs, 1)
	response := msgs[0]
	require.Equal(t, transport.BaseMessageTypeJSONRPCErrorType, response.Type)
	assert.Equal(t, "sample error", response.JsonRpcError.Error.Message)
}

// TestProtocol_NotificationHandler tests the handling of incoming notifications.
// This is important for asynchronous events and status updates.
// It verifies:
// 1. Notification handlers can be registered
// 2. Handlers are called with correct notification data
// 3. Multiple handlers can be registered for different methods
// 4. Unknown notifications are handled gracefully
func TestProtocol_NotificationHandler(t *testing.T) {
	p := NewProtocol(nil)
	tr := testingutils.NewMockTransport()
	require.NoError(t, p.Connect(tr))

	// Register notification handler
	called := make(chan string, 1)
	p.SetNotificationHandler("test_notification", func(notification *transport.BaseJSONRPCNotification) error {
		called <- notification.Method
		return nil
	})

	// Simulate incoming notification
	tr.SimulateMessage(&transport.BaseJsonRpcMessage{
		Type: transport.BaseMessageTypeJSONRPCNotificationType,
		JsonRpcNotification: &transport.BaseJSONRPCNotification{
			Jsonrpc: "2.0",
			Method:  "test_notification",
		},
	})

	assert.Equal(t, "test_notification", recv(t, called))
}

// TestProtocol_Progress tests the progress tracking functionality.
// Progress tracking is essential for long-running operations.
// The test covers:
// 1. Progress notifications can be sent and received
// 2. Progress tokens are properly correlated with requests
// 3. Progress callbacks are invoked with correct values
// 4. Progress handling works alongside normal request processing
func TestProtocol_Progress(t *testing.T) {
	p := NewProtocol(nil)
	tr := testingutils.NewMockTransport()

	if err := p.Connect(tr); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	progressReceived := make(chan Progress, 1)
	opts := &RequestOptions{
		OnProgress: func(p Progress) {
			progressReceived <- p
		},
	}

	// Start request
	results := startRequest(p, context.Background(), nil, opts)

	// Get the progress token from the sent request
	msgs := waitForMessages(t, tr, 1)

	req := msgs[0]

	if req.Type != transport.BaseMessageTypeJSONRPCRequestType {
		t.Fatal("Message is not a request")
	}

	var params map[string]any
	err := json.Unmarshal(req.JsonRpcRequest.Params, &params)
	if err != nil {
		t.Fatalf("Failed to unmarshal params: %v", err)
	}

	meta, ok := params["_meta"].(map[string]any)
	if !ok {
		t.Fatal("Request has no _meta in params")
	}

	progressToken := meta["progressToken"]

	// Simulate progress notification
	marshal, err := json.Marshal(map[string]any{
		"progress":      50,
		"total":         100,
		"progressToken": progressToken,
	})
	if err != nil {
		t.Fatalf("Failed to marshal progress: %v", err)
	}
	tr.SimulateMessage(transport.NewBaseMessageNotification(&transport.BaseJSONRPCNotification{
		Jsonrpc: "2.0",
		Method:  "$/progress",
		Params:  marshal,
	}))

	// Wait for progress
	assert.Equal(t, Progress{Progress: 50, Total: 100}, recv(t, progressReceived))

	// Complete the request so that its goroutine does not leak.
	tr.SimulateMessage(transport.NewBaseMessageResponse(&transport.BaseJSONRPCResponse{
		Jsonrpc: "2.0",
		Id:      req.JsonRpcRequest.Id,
		Result:  json.RawMessage(`{}`),
	}))
	require.NoError(t, recv(t, results).err)
}

// TestProtocol_ErrorHandling tests various error conditions in the protocol.
// Proper error handling is crucial for reliability and debugging.
// It verifies:
// 1. Transport errors are properly propagated
// 2. Protocol-level errors are handled correctly
// 3. Error responses include appropriate error codes and messages
// 4. Resources are cleaned up after errors
func TestProtocol_ErrorHandling(t *testing.T) {
	p := NewProtocol(nil)
	tr := testingutils.NewMockTransport()

	if err := p.Connect(tr); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	errorReceived := make(chan error, 1)
	p.OnError = func(err error) {
		errorReceived <- err
	}

	// Simulate tr error
	testErr := errors.New("test error")
	tr.SimulateError(testErr)

	// Wait for error
	select {
	case err := <-errorReceived:
		if err != testErr {
			t.Errorf("Expected error %v, got %v", testErr, err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Error not received")
	}
}

const (
	testMethod      = "test_method"
	errSendFailed   = "send failed"
	waitTimeout     = 2 * time.Second
	waitTick        = time.Millisecond
	methodCancelled = "notifications/cancelled"
)

// failingTransport is a MockTransport whose Send fails for selected messages.
type failingTransport struct {
	*testingutils.MockTransport
	fail func(msg *transport.BaseJsonRpcMessage) bool
}

func (f *failingTransport) Send(ctx context.Context, msg *transport.BaseJsonRpcMessage) error {
	if f.fail(msg) {
		return errors.New(errSendFailed)
	}
	return f.MockTransport.Send(ctx, msg)
}

func failType(typ transport.BaseMessageType) func(msg *transport.BaseJsonRpcMessage) bool {
	return func(msg *transport.BaseJsonRpcMessage) bool {
		return msg.Type == typ
	}
}

type requestResult struct {
	resp any
	err  error
}

func connected(t *testing.T) (*Protocol, *testingutils.MockTransport) {
	t.Helper()
	p := NewProtocol(nil)
	tr := testingutils.NewMockTransport()
	require.NoError(t, p.Connect(tr))
	return p, tr
}

func errorSink(p *Protocol) <-chan error {
	ch := make(chan error, 8)
	p.OnError = func(err error) {
		ch <- err
	}
	return ch
}

func recv[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(waitTimeout):
		require.FailNow(t, "timed out waiting for value")
	}
	var zero T
	return zero
}

func waitForMessages(t *testing.T, tr *testingutils.MockTransport, n int) []*transport.BaseJsonRpcMessage {
	t.Helper()
	require.Eventually(t, func() bool {
		return len(tr.GetMessages()) >= n
	}, waitTimeout, waitTick)
	return tr.GetMessages()
}

func startRequest(p *Protocol, ctx context.Context, params any, opts *RequestOptions) <-chan requestResult {
	results := make(chan requestResult, 1)
	go func() {
		resp, err := p.Request(ctx, testMethod, params, opts)
		results <- requestResult{
			resp: resp,
			err:  err,
		}
	}()
	return results
}

func simulateRequest(tr *testingutils.MockTransport, id transport.RequestId, method string) {
	tr.SimulateMessage(transport.NewBaseMessageRequest(&transport.BaseJSONRPCRequest{
		Jsonrpc: "2.0",
		Id:      id,
		Method:  method,
		Params:  json.RawMessage(`{}`),
	}))
}

func simulateNotification(tr *testingutils.MockTransport, method, params string) {
	n := &transport.BaseJSONRPCNotification{
		Jsonrpc: "2.0",
		Method:  method,
	}
	if params != "" {
		n.Params = json.RawMessage(params)
	}
	tr.SimulateMessage(transport.NewBaseMessageNotification(n))
}

func TestProtocol_RequestReceivesResponse(t *testing.T) {
	p, tr := connected(t)
	results := startRequest(p, context.Background(), map[string]any{
		"a": 1,
	}, nil)

	req := waitForMessages(t, tr, 1)[0]
	require.Equal(t, transport.BaseMessageTypeJSONRPCRequestType, req.Type)
	assert.Equal(t, testMethod, req.JsonRpcRequest.Method)
	assert.Equal(t, "2.0", req.JsonRpcRequest.Jsonrpc)
	assert.JSONEq(t, `{"a":1}`, string(req.JsonRpcRequest.Params))

	// A response for an unknown id is ignored.
	tr.SimulateMessage(transport.NewBaseMessageResponse(&transport.BaseJSONRPCResponse{
		Jsonrpc: "2.0",
		Id:      req.JsonRpcRequest.Id + 100,
		Result:  json.RawMessage(`{"other":true}`),
	}))
	tr.SimulateMessage(transport.NewBaseMessageResponse(&transport.BaseJSONRPCResponse{
		Jsonrpc: "2.0",
		Id:      req.JsonRpcRequest.Id,
		Result:  json.RawMessage(`{"ok":true}`),
	}))

	res := recv(t, results)
	require.NoError(t, res.err)
	assert.Equal(t, json.RawMessage(`{"ok":true}`), res.resp)
}

func TestProtocol_RequestReceivesErrorResponse(t *testing.T) {
	p, tr := connected(t)
	results := startRequest(p, context.Background(), nil, nil)

	req := waitForMessages(t, tr, 1)[0]
	assert.Equal(t, "null", string(req.JsonRpcRequest.Params))
	tr.SimulateMessage(transport.NewBaseMessageError(&transport.BaseJSONRPCError{
		Jsonrpc: "2.0",
		Id:      req.JsonRpcRequest.Id,
		Error: transport.BaseJSONRPCErrorInner{
			Code:    -32601,
			Message: "Method not found",
		},
	}))

	res := recv(t, results)
	assert.EqualError(t, res.err, "RPC error -32601: Method not found")
	assert.Nil(t, res.resp)
}

func TestProtocol_RequestIDsIncrement(t *testing.T) {
	p, tr := connected(t)
	first := startRequest(p, context.Background(), nil, nil)
	firstReq := waitForMessages(t, tr, 1)[0].JsonRpcRequest
	second := startRequest(p, context.Background(), nil, nil)
	secondReq := waitForMessages(t, tr, 2)[1].JsonRpcRequest
	assert.Equal(t, firstReq.Id+1, secondReq.Id)

	for _, req := range []*transport.BaseJSONRPCRequest{secondReq, firstReq} {
		tr.SimulateMessage(transport.NewBaseMessageResponse(&transport.BaseJSONRPCResponse{
			Jsonrpc: "2.0",
			Id:      req.Id,
			Result:  json.RawMessage(`{}`),
		}))
	}
	assert.NoError(t, recv(t, first).err)
	assert.NoError(t, recv(t, second).err)
}

func TestProtocol_CloseFailsPendingWork(t *testing.T) {
	p, tr := connected(t)
	closed := make(chan struct{}, 1)
	p.OnClose = func() {
		closed <- struct{}{}
	}

	handlerErr := make(chan error, 1)
	p.SetRequestHandler("block", func(ctx context.Context, _ *transport.BaseJSONRPCRequest, extra RequestHandlerExtra) (transport.JsonRpcBody, error) {
		<-ctx.Done()
		handlerErr <- extra.Context.Err()
		return nil, ctx.Err()
	})
	simulateRequest(tr, 42, "block")

	results := startRequest(p, context.Background(), nil, nil)
	waitForMessages(t, tr, 1)

	require.NoError(t, p.Close())
	recv(t, closed)
	assert.True(t, tr.IsClosed())

	res := recv(t, results)
	assert.EqualError(t, res.err, "connection closed")
	assert.Nil(t, res.resp)

	assert.ErrorIs(t, recv(t, handlerErr), context.Canceled)
	msgs := waitForMessages(t, tr, 2)
	errMsg := msgs[1]
	require.Equal(t, transport.BaseMessageTypeJSONRPCErrorType, errMsg.Type)
	assert.Equal(t, transport.RequestId(42), errMsg.JsonRpcError.Id)
	assert.Equal(t, -32000, errMsg.JsonRpcError.Error.Code)
	assert.Equal(t, "context canceled", errMsg.JsonRpcError.Error.Message)

	// All handlers are dropped on close, so the method is now unknown.
	simulateRequest(tr, 43, "block")
	msgs = waitForMessages(t, tr, 3)
	require.Equal(t, transport.BaseMessageTypeJSONRPCErrorType, msgs[2].Type)
	assert.Equal(t, "method not found: block", msgs[2].JsonRpcError.Error.Message)
}

func TestProtocol_CloseWithoutTransport(t *testing.T) {
	p := NewProtocol(nil)
	assert.NoError(t, p.Close())
}

func TestProtocol_NotConnected(t *testing.T) {
	p := NewProtocol(nil)
	_, err := p.Request(context.Background(), testMethod, nil, nil)
	assert.EqualError(t, err, "not connected")
	assert.EqualError(t, p.Notification(testMethod, nil), "not connected")
}

func TestProtocol_RequestErrors(t *testing.T) {
	t.Run("progress requires map params", func(t *testing.T) {
		p, tr := connected(t)
		_, err := p.Request(context.Background(), testMethod, []int{1}, &RequestOptions{
			OnProgress: func(Progress) {},
		})
		assert.EqualError(t, err, "params must be nil or map[string]interface{} when using progress")
		assert.Empty(t, tr.GetMessages())
	})

	t.Run("params cannot be marshaled", func(t *testing.T) {
		p, tr := connected(t)
		_, err := p.Request(context.Background(), testMethod, make(chan int), nil)
		assert.EqualError(t, err, "failed to marshal params: json: unsupported type: chan int")
		assert.Empty(t, tr.GetMessages())
	})

	t.Run("send fails", func(t *testing.T) {
		p := NewProtocol(nil)
		require.NoError(t, p.Connect(&failingTransport{
			MockTransport: testingutils.NewMockTransport(),
			fail:          failType(transport.BaseMessageTypeJSONRPCRequestType),
		}))
		_, err := p.Request(context.Background(), testMethod, nil, nil)
		assert.EqualError(t, err, "failed to send request: send failed")
	})
}

func TestProtocol_ProgressMergesMeta(t *testing.T) {
	p, tr := connected(t)
	progress := make(chan Progress, 1)
	results := startRequest(p, context.Background(), map[string]any{
		"q": "x",
	}, &RequestOptions{
		OnProgress: func(pr Progress) {
			progress <- pr
		},
	})

	req := waitForMessages(t, tr, 1)[0].JsonRpcRequest
	assert.JSONEq(t, `{"q":"x","_meta":{"progressToken":`+jsonID(t, req.Id)+`}}`, string(req.Params))

	simulateNotification(tr, "$/progress", `{"progress":3,"total":4,"progressToken":`+jsonID(t, req.Id)+`}`)
	assert.Equal(t, Progress{Progress: 3, Total: 4}, recv(t, progress))

	tr.SimulateMessage(transport.NewBaseMessageResponse(&transport.BaseJSONRPCResponse{
		Jsonrpc: "2.0",
		Id:      req.Id,
		Result:  json.RawMessage(`{}`),
	}))
	require.NoError(t, recv(t, results).err)
}

func jsonID(t *testing.T, id transport.RequestId) string {
	t.Helper()
	b, err := json.Marshal(id)
	require.NoError(t, err)
	return string(b)
}

func TestProtocol_CancelledContextSendsCancelNotification(t *testing.T) {
	p, tr := connected(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Request(ctx, testMethod, nil, nil)
	assert.ErrorIs(t, err, context.Canceled)

	msgs := tr.GetMessages()
	require.Len(t, msgs, 2)
	req := msgs[0].JsonRpcRequest
	notif := msgs[1]
	require.Equal(t, transport.BaseMessageTypeJSONRPCNotificationType, notif.Type)
	assert.Equal(t, methodCancelled, notif.JsonRpcNotification.Method)
	assert.JSONEq(t, `{"requestId":`+jsonID(t, req.Id)+`,"reason":"context canceled"}`, string(notif.JsonRpcNotification.Params))
}

func TestProtocol_CancelNotificationSendFailure(t *testing.T) {
	p := NewProtocol(nil)
	require.NoError(t, p.Connect(&failingTransport{
		MockTransport: testingutils.NewMockTransport(),
		fail:          failType(transport.BaseMessageTypeJSONRPCNotificationType),
	}))
	errs := errorSink(p)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Request(ctx, testMethod, nil, nil)
	assert.ErrorIs(t, err, context.Canceled)
	assert.EqualError(t, recv(t, errs), "failed to send cancel notification: send failed")
}

func TestProtocol_NotificationErrors(t *testing.T) {
	p, _ := connected(t)
	assert.EqualError(t, p.Notification(testMethod, make(chan int)), "failed to marshal notification params: json: unsupported type: chan int")
}

func TestProtocol_IncomingNotifications(t *testing.T) {
	t.Run("handler error is reported", func(t *testing.T) {
		p, tr := connected(t)
		errs := errorSink(p)
		p.SetNotificationHandler("custom", func(*transport.BaseJSONRPCNotification) error {
			return errors.New("bad")
		})
		simulateNotification(tr, "custom", "")
		assert.EqualError(t, recv(t, errs), "notification handler error: bad")
	})

	t.Run("invalid progress params", func(t *testing.T) {
		p, tr := connected(t)
		errs := errorSink(p)
		simulateNotification(tr, "$/progress", `"oops"`)
		assert.EqualError(t, recv(t, errs),
			"notification handler error: failed to unmarshal progress params: json: cannot unmarshal string into Go value of type struct { Progress int64 \"json:\\\"progress\\\"\"; Total int64 \"json:\\\"total\\\"\"; ProgressToken transport.RequestId \"json:\\\"progressToken\\\"\" }")
	})

	t.Run("invalid cancelled params", func(t *testing.T) {
		p, tr := connected(t)
		errs := errorSink(p)
		simulateNotification(tr, methodCancelled, `"oops"`)
		assert.EqualError(t, recv(t, errs),
			"notification handler error: failed to unmarshal cancelled params: json: cannot unmarshal string into Go value of type struct { RequestId transport.RequestId \"json:\\\"requestId\\\"\"; Reason string \"json:\\\"reason\\\"\" }")
	})

	t.Run("fallback handler", func(t *testing.T) {
		p, tr := connected(t)
		got := make(chan string, 1)
		p.FallbackNotificationHandler = func(n *transport.BaseJSONRPCNotification) error {
			got <- n.Method
			return nil
		}
		simulateNotification(tr, "unknown/thing", "")
		assert.Equal(t, "unknown/thing", recv(t, got))
	})

	t.Run("removed handler falls back", func(t *testing.T) {
		p, tr := connected(t)
		got := make(chan string, 2)
		p.SetNotificationHandler("custom", func(*transport.BaseJSONRPCNotification) error {
			got <- "custom"
			return nil
		})
		p.FallbackNotificationHandler = func(*transport.BaseJSONRPCNotification) error {
			got <- "fallback"
			return nil
		}
		p.RemoveNotificationHandler("custom")
		simulateNotification(tr, "custom", "")
		assert.Equal(t, "fallback", recv(t, got))
	})

	t.Run("unhandled notification is dropped", func(t *testing.T) {
		p, tr := connected(t)
		errs := errorSink(p)
		simulateNotification(tr, "unknown/thing", "")
		assert.Empty(t, tr.GetMessages())
		assert.Empty(t, errs)
	})

	t.Run("initialized notification is accepted", func(t *testing.T) {
		p := NewProtocol(nil)
		assert.NoError(t, p.handleInitializedNotification(&transport.BaseJSONRPCNotification{
			Method: "notifications/initialized",
		}))
	})
}

func TestProtocol_CancelledNotificationCancelsHandler(t *testing.T) {
	p, tr := connected(t)
	handlerErr := make(chan error, 1)
	p.SetRequestHandler("slow", func(ctx context.Context, _ *transport.BaseJSONRPCRequest, _ RequestHandlerExtra) (transport.JsonRpcBody, error) {
		<-ctx.Done()
		handlerErr <- ctx.Err()
		return nil, ctx.Err()
	})
	simulateRequest(tr, 7, "slow")

	// Cancelling an unknown request is a no-op.
	simulateNotification(tr, methodCancelled, `{"requestId":99,"reason":"x"}`)
	simulateNotification(tr, methodCancelled, `{"requestId":7,"reason":"user abort"}`)

	assert.ErrorIs(t, recv(t, handlerErr), context.Canceled)
	msg := waitForMessages(t, tr, 1)[0]
	require.Equal(t, transport.BaseMessageTypeJSONRPCErrorType, msg.Type)
	assert.Equal(t, transport.RequestId(7), msg.JsonRpcError.Id)
	assert.Equal(t, "context canceled", msg.JsonRpcError.Error.Message)
}

func TestProtocol_IncomingRequests(t *testing.T) {
	t.Run("unknown method", func(t *testing.T) {
		_, tr := connected(t)
		simulateRequest(tr, 1, "nope")
		msg := waitForMessages(t, tr, 1)[0]
		require.Equal(t, transport.BaseMessageTypeJSONRPCErrorType, msg.Type)
		assert.Equal(t, transport.RequestId(1), msg.JsonRpcError.Id)
		assert.Equal(t, "2.0", msg.JsonRpcError.Jsonrpc)
		assert.Equal(t, "method not found: nope", msg.JsonRpcError.Error.Message)
	})

	t.Run("fallback handler", func(t *testing.T) {
		p, tr := connected(t)
		p.FallbackRequestHandler = func(_ context.Context, req *transport.BaseJSONRPCRequest) (transport.JsonRpcBody, error) {
			return map[string]string{
				"handled": req.Method,
			}, nil
		}
		simulateRequest(tr, 2, "anything")
		msg := waitForMessages(t, tr, 1)[0]
		require.Equal(t, transport.BaseMessageTypeJSONRPCResponseType, msg.Type)
		assert.Equal(t, transport.RequestId(2), msg.JsonRpcResponse.Id)
		assert.JSONEq(t, `{"handled":"anything"}`, string(msg.JsonRpcResponse.Result))
	})

	t.Run("removed handler", func(t *testing.T) {
		p, tr := connected(t)
		p.SetRequestHandler(testMethod, func(context.Context, *transport.BaseJSONRPCRequest, RequestHandlerExtra) (transport.JsonRpcBody, error) {
			return map[string]any{}, nil
		})
		p.RemoveRequestHandler(testMethod)
		simulateRequest(tr, 3, testMethod)
		msg := waitForMessages(t, tr, 1)[0]
		require.Equal(t, transport.BaseMessageTypeJSONRPCErrorType, msg.Type)
		assert.Equal(t, "method not found: test_method", msg.JsonRpcError.Error.Message)
	})

	t.Run("result cannot be marshaled", func(t *testing.T) {
		p, tr := connected(t)
		p.SetRequestHandler(testMethod, func(context.Context, *transport.BaseJSONRPCRequest, RequestHandlerExtra) (transport.JsonRpcBody, error) {
			return make(chan int), nil
		})
		simulateRequest(tr, 4, testMethod)
		msg := waitForMessages(t, tr, 1)[0]
		require.Equal(t, transport.BaseMessageTypeJSONRPCErrorType, msg.Type)
		assert.Equal(t, transport.RequestId(4), msg.JsonRpcError.Id)
		assert.Equal(t, "failed to marshal result: json: unsupported type: chan int", msg.JsonRpcError.Error.Message)
	})

	t.Run("response send failure is reported", func(t *testing.T) {
		p := NewProtocol(nil)
		tr := &failingTransport{
			MockTransport: testingutils.NewMockTransport(),
			fail:          failType(transport.BaseMessageTypeJSONRPCResponseType),
		}
		require.NoError(t, p.Connect(tr))
		errs := errorSink(p)
		p.SetRequestHandler(testMethod, func(context.Context, *transport.BaseJSONRPCRequest, RequestHandlerExtra) (transport.JsonRpcBody, error) {
			return map[string]any{}, nil
		})
		simulateRequest(tr.MockTransport, 5, testMethod)
		assert.EqualError(t, recv(t, errs), "failed to send response: send failed")
	})

	t.Run("error response send failure is reported", func(t *testing.T) {
		p := NewProtocol(nil)
		tr := &failingTransport{
			MockTransport: testingutils.NewMockTransport(),
			fail:          failType(transport.BaseMessageTypeJSONRPCErrorType),
		}
		require.NoError(t, p.Connect(tr))
		errs := errorSink(p)
		simulateRequest(tr.MockTransport, 6, "nope")
		assert.EqualError(t, recv(t, errs), "failed to send error response: send failed")
	})
}

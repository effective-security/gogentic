package localtransport

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/metoro-io/mcp-golang/transport"
)

// Base implements the common functionality for the MCP transports
type Base struct {
	messageHandler func(ctx context.Context, message *transport.BaseJsonRpcMessage)
	errorHandler   func(error)
	closeHandler   func()
	mu             sync.RWMutex
	responseMap    map[int64]chan *transport.BaseJsonRpcMessage
	atomicCounter  int64
}

func NewBase() *Base {
	return &Base{
		responseMap: make(map[int64]chan *transport.BaseJsonRpcMessage),
	}
}

// Send implements Transport.Send
func (t *Base) Send(ctx context.Context, message *transport.BaseJsonRpcMessage) error {
	key := message.JsonRpcResponse.Id
	t.mu.Lock()
	defer t.mu.Unlock()

	responseChannel := t.responseMap[int64(key)]
	if responseChannel == nil {
		return fmt.Errorf("no response channel found for key: %d", key)
	}
	responseChannel <- message
	return nil
}

// Close implements Transport.Close
func (t *Base) Close() error {
	if t.closeHandler != nil {
		t.closeHandler()
	}
	return nil
}

// SetCloseHandler implements Transport.SetCloseHandler
func (t *Base) SetCloseHandler(handler func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closeHandler = handler
}

// SetErrorHandler implements Transport.SetErrorHandler
func (t *Base) SetErrorHandler(handler func(error)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.errorHandler = handler
}

// SetMessageHandler implements Transport.SetMessageHandler
func (t *Base) SetMessageHandler(handler func(ctx context.Context, message *transport.BaseJsonRpcMessage)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.messageHandler = handler
}

// HandleMessage processes an incoming message and returns a response
func (t *Base) HandleMessage(ctx context.Context, body []byte) (*transport.BaseJsonRpcMessage, error) {
	// Store the response writer for later use
	t.mu.Lock()

	key := atomic.AddInt64(&t.atomicCounter, 1)
	t.responseMap[key] = make(chan *transport.BaseJsonRpcMessage)
	t.mu.Unlock()

	var prevId *transport.RequestId = nil
	deserialized := false
	// Try to unmarshal as a request first
	var request transport.BaseJSONRPCRequest
	if err := json.Unmarshal(body, &request); err == nil {
		deserialized = true
		id := request.Id
		prevId = &id
		request.Id = transport.RequestId(key)
		t.mu.RLock()
		handler := t.messageHandler
		t.mu.RUnlock()

		if handler != nil {
			handler(ctx, transport.NewBaseMessageRequest(&request))
		}
	}

	// Try as a notification
	var notification transport.BaseJSONRPCNotification
	if !deserialized {
		if err := json.Unmarshal(body, &notification); err == nil {
			//deserialized = true
			t.mu.RLock()
			handler := t.messageHandler
			t.mu.RUnlock()

			if handler != nil {
				handler(ctx, transport.NewBaseMessageNotification(&notification))
			}
		}
		return &transport.BaseJsonRpcMessage{
			Type: transport.BaseMessageTypeJSONRPCResponseType,
		}, nil
	}

	// Try as a response
	var response transport.BaseJSONRPCResponse
	if !deserialized {
		if err := json.Unmarshal(body, &response); err == nil {
			deserialized = true
			t.mu.RLock()
			handler := t.messageHandler
			t.mu.RUnlock()

			if handler != nil {
				handler(ctx, transport.NewBaseMessageResponse(&response))
			}
		}
	}

	// Try as an error
	var errorResponse transport.BaseJSONRPCError
	if !deserialized {
		if err := json.Unmarshal(body, &errorResponse); err == nil {
			//deserialized = true
			t.mu.RLock()
			handler := t.messageHandler
			t.mu.RUnlock()

			if handler != nil {
				handler(ctx, transport.NewBaseMessageError(&errorResponse))
			}
		}
	}

	// Block until the response is received
	t.mu.Lock()
	ch := t.responseMap[key]
	t.mu.Unlock()

	responseToUse := <-ch

	t.mu.Lock()
	delete(t.responseMap, key)
	t.mu.Unlock()

	if prevId != nil {
		responseToUse.JsonRpcResponse.Id = *prevId
	}

	return responseToUse, nil
}

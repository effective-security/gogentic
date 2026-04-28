package localtransport

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/mcp/transport"
	"github.com/effective-security/xlog"
)

var logger = xlog.NewPackageLogger("github.com/effective-security/gogentic/mcp/transport", "localtransport")

type Transport struct {
	messageHandler func(ctx context.Context, message *transport.BaseJsonRpcMessage)
	errorHandler   func(error)
	closeHandler   func()
	mu             sync.RWMutex
	responseMap    map[int64]chan *transport.BaseJsonRpcMessage
	atomicCounter  int64
}

func New() *Transport {
	return &Transport{
		responseMap: make(map[int64]chan *transport.BaseJsonRpcMessage),
	}
}

func (s *Transport) Start(ctx context.Context) error {
	// Does nothing in the stateless local transport
	return nil
}

// Close closes the connection.
func (s *Transport) Close() error {
	if s.closeHandler != nil {
		s.closeHandler()
	}
	return nil
}

// SetErrorHandler sets the callback for when an error occurs.
// Note that errors are not necessarily fatal; they are used for reporting any kind of exceptional condition out of band.
func (s *Transport) SetErrorHandler(handler func(error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errorHandler = handler
}

// SetCloseHandler sets the callback for when the connection is closed for any reason.
// This should be invoked when Close() is called as well.
func (s *Transport) SetCloseHandler(handler func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeHandler = handler
}

// SetMessageHandler sets the callback for when a message (request, notification or response) is received over the connection.
// Partially deserializes the messages to pass a BaseJsonRpcMessage
func (s *Transport) SetMessageHandler(handler func(ctx context.Context, message *transport.BaseJsonRpcMessage)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messageHandler = handler
}

// Send sends a JSON-RPC message (request, notification or response).
func (s *Transport) Send(ctx context.Context, message *transport.BaseJsonRpcMessage) error {
	if message.Type == transport.BaseMessageTypeJSONRPCNotificationType {
		// Should not happen, but just in case
		return nil
	}
	key := message.MessageID()
	logger.ContextKV(ctx, xlog.DEBUG,
		"type", message.Type,
		"key", key,
	)
	s.mu.RLock()
	responseChannel := s.responseMap[int64(key)]
	s.mu.RUnlock()
	if responseChannel == nil {
		logger.ContextKV(ctx, xlog.ERROR,
			"type", message.Type,
			"key", key,
			"err", "no response channel found",
		)
		return errors.Errorf("no response channel found for key: %d", key)
	}
	select {
	case responseChannel <- message:
		return nil
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "send cancelled")
	}
}

// HandleMessage processes an incoming message and returns a response
func (s *Transport) HandleMessage(ctx context.Context, body []byte) (*transport.BaseJsonRpcMessage, error) {
	// Try to unmarshal as a request first (requests have an id field)
	var request transport.BaseJSONRPCRequest
	if err := json.Unmarshal(body, &request); err == nil {
		key := atomic.AddInt64(&s.atomicCounter, 1)
		s.mu.Lock()
		s.responseMap[key] = make(chan *transport.BaseJsonRpcMessage)
		s.mu.Unlock()

		prevId := request.Id
		request.Id = transport.RequestId(key)

		s.mu.RLock()
		handler := s.messageHandler
		s.mu.RUnlock()
		if handler != nil {
			handler(ctx, transport.NewBaseMessageRequest(&request))
		}

		// Block until the protocol layer sends the response via Send()
		s.mu.RLock()
		ch := s.responseMap[key]
		s.mu.RUnlock()

		responseToUse := <-ch

		s.mu.Lock()
		delete(s.responseMap, key)
		s.mu.Unlock()

		if responseToUse.JsonRpcResponse != nil {
			responseToUse.JsonRpcResponse.Id = prevId
		}
		return responseToUse, nil
	}

	// Try as a notification (has method, no id)
	var notification transport.BaseJSONRPCNotification
	if err := json.Unmarshal(body, &notification); err == nil {
		s.mu.RLock()
		handler := s.messageHandler
		s.mu.RUnlock()
		if handler != nil {
			handler(ctx, transport.NewBaseMessageNotification(&notification))
		}
		return nil, nil
	}

	// Try as a response
	var response transport.BaseJSONRPCResponse
	if err := json.Unmarshal(body, &response); err == nil {
		s.mu.RLock()
		handler := s.messageHandler
		s.mu.RUnlock()
		if handler != nil {
			handler(ctx, transport.NewBaseMessageResponse(&response))
		}
		return nil, nil
	}

	// Try as an error response
	var errorResponse transport.BaseJSONRPCError
	if err := json.Unmarshal(body, &errorResponse); err == nil {
		s.mu.RLock()
		handler := s.messageHandler
		s.mu.RUnlock()
		if handler != nil {
			handler(ctx, transport.NewBaseMessageError(&errorResponse))
		}
		return nil, nil
	}

	return nil, nil
}

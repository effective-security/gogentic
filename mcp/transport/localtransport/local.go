package localtransport

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/mcp/transport"
)

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
	key := message.JsonRpcResponse.Id

	s.mu.Lock()
	defer s.mu.Unlock()

	responseChannel := s.responseMap[int64(key)]
	if responseChannel == nil {
		return errors.Errorf("no response channel found for key: %d", key)
	}
	responseChannel <- message
	return nil
}

// HandleMessage processes an incoming message and returns a response
func (s *Transport) HandleMessage(ctx context.Context, body []byte) (*transport.BaseJsonRpcMessage, error) {
	// Store the response writer for later use
	s.mu.Lock()

	key := atomic.AddInt64(&s.atomicCounter, 1)
	s.responseMap[key] = make(chan *transport.BaseJsonRpcMessage)
	s.mu.Unlock()

	var prevId *transport.RequestId = nil
	deserialized := false
	// Try to unmarshal as a request first
	var request transport.BaseJSONRPCRequest
	if err := json.Unmarshal(body, &request); err == nil {
		deserialized = true
		id := request.Id
		prevId = &id
		request.Id = transport.RequestId(key)
		s.mu.RLock()
		handler := s.messageHandler
		s.mu.RUnlock()

		if handler != nil {
			handler(ctx, transport.NewBaseMessageRequest(&request))
		}
	}

	// Try as a notification
	var notification transport.BaseJSONRPCNotification
	if !deserialized {
		if err := json.Unmarshal(body, &notification); err == nil {
			//deserialized = true
			s.mu.RLock()
			handler := s.messageHandler
			s.mu.RUnlock()

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
			s.mu.RLock()
			handler := s.messageHandler
			s.mu.RUnlock()

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
			s.mu.RLock()
			handler := s.messageHandler
			s.mu.RUnlock()

			if handler != nil {
				handler(ctx, transport.NewBaseMessageError(&errorResponse))
			}
		}
	}

	// Block until the response is received
	s.mu.Lock()
	ch := s.responseMap[key]
	s.mu.Unlock()

	responseToUse := <-ch

	s.mu.Lock()
	delete(s.responseMap, key)
	s.mu.Unlock()

	if prevId != nil {
		responseToUse.JsonRpcResponse.Id = *prevId
	}

	return responseToUse, nil
}

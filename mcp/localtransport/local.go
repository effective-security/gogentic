package localtransport

import (
	"context"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/metoro-io/mcp-golang/transport"
)

type Transport struct {
	*Base
	lock sync.RWMutex
}

func New() *Transport {
	return &Transport{
		Base: NewBase(),
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
	s.lock.Lock()
	defer s.lock.Unlock()
	s.errorHandler = handler
}

// SetCloseHandler sets the callback for when the connection is closed for any reason.
// This should be invoked when Close() is called as well.
func (s *Transport) SetCloseHandler(handler func()) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.closeHandler = handler
}

// SetMessageHandler sets the callback for when a message (request, notification or response) is received over the connection.
// Partially deserializes the messages to pass a BaseJsonRpcMessage
func (s *Transport) SetMessageHandler(handler func(ctx context.Context, message *transport.BaseJsonRpcMessage)) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.messageHandler = handler
}

// Send sends a JSON-RPC message (request, notification or response).
func (s *Transport) Send(ctx context.Context, message *transport.BaseJsonRpcMessage) error {
	key := message.JsonRpcResponse.Id

	responseChannel := s.responseMap[int64(key)]
	if responseChannel == nil {
		return errors.Errorf("no response channel found for key: %d", key)
	}
	responseChannel <- message
	return nil
}

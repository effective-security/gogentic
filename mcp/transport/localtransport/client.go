package localtransport

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/mcp/transport"
)

type McpProxyRequest struct {
	Body    []byte            `json:"body"`
	Headers map[string]string `json:"headers"`
}

type McpProxyResponse struct {
	Type    transport.BaseMessageType `json:"type"`
	Status  int                       `json:"status"`
	Body    []byte                    `json:"body"`
	Headers map[string]string         `json:"headers"`
}

// Handler is an interface for handling MCP requests using local transport or proxy
type Handler interface {
	HandleMCP(ctx context.Context, req *McpProxyRequest) (*McpProxyResponse, error)
}

// LocalMcpClientTransport implements a client-side HTTP transport for MCP
type LocalMcpClientTransport struct {
	messageHandler func(ctx context.Context, message *transport.BaseJsonRpcMessage)
	errorHandler   func(error)
	closeHandler   func()
	mu             sync.RWMutex
	handler        Handler
	headers        map[string]string
}

// NewLocalClientTransport creates a new client transport that connects to the workflow Provider
func NewLocalClientTransport(workflow Handler) *LocalMcpClientTransport {
	return &LocalMcpClientTransport{
		handler: workflow,
		headers: make(map[string]string),
	}
}

// WithHeader adds a header to the request
func (t *LocalMcpClientTransport) WithHeader(key, value string) *LocalMcpClientTransport {
	t.headers[key] = value
	return t
}

// Start implements Transport.Start
func (t *LocalMcpClientTransport) Start(ctx context.Context) error {
	// Does nothing in the stateless http client transport
	return nil
}

// Send implements Transport.Send
func (t *LocalMcpClientTransport) Send(ctx context.Context, message *transport.BaseJsonRpcMessage) error {
	jsonData, err := json.Marshal(message)
	if err != nil {
		return errors.Wrap(err, "failed to marshal message")
	}

	req := &McpProxyRequest{
		Body:    jsonData,
		Headers: t.headers,
	}

	resp, err := t.handler.HandleMCP(ctx, req)
	if err != nil {
		return err
	}

	if resp.Status != http.StatusOK {
		return errors.Errorf("server returned error: %d", resp.Status)
	}

	if len(resp.Body) > 0 {
		// Try to unmarshal as a response first
		var response transport.BaseJSONRPCResponse
		if err := json.Unmarshal(resp.Body, &response); err == nil {
			t.mu.RLock()
			handler := t.messageHandler
			t.mu.RUnlock()

			if handler != nil {
				handler(ctx, transport.NewBaseMessageResponse(&response))
			}
			return nil
		}

		// Try as an error
		var errorResponse transport.BaseJSONRPCError
		if err := json.Unmarshal(resp.Body, &errorResponse); err == nil {
			t.mu.RLock()
			handler := t.messageHandler
			t.mu.RUnlock()

			if handler != nil {
				handler(ctx, transport.NewBaseMessageError(&errorResponse))
			}
			return nil
		}

		// Try as a notification
		var notification transport.BaseJSONRPCNotification
		if err := json.Unmarshal(resp.Body, &notification); err == nil {
			t.mu.RLock()
			handler := t.messageHandler
			t.mu.RUnlock()

			if handler != nil {
				handler(ctx, transport.NewBaseMessageNotification(&notification))
			}
			return nil
		}

		// Try as a request
		var request transport.BaseJSONRPCRequest
		if err := json.Unmarshal(resp.Body, &request); err == nil {
			t.mu.RLock()
			handler := t.messageHandler
			t.mu.RUnlock()

			if handler != nil {
				handler(ctx, transport.NewBaseMessageRequest(&request))
			}
			return nil
		}

		return errors.Errorf("received invalid response")
	}

	return nil
}

// Close implements Transport.Close
func (t *LocalMcpClientTransport) Close() error {
	if t.closeHandler != nil {
		t.closeHandler()
	}
	return nil
}

// SetCloseHandler implements Transport.SetCloseHandler
func (t *LocalMcpClientTransport) SetCloseHandler(handler func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closeHandler = handler
}

// SetErrorHandler implements Transport.SetErrorHandler
func (t *LocalMcpClientTransport) SetErrorHandler(handler func(error)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.errorHandler = handler
}

// SetMessageHandler implements Transport.SetMessageHandler
func (t *LocalMcpClientTransport) SetMessageHandler(handler func(ctx context.Context, message *transport.BaseJsonRpcMessage)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.messageHandler = handler
}

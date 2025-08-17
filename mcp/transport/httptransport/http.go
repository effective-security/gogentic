package httptransport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/effective-security/gogentic/mcp/transport"
	"github.com/effective-security/xlog"
	"github.com/pkg/errors"
)

var logger = xlog.NewPackageLogger("github.com/effective-security/gogentic/mcp/transport", "httptransport")

// HTTPTransport implements a stateless HTTP transport for MCP
type HTTPTransport struct {
	server         *http.Server
	endpoint       string
	messageHandler func(ctx context.Context, message *transport.BaseJsonRpcMessage)
	errorHandler   func(error)
	closeHandler   func()
	mu             sync.RWMutex
	responseMap    map[int64]chan *transport.BaseJsonRpcMessage
	atomicCounter  int64
	addr           string
}

// NewHTTPTransport creates a new HTTP transport that listens on the specified endpoint
func NewHTTPTransport(endpoint string) *HTTPTransport {
	return &HTTPTransport{
		endpoint:    endpoint,
		responseMap: make(map[int64]chan *transport.BaseJsonRpcMessage),
		addr:        ":8080", // Default port
	}
}

// WithAddr sets the address to listen on
func (t *HTTPTransport) WithAddr(addr string) *HTTPTransport {
	t.addr = addr
	return t
}

// Start implements Transport.Start
func (t *HTTPTransport) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc(t.endpoint, t.handleRequest)

	t.server = &http.Server{
		Addr:    t.addr,
		Handler: mux,
	}

	return t.server.ListenAndServe()
}

// Send implements Transport.Send
func (t *HTTPTransport) Send(ctx context.Context, message *transport.BaseJsonRpcMessage) error {
	if message.Type == transport.BaseMessageTypeJSONRPCNotificationType {
		// Should not happen, but just in case
		return nil
	}
	key := message.MessageID()
	logger.ContextKV(ctx, xlog.DEBUG,
		"type", message.Type,
		"key", key,
	)

	responseChannel := t.responseMap[int64(key)]
	if responseChannel == nil {
		logger.ContextKV(ctx, xlog.ERROR,
			"type", message.Type,
			"key", key,
			"err", "no response channel found",
		)
		return errors.Errorf("no response channel found for key: %d", key)
	}
	responseChannel <- message
	return nil
}

// Close implements Transport.Close
func (t *HTTPTransport) Close() error {
	if t.server != nil {
		if err := t.server.Close(); err != nil {
			return err
		}
	}
	if t.closeHandler != nil {
		t.closeHandler()
	}
	return nil
}

// SetCloseHandler implements Transport.SetCloseHandler
func (t *HTTPTransport) SetCloseHandler(handler func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closeHandler = handler
}

// SetErrorHandler implements Transport.SetErrorHandler
func (t *HTTPTransport) SetErrorHandler(handler func(error)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.errorHandler = handler
}

// SetMessageHandler implements Transport.SetMessageHandler
func (t *HTTPTransport) SetMessageHandler(handler func(ctx context.Context, message *transport.BaseJsonRpcMessage)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.messageHandler = handler
}

func (t *HTTPTransport) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is supported", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	body, err := t.readBody(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response, err := t.handleMessage(ctx, body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonData, err := json.Marshal(response)
	if err != nil {
		if t.errorHandler != nil {
			t.errorHandler(errors.Wrap(err, "failed to marshal response"))
		}
		http.Error(w, "Failed to marshal response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(jsonData)
}

// handleMessage processes an incoming message and returns a response
func (t *HTTPTransport) handleMessage(ctx context.Context, body []byte) (*transport.BaseJsonRpcMessage, error) {
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
	responseToUse := <-t.responseMap[key]
	delete(t.responseMap, key)
	if prevId != nil && responseToUse.JsonRpcResponse != nil {
		responseToUse.JsonRpcResponse.Id = *prevId
	}

	return responseToUse, nil
}

// readBody reads and returns the body from an io.Reader
func (t *HTTPTransport) readBody(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		if t.errorHandler != nil {
			t.errorHandler(errors.Wrap(err, "failed to read request body"))
		}
		return nil, errors.Wrap(err, "failed to read request body")
	}
	return body, nil
}

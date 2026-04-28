package httptransport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/mcp/transport"
	"github.com/effective-security/xlog"
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

// ServeHTTP implements http.Handler, allowing HTTPTransport to be used directly
func (t *HTTPTransport) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t.handleRequest(w, r)
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

	if response == nil {
		// Notifications produce no response body; return 202 Accepted
		w.WriteHeader(http.StatusAccepted)
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
	// Try to unmarshal as a request first (requests have an id field)
	var request transport.BaseJSONRPCRequest
	if err := json.Unmarshal(body, &request); err == nil {
		key := atomic.AddInt64(&t.atomicCounter, 1)
		t.mu.Lock()
		t.responseMap[key] = make(chan *transport.BaseJsonRpcMessage)
		t.mu.Unlock()

		prevId := request.Id
		request.Id = transport.RequestId(key)

		t.mu.RLock()
		handler := t.messageHandler
		t.mu.RUnlock()
		if handler != nil {
			handler(ctx, transport.NewBaseMessageRequest(&request))
		}

		// Block until the protocol layer sends the response via Send()
		responseToUse := <-t.responseMap[key]
		t.mu.Lock()
		delete(t.responseMap, key)
		t.mu.Unlock()

		if responseToUse.JsonRpcResponse != nil {
			responseToUse.JsonRpcResponse.Id = prevId
		}
		return responseToUse, nil
	}

	// Try as a notification (has method, no id)
	var notification transport.BaseJSONRPCNotification
	if err := json.Unmarshal(body, &notification); err == nil {
		t.mu.RLock()
		handler := t.messageHandler
		t.mu.RUnlock()
		if handler != nil {
			handler(ctx, transport.NewBaseMessageNotification(&notification))
		}
		// Notifications require no response body
		return nil, nil
	}

	// Try as a response
	var response transport.BaseJSONRPCResponse
	if err := json.Unmarshal(body, &response); err == nil {
		t.mu.RLock()
		handler := t.messageHandler
		t.mu.RUnlock()
		if handler != nil {
			handler(ctx, transport.NewBaseMessageResponse(&response))
		}
		return nil, nil
	}

	// Try as an error response
	var errorResponse transport.BaseJSONRPCError
	if err := json.Unmarshal(body, &errorResponse); err == nil {
		t.mu.RLock()
		handler := t.messageHandler
		t.mu.RUnlock()
		if handler != nil {
			handler(ctx, transport.NewBaseMessageError(&errorResponse))
		}
		return nil, nil
	}

	return nil, nil
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

package sse_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/effective-security/gogentic/mcp/sse/internal/sse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockResponseWriter implements http.ResponseWriter and http.Flusher for testing
type mockResponseWriter struct {
	*httptest.ResponseRecorder
	flushed bool
}

func newMockResponseWriter() *mockResponseWriter {
	return &mockResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
	}
}

func (m *mockResponseWriter) Flush() {
	m.flushed = true
}

// // mockResponseWriterWithoutFlusher implements http.ResponseWriter but not http.Flusher
// type mockResponseWriterWithoutFlusher struct {
// 	*httptest.ResponseRecorder
// }

// func newMockResponseWriterWithoutFlusher() *mockResponseWriterWithoutFlusher {
// 	return &mockResponseWriterWithoutFlusher{
// 		ResponseRecorder: httptest.NewRecorder(),
// 	}
// }

func TestNewSSETransport(t *testing.T) {
	t.Run("successful creation", func(t *testing.T) {
		w := newMockResponseWriter()
		transport, err := sse.NewSSETransport("/messages", w)
		require.NoError(t, err)
		assert.NotNil(t, transport)
		// Only public API: SessionID should be non-empty and look like a UUID
		assert.NotEmpty(t, transport.SessionID())
		assert.Len(t, transport.SessionID(), 36)
	})

	// t.Run("streaming not supported", func(t *testing.T) {
	// 	w := newMockResponseWriterWithoutFlusher()
	// 	transport, err := sse.NewSSETransport("/messages", w)
	// 	assert.EqualError(t, err, "streaming not supported")
	// 	assert.Nil(t, transport)
	// })
}

func TestSSETransport_Start(t *testing.T) {
	t.Run("successful start", func(t *testing.T) {
		w := newMockResponseWriter()
		transport, err := sse.NewSSETransport("/messages", w)
		require.NoError(t, err)
		ctx := context.Background()
		err = transport.Start(ctx)
		assert.NoError(t, err)
		// Check headers
		headers := w.Header()
		assert.Equal(t, "text/event-stream", headers.Get("Content-Type"))
		assert.Equal(t, "no-cache", headers.Get("Cache-Control"))
		assert.Equal(t, "keep-alive", headers.Get("Connection"))
		assert.Equal(t, "*", headers.Get("Access-Control-Allow-Origin"))
		// Check endpoint event
		body := w.Body.String()
		assert.Contains(t, body, "event: endpoint")
		assert.Contains(t, body, "/messages?session=")
		assert.Contains(t, body, transport.SessionID())
	})

	t.Run("already started", func(t *testing.T) {
		w := newMockResponseWriter()
		transport, err := sse.NewSSETransport("/messages", w)
		require.NoError(t, err)
		ctx := context.Background()
		err = transport.Start(ctx)
		require.NoError(t, err)
		err = transport.Start(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "SSE transport already started")
	})

	t.Run("context cancellation closes connection", func(t *testing.T) {
		w := newMockResponseWriter()
		transport, err := sse.NewSSETransport("/messages", w)
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		err = transport.Start(ctx)
		require.NoError(t, err)
		cancel()
		time.Sleep(10 * time.Millisecond)
		// No direct way to check connection, but Close should be idempotent
		err = transport.Close()
		assert.NoError(t, err)
	})
}

func TestSSETransport_HandleMessage(t *testing.T) {
	t.Run("invalid JSON", func(t *testing.T) {
		w := newMockResponseWriter()
		transport, err := sse.NewSSETransport("/messages", w)
		require.NoError(t, err)
		var receivedError error
		transport.SetErrorHandler(func(err error) { receivedError = err })
		err = transport.HandleMessage([]byte("invalid json"))
		assert.Error(t, err)
		assert.NotNil(t, receivedError)
		assert.Contains(t, receivedError.Error(), "invalid")
	})

	t.Run("empty message", func(t *testing.T) {
		w := newMockResponseWriter()
		transport, err := sse.NewSSETransport("/messages", w)
		require.NoError(t, err)
		err = transport.HandleMessage([]byte{})
		assert.Error(t, err)
	})

	t.Run("nil message", func(t *testing.T) {
		w := newMockResponseWriter()
		transport, err := sse.NewSSETransport("/messages", w)
		require.NoError(t, err)
		err = transport.HandleMessage(nil)
		assert.Error(t, err)
	})
}

func TestSSETransport_Close(t *testing.T) {
	t.Run("successful close", func(t *testing.T) {
		w := newMockResponseWriter()
		transport, err := sse.NewSSETransport("/messages", w)
		require.NoError(t, err)
		ctx := context.Background()
		err = transport.Start(ctx)
		require.NoError(t, err)
		var closeCalled bool
		transport.SetCloseHandler(func() { closeCalled = true })
		err = transport.Close()
		assert.NoError(t, err)
		assert.True(t, closeCalled)
	})

	t.Run("close when not connected", func(t *testing.T) {
		w := newMockResponseWriter()
		transport, err := sse.NewSSETransport("/messages", w)
		require.NoError(t, err)
		err = transport.Close()
		assert.NoError(t, err)
	})

	t.Run("close multiple times", func(t *testing.T) {
		w := newMockResponseWriter()
		transport, err := sse.NewSSETransport("/messages", w)
		require.NoError(t, err)
		ctx := context.Background()
		err = transport.Start(ctx)
		require.NoError(t, err)
		err = transport.Close()
		assert.NoError(t, err)
		err = transport.Close()
		assert.NoError(t, err)
	})
}

func TestSSETransport_Handlers(t *testing.T) {
	t.Run("set close handler", func(t *testing.T) {
		w := newMockResponseWriter()
		transport, err := sse.NewSSETransport("/messages", w)
		require.NoError(t, err)
		var handlerCalled bool
		transport.SetCloseHandler(func() { handlerCalled = true })
		ctx := context.Background()
		err = transport.Start(ctx)
		require.NoError(t, err)
		err = transport.Close()
		assert.NoError(t, err)
		assert.True(t, handlerCalled)
	})

	t.Run("set error handler", func(t *testing.T) {
		w := newMockResponseWriter()
		transport, err := sse.NewSSETransport("/messages", w)
		require.NoError(t, err)
		var receivedError error
		transport.SetErrorHandler(func(err error) { receivedError = err })
		err = transport.HandleMessage([]byte("invalid json"))
		assert.Error(t, err)
		assert.NotNil(t, receivedError)
	})
}

func TestSSETransport_SessionID(t *testing.T) {
	w := newMockResponseWriter()
	transport, err := sse.NewSSETransport("/messages", w)
	require.NoError(t, err)
	sessionID := transport.SessionID()
	assert.NotEmpty(t, sessionID)
	assert.Len(t, sessionID, 36) // UUID length
}

func TestSSETransport_WriteEvent(t *testing.T) {
	t.Run("write event through start", func(t *testing.T) {
		w := newMockResponseWriter()
		transport, err := sse.NewSSETransport("/messages", w)
		require.NoError(t, err)
		ctx := context.Background()
		err = transport.Start(ctx)
		require.NoError(t, err)
		body := w.Body.String()
		assert.Contains(t, body, "event: endpoint")
		assert.Contains(t, body, "/messages?session=")
		assert.Contains(t, body, transport.SessionID())
		assert.True(t, w.flushed)
	})
}

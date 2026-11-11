package stdio

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/mcp/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const waitTimeout = 2 * time.Second

// syncBuffer is a goroutine-safe bytes.Buffer for capturing transport output.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newPipeTransport returns a transport reading from an in-memory pipe. The
// pipe is closed on cleanup so that the read loop always terminates.
func newPipeTransport(t *testing.T) (*StdioServerTransport, *io.PipeWriter, *syncBuffer) {
	t.Helper()
	pr, pw := io.Pipe()
	out := &syncBuffer{}
	t.Cleanup(func() {
		_ = pw.Close()
	})
	return NewStdioServerTransportWithIO(pr, out), pw, out
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

func TestStdioServerTransport(t *testing.T) {
	t.Run("default constructor", func(t *testing.T) {
		tr := NewStdioServerTransport()
		require.NotNil(t, tr)
		assert.False(t, tr.started)
		assert.NotNil(t, tr.reader)
		assert.NotNil(t, tr.writer)
		assert.NotNil(t, tr.readBuf)
	})

	t.Run("basic message handling", func(t *testing.T) {
		tr, in, _ := newPipeTransport(t)

		received := make(chan *transport.BaseJsonRpcMessage, 1)
		tr.SetMessageHandler(func(ctx context.Context, msg *transport.BaseJsonRpcMessage) {
			received <- msg
		})
		require.NoError(t, tr.Start(context.Background()))

		_, err := in.Write([]byte(`{"jsonrpc": "2.0", "method": "test", "params": {}, "id": 1}` + "\n"))
		require.NoError(t, err)

		req := recv(t, received)
		require.Equal(t, transport.BaseMessageTypeJSONRPCRequestType, req.Type)
		assert.Equal(t, "test", req.JsonRpcRequest.Method)
		assert.Equal(t, transport.RequestId(1), req.JsonRpcRequest.Id)

		assert.NoError(t, tr.Close())
	})

	t.Run("double start error", func(t *testing.T) {
		tr, _, _ := newPipeTransport(t)
		ctx := context.Background()
		require.NoError(t, tr.Start(ctx))
		assert.EqualError(t, tr.Start(ctx), "StdioServerTransport already started")
		assert.NoError(t, tr.Close())
	})

	t.Run("send message", func(t *testing.T) {
		out := &bytes.Buffer{}
		tr := NewStdioServerTransportWithIO(&bytes.Buffer{}, out)

		msg := &transport.BaseJSONRPCResponse{
			Jsonrpc: "2.0",
			Result:  []byte(`{"status": "ok"}`),
			Id:      1,
		}
		require.NoError(t, tr.Send(context.Background(), transport.NewBaseMessageResponse(msg)))
		assert.Equal(t, `{"id":1,"jsonrpc":"2.0","result":{"status":"ok"}}`+"\n", out.String())
	})

	t.Run("send unmarshalable message", func(t *testing.T) {
		out := &bytes.Buffer{}
		tr := NewStdioServerTransportWithIO(&bytes.Buffer{}, out)
		err := tr.Send(context.Background(), &transport.BaseJsonRpcMessage{
			Type: "bogus",
		})
		assert.EqualError(t, err, "failed to marshal message: json: error calling MarshalJSON for type *transport.BaseJsonRpcMessage: failed to marshal: unknown message type: bogus")
		assert.Empty(t, out.String())
	})

	t.Run("invalid message reports error", func(t *testing.T) {
		tr, in, _ := newPipeTransport(t)

		errs := make(chan error, 1)
		tr.SetErrorHandler(func(err error) {
			errs <- err
		})
		require.NoError(t, tr.Start(context.Background()))

		_, err := in.Write([]byte(`{"invalid json` + "\n"))
		require.NoError(t, err)

		err = recv(t, errs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal JSON-RPC message, unrecognized type")

		assert.NoError(t, tr.Close())
	})

	t.Run("read error reports error", func(t *testing.T) {
		tr, in, _ := newPipeTransport(t)

		errs := make(chan error, 1)
		tr.SetErrorHandler(func(err error) {
			errs <- err
		})
		require.NoError(t, tr.Start(context.Background()))
		require.NoError(t, in.CloseWithError(errors.New("disk on fire")))

		assert.EqualError(t, recv(t, errs), "read error: disk on fire")
	})

	t.Run("context cancellation", func(t *testing.T) {
		tr, in, _ := newPipeTransport(t)

		closed := make(chan struct{}, 1)
		tr.SetCloseHandler(func() {
			closed <- struct{}{}
		})

		ctx, cancel := context.WithCancel(context.Background())
		require.NoError(t, tr.Start(ctx))
		cancel()

		// Unblock a pending read so the loop observes the cancellation. The
		// write is async: if the loop saw the cancellation first, nobody reads
		// and the write is released when the pipe is closed on cleanup.
		go func() {
			_, _ = in.Write([]byte("\n"))
		}()

		recv(t, closed)
		tr.mu.Lock()
		started := tr.started
		tr.mu.Unlock()
		assert.False(t, started, "transport should be stopped after context cancellation")
	})
}

// closeAsync closes tr on another goroutine, so that a Close that deadlocks
// fails the test on a timeout instead of hanging it.
func closeAsync(tr *StdioServerTransport) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- tr.Close()
	}()
	return done
}

// F-035: the close handler runs after the transport lock is released, and only
// once however often Close is called.
func TestStdioServerTransport_CloseHandler(t *testing.T) {
	t.Run("handler may call back into the transport", func(t *testing.T) {
		tr, _, out := newPipeTransport(t)
		sendErr := make(chan error, 1)
		tr.SetCloseHandler(func() {
			tr.SetErrorHandler(nil)
			sendErr <- tr.Send(context.Background(), transport.NewBaseMessageNotification(&transport.BaseJSONRPCNotification{
				Jsonrpc: "2.0",
				Method:  "notifications/closing",
			}))
		})
		require.NoError(t, tr.Start(context.Background()))

		require.NoError(t, recv(t, closeAsync(tr)))
		require.NoError(t, recv(t, sendErr))
		assert.Equal(t, `{"jsonrpc":"2.0","method":"notifications/closing"}`+"\n", out.String())
	})

	t.Run("concurrent and repeated close run the handler once", func(t *testing.T) {
		const closers = 8
		tr, _, _ := newPipeTransport(t)
		var mu sync.Mutex
		calls := 0
		tr.SetCloseHandler(func() {
			mu.Lock()
			defer mu.Unlock()
			calls++
		})
		require.NoError(t, tr.Start(context.Background()))

		results := make([]<-chan error, 0, closers)
		for range closers {
			results = append(results, closeAsync(tr))
		}
		for _, res := range results {
			require.NoError(t, recv(t, res))
		}
		require.NoError(t, tr.Close())

		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, 1, calls)
	})

	t.Run("close before start does not run the handler", func(t *testing.T) {
		tr, _, _ := newPipeTransport(t)
		called := false
		tr.SetCloseHandler(func() {
			called = true
		})
		require.NoError(t, tr.Close())
		assert.False(t, called)
	})
}

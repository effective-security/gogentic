package openaiclient

import (
	"context"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/require"
)

const (
	// streamReaderFunc names the goroutine that reads a streamed chat
	// response; it must exit once the call returns.
	streamReaderFunc = "openaiclient.readStreamingChat"
	// leakWait bounds how long the reader may take to notice the call ended.
	leakWait = 5 * time.Second
	// leakPoll is how often the goroutine dump is taken.
	leakPoll = 10 * time.Millisecond
)

// goroutinesRunning reports whether any goroutine is executing fn.
func goroutinesRunning(fn string) bool {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return strings.Contains(string(buf[:n]), fn)
}

// F-008: when a streaming callback fails while chunks are still coming, the
// goroutine reading the stream must not stay blocked on its channel. Not
// parallel, so the goroutine dump sees only this test's reader.
func TestCreateChat_StreamingCallbackErrorStopsReader(t *testing.T) {
	errStop := errors.New("stop now")
	stream := sseChat(
		`{"choices":[{"index":0,"delta":{"content":"a"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"b"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"c"}}]}`,
	)
	_, srv := newStubServer(t, http.StatusOK, stream)
	c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)

	_, err := c.CreateChat(context.Background(), &ChatRequest{
		StreamingFunc: func(context.Context, []byte) error { return errStop },
	})
	require.ErrorIs(t, err, errStop)

	require.Eventually(t, func() bool {
		return !goroutinesRunning(streamReaderFunc)
	}, leakWait, leakPoll, "the stream reader goroutine leaked")
}

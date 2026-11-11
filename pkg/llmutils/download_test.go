package llmutils_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	headerContentType = "Content-Type"
	testImageBytes    = "\x89PNG-data"
)

func newImageServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func Test_DownloadImageData(t *testing.T) {
	server := newImageServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(headerContentType, "image/png")
		_, _ = w.Write([]byte(testImageBytes))
	})

	typ, data, err := llmutils.DownloadImageData(server.URL)
	require.NoError(t, err)
	assert.Equal(t, "image/png", typ)
	assert.Equal(t, []byte(testImageBytes), data)
}

func Test_DownloadImageData_StripsMediaTypeParameters(t *testing.T) {
	server := newImageServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(headerContentType, "Image/JPEG; charset=binary")
		_, _ = w.Write([]byte(testImageBytes))
	})

	typ, data, err := llmutils.DownloadImageData(server.URL)
	require.NoError(t, err)
	assert.Equal(t, "image/jpeg", typ)
	assert.Equal(t, []byte(testImageBytes), data)
}

func Test_DownloadImageData_InvalidMimeType(t *testing.T) {
	for _, contentType := range []string{"binary", "image/png; =bad"} {
		t.Run(contentType, func(t *testing.T) {
			server := newImageServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set(headerContentType, contentType)
				_, _ = w.Write([]byte(testImageBytes))
			})

			typ, data, err := llmutils.DownloadImageData(server.URL)
			assert.EqualError(t, err, "invalid mime type "+contentType)
			assert.Empty(t, typ)
			assert.Nil(t, data)
		})
	}
}

func Test_DownloadImageData_FetchError(t *testing.T) {
	typ, data, err := llmutils.DownloadImageData("://missing-scheme")
	assert.EqualError(t, err, `failed to fetch image from url: parse "://missing-scheme": missing protocol scheme`)
	assert.Empty(t, typ)
	assert.Nil(t, data)
}

func Test_DownloadImageData_ReadError(t *testing.T) {
	server := newImageServer(t, func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !assert.True(t, ok) {
			return
		}
		conn, buf, err := hj.Hijack()
		if !assert.NoError(t, err) {
			return
		}
		// Promise more bytes than are sent, then drop the connection.
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nContent-Length: 100\r\n\r\nshort")
		_ = buf.Flush()
		_ = conn.Close()
	})

	typ, data, err := llmutils.DownloadImageData(server.URL)
	assert.EqualError(t, err, "failed to read image bytes: unexpected EOF")
	assert.Empty(t, typ)
	assert.Nil(t, data)
}

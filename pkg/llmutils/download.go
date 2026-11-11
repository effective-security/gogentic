package llmutils

import (
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/cockroachdb/errors"
)

const headerContentType = "Content-Type"

// DownloadImageData downloads the content from the given URL and returns its
// MIME type and data, for providers that need an image inlined in the request.
// The MIME type is the response's Content-Type media type without parameters
// (e.g. "image/png" from "image/png; charset=binary").
func DownloadImageData(url string) (string, []byte, error) {
	resp, err := http.Get(url) //nolint
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to fetch image from url")
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	urlData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to read image bytes")
	}

	contentType := resp.Header.Get(headerContentType)

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.Contains(mediaType, "/") {
		return "", nil, errors.Newf("invalid mime type %v", contentType)
	}

	return mediaType, urlData, nil
}

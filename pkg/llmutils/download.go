package llmutils

import (
	"io"
	"net/http"
	"strings"

	"github.com/cockroachdb/errors"
)

// downloadImageData downloads the content from the given URL and returns the
// image type and data. The image type is the second part of the response's
// MIME (e.g. "png" from "image/png").
func DownloadImageData(url string) (string, []byte, error) {
	resp, err := http.Get(url) //nolint
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to fetch image from url")
	}
	defer resp.Body.Close()

	urlData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to read image bytes")
	}

	mimeType := resp.Header.Get("Content-Type")

	parts := strings.Split(mimeType, "/")
	if len(parts) != 2 {
		return "", nil, errors.Newf("invalid mime type %v", mimeType)
	}

	return parts[1], urlData, nil
}

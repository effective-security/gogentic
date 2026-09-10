package anthropic_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/router"
	"github.com/effective-security/gogentic/router/dialect"
	"github.com/effective-security/gogentic/router/dialect/anthropic"
)

type exampleModel struct{}

func (exampleModel) ValidateInference(llms.InferenceRequest) error { return nil }
func (exampleModel) Infer(context.Context, llms.InferenceRequest) (*llms.InferenceResponse, error) {
	return &llms.InferenceResponse{
		Content: []llms.InferenceBlock{{
			Type: llms.BlockText,
			Text: "Hello from the router",
		}},
		FinishReason: llms.FinishStop,
	}, nil
}

const exampleMaxBody = 1 << 20

// ExampleDecodeMessages shows the host handler pattern: authenticate, read a
// bounded body, decode, route, encode. The library itself serves no HTTP.
func ExampleDecodeMessages() {
	r, err := router.New(router.Config{
		Targets: []router.Target{{
			ID:           "claude",
			BackendModel: "configured-model",
			Connector:    exampleModel{},
		}},
	})
	if err != nil {
		panic(err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("x-api-key") == "" { // host-owned authentication
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, req.Body, exampleMaxBody))
		if err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		var body []byte
		status := http.StatusOK
		in, err := anthropic.DecodeMessages(raw, dialect.DecodeOptions{})
		if err == nil {
			var res *router.Result
			if res, err = r.Generate(req.Context(), in); err == nil {
				body, err = anthropic.EncodeMessages(res, dialect.EncodeOptions{})
			}
		}
		if err != nil {
			status, body = anthropic.EncodeError(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(
		`{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"Hi"}]}`))
	req.Header.Set("x-api-key", "host-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	fmt.Println(rec.Code, strings.Contains(rec.Body.String(), `"text":"Hello from the router"`))

	req = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(
		`{"model":"claude","max_tokens":64,"top_k":3,"messages":[{"role":"user","content":"Hi"}]}`))
	req.Header.Set("x-api-key", "host-key")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	fmt.Println(rec.Code, rec.Body.String())
	// Output:
	// 200 true
	// 400 {"error":{"message":"unsupported value for this field","type":"invalid_request_error"},"request_id":null,"type":"error"}
}

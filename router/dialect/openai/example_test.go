package openai_test

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
	openaidialect "github.com/effective-security/gogentic/router/dialect/openai"
)

type exampleModel struct{}

func (exampleModel) ValidateInference(llms.InferenceRequest) error { return nil }
func (exampleModel) Infer(context.Context, llms.InferenceRequest) (*llms.InferenceResponse, error) {
	return &llms.InferenceResponse{
		Content: []llms.InferenceBlock{{
			Type: llms.BlockText,
			Text: "Hello",
		}},
		FinishReason: llms.FinishStop,
	}, nil
}

const maxBodyBytes = 4 << 20

// ExampleDecodeChat shows the host handler pattern: authenticate, read a bounded
// body, decode, route, encode. The router library ships no HTTP handler.
func ExampleDecodeChat() {
	r, err := router.New(router.Config{
		Targets: []router.Target{{
			ID:           "chat",
			BackendModel: "configured-model",
			Connector:    exampleModel{},
		}},
	})
	if err != nil {
		panic(err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer demo-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, req.Body, maxBodyBytes))
		if err != nil {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		var body []byte
		decoded, err := openaidialect.DecodeChat(raw, dialect.DecodeOptions{})
		if err == nil {
			var result *router.Result
			if result, err = r.Generate(req.Context(), decoded); err == nil {
				body, err = openaidialect.EncodeChat(result, dialect.EncodeOptions{})
			}
		}
		if err != nil {
			status, envelope := openaidialect.EncodeError(err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write(envelope)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"chat","messages":[{"role":"user","content":"Hi"}]}`))
	req.Header.Set("Authorization", "Bearer demo-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	fmt.Println(rec.Code, strings.Contains(rec.Body.String(), `"content":"Hello"`))

	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"chat","messages":[{"role":"user","content":"Hi"}],"stream":true}`))
	req.Header.Set("Authorization", "Bearer demo-key")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	fmt.Println(rec.Code, strings.Contains(rec.Body.String(), `"param":"stream"`))
	// Output:
	// 200 true
	// 400 true
}

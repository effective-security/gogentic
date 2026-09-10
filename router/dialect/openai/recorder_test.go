package openai

import (
	"net/http"
	"net/http/httptest"
)

// recorder adapts httptest.ResponseRecorder so an SDK client can be driven
// through an in-process handler.
type recorder struct {
	*httptest.ResponseRecorder
}

func newRecorder() *recorder {
	return &recorder{ResponseRecorder: httptest.NewRecorder()}
}

func (r *recorder) result(req *http.Request) *http.Response {
	res := r.Result()
	res.Request = req
	return res
}

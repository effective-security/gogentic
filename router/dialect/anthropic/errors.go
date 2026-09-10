package anthropic

import (
	"encoding/json"

	"github.com/effective-security/gogentic/router"
	"github.com/effective-security/gogentic/router/dialect"
)

// Anthropic error envelope types.
const (
	errorTypeInvalidRequest = "invalid_request_error"
	errorTypeNotFound       = "not_found_error"
	errorTypeRateLimit      = "rate_limit_error"
	errorTypeAPI            = "api_error"
	typeError               = "error"
)

// EncodeError renders any error as an Anthropic error envelope and returns the
// HTTP status the host should write. Only the sanitized router message is used;
// diagnostic causes never reach the client.
func EncodeError(err error) (int, []byte) {
	re := router.AsError(err)
	if re == nil {
		re = &router.Error{
			Kind:    router.KindInternal,
			Message: "internal router error",
		}
	}
	body, marshalErr := json.Marshal(map[string]any{
		"type": typeError,
		"error": map[string]any{
			"type":    errorType(re.Kind),
			"message": re.Message,
		},
		"request_id": nil,
	})
	if marshalErr != nil {
		body = []byte(`{"type":"error","error":{"type":"api_error","message":"internal router error"}}`)
	}
	return dialect.HTTPStatus(re.Kind), body
}

func errorType(kind router.Kind) string {
	switch kind {
	case router.KindInvalidRequest, router.KindUnsupported:
		return errorTypeInvalidRequest
	case router.KindModelNotFound:
		return errorTypeNotFound
	case router.KindRateLimited:
		return errorTypeRateLimit
	}
	return errorTypeAPI
}

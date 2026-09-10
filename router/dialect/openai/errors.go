package openai

import (
	"net/http"

	"github.com/effective-security/gogentic/router"
	"github.com/effective-security/gogentic/router/dialect"
)

// EncodeError renders any error as the OpenAI error envelope and returns the
// HTTP status a host should write. Only the sanitized router message is
// exposed; diagnostic causes never reach the body.
func EncodeError(err error) (int, []byte) {
	re := router.AsError(err)
	status := dialect.HTTPStatus(re.Kind)
	typ := errorTypeInvalid
	if status >= http.StatusInternalServerError {
		typ = errorTypeServer
	}
	var param any
	if re.Param != "" {
		param = re.Param
	}
	body, marshalErr := marshal(map[string]any{
		keyError: map[string]any{
			keyMessage: re.Message,
			keyType:    typ,
			keyParam:   param,
			keyCode:    string(re.Kind),
		},
	})
	if marshalErr != nil {
		return http.StatusInternalServerError, []byte(`{"error":{"message":"internal router error","type":"server_error","param":null,"code":"internal_error"}}`)
	}
	return status, body
}

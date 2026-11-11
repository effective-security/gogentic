package transport

import (
	"bytes"
	"encoding/json"

	"github.com/cockroachdb/errors"
)

// JSON-RPC members that tell the message types apart.
const (
	memberID     = "id"
	memberMethod = "method"
	memberResult = "result"
	memberError  = "error"

	// jsonObjectStart opens a JSON object.
	jsonObjectStart = '{'
)

// errNotErrorObject reports an "error" member that is not an error object,
// such as null, which would otherwise decode as a zero error.
var errNotErrorObject = errors.New("invalid JSON-RPC error: error member is not an object")

// ParseMessage decodes a single JSON-RPC message and classifies it by the
// members it carries: "method" with "id" is a request, "method" without "id"
// is a notification, "error" is an error response and "result" is a
// successful response. Transports use it to dispatch a received payload as
// its real type; a payload that is none of these, or whose "error" member is
// not an object, is an error, never a zero-valued error response.
func ParseMessage(data []byte) (*BaseJsonRpcMessage, error) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal JSON-RPC message")
	}
	_, hasID := members[memberID]
	_, hasMethod := members[memberMethod]
	_, hasResult := members[memberResult]
	_, hasError := members[memberError]

	switch {
	case hasMethod && hasID:
		var request BaseJSONRPCRequest
		if err := json.Unmarshal(data, &request); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal JSON-RPC request")
		}
		return NewBaseMessageRequest(&request), nil
	case hasMethod:
		var notification BaseJSONRPCNotification
		if err := json.Unmarshal(data, &notification); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal JSON-RPC notification")
		}
		return NewBaseMessageNotification(&notification), nil
	case hasError:
		if !isJSONObject(members[memberError]) {
			return nil, errors.WithStack(errNotErrorObject)
		}
		var errorResponse BaseJSONRPCError
		if err := json.Unmarshal(data, &errorResponse); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal JSON-RPC error")
		}
		return NewBaseMessageError(&errorResponse), nil
	case hasResult:
		var response BaseJSONRPCResponse
		if err := json.Unmarshal(data, &response); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal JSON-RPC response")
		}
		return NewBaseMessageResponse(&response), nil
	default:
		return nil, errors.New("invalid JSON-RPC message: no method, result or error member")
	}
}

// isJSONObject reports whether raw holds a JSON object.
func isJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == jsonObjectStart
}

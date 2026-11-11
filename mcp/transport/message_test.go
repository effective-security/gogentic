package transport_test

import (
	"encoding/json"
	"testing"

	"github.com/effective-security/gogentic/mcp/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMessage(t *testing.T) {
	tcs := []struct {
		name string
		data string
		want *transport.BaseJsonRpcMessage
	}{
		{
			name: "request",
			data: `{"jsonrpc":"2.0","id":3,"method":"sampling/createMessage","params":{"a":1}}`,
			want: transport.NewBaseMessageRequest(&transport.BaseJSONRPCRequest{
				Jsonrpc: "2.0",
				Id:      3,
				Method:  "sampling/createMessage",
				Params:  json.RawMessage(`{"a":1}`),
			}),
		},
		{
			name: "notification",
			data: `{"jsonrpc":"2.0","method":"notifications/progress","params":{"progress":1}}`,
			want: transport.NewBaseMessageNotification(&transport.BaseJSONRPCNotification{
				Jsonrpc: "2.0",
				Method:  "notifications/progress",
				Params:  json.RawMessage(`{"progress":1}`),
			}),
		},
		{
			name: "response",
			data: `{"jsonrpc":"2.0","id":4,"result":{"ok":true}}`,
			want: transport.NewBaseMessageResponse(&transport.BaseJSONRPCResponse{
				Jsonrpc: "2.0",
				Id:      4,
				Result:  json.RawMessage(`{"ok":true}`),
			}),
		},
		{
			name: "error response",
			data: `{"jsonrpc":"2.0","id":5,"error":{"code":-32601,"message":"Method not found","data":"x"}}`,
			want: transport.NewBaseMessageError(&transport.BaseJSONRPCError{
				Jsonrpc: "2.0",
				Id:      5,
				Error: transport.BaseJSONRPCErrorInner{
					Code:    -32601,
					Message: "Method not found",
					Data:    "x",
				},
			}),
		},
		{
			// A server that cannot read the request id answers with a null id.
			name: "error response with null id",
			data: `{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"Parse error"}}`,
			want: transport.NewBaseMessageError(&transport.BaseJSONRPCError{
				Jsonrpc: "2.0",
				Error: transport.BaseJSONRPCErrorInner{
					Code:    -32700,
					Message: "Parse error",
				},
			}),
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got, err := transport.ParseMessage([]byte(tc.data))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	// The decoder's wording for a non-object depends on the Go version.
	const notAnObject = `"just a string"`
	var members map[string]json.RawMessage
	notAnObjectErr := json.Unmarshal([]byte(notAnObject), &members)
	require.Error(t, notAnObjectErr)

	invalid := []struct {
		name    string
		data    string
		wantErr string
	}{
		{
			name:    "not an object",
			data:    notAnObject,
			wantErr: "failed to unmarshal JSON-RPC message: " + notAnObjectErr.Error(),
		},
		{
			name:    "truncated",
			data:    truncatedJSON,
			wantErr: "failed to unmarshal JSON-RPC message: unexpected end of JSON input",
		},
		{
			name:    "null",
			data:    `null`,
			wantErr: "invalid JSON-RPC message: no method, result or error member",
		},
		{
			name:    "no method, result or error",
			data:    `{"jsonrpc":"2.0","id":1}`,
			wantErr: "invalid JSON-RPC message: no method, result or error member",
		},
		{
			name:    "request without jsonrpc",
			data:    `{"id":1,"method":"m"}`,
			wantErr: "failed to unmarshal JSON-RPC request: field jsonrpc in BaseJSONRPCRequest: required",
		},
		{
			name:    "notification without jsonrpc",
			data:    `{"method":"m"}`,
			wantErr: "failed to unmarshal JSON-RPC notification: field jsonrpc in BaseJSONRPCNotification: required",
		},
		{
			name:    "error that is not an object",
			data:    `{"jsonrpc":"2.0","id":1,"error":"x"}`,
			wantErr: "invalid JSON-RPC error: error member is not an object",
		},
		{
			name:    "null error",
			data:    `{"jsonrpc":"2.0","id":1,"error":null}`,
			wantErr: "invalid JSON-RPC error: error member is not an object",
		},
		{
			name:    "response without id",
			data:    `{"jsonrpc":"2.0","result":{}}`,
			wantErr: "failed to unmarshal JSON-RPC response: field id in BaseJSONRPCResponse: required",
		},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			got, err := transport.ParseMessage([]byte(tc.data))
			assert.EqualError(t, err, tc.wantErr)
			assert.Nil(t, got)
		})
	}
}

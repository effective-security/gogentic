package transport_test

import (
	"encoding/json"
	"testing"

	"github.com/effective-security/gogentic/mcp/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const truncatedJSON = `{`

func TestBaseJSONRPCRequest_UnmarshalJSON(t *testing.T) {
	t.Run("with params", func(t *testing.T) {
		var r transport.BaseJSONRPCRequest
		require.NoError(t, json.Unmarshal([]byte(`{"id":3,"jsonrpc":"2.0","method":"m","params":{"a":1}}`), &r))
		assert.Equal(t, transport.BaseJSONRPCRequest{
			Id:      3,
			Jsonrpc: "2.0",
			Method:  "m",
			Params:  json.RawMessage(`{"a":1}`),
		}, r)
	})

	t.Run("without params", func(t *testing.T) {
		var r transport.BaseJSONRPCRequest
		require.NoError(t, json.Unmarshal([]byte(`{"id":0,"jsonrpc":"2.0","method":"m"}`), &r))
		assert.Equal(t, transport.RequestId(0), r.Id)
		assert.Equal(t, "m", r.Method)
		assert.Empty(t, r.Params)
	})

	tcs := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "invalid json",
			input:   truncatedJSON,
			wantErr: "unexpected end of JSON input",
		},
		{
			name:    "wrong id type",
			input:   `{"id":"x","jsonrpc":"2.0","method":"m"}`,
			wantErr: "json: cannot unmarshal string into Go struct field .id of type transport.RequestId",
		},
		{
			name:    "missing id",
			input:   `{"jsonrpc":"2.0","method":"m"}`,
			wantErr: "field id in BaseJSONRPCRequest: required",
		},
		{
			name:    "missing jsonrpc",
			input:   `{"id":1,"method":"m"}`,
			wantErr: "field jsonrpc in BaseJSONRPCRequest: required",
		},
		{
			name:    "missing method",
			input:   `{"id":1,"jsonrpc":"2.0"}`,
			wantErr: "field method in BaseJSONRPCRequest: required",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var r transport.BaseJSONRPCRequest
			assert.EqualError(t, json.Unmarshal([]byte(tc.input), &r), tc.wantErr)
		})
	}
}

func TestBaseJSONRPCNotification_UnmarshalJSON(t *testing.T) {
	t.Run("with params", func(t *testing.T) {
		var n transport.BaseJSONRPCNotification
		require.NoError(t, json.Unmarshal([]byte(`{"jsonrpc":"2.0","method":"n","params":[1]}`), &n))
		assert.Equal(t, transport.BaseJSONRPCNotification{
			Jsonrpc: "2.0",
			Method:  "n",
			Params:  json.RawMessage(`[1]`),
		}, n)
	})

	t.Run("without params", func(t *testing.T) {
		var n transport.BaseJSONRPCNotification
		require.NoError(t, json.Unmarshal([]byte(`{"jsonrpc":"2.0","method":"n"}`), &n))
		assert.Equal(t, "n", n.Method)
		assert.Nil(t, n.Params)
	})

	tcs := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "invalid json",
			input:   truncatedJSON,
			wantErr: "unexpected end of JSON input",
		},
		{
			name:    "wrong method type",
			input:   `{"jsonrpc":"2.0","method":1}`,
			wantErr: "json: cannot unmarshal number into Go struct field .method of type string",
		},
		{
			name:    "missing jsonrpc",
			input:   `{"method":"n"}`,
			wantErr: "field jsonrpc in BaseJSONRPCNotification: required",
		},
		{
			name:    "missing method",
			input:   `{"jsonrpc":"2.0"}`,
			wantErr: "field method in BaseJSONRPCNotification: required",
		},
		{
			name:    "id is not allowed",
			input:   `{"jsonrpc":"2.0","method":"n","id":1}`,
			wantErr: "field id in BaseJSONRPCNotification: not allowed",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var n transport.BaseJSONRPCNotification
			assert.EqualError(t, json.Unmarshal([]byte(tc.input), &n), tc.wantErr)
		})
	}
}

func TestBaseJSONRPCResponse_UnmarshalJSON(t *testing.T) {
	var r transport.BaseJSONRPCResponse
	require.NoError(t, json.Unmarshal([]byte(`{"id":5,"jsonrpc":"2.0","result":{"ok":true}}`), &r))
	assert.Equal(t, transport.BaseJSONRPCResponse{
		Id:      5,
		Jsonrpc: "2.0",
		Result:  json.RawMessage(`{"ok":true}`),
	}, r)

	tcs := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "invalid json",
			input:   truncatedJSON,
			wantErr: "unexpected end of JSON input",
		},
		{
			name:    "wrong jsonrpc type",
			input:   `{"id":1,"jsonrpc":2,"result":{}}`,
			wantErr: "json: cannot unmarshal number into Go struct field .jsonrpc of type string",
		},
		{
			name:    "missing id",
			input:   `{"jsonrpc":"2.0","result":{}}`,
			wantErr: "field id in BaseJSONRPCResponse: required",
		},
		{
			name:    "missing jsonrpc",
			input:   `{"id":1,"result":{}}`,
			wantErr: "field jsonrpc in BaseJSONRPCResponse: required",
		},
		{
			name:    "missing result",
			input:   `{"id":1,"jsonrpc":"2.0"}`,
			wantErr: "field result in BaseJSONRPCResponse: required",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var resp transport.BaseJSONRPCResponse
			assert.EqualError(t, json.Unmarshal([]byte(tc.input), &resp), tc.wantErr)
		})
	}
}

func TestBaseJsonRpcMessage_MarshalAndID(t *testing.T) {
	errMsg := &transport.BaseJSONRPCError{
		Jsonrpc: "2.0",
		Id:      9,
		Error: transport.BaseJSONRPCErrorInner{
			Code:    -32000,
			Message: "bad",
			Data:    "details",
		},
	}

	tcs := []struct {
		name   string
		msg    *transport.BaseJsonRpcMessage
		wantJS string
		wantID transport.RequestId
	}{
		{
			name: "request",
			msg: transport.NewBaseMessageRequest(&transport.BaseJSONRPCRequest{
				Id:      7,
				Jsonrpc: "2.0",
				Method:  "m",
			}),
			wantJS: `{"id":7,"jsonrpc":"2.0","method":"m"}`,
			wantID: 7,
		},
		{
			name: "notification",
			msg: transport.NewBaseMessageNotification(&transport.BaseJSONRPCNotification{
				Jsonrpc: "2.0",
				Method:  "n",
				Params:  json.RawMessage(`{"x":1}`),
			}),
			wantJS: `{"jsonrpc":"2.0","method":"n","params":{"x":1}}`,
			wantID: 0,
		},
		{
			name: "response",
			msg: transport.NewBaseMessageResponse(&transport.BaseJSONRPCResponse{
				Id:      8,
				Jsonrpc: "2.0",
				Result:  json.RawMessage(`[]`),
			}),
			wantJS: `{"id":8,"jsonrpc":"2.0","result":[]}`,
			wantID: 8,
		},
		{
			name:   "error",
			msg:    transport.NewBaseMessageError(errMsg),
			wantJS: `{"id":9,"jsonrpc":"2.0","error":{"code":-32000,"message":"bad","data":"details"}}`,
			wantID: 9,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.msg)
			require.NoError(t, err)
			assert.JSONEq(t, tc.wantJS, string(b))
			assert.Equal(t, tc.wantID, tc.msg.MessageID())
		})
	}

	t.Run("error message is a copy", func(t *testing.T) {
		msg := transport.NewBaseMessageError(errMsg)
		assert.Equal(t, transport.BaseMessageTypeJSONRPCErrorType, msg.Type)
		assert.NotSame(t, errMsg, msg.JsonRpcError)
		assert.Equal(t, *errMsg, *msg.JsonRpcError)
	})

	t.Run("unknown type", func(t *testing.T) {
		msg := &transport.BaseJsonRpcMessage{
			Type: "bogus",
		}
		_, err := json.Marshal(msg)
		assert.EqualError(t, err, "json: error calling MarshalJSON for type *transport.BaseJsonRpcMessage: failed to marshal: unknown message type: bogus")
		assert.Equal(t, transport.RequestId(0), msg.MessageID())
	})
}

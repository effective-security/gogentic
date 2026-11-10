package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitializeResponse_UnmarshalJSON(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		var r InitializeResponse
		err := json.Unmarshal([]byte(`{
			"_meta":{"k":"v"},
			"capabilities":{"tools":{"listChanged":true},"logging":{}},
			"instructions":"be nice",
			"protocolVersion":"2025-06-18",
			"serverInfo":{"name":"srv","version":"9"}
		}`), &r)
		require.NoError(t, err)

		assert.Equal(t, initializeResultMeta{"k": "v"}, r.Meta)
		assert.Equal(t, "2025-06-18", r.ProtocolVersion)
		assert.Equal(t, implementation{Name: "srv", Version: "9"}, r.ServerInfo)
		require.NotNil(t, r.Instructions)
		assert.Equal(t, "be nice", *r.Instructions)
		require.NotNil(t, r.Capabilities.Tools)
		require.NotNil(t, r.Capabilities.Tools.ListChanged)
		assert.True(t, *r.Capabilities.Tools.ListChanged)
		assert.Equal(t, ServerCapabilitiesLogging{}, r.Capabilities.Logging)
		assert.Nil(t, r.Capabilities.Prompts)
		assert.Nil(t, r.Capabilities.Resources)
	})

	tcs := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "not an object",
			input:   `[]`,
			wantErr: "json: cannot unmarshal array into Go value of type map[string]interface {}",
		},
		{
			name:    "missing capabilities",
			input:   `{"protocolVersion":"1","serverInfo":{"name":"a","version":"b"}}`,
			wantErr: "field capabilities in initializeResult: required",
		},
		{
			name:    "missing protocolVersion",
			input:   `{"capabilities":{},"serverInfo":{"name":"a","version":"b"}}`,
			wantErr: "field protocolVersion in initializeResult: required",
		},
		{
			name:    "missing serverInfo",
			input:   `{"capabilities":{},"protocolVersion":"1"}`,
			wantErr: "field serverInfo in initializeResult: required",
		},
		{
			name:    "wrong protocolVersion type",
			input:   `{"capabilities":{},"protocolVersion":1,"serverInfo":{"name":"a","version":"b"}}`,
			wantErr: "json: cannot unmarshal number into Go struct field Plain.protocolVersion of type string",
		},
		{
			name:    "serverInfo not an object",
			input:   `{"capabilities":{},"protocolVersion":"1","serverInfo":5}`,
			wantErr: "json: cannot unmarshal number into Go value of type map[string]interface {}",
		},
		{
			name:    "serverInfo missing name",
			input:   `{"capabilities":{},"protocolVersion":"1","serverInfo":{"version":"b"}}`,
			wantErr: "field name in implementation: required",
		},
		{
			name:    "serverInfo missing version",
			input:   `{"capabilities":{},"protocolVersion":"1","serverInfo":{"name":"a"}}`,
			wantErr: "field version in implementation: required",
		},
		{
			name:    "serverInfo wrong name type",
			input:   `{"capabilities":{},"protocolVersion":"1","serverInfo":{"name":1,"version":"b"}}`,
			wantErr: "json: cannot unmarshal number into Go struct field Plain.name of type string",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var r InitializeResponse
			assert.EqualError(t, json.Unmarshal([]byte(tc.input), &r), tc.wantErr)
		})
	}
}

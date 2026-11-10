package openaiclient

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/effective-security/gogentic/pkg/llms"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/respjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListModels_NullPage(t *testing.T) {
	t.Parallel()

	stub, srv := newStubServer(t, http.StatusOK, `null`)
	c := newTestClient(t, ProviderOpenRouter, testModel, srv.URL, http.DefaultClient)

	models, err := c.ListModels(context.Background())
	require.NoError(t, err)
	assert.Nil(t, models)
	assert.Equal(t, "/models", stub.last().path)
}

func TestListModels_UpstreamError(t *testing.T) {
	t.Parallel()

	_, srv := newStubServer(t, http.StatusUnauthorized, `{"error":{"message":"bad key"}}`)
	c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)

	_, err := c.ListModels(context.Background())
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "failed to list models: "), err.Error())
	var apiErr *openaisdk.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
}

func TestListModels_InvalidFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		reply      string
		wantPrefix string
	}{
		{
			name:       "total count",
			reply:      `{"object":"list","data":[],"total_count":"many"}`,
			wantPrefix: "invalid total_count: ",
		},
		{
			name:       "links",
			reply:      `{"object":"list","data":[],"links":[]}`,
			wantPrefix: "invalid links: ",
		},
		{
			name:       "name",
			reply:      `{"object":"list","data":[{"id":"m1","object":"model","created":1,"owned_by":"x","name":7}]}`,
			wantPrefix: "model m1: invalid name: ",
		},
		{
			name:       "expiration date type",
			reply:      `{"object":"list","data":[{"id":"m1","object":"model","created":1,"owned_by":"x","expiration_date":7}]}`,
			wantPrefix: "model m1: invalid expiration_date: ",
		},
		{
			name:       "expiration date format",
			reply:      `{"object":"list","data":[{"id":"m1","object":"model","created":1,"owned_by":"x","expiration_date":"soon"}]}`,
			wantPrefix: "model m1: invalid expiration_date: ",
		},
		{
			name:       "reasoning",
			reply:      `{"object":"list","data":[{"id":"m1","object":"model","created":1,"owned_by":"x","reasoning":"yes"}]}`,
			wantPrefix: "model m1: invalid reasoning: ",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, srv := newStubServer(t, http.StatusOK, tc.reply)
			c := newTestClient(t, ProviderOpenRouter, testModel, srv.URL, http.DefaultClient)

			_, err := c.ListModels(context.Background())
			require.Error(t, err)
			assert.True(t, strings.HasPrefix(err.Error(), tc.wantPrefix), err.Error())
		})
	}
}

func TestListModels_IncompleteList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		reply string
		want  string
	}{
		{
			name:  "total above returned",
			reply: `{"object":"list","data":[{"id":"m1","object":"model","created":1,"owned_by":"x"}],"total_count":2}`,
			want:  "model list is incomplete: got 1 of 2 models",
		},
		{
			name:  "next page",
			reply: `{"object":"list","data":[],"links":{"next":"/models?page=2"}}`,
			want:  "model list is incomplete: got 0 models and a next page",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, srv := newStubServer(t, http.StatusOK, tc.reply)
			c := newTestClient(t, ProviderOpenRouter, testModel, srv.URL, http.DefaultClient)

			_, err := c.ListModels(context.Background())
			assert.EqualError(t, err, tc.want)
		})
	}
}

func TestListModels_CompleteList(t *testing.T) {
	t.Parallel()

	const reply = `{"object":"list","data":[` +
		`{"id":"m1","object":"model","created":1700000000,"owned_by":"x","name":"Model One","expiration_date":null,"reasoning":{"supported_efforts":["minimal","low","high","future"],"default_effort":"low"}},` +
		`{"id":"m2","object":"model","created":0,"owned_by":"x","expiration_date":"2027-01-02"}` +
		`],"total_count":2,"links":{"next":null}}`
	_, srv := newStubServer(t, http.StatusOK, reply)
	c := newTestClient(t, ProviderOpenRouter, testModel, srv.URL, http.DefaultClient)

	models, err := c.ListModels(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []llms.ModelInfo{
		{
			ID:            "m1",
			DisplayName:   "Model One",
			Created:       time.Unix(1700000000, 0).UTC(),
			Lifecycle:     llms.ModelLifecycleActive,
			Efforts:       llms.NewEffortSet(llms.ReasoningEffortLow, llms.ReasoningEffortHigh),
			DefaultEffort: llms.ReasoningEffortLow,
		},
		{
			ID:        "m2",
			Lifecycle: llms.ModelLifecycleDeprecated,
			RetiresAt: time.Date(2027, time.January, 2, 0, 0, 0, 0, time.UTC),
			Efforts:   llms.NewEffortSet(),
		},
	}, models)
}

func TestDecodeField(t *testing.T) {
	t.Parallel()

	var name string
	found, err := decodeField(nil, fieldName, &name)
	require.NoError(t, err)
	assert.False(t, found, "a missing field is not found")

	// A zero Field is the SDK's marker for an omitted value.
	found, err = decodeField(map[string]respjson.Field{fieldName: {}}, fieldName, &name)
	require.NoError(t, err)
	assert.False(t, found, "an omitted field is not found")
	assert.Empty(t, name)

	found, err = decodeField(map[string]respjson.Field{fieldName: respjson.NewField(`"display"`)}, fieldName, &name)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "display", name)

	found, err = decodeField(map[string]respjson.Field{fieldName: respjson.NewField(`7`)}, fieldName, &name)
	assert.True(t, found)
	var typeErr *json.UnmarshalTypeError
	require.ErrorAs(t, err, &typeErr)
	assert.True(t, strings.HasPrefix(err.Error(), "invalid name: "), err.Error())
}

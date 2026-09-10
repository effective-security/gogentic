package dialect

import (
	"encoding/json"
	"testing"

	"github.com/effective-security/gogentic/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpaqueSealRoundTrip(t *testing.T) {
	sealed, err := SealOpaque("target-a", json.RawMessage(`{"signature":"abc"}`))
	require.NoError(t, err)
	source, state, err := OpenOpaque(sealed)
	require.NoError(t, err)
	assert.Equal(t, "target-a", source)
	assert.JSONEq(t, `{"signature":"abc"}`, string(state))

	_, err = SealOpaque("", json.RawMessage(`{}`))
	require.Error(t, err)
	_, _, err = OpenOpaque("not base64!")
	assert.Equal(t, router.KindInvalidRequest, router.AsError(err).Kind)
	_, _, err = OpenOpaque("e30")
	require.Error(t, err, "envelope without source is rejected")
}

func TestHTTPStatus(t *testing.T) {
	assert.Equal(t, 400, HTTPStatus(router.KindInvalidRequest))
	assert.Equal(t, 400, HTTPStatus(router.KindUnsupported))
	assert.Equal(t, 404, HTTPStatus(router.KindModelNotFound))
	assert.Equal(t, 429, HTTPStatus(router.KindRateLimited))
	assert.Equal(t, 502, HTTPStatus(router.KindUpstream))
	assert.Equal(t, 503, HTTPStatus(router.KindSelectionUnavailable))
	assert.Equal(t, 504, HTTPStatus(router.KindTimeout))
	assert.Equal(t, 499, HTTPStatus(router.KindCancelled))
	assert.Equal(t, 500, HTTPStatus(router.KindInternal))
	assert.Equal(t, 500, HTTPStatus(router.Kind("other")))
}

func TestInspect(t *testing.T) {
	lenient := DecodeOptions{}
	strict := DecodeOptions{Strict: true}
	require.NoError(t, Inspect([]byte(`{"a":1,"a":2}`), lenient))
	require.Error(t, Inspect([]byte(`{"a":1,"a":2}`), strict))
	require.Error(t, Inspect([]byte(`{"a":1} x`), lenient))
	require.Error(t, Inspect([]byte(`{"a":`), lenient))
	deep := ""
	for range MaxDepth + 2 {
		deep += "["
	}
	require.Error(t, Inspect([]byte(deep), lenient))
}

func TestObjectNullAndUnknownHandling(t *testing.T) {
	raw := json.RawMessage(`{"model":"m","stop":null,"content":null,"extra":true,"n":2,"t":0.5}`)
	o, err := ParseObject(raw, "", DecodeOptions{}, "content")
	require.NoError(t, err)
	assert.False(t, o.Has("stop"), "lenient null is absent")
	assert.True(t, o.Has("content"), "nullable null stays present")
	assert.True(t, IsNull(o.Raw("content")))
	model, present, err := o.String("model")
	require.NoError(t, err)
	assert.True(t, present)
	assert.Equal(t, "m", model)
	n, _, err := o.Int("n")
	require.NoError(t, err)
	assert.EqualValues(t, 2, n)
	_, _, err = o.Int("t")
	require.Error(t, err)
	f, _, err := o.Float("t")
	require.NoError(t, err)
	assert.Equal(t, 0.5, f)
	require.NoError(t, o.Finish(), "lenient ignores unknown fields")

	_, err = ParseObject(raw, "", DecodeOptions{Strict: true}, "content")
	re := router.AsError(err)
	assert.Equal(t, router.KindInvalidRequest, re.Kind)
	assert.Equal(t, "stop", re.Param)

	o, err = ParseObject(json.RawMessage(`{"model":"m","extra":true}`), "req", DecodeOptions{Strict: true})
	require.NoError(t, err)
	_, _, err = o.String("model")
	require.NoError(t, err)
	err = o.Finish()
	re = router.AsError(err)
	assert.Equal(t, "req.extra", re.Param)

	_, err = ParseObject(json.RawMessage(`[]`), "messages[0]", DecodeOptions{})
	assert.Equal(t, "messages[0]", router.AsError(err).Param)
	assert.Equal(t, "messages[1]", Index("messages", 1))
}

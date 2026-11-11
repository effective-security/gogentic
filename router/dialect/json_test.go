package dialect

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	paramRequest  = "request"
	objectPath    = "req"
	msgMalformed  = "malformed JSON"
	msgMalObject  = "malformed JSON object"
	msgMalArray   = "malformed JSON array"
	msgExpectObj  = "expected an object"
	reasoningPath = "reasoning"
	notIssued     = "reasoning state is not a router-issued value"
)

// requireInvalid asserts a KindInvalidRequest error with exact param and message.
func requireInvalid(t *testing.T, err error, param, msg string) {
	t.Helper()
	require.Error(t, err)
	re := router.AsError(err)
	assert.Equal(t, router.KindInvalidRequest, re.Kind)
	assert.Equal(t, param, re.Param)
	assert.Equal(t, msg, re.Message)
}

func TestInspectRejections(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		msg  string
	}{
		{
			name: "empty input",
			raw:  ``,
			msg:  msgMalformed,
		},
		{
			name: "non-string key",
			raw:  `{1:2}`,
			msg:  msgMalObject,
		},
		{
			name: "unterminated object",
			raw:  `{"a":"x"`,
			msg:  msgMalObject,
		},
		{
			name: "object closed as array",
			raw:  `{"a":1]`,
			msg:  msgMalObject,
		},
		{
			name: "unterminated array",
			raw:  `[1,"x"`,
			msg:  msgMalformed,
		},
		{
			name: "array closed as object",
			raw:  `[1}`,
			msg:  msgMalArray,
		},
		{
			name: "invalid element",
			raw:  `[1,}`,
			msg:  msgMalformed,
		},
		{
			name: "trailing value",
			raw:  `{} {}`,
			msg:  "unexpected data after JSON value",
		},
		{
			name: "nesting too deep",
			raw:  strings.Repeat(`{"a":`, MaxDepth+1) + `1` + strings.Repeat(`}`, MaxDepth+1),
			msg:  "JSON nesting exceeds 32",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireInvalid(t, Inspect([]byte(tc.raw), DecodeOptions{}), paramRequest, tc.msg)
		})
	}

	err := Inspect([]byte(`{"a":{"b":1,"b":2}}`), DecodeOptions{Strict: true})
	requireInvalid(t, err, paramRequest, `duplicate JSON key "b"`)
	require.NoError(t, Inspect([]byte(`{"a":[1,"x",null,true,{"b":[]}]}`), DecodeOptions{Strict: true}))
	require.NoError(t, Inspect([]byte(strings.Repeat(`[`, MaxDepth+1)+strings.Repeat(`]`, MaxDepth+1)), DecodeOptions{}))
}

func TestParseObjectRejections(t *testing.T) {
	for _, raw := range []string{``, `null`, ` null `, `[]`, `"s"`} {
		_, err := ParseObject(json.RawMessage(raw), objectPath, DecodeOptions{})
		requireInvalid(t, err, objectPath, msgExpectObj)
	}

	o, err := ParseObject(json.RawMessage(`{"keep":null,"drop":null}`), objectPath, DecodeOptions{Strict: true}, "keep", "drop")
	require.NoError(t, err)
	assert.True(t, o.Has("keep"), "strict nullable null stays present")
	assert.True(t, o.Has("drop"))
	assert.Equal(t, objectPath, o.Path())
	assert.Equal(t, DecodeOptions{Strict: true}, o.Options())
	assert.Equal(t, objectPath+".keep", o.Field("keep"))

	o, err = ParseObject(json.RawMessage(`{"a":1}`), "", DecodeOptions{})
	require.NoError(t, err)
	assert.Equal(t, "a", o.Field("a"), "root fields have no prefix")
	assert.Nil(t, o.Raw("missing"))
}

func TestObjectAccessors(t *testing.T) {
	raw := json.RawMessage(`{
		"s":"text","b":true,"i":42,"f":1.5,"arr":[1,"x"],"obj":{"k":"v"},"m":{"a":"b"},
		"badS":1,"badB":"yes","badI":"1","fracI":1.5,"badF":"x","hugeF":1e400,"bigI":1e30,
		"badArr":{},"badObj":[],"badM":{"a":1},"nullChild":{"n":null}
	}`)
	o, err := ParseObject(raw, objectPath, DecodeOptions{})
	require.NoError(t, err)

	s, present, err := o.String("s")
	require.NoError(t, err)
	assert.True(t, present)
	assert.Equal(t, "text", s)
	b, present, err := o.Bool("b")
	require.NoError(t, err)
	assert.True(t, present)
	assert.True(t, b)
	i, present, err := o.Int("i")
	require.NoError(t, err)
	assert.True(t, present)
	assert.EqualValues(t, 42, i)
	f, present, err := o.Float("f")
	require.NoError(t, err)
	assert.True(t, present)
	assert.Equal(t, 1.5, f)
	arr, present, err := o.Array("arr")
	require.NoError(t, err)
	assert.True(t, present)
	require.Len(t, arr, 2)
	assert.JSONEq(t, `"x"`, string(arr[1]))
	child, present, err := o.Object("obj")
	require.NoError(t, err)
	assert.True(t, present)
	assert.Equal(t, objectPath+".obj", child.Path())
	m, present, err := o.StringMap("m")
	require.NoError(t, err)
	assert.True(t, present)
	assert.Equal(t, map[string]string{"a": "b"}, m)

	_, present, err = o.String("badS")
	assert.True(t, present)
	requireInvalid(t, err, objectPath+".badS", "expected a string")
	_, present, err = o.Bool("badB")
	assert.True(t, present)
	requireInvalid(t, err, objectPath+".badB", "expected a boolean")
	_, present, err = o.Int("badI")
	assert.True(t, present)
	requireInvalid(t, err, objectPath+".badI", "expected an integer")
	_, _, err = o.Int("fracI")
	requireInvalid(t, err, objectPath+".fracI", "expected an integer")
	_, _, err = o.Int("bigI")
	requireInvalid(t, err, objectPath+".bigI", "expected an integer")
	_, present, err = o.Float("badF")
	assert.True(t, present)
	requireInvalid(t, err, objectPath+".badF", "expected a number")
	_, _, err = o.Float("hugeF")
	requireInvalid(t, err, objectPath+".hugeF", "expected a finite number")
	_, present, err = o.Array("badArr")
	assert.True(t, present)
	requireInvalid(t, err, objectPath+".badArr", "expected an array")
	_, present, err = o.Object("badObj")
	assert.True(t, present)
	requireInvalid(t, err, objectPath+".badObj", msgExpectObj)
	_, present, err = o.StringMap("badM")
	assert.True(t, present)
	requireInvalid(t, err, objectPath+".badM", "expected an object of strings")

	child, _, err = o.Object("nullChild", "n")
	require.NoError(t, err)
	assert.True(t, child.Has("n"), "nullable keys propagate to child objects")

	const missingKey = "none"
	_, present, err = o.String(missingKey)
	require.NoError(t, err)
	assert.False(t, present)
	_, present, err = o.Bool(missingKey)
	require.NoError(t, err)
	assert.False(t, present)
	_, present, err = o.Int(missingKey)
	require.NoError(t, err)
	assert.False(t, present)
	_, present, err = o.Float(missingKey)
	require.NoError(t, err)
	assert.False(t, present)
	_, present, err = o.Array(missingKey)
	require.NoError(t, err)
	assert.False(t, present)
	_, present, err = o.Object(missingKey)
	require.NoError(t, err)
	assert.False(t, present)
	_, present, err = o.StringMap(missingKey)
	require.NoError(t, err)
	assert.False(t, present)

	err = o.Unsupported("s")
	re := router.AsError(err)
	assert.Equal(t, router.KindUnsupported, re.Kind)
	assert.Equal(t, objectPath+".s", re.Param)
	assert.Equal(t, "unsupported value for this field", re.Message)
}

func TestJSONShapePredicates(t *testing.T) {
	assert.True(t, IsNull(nil))
	assert.True(t, IsNull(json.RawMessage(" null\n")))
	assert.False(t, IsNull(json.RawMessage(`"null"`)))
	assert.True(t, IsString(json.RawMessage(` "x"`)))
	assert.False(t, IsString(json.RawMessage(`  `)))
	assert.True(t, IsArray(json.RawMessage("\t[]")))
	assert.False(t, IsArray(json.RawMessage(`{}`)))
	assert.True(t, IsObject(json.RawMessage(" {}")))
	assert.False(t, IsObject(nil))
}

func TestOpenOpaqueRejections(t *testing.T) {
	_, _, err := OpenOpaque(strings.Repeat("A", MaxOpaqueBytes+1))
	requireInvalid(t, err, reasoningPath, "reasoning state too large")

	seal := base64.RawURLEncoding.EncodeToString
	cases := []struct {
		name   string
		sealed string
	}{
		{
			name:   "not base64",
			sealed: "not base64!",
		},
		{
			name:   "padded base64",
			sealed: base64.URLEncoding.EncodeToString([]byte(`{"s":"a","d":{}}`)),
		},
		{
			name:   "not JSON",
			sealed: seal([]byte("not-json")),
		},
		{
			name:   "empty envelope",
			sealed: seal([]byte(`{}`)),
		},
		{
			name:   "missing state",
			sealed: seal([]byte(`{"s":"a"}`)),
		},
		{
			name:   "empty source",
			sealed: seal([]byte(`{"s":"","d":{}}`)),
		},
		{
			name:   "non-string source",
			sealed: seal([]byte(`{"s":1,"d":{}}`)),
		},
		{
			name:   "null state",
			sealed: seal([]byte(`{"s":"a","d":null}`)),
		},
		{
			name:   "spaced null state",
			sealed: seal([]byte(`{"s":"a","d": null }`)),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source, state, err := OpenOpaque(tc.sealed)
			requireInvalid(t, err, reasoningPath, notIssued)
			assert.Empty(t, source)
			assert.Nil(t, state)
		})
	}

	source, state, err := OpenOpaque(seal([]byte(`{"s":"a","d":[1]}`)))
	require.NoError(t, err)
	assert.Equal(t, "a", source)
	assert.JSONEq(t, `[1]`, string(state))
}

func TestSealOpaqueRejections(t *testing.T) {
	_, err := SealOpaque("a", nil)
	require.EqualError(t, err, "dialect: opaque state requires a source and a payload")

	sealed, err := SealOpaque("a", json.RawMessage("null"))
	require.EqualError(t, err, "dialect: opaque state requires a source and a payload")
	assert.Empty(t, sealed)

	sealed, err = SealOpaque("a", json.RawMessage(`{`))
	require.Error(t, err)
	assert.Empty(t, sealed)
	assert.Contains(t, err.Error(), "dialect: seal opaque state")
}

package dialect

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"strconv"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/router"
)

// MaxDepth bounds JSON nesting accepted from clients.
const MaxDepth = 32

// Inspect checks that raw is one well-formed JSON value without trailing data
// and within MaxDepth. In strict mode it also rejects duplicate object keys.
// Null handling is decided per field by Object.
func Inspect(raw []byte, opts DecodeOptions) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := inspectValue(d, 0, opts.Strict); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return router.Invalid("request", "unexpected data after JSON value")
	}
	return nil
}

func inspectValue(d *json.Decoder, depth int, strict bool) error {
	if depth > MaxDepth {
		return router.Invalid("request", "JSON nesting exceeds %d", MaxDepth)
	}
	t, err := d.Token()
	if err != nil {
		return router.Invalid("request", "malformed JSON")
	}
	switch t {
	case json.Delim('{'):
		seen := map[string]bool{}
		for d.More() {
			k, err := d.Token()
			if err != nil {
				return router.Invalid("request", "malformed JSON object")
			}
			s, ok := k.(string)
			if !ok {
				return router.Invalid("request", "malformed JSON key")
			}
			if strict && seen[s] {
				return router.Invalid("request", "duplicate JSON key %q", s)
			}
			seen[s] = true
			if err := inspectValue(d, depth+1, strict); err != nil {
				return err
			}
		}
		if _, err := d.Token(); err != nil {
			return router.Invalid("request", "malformed JSON object")
		}
	case json.Delim('['):
		for d.More() {
			if err := inspectValue(d, depth+1, strict); err != nil {
				return err
			}
		}
		if _, err := d.Token(); err != nil {
			return router.Invalid("request", "malformed JSON array")
		}
	}
	return nil
}

// Object is one decoded JSON object with dialect-aware null and unknown-field
// handling. Path names the object in error messages ("messages[2]").
type Object struct {
	path   string
	opts   DecodeOptions
	fields map[string]json.RawMessage
	used   map[string]bool
}

// ParseObject decodes raw as an object. In lenient mode null members are treated
// as absent. In strict mode a null member is rejected unless its key is listed in
// nullable. A non-object value is a client error naming path.
func ParseObject(raw json.RawMessage, path string, opts DecodeOptions, nullable ...string) (*Object, error) {
	if IsNull(raw) {
		return nil, router.Invalid(path, "expected an object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, router.Invalid(path, "expected an object")
	}
	allowNull := map[string]bool{}
	for _, k := range nullable {
		allowNull[k] = true
	}
	for k, v := range fields {
		if !IsNull(v) {
			continue
		}
		if opts.Strict && !allowNull[k] {
			return nil, router.Invalid(join(path, k), "null is not accepted for this field")
		}
		if !allowNull[k] {
			delete(fields, k)
		}
	}
	return &Object{
		path:   path,
		opts:   opts,
		fields: fields,
		used:   map[string]bool{},
	}, nil
}

// Path returns the object's location for error messages.
func (o *Object) Path() string { return o.path }

// Options returns the decode options this object was parsed with.
func (o *Object) Options() DecodeOptions { return o.opts }

// Has reports whether key is present (and, in lenient mode, non-null).
func (o *Object) Has(key string) bool {
	_, ok := o.fields[key]
	return ok
}

// Raw returns the member bytes, or nil when absent. It marks the key as known.
func (o *Object) Raw(key string) json.RawMessage {
	o.used[key] = true
	return o.fields[key]
}

// Field returns the error-message path of a member.
func (o *Object) Field(key string) string { return join(o.path, key) }

// String reads a string member. present is false when absent.
func (o *Object) String(key string) (value string, present bool, err error) {
	raw := o.Raw(key)
	if raw == nil {
		return "", false, nil
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", true, router.Invalid(o.Field(key), "expected a string")
	}
	return value, true, nil
}

// Bool reads a boolean member.
func (o *Object) Bool(key string) (value bool, present bool, err error) {
	raw := o.Raw(key)
	if raw == nil {
		return false, false, nil
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, true, router.Invalid(o.Field(key), "expected a boolean")
	}
	return value, true, nil
}

// Int reads an integral number member. Fractions and out-of-range values fail.
func (o *Object) Int(key string) (value int64, present bool, err error) {
	raw := o.Raw(key)
	if raw == nil {
		return 0, false, nil
	}
	var n json.Number
	if err := unmarshalNumber(raw, &n); err != nil {
		return 0, true, router.Invalid(o.Field(key), "expected an integer")
	}
	value, err = n.Int64()
	if err != nil {
		return 0, true, router.Invalid(o.Field(key), "expected an integer")
	}
	return value, true, nil
}

// Float reads a finite number member.
func (o *Object) Float(key string) (value float64, present bool, err error) {
	raw := o.Raw(key)
	if raw == nil {
		return 0, false, nil
	}
	var n json.Number
	if err := unmarshalNumber(raw, &n); err != nil {
		return 0, true, router.Invalid(o.Field(key), "expected a number")
	}
	value, err = n.Float64()
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, true, router.Invalid(o.Field(key), "expected a finite number")
	}
	return value, true, nil
}

// Array reads an array member as raw elements.
func (o *Object) Array(key string) (items []json.RawMessage, present bool, err error) {
	raw := o.Raw(key)
	if raw == nil {
		return nil, false, nil
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, true, router.Invalid(o.Field(key), "expected an array")
	}
	return items, true, nil
}

// Object reads a nested object member with the same options.
func (o *Object) Object(key string, nullable ...string) (child *Object, present bool, err error) {
	raw := o.Raw(key)
	if raw == nil {
		return nil, false, nil
	}
	child, err = ParseObject(raw, o.Field(key), o.opts, nullable...)
	if err != nil {
		return nil, true, err
	}
	return child, true, nil
}

// StringMap reads an object of string values.
func (o *Object) StringMap(key string) (value map[string]string, present bool, err error) {
	raw := o.Raw(key)
	if raw == nil {
		return nil, false, nil
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, true, router.Invalid(o.Field(key), "expected an object of strings")
	}
	return value, true, nil
}

// Finish must be called after all known members were read. In strict mode any
// unread member is an unknown field and rejected; in lenient mode it is ignored.
func (o *Object) Finish() error {
	if !o.opts.Strict {
		return nil
	}
	for k := range o.fields {
		if !o.used[k] {
			return router.Invalid(o.Field(k), "unknown field")
		}
	}
	return nil
}

// Unsupported reports a known member whose value the dialect cannot honor.
func (o *Object) Unsupported(key string) error {
	return router.Unsupported(o.Field(key), "unsupported value for this field")
}

// IsNull reports whether raw is absent or the JSON null literal.
func IsNull(raw json.RawMessage) bool {
	return len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// IsString reports whether raw is a JSON string.
func IsString(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && t[0] == '"'
}

// IsArray reports whether raw is a JSON array.
func IsArray(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && t[0] == '['
}

// IsObject reports whether raw is a JSON object.
func IsObject(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && t[0] == '{'
}

// Index formats an array element path such as "messages[2]".
func Index(path string, i int) string {
	return path + "[" + strconv.Itoa(i) + "]"
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func unmarshalNumber(raw json.RawMessage, n *json.Number) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return errors.WithMessage(err, "decode number")
	}
	num, ok := v.(json.Number)
	if !ok {
		return errors.New("not a number")
	}
	*n = num
	return nil
}

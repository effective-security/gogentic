package schema

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/cespare/xxhash/v2"
	"github.com/invopop/jsonschema"
	orderedmap "github.com/pb33f/ordered-map/v2"
)

// Faker is an interface for generating structures with fake data. It can be
// implemented by custom types to provide exemplar values used in tests or
// documentation.
type Faker interface {
	Fake() any
}

var (
	cache   = make(map[reflect.Type]*Schema)
	cacheMu sync.RWMutex
)

type Schema struct {
	RawSchema *jsonschema.Schema
	// Parameters represents the Function parameters definition
	Parameters *jsonschema.Schema
}

// New creates a new Schema from the given Go type. The result contains both the
// raw reflected jsonschema and a flattened Parameters schema suitable for use
// as tool/function parameters.
func New(t reflect.Type) (*Schema, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	if s, ok := cache[t]; ok {
		return s, nil
	}

	s, err := buildSchema(t)
	if err != nil {
		return nil, err
	}
	cache[t] = s

	return s, nil
}

// String returns the Parameters schema as indented JSON for readability.
func (s *Schema) String() string {
	js, _ := json.MarshalIndent(s.Parameters, "", "  ")
	return string(js)
}

func buildSchema(t reflect.Type) (*Schema, error) {
	schema := JSONSchema(t)

	funcDef := ToFunctionSchema(t, schema)
	s := &Schema{
		RawSchema:  schema,
		Parameters: funcDef,
	}

	return s, nil
}

// defsRefPrefix is the prefix of a $ref to a local definition.
const defsRefPrefix = "#/$defs/"

// ToFunctionSchema converts a reflected Schema into a simplified, top-level
// object schema that exposes the first-level properties directly. This
// structure is more suitable for function/tool parameter documentation.
//
// A $ref to a local definition ("#/$defs/<name>") in a property or array
// items, at any depth, is replaced by a copy of that definition. A $ref that
// names no definition, or one already being inlined above it (a recursive
// type), is kept as is. tSchema and its definitions are not modified. tType
// is unused.
func ToFunctionSchema(tType reflect.Type, tSchema *jsonschema.Schema) *jsonschema.Schema {
	// find top level properties
	rootID := strings.TrimPrefix(tSchema.Ref, defsRefPrefix)

	defs := make(map[string]*jsonschema.Schema, len(tSchema.Definitions))
	root := tSchema
	for name, def := range tSchema.Definitions {
		if name == rootID {
			root = def
		} else {
			defs[name] = def
		}
	}

	r := refResolver{
		defs:     defs,
		inlining: make(map[string]bool),
	}
	return &jsonschema.Schema{
		Type:       root.Type,
		Properties: r.properties(root.Properties),
		Required:   root.Required,
	}
}

// refResolver inlines local $ref definitions into copies of the schemas it
// walks.
type refResolver struct {
	defs map[string]*jsonschema.Schema
	// inlining holds the definitions being inlined on the current path, so
	// that a recursive reference is kept instead of expanded forever.
	inlining map[string]bool
}

// schema returns s with the $refs in its properties and items resolved. s is
// returned unchanged when there is nothing to resolve.
func (r *refResolver) schema(s *jsonschema.Schema) *jsonschema.Schema {
	if s == nil {
		return nil
	}
	if s.Ref != "" {
		name := strings.TrimPrefix(s.Ref, defsRefPrefix)
		def, ok := r.defs[name]
		if !ok || r.inlining[name] {
			return s
		}
		r.inlining[name] = true
		defer delete(r.inlining, name)
		return r.schema(def)
	}
	if s.Properties == nil && s.Items == nil {
		return s
	}
	c := *s
	c.Properties = r.properties(s.Properties)
	c.Items = r.schema(s.Items)
	return &c
}

// properties returns a copy of props with every property schema resolved.
func (r *refResolver) properties(props *orderedmap.OrderedMap[string, *jsonschema.Schema]) *orderedmap.OrderedMap[string, *jsonschema.Schema] {
	if props == nil {
		return nil
	}
	res := jsonschema.NewProperties()
	for pair := props.Oldest(); pair != nil; pair = pair.Next() {
		res.Set(pair.Key, r.schema(pair.Value))
	}
	return res
}

// NameFromRef returns the definition name of a RawSchema that is a local
// reference ("#/$defs/MyStruct" gives "MyStruct"), or "" when it is not one.
// Schemas built by New are never references, so it returns "" for them.
func (s *Schema) NameFromRef() string {
	if s.RawSchema == nil {
		return ""
	}
	name, ok := strings.CutPrefix(s.RawSchema.Ref, defsRefPrefix)
	if !ok {
		return ""
	}
	return name
}

// JSONSchema returns the jsonschema model for a type using invopop/jsonschema.
// The reflector is configured to produce expanded structs and stable names to
// avoid collisions across packages with identically-named structs.
func JSONSchema(t reflect.Type) *jsonschema.Schema {
	// VS Code does not support the jsonschema version 2020-12
	jsonschema.Version = "http://json-schema.org/draft-07/schema#"

	r := new(jsonschema.Reflector)
	r.ExpandedStruct = true
	r.DoNotReference = true
	r.AllowAdditionalProperties = true

	// The Struct name could be same, but the package name is different
	// For example, all of the notification plugins have the same struct name - `NotifyConfig`
	// This would cause the json schema to be wrong `$ref` to the same name.
	// the following code is to fix this issue by adding the package name to the struct name
	// p.s. this issue has been reported in: https://github.com/invopop/jsonschema/issues/42

	r.Namer = func(t reflect.Type) string {
		name := t.Name()
		if t.Kind() == reflect.Struct {
			v := reflect.New(t)
			vt := v.Elem().Type()
			fullname := vt.PkgPath() + "/" + vt.Name()
			// add hash to name
			name = vt.Name() + "@" + strconv.FormatUint(xxhash.Sum64String(fullname), 10)
		}
		return name
	}

	return r.ReflectFromType(t)
}

// MustFromAny creates a jsonschema.Schema from an arbitrary value.
// It panics if the input cannot be marshaled into a valid schema.
// Useful in tests or when construction failures should be considered programmer errors.
func MustFromAny(t any) *jsonschema.Schema {
	js, err := json.Marshal(t)
	if err != nil {
		panic(err)
	}
	schema := &jsonschema.Schema{}
	err = json.Unmarshal(js, schema)
	if err != nil {
		panic(err)
	}
	return schema
}

// FromAny creates a jsonschema.Schema from an arbitrary value, returning an
// error if the input is invalid.
func FromAny(t any) (*jsonschema.Schema, error) {
	js, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	schema := &jsonschema.Schema{}
	err = json.Unmarshal(js, schema)
	if err != nil {
		return nil, err
	}
	return schema, nil
}

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

// ToFunctionSchema converts a reflected Schema into a simplified, top-level
// object schema by resolving local $ref entries and exposing first-level
// properties directly. This structure is more suitable for function/tool
// parameter documentation.
func ToFunctionSchema(tType reflect.Type, tSchema *jsonschema.Schema) *jsonschema.Schema {
	// find top level properties
	redID := strings.TrimPrefix(tSchema.Ref, "#/$defs/")

	var defs = make(map[string]*jsonschema.Schema)
	root := tSchema

	for name, def := range tSchema.Definitions {
		if name == redID {
			root = def
		} else {
			defs[name] = def
		}
	}

	res := &jsonschema.Schema{
		Type:       root.Type,
		Properties: root.Properties,
		Required:   root.Required,
	}

	resolveRefs(res.Properties, defs)

	return res
}

func resolveRefs(props *orderedmap.OrderedMap[string, *jsonschema.Schema], defs map[string]*jsonschema.Schema) {
	for pair := props.Oldest(); pair != nil; pair = pair.Next() {
		child := pair.Value
		if child.Ref != "" {
			name := strings.TrimPrefix(pair.Value.Ref, "#/$defs/")
			if def, ok := defs[name]; ok {
				pair.Value = def
			} else {
				// TODO: this is a hack to make it work
				panic("not found")
				// 	pair.Value = &jsonschema.Schema{
				// 		Type:        "object",
				// 		Description: child.Description,
				// 		Properties:  child.Properties,
				// 		Required:    child.Required,
				// 	}
			}
		}
		if child.Properties != nil {
			resolveRefs(child.Properties, defs)
		}
		if child.Items != nil && child.Items.Ref != "" {
			name := strings.TrimPrefix(child.Items.Ref, "#/$defs/")
			if def, ok := defs[name]; ok {
				child.Items = def
			} else {
				// TODO: this is a hack to make it work
				panic("not found")
				// 	child.Items = &jsonschema.Schema{
				// 		Type:        "object",
				// 		Description: child.Description,
				// 		Properties:  child.Properties,
				// 		Required:    child.Required,
				// 	}
			}
		}
	}
}

func (s *Schema) NameFromRef() string {
	return strings.Split(s.RawSchema.Ref, "/")[2] // ex: '#/$defs/MyStruct'
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

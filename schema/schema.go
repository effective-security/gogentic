package schema

import (
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/cespare/xxhash/v2"
	"github.com/effective-security/gogentic/utils"
	"github.com/invopop/jsonschema"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

// Faker is a interface for generating structures
// with fake data. It is used for generating test data.
type Faker interface {
	Fake() any
}

var (
	cache   = make(map[reflect.Type]*Schema)
	cacheMu sync.RWMutex
)

type Schema struct {
	*jsonschema.Schema
	// Parameters represents the Function parameters definition
	Parameters any
}

// New creates a new schema from the given type
func New(t reflect.Type) (*Schema, error) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()

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

func (s *Schema) String() string {
	return utils.ToJSONIndent(s.Parameters)
}

func buildSchema(t reflect.Type) (*Schema, error) {
	schema := JSONSchema(t)

	funcDef := ToFunctionSchema(t, schema)
	s := &Schema{
		Schema:     schema,
		Parameters: funcDef,
	}

	return s, nil
}

func ToFunctionSchema(tType reflect.Type, tSchema *jsonschema.Schema) *jsonschema.Schema {
	// find top level properties
	redID := strings.TrimPrefix(tSchema.Ref, "#/$defs/")

	var defs = make(map[string]*jsonschema.Schema)
	var root *jsonschema.Schema

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
	return strings.Split(s.Ref, "/")[2] // ex: '#/$defs/MyStruct'
}

// JSONSchema return the json schema of the configuration
func JSONSchema(t reflect.Type) *jsonschema.Schema {
	r := new(jsonschema.Reflector)
	//r.ExpandedStruct = true

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

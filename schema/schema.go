package schema

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/cespare/xxhash/v2"
	"github.com/invopop/jsonschema"
	"github.com/pkg/errors"
	"github.com/tmc/langchaingo/llms"
)

// Faker is a interface for generating structures
// with fake data. It is used for generating test data.
type Faker interface {
	Fake() any
}

var (
	reflectorPool = sync.Pool{
		New: func() any {
			return new(jsonschema.Reflector)
		},
	}

	cache   = make(map[reflect.Type]*Schema)
	cacheMu sync.RWMutex
)

type Schema struct {
	*jsonschema.Schema
	String string

	Functions []llms.FunctionDefinition
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

func buildSchema(t reflect.Type) (*Schema, error) {
	schema := JSONSchema(t)

	str, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal schema for %s", t.Name())
	}

	funcs := ToFunctionSchema(t, schema)

	s := &Schema{
		Schema: schema,
		String: string(str),

		Functions: funcs,
	}

	return s, nil
}

func ToFunctionSchema(tType reflect.Type, tSchema *jsonschema.Schema) []llms.FunctionDefinition {
	fds := []llms.FunctionDefinition{}

	for name, def := range tSchema.Definitions {
		parameters := &jsonschema.Schema{
			Type:       "object",
			Properties: def.Properties,
			Required:   def.Required,
		}

		fd := llms.FunctionDefinition{
			Name:        name,
			Description: def.Description,
			Parameters:  parameters,
		}

		fds = append(fds, fd)
	}

	return fds
}

func (s *Schema) NameFromRef() string {
	return strings.Split(s.Ref, "/")[2] // ex: '#/$defs/MyStruct'
}

// JSONSchema return the json schema of the configuration
func JSONSchema(t reflect.Type) *jsonschema.Schema {
	r := reflectorPool.Get().(*jsonschema.Reflector)
	defer reflectorPool.Put(r)

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
			name = vt.PkgPath() + "/" + vt.Name()
			name = strconv.FormatUint(xxhash.Sum64String(name), 10)
		}
		return name
	}

	return r.ReflectFromType(t)
}

package schema

import (
	"reflect"
	"slices"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/invopop/jsonschema"
)

// ErrRecursiveType is returned for a type that contains itself, such as
// type Node struct { Children []Node }. A schema without references cannot
// describe such a type, and references ($ref) are not used because several
// providers do not resolve them. Test for it with errors.Is.
var ErrRecursiveType = errors.New("recursive type is not supported")

// Struct tags and tag values that decide which fields the reflector
// describes.
const (
	tagJSON         = "json"
	tagJSONSchema   = "jsonschema"
	tagSeparator    = ","
	tagIgnore       = "-"
	tagOptionInline = "inline"
)

// cycleSeparator joins the types of a cycle in an ErrRecursiveType error.
const cycleSeparator = " -> "

// Methods through which a type changes how invopop/jsonschema reflects it.
// The interfaces repeat the reflector's own, so that CheckRecursion follows
// the same types the reflector does.
type (
	// schemaAliaser is reflected as the type of the value it returns.
	schemaAliaser interface {
		JSONSchemaAlias() any
	}
	// schemaPropertyAliaser has a property reflected as the type of the
	// value it returns for the property's name, when that value is not nil.
	schemaPropertyAliaser interface {
		JSONSchemaProperty(prop string) any
	}
	// schemaProvider supplies its own schema, so its fields are not
	// reflected.
	schemaProvider interface {
		JSONSchema() *jsonschema.Schema
	}
	// protoEnum is a protobuf enum, reflected as a string or an integer.
	protoEnum interface {
		EnumDescriptor() ([]byte, []int)
	}
)

var (
	schemaAliaserType         = reflect.TypeFor[schemaAliaser]()
	schemaPropertyAliaserType = reflect.TypeFor[schemaPropertyAliaser]()
	schemaProviderType        = reflect.TypeFor[schemaProvider]()
	protoEnumType             = reflect.TypeFor[protoEnum]()
)

// CheckRecursion returns an error wrapping ErrRecursiveType when t contains
// itself, directly or through other types. Call it before reflecting t with
// an invopop/jsonschema Reflector that has DoNotReference set: such a
// reflector inlines every type and, on a recursive one, overflows the stack,
// a fatal error that recover cannot catch. New and JSONSchema call it for
// you. The error names the type and the path back to it.
//
// It follows t as that reflector does: through pointers, slice and array
// elements, map values (not keys), and the struct fields it describes, with
// embedded structs and fields tagged json:",inline" merged into the parent.
// It skips unexported fields and fields tagged json:"-" or jsonschema:"-".
// It stops at interfaces, at types whose value receiver has a JSONSchema
// method, and at protobuf enums, and it follows the types returned by
// JSONSchemaAlias and JSONSchemaProperty. A type that only implements
// json.Marshaler or encoding.TextMarshaler is still reflected field by field,
// so it is still checked.
//
// A type used more than once without containing itself, such as
// struct{ A, B Inner }, is not recursive.
func CheckRecursion(t reflect.Type) error {
	if t == nil {
		return nil
	}
	c := &recursionChecker{
		states: make(map[visit]visitState),
	}
	return c.walk(t)
}

// visitKind is how the reflector expands a type.
type visitKind int

const (
	// visitAlias follows the type returned by JSONSchemaAlias.
	visitAlias visitKind = iota
	// visitFields describes the fields of a struct.
	visitFields
	// visitElem describes the element type of a slice or an array, or the
	// value type of a map.
	visitElem
)

// visit is one expansion of a type by the reflector. The reflector expands
// a visit the same way every time, so a visit repeated within itself never
// ends.
type visit struct {
	t    reflect.Type
	kind visitKind
}

// visitState is the progress of the walk of a visit. The zero value is a
// visit not walked yet.
type visitState int

const (
	// stateOnPath is a visit being walked.
	stateOnPath visitState = iota + 1
	// stateDone is a visit walked without a cycle, which need not be walked
	// again.
	stateDone
)

// recursionChecker walks a type the way the reflector expands it and
// reports a visit repeated within itself.
type recursionChecker struct {
	states map[visit]visitState
	// path holds the visits being walked, outermost first.
	path []visit
}

// within walks v with expand, unless v was already walked. It reports a
// cycle when v is being walked.
func (c *recursionChecker) within(v visit, expand func() error) error {
	switch c.states[v] {
	case stateDone:
		return nil
	case stateOnPath:
		return c.cycleError(v)
	}
	c.states[v] = stateOnPath
	c.path = append(c.path, v)
	if err := expand(); err != nil {
		return err
	}
	c.path = c.path[:len(c.path)-1]
	c.states[v] = stateDone
	return nil
}

// cycleError returns ErrRecursiveType wrapped with the type of v and the
// types walked from v back to it.
func (c *recursionChecker) cycleError(v visit) error {
	cycle := c.path[slices.Index(c.path, v):]
	names := make([]string, 0, len(cycle)+1)
	for _, p := range cycle {
		names = append(names, p.t.String())
	}
	names = append(names, v.t.String())
	return errors.Wrapf(ErrRecursiveType, "type %s refers to itself through %s",
		v.t, strings.Join(names, cycleSeparator))
}

// walk follows t as the reflector does for a value of type t.
func (c *recursionChecker) walk(t reflect.Type) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Interface {
		return nil
	}
	if t.Implements(schemaAliaserType) {
		return c.within(visit{t: t, kind: visitAlias}, func() error {
			return c.walkAlias(t)
		})
	}
	if t.Implements(schemaProviderType) || t.Implements(protoEnumType) {
		return nil
	}
	switch t.Kind() {
	case reflect.Struct:
		return c.walkFields(t)
	case reflect.Slice, reflect.Array, reflect.Map:
		return c.within(visit{t: t, kind: visitElem}, func() error {
			return c.walk(t.Elem())
		})
	}
	return nil
}

// walkAlias follows the type that t, a schemaAliaser, is reflected as. A
// pointer to t has the methods of t, so the assertion holds.
func (c *recursionChecker) walkAlias(t reflect.Type) error {
	aliaser := reflect.New(t).Interface().(schemaAliaser)
	alias := reflect.TypeOf(aliaser.JSONSchemaAlias())
	if alias == nil {
		return nil
	}
	return c.walk(alias)
}

// walkFields follows the fields of the struct t, or of the struct t points
// to. The hooks of t itself are not consulted, as the reflector also merges
// the fields of an embedded struct that supplies its own schema.
func (c *recursionChecker) walkFields(t reflect.Type) error {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	return c.within(visit{t: t, kind: visitFields}, func() error {
		var propertyAliaser schemaPropertyAliaser
		if t.Implements(schemaPropertyAliaserType) {
			propertyAliaser = reflect.New(t).Interface().(schemaPropertyAliaser)
		}
		for f := range t.Fields() {
			if err := c.walkField(f, propertyAliaser); err != nil {
				return err
			}
		}
		return nil
	})
}

// walkField follows the type of the struct field f. propertyAliaser is the
// struct's JSONSchemaProperty hook, or nil.
func (c *recursionChecker) walkField(f reflect.StructField, propertyAliaser schemaPropertyAliaser) error {
	jsonTags := strings.Split(f.Tag.Get(tagJSON), tagSeparator)
	schemaTags := strings.Split(f.Tag.Get(tagJSONSchema), tagSeparator)
	if jsonTags[0] == tagIgnore || schemaTags[0] == tagIgnore {
		return nil
	}
	if isInlined(f, jsonTags) {
		return c.walkFields(f.Type)
	}
	if !f.Anonymous && !f.IsExported() {
		return nil
	}
	if propertyAliaser != nil {
		name := f.Name
		if jsonTags[0] != "" {
			name = jsonTags[0]
		}
		if alias := propertyAliaser.JSONSchemaProperty(name); alias != nil {
			return c.walk(reflect.TypeOf(alias))
		}
	}
	return c.walk(f.Type)
}

// isInlined reports whether the reflector merges the fields of f into its
// parent: f embeds a struct, or a pointer to one, without a name in its json
// tag, or its json tag has the inline option. jsonTags is the json tag of f
// split on commas.
func isInlined(f reflect.StructField, jsonTags []string) bool {
	if f.Anonymous && jsonTags[0] == "" {
		t := f.Type
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() == reflect.Struct {
			return true
		}
	}
	return slices.Contains(jsonTags[1:], tagOptionInline)
}

package schema_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Types that contain themselves. Reflecting any of them without references
// overflowed the stack (F-058).

type sliceNode struct {
	Name     string      `json:"name"`
	Children []sliceNode `json:"children"`
}

type listNode struct {
	Value int       `json:"value"`
	Next  *listNode `json:"next,omitempty"`
}

type mapNode struct {
	Children map[string]mapNode `json:"children"`
}

type mutualA struct {
	B *mutualB `json:"b"`
}

type mutualB struct {
	As []mutualA `json:"as"`
}

// embeddedBase is embedded by embeddedDerived, so its fields are merged into
// embeddedDerived, which embeddedBase refers to.
type embeddedBase struct {
	Extra *embeddedDerived `json:"extra"`
}

type embeddedDerived struct {
	embeddedBase
	Name string `json:"name"`
}

// selfEmbedded embeds a pointer to itself.
type selfEmbedded struct {
	*selfEmbedded
	Name string `json:"name"`
}

type tree []tree

// inlineSelf merges the fields of the struct its field points to, which is
// itself.
type inlineSelf struct {
	Name string      `json:"name"`
	Next *inlineSelf `json:"next,inline"`
}

// selfAlias is reflected as itself.
type selfAlias struct{}

func (selfAlias) JSONSchemaAlias() any { return selfAlias{} }

// marshalerNode implements json.Marshaler, which the reflector ignores.
type marshalerNode struct {
	Children []marshalerNode `json:"children"`
}

func (marshalerNode) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }

// pointerSchemaNode supplies its own schema through a pointer receiver, which
// the reflector does not consult for a value.
type pointerSchemaNode struct {
	Children []pointerSchemaNode `json:"children"`
}

func (*pointerSchemaNode) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
	}
}

// Types that reuse a type, or would be recursive without the reflector's
// rules, and are reflected.

type leaf struct {
	Value string `json:"value"`
}

type siblings struct {
	A     leaf            `json:"a"`
	B     leaf            `json:"b"`
	List  []leaf          `json:"list"`
	ByKey map[string]leaf `json:"by_key"`
	Ptr   *leaf           `json:"ptr"`
	More  []*leaf         `json:"more"`
}

// valueSchemaNode supplies its own schema, so its fields are not reflected.
type valueSchemaNode struct {
	Children []valueSchemaNode `json:"children"`
}

func (valueSchemaNode) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
	}
}

type holdsValueSchemaNode struct {
	Node  valueSchemaNode   `json:"node"`
	Nodes []valueSchemaNode `json:"nodes"`
}

// skippedSelf refers to itself only through fields the reflector skips.
type skippedSelf struct {
	Name   string       `json:"name"`
	Parent *skippedSelf `json:"-"`
	next   *skippedSelf //nolint:unused // proves unexported fields are skipped
}

// skippedBySchemaTag returns a struct whose only recursive field is tagged
// jsonschema:"-", which invopop skips. The type is built at run time because
// the repository's jsonschema linter allows only key=value tags.
func skippedBySchemaTag() reflect.Type {
	return reflect.StructOf([]reflect.StructField{
		{
			Name: "Name",
			Type: reflect.TypeFor[string](),
			Tag:  `json:"name"`,
		},
		{
			Name: "Tree",
			Type: reflect.TypeFor[*sliceNode](),
			Tag:  `json:"tree" jsonschema:"-"`,
		},
	})
}

// propertyAliasNode has its children reflected as strings.
type propertyAliasNode struct {
	Children []propertyAliasNode `json:"children"`
}

func (propertyAliasNode) JSONSchemaProperty(prop string) any {
	if prop == "children" {
		return []string{}
	}
	return nil
}

// recursiveKey is recursive, but only used as a map key, which the reflector
// does not describe.
type recursiveKey struct {
	Parent *recursiveKey
}

type keyedByRecursive struct {
	ByKey map[recursiveKey]string `json:"by_key"`
}

// protoNode is a protobuf enum, reflected as a string or an integer.
type protoNode struct {
	Children []protoNode
}

func (protoNode) EnumDescriptor() ([]byte, []int) { return nil, nil }

type holdsProtoNode struct {
	Node protoNode `json:"node"`
}

// inlineNonStruct has a field tagged inline that is not a struct, so it adds
// no properties.
type inlineNonStruct struct {
	Name string   `json:"name"`
	Tags []string `json:"tags,inline"`
}

// nilAlias is reflected as the type of nil, so there is nothing to follow.
type nilAlias struct {
	Self *nilAlias
}

func (nilAlias) JSONSchemaAlias() any { return nil }

type leaves struct {
	At       time.Time       `json:"at"`
	Raw      json.RawMessage `json:"raw"`
	Bytes    []byte          `json:"bytes"`
	Anything []any           `json:"anything"`
}

func TestCheckRecursion_RecursiveTypes(t *testing.T) {
	t.Parallel()

	tcs := []struct {
		name    string
		typ     reflect.Type
		wantErr string
	}{
		{
			name:    "slice",
			typ:     reflect.TypeFor[sliceNode](),
			wantErr: "type schema_test.sliceNode refers to itself through schema_test.sliceNode -> []schema_test.sliceNode -> schema_test.sliceNode: recursive type is not supported",
		},
		{
			name:    "pointer",
			typ:     reflect.TypeFor[listNode](),
			wantErr: "type schema_test.listNode refers to itself through schema_test.listNode -> schema_test.listNode: recursive type is not supported",
		},
		{
			name:    "pointer to root",
			typ:     reflect.TypeFor[*listNode](),
			wantErr: "type schema_test.listNode refers to itself through schema_test.listNode -> schema_test.listNode: recursive type is not supported",
		},
		{
			name:    "map value",
			typ:     reflect.TypeFor[mapNode](),
			wantErr: "type schema_test.mapNode refers to itself through schema_test.mapNode -> map[string]schema_test.mapNode -> schema_test.mapNode: recursive type is not supported",
		},
		{
			name:    "mutual",
			typ:     reflect.TypeFor[mutualA](),
			wantErr: "type schema_test.mutualA refers to itself through schema_test.mutualA -> schema_test.mutualB -> []schema_test.mutualA -> schema_test.mutualA: recursive type is not supported",
		},
		{
			name:    "embedded",
			typ:     reflect.TypeFor[embeddedDerived](),
			wantErr: "type schema_test.embeddedDerived refers to itself through schema_test.embeddedDerived -> schema_test.embeddedBase -> schema_test.embeddedDerived: recursive type is not supported",
		},
		{
			name:    "self embedded",
			typ:     reflect.TypeFor[selfEmbedded](),
			wantErr: "type schema_test.selfEmbedded refers to itself through schema_test.selfEmbedded -> schema_test.selfEmbedded: recursive type is not supported",
		},
		{
			name:    "unnamed root",
			typ:     reflect.TypeFor[[]sliceNode](),
			wantErr: "type []schema_test.sliceNode refers to itself through []schema_test.sliceNode -> schema_test.sliceNode -> []schema_test.sliceNode: recursive type is not supported",
		},
		{
			name:    "named slice",
			typ:     reflect.TypeFor[tree](),
			wantErr: "type schema_test.tree refers to itself through schema_test.tree -> schema_test.tree: recursive type is not supported",
		},
		{
			name:    "inline tag",
			typ:     reflect.TypeFor[inlineSelf](),
			wantErr: "type schema_test.inlineSelf refers to itself through schema_test.inlineSelf -> schema_test.inlineSelf: recursive type is not supported",
		},
		{
			name:    "alias to itself",
			typ:     reflect.TypeFor[selfAlias](),
			wantErr: "type schema_test.selfAlias refers to itself through schema_test.selfAlias -> schema_test.selfAlias: recursive type is not supported",
		},
		{
			name:    "json.Marshaler",
			typ:     reflect.TypeFor[marshalerNode](),
			wantErr: "type schema_test.marshalerNode refers to itself through schema_test.marshalerNode -> []schema_test.marshalerNode -> schema_test.marshalerNode: recursive type is not supported",
		},
		{
			name:    "JSONSchema on a pointer receiver",
			typ:     reflect.TypeFor[pointerSchemaNode](),
			wantErr: "type schema_test.pointerSchemaNode refers to itself through schema_test.pointerSchemaNode -> []schema_test.pointerSchemaNode -> schema_test.pointerSchemaNode: recursive type is not supported",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := schema.CheckRecursion(tc.typ)
			require.ErrorIs(t, err, schema.ErrRecursiveType)
			assert.EqualError(t, err, tc.wantErr)

			// A recursive type is rejected before JSONSchema sets
			// jsonschema.Version, so this call does not race.
			js, err := schema.JSONSchema(tc.typ)
			require.ErrorIs(t, err, schema.ErrRecursiveType)
			assert.EqualError(t, err, tc.wantErr)
			assert.Nil(t, js)

			// The failure is not cached: every call reports it.
			for range 2 {
				sc, err := schema.New(tc.typ)
				require.ErrorIs(t, err, schema.ErrRecursiveType)
				assert.EqualError(t, err, tc.wantErr)
				assert.Nil(t, sc)
			}
		})
	}
}

func TestCheckRecursion_NonRecursiveTypes(t *testing.T) {
	t.Parallel()

	// Each type is reflected by New, so a type CheckRecursion wrongly
	// accepts would crash this test.
	tcs := []struct {
		name      string
		typ       reflect.Type
		wantProps []string
	}{
		{
			name:      "repeated siblings",
			typ:       reflect.TypeFor[siblings](),
			wantProps: []string{"a", "b", "list", "by_key", "ptr", "more"},
		},
		{
			name:      "JSONSchema on a value receiver",
			typ:       reflect.TypeFor[holdsValueSchemaNode](),
			wantProps: []string{"node", "nodes"},
		},
		{
			name:      "self through skipped fields",
			typ:       reflect.TypeFor[skippedSelf](),
			wantProps: []string{"name"},
		},
		{
			name:      "recursive field skipped by the jsonschema tag",
			typ:       skippedBySchemaTag(),
			wantProps: []string{"name"},
		},
		{
			name:      "property alias",
			typ:       reflect.TypeFor[propertyAliasNode](),
			wantProps: []string{"children"},
		},
		{
			name:      "recursive map key",
			typ:       reflect.TypeFor[keyedByRecursive](),
			wantProps: []string{"by_key"},
		},
		{
			name:      "protobuf enum",
			typ:       reflect.TypeFor[holdsProtoNode](),
			wantProps: []string{"node"},
		},
		{
			name:      "inline tag on a non-struct",
			typ:       reflect.TypeFor[inlineNonStruct](),
			wantProps: []string{"name"},
		},
		{
			name:      "leaf types",
			typ:       reflect.TypeFor[leaves](),
			wantProps: []string{"at", "raw", "bytes", "anything"},
		},
		{
			name:      "nested distinct types",
			typ:       reflect.TypeFor[Search](),
			wantProps: []string{"topic", "query", "type", "args", "prov"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.NoError(t, schema.CheckRecursion(tc.typ))

			sc, err := schema.New(tc.typ)
			require.NoError(t, err)
			props := make([]string, 0, sc.Parameters.Properties.Len())
			for pair := sc.Parameters.Properties.Oldest(); pair != nil; pair = pair.Next() {
				props = append(props, pair.Key)
			}
			assert.Equal(t, tc.wantProps, props)
		})
	}

	// New rather than JSONSchema: JSONSchema sets the package-level
	// jsonschema.Version, so concurrent calls race; New serializes them.
	t.Run("value schema hook is used", func(t *testing.T) {
		t.Parallel()

		sc, err := schema.New(reflect.TypeFor[valueSchemaNode]())
		require.NoError(t, err)
		assert.Equal(t, "object", sc.RawSchema.Type)
		assert.Nil(t, sc.RawSchema.Properties)
	})

	t.Run("property alias is used", func(t *testing.T) {
		t.Parallel()

		sc, err := schema.New(reflect.TypeFor[propertyAliasNode]())
		require.NoError(t, err)
		children, ok := sc.RawSchema.Properties.Get("children")
		require.True(t, ok)
		assert.Equal(t, "array", children.Type)
		assert.Equal(t, "string", children.Items.Type)
	})

	t.Run("nil type", func(t *testing.T) {
		t.Parallel()

		assert.NoError(t, schema.CheckRecursion(nil))
	})

	// Not reflected: the reflector itself panics on a nil alias.
	t.Run("nil alias", func(t *testing.T) {
		t.Parallel()

		assert.NoError(t, schema.CheckRecursion(reflect.TypeFor[nilAlias]()))
	})
}

package yaml

import (
	"bytes"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/effective-security/gogentic/llmutils"
	"github.com/effective-security/gogentic/schema"
	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
)

type CommentStyle int

const (
	NoComment CommentStyle = iota
	HeadComment
	LineComment
	FootComment
)

type Encoder struct {
	reqType      reflect.Type
	commentStyle CommentStyle
}

func NewEncoder(req any) *Encoder {
	t := reflect.TypeOf(req)
	return &Encoder{
		reqType:      t,
		commentStyle: NoComment,
	}
}

func (e *Encoder) Marshal(v any) ([]byte, error) {
	if e.commentStyle == NoComment {
		return yaml.Marshal(v)
	}
	node, err := e.structToYAMLWithComments(v)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(node)
}

func (e *Encoder) Unmarshal(bs []byte, ret any) error {
	data := llmutils.BytesTrimBackticks(bs)
	return yaml.Unmarshal(data, ret)
}

func (e *Encoder) Validate(req any) error {
	validate := validator.New()
	return validate.Struct(req)
}

func (e *Encoder) WithCommentStyle(style CommentStyle) *Encoder {
	e.commentStyle = style
	return e
}

func (e *Encoder) GetFormatInstructions() string {
	tValue := reflect.New(e.reqType)
	instance := tValue.Interface()
	if f, ok := tValue.Elem().Interface().(schema.Faker); ok {
		instance = f.Fake()
	} else {
		_ = gofakeit.Struct(instance)
	}
	bs, err := e.Marshal(instance)
	if err != nil {
		return ""
	}
	var b bytes.Buffer
	b.WriteString("\nRespond with YAML in the following YAML schema without comments:\n")
	b.WriteString("```yaml\n")
	b.Write(bs)
	b.WriteString("```")
	b.WriteString("\nMake sure to return an instance of the YAML, not the schema itself.\n")
	return b.String()
}

// Parse struct and convert it to a YAML Node with comments
func (e *Encoder) structToYAMLWithComments(v any) (*yaml.Node, error) {
	val := reflect.ValueOf(v)

	val = dereference(val)
	if !val.IsValid() {
		return &yaml.Node{Kind: yaml.ScalarNode, Value: "null", Tag: "!!null"}, nil
	}

	typ := val.Type()

	// Ensure it is a struct
	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct, got %s", val.Kind())
	}

	// Create the root YAML node
	root := &yaml.Node{Kind: yaml.MappingNode}

	// Iterate over the struct fields
	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)

		// Get the YAML key
		yamlKey := field.Tag.Get("yaml")
		if yamlKey == "" || yamlKey == "-" {
			continue // Skip unexported fields
		}

		// Get the comment
		comment := field.Tag.Get("comment")
		if comment == "" {
			comment = extractDescription(field.Tag.Get("jsonschema"))
		}

		// Add the key
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: yamlKey}
		if comment != "" {
			switch e.commentStyle {
			case HeadComment:
				keyNode.HeadComment = comment
			case LineComment:
				keyNode.LineComment = comment
			case FootComment:
				keyNode.FootComment = comment
			}
		}

		// Add the value
		valueNode := e.getValueNode(val.Field(i))

		// Assemble the YAML structure
		root.Content = append(root.Content, keyNode, valueNode)
	}

	return root, nil
}

// Recursively parse values, supporting pointers and interfaces
func (e *Encoder) getValueNode(v reflect.Value) *yaml.Node {
	// Handle pointers
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return &yaml.Node{Kind: yaml.ScalarNode, Value: "null", Tag: "!!null"}
		}
		v = v.Elem()
	}

	// Handle interfaces
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return &yaml.Node{Kind: yaml.ScalarNode, Value: "null", Tag: "!!null"}
		}
		v = reflect.ValueOf(v.Interface()) // Get the actual value
	}

	var node *yaml.Node
	// Handle basic types
	switch v.Kind() {
	case reflect.String:
		node = &yaml.Node{Kind: yaml.ScalarNode, Value: v.String()}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		node = &yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%d", v.Int()), Tag: "!!int"}
	case reflect.Float32, reflect.Float64:
		node = &yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%f", v.Float()), Tag: "!!float"}
	case reflect.Bool:
		node = &yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%t", v.Bool()), Tag: "!!bool"}
	case reflect.Map:
		node = e.mapToYAMLNode(v)
	case reflect.Struct:
		node, _ = e.structToYAMLWithComments(v.Interface()) // Recursively parse struct
	case reflect.Slice, reflect.Array:
		node = e.sliceToYAMLNode(v)
	default:
		node = &yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%v", v.Interface())}
	}
	return node
}

// Handle map types
func (e *Encoder) mapToYAMLNode(v reflect.Value) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode}
	for _, key := range v.MapKeys() {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%v", key.Interface())}
		valueNode := e.getValueNode(v.MapIndex(key))
		node.Content = append(node.Content, keyNode, valueNode)
	}
	return node
}

// Handle slice/array types
func (e *Encoder) sliceToYAMLNode(v reflect.Value) *yaml.Node {
	node := &yaml.Node{Kind: yaml.SequenceNode}
	for i := 0; i < v.Len(); i++ {
		node.Content = append(node.Content, e.getValueNode(v.Index(i)))
	}
	return node
}

// Parse description from jsonschema
func extractDescription(tag string) string {
	re := regexp.MustCompile(`description=([^,]+)`)
	matches := re.FindStringSubmatch(tag)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// Recursively dereference pointers until `v` is not a pointer type
func dereference(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return reflect.Value{} // Return an empty value to prevent nil pointer dereference
		}
		v = v.Elem()
	}
	return v
}

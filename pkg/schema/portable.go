package schema

import (
	"bytes"
	"encoding/json"
	"math/big"
	"reflect"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
)

const (
	portableSchemaDepth    = 16
	portableNumberBytes    = 128
	portableNumberExponent = 308
)

// ValidatePortableSchema validates the bounded JSON Schema subset supported by the
// text router. It rejects unknown keywords instead of weakening caller constraints.
// Supported keywords: type, title, description, enum, properties, required,
// additionalProperties (boolean), and items. Strict objects require every property
// and disallow additional properties. The root must be an object.
func ValidatePortableSchema(raw json.RawMessage, strict bool) error {
	var node map[string]any
	if err := decodePortable(raw, &node); err != nil {
		return errors.WithMessage(err, "decode JSON schema")
	}
	if node == nil || node["type"] != "object" {
		return errors.New("schema root must be an object")
	}
	return checkPortableSchema(node, strict, 0)
}

func checkPortableSchema(n map[string]any, strict bool, depth int) error {
	if depth > portableSchemaDepth {
		return errors.New("schema nesting exceeds 16")
	}
	for k := range n {
		switch k {
		case "type", "title", "description", "enum", "properties", "required", "additionalProperties", "items":
		default:
			return errors.Errorf("unsupported schema keyword: %s", k)
		}
	}
	for _, k := range []string{"title", "description"} {
		if v, ok := n[k]; ok {
			if _, ok := v.(string); !ok {
				return errors.Errorf("%s must be a string", k)
			}
		}
	}
	types := []string{}
	switch t := n["type"].(type) {
	case string:
		types = append(types, t)
	case []any:
		for _, v := range t {
			s, ok := v.(string)
			if !ok {
				return errors.New("invalid schema type")
			}
			types = append(types, s)
		}
	default:
		return errors.New("schema requires a type")
	}
	if len(types) == 0 {
		return errors.New("schema requires a type")
	}
	hasObject, hasArray := false, false
	seen := map[string]bool{}
	for _, t := range types {
		if seen[t] {
			return errors.New("duplicate schema type")
		}
		seen[t] = true
		switch t {
		case "object":
			hasObject = true
		case "array":
			hasArray = true
		case "string", "number", "integer", "boolean", "null":
		default:
			return errors.New("invalid schema type")
		}
	}
	if e, ok := n["enum"]; ok {
		a, ok := e.([]any)
		if !ok || len(a) == 0 {
			return errors.New("enum must be nonempty")
		}
		for _, v := range a {
			matched := false
			for _, t := range types {
				matched = matched || portableType(t, v)
			}
			if !matched {
				return errors.New("enum does not match type")
			}
		}
	}
	if hasObject {
		props := map[string]any{}
		if v, ok := n["properties"]; ok {
			var valid bool
			props, valid = v.(map[string]any)
			if !valid {
				return errors.New("properties must be an object")
			}
		}
		required := map[string]bool{}
		if v, ok := n["required"]; ok {
			a, ok := v.([]any)
			if !ok {
				return errors.New("required must be an array")
			}
			for _, v := range a {
				key, ok := v.(string)
				if !ok || required[key] {
					return errors.New("invalid required property")
				}
				if _, ok := props[key]; !ok {
					return errors.New("required property missing from properties")
				}
				required[key] = true
			}
		}
		if v, ok := n["additionalProperties"]; ok {
			if _, ok := v.(bool); !ok {
				return errors.New("additionalProperties must be boolean")
			}
		}
		if strict && (n["additionalProperties"] != false || len(required) != len(props)) {
			return errors.New("strict objects require all properties and additionalProperties:false")
		}
		for _, v := range props {
			child, ok := v.(map[string]any)
			if !ok {
				return errors.New("invalid property schema")
			}
			if err := checkPortableSchema(child, strict, depth+1); err != nil {
				return err
			}
		}
	} else {
		for _, k := range []string{"properties", "required", "additionalProperties"} {
			if _, ok := n[k]; ok {
				return errors.New("object keywords require object type")
			}
		}
	}
	if hasArray {
		child, ok := n["items"].(map[string]any)
		if !ok {
			return errors.New("arrays require an items schema")
		}
		if err := checkPortableSchema(child, strict, depth+1); err != nil {
			return err
		}
	} else if _, ok := n["items"]; ok {
		return errors.New("items requires array type")
	}
	return nil
}

// ValidatePortableJSON checks completed JSON against a previously validated schema.
// It never repairs output or accepts partial JSON. Call ValidatePortableSchema first.
func ValidatePortableJSON(raw json.RawMessage, output string) error {
	if err := ValidatePortableSchema(raw, false); err != nil {
		return err
	}
	var n map[string]any
	if err := decodePortable(raw, &n); err != nil {
		return errors.WithMessage(err, "decode output schema")
	}
	var value any
	if err := decodePortable([]byte(output), &value); err != nil {
		return errors.WithMessage(err, "decode JSON output")
	}
	return checkPortableValue(n, value, 0)
}

func portableType(t string, v any) bool {
	switch t {
	case "null":
		return v == nil
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "number":
		n, ok := v.(json.Number)
		return ok && boundedNumber(n)
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		if !boundedNumber(n) {
			return false
		}
		rat, ok := new(big.Rat).SetString(n.String())
		return ok && rat.IsInt()
	}
	return false
}

func checkPortableValue(n map[string]any, v any, depth int) error {
	if depth > portableSchemaDepth {
		return errors.New("output nesting exceeds schema limit")
	}
	matches := false
	switch t := n["type"].(type) {
	case string:
		matches = portableType(t, v)
	case []any:
		for _, x := range t {
			if s, ok := x.(string); ok {
				matches = matches || portableType(s, v)
			}
		}
	}
	if !matches {
		return errors.New("output type does not match schema")
	}
	if a, ok := n["enum"].([]any); ok {
		found := false
		for _, x := range a {
			found = found || portableEqual(x, v)
		}
		if !found {
			return errors.New("output is outside enum")
		}
	}
	switch val := v.(type) {
	case map[string]any:
		if a, ok := n["required"].([]any); ok {
			for _, x := range a {
				if _, ok := val[x.(string)]; !ok {
					return errors.New("required output property missing")
				}
			}
		}
		props, _ := n["properties"].(map[string]any)
		for key, x := range val {
			if child, ok := props[key].(map[string]any); ok {
				if err := checkPortableValue(child, x, depth+1); err != nil {
					return err
				}
			} else if n["additionalProperties"] == false {
				return errors.New("unexpected output property")
			}
		}
	case []any:
		child, ok := n["items"].(map[string]any)
		if !ok {
			return errors.New("missing items schema")
		}
		for _, x := range val {
			if err := checkPortableValue(child, x, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func decodePortable(raw []byte, v any) error {
	if !json.Valid(raw) {
		return errors.New("invalid JSON")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(v); err != nil {
		return errors.WithMessage(err, "decode portable JSON")
	}
	return nil
}
func portableEqual(a, b any) bool {
	if x, ok := a.(json.Number); ok {
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		if !boundedNumber(x) || !boundedNumber(y) {
			return false
		}
		xr, xok := new(big.Rat).SetString(x.String())
		yr, yok := new(big.Rat).SetString(y.String())
		return xok && yok && xr.Cmp(yr) == 0
	}
	switch x := a.(type) {
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !portableEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			z, ok := y[k]
			if !ok || !portableEqual(v, z) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}

// Bound work before arbitrary-precision arithmetic on caller-controlled exponents.
func boundedNumber(n json.Number) bool {
	s := n.String()
	if len(s) > portableNumberBytes {
		return false
	}
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		exponent, err := strconv.Atoi(s[i+1:])
		return err == nil && exponent >= -portableNumberExponent && exponent <= portableNumberExponent
	}
	return true
}

// Package schema provides helpers to generate JSON Schemas from Go types and
// shape those schemas for LLM prompting. It is used throughout the project to
// derive structured parameter definitions and response formats from Go structs.
//
// Core ideas
//   - Reflect Go structs and produce a jsonschema (draft-07) model.
//   - Convert reflected schema to a compact “function parameter” schema where
//     top-level properties are flattened and $ref entries are resolved.
//   - Build OpenAI-style JSON Schema response formats to steer model outputs.
//
// # Common usage
//
// Generate a schema from a struct and use it as function parameters:
//
//	type Search struct {
//		Query string `json:"query" jsonschema:"description=Query to search"`
//		Type  string `json:"type"  jsonschema:"description=Type of search,enum=web,enum=image,default=web"`
//	}
//
//	s, err := New(reflect.TypeOf(Search{}))
//	if err != nil { /* handle */ }
//	_ = s.Parameters // jsonschema.Schema describing parameters
//
// Build a response format (OpenAI style) from a type:
//
//	rf, err := NewResponseFormat(reflect.TypeOf(Search{}), true)
//	if err != nil { /* handle */ }
//	// rf.Type == "json_schema" and rf.JSONSchema contains the strict schema
//
// You can also construct a schema from an arbitrary map using FromAny/MustFromAny
// when you want explicit control over the resulting structure.
package schema

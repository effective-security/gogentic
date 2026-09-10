// Package openai translates the OpenAI Chat Completions and Responses request
// and response shapes to and from the portable router contract. It is a pure
// codec: DecodeChat/DecodeResponses turn request bytes into router.Request,
// EncodeChat/EncodeResponses turn a router.Result into response bytes, and
// EncodeError produces the OpenAI error envelope with a status code. Nothing
// here serves HTTP; see ExampleDecodeChat for the host handler pattern.
//
// Decoding is lenient by default: unknown fields are ignored and null means
// omitted. dialect.DecodeOptions.Strict rejects unknown fields, duplicate keys
// and nulls. In both modes, known fields whose values the router cannot honor
// (streaming, stored state, built-in tools, media parts, ...) are rejected with
// an error naming the field.
package openai

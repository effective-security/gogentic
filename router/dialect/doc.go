// Package dialect holds what the public API codecs share: decode and encode
// options, lenient/strict JSON object parsing with dialect-aware null handling,
// the sealed envelope for opaque reasoning state, and the mapping from router
// error kinds to HTTP status codes.
//
// Concrete codecs live in subpackages: openai (Chat Completions and Responses)
// and anthropic (Messages). Each exposes Decode*/Encode*/EncodeError functions
// over bytes; none of them serves HTTP. A host handler reads a bounded body,
// decodes, calls router.Generate, and encodes the result or the error.
package dialect

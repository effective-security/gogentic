// Package anthropic is the Anthropic Messages codec for the router. It converts
// a POST /v1/messages request body into a router.Request and a router.Result
// back into a Messages response, and renders router errors in the Anthropic
// error envelope. It serves no HTTP; a host handler reads a bounded body, calls
// DecodeMessages, router.Generate, then EncodeMessages or EncodeError.
//
// The codec is stateless and non-streaming. Thinking blocks carry the router's
// sealed opaque state in their signature (or data for redacted thinking), so a
// client that replays assistant content verbatim continues reasoning on the same
// target. Usage is always present because the dialect requires it; when the
// backend reported no usage every counter is zero.
//
// See Documentation/router.md for the field ledger.
package anthropic

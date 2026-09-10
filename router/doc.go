// Package router selects an authorized, compatible text model and performs one
// non-streaming inference. It supports application-executed function tools, a
// bounded JSON Schema subset, and reasoning passthrough. It does not execute
// tools, persist conversations, stream, or serve HTTP.
//
// Construct shared providers, adapt each with Connector, register them as Targets
// and call New. With no Selector, Generate resolves Request.Model exactly. A
// Selector may choose only one of the eligible candidates it is given. Hosts own
// authentication and supply trusted identity through context; Target.Allow and an
// optional Admission implementation enforce it on every request.
//
// Errors returned by Generate carry a transport-neutral Kind. The dialect codecs
// under router/dialect translate public API shapes (OpenAI Chat Completions,
// OpenAI Responses, Anthropic Messages) to and from Request and Result and map
// kinds to status codes.
//
// See ExampleNew and Documentation/router.md for complete construction examples.
package router

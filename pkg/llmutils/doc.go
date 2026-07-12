// Package llmutils provides utilities for formatting prompts, cleaning model
// outputs, rendering data as JSON/YAML/markdown, and inspecting chat messages.
//
// Handy helpers
//   - CleanJSON/TrimBackticks: extract JSON payloads from verbose LLM outputs.
//   - BackticksJSON/BackticksYAM: wrap data with code fences for prompts.
//   - PrintMessages: debug-print a sequence of chat messages.
//
// Examples
//
//	raw := "Here you go:\n```json\n{\"city\":\"Paris\"}\n```"
//	clean := llmutils.CleanJSON([]byte(raw)) // -> {"city":"Paris"}
//
//	js := llmutils.BackticksJSON(llmutils.ToJSONIndent(map[string]int{"a":1}))
//	_ = js // "```json\n{\n  \"a\": 1\n}\n```"
package llmutils

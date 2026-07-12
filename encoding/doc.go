// Package encoding provides a pluggable framework to describe, encode and
// decode structured data for agentic flows. It is primarily used to:
//
//   - Generate “format instructions” from a Go type that can be embedded in
//     prompts to guide LLMs to return well‑structured outputs.
//   - Marshal/Unmarshal LLM outputs to/from Go structs using JSON, YAML, TOML
//     or custom encoders.
//   - Optionally validate decoded outputs using struct validation tags.
//
// The package exposes SchemaEncoder implementations for popular formats and a
// generic TypedOutputParser[T] that leverages an encoder to parse LLM output
// directly into a target Go type.
//
// Example: Generate format instructions and parse JSON output
//
//	package encoding
//
//	// Define the expected shape of the model response.
//	type Weather struct {
//		City string `json:"city" jsonschema:"description=City name"`
//		TempC int    `json:"temp_c" jsonschema:"description=Temperature in Celsius"`
//	}
//
//	// Create a JSON‑Schema based parser and obtain instructions to include in a prompt.
//	parser, err := NewTypedOutputParser(Weather{}, ModeJSONSchema)
//	if err != nil {
//		// handle error
//	}
//
//	// Put this in your prompt so the LLM knows how to format its output.
//	instructions := parser.GetFormatInstructions()
//
//	// Later, parse the model output into the typed struct.
//	// For example, given a model output like:
//	//   {"city":"Paris","temp_c":22}
//	res, err := parser.Parse(`{"city":"Paris","temp_c":22}`)
//	if err != nil {
//		// handle parse/validation error
//	}
//	_ = res // use *Weather
//
// Example: Switch to YAML or TOML while keeping the same Go type
//
//	_ = func() error {
//		_, err := NewTypedOutputParser(Weather{}, ModeYAML) // or ModeTOML
//		return err
//	}
package encoding

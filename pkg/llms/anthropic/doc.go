// Package anthropic implements the Anthropic (Claude) llms.Model with options
// for API key, model selection, and custom HTTP client or AWS config when used
// via Bedrock (see NewBedrock in this package).
//
// Example
//
//	mdl, err := anthropic.New(
//	    anthropic.WithToken(os.Getenv("ANTHROPIC_API_KEY")),
//	    anthropic.WithModel("claude-3-5-sonnet-20240620"),
//	)
//	_ = mdl; _ = err
package anthropic

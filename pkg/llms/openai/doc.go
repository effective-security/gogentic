// Package openai provides an llms.Model implementation for OpenAI-compatible
// APIs, including OpenAI and Azure OpenAI endpoints. Use options to select the
// provider style, model, base URL, token, and API version where applicable.
//
// Example
//
//	mdl, err := openai.New(
//	    openai.WithProvider(openai.ProviderOpenAI),
//	    openai.WithToken(os.Getenv("OPENAI_API_KEY")),
//	    openai.WithModel("gpt-4o-mini"),
//	)
//	_ = mdl; _ = err
package openai

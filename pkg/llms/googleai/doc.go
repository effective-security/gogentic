// Package googleai implements Google AI (Vertex/Gemini) llms.Model wrappers.
// Configure with an API key and default model; advanced flows are provided via
// internal clients in the package's internal submodules.
//
// Example
//
//	mdl, err := googleai.New(context.Background(),
//	    googleai.WithAPIKey(os.Getenv("GOOGLE_API_KEY")),
//	    googleai.WithDefaultModel("gemini-1.5-flash"),
//	)
//	_ = mdl; _ = err
package googleai

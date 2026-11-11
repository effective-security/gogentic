// Package cloudflare implements an llms.Model for Cloudflare Workers AI and
// compatible APIs. Configure with an API token, account ID and model ID.
// Requests go to <server URL>/<account ID>/ai/run/<model>; the server URL
// defaults to DefaultServerURL and can be changed with WithServerURL.
//
// Example
//
//	mdl, err := cloudflare.New(
//	    cloudflare.WithToken(os.Getenv("CLOUDFLARE_API_TOKEN")),
//	    cloudflare.WithAccountID(os.Getenv("CLOUDFLARE_ACCOUNT_ID")),
//	    cloudflare.WithModel("@cf/meta/llama-3.1-8b-instruct"),
//	)
//	_ = mdl; _ = err
package cloudflare

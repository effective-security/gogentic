// Package cloudflare implements an llms.Model for Cloudflare AI Gateway and
// compatible APIs. Configure with an API token, server URL, and model ID.
//
// Example
//
//	mdl, err := cloudflare.New(
//	    cloudflare.WithToken(os.Getenv("CLOUDFLARE_API_TOKEN")),
//	    cloudflare.WithServerURL("https://api.cloudflare.com/client/v4/accounts/<acct>/ai/run"),
//	    cloudflare.WithModel("@cf/meta/llama-3.1-8b-instruct"),
//	)
//	_ = mdl; _ = err
package cloudflare

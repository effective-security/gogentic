// Package bedrock implements an llms.Model backed by AWS Bedrock. Options allow
// selecting a model ID and injecting an AWS config (and optional custom HTTP
// client) for region/credentials control.
//
// Example
//
//	cfg, _ := config.LoadDefaultConfig(ctx)
//	mdl, err := bedrock.New(
//	    bedrock.WithModel("anthropic.claude-3-5-sonnet-20240620-v1:0"),
//	    bedrock.WithConfig(&cfg),
//	)
//	_ = mdl; _ = err
package bedrock

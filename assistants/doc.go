// Package assistants provides the core agent loop: system prompt assembly,
// LLM invocation, parallel tool calling, structured output parsing, callbacks
// and message history.
//
// An [Assistant] is generic over its output type, which must implement
// chatmodel.ContentProvider. The type's JSON schema is what steers the model:
// it is sent as a provider response format when the provider supports one, and
// appended to the system prompt otherwise.
//
// # Quick example
//
//	// The output type. jsonschema tags are the model's instructions.
//	type Answer struct {
//	    Text string `json:"text" jsonschema:"required,title=Text,description=The answer."`
//	}
//
//	func (a Answer) GetContent() string { return llmutils.ToJSON(a) }
//
//	// A factory resolves the model from configuration (see pkg/llmfactory).
//	fac, err := llmfactory.Load("llm.yaml")
//	if err != nil {
//	    return err
//	}
//
//	agent := assistants.NewAssistant[Answer](fac,
//	    prompts.NewPromptTemplate("You are a concise assistant.", nil),
//	    assistants.WithMessageStore(store.NewMemoryStore()),
//	).
//	    WithName("researcher"). // binds to assistant_models.researcher
//	    WithDescription("Answers factual questions.")
//
//	// Every run requires a ChatContext; without one Run fails with
//	// chatmodel.ErrInvalidChatContext.
//	chatCtx := chatmodel.NewChatContext("user-123", "", nil)
//	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)
//
//	var out Answer
//	resp, err := agent.Run(ctx, &assistants.CallInput{Input: "Say hi"}, &out)
//
// # Tools
//
// Tools are attached with [Assistant.WithTools] and are keyed by lower-cased
// name. All tools the model requests in one turn are executed concurrently, so
// a tool's Call must be safe for concurrent use. A tool error other than
// chatmodel.ErrFailedUnmarshalInput is serialized back to the model as a tool
// response so it can recover, rather than aborting the run.
//
// # Delegation
//
// [NewAssistantTool] wraps an assistant as a tools.ITool, which is how
// multi-agent flows are built: give the wrapper to a supervising assistant and
// its model decides when to delegate. Context, callbacks and per-call options
// flow downward; token usage is aggregated upward into Response.Usage.
//
// # Limits
//
// Each run is bounded by [DefaultMaxToolCalls], [DefaultMaxMessages],
// [DefaultMaxContentSize] and [DefaultMaxRetries], overridable per assistant
// or per call. Exceeding a limit fails the run with an error naming the
// assistant rather than truncating silently. A response that fails to parse
// into the output type is retried once with tools disabled.
//
// See Documentation/assistants.md for the full option reference and the
// anatomy of a run, and Documentation/orchestration.md for multi-agent
// patterns.
package assistants

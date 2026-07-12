// Package assistants provides the core logic for building LLM agents:
// orchestration, tool integration, callbacks, and structured I/O.
//
// Quick example
//
//	// Build a minimal assistant that returns a typed JSON result.
//	// Define output shape:
//	 type Answer struct { Text string `json:"text"` }
//
//	// Create a system prompt (see pkg/prompts for more helpers)
//	 sys := prompts.NewChatPromptTemplate([]prompts.MessageFormatter{
//	     prompts.NewSystemMessagePromptTemplate("You are a concise assistant.", nil),
//	 })
//
//	 // Construct the assistant using a factory (or set cfg.Model directly)
//	 fac, _ := llmfactory.Load("path/to/config.yaml")
//	 a := assistants.NewAssistant[Answer](fac, sys)
//
//	 // Provide chat context and call
//	 ctx := chatmodel.WithChatContext(context.Background(), chatmodel.NewChatContext("user-1", "", nil))
//	 resp, err := a.Call(ctx, &assistants.CallInput{Input: "Say hi"})
//	 _ = resp; _ = err
package assistants

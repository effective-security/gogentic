// Package chatmodel defines common chat I/O types, context helpers, and parser
// interfaces used across the project.
//
// Highlights
//   - ChatContext stores user/chat/run identifiers and metadata for flows.
//   - Input/Output models (`InputRequest`, `OutputResult`) implement
//     `ContentProvider` to unify message handling.
//   - `OutputParser[T]` abstracts parsing LLM output into typed results.
//
// Example: Create and propagate ChatContext
//
//	ctx := context.Background()
//	chat := NewChatContext("user-123", "", nil) // auto-generates ChatID + RunID
//	ctx = WithChatContext(ctx, chat)
//	// Later in a worker goroutine:
//	bg := NewFromContext(ctx) // preserves the ChatContext on a new background context
//	_ = bg
//
// Example: Wrap user input/output
//
//	in := NewInputRequest("How is the weather in Paris?")
//	out := NewOutputResult("It’s 22°C and sunny in Paris today.")
//	_ = in.GetContent()
//	_ = out.GetContent()
//
// Example: Use an OutputParser with an encoder (see encoding package for more)
//
//	type Weather struct { City string; TempC int }
//	parser, _ := encoding.NewTypedOutputParser(Weather{}, encoding.ModeJSONSchema)
//	_ = parser.GetFormatInstructions() // embed in prompt
//	res, _ := parser.Parse(`{"City":"Paris","TempC":22}`)
//	_ = res // *Weather
package chatmodel

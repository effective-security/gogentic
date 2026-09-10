// Package callbacks provides implementations of the assistant and tool
// callback interfaces for tracing, logging and per-run accounting.
//
// A handler is attached with assistants.WithCallback and receives every
// assistant, LLM and tool event. The handler is propagated into nested
// assistants invoked through assistants.AssistantTool, so one handler observes
// a whole multi-agent run.
//
// Handlers are invoked from the parallel tool-call goroutines, so every
// implementation must be safe for concurrent use. All handlers in this package
// lock internally.
//
// # Handlers
//
//   - [Noop] does nothing; use it as an embedded base for custom handlers, or
//     with WithProgress for progress reporting only.
//   - [Printer] writes a human-readable trace to an io.Writer.
//   - [PackageLogger] emits structured xlog key/value entries.
//   - [Fanout] forwards every event to several handlers.
//   - [Scratchpad] accumulates a transcript and [RunStats] per run.
//
// # Example: trace a run to stdout
//
//	agent := assistants.NewAssistant[Answer](fac, sysPrompt,
//	    assistants.WithCallback(callbacks.NewPrinter(os.Stdout, callbacks.ModeVerbose)),
//	)
//
// # Example: collect a per-run report
//
//	pad := callbacks.NewScratchpad(callbacks.ModeVerbose)
//	agent := assistants.NewAssistant[Answer](fac, sysPrompt,
//	    assistants.WithCallback(pad))
//
//	pad.StartRun(ctx) // requires a chatmodel.ChatContext on ctx
//	_, err := agent.Run(ctx, input, &out)
//	stats, transcript := pad.EndRun(ctx)
//
// Scratchpad accumulates token usage at the LLM-call boundary rather than in
// OnAssistantEnd, because Response.Usage is already the aggregated subtree
// total and would otherwise be counted twice for nested assistants.
package callbacks

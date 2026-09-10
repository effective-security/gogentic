# Observability

Three layers, usable independently:

- **Callbacks** — in-process hooks on every assistant, LLM and tool event.
- **Scratchpad** — a callback that accumulates a per-run transcript and stats.
- **Metrics** — counters and timings emitted by the assistant loop.

Sources: [`callbacks/`](../callbacks), [`pkg/metricskey/`](../pkg/metricskey).

## Callbacks

`assistants.Callback` embeds `tools.Callback`:

```go
type Callback interface {
    // from tools.Callback
    OnToolStart(ctx context.Context, tool tools.ITool, assistantName, input string)
    OnToolEnd(ctx context.Context, tool tools.ITool, assistantName, input, output string)
    OnToolError(ctx context.Context, tool tools.ITool, assistantName, input string, err error)

    OnAssistantStart(ctx context.Context, a IAssistant, input string)
    OnAssistantEnd(ctx context.Context, a IAssistant, input string, resp *Response, messageHistory llms.Messages)
    OnAssistantError(ctx context.Context, a IAssistant, input string, err error, messageHistory llms.Messages)
    OnAssistantLLMCallStart(ctx context.Context, a IAssistant, llm llms.Model, payload llms.Messages)
    OnAssistantLLMCallEnd(ctx context.Context, a IAssistant, llm llms.Model, resp *llms.ContentResponse)
    OnAssistantLLMParseError(ctx context.Context, a IAssistant, input, response string, err error)
    OnToolNotFound(ctx context.Context, a IAssistant, tool string)
}
```

Attach one handler:

```go
agent := assistants.NewAssistant[Answer](fac, sysPrompt,
    assistants.WithCallback(callbacks.NewPrinter(os.Stdout, callbacks.ModeVerbose)),
)
```

Two properties matter for multi-agent flows:

- The handler is **propagated into nested assistants** invoked through
  `AssistantTool`, so one handler sees the whole tree.
- Tool callbacks fire from the parallel tool goroutines, so **a handler must be
  goroutine-safe**. The built-in handlers all lock internally.

### Built-in handlers

| Handler | Constructor | Behavior |
|---------|-------------|----------|
| `Printer` | `callbacks.NewPrinter(io.Writer, mode)` | Human-readable trace to a writer |
| `PackageLogger` | `callbacks.NewPackageLogger(*xlog.PackageLogger)` | Structured key/value logging |
| `Noop` | `callbacks.NewNoop()` | Does nothing; `WithProgress(cb)` adds progress-only reporting |
| `Fanout` | `callbacks.NewFanout(handlers...)` | Forwards every event to several handlers; `Add` appends one |
| `Scratchpad` | `callbacks.NewScratchpad(mode)` | Accumulates a transcript and stats per run |

`Mode` is `ModeDefault` or `ModeVerbose`. Verbose adds the full message
payloads and assistant output; default keeps the trace short.

```go
handler := callbacks.NewFanout(
    callbacks.NewPackageLogger(logger),
    scratchpad,
)
```

### Progress reporting

For coarse user-facing progress, independent of the event hooks:

```go
resp, err := agent.Run(ctx, &assistants.CallInput{
    Input: question,
    OnProgress: func(ctx context.Context, a assistants.IAssistant, title, message string) {
        publishToUI(a.Name(), title, message)
    },
}, &out)
```

`Noop.WithProgress` lets you use progress reporting without any tracing:

```go
h := callbacks.NewNoop()
h.WithProgress(func(ctx context.Context, a assistants.IAssistant, title, msg string) {
    publishToUI(a.Name(), title, msg)
})
agent := assistants.NewAssistant[Answer](fac, sysPrompt, assistants.WithCallback(h))
```

## Scratchpad

The scratchpad is the tool to reach for when debugging a multi-agent run: it
produces one transcript and one stats block covering every nested assistant and
tool call.

```go
pad := callbacks.NewScratchpad(callbacks.ModeVerbose)

agent := assistants.NewAssistant[chatmodel.OutputResult](fac, sysPrompt,
    assistants.WithCallback(pad),
).WithTools(researchTool)

pad.StartRun(ctx)                  // keyed by ChatID from the ChatContext
resp, err := agent.Run(ctx, input, &out)
stats, transcript := pad.EndRun(ctx)

fmt.Printf("%s\n", transcript)
fmt.Printf("duration=%s llm_calls=%d tools=%d/%d\n",
    stats.Duration, stats.Usage.LlmCallCount,
    stats.ToolsCallsSucceeded, stats.ToolsCalls)
```

`StartRun` requires a `ChatContext` on the context (runs are keyed by chat ID),
and `EndRun` removes the run and returns its buffer. Both return zero values
when no run is active, so they are safe to call unconditionally.

```go
type RunStats struct {
    ChatID, RunID string
    Duration      time.Duration
    TotalMessages uint32
    Usage         llms.UsageStats

    AssistantCalls, AssistantCallsSucceeded, AssistantCallsFailed uint32
    ToolsCalls, ToolsCallsSucceeded, ToolsCallsFailed             uint32
    ToolNotFound                                                  uint32
}
```

Usage is accumulated at the **LLM-call boundary**, not in `OnAssistantEnd` —
`Response.Usage` is already the subtree total, so counting it per assistant
would double count when the handler is propagated into nested assistants.
`Response.Usage` and `RunStats.Usage` should therefore agree for a single
top-level run.

Entries are tagged with the action ID from `chatmodel.WithActionID` and the
assistant name, which is what makes a pipeline's transcript readable.

`callbacks.TimeNowFn` is a package-level variable — override it in tests for
deterministic durations.

## Metrics

The assistant loop emits metrics through
[`effective-security/metrics`](https://github.com/effective-security/metrics).
Descriptors live in `pkg/metricskey`; `metricskey.Metrics` is the full slice,
ready to register with your collector.

**Counters, tagged `agent`, `model`, `org`:**

| Metric | Meaning |
|--------|---------|
| `stats_llm_messages_sent` | Messages in each request |
| `stats_llm_bytes_sent` / `_received` / `_total` | Payload sizes |
| `stats_llm_input_tokens` / `_output_tokens` / `_total_tokens` | Token usage |
| `stats_llm_cached_write_tokens` / `_cached_read_tokens` | Prompt-cache effectiveness |
| `stats_assistant_calls_succeeded` / `_failed` / `_retried` | Assistant outcomes (`_retried` = output-parse retry) |
| `stats_assistant_llm_parse_errors` | Output failed to parse into the typed result |

**Counters, tagged `tool`, `model`, `org`:**

| Metric | Meaning |
|--------|---------|
| `stats_tool_calls_succeeded` / `_failed` | Tool outcomes |
| `stats_tool_calls_not_found` | Model asked for an unregistered tool |

**Samples (durations):**

| Metric | Tags |
|--------|------|
| `perf_assistant_call` | `agent`, `model`, `org` |
| `perf_tool_call` | `tool`, `model`, `org` |

The `org` tag comes from `ChatContext.GetOrgID()`, so setting it is what makes
per-tenant dashboards possible. The `agent` tag is `Assistant.Name()` — another
reason to name assistants deliberately.

### What to watch

| Signal | Likely cause |
|--------|--------------|
| `stats_tool_calls_not_found` climbing | Tool descriptions are ambiguous, or a tool was renamed while history still references it |
| `stats_assistant_llm_parse_errors` climbing | Output type too complex, or the provider lacks `CapabilityJSONSchema` and is ignoring the prompt schema |
| `stats_assistant_calls_retried` climbing | Same as above; each retry is a full extra LLM call |
| `stats_llm_cached_read_tokens` near zero with a cache policy set | Prompt prefix is not stable, or the provider lacks `CapabilityPromptCaching` |
| `perf_tool_call` p99 spikes | A slow tool serializing the parallel tool batch — the LLM turn waits for the slowest tool |

## Logging

Packages log via `xlog` package loggers named
`github.com/effective-security/gogentic/<package>`. Enable them in development:

```go
xlog.SetFormatter(xlog.NewStringFormatter(os.Stdout))
xlog.SetGlobalLogLevel(xlog.DEBUG)
```

At `DEBUG` the assistant logs history size, tool-call discovery and responses,
and model resolution — usually enough to diagnose a misbehaving run without a
scratchpad.

## Writing a custom handler

Embed `callbacks.Noop` to implement only what you need; new methods added to
the interface then will not break your handler.

```go
type otelHandler struct {
    *callbacks.Noop
    tracer trace.Tracer
}

func (h *otelHandler) OnToolStart(ctx context.Context, tool tools.ITool, assistantName, input string) {
    _, span := h.tracer.Start(ctx, "tool/"+tool.Name())
    defer span.End()
    // ...
}
```

Two caveats: handlers run **inline** on the request path, so keep them cheap
(buffer or queue anything slow), and they must be safe for concurrent use.

Note that the callback interface has no per-call handle, so correlating a
`Start` with its `End` is done through the `ChatContext`/action ID on the
context — which is what `Scratchpad` does.

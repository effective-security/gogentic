# Orchestration

gogentic has no separate "orchestrator" type. Multi-agent flows are built by
wrapping an assistant as a tool and giving it to another assistant. The
supervisor's LLM then decides which sub-agent to delegate to, using the normal
tool-calling loop.

Source: [`assistants/assistant_tool.go`](../assistants/assistant_tool.go).

## Assistant as a tool

`AssistantTool[I, O]` adapts a `TypeableAssistant[O]` to `tools.ITool`:

```go
// Sub-agent: searches the web and returns a cited answer.
researcher := assistants.NewAssistant[Answer](fac, researchPrompt,
    assistants.WithMessageStore(store.NewMemoryStore()),
).
    WithName("researcher").
    WithDescription("Searches the web and answers factual questions with sources.").
    WithTools(websearch)

// Wrap it as a tool. I = the input type the caller must produce,
// O is inferred from the assistant.
researchTool, err := assistants.NewAssistantTool[chatmodel.InputRequest](researcher)
if err != nil {
    return err
}

// Supervisor: owns the conversation and delegates.
supervisor := assistants.NewAssistant[chatmodel.OutputResult](fac, supervisorPrompt,
    assistants.WithMessageStore(store.NewMemoryStore()),
    assistants.WithCallback(callbacks.NewScratchpad(callbacks.ModeVerbose)),
).
    WithName("supervisor").
    WithDescription("Coordinates specialist assistants.").
    WithTools(researchTool)

var out chatmodel.OutputResult
resp, err := supervisor.Run(ctx, &assistants.CallInput{
    Input: "Which European capital had the most rainfall last month? Cite sources.",
}, &out)
```

The tool's `Name()` and `Description()` default to the wrapped assistant's, so
naming the assistant well is what makes delegation work. Override them by
type-asserting to the concrete type:

```go
concrete := researchTool.(*assistants.AssistantTool[chatmodel.InputRequest, Answer])
concrete.WithName("web_research").WithDescription("Use for anything needing live web data.")
```

### The input type `I`

`I` defines the JSON Schema the supervisor's model must satisfy, and how that
JSON becomes the sub-agent's `Input` string:

1. If `*I` implements `chatmodel.InputParser`, its `ParseInput` is used.
2. Otherwise the arguments are `CleanJSON`-ed and `json.Unmarshal`-ed into `I`.
3. Either way, `I.GetContent()` becomes the sub-agent's `CallInput.Input`.

`chatmodel.InputRequest` (`{"input": "..."}`) is the usual choice. Define your
own when the sub-agent needs structured delegation:

```go
type ReviewRequest struct {
    Diff  string `json:"diff"  jsonschema:"required,title=Diff,description=Unified diff to review."`
    Focus string `json:"focus" jsonschema:"title=Focus,description=What to prioritize,enum=security,enum=performance,enum=style"`
}

func (r ReviewRequest) GetContent() string { return llmutils.ToJSON(r) }
```

Malformed arguments return `chatmodel.ErrFailedUnmarshalInput`, which the
assistant loop translates into a "fix your JSON" nudge rather than a failure.

## What propagates into a sub-agent

| Thing | Propagates? | Notes |
|-------|-------------|-------|
| `context.Context` (incl. `ChatContext`) | yes | Same chat and run IDs, so history and metrics line up |
| Callback handler | yes | Injected as the first option, so the whole tree reports to one handler |
| `CallInput.Options` from the parent call | yes | Passed through `CallAssistant(ctx, input, options...)` |
| Token usage | yes, upward | Aggregated into the parent's `Response.Usage` |
| Message store | no | Each assistant uses its own store |
| Tools | no | Each assistant has its own tool set |

Because the callback handler is shared, a
[`callbacks.Scratchpad`](observability.md#scratchpad) at the top level produces
a single report covering every nested agent and tool.

## Usage accounting

`CallAssistant` returns `*llms.UsageStats`, which the parent adds to its own
`Response.Usage`. So the top-level response's `Usage` is the total for the
whole tree: `LlmCallCount`, `BytesIn`/`BytesOut`, and per-model token counts.

```go
for model, u := range resp.Usage.ModelUsage {
    fmt.Printf("%s: in=%d out=%d total=%d\n",
        model, u.InputTokens, u.OutputTokens, u.TotalTokens)
}
```

Usage is aggregated at the LLM-call boundary by the handler and separately on
the response, so there is no double counting.

## Error recovery across agents

By default a sub-agent's error is returned to the caller. But if the sub-agent's
output type implements `chatmodel.IBaseResult` — the easiest way is to embed
`chatmodel.BaseClarificationResult` — `CallAssistant` instead writes the error
into the `Clarification` field and returns a normal result:

```go
type Answer struct {
    chatmodel.BaseClarificationResult
    Text string `json:"text"`
}
```

The supervisor's model then sees something like
`<!-- tool: web_research, error: ... -->` in the clarification and can retry,
delegate elsewhere, or answer without the sub-agent. This turns a hard failure
into a recoverable one — prefer it for any sub-agent that is not critical.

## Routing patterns

**Model-driven delegation (default).** Register several assistant tools; let
the supervisor's model choose. Simple, and the model can chain several.

```go
supervisor = supervisor.WithTools(researchTool, coderTool, reviewerTool)
```

Give the supervisor a catalog in its system prompt so the choice is informed:

```go
catalog := assistants.GetDescriptionsWithTools(researcher, coder, reviewer).
    Render(llmutils.RenderFormatMarkdown)

supervisorPrompt := prompts.NewPromptTemplate(
    "You coordinate specialists. Delegate rather than answering directly.\n\n"+
        "# SPECIALISTS\n{{.specialists}}", []string{"specialists"})

supervisor := assistants.NewAssistant[chatmodel.OutputResult](fac, supervisorPrompt,
    assistants.WithPromptInput(map[string]any{"specialists": catalog}),
)
```

**Code-driven routing.** When the choice is deterministic, skip the LLM: have a
small classifier assistant return an enum, then dispatch in Go.

```go
byName := assistants.MapAssistants(researcher, coder, reviewer)

var route Route // {"assistant": "coder"} with a jsonschema enum
if _, err := router.Run(ctx, &assistants.CallInput{Input: question}, &route); err != nil {
    return err
}
target, ok := byName[route.Assistant]
if !ok {
    return errors.Errorf("unknown assistant: %s", route.Assistant)
}
resp, err := target.Call(ctx, &assistants.CallInput{Input: question})
```

**Fixed pipeline.** Chain `Run` calls directly and pass typed values between
stages. Use `chatmodel.WithActionID` to tag each stage so stored messages and
the scratchpad show which step produced what:

```go
ctx = chatmodel.WithActionID(ctx, "extract")
if _, err := extractor.Run(ctx, &assistants.CallInput{Input: doc}, &facts); err != nil {
    return err
}

ctx = chatmodel.WithActionID(ctx, "summarize")
if _, err := summarizer.Run(ctx, &assistants.CallInput{Input: facts.GetContent()}, &summary); err != nil {
    return err
}
```

## Keeping sub-agent chatter out of the main transcript

A sub-agent's turns usually should not pollute the user-visible conversation:

```go
researcher := assistants.NewAssistant[Answer](fac, researchPrompt,
    assistants.WithSkipMessageHistory(true), // write nothing to a store
    assistants.WithGeneric(true),            // record turns as RoleGeneric with provenance
)
```

`WithGeneric(true)` also wraps the sub-agent's input and observation in
`<!-- assistant: name, ... -->` comments, which makes multi-agent transcripts
readable when they *are* stored. Use `WithSkipToolHistory(true)` to keep the
turn but drop the verbose tool-call pairs.

## Budget control

Nested agents multiply LLM calls. Bound them explicitly:

```go
supervisor := assistants.NewAssistant[chatmodel.OutputResult](fac, supervisorPrompt,
    assistants.WithMaxToolCalls(8),   // at most 8 delegations per run
    assistants.WithMaxMessages(60),
)
```

Because `CallInput.Options` propagate downward, passing
`assistants.WithMaxToolCalls(2)` on the supervisor's call also caps each
sub-agent's own tool usage.

Per-organization model limits belong in the factory instead — see
[LLM Factory → Restricting models](llm-factory.md#restricting-models-per-organization).

## Exposing a sub-agent over MCP

`AssistantTool` implements `tools.IMCPTool`, so the same wrapper serves local
delegation and remote MCP callers:

```go
if err := researchTool.RegisterMCP(mcpServer); err != nil {
    return err
}
```

An assistant can also be registered as an MCP *prompt* via
`IMCPAssistant.RegisterMCP`. See [MCP](mcp.md#exposing-assistants-and-tools).

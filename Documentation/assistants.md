# Assistants

An *assistant* is one agent: a system prompt, an output type, a set of tools,
and a loop that keeps calling the LLM until it stops asking for tools.

Source: [`assistants/`](../assistants) —
`assistants.go` (interfaces), `assistant.go` (the loop),
`options.go` (config), `assistant_tool.go` (assistant-as-tool),
`skills.go` (skills prompt).

## Quick start

```go
// 1. The output type. It must implement chatmodel.ContentProvider.
type Answer struct {
    Text    string   `json:"text"    jsonschema:"title=Text,description=The answer to the question."`
    Sources []string `json:"sources" jsonschema:"title=Sources,description=URLs backing the answer."`
}

func (a Answer) GetContent() string { return llmutils.ToJSON(a) }

// 2. A factory that knows how to build models (see llm-factory.md).
fac, err := llmfactory.Load("llm.yaml")
if err != nil {
    return err
}

// 3. The system prompt.
sysPrompt := prompts.NewPromptTemplate(
    "You are a concise research assistant.", nil)

// 4. The assistant, parameterized by its output type.
agent := assistants.NewAssistant[Answer](fac, sysPrompt,
    assistants.WithMessageStore(store.NewMemoryStore()),
).
    WithName("researcher").
    WithDescription("Answers factual questions and cites sources.")

// 5. Every call needs a ChatContext.
chatCtx := chatmodel.NewChatContext("user-123", "", nil) // empty chatID => generated
ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

// 6. Run it. `out` is populated from the parsed model output.
var out Answer
resp, err := agent.Run(ctx, &assistants.CallInput{
    Input: "What is the capital of the largest country in Europe?",
}, &out)
if err != nil {
    return err
}

fmt.Println(out.Text, resp.Usage.ModelUsage)
```

`NewAssistant` defaults `Name()` to `"Generic Assistant"` and `Description()`
to a generic string — always set them, because both are used by
`llmfactory` (assistant→model mapping keys off the name) and by other
assistants when this one is exposed as a tool.

## The output type

`Assistant[O]` is generic over its output type `O`, which must satisfy
`chatmodel.ContentProvider`:

```go
type ContentProvider interface {
    GetContent() string
}
```

`GetContent()` is what gets written to the message history for this turn, so it
should be the model-visible rendering of the result (usually the JSON form).

Two ready-made types cover the common cases:

| Type | Use for |
|------|---------|
| `chatmodel.OutputResult` | Free-form markdown answers (`{"content": "..."}`) |
| `chatmodel.String` | Raw text, no structure imposed |

Embed `chatmodel.BaseClarificationResult` in your own type to get
`confidence`, `clarification` and `reasoning` fields, and the
`IBaseResult` setters. `AssistantTool` uses those setters to hand an error back
to the calling agent as a clarification instead of failing the whole flow —
see [Orchestration](orchestration.md#error-recovery-across-agents).

```go
type CVEResult struct {
    chatmodel.BaseClarificationResult
    CVE     string   `json:"cve"     jsonschema:"title=CVE,description=The CVE identifier."`
    Sources []string `json:"sources" jsonschema:"title=Sources,description=Sources for the CVE."`
}

func (r CVEResult) GetContent() string { return llmutils.ToJSON(r) }
```

The JSON Schema derived from `O` is what steers the model. See
[Structured Output](structured-output.md) for how the schema is generated and
how it reaches the provider.

## `Call` versus `Run`

```go
Call(ctx, input) (*Response, error)                     // always parses into O
Run(ctx, input, optionalOutputType *O) (*Response, error)  // parses only if non-nil
```

- `Call` is the `IAssistant` interface method; it allocates an `O` internally
  and discards it, so use it when you only need `Response.Choices` but still
  want the output validated.
- `Run(ctx, in, &out)` is what you normally call: you get the typed result.
- `Run(ctx, in, nil)` **skips output parsing entirely**. Use it when the model
  returns prose and imposing a schema would be wrong.

## `CallInput`

```go
type CallInput struct {
    Input        string            // the user turn
    PromptInputs map[string]any    // values for the system prompt template
    Options      []Option          // per-call config overrides
    Messages     []llms.Message    // extra messages appended after the user turn
    Args         map[string]string // free-form args for custom IAssistant impls
    OnProgress   OnProgressFunc    // generic progress reporting
}
```

`Options` are applied on top of the assistant's config into a copy, so they
never leak into the next call:

```go
resp, err := agent.Run(ctx, &assistants.CallInput{
    Input: "Summarize this in one sentence.",
    Options: []assistants.Option{
        assistants.WithTemperature(0),
        assistants.WithMaxToolCalls(5),
    },
}, &out)
```

## System prompt assembly

`GetSystemPrompt` builds the final system message in this order:

1. **Your template**, formatted with `cfg.PromptInput` merged with
   `CallInput.PromptInputs` (call-level wins).
2. **Dynamic inputs** from `WithPromptInputProvider`, merged on top.
3. **The skills catalog**, if skills are attached — rendered once and cached on
   the assistant. Override the rendering with `WithSkillsPromptProvider`.
4. **`# OUTPUT SCHEMA`** with the parser's format instructions — but *only* when
   no provider-native `ResponseFormat` was negotiated. If the provider supports
   `json_schema`, the schema goes on the request instead of into the prompt.

```go
sysPrompt := prompts.NewPromptTemplate(
    "You are an assistant for {{.org}}. Today is {{.today}}.",
    []string{"org", "today"})

agent := assistants.NewAssistant[Answer](fac, sysPrompt,
    // static values, available to every call
    assistants.WithPromptInput(map[string]any{"org": "Acme"}),
)

// dynamic values, computed per call from the input
agent.WithPromptInputProvider(func(ctx context.Context, input string) (map[string]any, error) {
    return map[string]any{"today": time.Now().Format("2006-01-02")}, nil
})
```

Missing template variables are an error (`missingkey=error`), so every variable
must be supplied by one of the three sources.

## Tools

```go
agent = agent.WithTools(myTool, anotherTool)
```

`WithTools` is additive and idempotent: tools are keyed by lower-cased name, so
registering the same name twice keeps the first. For each tool it records an
`llms.Tool` function definition built from `Name()`, `Description()` and
`Parameters()`.

Two special cases:

- A tool named `web_search` (case-insensitive) is **not** registered as a
  function. It is added as a provider-native `llms.Tool{Type: "web_search"}`,
  and is dropped from the request for providers that do not advertise
  `CapabilityWebSearchTool`.
- Attaching function tools to a provider without `CapabilityFunctionCalling`
  makes `Run` fail early with a clear error rather than silently dropping them.

You can also pass provider-native tools directly, bypassing `ITool`:

```go
agent := assistants.NewAssistant[CVEResult](fac, sysPrompt,
    assistants.WithTools(llms.Tool{Type: "google_search"}),
)
```

See [Tools](tools.md) for writing tools, and
[Orchestration](orchestration.md) for using assistants as tools.

### The tool-calling loop

Per LLM turn, `executeToolCalls`:

1. Records one assistant message containing all requested tool calls.
2. Runs every requested tool **concurrently**, one goroutine per call.
3. Re-orders responses to match the requested order and appends one
   `RoleTool` message per `tool_call_id` — always, including for failures.
4. Repeats the LLM turn if any tool ran; otherwise the loop ends.

Because tools run in parallel, **a tool's `Call` must be safe for concurrent
use**.

Unknown tool names are not fatal: the model gets
`Tool `x` not found. ... Available tools: a, b` and can retry. More than three
unknown names *within a single turn* aborts the run (the counter is reset at
the end of each turn).

### Limits

| Limit | Default | Option | Behavior when exceeded |
|-------|---------|--------|------------------------|
| Tool calls per run | `DefaultMaxToolCalls` = 50 | `WithMaxToolCalls` | run fails |
| Messages per run | `DefaultMaxMessages` = 100 | `WithMaxMessages` | run fails |
| Prompt content bytes | `DefaultMaxContentSize` = 500000 | `WithMaxLength` | run fails |
| Empty-response retries | `DefaultMaxRetries` = 2 | — | run fails |
| Output-parse retries | 1 extra attempt | — | parse error returned |

All limits fail loudly with an error naming the assistant, rather than
truncating silently.

## Options reference

Options are `func(*Config)`; `Config` is copied per call.

**Model selection and sampling**

| Option | Effect |
|--------|--------|
| `WithModel(llms.Model)` | Use this model; skips the factory lookup entirely |
| `WithModelOptions(func(llms.Model) []Option)` | Compute options from the resolved model, e.g. per-provider temperature |
| `WithTemperature`, `WithTopK`, `WithTopP`, `WithSeed` | Sampling controls |
| `WithMaxTokens`, `WithMinLength`, `WithMaxLength` | Length controls (`MaxLength` also caps prompt bytes) |
| `WithStopWords`, `WithRepetitionPenalty` | Decoding controls |
| `WithReasoningEffort(llms.ReasoningEffort)` | Reasoning budget on models that support it |
| `WithPromptCachePolicy(*llms.PromptCachePolicy)` | Provider-native prompt caching, see [LLM Providers](llms.md#prompt-caching) |

Only options you set are forwarded — each has a "was set" flag, so an unset
`Temperature` is not sent as `0`.

**Output shape**

| Option | Effect |
|--------|--------|
| `WithMode(encoding.Mode)` | How the output type is described and parsed: `ModeJSON`, `ModeJSONSchema` (default), `ModeJSONSchemaStrict`, `ModeYAML`, `ModeTOML`, `ModePlainText` |
| `WithResponseFormat(*schema.ResponseFormat)` | Override the derived provider response format |

**Tools**

| Option | Effect |
|--------|--------|
| `WithTools(...llms.Tool)` | Add provider-native tool definitions |
| `WithToolChoice(any)` | `"none"`, `"auto"`, or a specific tool |
| `WithMaxToolCalls(int)` | Cap tool calls per run |
| `WithEnableFunctionCalls(bool)` | Enable the legacy function-call API |

**History**

| Option | Effect |
|--------|--------|
| `WithMessageStore(store.MessageStore)` | Where history is read from and written to |
| `WithSkipMessageHistory(bool)` | Do not write anything to the store |
| `WithSkipToolHistory(bool)` | Write the turn, but not tool-call/tool-response messages |
| `WithExamples(chatmodel.FewShotExamples)` | Few-shot pairs injected after the system prompt |
| `WithGeneric(bool)` | Record the user turn as `RoleGeneric` with a provenance comment instead of `RoleHuman` — used for agent-to-agent turns |

**Observability**

| Option | Effect |
|--------|--------|
| `WithCallback(assistants.Callback)` | Receive lifecycle events; propagated into nested assistants |
| `WithStreamingFunc(func(ctx, []byte) error)` | Stream response chunks |

See [Observability](observability.md).

## Input pre-processing

`WithInputParser` rewrites the user turn before it reaches the model. Use it to
normalize, redact, or unwrap application-specific envelopes:

```go
agent.WithInputParser(func(in string) (string, error) {
    return strings.TrimSpace(redactSecrets(in)), nil
})
```

## The response

```go
type Response struct {
    Choices  []*llms.ContentChoice // raw provider choices from the final turn
    Messages []llms.Message        // messages produced by this run (also stored)
    Model    string                // model actually used
    Usage    llms.UsageStats       // tokens, bytes, LLM call count, per model
}
```

`Response.String()` concatenates choice contents — handy for prose assistants.
`Usage` aggregates nested assistant usage too, so a top-level orchestrator's
`Usage` covers the whole tree.

If several choices come back, their contents are joined with a blank line
before parsing.

## Error contract

| Error | Meaning | Typical fix |
|-------|---------|-------------|
| `chatmodel.ErrInvalidChatContext` | No `ChatContext` on the context | wrap with `chatmodel.WithChatContext` |
| `chatmodel.ErrFailedUnmarshalOutput` | Model output did not match `O` | already retried once; simplify the type or switch `Mode` |
| `chatmodel.ErrFailedUnmarshalInput` | A tool or nested assistant could not parse its input | the model is told to fix the JSON and retries |
| `"the tool calls limit is exceeded"` | `MaxToolCalls` hit | raise the cap or tighten the prompt |
| `"the messages count exceeded limit"` | `MaxMessages` hit | raise the cap, or trim history |
| `"the content size exceeded limit"` | Prompt bytes over `MaxLength` | trim history or raise `WithMaxLength` |
| `"the %s provider does not support function calling"` | Tools attached to a text-only provider | pick a provider with `CapabilityFunctionCalling` |
| `"unable to get LLM model for assistant %s"` | Factory could not resolve a model | check `assistant_models` and `available_models` |

Use `errors.Is` — these are sentinels wrapped with `cockroachdb/errors`.

## Interfaces

```go
// Every assistant.
type IAssistant interface {
    Name() string
    Description() string
    GetTools() []tools.ITool
    GetSkills() skills.Skills
    FormatPrompt(values map[string]any) (llms.PromptValue, error)
    GetPromptInputVariables() []string
    Call(ctx context.Context, input *CallInput) (*Response, error)
}

// Assistants with a known output type.
type TypeableAssistant[O chatmodel.ContentProvider] interface {
    IAssistant
    Run(ctx context.Context, input *CallInput, optionalOutputType *O) (*Response, error)
}

// Assistants callable as a tool by another assistant.
type IAssistantTool interface {
    tools.ITool
    CallAssistant(ctx context.Context, input string, options ...Option) (string, *llms.UsageStats, error)
}

// Assistants exposable over MCP.
type IMCPAssistant interface {
    IAssistant
    RegisterMCP(registrator McpServerRegistrator) error
    CallMCP(context.Context, chatmodel.MCPInputRequest) (*mcp.PromptResponse, error)
}
```

`*Assistant[O]` implements all four. Implement `IAssistant` yourself when you
need a hand-written flow — `CallInput.Args` exists for that case.

## Describing a fleet of assistants

For router or supervisor prompts, render the available assistants compactly:

```go
list := assistants.GetDescriptionsWithTools(researcher, coder, reviewer)
catalog := list.Render(llmutils.RenderFormatMarkdown)
// - Name: researcher
//   Description: Answers factual questions and cites sources.
//   Tools:
//     - Name: tavily_web_search
//       Description: A tool that provides a web search functionality.
```

`GetDescriptions` omits tools; `MapAssistants` builds a `map[string]IAssistant`
for dispatching by name.

# LLM Providers

`pkg/llms` defines the provider-agnostic surface every model implements, plus
the message, option and usage types used everywhere else in the repo. The
subpackages under `pkg/llms/` implement it per provider.

Source: [`pkg/llms/`](../pkg/llms) — `llms.go` (the `Model` interface and
capability table), `generatecontent.go` (messages, content parts, usage),
`options.go` (`CallOption`s, prompt caching), `batch.go` (batch API),
`prompts.go` (`PromptValue`).

Most applications never call this package directly: build a model with
[`llmfactory`](llm-factory.md) and hand it to an
[assistant](assistants.md). Reach for `pkg/llms` when you need a single
completion, batch processing, or embeddings.

## The `Model` interface

```go
type Model interface {
    GetName() string
    GetProviderType() ProviderType
    GenerateContent(ctx context.Context, messages []Message, options ...CallOption) (*ContentResponse, error)
}
```

Convenience wrapper for the single-prompt case:

```go
answer, err := llms.GenerateFromSinglePrompt(ctx, model,
    "Summarize the CAP theorem in two sentences.",
    llms.WithTemperature(0),
    llms.WithMaxTokens(200),
)
```

Two optional interfaces are discovered by type assertion:

```go
// Embeddings.
if e, ok := model.(llms.Embedder); ok {
    vectors, err := e.CreateEmbedding(ctx, []string{"hello", "world"})
}

// Asynchronous batch processing.
if b, ok := model.(llms.Batcher); ok {
    handle, err := b.SubmitBatch(ctx, requests)
}
```

## Messages and content parts

A `Message` is a role plus a list of typed parts:

```go
type Message struct {
    Role   Role
    Parts  []ContentPart
    Source *MessageSource // provenance: assistant name, run ID, action ID
}
```

Roles: `RoleSystem`, `RoleHuman`, `RoleAI`, `RoleTool`, and `RoleGeneric`
(used for agent-to-agent turns; see
[Orchestration](orchestration.md#keeping-sub-agent-chatter-out-of-the-main-transcript)).

Content parts:

| Part | Constructor | Carries |
|------|-------------|---------|
| `TextContent` | `llms.TextPart(s)` | plain text |
| `ImageURLContent` | `llms.ImageURLPart(url)`, `llms.ImageURLWithDetailPart(url, detail)` | image reference |
| `BinaryContent` | `llms.BinaryPart(mime, data)` | inline bytes (rendered as a data URI) |
| `ToolCall` | — | a call the model requested |
| `ToolCallResponse` | — | the result you return for a `tool_call_id` |

Message builders:

```go
llms.MessageFromTextParts(llms.RoleHuman, "What is in this image?")
llms.MessageFromParts(llms.RoleHuman,
    llms.TextPart("Describe this:"),
    llms.ImageURLPart("https://example.com/cat.png"),
)
llms.MessageFromToolCalls(llms.RoleAI, toolCalls...)
llms.MessageFromToolResponse(llms.RoleTool, llms.ToolCallResponse{
    ToolCallID: "call_1", Name: "get_weather", Content: `{"temp_c":21}`,
})
```

Every part implements `ContentLength()`, which is what the assistant loop uses
to enforce `MaxLength` without tokenizing. `llmutils.CountMessagesContentSize`
sums a slice, and `llmutils.PrintMessages` / `Message.Print` dump a transcript
for debugging.

`Message.WithSource` attaches provenance and is **non-overwriting**: an
existing `Source` is preserved, so a message tagged by a sub-agent keeps its
original attribution as it flows up.

## Responses and usage

```go
type ContentResponse struct { Choices []*ContentChoice }

type ContentChoice struct {
    Content          string
    StopReason       string
    GenerationInfo   map[string]any
    Usage            Usage
    FuncCall         *FunctionCall  // legacy single function call
    ToolCalls        []ToolCall
    ReasoningContent string
}
```

`Usage` counts tokens (`InputTokens`, `OutputTokens`, `CacheWriteTokens`,
`CacheReadTokens`, `ReasoningTokens`, `TotalTokens`).
`ContentResponse.Usage()` sums across choices.

`UsageStats` is the aggregate the assistant layer reports: per-model `Usage`,
plus `BytesIn`, `BytesOut` and `LlmCallCount`. Both types have `Add` helpers,
which is how nested assistant usage rolls up.

## Call options

Pass `CallOption`s to `GenerateContent`. When you are inside an assistant, use
the `assistants.WithXxx` equivalents instead — they translate to these, and
only forward what you actually set.

| Group | Options |
|-------|---------|
| Model & length | `WithModel`, `WithMaxTokens`, `WithMinLength`, `WithMaxLength`, `WithN`, `WithCandidateCount` |
| Sampling | `WithTemperature`, `WithTopK`, `WithTopP`, `WithSeed`, `WithStopWords`, `WithRepetitionPenalty`, `WithFrequencyPenalty`, `WithPresencePenalty` |
| Tools | `WithTools`, `WithToolChoice` |
| Output | `WithResponseFormat`, `WithReasoningEffort` |
| Streaming | `WithStreamingFunc`, `WithStreamingReasoningFunc` |
| Other | `WithMetadata`, `WithPromptCachePolicy`, `WithOptions` |

Streaming:

```go
_, err := model.GenerateContent(ctx, messages,
    llms.WithStreamingFunc(func(ctx context.Context, chunk []byte) error {
        _, err := os.Stdout.Write(chunk)
        return err // returning an error stops the stream early
    }),
)
```

`ReasoningEffort` is an enum: `ReasoningEffortDefault` (not sent),
`ReasoningEffortNone`, `Low`, `Medium`, `High`.

## Capabilities

`providerCapabilities` in `llms.go` is the authoritative table of what each
provider family supports.

| Capability | Meaning |
|------------|---------|
| `CapabilityText` | Basic chat/text generation |
| `CapabilityJSONResponse` | JSON output mode |
| `CapabilityJSONSchema` | Schema-constrained output |
| `CapabilityJSONSchemaStrict` | Strict schema (all properties required) |
| `CapabilityFunctionCalling` | Tool/function calls |
| `CapabilityMultiToolCalling` | Several tool calls per turn |
| `CapabilityToolCallStreaming` | Streamed tool calls |
| `CapabilityVision` | Image inputs |
| `CapabilityImageGeneration`, `CapabilityAudioTranscription` | Multimodal generation |
| `CapabilitySelfHosted` | Open-weight / self-hosted |
| `CapabilitySystemPrompt` | Dedicated system role |
| `CapabilityWebSearchTool` | Provider-native web search grounding |
| `CapabilityPromptCaching` | Provider-native prompt caching |
| `CapabilityBatch` | Implements `Batcher` |

**Two different checks, do not mix them up:**

```go
// ANY of the bits — a loose check, used for single-capability questions.
if model.GetProviderType().Supports(llms.CapabilityVision) { ... }

// ALL of the bits — what llmfactory.ModelOptions.RequiredCapabilities enforces.
llms.ProviderCapabilities(pt)&required == required
```

`ProviderType.Supports` returns true if *any* requested bit is set, so passing
it an OR of several capabilities does not mean "supports all of them". For an
all-of check, use `RequiredCapabilities` on
[`llmfactory.ModelOptions`](llm-factory.md#capabilities) or compare the mask
yourself.

Capabilities are consumed in three places: the factory filters candidate
providers, `Assistant.setResponseFormat` decides whether to send a JSON Schema
response format or fall back to prompt instructions, and
`Config.GetCallOptions` drops `web_search` tools for providers that lack it.

## Prompt caching

`PromptCachePolicy` unifies two provider models under one type. Set it via
`assistants.WithPromptCachePolicy` or `llms.WithPromptCachePolicy`.

**OpenAI-style (request-level).** The provider decides what to cache; you
supply a stable key and a retention window.

```go
policy := &llms.PromptCachePolicy{
    Request: &llms.PromptCacheRequestPolicy{
        Key:       "tenant-1000/system-v3",
        Retention: llms.PromptCacheRetention24h, // or PromptCacheRetentionInMemory
    },
}
```

**Anthropic-style (explicit breakpoints).** You mark which prompt blocks or
tool definitions are cacheable, by index into the messages you passed.

```go
policy := &llms.PromptCachePolicy{
    Breakpoints: []llms.PromptCacheBreakpoint{
        {
            // Cache the system message's first part.
            Target: llms.PromptCacheTarget{
                Kind:         llms.PromptCacheTargetMessagePart,
                MessageIndex: 0,
                PartIndex:    0,
            },
            TTL: llms.PromptCacheTTL1h, // or PromptCacheTTL5m
        },
        {
            // Cache the third tool definition.
            Target: llms.PromptCacheTarget{
                Kind: llms.PromptCacheTargetTool, ToolIndex: 2,
            },
        },
    },
}
```

Indexes refer to the caller-visible message/part positions; the Anthropic
implementation maps them onto the provider's own block layout. Requesting
`PromptCacheTTL1h` makes the client add the extended-cache-TTL beta header for
that request only.

Cache effectiveness shows up as `CacheWriteTokens` / `CacheReadTokens` in
`Usage`, which are also exported as
`stats_llm_cached_write_tokens` / `stats_llm_cached_read_tokens`
([Observability](observability.md#metrics)).

Providers without `CapabilityPromptCaching` ignore the policy.

## Batch API

Providers advertising `CapabilityBatch` implement `Batcher`. The contract is
deliberately async-only: you own the polling loop, and `BatchHandle.ID` is
persistable so a different process can reattach.

```go
b, ok := model.(llms.Batcher)
if !ok {
    return errors.WithStack(llms.ErrBatchNotSupported)
}

handle, err := b.SubmitBatch(ctx, []llms.BatchRequest{
    {
        CustomID: "doc-1",
        Messages: []llms.Message{llms.MessageFromTextParts(llms.RoleHuman, "Summarize doc 1")},
        Options:  []llms.CallOption{llms.WithMaxTokens(500)},
    },
    {
        CustomID: "doc-2",
        Messages: []llms.Message{llms.MessageFromTextParts(llms.RoleHuman, "Summarize doc 2")},
    },
}, llms.WithBatchMetadata(map[string]string{"job": "nightly"}))
if err != nil {
    return err
}

// ... persist handle.ID, then later, possibly in another process:
for {
    handle, err = b.GetBatch(ctx, handle.ID)
    if err != nil {
        return err
    }
    if handle.Status.IsTerminal() {
        break
    }
    // sleep and poll again
}

results, err := b.FetchBatchResults(ctx, handle.ID)
if err != nil {
    return err // llms.ErrBatchNotReady if the batch is still running
}
for _, r := range results {
    if r.Error != nil {
        log.Printf("%s failed: %s", r.CustomID, r.Error) // per-request failure
        continue
    }
    process(r.CustomID, r.Response)
}
```

- `BatchStatus.IsTerminal()` covers `completed`, `failed`, `expired`,
  `cancelled` — stop polling when it returns true.
- Per-request failures arrive on `BatchResult.Error`, not as a returned error,
  so partial successes are usable.
- Sentinels: `ErrBatchNotReady`, `ErrBatchNotSupported`, `ErrBatchNotFound`.
- `BatchHandle.ProviderMeta` holds the raw provider object for fields this
  abstraction does not surface (e.g. OpenAI's `output_file_id`).

## Constructing providers directly

The factory is the recommended path, but direct construction is available:

```go
model, err := openai.New(
    openai.WithProvider(llms.ProviderOpenAI),
    openai.WithModel("gpt-5.1"),
    openai.WithToken(os.Getenv("OPENAI_API_KEY")),
)

model, err = anthropic.New(
    anthropic.WithModel("claude-sonnet-4-8"),
    anthropic.WithToken(os.Getenv("ANTHROPIC_API_KEY")),
)

// Anthropic models served through AWS Bedrock.
model, err = anthropic.NewBedrock(anthropic.WithAWSConfig(awsCfg))
```

The `openai` package also backs Azure, OpenRouter, Perplexity and
OpenAI-on-Bedrock — `openai.WithProvider` selects the dialect, and the
capability table keys off that same `ProviderType`. See
[`pkg/llmfactory/factory.go`](../pkg/llmfactory/factory.go) for the exact
option wiring per `api_type`.

## Portable single-turn inference

The optional `llms.InferenceModel` interface supplies `ValidateInference` and
`Infer` for presence-aware text requests with ordered blocks, raw schemas,
reasoning passthrough and request-level usage. OpenAI, Anthropic, GoogleAI, and
explicitly enabled Bedrock Converse implement it alongside the unchanged `Model`
interface. The new paths make one SDK attempt and do not change legacy
generation defaults.

Use the typed enums when constructing Go requests: `llms.InferenceRole*` roles,
`llms.Block*` block types, `llms.ToolChoice*` modes (via `InferenceToolChoice`),
`llms.Format*` output formats, `llms.Finish*` finish reasons and `llms.Effort*`
reasoning efforts. Reasoning blocks carry a summary in `Text` and provider state in
`Opaque`; `Source` names the router target that produced the state and is checked
by the router before replay. `InferenceRequest.Clone` is an explicit deep copy.

Connector obligations (validation, role and tool-result merging, finish-reason and
usage normalization, one attempt, reasoning mapping) are specified in
[router/DESIGN.md](../router/DESIGN.md) sections 3.1, 5 and 6; per-connector
restrictions are listed in [the router guide](router.md).

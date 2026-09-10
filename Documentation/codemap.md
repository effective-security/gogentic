# Code Map

A navigation index for automated agents and newcomers. It answers "which file
owns this concept" so you can open one file instead of grepping the tree.

Read [architecture.md](architecture.md) for *why* the layering looks like this.

## Module

- Module path: `github.com/effective-security/gogentic`
- Go: see `go.mod` (`go 1.27`)
- Build/test: `make generate` (mocks), `make test`, `make lint`
- Conventions: [`AGENTS.md`](../AGENTS.md) — `cockroachdb/errors` for all errors,
  `testify` `assert`/`require` in tests

## Entry points by task

| Task | Start here |
|------|-----------|
| Understand the agent loop | `assistants/assistant.go` → `Assistant.Run`, `Assistant.run`, `Assistant.executeToolCalls` |
| Add or change an assistant option | `assistants/options.go` → `Config`, `Config.GetCallOptions` |
| Understand tool execution and error handling | `assistants/assistant.go` → `executeToolCalls` |
| Write a tool | `tools/tools.go` (contract), `tools/tavily/tavily.go` (example) |
| Chain assistants | `assistants/assistant_tool.go` → `AssistantTool.CallAssistant` |
| Add a provider | `pkg/llmfactory/factory.go` → `CreateLLM` + a `new<Provider>` func; then `pkg/llms/llms.go` → `providerCapabilities` |
| Change model selection | `pkg/llmfactory/factory.go` → `GetModel`, `getModelByName`, `resolveDefault` |
| Change the request/response types | `pkg/llms/generatecontent.go` |
| Add a call option | `pkg/llms/options.go` → `CallOptions` + `WithXxx`; then `assistants/options.go` → `GetCallOptions` |
| Change how output schemas are produced | `pkg/schema/schema.go`, `pkg/schema/response_format.go` |
| Change how output is parsed | `encoding/defined.go`, `encoding/json/encoder.go` |
| Change the system prompt assembly | `assistants/assistant.go` → `GetSystemPrompt` |
| Change the skills prompt | `assistants/skills.go` → `promptTemplate`, `DefaultPromptProvider` |
| Change skill discovery | `skills/loader.go` → `loader.load`, `loadFolder`, `loadTar`, `parseSkillContent` |
| Add an MCP method | `mcp/server.go` → `Serve` (handler table) + a `handleXxx` |
| Add an MCP transport | `mcp/transport/transport.go` (interface), then a new subpackage |
| Add a metric | `pkg/metricskey/metricskey.go`, then emit it in `assistants/assistant.go` |
| Persist history differently | `store/store.go` (interface), `store/memory.go`, `store/redis.go` |

## Package index

### `assistants` — the agent loop
| Concept | Symbol | File |
|---------|--------|------|
| Assistant interface | `IAssistant` | `assistants.go` |
| Typed assistant | `TypeableAssistant[O]`, `Assistant[O]` | `assistants.go`, `assistant.go` |
| Construction | `NewAssistant[O]`, `WithName`, `WithDescription`, `WithTools`, `WithSkills` | `assistant.go` |
| Call input/output | `CallInput`, `Response` | `assistants.go` |
| The loop | `Run`, `run`, `executeToolCalls` | `assistant.go` |
| System prompt | `GetSystemPrompt` | `assistant.go` |
| Config and options | `Config`, `Option`, `WithXxx`, `GetCallOptions` | `options.go` |
| Limits | `DefaultMaxToolCalls`, `DefaultMaxMessages`, `DefaultMaxContentSize`, `DefaultMaxRetries` | `options.go` |
| Assistant as tool | `AssistantTool[I,O]`, `NewAssistantTool`, `CallAssistant` | `assistant_tool.go` |
| MCP integration | `IMCPAssistant`, `RegisterMCP`, `CallMCP` | `assistants.go`, `assistant.go` |
| Callback contract | `Callback` | `assistants.go` |
| Skills prompt | `DefaultPromptProvider` | `skills.go` |
| Fleet descriptions | `GetDescriptions`, `GetDescriptionsWithTools`, `MapAssistants` | `assistants.go` |

### `tools` — tool contract
| Concept | Symbol | File |
|---------|--------|------|
| Core contract | `ITool` | `tools.go` |
| Typed / MCP variants | `Tool[I,O]`, `IMCPTool`, `MCPTool[I]` | `tools.go` |
| Callback contract | `Callback` | `tools.go` |
| Registrator | `McpServerRegistrator` | `tools.go` |
| Prompt rendering | `Description`, `Descriptions`, `GetDescriptions` | `tools.go` |
| Example implementation | `tavily.Tool` | `tavily/tavily.go` |

### `chatmodel` — identity and I/O types
| Concept | Symbol | File |
|---------|--------|------|
| Chat identity | `ChatContext`, `NewChatContext`, `WithChatContext`, `GetChatContext`, `NewFromContext`, `SetChatID` | `chat_context.go` |
| Action tagging | `WithActionID`, `GetActionID` | `chat_context.go` |
| ID generation | `NewChatID` | `chat_context.go` |
| Content contract | `ContentProvider`, `InputParser` | `io.go` |
| Ready-made I/O types | `InputRequest`, `OutputResult`, `MCPInputRequest`, `BaseClarificationResult`, `IBaseResult` | `io.go` |
| String value type | `String`, `NewString` | `string.go` |
| Parser contract | `OutputParser[T]` | `model.go` |
| Sentinel errors | `ErrInvalidChatContext`, `ErrFailedUnmarshalInput`, `ErrFailedUnmarshalOutput` | `chat_context.go`, `model.go` |
| Few-shot pairs | `FewShotExample`, `FewShotExamples` | `model.go` |

### `encoding` — format instructions and parsing
| Concept | Symbol | File |
|---------|--------|------|
| Encoder contract | `SchemaEncoder`, `Validator`, `SchemaStreamEncoder` | `encoder.go` |
| Modes | `Mode`, `ModeJSON`, `ModeJSONSchema`, `ModeJSONSchemaStrict`, `ModeYAML`, `ModeTOML`, `ModePlainText`, `ModeDefault` | `encoder.go` |
| Encoder selection | `PredefinedSchemaEncoder` | `encoder.go` |
| Typed parser | `TypedOutputParser[T]`, `NewTypedOutputParser` | `defined.go` |
| Pass-through parser | `SimpleOutputParser`, `NewSimpleOutputParser` | `simple.go` |
| Back ends | `json.Encoder`, `yaml.Encoder`, `toml.Encoder`, `dummy.Encoder` | `json/`, `yaml/`, `toml/`, `dummy/` |

### `store` — message history
| Concept | Symbol | File |
|---------|--------|------|
| Store contract | `MessageStore`, `MessageStoreManager` | `store.go` |
| Chat metadata | `ChatInfo`, `Clone` | `store.go` |
| Key derivation | `GetTenantAndChatID` | `store.go` |
| Snapshot helper | `PopulateMemoryStore` | `store.go` |
| In-memory | `NewMemoryStore`, `NewMemoryStoreManager` | `memory.go` |
| Redis | `NewRedisStore`, `NewRedisStoreManager` (key layout in the file header) | `redis.go` |

### `callbacks` — observability hooks
| Concept | Symbol | File |
|---------|--------|------|
| Modes | `Mode`, `ModeDefault`, `ModeVerbose` | `callback.go` |
| Handlers | `Noop`, `Printer`, `PackageLogger`, `Fanout` | `callback.go` |
| Run recorder | `Scratchpad`, `NewScratchpad`, `StartRun`, `EndRun`, `RunStats` | `scratchpad.go` |
| Test hook | `TimeNowFn` | `scratchpad.go` |

### `skills` — Agent Skills
| Concept | Symbol | File |
|---------|--------|------|
| Types | `Skill`, `Skills`, `Frontmatter` | `skills.go` |
| Filtering | `Skills.Filter`, `Names`, `NamesEnum` | `skills.go` |
| Resources | `ListResources`, `LoadResources` | `skills.go` |
| Loader | `Loader`, `NewLoader`, `Config`, `AgentConfig`, `LoadConfig` | `loader.go` |
| Discovery internals | `loader.load`, `loadFolder`, `loadTar`, `parseSkillFile`, `parseSkillContent` | `loader.go` |
| Activation tool | `ActivateSkillTool`, `NewActivateSkillTool`, `ActivateSkillToolName` | `activate_skill_tool.go` |

### `mcp` — Model Context Protocol
| Concept | Symbol | File |
|---------|--------|------|
| Server | `Server`, `NewServer`, `Serve`, `RegisterTool/Prompt/Resource/ResourceTemplate` | `server.go` |
| Server options | `WithName`, `WithVersion`, `WithInstructions`, `WithPaginationLimit`, `WithProtocol` | `server.go` |
| Handler validation & schema | `validateToolHandler`, `validatePromptHandler`, `createJsonSchemaFromHandler` | `server.go` |
| Client | `Client`, `NewClient`, `NewClientWithInfo`, `Initialize`, `ListTools`, `CallTool`, `ListPrompts`, `GetPrompt`, `ListResources`, `ReadResource`, `Ping` | `client.go` |
| Content | `Content`, `NewTextContent`, `NewImageContent`, `NewTextResourceContent`, `NewBlobResourceContent`, `Annotations` | `content_api.go` |
| Responses | `ToolResponse`, `PromptResponse`, `ResourceResponse` | `tool_api.go`, `prompt_api.go`, `resource_api.go` |
| Transport contract | `Transport`, JSON-RPC frames | `transport/transport.go`, `transport/types.go` |
| Transports | `stdio`, `httptransport` (server + client + `http.Handler`), `sse`, `localtransport` | `transport/*/` |

### `pkg/llms` — provider abstraction
| Concept | Symbol | File |
|---------|--------|------|
| Model contract | `Model`, `Embedder` | `llms.go` |
| Providers | `ProviderType`, `ProviderOpenAI`, `ProviderAnthropic`, ... | `llms.go` |
| Capabilities | `Capability`, `providerCapabilities`, `ProviderCapabilities`, `ProviderType.Supports` | `llms.go` |
| Single prompt helper | `GenerateFromSinglePrompt` | `llms.go` |
| Messages | `Message`, `Messages`, `Role`, `MessageSource`, `MessageFromTextParts`, `MessageFromToolCalls`, `MessageFromToolResponse` | `generatecontent.go` |
| Content parts | `ContentPart`, `TextContent`, `ImageURLContent`, `BinaryContent`, `ToolCall`, `ToolCallResponse` | `generatecontent.go` |
| Responses | `ContentResponse`, `ContentChoice` | `generatecontent.go` |
| Usage | `Usage`, `UsageStats` | `generatecontent.go` |
| Call options | `CallOptions`, `CallOption`, `WithXxx` | `options.go` |
| Tool definitions | `Tool`, `FunctionDefinition`, `ToolChoice`, `WebSearchOptions` | `options.go` |
| Prompt caching | `PromptCachePolicy`, `PromptCacheBreakpoint`, `PromptCacheRequestPolicy`, `PromptCacheTTL`, `PromptCacheRetention` | `options.go` |
| Batch API | `Batcher`, `BatchRequest`, `BatchResult`, `BatchHandle`, `BatchStatus`, `ErrBatchNotReady` | `batch.go` |
| Prompt value | `PromptValue` | `prompts.go` |
| Serialization | message marshal/unmarshal | `marshaling.go` |

Provider implementations: `pkg/llms/openai` (also Azure, OpenRouter,
Perplexity, OpenAI-on-Bedrock), `pkg/llms/anthropic` (`New`, `NewBedrock`),
`pkg/llms/bedrock`, `pkg/llms/googleai`, `pkg/llms/cloudflare`. Wire protocol
details live under each provider's `internal/` directory.

### `pkg/llmfactory` — config to model
| Concept | Symbol | File |
|---------|--------|------|
| Factory contract | `Factory`, `ModelOptions` | `factory.go` |
| Construction | `Load`, `New`, `LoadConfig` | `factory.go`, `config.go` |
| Config types | `Config`, `ProviderConfig`, `OpenAIConfig`, `OrgConfig` | `config.go` |
| Model selection | `GetModel`, `getModelByType`, `getModelByName`, `resolveDefault`, `resolveModelNamePath`, `FindModel` | `factory.go`, `config.go` |
| Capability filtering | `supportsCapabilities`, `configProviderType` | `factory.go` |
| Provider construction | `CreateLLM`, `NewLLM` (overridable var), `newOpenAI`, `newAnthropic`, ... | `factory.go` |
| Options | `Options`, `WithHTTPClient`, `WithAWSConfigFactory`, `WithModelFilter`, `ModelFilterFunc` | `options.go` |
| Skills passthrough | `factory.Skills` | `factory.go` |

### `pkg/prompts` — templates
| Concept | Symbol | File |
|---------|--------|------|
| Contracts | `Formatter`, `MessageFormatter`, `FormatPrompter` | `prompts.go` |
| String template | `PromptTemplate`, `NewPromptTemplate` | `prompt_template.go` |
| Chat template | `ChatPromptTemplate`, `NewChatPromptTemplate` | `chat_prompt_template.go` |
| Message templates | `NewSystemMessagePromptTemplate`, `NewHumanMessagePromptTemplate`, `NewAIMessagePromptTemplate`, `NewGenericMessagePromptTemplate`, `MessagesPlaceholder` | `message_prompt_template.go` |
| Few-shot | `FewShotPrompt`, `NewFewShotPrompt`, `ExampleSelector` | `few_shot.go`, `example_selector.go` |
| Engines | `TemplateFormat`, `RenderTemplate`, `CheckValidTemplate` | `templates.go` |
| Prompt values | `StringPromptValue`, `ChatPromptValue` | `string_prompt.go`, `chat_prompt.go` |

### `pkg/schema` — JSON Schema
| Concept | Symbol | File |
|---------|--------|------|
| Reflection | `New` (type-cached), `JSONSchema`, `buildSchema` | `schema.go` |
| Function parameters | `Schema.Parameters`, `ToFunctionSchema`, `resolveRefs` | `schema.go` |
| Ad-hoc schemas | `FromAny`, `MustFromAny` | `schema.go` |
| Response formats | `NewResponseFormat`, `ResponseFormat`, `ResponseFormatJSONSchema`, `toOpenAISchema` | `response_format.go` |

### `pkg/llmutils` — helpers
| Concept | Symbol | File |
|---------|--------|------|
| Output cleaning | `CleanJSON`, `TrimBackticks`, `BytesTrimBackticks`, `StripComments`, `RemoveAllComments` | `utils.go` |
| Rendering | `ToJSON`, `ToJSONIndent`, `ToYAML`, `BackticksJSON`, `BackticksYAM`, `RenderToString`, `RenderFormat` | `utils.go` |
| Prompt helpers | `AddComment`, `MergeInputs`, `EnsureEndsWithNewline`, `ExtractTag` | `utils.go` |
| Message helpers | `PrintMessages`, `CountMessagesContentSize`, `FindLastUserQuestion` | `utils.go` |
| Images | `DownloadImageData` | `download.go` |

### `pkg/metricskey` — metric descriptors
All descriptors plus the `Metrics` slice live in `metricskey.go`.

### `mocks` — generated
`mocks/mockassitants`, `mocks/mockllms`, `mocks/mockllmfactory`,
`mocks/mocktools`. Generated by `make generate` from the `//go:generate`
directives in `assistants/assistants.go`, `tools/tools.go` and
`pkg/llmfactory/factory.go`. Do not edit by hand.

## Invariants to preserve when editing

1. **Every `tool_call_id` gets exactly one `RoleTool` message.** Providers
   reject histories where one is missing. `executeToolCalls` guarantees this
   even for failures and missing results.
2. **`ChatContext` is required for any assistant run.** Store keys, metrics
   tags and message provenance all derive from it.
3. **A tool error is data, not control flow.** Non-`ErrFailedUnmarshalInput`
   errors are serialized into the tool response so the model can recover; only
   the assistant's own guards abort a run.
4. **`Config` is copied per call.** `Config.Apply` must keep copying, or
   per-call options would leak across calls.
5. **`schema.New` results are cached by `reflect.Type`.** Returned `*Schema`
   values must be treated as immutable.
6. **`Message.WithSource` does not overwrite an existing source.** Sub-agent
   attribution survives as messages flow upward.
7. **Store `Add` must be atomic across the variadic set.** A partial write can
   split a tool-call pair.
8. **Tools run concurrently.** Anything reachable from `ITool.Call` must be
   goroutine-safe; so must callback handlers.
9. **`ProviderType.Supports` is an ANY check; `RequiredCapabilities` is an ALL
   check.** Do not swap one for the other.
10. **Errors use `cockroachdb/errors` and preserve sentinels** so callers can
    use `errors.Is`. See [`AGENTS.md`](../AGENTS.md).

## Test layout

Tests sit beside their code as `*_test.go`. Notable ones:

| File | Covers |
|------|--------|
| `assistants/assistants_test.go` | The loop with mocked models |
| `assistants/assistants_real_test.go` | End-to-end against live providers; **skipped by default** (`t.Skip` in `loadOpenAIConfigOrSkipRealTest`) |
| `assistants/assistant_tool_test.go` | Assistant-as-tool, including input parsing and MCP registration |
| `pkg/llmfactory/factory_test.go` | Model resolution, org overrides, capability filtering |
| `skills/loader_test.go` | Discovery from folders and tar archives |
| `mcp/server_test.go` | Registration, notifications, handler validation |

`Makefile` exports fake provider credentials, so unit tests run without
secrets. Provider integration tests are guarded by env vars or `t.Skip`.

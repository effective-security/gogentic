# Findings

Active defects and verification gaps. Use stable IDs in code comments and
reviews; IDs are never reused. Update or remove an item in the same change that resolves it. Larger contract
or architecture work belongs in [ROADMAP.md](ROADMAP.md).

Open = actionable work; Needs Decision = a compatibility or operational
choice. Source symbols are stable references; line numbers are omitted.

## Index

| ID              | Package / location                                                       | Title                                                                          | Type     | Severity | Status         |
| --------------- | ------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | -------- | -------- | -------------- |
| [F-022](#f-022) | `pkg/llms/anthropic` · `ToTools`                                         | `web_search` tools are sent as web fetch tools                                 | Bug      | Medium   | Needs Decision |
| [F-040](#f-040) | `router/dialect/openai` · `decodeReasoningDetail`, `decodeReasoningItem` | Forged reasoning error names `reasoning`, not the field path                   | Bug      | Low      | Open           |
| [F-041](#f-041) | `pkg/llms/anthropic` · `convertToAnthropicSchema`                        | Drops `$ref` and `examples`; always emits `type`                               | Bug      | Low      | Open           |
| [F-042](#f-042) | `pkg/llms/anthropic` · `LLM.GetProviderType`                             | Bedrock-hosted models report `ProviderAnthropic`                               | Bug      | Low      | Needs Decision |
| [F-043](#f-043) | `pkg/llms/openai` · `toResponsesText`                                    | Schema serialization errors are ignored; schema silently dropped               | Bug      | Low      | Open           |
| [F-044](#f-044) | `pkg/llms/openai` · `newClient`                                          | Azure model checks are skipped when an API version is set                      | Bug      | Low      | Open           |
| [F-045](#f-045) | `pkg/llms/openai/internal/openaiclient` · `Client.createEmbedding`       | Client embedding model overrides the request's model                           | Bug      | Low      | Open           |
| [F-049](#f-049) | `mcp` · `EmbeddedResource.UnmarshalJSON`                                 | Error text ends in `<nil>`                                                     | Bug      | Low      | Open           |
| [F-050](#f-050) | `pkg/llms/googleai` · `New`                                              | Returns a non-nil client with a nil inner client on error                      | Bug      | Low      | Open           |
| [F-051](#f-051) | `store` · `inMemory.GetChatInfo`                                         | Unknown tenant and unknown chat behave differently, and differently from Redis | Bug      | Low      | Needs Decision |
| [F-053](#f-053) | `pkg/llms/anthropic` · `TestNew`, `TestNewWithEnvironmentVariable`       | Parallel tests mutate process environment                                      | Test     | Low      | Open           |
| [F-054](#f-054) | `pkg/prompts` · `interpolateJinja2`                                      | Jinja2 templates can `{% include %}` any local file                            | Security | Medium   | Needs Decision |
| [F-055](#f-055) | `pkg/llms/anthropic` · `GenerateMessagesContent`, `HandleAIMessage`      | Thinking blocks are not kept in the message history                            | Bug      | Medium   | Needs Decision |
| [F-056](#f-056) | `mcp` · `Server.Serve`; `httptransport.HTTPTransport.Start`              | `Serve` blocks with the HTTP transport; documented start sequence is wrong     | Bug      | Medium   | Needs Decision |
| [F-057](#f-057) | `mcp/transport/httptransport` · `HTTPTransport.Send`, `handleRequest`    | `Send` ignores cancellation; `errorHandler` read without the lock              | Race     | Low      | Open           |
| [F-059](#f-059) | `assistants` · `executeToolCalls`                                        | Provider-native tool calls are run as missing tools, and the next turn fails   | Bug      | Low      | Needs Decision |
| [F-060](#f-060) | `pkg/llms/bedrock/internal/bedrockclient` · `anthropicChoices`           | Usage is counted twice when a message has text and tool calls                  | Bug      | Medium   | Open           |
| [F-061](#f-061) | `pkg/schema` · `JSONSchema`                                              | Writes the global `jsonschema.Version` on every call                           | Race     | Low      | Open           |
| [F-062](#f-062) | `pkg/schema` · `New`; `mcp` · `RegisterTool`                             | Exported chan, func or complex fields panic in the reflector                   | Crash    | Low      | Open           |
| [F-063](#f-063) | `mcp/transport/...` · server `HandleMessage`, `Close`                    | Server transports misread unknown messages; some `Close` methods race          | Race     | Low      | Open           |

## Details

### F-022

**`ToTools` maps `web_search` to web fetch tools.** A tool with
`Type == "web_search"` becomes `WebFetchTool20250910` or
`WebFetchTool20260209`, not a web search tool. Decide whether fetch was
intended.

### F-040

**Forged reasoning error parameter.** The OpenAI codecs report the parameter
as `"reasoning"`; the Anthropic codec reports the field path, for example
`messages[1].content[0].data`.

### F-041

**`convertToAnthropicSchema` loses keywords.** It drops `$ref` and
`examples`, and always emits `"type"`, even when empty.

### F-042

**Provider type of Bedrock-hosted Anthropic models.** Models from
`anthropic.NewBedrock` report `ProviderAnthropic`, not
`ProviderAnthropicBedrock`, so capability checks use the Anthropic table.
Changing it affects callers that match on the provider type.

DECISION: fix

### F-043

**Schema errors ignored in `toResponsesText`.** Errors from `json.Marshal`
and `json.Unmarshal` are discarded; a schema that cannot be serialized is
sent as `nil` with no error.

- Repro: `ResponseFormat{Type: "json_schema"}` whose schema has `Default: func(){}`.

### F-044

**Azure checks skipped with an API version.** In `newClient`, the "model
required" and "embedding model required" checks run only when
`apiVersion == ""`.

- Repro: `New(WithProvider(Azure), WithToken(x), WithModel(""), WithAPIVersion("2024-10-21"))`
  succeeds and chat goes to `/openai/deployments//chat/completions`.

### F-045

**Client embedding model overrides the request.** When the client has no chat
`Model`, `Client.createEmbedding` replaces the caller's `r.Model` with
`c.EmbeddingModel`, which may be empty and then falls back to ada-002.
`LLM.CreateEmbedding` always passes `EmbeddingModel`, so only direct callers
of the internal client are affected.

- Pinned: the "client embedding model overrides the request when the client
  has no chat model" case of `TestCreateEmbedding`
  (`openaiclient/openaiclient_test.go`) asserts the current behavior; update
  it with the fix.

### F-049

**Unhelpful embedded resource error.** When neither `text` nor `blob` is
present, `EmbeddedResource.UnmarshalJSON` returns
"failed to unmarshal embedded resource: <nil>". `Content.UnmarshalJSON` now
decodes embedded resources with `decodeResourceContents`, which names the
missing field; `EmbeddedResource.UnmarshalJSON` could reuse it.

### F-050

**`googleai.New` returns a value on error.** It returns a non-nil
`*GoogleAI` whose client is nil when `genai.NewClient` or the transport wrap
fails.

### F-051

**`GetChatInfo` semantics differ.** The in-memory store returns `(nil, nil)`
for an unknown tenant but a "chat not found" error for an unknown chat; the
Redis store creates the chat. Decide the contract and align both stores.

### F-053

**Parallel tests mutate the environment.** The "missing token" case of
`TestNew` and `TestNewWithEnvironmentVariable` in
`pkg/llms/anthropic/anthropicllm_test.go` call `os.Setenv`/`os.Unsetenv`
inside `t.Parallel()` tests, racing with other parallel tests that read the
environment. Use `t.Setenv` in non-parallel tests.

### F-054

**Jinja2 templates can read local files.** `interpolateJinja2` renders with
gonja's default environment, whose filesystem loader resolves
`{% include %}` paths relative to the working directory and accepts
absolute paths. `{% include "/etc/hostname" %}` renders the file, and a
failed include echoes the file's contents in the error. Both gonja v1 and v2
behave this way. Harmless for templates written by developers; a file
disclosure wherever template text comes from users. Decide whether
`TemplateFormatJinja2` should support includes; if not, render with an
environment whose loader refuses every path.

### F-055

**Anthropic thinking blocks are not kept in the history.**
`GenerateMessagesContent` returns thinking text in `ReasoningContent` only,
and `HandleAIMessage` has no message part to send thinking blocks (with
their `signature`, or `redacted_thinking` data) back. The Claude API
expects thinking blocks passed back unchanged in later turns; dropping them
breaks the turn, notably in a tool loop with thinking on, which is every
call on models where thinking cannot be disabled. Not verified against the
live API here. Fixing it needs a new `llms` content part carried through
`MessageFromToolCalls`, the stores and `HandleAIMessage`, ignored by other
providers.

### F-056

**`Serve` blocks with the HTTP transport.** `Server.Serve` connects the
protocol, and `Protocol.Connect` calls the transport's `Start`
synchronously. `HTTPTransport.Start` runs `ListenAndServe`, so `Serve` does
not return while the server runs; it also binds the transport's own
address (":8080" by default). The `httptransport` package example, which
calls `Serve` and then mounts the transport on the caller's mux, never
reaches the mux. The `Documentation/mcp.md` example calls `Serve` and then
`Start`: for HTTP the `Start` is never reached, and for stdio, whose `Start`
returns at once, the second `Start` fails with "already started". Decide the
transport lifecycle (start in `Connect` without blocking, or leave starting
to the caller) and fix the examples to match.

### F-057

**HTTP transport cancellation and error handler.**
`HTTPTransport.Send` does not stop when its context is cancelled (the local
transport does), and `handleRequest` reads `errorHandler` without holding
`t.mu` while `SetErrorHandler` writes it under the lock.

### F-059

**Provider-native tool calls go through the tool loop.**
`Assistant.executeToolCalls` treats every tool call as a function to run. A
call without `FunctionCall` (a provider-native tool such as `web_search`) is
looked up as a tool named after its type, counted and reported as not found
(`OnToolNotFound`), and answered with a `RoleTool` message. The next request
then fails: OpenAI Chat Completions, Anthropic, Bedrock and Google AI reject
the history with an error wrapping `llms.ErrMissingFunctionCall`, and the
OpenAI Responses API leaves the call out but still sends its
`function_call_output`. Latent: current providers always set `FunctionCall`.

- Covered by `Test_Assistant_Run_ToolCallWithoutFunctionCall`, whose mock
  model accepts the history that real providers reject.
- Decide how native calls are kept in the history (skipped, or carried as a
  provider-specific part; see [F-055](#f-055)) before a provider returns them.

### F-060

**Bedrock Anthropic usage is doubled.** `anthropicChoices` (streaming and
non-streaming `InvokeModel`) returns a text choice and a tool-call choice for
one message, and each carries the whole message usage.
`ContentResponse.Usage`, which the assistant uses for token metrics and
`Response.Usage`, sums the choices, so a turn with text and tool calls counts
its tokens twice. The native `pkg/llms/anthropic` provider returns one merged
choice. By reading; found while fixing F-020.

### F-061

**`JSONSchema` writes a package global.** It sets `jsonschema.Version` on
every call, while every invopop reflector reads it, including the MCP
server's. `New` serializes its own calls with a mutex, but a direct
`JSONSchema` call, or `New` running while the MCP server reflects a tool,
races under `-race`. Setting the version once, at package init, would remove
the write. Reported while fixing F-058.

### F-062

**Reflector panics on unsupported kinds.** invopop calls
`panic("unsupported type ...")` for an exported chan, func or complex field,
and panics on a nil `JSONSchemaAlias()` result. `schema.New` and
`Server.RegisterTool` do not recover, so such an arguments type panics instead
of returning an error. Reported while fixing F-058.

### F-063

**Remaining MCP transport gaps.** The server transports (stdio
`deserializeMessage`, `localtransport.Transport.HandleMessage`,
`HTTPTransport.handleMessage`) still treat an unrecognised JSON object such as
`{}` as an error response with ID 0; `transport.ParseMessage` classifies it
as invalid. `sse.SSETransport.HandleMessage` reads its handlers without the
lock, and the `Close` methods of `LocalMcpClientTransport`,
`HTTPClientTransport`, `localtransport.Transport` and `HTTPTransport` read the
close handler without the lock and run it on every call. By reading; found
while fixing F-034 and F-035.

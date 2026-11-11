# Findings

Active defects and verification gaps. Use stable IDs in code comments and
reviews; IDs are never reused. Update or remove an item in the same change that resolves it. Larger contract
or architecture work belongs in [ROADMAP.md](ROADMAP.md).

Open = actionable work; Needs Decision = a compatibility or operational
choice. Source symbols are stable references; line numbers are omitted.

## Index

| ID  | Package / location | Title | Type | Severity | Status |
| --- | ------------------ | ----- | ---- | -------- | ------ |
| [F-011](#f-011) | `assistants` · `Assistant.Run` | Retry after unparsable output loses the question, overwrites the input, keeps tools | Bug | Medium | Open |
| [F-014](#f-014) | `pkg/llmfactory` · `newOpenAIBedrock`, `newAnthropicBedrock` | Factory HTTP client is ignored for the Bedrock-hosted OpenAI and Anthropic clients | Bug | Medium | Open |
| [F-015](#f-015) | `pkg/llmfactory` · Cloudflare provider | No way to configure the Cloudflare account ID | Bug | Medium | Needs Decision |
| [F-016](#f-016) | `pkg/llms/googleai` · `convertCandidates` | Tool calls accumulate across candidates | Bug | Medium | Open |
| [F-017](#f-017) | `pkg/llms/googleai` · `convertParts`; `pkg/llmutils` · `DownloadImageData` | Image URL parts are sent with MIME type `png` instead of `image/png` | Bug | Medium | Open |
| [F-018](#f-018) | `pkg/llms/bedrock` · `LLM.Infer` | Transport failures report status 0 instead of 502 | Bug | Medium | Open |
| [F-020](#f-020) | `pkg/llms/bedrock/internal/bedrockclient` · `parseStreamingCompletionResponse` | Streaming drops tool calls and total token usage | Bug | Medium | Open |
| [F-021](#f-021) | `pkg/llms/anthropic` · `GenerateStreamingContent` | Streamed tool call without input has empty arguments that fail on the next turn | Bug | Medium | Open |
| [F-022](#f-022) | `pkg/llms/anthropic` · `ToTools` | `web_search` tools are sent as web fetch tools | Bug | Medium | Needs Decision |
| [F-024](#f-024) | `pkg/llms/cloudflare` · `LLM.GenerateContent` | Streaming returns empty content | Bug | Medium | Open |
| [F-025](#f-025) | `pkg/llms/cloudflare/internal/cloudflareclient` · `Client.CreateEmbedding` | Embeddings call the chat model; `WithEmbeddingModel` is ignored | Bug | Medium | Open |
| [F-026](#f-026) | `pkg/llms/cloudflare/internal/cloudflareclient` · `Client.GenerateContent` | Streaming error handling loses the error | Bug | Medium | Open |
| [F-027](#f-027) | `router/dialect` · `OpenOpaque` | Accepts a JSON `null` provider state | Bug | Medium | Open |
| [F-032](#f-032) | `mcp` · `ToolResponse` | Client cannot see the server's `isError` flag | Bug | Medium | Open |
| [F-034](#f-034) | `mcp/transport/{localtransport,httptransport}` · client `Send` | Notifications and requests in a response are dispatched as errors | Bug | Medium | Open |
| [F-035](#f-035) | `mcp/internal/protocol` · `Protocol.handleClose`; stdio and SSE `Close` | Callbacks and channel sends run while holding a lock | Race | Medium | Open |
| [F-036](#f-036) | `skills` · `loader.loadFolder` | `Strict` mode does not apply to agent folders | Bug | Medium | Open |
| [F-037](#f-037) | `skills` · `AgentConfig.Paths` | Per-agent paths are never searched | Bug | Medium | Open |
| [F-040](#f-040) | `router/dialect/openai` · `decodeReasoningDetail`, `decodeReasoningItem` | Forged reasoning error names `reasoning`, not the field path | Bug | Low | Open |
| [F-041](#f-041) | `pkg/llms/anthropic` · `convertToAnthropicSchema` | Drops `$ref` and `examples`; always emits `type` | Bug | Low | Open |
| [F-042](#f-042) | `pkg/llms/anthropic` · `LLM.GetProviderType` | Bedrock-hosted models report `ProviderAnthropic` | Bug | Low | Needs Decision |
| [F-043](#f-043) | `pkg/llms/openai` · `toResponsesText` | Schema serialization errors are ignored; schema silently dropped | Bug | Low | Open |
| [F-044](#f-044) | `pkg/llms/openai` · `newClient` | Azure model checks are skipped when an API version is set | Bug | Low | Open |
| [F-045](#f-045) | `pkg/llms/openai/internal/openaiclient` · `Client.createEmbedding` | Client embedding model overrides the request's model | Bug | Low | Open |
| [F-049](#f-049) | `mcp` · `EmbeddedResource.UnmarshalJSON` | Error text ends in `<nil>` | Bug | Low | Open |
| [F-050](#f-050) | `pkg/llms/googleai` · `New` | Returns a non-nil client with a nil inner client on error | Bug | Low | Open |
| [F-051](#f-051) | `store` · `inMemory.GetChatInfo` | Unknown tenant and unknown chat behave differently, and differently from Redis | Bug | Low | Needs Decision |
| [F-053](#f-053) | `pkg/llms/anthropic` · `TestNew`, `TestNewWithEnvironmentVariable` | Parallel tests mutate process environment | Test | Low | Open |
| [F-054](#f-054) | `pkg/prompts` · `interpolateJinja2` | Jinja2 templates can `{% include %}` any local file | Security | Medium | Needs Decision |
| [F-055](#f-055) | `pkg/llms/anthropic` · `GenerateMessagesContent`, `HandleAIMessage` | Thinking blocks are not kept in the message history | Bug | Medium | Needs Decision |
| [F-056](#f-056) | `mcp` · `Server.Serve`; `httptransport.HTTPTransport.Start` | `Serve` blocks with the HTTP transport; documented start sequence is wrong | Bug | Medium | Needs Decision |
| [F-057](#f-057) | `mcp/transport/httptransport` · `HTTPTransport.Send`, `handleRequest` | `Send` ignores cancellation; `errorHandler` read without the lock | Race | Low | Open |
| [F-058](#f-058) | `pkg/schema` · `JSONSchema`, `New`; `mcp` · `argumentsSchema` | A recursive type overflows the stack | Crash | Medium | Open |
| [F-059](#f-059) | `assistants` · `executeToolCalls` | Provider-native tool calls are run as missing tools, and the next turn fails | Bug | Low | Needs Decision |

## Details

### F-011

**Output-parse retry in `Assistant.Run`.** On `chatmodel.ErrFailedUnmarshalOutput`
the retry sets `input.Input = "Return the response in JSON format as requested."`
and `cfg.Tools = nil`, then calls `run` again.

- Without a message store, the history is rebuilt from scratch, so the model
  receives only the system prompt and the retry text; the original question is lost.
- `input` is the caller's `*CallInput`; after `Run` its `Input` holds the retry text.
- `cfg.Tools = nil` does not remove the assistant's function tools, which
  `run` adds again. Provider-native tools are dropped only through a map
  shared by `c := *cfg`, so behavior differs by tool kind.
- Covered with a store by `Test_Assistant_Run_RetryOnUnparsableOutput`.

### F-014

**Factory HTTP client ignored for Bedrock-hosted OpenAI and Anthropic.**
`newOpenAIBedrock` and `newAnthropicBedrock` copy the factory's `WithHTTPClient`
only into `aws.Config.HTTPClient`, which neither client uses for requests, so
proxies, timeouts and instrumentation are bypassed.

- `OPENAI_BEDROCK`: reproduced against a local server; 0 calls on the
  factory client, 1 on the default client.
- `ANTHROPIC_BEDROCK`: by reading; the SDK's `bedrock.WithConfig` ignores
  `cfg.HTTPClient`.
- Native `BEDROCK` uses the factory client.

### F-015

**No Cloudflare account ID in factory configuration.** The factory cannot set
`cloudflare.WithAccountID`, so the endpoint becomes
`<base_url>//ai/run/<model>`. Decide where the account ID goes in
`ProviderConfig` (a new field, or a `base_url` that already ends in the
account).

### F-016

**Gemini tool calls accumulate across candidates.** `convertCandidates`
declares `toolCalls` outside the candidate loop, so each choice also carries
the tool calls of every earlier candidate.

- Repro: two candidates with one `FunctionCall` each; `Choices[1].ToolCalls`
  has 2 entries.

### F-017

**Wrong MIME type for image URLs sent to Gemini.** `convertParts` uses the
type from `llmutils.DownloadImageData`, which returns only the subtype, so
`inlineData.mimeType` is `"png"` instead of `"image/png"`.

### F-018

**Bedrock `Infer` reports status 0 for transport failures.** The SDK wraps
send failures in `*smithyhttp.ResponseError` whose placeholder response has
`StatusCode` 0; `errors.As` succeeds and replaces the intended 502.

- Repro: `BaseEndpoint: "https://127.0.0.1:1"`, call `Infer`;
  `InferenceError.Status == 0`.

### F-020

**Bedrock Anthropic streaming drops tool calls and total usage.**
`parseStreamingCompletionResponse` ignores `tool_use` content blocks and
`input_json_delta` events, so tool calls are lost when streaming, and it never
sets `Usage.TotalTokens` (the non-streaming path does).

### F-021

**Streamed Anthropic tool call without input.** When a streamed `tool_use`
block receives no `input_json_delta`, `GenerateStreamingContent` leaves
`Arguments == ""`; the non-streaming path gives `"{}"`. Sending that tool
call back in the history fails in `HandleAIMessage` with
"failed to unmarshal tool call arguments".

### F-022

**`ToTools` maps `web_search` to web fetch tools.** A tool with
`Type == "web_search"` becomes `WebFetchTool20250910` or
`WebFetchTool20260209`, not a web search tool. Decide whether fetch was
intended.

### F-024

**Cloudflare streaming returns empty content.** With a streaming function,
`LLM.GenerateContent` returns `Choices[0].Content == ""` because streamed
chunks are never collected into the response.

### F-025

**Cloudflare embeddings use the chat model.** `Client.CreateEmbedding` posts
to the chat model's endpoint; the embedding model passed to `NewClient` via
`WithEmbeddingModel` is stored and never used.

### F-026

**Cloudflare streaming error handling.** In `Client.GenerateContent` with
streaming on:

- A 400 with `{"errors":[{"message":"bad"}]}` produces an error with an empty
  message; the scanner has already consumed the body before `ReadAll`.
- The status is checked only after the first line parses as JSON, so a
  non-JSON error page returns a JSON syntax error.
- `scanner.Err()` is ignored; a read error or a line over 512000 bytes ends
  the call with an empty response and a nil error.

### F-027

**`OpenOpaque` accepts a `null` state.** The length and `json.Valid` checks
pass for `{"s":"a","d":null}`, so a client can forge a reasoning block whose
provider state is `null`; history validation accepts it and it is forwarded
to the named target.

### F-032

**Client cannot see tool errors.** `ToolResponse` has no `IsError` field, so
`Client.CallTool` drops the server's `isError` flag.

### F-034

**Client transports misread notifications and requests.**
`LocalMcpClientTransport.Send` and `HTTPClientTransport.Send` decode any JSON
object as `BaseJSONRPCError`, so a notification or request in a response
body is dispatched as an error message with ID 0.

### F-035

**Lock-held callbacks.** `Protocol.handleClose` holds `p.mu` while sending
into response channels (blocking if a response is already buffered) and
while calling `OnClose`. `StdioServerTransport.Close` and
`SSEServerTransport.Close` call their close handler while holding their own
mutex. A handler that calls back into the transport deadlocks.

### F-036

**`Strict` mode ignores agent folders.** `loader.loadFolder` errors for
`.<agent>/` folders are logged and skipped, so `NewLoader` returns no error
even though `Config.Strict` is documented to return one.

- Repro: `Strict: true` with `dir/.a/bad/SKILL.md` that has no description;
  `err == nil`.

### F-037

**`AgentConfig.Paths` is never read.** Only `Config.Paths` is searched, so
documented per-agent extra paths have no effect. `testdata/skills.yaml` sets
`agents.agent-foo.paths`, which is ignored.

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

### F-058

**Recursive types overflow the stack.** `JSONSchema` reflects with
`DoNotReference`, so the reflector inlines every struct and never stops on a
type that contains itself. `schema.New` on
`type Node struct { Children []Node }` ends in `fatal error: stack overflow`,
which `recover` cannot catch. The MCP server's reflector has the same
setting, so registering a tool with such an argument type crashes too.
Every typed tool and assistant input goes through `New`.

- Fix options: reflect with references and inline them with
  `ToFunctionSchema`, which keeps a recursive `$ref` (the output then needs
  `$defs`); or detect the cycle in the Go type before reflecting and return an
  error from `New`.

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


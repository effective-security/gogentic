# CODING GUIDELINES

## Go Code

### Errors

- Use `github.com/cockroachdb/errors` for all error creation and wrapping.
- Wrap all external errors (from DB calls, cloud SDKs, queue operations,
  serialization, filesystem, generated helpers, etc.) using either:
  - `errors.WithMessage(err, "failed to initialize mcp client")`, for static context strings.
  - `errors.Wrapf(err, "invalid %s for asset %s", LabelOpenPorts, resourceID)`, when context includes dynamic values.
- Never ignore errors from serialization, DB calls, queue operations, cloud SDK
  calls, generated helpers, or filesystem operations.
- Preserve sentinel errors used by callers. For example, invalid SQS payloads
  should still be detectable with `errors.Is(err, awsprov.ErrInvalidSQSMessage)`.
- Wrap errors with context that identifies the failed operation without losing
  the original cause.
- Keep error strings accurate after refactors. Do not leave stale component
  names, old interface names, or copy-pasted action names in runtime errors.
- Map not-found cases explicitly when the response type already supports a
  not-found path. Do not collapse expected not-found errors into generic
  "failed to get ..." responses.
- Keep user-facing and LLM/tool-facing errors sanitized, but specific enough to
  be useful. Put internal diagnostics in server-side logs.

### Tests

- Build tests using `assert` and `require` pattern.
- Assert exact behavior, including key absence versus empty values and the real
  `err` from the call being tested.
- Keep test setup and cleanup trustworthy: fixtures should match their comments,
  cleanup should run before resources close, and tests should actually execute
  in CI.
- Match mocks on meaningful request fields such as S3 bucket/key/prefix or queue
  URL instead of relying on indistinguishable `gomock.Any()` call order.

### Tools

- `make generate` : generate mocks on updated interfaces
- `make test` : test entire project
- `make lint` : final check

### Documentation

- Every package has a `doc.go` with a package comment and, where the package
  has a non-obvious entry point, a short usage example.
- Document all exported types, functions, interfaces and interface methods.
  Say what a symbol is _for_, not just what it is named.
- Keep code samples in `doc.go` and under `Documentation/` compiling against
  the current API. A wrong sample is worse than no sample.
- When behavior changes, update the area document under `Documentation/` in the
  same change, and re-check `Documentation/codemap.md` if you added, moved or
  renamed an exported symbol.

# REPOSITORY MAP

Start here instead of grepping the tree.

- **[`Documentation/codemap.md`](Documentation/codemap.md)** — the navigation
  index: which file owns which concept, entry-point symbols per package, the
  invariants the agent loop depends on, and the test layout.
- **[`Documentation/README.md`](Documentation/README.md)** — area documents
  (architecture, assistants, tools, orchestration, LLM factory, providers,
  structured output, prompts, memory, skills, MCP, observability).
- **[`README.md`](README.md)** — high-level overview and quick-start samples.

### Where things live

| Area                                         | Package                                   | Read first                                         |
| -------------------------------------------- | ----------------------------------------- | -------------------------------------------------- |
| Agent loop, tool calling, options            | `assistants`                              | `assistants/assistant.go`, `assistants/options.go` |
| Tool contract                                | `tools` (+ `tools/tavily` as the example) | `tools/tools.go`                                   |
| Multi-agent delegation                       | `assistants`                              | `assistants/assistant_tool.go`                     |
| Provider abstraction, messages, capabilities | `pkg/llms`                                | `pkg/llms/llms.go`, `pkg/llms/generatecontent.go`  |
| Config → model, per-org routing              | `pkg/llmfactory`                          | `pkg/llmfactory/factory.go`                        |
| Typed output, JSON Schema                    | `encoding`, `pkg/schema`                  | `encoding/defined.go`, `pkg/schema/schema.go`      |
| Prompt rendering                             | `pkg/prompts`                             | `pkg/prompts/prompt_template.go`                   |
| Chat identity, I/O types                     | `chatmodel`                               | `chatmodel/chat_context.go`, `chatmodel/io.go`     |
| Message history                              | `store`                                   | `store/store.go`                                   |
| Agent Skills                                 | `skills`                                  | `skills/loader.go`                                 |
| MCP server/client/transports                 | `mcp`, `mcp/transport/...`                | `mcp/server.go`, `mcp/client.go`                   |
| Callbacks, run stats                         | `callbacks`                               | `callbacks/callback.go`, `callbacks/scratchpad.go` |
| Metric descriptors                           | `pkg/metricskey`                          | `pkg/metricskey/metricskey.go`                     |
| Generated mocks (do not edit)                | `mocks/...`                               | regenerate with `make generate`                    |

### Non-obvious behavior worth knowing before editing

1. Every `tool_call_id` must receive exactly one `RoleTool` message; providers
   reject histories where one is missing.
2. `chatmodel.ChatContext` is required for any assistant run — store keys,
   metric tags and message provenance all derive from it.
3. Tool errors other than `chatmodel.ErrFailedUnmarshalInput` are serialized
   into the tool response so the model can recover; they do not abort the run.
4. `assistants.Config` is copied per call by `Config.Apply`; per-call options
   must never leak into the assistant.
5. `pkg/schema.New` caches by `reflect.Type` — treat returned `*Schema` values
   as immutable.
6. Tools requested in one LLM turn run concurrently, so tools and callback
   handlers must be goroutine-safe.
7. `llms.ProviderType.Supports` is an _any-of_ check;
   `llmfactory.ModelOptions.RequiredCapabilities` is an _all-of_ check.

The full list with file references is in
[`Documentation/codemap.md`](Documentation/codemap.md#invariants-to-preserve-when-editing).

# Post Work

If new packages, methods, interfaces are added or changed, make sure the documentation stays up to date in sync with applied changes.

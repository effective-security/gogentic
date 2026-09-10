# Memory and Chat Context

Two pieces work together:

- **`chatmodel.ChatContext`** — the identity of the current conversation
  (user, org, chat, run), carried on the `context.Context`.
- **`store.MessageStore`** — the conversation history, keyed by that identity.

Sources: [`chatmodel/chat_context.go`](../chatmodel/chat_context.go),
[`store/`](../store).

## ChatContext

Every `Assistant.Run` requires a `ChatContext`; without one it fails with
`chatmodel.ErrInvalidChatContext`.

```go
// userID is required (NewChatContext panics on an empty one).
// An empty chatID is generated; appData is arbitrary immutable payload.
chatCtx := chatmodel.NewChatContext("user-123", "", nil)
ctx := chatmodel.WithChatContext(context.Background(), chatCtx)
```

```go
type ChatContext interface {
    GetUserID() string
    GetOrgID() string
    SetOrgID(string)
    GetChatID() string
    SetChatID(string)
    GetRunID() string
    SetRunID(string)
    AppData() any
    GetMetadata(key string) (any, bool)
    SetMetadata(key string, value any)
}
```

| Field | Lifetime | Used for |
|-------|----------|----------|
| `UserID` | the user | store key (tenant) |
| `OrgID` | the tenant | store key prefix, metrics `org` tag, `llmfactory` per-org model overrides |
| `ChatID` | one conversation, across many runs | store key |
| `RunID` | one `Run` | `MessageSource` provenance |
| `AppData` | as set | your own payload; read-only via `AppData()` |
| Metadata | as set | ad-hoc values, backed by `sync.Map` (safe for concurrent use) |

`NewChatContext` generates both `ChatID` (when empty) and `RunID` using a flake
ID generator. Set `OrgID` explicitly for multi-tenant deployments — it is what
routes both model selection and metrics:

```go
chatCtx.SetOrgID("1000")
```

### Action IDs

`chatmodel.WithActionID(ctx, "extract")` tags a step of a multi-step flow. The
ID lands in each message's `MessageSource`, which makes stored transcripts and
[scratchpad reports](observability.md#scratchpad) readable when several agents
share one run.

```go
ctx = chatmodel.WithActionID(ctx, "classify")
```

### Crossing goroutine boundaries

Use `NewFromContext` to keep the chat identity while dropping cancellation and
deadlines — the right move when handing work to a background worker:

```go
go func(bg context.Context) {
    _, _ = agent.Call(bg, &assistants.CallInput{Input: followUp})
}(chatmodel.NewFromContext(ctx))
```

`chatmodel.SetChatID(ctx, id)` mutates the `ChatContext` already on the context
and returns `ErrInvalidChatContext` if there isn't one. This is how
`Assistant.CallMCP` honors the `chatID` an MCP client sends.

Note that `ChatContext` is a pointer-backed interface, so `SetChatID`,
`SetRunID` and `SetOrgID` mutate the shared value: every goroutine holding that
context sees the change. Create a fresh `ChatContext` per concurrent
conversation rather than re-pointing one.

## Message stores

```go
type MessageStore interface {
    Messages(ctx context.Context) []llms.Message
    Add(ctx context.Context, msgs ...llms.Message) error
    Reset(ctx context.Context) error

    UpdateChat(ctx context.Context, title string, metadata map[string]any, tags []string) (*ChatInfo, error)
    ListChatIDs(ctx context.Context) ([]string, error)
    GetChatInfo(ctx context.Context, id string, withMessages bool) (*ChatInfo, error)
}
```

Every method derives its key from the context via `store.GetTenantAndChatID`,
which returns `orgID + ":" + userID` (or just `userID` when `OrgID` is empty)
as the tenant, plus the chat ID. So the same store instance serves all tenants
safely.

### In-memory

```go
memstore := store.NewMemoryStore()

agent := assistants.NewAssistant[Answer](fac, sysPrompt,
    assistants.WithMessageStore(memstore),
)
```

Process-local, locked per tenant. Ideal for tests, single-process tools, and
sub-agents that only need history within one run.

### Redis

```go
client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
redisStore := store.NewRedisStore(client, "myapp")
```

Key layout (`prefix` = `"myapp"`):

```
/myapp/chatstore/<tenantID>/messages/<chatID>   messages (list)
/myapp/chatstore/<tenantID>/info/<chatID>       ChatInfo metadata
/myapp/chatstore/<tenantID>/chats               set of chat IDs
```

`Reset` deletes both keys and removes the chat from the tenant's set, in one
pipeline.

### Chat metadata

```go
info, err := redisStore.UpdateChat(ctx, "Incident 4711 triage",
    map[string]any{"ticket": "INC-4711"},
    []string{"incident", "p1"})
```

Empty title, nil metadata and empty tags are each skipped rather than clearing
the stored value; metadata and tags are merged into what exists.

```go
ids, err := redisStore.ListChatIDs(ctx)
info, err := redisStore.GetChatInfo(ctx, chatID, true /* include messages */)
```

`ChatInfo.Clone()` returns a copy **without** messages — useful for returning
metadata to an API without copying a long transcript.

### Housekeeping

```go
type MessageStoreManager interface {
    ListTenants(ctx context.Context) ([]string, error)
    Cleanup(ctx context.Context, tenantID string, olderThan time.Duration) (uint32, error)
}

mgr := store.NewRedisStoreManager(client, "myapp")
// or store.NewMemoryStoreManager(memstore)

tenants, err := mgr.ListTenants(ctx)
for _, tenant := range tenants {
    removed, err := mgr.Cleanup(ctx, tenant, 30*24*time.Hour)
}
```

## What the assistant reads and writes

On each run the assistant:

1. Calls `Store.Messages(ctx)` and inserts the result right after the system
   prompt.
2. Collects everything the run produced in `Response.Messages` — the user turn,
   the assistant's tool-call messages, the tool responses, and the final answer.
3. Calls `Store.Add(ctx, resp.Messages...)` once, atomically, at the end.

Control this with three options:

| Option | Effect |
|--------|--------|
| `WithMessageStore(nil)` (or omitted) | No history is read or written; each run is stateless |
| `WithSkipMessageHistory(true)` | History is read, nothing is written |
| `WithSkipToolHistory(true)` | Tool-call and tool-response messages are excluded from what is written |

`WithSkipToolHistory(true)` is a good default for long-running chats: tool
payloads dominate token usage on subsequent turns, while the final answer
usually carries the needed information.

## Sizing the history

The assistant enforces two guards each LLM turn:

- `MaxMessages` (default 100) — total messages including history.
- `MaxLength` / `DefaultMaxContentSize` (500000 bytes) — summed
  `ContentLength()` of all parts.

Both fail the run rather than truncating. Options for keeping under them:

- `WithSkipToolHistory(true)` to drop tool chatter.
- A fresh `ChatID` to start a new conversation.
- Summarize and `Reset`: read `Messages`, run a summarizer assistant, `Reset`,
  then `Add` a single summary message.
- `store.PopulateMemoryStore(ctx, existing)` copies the current chat's messages
  into a fresh in-memory store — handy for a "what-if" run that must not touch
  the durable history.

```go
scratch, err := store.PopulateMemoryStore(ctx, redisStore)
if err != nil {
    return err
}
resp, err := agent.Run(ctx, &assistants.CallInput{
    Input:   question,
    Options: []assistants.Option{assistants.WithMessageStore(scratch)},
}, &out)
```

## Implementing a store

Implement `MessageStore` and derive keys with `store.GetTenantAndChatID(ctx)`
so multi-tenancy behaves consistently. Requirements the assistant relies on:

- `Add` must be atomic across the whole variadic set — the assistant appends a
  complete turn at once, and a partial write can leave a `tool_call` without
  its `RoleTool` response, which providers reject.
- `Messages` must preserve insertion order.
- `Messages` returns `nil` rather than an error when the chat is unknown; the
  assistant treats a missing chat as an empty history.

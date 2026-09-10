// Package store persists chat messages and chat metadata for agentic flows,
// with in-memory and Redis backends.
//
// Every [MessageStore] method derives its keys from the chatmodel.ChatContext
// on the supplied context via [GetTenantAndChatID], which yields
// "orgID:userID" (or just userID when no org is set) as the tenant plus the
// chat ID. One store instance therefore serves all tenants safely.
//
//	memstore := store.NewMemoryStore()
//	// or: store.NewRedisStore(redisClient, "myapp")
//
//	agent := assistants.NewAssistant[Answer](fac, sysPrompt,
//	    assistants.WithMessageStore(memstore))
//
// # How an assistant uses a store
//
// On each run the assistant reads [MessageStore.Messages] and inserts the
// result after the system prompt, then appends everything the run produced —
// the user turn, tool calls, tool responses and the final answer — with a
// single [MessageStore.Add]. Use assistants.WithSkipMessageHistory to read
// without writing, and assistants.WithSkipToolHistory to omit the verbose
// tool-call pairs, which usually dominate token usage on later turns.
//
// Add must be atomic across its whole variadic set: a partial write can leave
// a tool call without its tool response, which providers reject.
//
// # Chat metadata and housekeeping
//
// [MessageStore.UpdateChat] merges a title, metadata and tags into the stored
// [ChatInfo]; empty arguments leave the existing values untouched.
// [MessageStoreManager] adds tenant enumeration and age-based cleanup.
//
// [PopulateMemoryStore] snapshots the current chat into a fresh in-memory
// store, which is useful for a speculative run that must not touch durable
// history.
//
// See Documentation/memory.md for the Redis key layout and history-sizing
// strategies.
package store

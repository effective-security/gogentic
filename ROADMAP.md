# gogentic Roadmap

This roadmap outlines the current state, missing features, and actionable next steps for evolving gogentic into a modern, production-ready agentic and multi-assistant chat platform.

## ✅ Current Capabilities

- Core agent/assistant abstractions (single-agent flows)
- Extensible tool system with schema generation and MCP integration
- LLM model factory (multi-provider support)
- In-memory and Redis chat/message storage
- Pluggable encoding (JSON, YAML, TOML, dummy)
- Real-time and local transport via MCP (SSE, local)
- Utilities for prompt formatting, message handling, and output cleaning
- Mocks for assistants, tools, and LLMs (testing)
- Comprehensive package-level documentation and coding guidelines

## ⚠️ Key Areas for Enhancement

### Multi-Assistant Orchestration

- [ ] Implement a `MultiAssistantOrchestrator` to:
  - Register and manage multiple assistants
  - Route/dispatch user queries to the appropriate assistant(s) based on context, intent, or user selection
  - Aggregate or coordinate responses from multiple assistants
  - Support tool-calling across assistants

### Conversation & Session Management

- [ ] Add a `SessionManager` abstraction for:
  - User/session lifecycle and authentication
  - Session timeouts, reconnections, and multi-device support
  - Persistent context across sessions

### Advanced Agentic Features

- [ ] Integrate with a vector DB or retrieval system for context-aware responses (RAG)
- [ ] Add a planner or workflow engine for multi-step reasoning and task chaining
- [ ] Support agent/assistant "personas" or profiles

### Observability & Monitoring

- [ ] Add Prometheus metrics and OpenTelemetry tracing
- [ ] (Optional) Build a simple admin dashboard for monitoring agent activity and health

### Security & Rate Limiting

- [ ] Add middleware for rate limiting, input validation, and abuse prevention

## 🛠️ How to Contribute

- See [AGENTS.md](AGENTS.md) and README for coding/testing guidelines
- Open issues or PRs for any of the above roadmap items

---

**Prioritize based on your use case! If you need help designing or implementing any of these features, open an issue or ask for guidance.**

# gogentic

LLM Agents in Go

## Overview

gogentic is a modular, extensible framework for building agentic LLM (Large Language Model) applications in Go. It is inspired by and forked from langchaingo, with a focus on improved tool and assistant abstractions, schema generation, and MCP (Message Control Protocol) support.

## Features

- **Agentic LLM Flows:** Compose complex agent behaviors using assistants, tools, and callbacks.
- **Schema Generation:** Automatic JSON schema generation for tool parameters and message formats.
- **MCP Support:** Native integration with MCP for distributed, real-time, and local transport communication.
- **Pluggable Tools:** Easily define, register, and use tools with LLM agents.
- **Multi-format Encoding:** Support for JSON, YAML, TOML, and custom encodings.
- **Memory and Persistence:** In-memory and Redis-backed chat/message stores.
- **Testable and Extensible:** Mocking, test utilities, and clear interfaces for rapid development.

## Architecture

- **assistants/**: Core agent and assistant logic, tool orchestration, and callback handling.
- **tools/**: Tool interface, registration, and MCP integration. Includes example tools (e.g., tavily).
- **llmfactory/**: LLM model factory and configuration (OpenAI, Azure, etc.).
- **chatmodel/**: Message and IO schema definitions for chat-based LLMs.
- **encoding/**: Pluggable encoders/decoders (json, yaml, toml, dummy).
- **store/**: Message and chat storage (memory, Redis).
- **mcp/**: Model Context Protocol extensions to `mcp-golang` (local, SSE, internal transport).
- **schema/**: JSON schema generation utilities.
- **llmutils/**: Utility functions for LLM operations.
- **mocks/**: Mock implementations for testing.

## Quickstart

```sh
go get github.com/effective-security/gogentic
```

See `assistants/assistants.go` and `tools/tools.go` for core interfaces and extension points.

## Usage Example

```go
import (
    "github.com/effective-security/gogentic/assistants"
    "github.com/effective-security/gogentic/tools"
)

// Define and register tools, create an assistant, and run agentic flows...
```

## Coding Guidelines

See [AGENTS.md](AGENTS.md) for detailed coding standards, error handling, and testing practices.

- Use `require` and `assert` from `testify` for tests
- Use `cockroachdb/errors` for error handling
- Prefer table-driven and parallel tests
- Document all exported types, functions, and interfaces

## Contributing

- Run `make lint` and `make test` before submitting PRs
- Follow the guidelines in [AGENTS.md](AGENTS.md)

## License

[Apache 2.0](LICENSE)

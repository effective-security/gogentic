// Package llms provides unified support for interacting with different Language Models (LLMs) from various providers.
// Designed with an extensible architecture, the package facilitates seamless integration of LLMs
// with a focus on modularity, encapsulation, and easy configurability.
//
// Each subpackage includes provider-specific LLM implementations and helper files for communication
// with supported LLM providers. The internal directories within these subpackages contain provider-specific
// client and API implementations.
//
// The `llms.go` file contains the types and interfaces for interacting with different LLMs.
//
// The `options.go` file provides various options and functions to configure the LLMs.
package llms

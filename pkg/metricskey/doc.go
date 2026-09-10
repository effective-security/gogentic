// Package metricskey declares the metric descriptors emitted by the assistant
// loop, for use with github.com/effective-security/metrics.
//
// [Metrics] is the full slice of descriptors, ready to register with a
// collector:
//
//	err := metrics.Register(metricskey.Metrics...)
//
// Counters tagged agent/model/org cover messages, bytes and tokens sent to and
// received from the LLM (including prompt-cache reads and writes), plus
// assistant call outcomes and output-parse errors. Counters tagged
// tool/model/org cover tool call outcomes, including calls to tools the model
// named but that were not registered. Samples record assistant and tool call
// durations.
//
// The "agent" tag is assistants.IAssistant.Name() and the "org" tag is
// chatmodel.ChatContext.GetOrgID(), so naming assistants and setting the org
// ID is what makes per-assistant and per-tenant dashboards possible.
package metricskey

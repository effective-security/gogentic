package metricskey

import "github.com/effective-security/metrics"

// Stats
var (
	// StatsLLMMessagesSent is base for counter metric for total messages sent to LLM
	StatsLLMMessagesSent = metrics.Describe{
		Type:         metrics.TypeCounter,
		Name:         "stats_llm_messages_sent",
		Help:         "stats_llm_messages_sent provides total messages sent to LLM",
		RequiredTags: []string{"agent"},
	}

	StatsLLMBytesSent = metrics.Describe{
		Type:         metrics.TypeCounter,
		Name:         "stats_llm_bytes_sent",
		Help:         "stats_llm_bytes_sent provides total bytes sent to LLM",
		RequiredTags: []string{"agent"},
	}

	StatsLLMBytesReceived = metrics.Describe{
		Type:         metrics.TypeCounter,
		Name:         "stats_llm_bytes_received",
		Help:         "stats_llm_bytes_received provides total bytes received from LLM",
		RequiredTags: []string{"agent"},
	}

	StatsLLMBytesTotal = metrics.Describe{
		Type:         metrics.TypeCounter,
		Name:         "stats_llm_bytes_total",
		Help:         "stats_llm_bytes_total provides total bytes sent and received from LLM",
		RequiredTags: []string{"agent"},
	}

	StatsAssistantCallsSucceeded = metrics.Describe{
		Type:         metrics.TypeCounter,
		Name:         "stats_assistant_calls_succeeded",
		Help:         "stats_assistant_calls_succeeded provides total assistant calls succeeded",
		RequiredTags: []string{"agent"},
	}

	StatsAssistantCallsFailed = metrics.Describe{
		Type:         metrics.TypeCounter,
		Name:         "stats_assistant_calls_failed",
		Help:         "stats_assistant_calls_failed provides total assistant calls failed",
		RequiredTags: []string{"agent"},
	}

	StatsToolCallsSucceeded = metrics.Describe{
		Type:         metrics.TypeCounter,
		Name:         "stats_tool_calls_succeeded",
		Help:         "stats_tool_calls_succeeded provides total tool calls succeeded",
		RequiredTags: []string{"tool"},
	}

	StatsToolCallsFailed = metrics.Describe{
		Type:         metrics.TypeCounter,
		Name:         "stats_tool_calls_failed",
		Help:         "stats_tool_calls_failed provides total tool calls failed",
		RequiredTags: []string{"tool"},
	}

	StatsToolCallsNotFound = metrics.Describe{
		Type:         metrics.TypeCounter,
		Name:         "stats_tool_calls_not_found",
		Help:         "stats_tool_calls_not_found provides total tool calls not found",
		RequiredTags: []string{"tool"},
	}

	StatsAssistantLLMParseErrors = metrics.Describe{
		Type:         metrics.TypeCounter,
		Name:         "stats_assistant_llm_parse_errors",
		Help:         "stats_assistant_llm_parse_errors provides total assistant LLM parse errors",
		RequiredTags: []string{"agent"},
	}
)

// Perf
var (
	PerfChatRun = metrics.Describe{
		Type:         metrics.TypeSample,
		Name:         "perf_chat_run",
		Help:         "perf_chat_run provides duration of chat run",
		RequiredTags: []string{"tenant"},
	}

	PerfAssistantCall = metrics.Describe{
		Type:         metrics.TypeSample,
		Name:         "perf_assistant_call",
		Help:         "perf_assistant_call provides duration of assistant call",
		RequiredTags: []string{"agent"},
	}

	PerfToolCall = metrics.Describe{
		Type:         metrics.TypeSample,
		Name:         "perf_tool_call",
		Help:         "perf_tool_call provides duration of tool call",
		RequiredTags: []string{"tool"},
	}
)

// Metrics returns slice of metrics from this repo
// keep sorted by name
var Metrics = []*metrics.Describe{
	&PerfAssistantCall,
	&PerfChatRun,
	&PerfToolCall,
	&StatsAssistantCallsFailed,
	&StatsAssistantCallsSucceeded,
	&StatsAssistantLLMParseErrors,
	&StatsLLMBytesReceived,
	&StatsLLMBytesSent,
	&StatsLLMBytesTotal,
	&StatsLLMMessagesSent,
	&StatsToolCallsFailed,
	&StatsToolCallsNotFound,
	&StatsToolCallsSucceeded,
}

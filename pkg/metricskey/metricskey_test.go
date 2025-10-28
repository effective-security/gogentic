package metricskey

import (
	"sort"
	"testing"

	"github.com/effective-security/metrics"
	"github.com/stretchr/testify/assert"
)

func TestMetricsDefinitions(t *testing.T) {
	// Test that all metrics have valid names and help text
	allMetrics := []*metrics.Describe{
		&PerfAssistantCall,
		&PerfToolCall,
		&StatsAssistantCallsFailed,
		&StatsAssistantCallsRetried,
		&StatsAssistantCallsSucceeded,
		&StatsAssistantLLMParseErrors,
		&StatsLLMBytesReceived,
		&StatsLLMBytesSent,
		&StatsLLMBytesTotal,
		&StatsLLMInputTokens,
		&StatsLLMMessagesSent,
		&StatsLLMOutputTokens,
		&StatsLLMTotalTokens,
		&StatsToolCallsFailed,
		&StatsToolCallsNotFound,
		&StatsToolCallsSucceeded,
	}

	for _, m := range allMetrics {
		assert.NotEmpty(t, m.Name, "Metric name should not be empty")
		assert.NotEmpty(t, m.Help, "Metric help text should not be empty")
		assert.NotEmpty(t, m.RequiredTags, "Metric should have required tags")
	}

	// Test that Metrics slice contains all metrics
	assert.Equal(t, len(allMetrics), len(Metrics), "Metrics slice should contain all defined metrics")

	// Test that Metrics slice is sorted by name
	isSorted := sort.SliceIsSorted(Metrics, func(i, j int) bool {
		return Metrics[i].Name < Metrics[j].Name
	})
	assert.True(t, isSorted, "Metrics slice should be sorted by name")

	// Test that all metrics in Metrics slice are unique
	seen := make(map[string]bool)
	for _, m := range Metrics {
		assert.False(t, seen[m.Name], "Metric name should be unique: %s", m.Name)
		seen[m.Name] = true
	}

	// Test specific metric properties
	t.Run("LLM metrics have agent tag", func(t *testing.T) {
		llmMetrics := []*metrics.Describe{
			&StatsLLMMessagesSent,
			&StatsLLMBytesSent,
			&StatsLLMBytesReceived,
			&StatsLLMBytesTotal,
			&StatsAssistantCallsSucceeded,
			&StatsAssistantCallsFailed,
			&StatsAssistantLLMParseErrors,
		}
		for _, m := range llmMetrics {
			assert.Contains(t, m.RequiredTags, "agent", "LLM metric should have agent tag: %s", m.Name)
		}
	})

	t.Run("Tool metrics have tool tag", func(t *testing.T) {
		toolMetrics := []*metrics.Describe{
			&StatsToolCallsSucceeded,
			&StatsToolCallsFailed,
			&StatsToolCallsNotFound,
		}
		for _, m := range toolMetrics {
			assert.Contains(t, m.RequiredTags, "tool", "Tool metric should have tool tag: %s", m.Name)
		}
	})
}

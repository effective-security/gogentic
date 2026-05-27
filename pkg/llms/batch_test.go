package llms_test

import (
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
)

func TestBatchStatus_IsTerminal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		status   llms.BatchStatus
		terminal bool
	}{
		{name: "validating", status: llms.BatchStatusValidating, terminal: false},
		{name: "in_progress", status: llms.BatchStatusInProgress, terminal: false},
		{name: "finalizing", status: llms.BatchStatusFinalizing, terminal: false},
		{name: "cancelling", status: llms.BatchStatusCancelling, terminal: false},
		{name: "completed", status: llms.BatchStatusCompleted, terminal: true},
		{name: "failed", status: llms.BatchStatusFailed, terminal: true},
		{name: "expired", status: llms.BatchStatusExpired, terminal: true},
		{name: "cancelled", status: llms.BatchStatusCancelled, terminal: true},
		{name: "unknown", status: llms.BatchStatus("something-else"), terminal: false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.terminal, tc.status.IsTerminal())
		})
	}
}

func TestBatchRequestError_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *llms.BatchRequestError
		want string
	}{
		{name: "nil", err: nil, want: ""},
		{name: "message only", err: &llms.BatchRequestError{Message: "boom"}, want: "boom"},
		{name: "code and message", err: &llms.BatchRequestError{Code: "rate_limit_exceeded", Message: "slow down"}, want: "rate_limit_exceeded: slow down"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.err.Error())
		})
	}
}

func TestCapabilityBatch_OpenAI(t *testing.T) {
	t.Parallel()
	assert.True(t, llms.ProviderOpenAI.Supports(llms.CapabilityBatch))
	assert.False(t, llms.ProviderAnthropic.Supports(llms.CapabilityBatch))
	assert.False(t, llms.ProviderAzure.Supports(llms.CapabilityBatch))
}

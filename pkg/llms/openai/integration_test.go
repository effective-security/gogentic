package openai

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrationPromptCaching(t *testing.T) {
	llm := newTestClient(t, WithModel("gpt-5.1"))

	// OpenAI prompt caching is automatic on prompt prefixes; make the stable prefix
	// large enough to cross the cache threshold so repeated requests can read cache.
	stableSystemBlock := strings.Repeat(
		"Review policy: validate legal name, tax classification, sanctions screening, beneficial ownership, invoice controls, and audit trail before approval. ",
		100,
	)

	content := []llms.Message{
		llms.MessageFromTextParts(llms.RoleSystem, stableSystemBlock),
		llms.MessageFromTextParts(llms.RoleHuman, "Summarize the approval requirements in one short sentence."),
	}

	cacheKey := fmt.Sprintf("gogentic-openai-prompt-cache-%d", time.Now().UnixNano())
	cachePolicy := &llms.PromptCachePolicy{
		Request: &llms.PromptCacheRequestPolicy{
			Key:       cacheKey,
			Retention: llms.PromptCacheRetentionInMemory,
		},
	}

	var inputTokens []uint64
	var cacheReads []uint64

	for i := 0; i < 3; i++ {
		resp, err := llm.GenerateContent(context.Background(), content,
			llms.WithPromptCachePolicy(cachePolicy),
			llms.WithMaxTokens(64),
		)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Choices)

		choice := resp.Choices[0]
		inputTokens = append(inputTokens, choice.Usage.InputTokens)
		cacheReads = append(cacheReads, choice.Usage.CacheReadTokens)

		if i >= 1 && cacheReads[i] > 0 {
			break
		}
	}

	assert.Greater(t, inputTokens[0], uint64(1024),
		"expected prompt to exceed OpenAI caching threshold, input tokens=%d", inputTokens[0])

	require.GreaterOrEqual(t, len(cacheReads), 2)
	assert.Greater(t, slices.Max(cacheReads[1:]), uint64(0),
		"expected a cache read hit on a repeated identical request, cacheReads=%v inputTokens=%v", cacheReads, inputTokens)
}

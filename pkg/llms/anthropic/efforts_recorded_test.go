package anthropic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordedSupport is the "supported" flag of one capability.
type recordedSupport struct {
	Supported bool `json:"supported"`
}

// recordedModel is the part of a recorded model list entry that describes the
// reasoning controls of the model.
type recordedModel struct {
	ID           string `json:"id"`
	Capabilities struct {
		Effort struct {
			Supported bool            `json:"supported"`
			Low       recordedSupport `json:"low"`
			Medium    recordedSupport `json:"medium"`
			High      recordedSupport `json:"high"`
			XHigh     recordedSupport `json:"xhigh"`
			Max       recordedSupport `json:"max"`
		} `json:"effort"`
		Thinking struct {
			Types struct {
				Enabled      recordedSupport `json:"enabled"`
				Adaptive     recordedSupport `json:"adaptive"`
				Disabled     recordedSupport `json:"disabled"`
				BetweenTools recordedSupport `json:"between_tools"`
			} `json:"types"`
		} `json:"thinking"`
	} `json:"capabilities"`
}

// recordedModels returns the entries of the model list recorded from the
// Anthropic API.
func recordedModels(t *testing.T) []recordedModel {
	t.Helper()
	var models []recordedModel
	for _, name := range []string{"models_page1.json", "models_page2.json"} {
		raw, err := os.ReadFile(filepath.Join("testdata", name))
		require.NoError(t, err)
		var page struct {
			Data []recordedModel `json:"data"`
		}
		require.NoError(t, json.Unmarshal(raw, &page))
		models = append(models, page.Data...)
	}
	require.NotEmpty(t, models)
	return models
}

// The effort table in efforts.go is written by hand. This test holds it to
// the capabilities the API reported for every model when the list in testdata
// was recorded.
func TestEffortProfile_MatchesRecordedCapabilities(t *testing.T) {
	for _, model := range recordedModels(t) {
		t.Run(model.ID, func(t *testing.T) {
			profile := effortProfileFor(model.ID)
			effort := model.Capabilities.Effort
			thinking := model.Capabilities.Thinking.Types

			// The output effort is sent exactly to the models that take it.
			assert.Equal(t, effort.Supported, profile.outputEffort, "output effort")

			if effort.Supported {
				reported := map[llms.ReasoningEffort]bool{
					llms.ReasoningEffortLow:    effort.Low.Supported,
					llms.ReasoningEffortMedium: effort.Medium.Supported,
					llms.ReasoningEffortHigh:   effort.High.Supported,
					llms.ReasoningEffortXHigh:  effort.XHigh.Supported,
					llms.ReasoningEffortMax:    effort.Max.Supported,
				}
				for level, supported := range reported {
					assert.Equal(t, supported, profile.levels.Has(level), "level %s", level)
				}
			}

			// The thinking configuration sent for a level is one the model takes.
			switch profile.thinking {
			case thinkingModeAdaptive:
				assert.True(t, thinking.Adaptive.Supported, "adaptive thinking")
			case thinkingModeBudget:
				assert.True(t, thinking.Enabled.Supported, "thinking budget")
			default:
				t.Errorf("no thinking mode for a model that thinks")
			}

			// The way thinking is turned off is one the model takes. From 5.0
			// a model thinks unless told not to, so a model that can stop must
			// not be left to think.
			switch profile.off {
			case thinkingOffDisabled:
				assert.True(t, thinking.Disabled.Supported, "disabled thinking")
			case thinkingOffBetweenTools:
				assert.True(t, thinking.BetweenTools.Supported, "between_tools thinking")
			case thinkingOffOmit:
				if llms.ParseModelName(model.ID).VersionAtLeast(5) {
					assert.False(t, thinking.Disabled.Supported, "disabled thinking is available")
					assert.False(t, thinking.BetweenTools.Supported, "between_tools thinking is available")
				}
			}
		})
	}
}

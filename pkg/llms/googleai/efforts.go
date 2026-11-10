package googleai

import (
	"strings"

	"github.com/effective-security/gogentic/pkg/llms"
	"google.golang.org/genai"
)

// Gemini family names, as returned by [llms.ParseModelName].
const (
	// familyGemini starts the family name of every Gemini model.
	familyGemini          = "gemini"
	familyGeminiPro       = "gemini-pro"
	familyGeminiFlash     = "gemini-flash"
	familyGeminiFlashLite = "gemini-flash-lite"
)

// Thinking token budgets sent to the Gemini 2.5 models, which take a budget
// instead of a thinking level. They are the budgets Google's
// OpenAI-compatible endpoint uses for low, medium and high.
const (
	thinkingBudgetOff    int32 = 0
	thinkingBudgetLow    int32 = 1024
	thinkingBudgetMedium int32 = 8192
	thinkingBudgetHigh   int32 = 24576
)

// Sets of portable reasoning levels accepted by the model families below.
// A portable none is sent as MINIMAL, or as a zero thinking budget.
var (
	effortsLowToHigh = llms.NewEffortSet(
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
	)
	effortsLowAndHigh = llms.NewEffortSet(
		llms.ReasoningEffortLow,
		llms.ReasoningEffortHigh,
	)
	effortsNoneToHigh = llms.NewEffortSet(
		llms.ReasoningEffortNone,
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
	)
)

// effortProfile describes the thinking control of one model.
type effortProfile struct {
	// levels are the portable levels the model accepts.
	levels llms.EffortSet
	// implicit is the level the model applies when a request names none.
	implicit llms.ReasoningEffort
	// budget is set for the models that take a thinking token budget instead
	// of a thinking level.
	budget bool
}

// effortProfileFor returns the thinking control of a model, derived from its
// family and version. It follows the thinking levels in the Gemini
// documentation:
//
//	gemini-3-pro                  low, high
//	gemini-pro, other versions    low, medium, high
//	gemini-flash 3.0 – 3.7        minimal, low, medium, high
//	gemini-flash, other versions  low, medium, high
//	gemini-flash-lite 3.x         minimal, low, medium, high
//
// Gemini 2.5 takes a thinking budget instead, and rejects a thinking level:
//
//	gemini-pro 2.5                low, medium, high; cannot stop thinking
//	gemini-flash 2.5              off, low, medium, high
//	gemini-flash-lite 2.5         off, low, medium, high
//
// Gemini before 2.5 has no thinking control. Any other model is assumed to
// accept the thinking levels low, medium and high.
func effortProfileFor(model string) effortProfile {
	name := llms.ParseModelName(model)
	if isGeminiFamily(name.Family) && name.VersionBelow(3) {
		return budgetEffortProfile(name)
	}
	switch name.Family {
	case familyGeminiPro:
		if name.VersionAtLeast(3) && name.VersionBelow(3, 1) {
			return effortProfile{
				levels:   effortsLowAndHigh,
				implicit: llms.ReasoningEffortHigh,
			}
		}
	case familyGeminiFlash:
		switch {
		case name.VersionAtLeast(3, 8):
			return effortProfile{
				levels:   effortsLowToHigh,
				implicit: llms.ReasoningEffortMedium,
			}
		case name.VersionAtLeast(3, 6):
			return effortProfile{
				levels:   effortsNoneToHigh,
				implicit: llms.ReasoningEffortMedium,
			}
		case name.VersionAtLeast(3):
			return effortProfile{
				levels:   effortsNoneToHigh,
				implicit: llms.ReasoningEffortHigh,
			}
		}
	case familyGeminiFlashLite:
		// Flash-Lite thinks as little as it can unless asked to.
		if name.VersionAtLeast(3) {
			return effortProfile{
				levels:   effortsNoneToHigh,
				implicit: llms.ReasoningEffortNone,
			}
		}
	}
	return effortProfile{
		levels:   effortsLowToHigh,
		implicit: llms.ReasoningEffortHigh,
	}
}

// budgetEffortProfile returns the thinking control of a Gemini model older
// than 3.0. Left alone, 2.5 Pro and Flash decide how much to think, up to
// their high budget; Flash-Lite does not think.
func budgetEffortProfile(name llms.ModelName) effortProfile {
	if name.VersionBelow(2, 5) {
		return effortProfile{}
	}
	switch name.Family {
	case familyGeminiFlash:
		return effortProfile{
			levels:   effortsNoneToHigh,
			implicit: llms.ReasoningEffortHigh,
			budget:   true,
		}
	case familyGeminiFlashLite:
		return effortProfile{
			levels:   effortsNoneToHigh,
			implicit: llms.ReasoningEffortNone,
			budget:   true,
		}
	}
	// Gemini 2.5 Pro cannot stop thinking: its lowest setting is low.
	return effortProfile{
		levels:   effortsLowToHigh,
		implicit: llms.ReasoningEffortHigh,
		budget:   true,
	}
}

// thinkingConfig returns the thinking configuration that asks the model for
// the level.
func (p effortProfile) thinkingConfig(level llms.ReasoningEffort) *genai.ThinkingConfig {
	if p.budget {
		// A zero budget turns thinking off, so it is sent like any other.
		budget := thinkingBudget(level)
		return &genai.ThinkingConfig{
			ThinkingBudget: &budget,
		}
	}
	return &genai.ThinkingConfig{
		ThinkingLevel: thinkingLevel(level),
	}
}

// thinkingLevel converts a portable level into the Gemini thinking level.
func thinkingLevel(level llms.ReasoningEffort) genai.ThinkingLevel {
	switch level {
	case llms.ReasoningEffortNone:
		return genai.ThinkingLevelMinimal
	case llms.ReasoningEffortLow:
		return genai.ThinkingLevelLow
	case llms.ReasoningEffortMedium:
		return genai.ThinkingLevelMedium
	}
	return genai.ThinkingLevelHigh
}

// thinkingBudget converts a portable level into a Gemini 2.5 thinking budget.
func thinkingBudget(level llms.ReasoningEffort) int32 {
	switch level {
	case llms.ReasoningEffortNone:
		return thinkingBudgetOff
	case llms.ReasoningEffortLow:
		return thinkingBudgetLow
	case llms.ReasoningEffortMedium:
		return thinkingBudgetMedium
	}
	return thinkingBudgetHigh
}

// isGeminiFamily reports whether the family is gemini or one of its tiers.
func isGeminiFamily(family string) bool {
	return family == familyGemini || strings.HasPrefix(family, familyGemini+"-")
}

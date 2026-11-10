package anthropic

import (
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/effective-security/gogentic/pkg/llms"
)

const (
	// claudeFamily starts the family name of every Claude model, as in
	// claude-opus or claude-fable.
	claudeFamily = "claude"

	// Model lines that follow the claude- prefix in a family name.
	lineOpus   = "opus"
	lineSonnet = "sonnet"
	lineHaiku  = "haiku"
	lineFable  = "fable"
	lineMythos = "mythos"

	// Token budgets sent to models that only take extended thinking with a
	// budget. The API requires at least 1024.
	thinkingBudgetLow    int64 = 1024
	thinkingBudgetMedium int64 = 5000
	thinkingBudgetHigh   int64 = 10000
)

// thinkingMode is the kind of thinking configuration a model accepts.
type thinkingMode int

const (
	// The zero value means that no thinking parameter is ever sent.
	_ thinkingMode = iota
	// thinkingModeBudget is extended thinking with a caller-set token budget,
	// the only mode of the models before 4.6.
	thinkingModeBudget
	// thinkingModeAdaptive lets the model decide how much to think, guided by the
	// output effort. Models from 4.7 on reject a token budget.
	thinkingModeAdaptive
)

// thinkingOff is the way a model is asked not to think, used for the portable
// level none.
type thinkingOff int

const (
	// thinkingOffOmit leaves the thinking parameter out. It turns thinking
	// off on the models before 5.0, and is the only form accepted by the
	// models that always think.
	thinkingOffOmit thinkingOff = iota
	// thinkingOffDisabled sends the thinking type "disabled".
	thinkingOffDisabled
	// thinkingOffBetweenTools sends the thinking type "between_tools", the
	// lowest thinking setting of Claude Sonnet 5.5.
	thinkingOffBetweenTools
)

// Sets of portable reasoning levels accepted by the model generations below.
// A portable none means the least thinking the model allows and the lowest
// output effort.
var (
	effortsNoneToHigh = llms.NewEffortSet(
		llms.ReasoningEffortNone,
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
	)
	effortsNoneToMaxNoXHigh = llms.NewEffortSet(
		llms.ReasoningEffortNone,
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
		llms.ReasoningEffortMax,
	)
	effortsNoneToMax = llms.NewEffortSet(
		llms.ReasoningEffortNone,
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
		llms.ReasoningEffortXHigh,
		llms.ReasoningEffortMax,
	)
)

// effortProfile describes the reasoning controls of one model.
type effortProfile struct {
	// levels are the portable levels the model accepts. An empty set means
	// the model has no reasoning control.
	levels llms.EffortSet
	// thinking is the thinking configuration the model accepts.
	thinking thinkingMode
	// outputEffort is set when the model accepts output_config.effort.
	outputEffort bool
	// lowByDefault is set for the models that get the low output effort when
	// a call requests no level.
	lowByDefault bool
	// off is the way the model is asked not to think.
	off thinkingOff
	// implicit is the level the model applies when a request names none.
	implicit llms.ReasoningEffort
}

// effortProfileFor returns the reasoning controls of a model, derived from its
// version. It follows the Anthropic model documentation:
//
//	5.x, 4.8, 4.7   adaptive thinking only; effort low, medium, high, xhigh, max
//	4.6             adaptive thinking; effort low, medium, high, max
//	Opus 4.5        thinking budget; effort low, medium, high
//	3.7 – 4.5       thinking budget; no effort setting
//
// A version newer than the table is treated like the newest known generation.
// From 5.0 a model thinks unless it is told not to; [thinkingOffFor] says how.
func effortProfileFor(model string) effortProfile {
	name := llms.ParseModelName(model)
	if !isClaudeFamily(name.Family) {
		return effortProfile{}
	}
	switch {
	case name.VersionAtLeast(5):
		return effortProfile{
			levels:       effortsNoneToMax,
			thinking:     thinkingModeAdaptive,
			outputEffort: true,
			off:          thinkingOffFor(name),
			// Thinking is on by default from 5.0, at high effort or lower.
			implicit: llms.ReasoningEffortHigh,
		}
	case name.VersionAtLeast(4, 7):
		return effortProfile{
			levels:       effortsNoneToMax,
			thinking:     thinkingModeAdaptive,
			outputEffort: true,
			lowByDefault: true,
			implicit:     llms.ReasoningEffortNone,
		}
	case name.VersionAtLeast(4, 6):
		return effortProfile{
			levels:       effortsNoneToMaxNoXHigh,
			thinking:     thinkingModeAdaptive,
			outputEffort: true,
			lowByDefault: true,
			implicit:     llms.ReasoningEffortNone,
		}
	case name.VersionAtLeast(3, 7):
		return effortProfile{
			levels:       effortsNoneToHigh,
			thinking:     thinkingModeBudget,
			outputEffort: claudeLine(name.Family) == lineOpus && name.VersionAtLeast(4, 5),
			implicit:     llms.ReasoningEffortNone,
		}
	}
	return effortProfile{}
}

// thinkingOffFor returns the way a 5.x model is asked not to think. These
// models think by default, and differ in how, and whether, that can be turned
// off:
//
//	Opus 5, Sonnet 5, Haiku 5.5   thinking type "disabled"
//	Sonnet 5.5                    thinking type "between_tools"
//	Opus 5.5, Fable, Mythos       always think; "disabled" is rejected
//
// Both types are accepted at high effort or below only; they are sent with the
// low effort. A model the table does not know gets no thinking parameter,
// which every model accepts.
func thinkingOffFor(name llms.ModelName) thinkingOff {
	switch claudeLine(name.Family) {
	case lineOpus:
		if name.VersionBelow(5, 5) {
			return thinkingOffDisabled
		}
	case lineSonnet:
		switch {
		case name.VersionBelow(5, 5):
			return thinkingOffDisabled
		case name.VersionBelow(5, 6):
			return thinkingOffBetweenTools
		}
	case lineHaiku:
		if name.VersionBelow(5, 6) {
			return thinkingOffDisabled
		}
	}
	return thinkingOffOmit
}

// withReported returns the profile corrected by the effort levels the model
// list reports for the model. They replace those of the table; none stays,
// since the API never names it and every model can be asked for its least. A
// model the list reports no levels for keeps the table's.
func (p effortProfile) withReported(policy llms.EffortPolicy) effortProfile {
	if policy.Supported.Empty() || !p.outputEffort {
		return p
	}
	p.levels = policy.Supported.With(llms.ReasoningEffortNone)
	return p
}

// apply writes the thinking and effort settings of one call into params.
// level is the outcome of [llms.EffortPolicy.Resolve]: ReasoningEffortDefault
// when the call and the configuration requested nothing.
func (p effortProfile) apply(params *anthropic.MessageNewParams, level llms.ReasoningEffort) {
	switch level {
	case llms.ReasoningEffortDefault:
		if p.lowByDefault {
			params.OutputConfig.Effort = anthropic.OutputConfigEffortLow
		}
		return
	case llms.ReasoningEffortNone:
		// The least reasoning: thinking off where the model allows it, and
		// the lowest output effort.
		if p.outputEffort {
			params.OutputConfig.Effort = anthropic.OutputConfigEffortLow
		}
		switch p.off {
		case thinkingOffDisabled:
			params.Thinking = anthropic.ThinkingConfigParamUnion{
				OfDisabled: &anthropic.ThinkingConfigDisabledParam{},
			}
		case thinkingOffBetweenTools:
			betweenTools := anthropic.NewThinkingConfigBetweenToolsParam()
			params.Thinking = anthropic.ThinkingConfigParamUnion{
				OfBetweenTools: &betweenTools,
			}
		}
		return
	}

	if p.outputEffort {
		params.OutputConfig.Effort = outputEffort(level)
	}
	switch p.thinking {
	case thinkingModeAdaptive:
		params.Thinking = anthropic.ThinkingConfigParamUnion{
			OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{},
		}
	case thinkingModeBudget:
		budget := thinkingBudgetFor(level)
		params.Thinking = anthropic.ThinkingConfigParamUnion{
			OfEnabled: &anthropic.ThinkingConfigEnabledParam{
				BudgetTokens: budget,
			},
		}
		// The budget counts against max_tokens and must be below it.
		if params.MaxTokens <= budget {
			params.MaxTokens = budget + DefaultMaxTokens
		}
	}
}

// outputEffort converts a portable level into the output effort value.
func outputEffort(level llms.ReasoningEffort) anthropic.OutputConfigEffort {
	switch level {
	case llms.ReasoningEffortMedium:
		return anthropic.OutputConfigEffortMedium
	case llms.ReasoningEffortHigh:
		return anthropic.OutputConfigEffortHigh
	case llms.ReasoningEffortXHigh:
		return anthropic.OutputConfigEffortXhigh
	case llms.ReasoningEffortMax:
		return anthropic.OutputConfigEffortMax
	}
	return anthropic.OutputConfigEffortLow
}

// thinkingBudgetFor converts a portable level into a thinking token budget.
func thinkingBudgetFor(level llms.ReasoningEffort) int64 {
	switch level {
	case llms.ReasoningEffortLow:
		return thinkingBudgetLow
	case llms.ReasoningEffortMedium:
		return thinkingBudgetMedium
	}
	return thinkingBudgetHigh
}

// modelNeedsLegacyToolVersions reports whether the model only supports the
// older web tool versions (web_fetch_20250910 instead of web_fetch_20260209).
// The newer versions need a Fable or Mythos model, or Opus or Sonnet 4.6 and
// later.
func modelNeedsLegacyToolVersions(model string) bool {
	name := llms.ParseModelName(model)
	if !isClaudeFamily(name.Family) {
		return true
	}
	switch claudeLine(name.Family) {
	case lineFable, lineMythos:
		return false
	case lineOpus, lineSonnet:
		return !name.VersionAtLeast(4, 6)
	}
	return true
}

// isClaudeFamily reports whether the family is claude or one of its lines.
func isClaudeFamily(family string) bool {
	return family == claudeFamily || strings.HasPrefix(family, claudeFamily+"-")
}

// claudeLine returns the model line of a Claude family, such as opus for
// claude-opus, or an empty string when the family names none.
func claudeLine(family string) string {
	_, line, _ := strings.Cut(family, claudeFamily+"-")
	return line
}

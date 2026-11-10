package openai

import (
	"strings"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/openai/openai-go/v3/shared"
)

const (
	// gptFamily is the family name shared by the GPT models; tiers and
	// variants extend it, as in gpt-mini or gpt-codex-max.
	gptFamily = "gpt"
	// variantPro marks the tier that does not reason below medium, and on
	// GPT-5 only at high.
	variantPro = "pro"
	// variantCodex marks the coding models, which have no "none" level.
	variantCodex = "codex"
	// variantSol marks the top GPT-5.6 tier, which is never run below high.
	variantSol = "sol"
	// oSeriesPrefix starts the family of the o1, o3 and o4 reasoning models.
	oSeriesPrefix = "o"

	// firstReasoningMajor is the first GPT generation with a reasoning control.
	firstReasoningMajor = 5
	// legacyVersionFloor separates real versions from names such as
	// gpt-35-turbo, where 35 stands for 3.5.
	legacyVersionFloor = 30
)

// Sets of portable reasoning levels accepted by the model generations below.
// A portable none is sent as the model's lowest level.
var (
	effortsLowToHigh = llms.NewEffortSet(
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
	)
	effortsLowToXHigh = llms.NewEffortSet(
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
		llms.ReasoningEffortXHigh,
	)
	effortsMediumToXHigh = llms.NewEffortSet(
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
		llms.ReasoningEffortXHigh,
	)
	effortsNoneToHigh = llms.NewEffortSet(
		llms.ReasoningEffortNone,
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
	)
	effortsNoneToXHigh = llms.NewEffortSet(
		llms.ReasoningEffortNone,
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
		llms.ReasoningEffortXHigh,
	)
	effortsNoneToMax = llms.NewEffortSet(
		llms.ReasoningEffortNone,
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
		llms.ReasoningEffortXHigh,
		llms.ReasoningEffortMax,
	)
	effortsHighOnly  = llms.NewEffortSet(llms.ReasoningEffortHigh)
	effortsHighToMax = llms.NewEffortSet(
		llms.ReasoningEffortHigh,
		llms.ReasoningEffortXHigh,
		llms.ReasoningEffortMax,
	)
)

// effortProfile describes the reasoning control of one model.
type effortProfile struct {
	// levels are the portable levels the model accepts. An empty set means
	// the model has no reasoning control and no setting is sent.
	levels llms.EffortSet
	// implicit is the level this client sends on the Responses API when a
	// call requests none. ReasoningEffortDefault means nothing is sent.
	implicit llms.ReasoningEffort
	// upstream is the level the API applies to a request that carries no
	// effort setting. An effort ceiling is checked against it whenever this
	// client sends nothing.
	upstream llms.ReasoningEffort
	// minimalFloor is set for models whose lowest level is "minimal" rather
	// than "none".
	minimalFloor bool
}

// effortProfileFor returns the reasoning control of a model, derived from its
// family and version. Supported levels follow the OpenAI model pages:
//
//	o1, o3, o4            low, medium, high
//	gpt-5                 minimal, low, medium, high
//	gpt-5-pro             high
//	gpt-5.1               none, low, medium, high
//	gpt-5.2 – 5.5         none, low, medium, high, xhigh
//	gpt-5.2 – 5.5 codex   low, medium, high, xhigh
//	gpt-5.2 – 5.5 pro     medium, high, xhigh
//	gpt-5.6               none, low, medium, high, xhigh, max
//
// A newer generation the table does not know is assumed to accept low, medium
// and high, which every reasoning model so far does.
//
// The level a model applies on its own follows the same pages: none for
// gpt-5.1 to 5.4, medium for the others. Where a page does not state a
// default, medium is assumed, and high for the pro tier.
func effortProfileFor(model string) effortProfile {
	name := llms.ParseModelName(model)
	profile := familyEffortProfile(name)
	if name.Prefix != "" {
		// IDs routed through another vendor (Bedrock, OpenRouter) accept a
		// requested level, but get no setting when a call requests none.
		profile.implicit = llms.ReasoningEffortDefault
	}
	return profile
}

func familyEffortProfile(name llms.ModelName) effortProfile {
	if isOSeries(name.Family) {
		return effortProfile{
			levels:   effortsLowToHigh,
			implicit: llms.ReasoningEffortLow,
			upstream: llms.ReasoningEffortMedium,
		}
	}
	if !isGPTFamily(name.Family) || !name.HasVersion() {
		return effortProfile{}
	}
	major := name.Version[0]
	if major < firstReasoningMajor || major >= legacyVersionFloor {
		return effortProfile{}
	}

	isPro := hasVariant(name.Family, variantPro)
	switch {
	case name.VersionBelow(5, 1):
		if isPro {
			return effortProfile{
				levels:   effortsHighOnly,
				implicit: llms.ReasoningEffortHigh,
				upstream: llms.ReasoningEffortHigh,
			}
		}
		return effortProfile{
			levels:       effortsNoneToHigh,
			implicit:     llms.ReasoningEffortLow,
			upstream:     llms.ReasoningEffortMedium,
			minimalFloor: true,
		}
	case name.VersionBelow(5, 2):
		return effortProfile{
			levels:   effortsNoneToHigh,
			implicit: llms.ReasoningEffortNone,
			upstream: llms.ReasoningEffortNone,
		}
	case name.VersionBelow(5, 6):
		switch {
		case isPro:
			return effortProfile{
				levels:   effortsMediumToXHigh,
				implicit: llms.ReasoningEffortMedium,
				upstream: llms.ReasoningEffortHigh,
			}
		case hasVariant(name.Family, variantCodex):
			return effortProfile{
				levels:   effortsLowToXHigh,
				implicit: llms.ReasoningEffortLow,
				upstream: llms.ReasoningEffortMedium,
			}
		}
		upstream := llms.ReasoningEffortNone
		if name.VersionAtLeast(5, 5) {
			upstream = llms.ReasoningEffortMedium
		}
		return effortProfile{
			levels:   effortsNoneToXHigh,
			implicit: llms.ReasoningEffortLow,
			upstream: upstream,
		}
	case name.VersionBelow(5, 7):
		if hasVariant(name.Family, variantSol) {
			return effortProfile{
				levels:   effortsHighToMax,
				implicit: llms.ReasoningEffortHigh,
				upstream: llms.ReasoningEffortMedium,
			}
		}
		return effortProfile{
			levels:   effortsNoneToMax,
			implicit: llms.ReasoningEffortLow,
			upstream: llms.ReasoningEffortMedium,
		}
	}
	return effortProfile{
		levels:   effortsLowToHigh,
		upstream: llms.ReasoningEffortMedium,
	}
}

// resolve returns the level to put on a Responses request for one call, or
// ReasoningEffortDefault when no setting should be sent.
func (p effortProfile) resolve(policy llms.EffortPolicy, requested llms.ReasoningEffort) llms.ReasoningEffort {
	if p.implicit == llms.ReasoningEffortDefault {
		return p.resolveOmitted(policy, requested)
	}
	level := policy.Resolve(requested, p.levels, p.implicit)
	if level == llms.ReasoningEffortDefault {
		level = p.levels.Nearest(p.implicit)
	}
	return level
}

// resolveOmitted returns the level for a request that carries an effort only
// when one is asked for: Chat Completions, and the models this client sends
// no level to on its own. Left alone, such a request runs at the level the API
// applies, so that level is what an effort ceiling is checked against.
func (p effortProfile) resolveOmitted(policy llms.EffortPolicy, requested llms.ReasoningEffort) llms.ReasoningEffort {
	return policy.Resolve(requested, p.levels, p.upstream)
}

// wire converts a portable level into the OpenAI effort value.
func (p effortProfile) wire(level llms.ReasoningEffort) shared.ReasoningEffort {
	switch level {
	case llms.ReasoningEffortNone:
		if p.minimalFloor {
			return shared.ReasoningEffortMinimal
		}
		return shared.ReasoningEffortNone
	case llms.ReasoningEffortLow:
		return shared.ReasoningEffortLow
	case llms.ReasoningEffortMedium:
		return shared.ReasoningEffortMedium
	case llms.ReasoningEffortHigh:
		return shared.ReasoningEffortHigh
	case llms.ReasoningEffortXHigh:
		return shared.ReasoningEffortXhigh
	case llms.ReasoningEffortMax:
		return shared.ReasoningEffortMax
	}
	return ""
}

// isGPTFamily reports whether the family is gpt or one of its variants.
func isGPTFamily(family string) bool {
	return family == gptFamily || strings.HasPrefix(family, gptFamily+"-")
}

// isOSeries reports whether the family is one of the o1, o3 or o4 reasoning
// models, such as o3 or o4-mini.
func isOSeries(family string) bool {
	head, _, _ := strings.Cut(family, "-")
	if len(head) < 2 || !strings.HasPrefix(head, oSeriesPrefix) {
		return false
	}
	for _, r := range head[len(oSeriesPrefix):] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// hasVariant reports whether the family name carries the variant token, as
// gpt-pro carries pro.
func hasVariant(family, variant string) bool {
	for _, token := range strings.Split(family, "-") {
		if token == variant {
			return true
		}
	}
	return false
}

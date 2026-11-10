package llms

import (
	"strings"

	"github.com/cockroachdb/errors"
)

// Names of the reasoning effort levels, as written in configuration.
const (
	effortNameNone   = "none"
	effortNameLow    = "low"
	effortNameMedium = "medium"
	effortNameHigh   = "high"
	effortNameXHigh  = "xhigh"
	effortNameMax    = "max"
)

// effortLevels lists the levels a model can be asked for, lowest first.
var effortLevels = []ReasoningEffort{
	ReasoningEffortNone,
	ReasoningEffortLow,
	ReasoningEffortMedium,
	ReasoningEffortHigh,
	ReasoningEffortXHigh,
	ReasoningEffortMax,
}

var effortNames = map[ReasoningEffort]string{
	ReasoningEffortNone:   effortNameNone,
	ReasoningEffortLow:    effortNameLow,
	ReasoningEffortMedium: effortNameMedium,
	ReasoningEffortHigh:   effortNameHigh,
	ReasoningEffortXHigh:  effortNameXHigh,
	ReasoningEffortMax:    effortNameMax,
}

// String returns the configuration name of the level, or an empty string for
// ReasoningEffortDefault and for values that are not levels.
func (e ReasoningEffort) String() string {
	return effortNames[e]
}

// ParseReasoningEffort converts a configuration name such as "medium" into a
// level. Matching ignores case and surrounding spaces. ReasoningEffortDefault
// has no name: leave the effort out instead of naming it.
func ParseReasoningEffort(s string) (ReasoningEffort, error) {
	name := strings.ToLower(strings.TrimSpace(s))
	for level, levelName := range effortNames {
		if name == levelName {
			return level, nil
		}
	}
	return ReasoningEffortDefault, errors.Errorf("invalid reasoning effort: %q", s)
}

// EffortSet is the set of reasoning effort levels a model accepts.
// The zero value is the empty set: a model without a reasoning control.
type EffortSet uint8

// NewEffortSet returns the set holding the given levels.
// ReasoningEffortDefault and values that are not levels are ignored.
func NewEffortSet(levels ...ReasoningEffort) EffortSet {
	var set EffortSet
	for _, level := range levels {
		if _, ok := effortNames[level]; ok {
			set |= 1 << uint(level)
		}
	}
	return set
}

// Has reports whether the set holds the level.
func (s EffortSet) Has(e ReasoningEffort) bool {
	if _, ok := effortNames[e]; !ok {
		return false
	}
	return s&(1<<uint(e)) != 0
}

// Empty reports whether the set holds no level.
func (s EffortSet) Empty() bool {
	return s == 0
}

// With returns the set extended by the given levels.
func (s EffortSet) With(levels ...ReasoningEffort) EffortSet {
	return s | NewEffortSet(levels...)
}

// Levels returns the levels in the set, lowest first.
func (s EffortSet) Levels() []ReasoningEffort {
	var levels []ReasoningEffort
	for _, level := range effortLevels {
		if s.Has(level) {
			levels = append(levels, level)
		}
	}
	return levels
}

// Nearest returns the level in the set that is closest to e: e itself when
// the set holds it, otherwise the highest level below e, otherwise the lowest
// level above e. It returns ReasoningEffortDefault when e is not a level or
// the set is empty.
func (s EffortSet) Nearest(e ReasoningEffort) ReasoningEffort {
	if _, ok := effortNames[e]; !ok || s.Empty() {
		return ReasoningEffortDefault
	}
	if s.Has(e) {
		return e
	}
	nearest := ReasoningEffortDefault
	for _, level := range effortLevels {
		if !s.Has(level) {
			continue
		}
		if level > e {
			if nearest == ReasoningEffortDefault {
				nearest = level
			}
			break
		}
		nearest = level
	}
	return nearest
}

// EffortPolicy is the reasoning effort configuration a provider client applies
// to every call it makes. The zero value changes nothing.
//
// Default and Max come from configuration. Supported and Implicit describe the
// model the client was built for, as its provider's model list reports it;
// they are empty unless the list was read.
type EffortPolicy struct {
	// Default is used when a call does not request a level.
	Default ReasoningEffort
	// Max is the highest level a call may use. ReasoningEffortDefault means
	// there is no ceiling.
	Max ReasoningEffort
	// Supported holds the levels the provider reports for the client's model.
	// When it is not empty, the client uses it in place of its own table for
	// that model. The empty set means the provider reported none.
	Supported EffortSet
	// Implicit is the level the provider reports the client's model applies
	// to a request that names none. ReasoningEffortDefault means the provider
	// did not report it.
	Implicit ReasoningEffort
}

// IsZero reports whether the policy changes nothing: it has no default, no
// ceiling, and nothing reported by the provider.
func (p EffortPolicy) IsZero() bool {
	return p == EffortPolicy{}
}

// Resolve returns the level to send for one call.
//
// requested is the level the call asked for, supported is the set the model
// accepts, and implicit is the level the model applies when a request names
// none (ReasoningEffortDefault when that is unknown or already minimal).
//
// The requested level wins over the policy default. The result is lowered to
// the ceiling and then snapped to the nearest supported level. When no level
// is requested and the policy has no default, Resolve returns
// ReasoningEffortDefault, which tells the provider to send what it sends
// without an effort setting; the only exception is an implicit level above the
// ceiling, for which the ceiling is returned when the model accepts a level
// below the implicit one. A ceiling never raises the level.
func (p EffortPolicy) Resolve(requested ReasoningEffort, supported EffortSet, implicit ReasoningEffort) ReasoningEffort {
	level := requested
	if level == ReasoningEffortDefault {
		level = p.Default
	}
	hasCeiling := p.Max != ReasoningEffortDefault
	if level == ReasoningEffortDefault {
		if !hasCeiling || implicit <= p.Max {
			return ReasoningEffortDefault
		}
		// The model's floor can be above the ceiling. Sending it would not
		// lower anything, and may raise an implicit level known only roughly.
		capped := supported.Nearest(p.Max)
		if capped >= implicit {
			return ReasoningEffortDefault
		}
		return capped
	}
	if hasCeiling && level > p.Max {
		level = p.Max
	}
	return supported.Nearest(level)
}

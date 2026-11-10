package llmfactory

import (
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
)

const (
	// effortSeparator precedes the effort level in a model reference.
	effortSeparator = ","
	// providerSeparator follows the provider prefix in a model reference.
	providerSeparator = "/"
	// wildcardAny matches any run of characters in a model pattern.
	wildcardAny = '*'
	// wildcardOne matches exactly one character in a model pattern.
	wildcardOne = '?'
	// wildcards lists the characters that turn a model ID into a pattern.
	wildcards = "*?"

	patternStart = "^"
	patternEnd   = "$"
	patternAny   = ".*"
	patternOne   = "."
)

// refSpec is a model reference, [PROVIDER/]MODEL[,EFFORT], split into its
// model path and its effort.
type refSpec struct {
	// path is the reference without the effort suffix: [PROVIDER/]MODEL.
	path string
	// effort is the level written on the reference, or
	// llms.ReasoningEffortDefault when it has none.
	effort llms.ReasoningEffort
}

// parseRefSpec splits a model reference into its path and effort. The text
// after the last comma, when there is one, must be an effort level.
func parseRefSpec(raw string) (refSpec, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return refSpec{}, errors.New("model reference is empty")
	}
	if _, err := llms.ParseReasoningEffort(text); err == nil {
		// A YAML flow sequence splits [MODEL,effort] into two entries.
		return refSpec{}, errors.Errorf(
			"model reference %q is only an effort level: write the model and its effort as one string, "+
				"in block style or quoted, for example \"MODEL,%s\"", raw, text)
	}

	spec := refSpec{
		path: text,
	}
	cut := strings.LastIndex(text, effortSeparator)
	if cut < 0 {
		return spec, nil
	}
	effort, err := llms.ParseReasoningEffort(text[cut+1:])
	if err != nil {
		return refSpec{}, errors.Wrapf(err, "model reference %q", raw)
	}
	spec.path = strings.TrimSpace(text[:cut])
	spec.effort = effort
	if spec.path == "" {
		return refSpec{}, errors.Errorf("model reference %q has no model", raw)
	}
	return spec, nil
}

// modelMatcher matches model IDs against an exact ID or a pattern in which
// "*" stands for any run of characters and "?" for exactly one.
type modelMatcher struct {
	// text is the ID or pattern as written.
	text string
	// pattern is the compiled pattern, or nil for an exact ID.
	pattern *regexp.Regexp
}

// newModelMatcher compiles an ID or pattern. Every character other than the
// two wildcards is literal, including "." and "/".
func newModelMatcher(text string) modelMatcher {
	matcher := modelMatcher{
		text: text,
	}
	if !strings.ContainsAny(text, wildcards) {
		return matcher
	}
	var expr strings.Builder
	expr.WriteString(patternStart)
	for _, r := range text {
		switch r {
		case wildcardAny:
			expr.WriteString(patternAny)
		case wildcardOne:
			expr.WriteString(patternOne)
		default:
			expr.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	expr.WriteString(patternEnd)
	// The expression is built from quoted literals, so it always compiles.
	matcher.pattern = regexp.MustCompile(expr.String())
	return matcher
}

// isPattern reports whether the matcher holds a pattern rather than an ID.
func (m modelMatcher) isPattern() bool {
	return m.pattern != nil
}

// match reports whether the model ID is the matcher's ID or fits its pattern.
func (m modelMatcher) match(model string) bool {
	if m.pattern != nil {
		return m.pattern.MatchString(model)
	}
	return m.text == model
}

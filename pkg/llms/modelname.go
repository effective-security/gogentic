package llms

import (
	"regexp"
	"strconv"
	"strings"
)

const (
	modelTokenSeparator  = "-"
	modelPathSeparator   = "/"
	modelVendorSeparator = "."
	versionSeparator     = "."

	// snapshotMinDigits is the length from which an all-digit token is a
	// snapshot (a date such as 20250514 or a build such as 1106) rather than a
	// version component.
	snapshotMinDigits = 4
	// dateTokens is the number of tokens in a dashed date: YYYY-MM-DD.
	dateTokens = 3
	// datePartDigits is the length of the month and day tokens of a dashed date.
	datePartDigits = 2
)

var (
	// dottedVersionToken matches version tokens such as 5.1 or 2.0.
	dottedVersionToken = regexp.MustCompile(`^\d+(\.\d+)+$`)
	// digitsToken matches tokens made of digits only.
	digitsToken = regexp.MustCompile(`^\d+$`)
	// yearToken matches the first token of a dashed date.
	yearToken = regexp.MustCompile(`^(19|20)\d{2}$`)
	// revisionToken matches revision suffixes such as v1 or v1:0.
	revisionToken = regexp.MustCompile(`^v\d+(:\d+)?$`)

	// stageTokens are the words that mark a model as not generally available.
	stageTokens = map[string]bool{
		"preview":      true,
		"exp":          true,
		"experimental": true,
		"beta":         true,
		"rc":           true,
		"latest":       true,
	}
)

// ModelName is a model ID split into the parts that tell a model family from
// its version. It is derived from naming conventions only, so it is a best
// guess: an ID that follows no convention keeps its whole name as the family
// and has no version.
type ModelName struct {
	// ID is the model ID as given.
	ID string
	// Prefix is the routing or vendor prefix, such as "us.anthropic." in a
	// Bedrock ID or "openai/" in an OpenRouter ID. Empty for first-party IDs.
	Prefix string
	// Family is the name without prefix, version, snapshot and stage, such as
	// "claude-opus", "gpt-mini" or "gemini-flash-lite".
	Family string
	// Version holds the numeric version components, such as [4 8] for
	// claude-opus-4-8 and [5 1] for gpt-5.1. Empty when the ID has none.
	Version []int
	// Snapshot is the dated or numbered build, such as "20250514",
	// "2025-11-13" or "v1:0". Empty for a floating ID.
	Snapshot string
	// Stage is the release stage word, such as "preview". Empty for a
	// generally available model.
	Stage string
}

// ParseModelName splits a model ID into family, version, snapshot and stage.
//
// The ID is cut on "-". Number tokens form the version, date and revision
// tokens the snapshot, stage words the stage, and the remaining tokens the
// family:
//
//	claude-fable-5-1                   family claude-fable   version 5.1
//	claude-3-5-sonnet-20241022         family claude-sonnet  version 3.5  snapshot 20241022
//	gpt-5.1-mini                       family gpt-mini       version 5.1
//	gemini-3-pro-preview               family gemini-pro     version 3    stage preview
//	us.anthropic.claude-opus-4-20250514-v1:0
//	                                   prefix us.anthropic.  family claude-opus  version 4
func ParseModelName(id string) ModelName {
	name := ModelName{
		ID: id,
	}
	rest := id
	if i := strings.LastIndex(rest, modelPathSeparator); i >= 0 {
		name.Prefix = rest[:i+1]
		rest = rest[i+1:]
	}
	tokens := strings.Split(rest, modelTokenSeparator)

	// A dotted first token carries a vendor or region prefix: openai.gpt,
	// us.anthropic.claude.
	first := tokens[0]
	if i := strings.LastIndex(first, modelVendorSeparator); i >= 0 && !dottedVersionToken.MatchString(first) {
		name.Prefix += first[:i+1]
		tokens[0] = first[i+1:]
	}

	var family, snapshot []string
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		switch {
		case token == "":
			// Skip empty tokens produced by doubled separators.
		case dottedVersionToken.MatchString(token):
			name.Version = append(name.Version, versionParts(token)...)
		case digitsToken.MatchString(token):
			if !isSnapshotToken(token) {
				name.Version = append(name.Version, versionParts(token)...)
				continue
			}
			if isDashedDate(tokens[i:]) {
				snapshot = append(snapshot, strings.Join(tokens[i:i+dateTokens], modelTokenSeparator))
				i += dateTokens - 1
				continue
			}
			snapshot = append(snapshot, token)
		case revisionToken.MatchString(token):
			snapshot = append(snapshot, token)
		case stageTokens[token]:
			name.Stage = token
		default:
			family = append(family, token)
		}
	}
	name.Family = strings.Join(family, modelTokenSeparator)
	name.Snapshot = strings.Join(snapshot, modelTokenSeparator)
	return name
}

// Key identifies the family across versions, including the prefix, so that
// models reached through different routes are not mixed up.
func (n ModelName) Key() string {
	return n.Prefix + n.Family
}

// HasVersion reports whether the ID carried a version.
func (n ModelName) HasVersion() bool {
	return len(n.Version) > 0
}

// VersionAtLeast reports whether the model version is equal to or newer than
// the given components, for example VersionAtLeast(4, 6). It is false for a
// model without a version.
func (n ModelName) VersionAtLeast(parts ...int) bool {
	return n.HasVersion() && CompareVersions(n.Version, parts) >= 0
}

// VersionBelow reports whether the model version is older than the given
// components. It is false for a model without a version.
func (n ModelName) VersionBelow(parts ...int) bool {
	return n.HasVersion() && CompareVersions(n.Version, parts) < 0
}

// CompareVersions compares two versions component by component, treating a
// missing component as zero. It returns -1, 0 or 1.
func CompareVersions(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}
	return 0
}

// isSnapshotToken reports whether an all-digit token names a build rather
// than a version: long numbers and numbers with a leading zero.
func isSnapshotToken(token string) bool {
	return len(token) >= snapshotMinDigits || (len(token) > 1 && token[0] == '0')
}

// isDashedDate reports whether tokens start with a YYYY-MM-DD date.
func isDashedDate(tokens []string) bool {
	if len(tokens) < dateTokens || !yearToken.MatchString(tokens[0]) {
		return false
	}
	for _, part := range tokens[1:dateTokens] {
		if len(part) != datePartDigits || !digitsToken.MatchString(part) {
			return false
		}
	}
	return true
}

// versionParts converts a version token into its numeric components.
func versionParts(token string) []int {
	fields := strings.Split(token, versionSeparator)
	parts := make([]int, 0, len(fields))
	for _, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil {
			// The token matched a digits-only pattern, so this only happens
			// on overflow; such a number is not a version.
			return nil
		}
		parts = append(parts, n)
	}
	return parts
}

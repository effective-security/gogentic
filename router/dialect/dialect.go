package dialect

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/router"
)

// DecodeOptions controls how much a codec tolerates in incoming requests.
type DecodeOptions struct {
	// Strict rejects unknown fields, duplicate keys and null values instead of
	// ignoring them. Known fields with unsupported non-default values are always
	// rejected, in both modes.
	Strict bool
}

// EncodeOptions controls what a codec writes into public responses.
type EncodeOptions struct {
	// OmitReasoning removes reasoning items from the public response. The Go
	// Result still contains them; clients then cannot replay opaque state.
	OmitReasoning bool
}

// HTTPStatus maps a router error kind to the HTTP status every supported dialect
// uses for it. Hosts may override the mapping in their own handlers.
func HTTPStatus(kind router.Kind) int {
	switch kind {
	case router.KindInvalidRequest, router.KindUnsupported:
		return http.StatusBadRequest
	case router.KindModelNotFound:
		return http.StatusNotFound
	case router.KindRateLimited:
		return http.StatusTooManyRequests
	case router.KindSelectionUnavailable:
		return http.StatusServiceUnavailable
	case router.KindUpstream:
		return http.StatusBadGateway
	case router.KindTimeout:
		return http.StatusGatewayTimeout
	case router.KindCancelled:
		return statusClientClosedRequest
	}
	return http.StatusInternalServerError
}

// statusClientClosedRequest is the conventional nginx status for a cancelled
// request. Nothing is written to a disconnected client; it exists for logs.
const statusClientClosedRequest = 499

// MetadataUser is the Request.Metadata key codecs use for the caller-supplied
// end-user identifier (OpenAI user / safety_identifier, Anthropic metadata.user_id).
const MetadataUser = "user"

// MaxOpaqueBytes bounds a sealed opaque state string accepted from a client.
const MaxOpaqueBytes = 512 << 10

type opaqueEnvelope struct {
	Source string          `json:"s"`
	State  json.RawMessage `json:"d"`
}

// SealOpaque packs a reasoning block's source target and provider state into
// one URL-safe string that clients replay verbatim.
func SealOpaque(source string, state json.RawMessage) (string, error) {
	if source == "" || len(state) == 0 {
		return "", errors.New("dialect: opaque state requires a source and a payload")
	}
	b, err := json.Marshal(opaqueEnvelope{
		Source: source,
		State:  state,
	})
	if err != nil {
		return "", errors.WithMessage(err, "dialect: seal opaque state")
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// OpenOpaque restores the source target and provider state from a sealed string.
// Malformed input is a client error.
func OpenOpaque(sealed string) (string, json.RawMessage, error) {
	if len(sealed) > MaxOpaqueBytes {
		return "", nil, router.Invalid("reasoning", "reasoning state too large")
	}
	b, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil {
		return "", nil, router.Invalid("reasoning", "reasoning state is not a router-issued value")
	}
	var env opaqueEnvelope
	if err := json.Unmarshal(b, &env); err != nil || env.Source == "" || len(env.State) == 0 || !json.Valid(env.State) {
		return "", nil, router.Invalid("reasoning", "reasoning state is not a router-issued value")
	}
	return env.Source, env.State, nil
}

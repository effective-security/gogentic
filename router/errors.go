package router

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
)

// Kind classifies a routing failure independently of any transport. Dialect
// codecs map kinds to their own status codes and envelopes.
type Kind string

const (
	// KindInvalidRequest is a malformed or out-of-bounds request.
	KindInvalidRequest Kind = "invalid_request"
	// KindUnsupported is a well-formed control the selected model cannot honor.
	KindUnsupported Kind = "unsupported"
	// KindModelNotFound is an unknown, unregistered or invisible model name.
	KindModelNotFound Kind = "model_not_found"
	// KindSelectionUnavailable is a selector failure with no configured recovery.
	KindSelectionUnavailable Kind = "selection_unavailable"
	// KindRateLimited is an admission rejection or an upstream rate limit.
	KindRateLimited Kind = "rate_limited"
	// KindUpstream is a provider failure or an invalid provider response.
	KindUpstream Kind = "upstream_error"
	// KindTimeout is an exhausted deadline before a response was available.
	KindTimeout Kind = "timeout"
	// KindCancelled is a caller cancellation.
	KindCancelled Kind = "cancelled"
	// KindInternal is an unexpected failure inside the router or a callback.
	KindInternal Kind = "internal_error"
)

// Error is a transport-neutral routing failure with a client-safe message and a
// retained diagnostic cause. Message never contains provider text.
type Error struct {
	Kind    Kind
	Param   string
	Message string
	Cause   error
}

// Error returns the sanitized message.
func (e *Error) Error() string { return e.Message }

// Unwrap exposes internal diagnostics to trusted callers.
func (e *Error) Unwrap() error { return e.Cause }

// NewError creates a classified error without a cause. The message is a
// fmt format string when args are supplied and used verbatim otherwise.
func NewError(kind Kind, param, format string, args ...any) error {
	return errors.WithStack(&Error{
		Kind:    kind,
		Param:   param,
		Message: formatMessage(format, args),
	})
}

// Invalid reports a malformed request tied to a parameter.
func Invalid(param, format string, args ...any) error {
	return NewError(KindInvalidRequest, param, format, args...)
}

// Unsupported reports a well-formed control that cannot be honored.
func Unsupported(param, format string, args ...any) error {
	return NewError(KindUnsupported, param, format, args...)
}

func formatMessage(format string, args []any) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

// AsError returns the *Error inside err, or wraps an unclassified error as
// KindInternal with a generic message. It never returns nil for a non-nil err.
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	var re *Error
	if errors.As(err, &re) {
		return re
	}
	return &Error{
		Kind:    KindInternal,
		Message: "internal router error",
		Cause:   err,
	}
}

func contextError(err error) error {
	kind := KindTimeout
	message := "request deadline exceeded"
	if errors.Is(err, context.Canceled) {
		kind = KindCancelled
		message = "request cancelled"
	}
	return errors.WithStack(&Error{
		Kind:    kind,
		Message: message,
		Cause:   err,
	})
}

func admissionError(err error) error {
	var re *Error
	if errors.As(err, &re) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return contextError(err)
	}
	return errors.WithStack(&Error{
		Kind:    KindRateLimited,
		Message: "request not admitted",
		Cause:   err,
	})
}

// translateError converts connector failures into classified router errors.
func translateError(err error) error {
	var re *Error
	if errors.As(err, &re) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return contextError(err)
	}
	var ie *llms.InferenceError
	if errors.As(err, &ie) {
		switch {
		case ie.Status == http.StatusBadRequest && ie.Param != "":
			return errors.WithStack(&Error{
				Kind:    KindUnsupported,
				Param:   ie.Param,
				Message: "parameter is unsupported by the selected model",
				Cause:   err,
			})
		case ie.Status == http.StatusTooManyRequests:
			return errors.WithStack(&Error{
				Kind:    KindRateLimited,
				Message: "model rate limit exceeded",
				Cause:   err,
			})
		}
	}
	return errors.WithStack(&Error{
		Kind:    KindUpstream,
		Message: "model inference failed",
		Cause:   err,
	})
}

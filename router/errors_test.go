package router

import (
	"context"
	"net/http"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	msgCancelled     = "request cancelled"
	msgDeadline      = "request deadline exceeded"
	msgNotAdmitted   = "request not admitted"
	msgInferenceFail = "model inference failed"
)

func TestNewErrorFormatting(t *testing.T) {
	err := NewError(KindUpstream, "p", "%d%% done", 50)
	re := requireRouterError(t, err, KindUpstream, "p", "50% done")
	assert.NoError(t, re.Cause)

	err = Unsupported(paramSeed, "100% literal")
	requireRouterError(t, err, KindUnsupported, paramSeed, "100% literal")
	assert.Equal(t, "100% literal", err.Error())
}

func TestAsError(t *testing.T) {
	assert.Nil(t, AsError(nil))

	cause := errors.New("raw failure")
	re := AsError(cause)
	require.NotNil(t, re)
	assert.Equal(t, KindInternal, re.Kind)
	assert.Empty(t, re.Param)
	assert.Equal(t, "internal router error", re.Message)
	assert.Same(t, cause, re.Cause)
	assert.ErrorIs(t, re, cause, "Unwrap exposes the diagnostic cause")

	classified := &Error{
		Kind:    KindTimeout,
		Message: "slow",
	}
	assert.Same(t, classified, AsError(errors.Wrap(classified, "context")))
}

func TestAdmissionError(t *testing.T) {
	classified := NewError(KindInvalidRequest, "budget", "budget exhausted")
	assert.Same(t, classified, admissionError(classified), "classified errors pass through")

	err := admissionError(context.Canceled)
	re := requireRouterError(t, err, KindCancelled, "", msgCancelled)
	assert.ErrorIs(t, re.Cause, context.Canceled)

	err = admissionError(errors.Wrap(context.DeadlineExceeded, "queue wait"))
	requireRouterError(t, err, KindTimeout, "", msgDeadline)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	cause := errors.New("tenant quota")
	err = admissionError(cause)
	re = requireRouterError(t, err, KindRateLimited, "", msgNotAdmitted)
	assert.Same(t, cause, re.Cause)
}

func TestTranslateError(t *testing.T) {
	classified := Unsupported(paramTools, "no tools")
	assert.Same(t, classified, translateError(classified))

	err := translateError(errors.Wrap(context.Canceled, "provider call"))
	requireRouterError(t, err, KindCancelled, "", msgCancelled)
	assert.ErrorIs(t, err, context.Canceled)

	err = translateError(context.DeadlineExceeded)
	requireRouterError(t, err, KindTimeout, "", msgDeadline)

	unsupported := &llms.InferenceError{
		Status: http.StatusBadRequest,
		Param:  paramTopP,
		Cause:  errors.New("top_p not allowed"),
	}
	err = translateError(unsupported)
	requireRouterError(t, err, KindUnsupported, paramTopP, "parameter is unsupported by the selected model")
	assert.ErrorIs(t, err, unsupported)

	limited := &llms.InferenceError{
		Status: http.StatusTooManyRequests,
		Cause:  errors.New("slow down"),
	}
	err = translateError(limited)
	requireRouterError(t, err, KindRateLimited, "", "model rate limit exceeded")

	badRequest := &llms.InferenceError{
		Status: http.StatusBadRequest,
		Cause:  errors.New("bad request without param"),
	}
	err = translateError(badRequest)
	requireRouterError(t, err, KindUpstream, "", msgInferenceFail)

	err = translateError(errors.New("provider secret"))
	requireRouterError(t, err, KindUpstream, "", msgInferenceFail)
	assert.NotContains(t, err.Error(), "secret")
}

func TestContextError(t *testing.T) {
	err := contextError(context.DeadlineExceeded)
	requireRouterError(t, err, KindTimeout, "", msgDeadline)
	err = contextError(context.Canceled)
	requireRouterError(t, err, KindCancelled, "", msgCancelled)
}

package router

import (
	"context"

	"github.com/cockroachdb/errors"
)

// concurrencyLimit admits at most n requests at a time and rejects the rest
// immediately. It is a convenience for hosts without their own admission layer.
type concurrencyLimit struct {
	slots chan struct{}
}

// ConcurrencyLimit returns an Admission that allows n concurrent requests and
// rejects further requests immediately with KindRateLimited. n must be positive.
func ConcurrencyLimit(n int) Admission {
	if n < 1 {
		n = 1
	}
	return &concurrencyLimit{
		slots: make(chan struct{}, n),
	}
}

// Admit takes a slot without waiting.
func (c *concurrencyLimit) Admit(context.Context, AdmissionRequest) (func(), error) {
	select {
	case c.slots <- struct{}{}:
		return func() { <-c.slots }, nil
	default:
		return nil, errors.WithStack(&Error{
			Kind:    KindRateLimited,
			Message: "router capacity exceeded",
		})
	}
}

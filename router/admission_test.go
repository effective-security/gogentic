package router

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConcurrencyLimitClampsToOneSlot(t *testing.T) {
	for _, n := range []int{0, -3} {
		limit := ConcurrencyLimit(n)
		release, err := limit.Admit(context.Background(), AdmissionRequest{})
		require.NoError(t, err)
		require.NotNil(t, release)

		second, err := limit.Admit(context.Background(), AdmissionRequest{})
		requireRouterError(t, err, KindRateLimited, "", "router capacity exceeded")
		assert.Nil(t, second)

		release()
		again, err := limit.Admit(context.Background(), AdmissionRequest{})
		require.NoError(t, err, "a released slot is reusable")
		again()
	}
}

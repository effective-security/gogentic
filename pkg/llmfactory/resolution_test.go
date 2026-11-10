package llmfactory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plainFactory implements Factory and nothing else: neither Catalog nor
// ExactResolver.
type plainFactory struct {
	Factory
}

// A factory that cannot resolve without building a client is refused.
func TestResolve_FactoryWithoutCatalog(t *testing.T) {
	ctx := context.Background()

	res, err := Resolve(ctx, plainFactory{}, ModelOptions{})
	require.EqualError(t, err, "factory does not support model resolution")
	assert.Nil(t, res)

	err = RefreshModels(ctx, plainFactory{})
	require.EqualError(t, err, "factory does not support model discovery")
}

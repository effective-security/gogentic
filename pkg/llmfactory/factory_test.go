package llmfactory_test

import (
	"testing"

	"github.com/effective-security/gogentic/pkg/llmfactory"
	"github.com/stretchr/testify/require"
)

func Test_Factory(t *testing.T) {
	cfg, err := llmfactory.LoadConfig("testdata/llm.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, cfg.Providers)

	f := llmfactory.New(cfg)
	model, err := f.DefaultModel()
	require.NoError(t, err)
	require.NotEmpty(t, model)

	model2, err := f.ModelByName("openai-dev")
	require.NoError(t, err)
	require.NotEmpty(t, model2)

	model3, err := f.ModelByType("OPEN_AI")
	require.NoError(t, err)
	require.NotEmpty(t, model3)
}

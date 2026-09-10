package router_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/pkg/llmfactory"
	"github.com/effective-security/gogentic/router"
	"github.com/stretchr/testify/require"
)

const (
	liveEnable = "GOGENTIC_ROUTER_LIVE"
	liveConfig = "GOGENTIC_ROUTER_CONFIG"
	livePrefix = "GOGENTIC_ROUTER_"
	liveTarget = "live"
)

// TestLiveSmoke is opt-in. Deployments are supplied explicitly through the
// environment, never inferred from ambient credentials or mutable aliases.
// It exercises all three dialects for text, a required tool call and a strict
// output schema against one configured deployment per provider family.
func TestLiveSmoke(t *testing.T) {
	if os.Getenv(liveEnable) != "1" {
		t.Skip("set " + liveEnable + "=1 with explicit deployment configuration")
	}
	configPath := os.Getenv(liveConfig)
	require.NotEmpty(t, configPath)
	factory, err := llmfactory.Load(configPath)
	require.NoError(t, err)
	for _, family := range []string{"OPENAI", "ANTHROPIC", "GOOGLEAI", "BEDROCK"} {
		t.Run(family, func(t *testing.T) {
			providerName := os.Getenv(livePrefix + family + "_PROVIDER")
			modelName := os.Getenv(livePrefix + family + "_MODEL")
			if providerName == "" || modelName == "" {
				t.Skip("no explicit deployment configured for " + family)
			}
			m, err := llmfactory.ResolveExact(context.Background(), factory, providerName, modelName, "")
			require.NoError(t, err)
			c, err := router.Connector(m)
			require.NoError(t, err)
			t.Logf("live deployment: provider=%s model=%s", providerName, modelName)
			rt, err := router.New(router.Config{
				Targets: []router.Target{{
					ID:           liveTarget,
					BackendModel: modelName,
					Connector:    c,
					Features: router.Features{
						Tools:           true,
						JSONSchema:      true,
						StrictSchema:    true,
						SchemaWithTools: true,
					},
					MaxOutputTokens: 512,
				}},
			})
			require.NoError(t, err)
			for _, name := range dialects {
				for _, mode := range []string{modeText, modeTool, modeSchema} {
					t.Run(name+"/"+mode, func(t *testing.T) {
						body := strings.Replace(initialBody(name, mode), `"model":"public"`, `"model":"`+liveTarget+`"`, 1)
						body = strings.Replace(body, `"content":"hello"`, `"content":"Reply hello, or call the lookup function when tools are supplied."`, 1)
						body = strings.Replace(body, `"input":"hello"`, `"input":"Reply hello, or call the lookup function when tools are supplied."`, 1)
						cd := codecFor(name)
						req, err := cd.decode([]byte(body))
						require.NoError(t, err)
						res, err := rt.Generate(context.Background(), req)
						require.NoError(t, err, body)
						encoded, err := cd.encode(res, req)
						require.NoError(t, err)
						t.Logf("%s/%s finish=%s usage_known=%v bytes=%d", name, mode, res.Response.FinishReason, res.Response.UsageKnown, len(encoded))
					})
				}
			}
		})
	}
}

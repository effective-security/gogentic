package llmfactory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	orgAcme   = "acme"
	orgGlobex = "globex"
)

// TestLoadConfig_NullOrgOverride loads organizations listed without
// overrides; LoadConfig used to panic on them (F-013).
func TestLoadConfig_NullOrgOverride(t *testing.T) {
	file := filepath.Join(t.TempDir(), "llm.yaml")
	require.NoError(t, os.WriteFile(file, []byte(`
providers:
  - name: OPENAI
    token: fakekey
    default_model: gpt-5.1
    open_ai:
      api_type: OPENAI
assistant_models:
  default:
    - OPENAI/gpt-5.1
  planner:
    - OPENAI/gpt-5.5
orgs_override:
  acme:
  globex:
    assistant_models:
      planner:
        - OPENAI/gpt-5.6
`), 0o600))

	cfg, err := LoadConfig(file)
	require.NoError(t, err)

	global := map[string][]string{
		"default": {"OPENAI/gpt-5.1"},
		"planner": {"OPENAI/gpt-5.5"},
	}
	require.Contains(t, cfg.Orgs, orgAcme)
	require.NotNil(t, cfg.Orgs[orgAcme])
	assert.Equal(t, global, cfg.Orgs[orgAcme].AssistantModels, "an org without overrides gets the global mappings")
	require.NotNil(t, cfg.Orgs[orgGlobex])
	assert.Equal(t, map[string][]string{
		"default": {"OPENAI/gpt-5.1"},
		"planner": {"OPENAI/gpt-5.6"},
	}, cfg.Orgs[orgGlobex].AssistantModels)
}

// TestNew_NullOrgOverride builds a factory from a configuration made in
// code with a nil organization entry, which New used to dereference.
func TestNew_NullOrgOverride(t *testing.T) {
	f, ok := New(&Config{
		AssistantModels: map[string][]string{
			"default": {"OPENAI/gpt-5.1"},
		},
		Orgs: map[string]*OrgConfig{
			orgAcme: nil,
		},
	}).(*factory)
	require.True(t, ok)
	assert.NotContains(t, f.orgAssistantModels, orgAcme)
	assert.Equal(t, map[string][]string{
		"default": {"OPENAI/gpt-5.1"},
	}, f.assistantModels)
}

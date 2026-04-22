package skills_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupLoader(t *testing.T) *skills.Loader {
	t.Helper()
	dir := t.TempDir()
	writeSkillFile(t, dir, "vuln-triage", `---
name: vuln-triage
description: Use when triaging vulnerabilities.
---

## Steps
1. Check CVSS score.
2. Check exploitability.
`)
	loader := skills.NewDefaultLoader("", dir)
	require.NoError(t, loader.Load())
	return loader
}

func TestActivateSkillTool_Call_Valid(t *testing.T) {
	loader := setupLoader(t)
	tool, err := skills.NewActivateSkillTool(loader)
	require.NoError(t, err)

	out, err := tool.Call(context.Background(), `{"name":"vuln-triage"}`)
	require.NoError(t, err)
	assert.Contains(t, out, `<skill_content name="vuln-triage"`)
	assert.Contains(t, out, "Check CVSS score.")
	assert.Contains(t, out, "</skill_content>")
}

func TestActivateSkillTool_Call_NotFound(t *testing.T) {
	loader := setupLoader(t)
	tool, err := skills.NewActivateSkillTool(loader)
	require.NoError(t, err)

	out, err := tool.Call(context.Background(), `{"name":"nonexistent"}`)
	require.NoError(t, err)
	assert.Contains(t, out, "not found")
	assert.Contains(t, out, "vuln-triage") // available skills listed
}

func TestActivateSkillTool_ParametersEnum(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "skill-one", "---\nname: skill-one\ndescription: One.\n---\nbody")
	writeSkillFile(t, dir, "skill-two", "---\nname: skill-two\ndescription: Two.\n---\nbody")

	loader := skills.NewDefaultLoader("", dir)
	require.NoError(t, loader.Load())

	tool, err := skills.NewActivateSkillTool(loader)
	require.NoError(t, err)

	params := tool.Parameters()
	require.NotNil(t, params)
	require.NotNil(t, params.Properties)

	nameProp, ok := params.Properties.Get("name")
	require.True(t, ok)
	require.Len(t, nameProp.Enum, 2)

	enumVals := make([]string, 2)
	for i, v := range nameProp.Enum {
		enumVals[i] = v.(string)
	}
	assert.Contains(t, enumVals, "skill-one")
	assert.Contains(t, enumVals, "skill-two")
}

func TestActivateSkillTool_ResourceListing(t *testing.T) {
	dir := t.TempDir()

	// Skill with scripts/ subdirectory
	skillDir := filepath.Join(dir, "rich-skill")
	require.NoError(t, os.MkdirAll(filepath.Join(skillDir, "scripts"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: rich-skill
description: Skill with scripts.
---
Body here.
`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "scripts", "run.sh"), []byte("#!/bin/bash\necho hi"), 0755))

	loader := skills.NewDefaultLoader("", dir)
	require.NoError(t, loader.Load())

	tool, err := skills.NewActivateSkillTool(loader)
	require.NoError(t, err)

	out, err := tool.Call(context.Background(), `{"name":"rich-skill"}`)
	require.NoError(t, err)
	assert.Contains(t, out, "<skill_resources>")
	assert.True(t, strings.Contains(out, "run.sh"))
}

func TestNewActivateSkillTool_NoSkills(t *testing.T) {
	loader := skills.NewDefaultLoader("", t.TempDir())
	require.NoError(t, loader.Load())

	_, err := skills.NewActivateSkillTool(loader)
	assert.Error(t, err)
}

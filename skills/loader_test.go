package skills_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSkillFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(skillDir, 0755))
	path := filepath.Join(skillDir, "SKILL.md")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func TestLoad_SingleSkill(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "vuln-triage", `---
name: vuln-triage
description: Use when triaging vulnerabilities.
---

## Instructions
Step 1: check CVSS score.
`)

	loader := skills.NewDefaultLoader("", dir)
	require.NoError(t, loader.Load())

	list := loader.Skills()
	require.Len(t, list, 1)
	assert.Equal(t, "vuln-triage", list[0].Name)
	assert.Equal(t, "Use when triaging vulnerabilities.", list[0].Description)
	assert.Contains(t, list[0].Body, "Step 1: check CVSS score.")
	assert.Equal(t, filepath.Join(dir, "vuln-triage"), list[0].Dir)
}

func TestLoad_SymlinkedSkillDirectory(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "..2026_04_23_06_50_24")
	writeSkillFile(t, dataDir, "cve-impact-analysis", `---
name: cve-impact-analysis
description: Use when analyzing CVE impact.
---

Use tenant data to determine impact.
`)
	require.NoError(t, os.Symlink(filepath.Base(dataDir), filepath.Join(dir, "..data")))
	require.NoError(t, os.Symlink(filepath.Join("..data", "cve-impact-analysis"), filepath.Join(dir, "cve-impact-analysis")))

	loader := skills.NewDefaultLoader("", dir)
	require.NoError(t, loader.Load())

	list := loader.Skills()
	require.Len(t, list, 1)
	assert.Equal(t, "cve-impact-analysis", list[0].Name)
	assert.Equal(t, "Use when analyzing CVE impact.", list[0].Description)
	assert.Equal(t, filepath.Join(dir, "cve-impact-analysis"), list[0].Dir)
}

func TestLoad_CollisionPriority(t *testing.T) {
	lowPriority := t.TempDir()
	highPriority := t.TempDir()

	writeSkillFile(t, lowPriority, "my-skill", `---
name: my-skill
description: Low priority version.
---
Low priority body.
`)
	writeSkillFile(t, highPriority, "my-skill", `---
name: my-skill
description: High priority version.
---
High priority body.
`)

	loader := skills.NewDefaultLoader("", lowPriority, highPriority)
	require.NoError(t, loader.Load())

	list := loader.Skills()
	require.Len(t, list, 1)
	assert.Equal(t, "High priority version.", list[0].Description)
	assert.Contains(t, list[0].Body, "High priority body.")
}

func TestLoad_FallbackNameFromDir(t *testing.T) {
	dir := t.TempDir()
	// SKILL.md with no frontmatter name — should fall back to directory name
	writeSkillFile(t, dir, "dir-name-skill", `---
description: A skill without an explicit name.
---
Body here.
`)

	loader := skills.NewDefaultLoader("", dir)
	require.NoError(t, loader.Load())

	list := loader.Skills()
	require.Len(t, list, 1)
	assert.Equal(t, "dir-name-skill", list[0].Name)
}

func TestLoad_MissingDescription(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "no-desc", `---
name: no-desc
---
Body without description.
`)

	loader := skills.NewDefaultLoader("", dir)
	// Should succeed even without description (warning is logged)
	require.NoError(t, loader.Load())

	list := loader.Skills()
	require.Len(t, list, 1)
	assert.Equal(t, "no-desc", list[0].Name)
	assert.Equal(t, "", list[0].Description)
}

func TestLoad_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "plain-skill", "Just plain markdown with no frontmatter.\n\nSome content.")

	loader := skills.NewDefaultLoader("", dir)
	require.NoError(t, loader.Load())

	list := loader.Skills()
	require.Len(t, list, 1)
	assert.Equal(t, "plain-skill", list[0].Name) // falls back to dir name
	assert.Contains(t, list[0].Body, "Just plain markdown")
}

func TestLoad_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	loader := skills.NewDefaultLoader("", dir)
	require.NoError(t, loader.Load())
	assert.Empty(t, loader.Skills())
}

func TestLoad_NonexistentDir(t *testing.T) {
	loader := skills.NewDefaultLoader("", "/nonexistent/path/that/does/not/exist")
	// Should not error — missing directories are silently skipped
	require.NoError(t, loader.Load())
	assert.Empty(t, loader.Skills())
}

func TestLoad_MultipleSkillsAlphabetical(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"zebra-skill", "alpha-skill", "middle-skill"} {
		writeSkillFile(t, dir, name, "---\nname: "+name+"\ndescription: "+name+" desc.\n---\nbody")
	}

	loader := skills.NewDefaultLoader("", dir)
	require.NoError(t, loader.Load())

	list := loader.Skills()
	require.Len(t, list, 3)
	assert.Equal(t, "alpha-skill", list[0].Name)
	assert.Equal(t, "middle-skill", list[1].Name)
	assert.Equal(t, "zebra-skill", list[2].Name)
}

func TestCatalog_Empty(t *testing.T) {
	loader := skills.NewDefaultLoader("", t.TempDir())
	require.NoError(t, loader.Load())
	assert.Equal(t, "", loader.Catalog())
}

func TestCatalog_Format(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "skill-a", "---\nname: skill-a\ndescription: Does A things.\n---\nbody")
	writeSkillFile(t, dir, "skill-b", "---\nname: skill-b\ndescription: Does B things.\n---\nbody")

	loader := skills.NewDefaultLoader("", dir)
	require.NoError(t, loader.Load())

	catalog := loader.Catalog()
	assert.True(t, strings.HasPrefix(catalog, "<skills_catalog>"))
	assert.True(t, strings.HasSuffix(catalog, "</skills_catalog>"))
	assert.Contains(t, catalog, "- skill-a: Does A things.")
	assert.Contains(t, catalog, "- skill-b: Does B things.")
}

func TestGet(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "my-skill", "---\nname: my-skill\ndescription: Test.\n---\nbody")

	loader := skills.NewDefaultLoader("", dir)
	require.NoError(t, loader.Load())

	s := loader.Get("my-skill")
	require.NotNil(t, s)
	assert.Equal(t, "my-skill", s.Name)

	assert.Nil(t, loader.Get("nonexistent"))
}

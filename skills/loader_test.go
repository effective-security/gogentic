package skills_test

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
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

func TestLoad_ParseConfig(t *testing.T) {
	t.Parallel()
	cfg, err := skills.LoadConfig("testdata/skills.yaml")
	require.NoError(t, err)
	assert.Equal(t, true, cfg.EnableDefaultSkills)
	assert.Equal(t, false, cfg.EnableStandardPaths)
	assert.Equal(t, []string{"testdata/skills"}, cfg.Paths)
}

func TestLoad_Filter(t *testing.T) {
	t.Parallel()
	list := skills.Skills{
		{Name: "vuln-triage", Description: "Use when triaging vulnerabilities.", Tags: []string{"t1", "t2"}},
		{Name: "cve-impact-analysis", Description: "Use when analyzing CVE impact.", Tags: []string{"t2", "t3"}},
		{Name: "cve-research", Description: "Use when analyzing CVE impact.", Tags: []string{"t4"}},
	}
	assert.Equal(t, 1, len(list.Filter("vuln-triage")))
	assert.Equal(t, 1, len(list.Filter("cve-impact-analysis")))
	assert.Equal(t, 1, len(list.Filter("vuln-triage", "t1")))
	assert.Equal(t, 1, len(list.Filter("cve-impact-analysis", "t2", "t3")))
	assert.Equal(t, 1, len(list.Filter("", "t2", "t3")))
	assert.Equal(t, 2, len(list.Filter("", "t2")))
	assert.Equal(t, 1, len(list.Filter("", "t4")))
	assert.Equal(t, 3, len(list.Filter("")))
}

func TestLoad_SingleSkill(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSkillFile(t, dir, "vuln-triage", `---
name: vuln-triage
description: Use when triaging vulnerabilities.
agents: a1
---

## Instructions
Step 1: check CVSS score.
`)

	cfg := &skills.Config{
		EnableDefaultSkills: true,
		EnableStandardPaths: false,
		Paths:               []string{dir},
	}

	loader, err := skills.NewLoader(cfg, "")
	require.NoError(t, err)

	list := loader.Skills("a1")
	require.Len(t, list, 1)
	assert.Equal(t, "vuln-triage", list[0].Name)
	assert.Equal(t, "Use when triaging vulnerabilities.", list[0].Description)
	assert.Contains(t, list[0].Body, "Step 1: check CVSS score.")
	assert.Equal(t, "/vuln-triage", list[0].Location)
}

func TestLoad_SymlinkedSkillDirectory(t *testing.T) {
	t.Parallel()
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

	cfg := &skills.Config{
		EnableDefaultSkills: true,
		EnableStandardPaths: false,
		Paths:               []string{dir},
	}
	loader, err := skills.NewLoader(cfg, "")
	require.NoError(t, err)

	list := loader.Skills("")
	require.Len(t, list, 1)
	assert.Equal(t, "cve-impact-analysis", list[0].Name)
	assert.Equal(t, "Use when analyzing CVE impact.", list[0].Description)
	assert.Equal(t, "/cve-impact-analysis", list[0].Location)
}

func TestLoad_CollisionPriority(t *testing.T) {
	t.Parallel()
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

	cfg := &skills.Config{
		EnableDefaultSkills: true,
		EnableStandardPaths: false,
		Paths:               []string{lowPriority, highPriority},
	}
	loader, err := skills.NewLoader(cfg, "")
	require.NoError(t, err)

	list := loader.Skills("")
	require.Len(t, list, 1)
	assert.Equal(t, "High priority version.", list[0].Description)
	assert.Contains(t, list[0].Body, "High priority body.")
}

func TestLoad_FallbackNameFromDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// SKILL.md with no frontmatter name — should fall back to directory name
	writeSkillFile(t, dir, "dir-name-skill", `---
description: A skill without an explicit name.
---
Body here.
`)

	cfg := &skills.Config{
		EnableDefaultSkills: true,
		EnableStandardPaths: false,
		Paths:               []string{dir},
	}
	loader, err := skills.NewLoader(cfg, "")
	require.NoError(t, err)

	list := loader.Skills("")
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

	cfg := &skills.Config{
		EnableDefaultSkills: true,
		EnableStandardPaths: false,
		Paths:               []string{dir},
	}
	loader, err := skills.NewLoader(cfg, "")
	// Should succeed even without description (warning is logged)
	require.NoError(t, err)

	// Should not load without description
	list := loader.Skills("")
	require.Len(t, list, 0)
}

func TestLoad_NoFrontmatter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSkillFile(t, dir, "plain-skill", "Just plain markdown with no frontmatter.\n\nSome content.")

	cfg := &skills.Config{
		EnableDefaultSkills: true,
		EnableStandardPaths: false,
		Paths:               []string{dir},
	}
	loader, err := skills.NewLoader(cfg, "")
	require.NoError(t, err)

	list := loader.Skills("")
	require.Len(t, list, 1)
	assert.Equal(t, "plain-skill", list[0].Name) // falls back to dir name
	assert.Contains(t, list[0].Body, "Just plain markdown")
}

func TestLoad_EmptyDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := &skills.Config{
		EnableDefaultSkills: true,
		EnableStandardPaths: false,
		Paths:               []string{dir},
	}
	loader, err := skills.NewLoader(cfg, "")
	require.NoError(t, err)
	assert.Empty(t, loader.Skills(""))
}

func TestLoad_NonexistentDir(t *testing.T) {
	t.Parallel()
	cfg := &skills.Config{
		EnableDefaultSkills: true,
		EnableStandardPaths: false,
		Paths:               []string{"/nonexistent/path/that/does/not/exist"},
	}
	loader, err := skills.NewLoader(cfg, "")
	// Should not error — missing directories are silently skipped
	require.NoError(t, err)
	assert.Empty(t, loader.Skills(""))
}

func TestLoad_MultipleSkillsAlphabetical(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"zebra-skill", "alpha-skill", "middle-skill"} {
		writeSkillFile(t, dir, name, "---\nname: "+name+"\ndescription: "+name+" desc.\n---\nbody")
	}

	cfg := &skills.Config{
		EnableDefaultSkills: true,
		EnableStandardPaths: false,
		Paths:               []string{dir},
	}
	loader, err := skills.NewLoader(cfg, "")
	require.NoError(t, err)

	list := loader.Skills("")
	require.Len(t, list, 3)
	assert.Equal(t, "alpha-skill", list[0].Name)
	assert.Equal(t, "middle-skill", list[1].Name)
	assert.Equal(t, "zebra-skill", list[2].Name)
}

func Test_LoadByAgent_NoDefault(t *testing.T) {
	t.Parallel()
	cfg := &skills.Config{
		EnableDefaultSkills: false,
		EnableStandardPaths: false,
		Paths:               []string{"./testdata/skills"},
	}
	loader, err := skills.NewLoader(cfg, "")
	require.NoError(t, err)

	agents := loader.Agents()
	assert.Equal(t, []string{"agent-foo"}, agents)

	//default
	skillList := loader.Skills("")
	require.Len(t, skillList, 0)

	skillList = loader.Skills("agent-foo")
	require.Equal(t, 2, len(skillList), "One default and two agent-foo skills")
	assert.Equal(t, "/.agent-foo/foo-skill-1", skillList[0].Location)
	assert.Equal(t, "/.agent-foo/foo-skill-2", skillList[1].Location)
	assert.Equal(t, []string{"foo-skill-1", "foo-skill-2"}, skillList.Names())
	assert.Equal(t, []string{"RESOURCE-1.md", "scripts/run.sh"}, skillList[1].ListResources())
	res := skillList[1].LoadResources()
	require.Len(t, res, 2)

	skillList = loader.Skills("agent-bar")
	require.Equal(t, 0, len(skillList), "One default and one agent-bar skills")
}

func Test_LoadByAgent_WithDefault(t *testing.T) {
	t.Parallel()
	cfg := &skills.Config{
		EnableDefaultSkills: true,
		EnableStandardPaths: false,
		Paths:               []string{"./testdata/skills"},
	}
	loader, err := skills.NewLoader(cfg, "")
	require.NoError(t, err)

	agents := loader.Agents()
	assert.Equal(t, []string{"agent-bar", "agent-foo", "agent-goo", "agent-moo", "default"}, agents)

	//default
	skillList := loader.Skills("")
	require.Len(t, skillList, 1)
	assert.Equal(t, "/skill-1", skillList[0].Location)

	skillList = loader.Skills("agent-foo")
	require.Equal(t, 3, len(skillList), "One default and two agent-foo skills")
	assert.Equal(t, "/.agent-foo/foo-skill-1", skillList[0].Location)
	assert.Equal(t, "/.agent-foo/foo-skill-2", skillList[1].Location)
	assert.Equal(t, "/skill-1", skillList[2].Location)
	assert.Equal(t, []string{"foo-skill-1", "foo-skill-2", "skill-1"}, skillList.Names())
	assert.Equal(t, []string{"RESOURCE-1.md", "scripts/run.sh"}, skillList[1].ListResources())
	res := skillList[1].LoadResources()
	require.Len(t, res, 2)

	skillList = loader.Skills("agent-bar")
	require.Equal(t, 2, len(skillList), "One default and one agent-bar skills")
	assert.Equal(t, "/skill-1", skillList[0].Location)
	assert.Equal(t, "/skill-2", skillList[1].Location)
	assert.Equal(t, []string{"agent-bar", "agent-moo", "agent-goo"}, skillList[1].Agents)
}

func Test_LoadFromTar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		file   string
		gzipIt bool
	}{
		{name: "plain_tar", file: "skills.tar", gzipIt: false},
		{name: "gzip_tar", file: "skills.tar.gz", gzipIt: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tarPath := filepath.Join(t.TempDir(), tc.file)
			buildSkillsTar(t, "./testdata/skills", tarPath, tc.gzipIt)

			cfg := &skills.Config{
				EnableDefaultSkills: true,
				EnableStandardPaths: false,
				Paths:               []string{tarPath},
			}
			loader, err := skills.NewLoader(cfg, "")
			require.NoError(t, err)

			// default
			skillList := loader.Skills("")
			require.Len(t, skillList, 1)
			assert.Equal(t, "/skill-1", skillList[0].Location)

			skillList = loader.Skills("agent-foo")
			require.Equal(t, 3, len(skillList), "One default and two agent-foo skills")
			assert.Equal(t, "/.agent-foo/foo-skill-1", skillList[0].Location)
			assert.Equal(t, "/.agent-foo/foo-skill-2", skillList[1].Location)
			assert.Equal(t, "/skill-1", skillList[2].Location)
			assert.Equal(t, []string{"foo-skill-1", "foo-skill-2", "skill-1"}, skillList.Names())

			// resources bundled with foo-skill-2 are discovered from the tar.
			assert.Equal(t, []string{"RESOURCE-1.md", "scripts/run.sh"}, skillList[1].ListResources())
			assert.Equal(t, []string{"EXAMPLES.md"}, skillList[0].ListResources())

			skillList = loader.Skills("agent-bar")
			require.Equal(t, 2, len(skillList), "One default and one agent-bar skills")
			assert.Equal(t, "/skill-1", skillList[0].Location)
			assert.Equal(t, "/skill-2", skillList[1].Location)
			assert.Equal(t, []string{"agent-bar", "agent-moo", "agent-goo"}, skillList[1].Agents)
		})
	}
}

func Test_LoadFromTar_Errors(t *testing.T) {
	t.Parallel()

	t.Run("missing_file", func(t *testing.T) {
		t.Parallel()
		cfg := &skills.Config{
			EnableDefaultSkills: true,
			EnableStandardPaths: false,
			Paths:               []string{filepath.Join(t.TempDir(), "missing.tar")},
		}
		_, err := skills.NewLoader(cfg, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open skills tar file")
	})

	t.Run("not_gzip", func(t *testing.T) {
		t.Parallel()
		// A plain (non-gzip) tar with a .tar.gz extension must fail the gzip reader.
		tarPath := filepath.Join(t.TempDir(), "bogus.tar.gz")
		cfg := &skills.Config{
			EnableDefaultSkills: true,
			EnableStandardPaths: false,
			Paths:               []string{tarPath},
		}
		buildSkillsTar(t, "./testdata/skills", tarPath, false)
		_, err := skills.NewLoader(cfg, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create gzip reader")
	})
}

// buildSkillsTar packs the contents of srcDir into a tar archive at dst,
// optionally gzip-compressed. Paths inside the archive are relative to srcDir.
func buildSkillsTar(t *testing.T, srcDir, dst string, gzipIt bool) {
	t.Helper()

	f, err := os.Create(dst)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	var w io.Writer = f
	var gw *gzip.Writer
	if gzipIt {
		gw = gzip.NewWriter(f)
		w = gw
	}
	tw := tar.NewWriter(w)

	err = filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		// tar uses forward slashes.
		name := filepath.ToSlash(rel)

		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = name
		if d.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	if gw != nil {
		require.NoError(t, gw.Close())
	}
}

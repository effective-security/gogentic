package skills_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/effective-security/gogentic/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	notesFile    = "notes.md"
	notesContent = "original notes"
)

func TestSkillsNamesEmpty(t *testing.T) {
	t.Parallel()

	var empty skills.Skills
	assert.Nil(t, empty.Names())
	assert.Nil(t, empty.NamesEnum())

	list := skills.Skills{
		{
			Name: "a",
		},
		{
			Name: "b",
		},
	}
	assert.Equal(t, []any{"a", "b"}, list.NamesEnum())
}

func TestSkillResourcesWithoutDirectory(t *testing.T) {
	t.Parallel()

	s := &skills.Skill{
		Name: "in-memory",
	}
	assert.Nil(t, s.ListResources())
	assert.Empty(t, s.LoadResources())
}

func TestSkillLoadResourcesSkipsUnreadableAndCaches(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	skillDir := filepath.Dir(writeSkillFile(t, dir, "res", validSkill))
	notesPath := filepath.Join(skillDir, notesFile)
	require.NoError(t, os.WriteFile(notesPath, []byte(notesContent), 0o600))
	require.NoError(t, os.Symlink("absent.md", filepath.Join(skillDir, "missing.md")))

	loader, err := skills.NewLoader(&skills.Config{
		EnableDefaultSkills: true,
		Paths:               []string{dir},
	}, "")
	require.NoError(t, err)
	list := loader.Skills("")
	require.Len(t, list, 1)
	skill := list[0]

	assert.Equal(t, []string{"missing.md", notesFile}, skill.ListResources())
	expected := map[string][]byte{
		notesFile: []byte(notesContent),
	}
	assert.Equal(t, expected, skill.LoadResources())

	// Resources are cached: later file changes are not observed.
	require.NoError(t, os.WriteFile(notesPath, []byte("changed"), 0o600))
	assert.Equal(t, expected, skill.LoadResources())
}

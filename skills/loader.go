package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/effective-security/xlog"
	"gopkg.in/yaml.v3"
)

// CatalogPromptKey is the template variable name injected into system prompts
// to expose the skills catalog. Clients must include this key in their
// PartialVariables (with a default of "") so the Go template engine does not
// error when no skills are loaded.
const CatalogPromptKey = "skills_catalog"

// Loader discovers and parses Agent Skills from the filesystem.
type Loader struct {
	clientName string
	searchDirs []string
	skills     map[string]*Skill
}

// NewDefaultLoader builds a Loader that scans the standard agentskills.io
// directories in ascending priority order:
//
//  1. ~/.agents/skills/
//  2. ~/.<clientName>/skills/
//  3. <cwd>/.agents/skills/
//  4. <cwd>/.<clientName>/skills/
//  5. searchDirs... (highest priority)
//
// Later directories override earlier ones when two skills share the same name,
// so project-level skills take precedence over user-level skills.
// clientName is optional; pass "" to skip client-specific paths.
func NewDefaultLoader(clientName string, searchDirs ...string) *Loader {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	var dirs []string

	// User-level (lowest priority)
	dirs = append(dirs, filepath.Join(home, ".agents", "skills"))
	if clientName != "" {
		dirs = append(dirs, filepath.Join(home, "."+clientName, "skills"))
	}

	// Project-level (overrides user-level on collision)
	dirs = append(dirs, filepath.Join(cwd, ".agents", "skills"))
	if clientName != "" {
		dirs = append(dirs, filepath.Join(cwd, "."+clientName, "skills"))
	}

	// Caller-supplied explicit overrides (highest priority)
	dirs = append(dirs, searchDirs...)

	return &Loader{
		clientName: clientName,
		searchDirs: dirs,
	}
}

// Load scans all configured directories for SKILL.md files and parses them.
// It is idempotent: repeated calls replace the previous results.
// Individual file errors are logged as warnings and do not abort the scan.
func (l *Loader) Load() error {
	l.skills = make(map[string]*Skill)

	for _, dir := range l.searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				logger.KV(xlog.WARNING, "reason", "read_dir", "dir", dir, "err", err.Error())
			}
			continue
		}

		for _, entry := range entries {
			// entry can be a symlink as well as a directory
			// so we need to stat the entry to get the actual path
			entryPath := filepath.Join(dir, entry.Name())
			info, err := os.Stat(entryPath)
			if err != nil {
				if !os.IsNotExist(err) {
					logger.KV(xlog.WARNING, "reason", "stat_entry", "path", entryPath, "err", err.Error())
				}
				continue
			}
			if !info.IsDir() {
				continue
			}
			skillMdPath := filepath.Join(dir, entry.Name(), "SKILL.md")
			if _, err := os.Stat(skillMdPath); os.IsNotExist(err) {
				continue
			}

			skill, err := parseSkillFile(skillMdPath)
			if err != nil {
				logger.KV(xlog.WARNING, "reason", "parse_skill_file", "path", skillMdPath, "err", err.Error())
				continue
			}

			// Later (higher-priority) dirs overwrite earlier ones on name collision.
			l.skills[skill.Name] = skill
		}
	}

	return nil
}

// Skills returns all loaded skills sorted alphabetically by name.
func (l *Loader) Skills() []*Skill {
	if len(l.skills) == 0 {
		return nil
	}
	skills := make([]*Skill, 0, len(l.skills))
	for _, s := range l.skills {
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})
	return skills
}

// Get returns the skill with the given name, or nil if not found.
func (l *Loader) Get(name string) *Skill {
	if l.skills == nil {
		return nil
	}
	return l.skills[name]
}

// Catalog returns a compact skills catalog string for injection into a system
// prompt. Each skill contributes one line (~50-100 tokens total for a typical
// set). Returns "" if no skills are loaded.
//
// Format:
//
//	<skills_catalog>
//	- skill-name: Description of what the skill does.
//	</skills_catalog>
func (l *Loader) Catalog() string {
	skills := l.Skills()
	if len(skills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("<skills_catalog>\n")
	for _, s := range skills {
		fmt.Fprintf(&sb, "- %s: %s\n", s.Name, s.Description)
	}
	sb.WriteString("</skills_catalog>")
	return sb.String()
}

// parseSkillFile reads and parses a SKILL.md file.
// If the frontmatter name is absent, the parent directory name is used as a fallback.
// Malformed YAML is logged as a warning but does not cause an error.
func parseSkillFile(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(path)
	skill := &Skill{
		Dir:  dir,
		Path: path,
		Name: filepath.Base(dir), // fallback if frontmatter name is absent
	}

	// Normalise line endings
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(content, "\n")

	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		skill.Body = strings.TrimSpace(content)
		return skill, nil
	}

	// Find the closing --- delimiter
	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closeIdx = i
			break
		}
	}
	if closeIdx < 0 {
		skill.Body = strings.TrimSpace(content)
		return skill, nil
	}

	// Parse YAML frontmatter
	frontmatter := strings.Join(lines[1:closeIdx], "\n")
	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(frontmatter), &fm); err != nil {
		logger.KV(xlog.WARNING, "reason", "parse_frontmatter", "path", path, "err", err.Error())
	} else {
		if fm.Name != "" {
			skill.Name = fm.Name
		}
		skill.Description = fm.Description
	}

	if skill.Description == "" {
		logger.KV(xlog.WARNING, "reason", "missing_description", "path", path, "name", skill.Name)
	}

	// Body is everything after the closing ---
	skill.Body = strings.TrimSpace(strings.Join(lines[closeIdx+1:], "\n"))

	return skill, nil
}

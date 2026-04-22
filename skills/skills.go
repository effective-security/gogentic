package skills

import "github.com/effective-security/xlog"

var logger = xlog.NewPackageLogger("github.com/effective-security/gogentic", "skills")

// Frontmatter holds the YAML metadata parsed from the header of a SKILL.md file.
type Frontmatter struct {
	Name          string            `yaml:"name"`
	Description   string            `yaml:"description"`
	License       string            `yaml:"license"`
	Compatibility string            `yaml:"compatibility"`
	AllowedTools  string            `yaml:"allowed-tools"`
	Metadata      map[string]string `yaml:"metadata"`
}

// Skill represents a parsed agent skill.
type Skill struct {
	Name        string
	Description string
	Dir         string // absolute path to the skill directory
	Path        string // absolute path to the SKILL.md file
	Body        string // markdown body after the frontmatter block
}

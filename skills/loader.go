package skills

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/x/configloader"
	"github.com/effective-security/x/maps"
	"github.com/effective-security/x/values"
	"github.com/effective-security/xlog"
	"gopkg.in/yaml.v3"
)

type Loader interface {
	// ClientName returns the name of the client that owns the skills.
	ClientName() string
	// Skills returns all loaded skills for the given agent sorted alphabetically by name.
	// Use tags to filter skills by tags. The Skill must have all the tags provided.
	Skills(agent string, tags ...string) Skills
	// Agents returns a list agents names for which skills are loaded.
	Agents() []string
}

// Config specifies the configuration for the loader.
// Example of the directory structure:
// ~/.agents/skills/
// ├── pdf-processing/
// │   ├── SKILL.md          ← discovered
// │   └── scripts/
// │       └── extract.py
// ├── data-analysis/
// │   └── SKILL.md          ← discovered
// └── README.md             ← ignored (not a skill directory)
//
// ~/.agents/.my-client/skills/
// ├── .analyzer-agent/      ← agent name
// │	└── pdf-processing/  ← skill name
// │       ├── SKILL.md      ← discovered
// │       └── scripts/
// │           └── extract.py
// ├── data-analysis/        ← skill name
// │   └── SKILL.md          ← discovered
// └── README.md             ← ignored (not a skill directory)
type Config struct {
	// Strict mode enforces that an error is returned if a skill is invalid or failed to parse.
	Strict bool `json:"strict,omitempty" yaml:"strict,omitempty"`
	// EnableDefaultSkills enables the default skills,
	// when the skill path does not include Agent name or Skill does not specify Agents field.
	EnableDefaultSkills bool `json:"enable_default_skills,omitempty" yaml:"enable_default_skills,omitempty"`
	// EnableStandardPaths enables to discover the standard paths for skills:
	//	~/.agents/skills/.<agent>/<skill>/...
	//	~/.agents/.<clientName>/skills/.<agent>/<skill>/...
	//	<cwd>/.agents/skills/.<agent>/<skill>/...
	//	<cwd>/.agents/.<clientName>/skills/.<agent>/<skill>/...
	//
	// If EnableStandardPaths is false, then only the Paths and Agents.Paths are used.
	EnableStandardPaths bool `json:"enable_standard_paths,omitempty" yaml:"enable_standard_paths,omitempty"`
	// Paths specifies the paths to search for skills.
	// The following pattern is applied to paths:
	//	./<skill>/...
	//	./.<agent>/<skill>/...
	// Later directories override earlier ones when two skills share the same name.
	Paths []string `json:"paths,omitempty" yaml:"paths,omitempty"`
	// Agents specifies override configuration for agents.
	// If an agent configuration is present, then it will override the default configuration.
	Agents map[string]*AgentConfig `json:"agents,omitempty" yaml:"agents,omitempty"`
}

type AgentConfig struct {
	// Disabled specifies to skip the agent to load skills.
	Disabled bool `json:"disabled,omitempty" yaml:"disabled,omitempty"`
	// Paths specifies additional paths to search for skills for the agent.
	// Later directories override earlier ones when two skills share the same name.
	Paths []string `json:"paths,omitempty" yaml:"paths,omitempty"`
}

func LoadConfig(path string) (*Config, error) {
	var cfg Config
	if err := configloader.Unmarshal(path, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Loader discovers and parses Agent Skills from the filesystem.
type loader struct {
	cfg         *Config
	clientName  string
	searchDirs  []string
	agentSkills map[string]map[string]*Skill // agent -> skill name -> skill
}

// NewLoader returns a Loader instance.
//
// `clientName` parameter is optional; pass "" to skip client-specific paths,
// otherwise only client specific skills are loaded.
func NewLoader(cfg *Config, clientName string) (Loader, error) {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	var dirs []string

	if cfg.EnableStandardPaths {
		// User-level (lowest priority)
		if clientName != "" {
			dirs = append(dirs, filepath.Join(home, ".agents", "."+clientName, "skills"))
			// Project-level (overrides user-level on collision)
			dirs = append(dirs, filepath.Join(cwd, ".agents", "."+clientName, "skills"))
		} else {
			dirs = append(dirs, filepath.Join(home, ".agents", "skills"))
			// Project-level (overrides user-level on collision)
			dirs = append(dirs, filepath.Join(cwd, ".agents", "skills"))
		}
	}

	// Caller-supplied explicit overrides (highest priority)
	for _, dir := range cfg.Paths {
		dirs = append(dirs, filepath.Clean(dir))
	}

	l := &loader{
		cfg:         cfg,
		clientName:  values.StringsCoalesce(clientName, "default"),
		searchDirs:  dirs,
		agentSkills: make(map[string]map[string]*Skill),
	}
	err := l.load()
	if err != nil {
		return nil, err
	}
	return l, nil
}

func (l *loader) ClientName() string {
	return l.clientName
}

// Agents returns a list agents names for which skills are loaded.
func (l *loader) Agents() []string {
	return maps.OrderedKeys(l.agentSkills)
}

// Load scans all configured directories for SKILL.md files and parses them.
// It is idempotent: repeated calls replace the previous results.
// Individual file errors are logged as warnings and do not abort the scan.
func (l *loader) load() error {
	for _, dir := range l.searchDirs {
		if strings.HasSuffix(dir, ".tar") || strings.HasSuffix(dir, ".tar.gz") {
			err := l.loadTar(dir)
			if err != nil {
				return errors.Wrap(err, "failed to load tar file")
			}
			continue
		}
		err := l.loadFolder("", dir)
		if err != nil {
			return errors.Wrap(err, "failed to load directory")
		}
	}

	return nil
}

func (l *loader) loadTar(file string) error {
	f, err := os.Open(file)
	if err != nil {
		return errors.Wrap(err, "failed to open skills tar file")
	}
	defer func() { _ = f.Close() }()

	var r io.Reader

	if strings.HasSuffix(file, ".gz") || strings.HasSuffix(file, ".gzip") {
		gw, err := gzip.NewReader(f)
		if err != nil {
			return errors.Wrap(err, "failed to create gzip reader")
		}
		defer func() { _ = gw.Close() }()
		r = gw
	} else {
		r = f
	}

	// Tar is a stream, so we cannot know which files belong to which skill
	// until we have seen all entries. Buffer all regular files (keyed by their
	// cleaned, slash-separated path), then resolve skills and resources.
	files := make(map[string][]byte)
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Wrap(err, "failed to read tar file")
		}
		if hdr == nil {
			break
		}
		if hdr.FileInfo().IsDir() {
			continue
		}

		// tar always uses forward slashes regardless of the host OS.
		name := strings.TrimPrefix(path.Clean(hdr.Name), "./")
		if name == "" || name == "." {
			continue
		}
		// Reject path traversal entries.
		if name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
			logger.KV(xlog.WARNING,
				"reason", "tar_unsafe_path",
				"path", hdr.Name,
			)
			continue
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return errors.Wrapf(err, "failed to read tar entry: %s", hdr.Name)
		}
		files[name] = data
	}

	// Collect SKILL.md entries and process them in a deterministic order so that
	// name collisions resolve predictably.
	var skillPaths []string
	for name := range files {
		if path.Base(name) == "SKILL.md" {
			skillPaths = append(skillPaths, name)
		}
	}
	sort.Strings(skillPaths)

	for _, skillPath := range skillPaths {
		skillDir := path.Dir(skillPath)
		if skillDir == "." {
			// SKILL.md at the tar root has no enclosing skill folder; skip it.
			continue
		}

		// Mirror loadFolder semantics:
		//   <skill>/SKILL.md           -> default agent
		//   <agent>/<skill>/SKILL.md   -> agent is the folder enclosing the skill
		var agent string
		parts := strings.Split(skillDir, "/")
		if len(parts) >= 2 {
			agent = strings.TrimPrefix(parts[len(parts)-2], ".")
		}

		skill, err := parseSkillContent(string(files[skillPath]), path.Base(skillDir), skillPath)
		if err != nil {
			logger.KV(xlog.WARNING,
				"reason", "parse_skill_file",
				"path", skillPath,
				"err", err.Error(),
			)
			continue
		}

		skill.dir = skillDir
		skill.path = skillPath
		skill.Location = "/" + skillDir
		skill.resources = tarResources(files, skillDir)
		skill.resourceNames = maps.OrderedKeys(skill.resources)

		l.addSkill(agent, skill)
	}

	return nil
}

// tarResources returns the resource paths bundled with a skill, relative to the
// skill directory, excluding the SKILL.md file itself.
func tarResources(files map[string][]byte, skillDir string) map[string][]byte {
	prefix := skillDir + "/"
	resources := make(map[string][]byte)
	for name := range files {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rel := strings.TrimPrefix(name, prefix)
		if rel == "SKILL.md" {
			continue
		}
		resources[rel] = files[name]
	}
	return resources
}

// addSkill registers a parsed skill under the agents declared in its frontmatter,
// or under the supplied agent (defaulting to "default") when none are declared.
func (l *loader) addSkill(agent string, skill *Skill) {
	if agent != "" {
		if ac := l.cfg.Agents[agent]; ac != nil && ac.Disabled {
			return
		}
	} else if !l.cfg.EnableDefaultSkills {
		return
	}

	if len(skill.Agents) > 0 {
		for _, a := range skill.Agents {
			if ac := l.cfg.Agents[a]; ac != nil && ac.Disabled {
				continue
			}
			if l.agentSkills[a] == nil {
				l.agentSkills[a] = make(map[string]*Skill)
			}
			l.agentSkills[a][skill.Name] = skill
		}
		return
	}

	agentName := values.StringsCoalesce(agent, "default")
	if l.agentSkills[agentName] == nil {
		l.agentSkills[agentName] = make(map[string]*Skill)
	}
	l.agentSkills[agentName][skill.Name] = skill
}

func (l *loader) loadFolder(parentAgent, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.Wrap(err, "failed to read directory")
		}
		return nil
	}

	for _, entry := range entries {
		name := entry.Name()
		isAgentFolder := strings.HasPrefix(name, ".")
		entryPath := filepath.Join(dir, name)
		agent := parentAgent
		if isAgentFolder {
			agent = name[1:]
		}

		// entry can be a symlink as well as a directory
		// so we need to stat the entry to get the actual path
		info, err := os.Stat(entryPath)
		if err != nil {
			if !os.IsNotExist(err) {
				logger.KV(xlog.WARNING,
					"reason", "stat_entry",
					"path", entryPath,
					"err", err.Error(),
				)
			}
			continue
		}
		if !info.IsDir() {
			continue
		}

		if isAgentFolder {
			err = l.loadFolder(agent, entryPath)
			if err != nil {
				logger.KV(xlog.WARNING,
					"reason", "stat_entry",
					"path", entryPath,
					"err", err.Error(),
				)
			}
			continue
		}

		skillMdPath := filepath.Join(entryPath, "SKILL.md")
		if _, err := os.Stat(skillMdPath); os.IsNotExist(err) {
			continue
		}

		skill, err := parseSkillFile(skillMdPath)
		if err != nil {
			logger.KV(xlog.WARNING,
				"reason", "parse_skill_file",
				"path", skillMdPath,
				"err", err.Error(),
			)
			if l.cfg.Strict {
				return errors.WithMessagef(err, "failed to parse skill file: %s", skillMdPath)
			}
			continue
		}
		root := values.Select(agent != "", "/."+agent, "/")
		rel, err := filepath.Rel(dir, skill.dir)
		if err != nil {
			rel = strings.TrimPrefix(skill.dir, dir)
		}
		skill.Location = path.Join(root, filepath.ToSlash(rel))
		l.addSkill(agent, skill)
	}
	return nil
}

// Skills returns all loaded skills for the given agent sorted alphabetically by name.
// If agent is empty, all skills are returned.
func (l *loader) Skills(agent string, tags ...string) Skills {
	merged := make(map[string]*Skill)
	if l.cfg.EnableDefaultSkills {
		for name, skill := range l.agentSkills["default"] {
			merged[name] = skill
		}
	}
	if agent != "" && agent != "default" {
		if ac := l.cfg.Agents[agent]; ac != nil && ac.Disabled {
			return nil
		}
		for name, skill := range l.agentSkills[agent] {
			merged[name] = skill
		}
	}

	var skills Skills
	for _, skill := range merged {
		if agent == "" || len(skill.Agents) == 0 || slices.Contains(skill.Agents, agent) {
			skills = append(skills, skill)
		}
	}

	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})
	return skills.Filter("", tags...)
}

// parseSkillFile reads and parses a SKILL.md file.
// If the frontmatter name is absent, the parent directory name is used as a fallback.
func parseSkillFile(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read skill file")
	}

	dir := filepath.Dir(path)
	skill, err := parseSkillContent(string(data), filepath.Base(dir), path)
	if err != nil {
		return nil, err
	}
	skill.dir = dir
	skill.path = path
	return skill, nil
}

// parseSkillContent parses the raw content of a SKILL.md file.
// fallbackName is used as the skill name when the frontmatter omits one.
// source is a human readable identifier (file path or tar entry) used in error messages.
func parseSkillContent(data, fallbackName, source string) (*Skill, error) {
	skill := &Skill{
		Name: fallbackName, // fallback if frontmatter name is absent
	}

	// Normalise line endings
	content := strings.ReplaceAll(data, "\r\n", "\n")
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
		return nil, errors.Wrapf(err, "failed to parse frontmatter: %s", source)
	}

	if fm.Description == "" {
		return nil, errors.Errorf("invalid frontmatter: description is required: %s", source)
	}
	if fm.Name != "" {
		skill.Name = fm.Name
	}
	skill.Description = fm.Description

	// Body is everything after the closing ---
	skill.Body = strings.TrimSpace(strings.Join(lines[closeIdx+1:], "\n"))

	skill.AllowedTools = splitBySpaceOrComma(fm.AllowedTools)
	skill.Agents = splitBySpaceOrComma(fm.Agents)
	skill.Tags = splitBySpaceOrComma(fm.Tags)

	return skill, nil
}

func splitBySpaceOrComma(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ','
	})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

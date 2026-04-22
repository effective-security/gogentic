package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/invopop/jsonschema"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

// ActivateSkillToolName is the name registered with the LLM.
const ActivateSkillToolName = "activate_skill"

// ActivateSkillRequest is the JSON input expected by the tool.
type ActivateSkillRequest struct {
	Name string `json:"name"`
}

// ActivateSkillTool implements tools.ITool. It loads and returns the full
// instructions for the named skill (tier-2 progressive disclosure).
type ActivateSkillTool struct {
	loader     *Loader
	funcParams *jsonschema.Schema
}

// NewActivateSkillTool creates the tool. The JSON schema for the "name"
// parameter is built with an enum constraint listing all available skill names,
// so the LLM receives concrete choices rather than a freeform string.
// Returns an error if loader is nil or has no skills loaded.
func NewActivateSkillTool(loader *Loader) (*ActivateSkillTool, error) {
	if loader == nil {
		return nil, errors.New("loader is required")
	}

	skills := loader.Skills()
	if len(skills) == 0 {
		return nil, errors.New("no skills loaded")
	}

	enum := make([]any, len(skills))
	for i, s := range skills {
		enum[i] = s.Name
	}

	props := orderedmap.New[string, *jsonschema.Schema]()
	props.Set("name", &jsonschema.Schema{
		Type:        "string",
		Description: "Name of the skill to activate",
		Enum:        enum,
	})

	return &ActivateSkillTool{
		loader: loader,
		funcParams: &jsonschema.Schema{
			Type:       "object",
			Properties: props,
			Required:   []string{"name"},
		},
	}, nil
}

func (t *ActivateSkillTool) Name() string {
	return ActivateSkillToolName
}

func (t *ActivateSkillTool) Description() string {
	return "Load the full instructions for an agent skill by name. When the user's request matches a skill's description, activate it before proceeding to load detailed task-specific instructions."
}

func (t *ActivateSkillTool) Parameters() *jsonschema.Schema {
	return t.funcParams
}

// Call parses the JSON input, looks up the skill, and returns its full body
// wrapped in <skill_content> tags along with a listing of bundled resources.
func (t *ActivateSkillTool) Call(_ context.Context, input string) (string, error) {
	var req ActivateSkillRequest
	if err := json.Unmarshal(llmutils.CleanJSON([]byte(input)), &req); err != nil {
		return "", errors.WithStack(chatmodel.ErrFailedUnmarshalInput)
	}

	skill := t.loader.Get(req.Name)
	if skill == nil {
		names := make([]string, 0, len(t.loader.Skills()))
		for _, s := range t.loader.Skills() {
			names = append(names, s.Name)
		}
		return fmt.Sprintf("skill %q not found; available skills: %s", req.Name, strings.Join(names, ", ")), nil
	}

	resources := listSkillResources(skill.Dir)

	var sb strings.Builder
	fmt.Fprintf(&sb, "<skill_content name=%q dir=%q>\n", skill.Name, skill.Dir)
	sb.WriteString(skill.Body)
	if len(resources) > 0 {
		sb.WriteString("\n\n<skill_resources>\n")
		for _, r := range resources {
			fmt.Fprintf(&sb, "  <file>%s</file>\n", r)
		}
		sb.WriteString("</skill_resources>")
	}
	sb.WriteString("\n</skill_content>")

	return sb.String(), nil
}

// listSkillResources returns relative paths to all non-SKILL.md files in the
// skill directory, including files in subdirectories (scripts/, references/, assets/).
func listSkillResources(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			subEntries, _ := os.ReadDir(filepath.Join(dir, entry.Name()))
			for _, sub := range subEntries {
				if !sub.IsDir() {
					files = append(files, filepath.Join(entry.Name(), sub.Name()))
				}
			}
		} else if entry.Name() != "SKILL.md" {
			files = append(files, entry.Name())
		}
	}
	return files
}

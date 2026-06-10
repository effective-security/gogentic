package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/x/maps"
	"github.com/invopop/jsonschema"
	orderedmap "github.com/pb33f/ordered-map/v2"
)

// ActivateSkillToolName is the name registered with the LLM.
const ActivateSkillToolName = "activate_skill"

// ActivateSkillRequest is the JSON input expected by the tool.
type ActivateSkillRequest struct {
	Name string `json:"name" jsonschema:"required,title=Name,description=The name of the skill to activate."`
}

type ActivateSkillResponse struct {
	Skill        string `json:"skill,omitempty"`
	Instructions string `json:"instructions,omitempty"`
	Location     string `json:"location,omitempty"`
	//Resources    []string `json:"resources,omitempty"`
}

type ActivateSkillErrorResponse struct {
	Error ActivateSkillError `json:"error,omitempty"`
}
type ActivateSkillError struct {
	Code            string `json:"code,omitempty"`
	Message         string `json:"message,omitempty"`
	AvailableSkills string `json:"available_skills,omitempty"`
}

// ActivateSkillTool implements tools.ITool. It loads and returns the full
// instructions for the named skill (tier-2 progressive disclosure).
type ActivateSkillTool struct {
	skillsByName map[string]*Skill
	funcParams   *jsonschema.Schema
}

// NewActivateSkillTool creates the tool.
// The JSON schema for the "name" parameter is built with an enum constraint listing all available skill names,
// so the LLM receives concrete choices rather than a freeform string.
// Returns an error if loader is nil or has no skills loaded.
func NewActivateSkillTool(skills Skills) (*ActivateSkillTool, error) {
	if len(skills) == 0 {
		return nil, errors.New("no skills provided")
	}

	props := orderedmap.New[string, *jsonschema.Schema]()
	props.Set("name", &jsonschema.Schema{
		Type:        "string",
		Description: "Name of the skill to activate",
		Enum:        skills.NamesEnum(),
	})

	t := &ActivateSkillTool{
		skillsByName: make(map[string]*Skill),
		funcParams: &jsonschema.Schema{
			Type:       "object",
			Properties: props,
			Required:   []string{"name"},
		},
	}
	for _, skill := range skills {
		t.skillsByName[skill.Name] = skill
	}

	return t, nil
}

func (t *ActivateSkillTool) Name() string {
	return ActivateSkillToolName
}

func (t *ActivateSkillTool) Description() string {
	return "Load the skill by name when the user's request matches a skill description, then follow the instructions."
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

	skill := t.skillsByName[req.Name]
	if skill == nil {
		available := maps.OrderedKeys(t.skillsByName)
		err := ActivateSkillError{
			Code:            "skill_not_found",
			Message:         fmt.Sprintf("skill %q not found", req.Name),
			AvailableSkills: strings.Join(available, ", "),
		}
		res := ActivateSkillErrorResponse{
			Error: err,
		}
		return llmutils.ToJSON(res), nil
	}

	res := ActivateSkillResponse{
		Skill:        req.Name,
		Location:     skill.Location,
		Instructions: skill.Body,
		//res.Resources = skill.ListResources()
	}

	return llmutils.ToJSON(res), nil
}

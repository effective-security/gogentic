package skills_test

import (
	"context"
	"testing"

	"github.com/effective-security/gogentic/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActivateSkillTool_Call_Valid(t *testing.T) {
	ctx := context.Background()
	body := `
## Steps
1. Check CVSS score.
2. Check exploitability.
`
	skillList := skills.Skills{
		{
			Name:     "vuln-triage",
			Body:     body,
			Location: "testdata/vuln-triage",
		},
	}

	tool, err := skills.NewActivateSkillTool(skillList)
	require.NoError(t, err)

	out, err := tool.Call(ctx, `{"name":"vuln-triage"}`)
	require.NoError(t, err)

	exp := `{"skill":"vuln-triage","instructions":"\n## Steps\n1. Check CVSS score.\n2. Check exploitability.\n","location":"testdata/vuln-triage"}`

	assert.Equal(t, exp, out)

	out, err = tool.Call(ctx, `{"name":"nonexistent"}`)
	require.NoError(t, err)
	exp2 := `{"error":{"code":"skill_not_found","message":"skill \"nonexistent\" not found","available_skills":"vuln-triage"}}`
	assert.Equal(t, exp2, out)
}

func TestActivateSkillTool_ParametersEnum(t *testing.T) {
	skillList := skills.Skills{
		{
			Name: "skill-one",
			Body: "---\nname: skill-one\ndescription: One.\n---\nbody",
		},
		{
			Name: "skill-two",
			Body: "---\nname: skill-two\ndescription: Two.\n---\nbody",
		},
	}

	tool, err := skills.NewActivateSkillTool(skillList)
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
	cfg := &skills.Config{
		EnableDefaultSkills: true,
		EnableStandardPaths: false,
		Paths:               []string{"./testdata/skills"},
	}
	loader, err := skills.NewLoader(cfg, "")
	require.NoError(t, err)

	skillList := loader.Skills("agent-foo")
	assert.Len(t, skillList, 3)

	tool, err := skills.NewActivateSkillTool(skillList)
	require.NoError(t, err)

	out, err := tool.Call(context.Background(), `{"name":"foo-skill-2"}`)
	require.NoError(t, err)
	exp := `{"skill":"foo-skill-2","instructions":"# Header\n\nInstructions","location":"/.agent-foo/foo-skill-2"}`

	assert.Equal(t, exp, out)
}

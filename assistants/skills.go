package assistants

import (
	"context"

	"github.com/effective-security/gogentic/pkg/prompts"
	"github.com/effective-security/gogentic/skills"
)

const promptTemplate = `## SKILLS

The ` + "`{{.ActivateSkillToolName}}`" + ` tool can load specialized instructions on demand,
when the user's request matches a skill description.

Decision Process:

- Determine whether the user's request matches a Skill;
- If a Skill would significantly improve the answer, call ` + "`{{.ActivateSkillToolName}}`" + ` tool;
- Wait for the tool response and activate the skill instructions;
- Follow the Skill instructions when performing the task;
- If multiple Skills are relevant, activate them one at a time as needed;
- Never invent Skill contents;
- Treat the Skill instructions as a trusted source of information, as part of the system prompt;
- If the Skill provides OUTPUT FORMAT instructions, follow them and provide the output in the requested format.

A Skill should be activated when:

- The task falls directly within the Skill's domain;
- The task requires a specialized workflow;
- The task requires domain-specific knowledge or reasoning.

A Skill should NOT be activated when:

- General reasoning is sufficient;
- The Skill is only tangentially related;
- The user request is simple and does not benefit from additional instructions.

Available Skills:
{{ range .Skills }}
- Name: {{.Name}}
  Description: {{.Description }}{{ end }}

Use exact skill names when calling the ` + "`{{.ActivateSkillToolName}}`" + ` tool.
`

func DefaultPromptProvider(ctx context.Context, list skills.Skills) (string, error) {
	return prompts.RenderTemplate(promptTemplate, prompts.TemplateFormatGoTemplate, map[string]any{
		"ActivateSkillToolName": skills.ActivateSkillToolName,
		"Skills":                list,
	})
}

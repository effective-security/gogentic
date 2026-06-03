package assistants

import (
	"context"

	"github.com/effective-security/gogentic/pkg/prompts"
	"github.com/effective-security/gogentic/skills"
)

const promptTemplate = `# SKILLS

The ` + "`{{.ActivateSkillToolName}}`" + ` tool can load specialized instructions on demand.

Available Skills:

{{ range .Skills -}}
- {{.Name}}
  Description: {{.Description}}
{{- end}}

Decision Process:

1. Determine whether the user's request matches a Skill.
2. If a Skill would significantly improve the answer, call ` + "`{{.ActivateSkillToolName}}`" + ` tool.
3. Wait for the Skill instructions.
4. Follow the Skill instructions when performing the task.
5. If multiple Skills are relevant, activate them one at a time as needed.
6. Never invent Skill contents.

A Skill should be activated when:
- The task falls directly within the Skill's domain.
- The task requires a specialized workflow.
- The task requires domain-specific knowledge or reasoning.

A Skill should NOT be activated when:
- General reasoning is sufficient.
- The Skill is only tangentially related.
- The user request is simple and does not benefit from additional instructions.
`

func DefaultPromptProvider(ctx context.Context, list skills.Skills) (string, error) {
	return prompts.RenderTemplate(promptTemplate, prompts.TemplateFormatGoTemplate, map[string]any{
		"ActivateSkillToolName": skills.ActivateSkillToolName,
		"Skills":                list,
	})
}

package assistants_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/callbacks"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/encoding"
	"github.com/effective-security/gogentic/mocks/mockllms"
	"github.com/effective-security/gogentic/pkg/llmfactory"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/pkg/prompts"
	"github.com/effective-security/gogentic/skills"
	gstore "github.com/effective-security/gogentic/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func Test_Skills_Prompt(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	skilslList := skills.Skills{
		{
			Name:        "search-web",
			Description: "Use ONLY when user asks to search the web for information.",
			Body:        `Search the web for information.`,
		},
		{
			Name:        "vulnerability-triage",
			Description: "Use when user asks to triage, prioritize, or assess risk of vulnerabilities in their cloud environment.",
			Body: `
## How to handle this question

Follow these steps in order:

1. **Severity gate** — focus only on findings with CVSS score >= 7 (high or critical).
2. **Exploit check** — cross-reference each finding against CISA KEV; mark those with a known exploit as "exploit-available".
3. **Exposure check** — determine whether the affected asset is internet-facing or has public exposure.
4. **Blast radius** — assess potential lateral movement if the asset were compromised.
5. **Prioritized list** — rank findings: internet-facing + known exploit > internet-facing > internal + known exploit > internal.
6. **Output format** — for each finding output: rank, CVE ID, CVSS, exploit-available (yes/no), exposure (internet/internal), brief remediation suggestion.
`,
		},
	}

	sysprompt := prompts.PromptTemplate{
		Template: `You are a cloud security assistant that helps analysts investigate and triage vulnerabilities.
When the user's request matches a skill's description, activate it with the activate_skill tool before answering.`,
		InputVariables: []string{},
		TemplateFormat: prompts.TemplateFormatGoTemplate,
	}

	// Create a mock LLM
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).AnyTimes()

	ag := assistants.NewAssistant[chatmodel.String](
		mockLLM,
		sysprompt,
		assistants.WithMode(encoding.ModePlainText),
	).WithSkills(skilslList)

	sysPrompt, err := ag.GetSystemPrompt(context.Background(), "", nil)
	require.NoError(t, err)

	exp := `You are a cloud security assistant that helps analysts investigate and triage vulnerabilities.
When the user's request matches a skill's description, activate it with the activate_skill tool before answering.

## SKILLS

The ` + "`activate_skill`" + ` tool can load specialized instructions on demand,
when the user's request matches a skill description.

Decision Process:

- Determine whether the user's request matches a Skill;
- If a Skill would significantly improve the answer, call ` + "`activate_skill`" + ` tool;
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

- Name: search-web
  Description: Use ONLY when user asks to search the web for information.
- Name: vulnerability-triage
  Description: Use when user asks to triage, prioritize, or assess risk of vulnerabilities in their cloud environment.

Use exact skill names when calling the ` + "`activate_skill`" + ` tool.`
	assert.Equal(t, exp, sysPrompt)
}

// Test_Real_Skills_ActivatesSkillAndGeneratesPlan exercises the full
// Agent Skills flow against a real LLM:
//
//  1. A real SKILL.md file is written to a temp directory.
//  2. NewDefaultLoader discovers and parses it.
//  3. WithSkills injects the catalog into the system prompt and registers
//     the activate_skill tool.
//  4. The LLM (real) sees the catalog, decides to activate the skill, and
//     receives the full SKILL.md body back as a tool response.
//  5. The LLM uses the skill's step-by-step instructions to produce a plan.
//
// The full conversation is printed so you can see exactly how the LLM
// planned its response given the skill.
//
// To run: comment out the t.Skip line in loadOpenAIConfigOrSkipRealTest.
func Test_Real_Skills_ActivatesSkillAndGeneratesPlan(t *testing.T) {
	cfg := loadOpenAIConfigOrSkipRealTest(t)

	f := llmfactory.New(cfg)
	llmModel, err := f.ModelByType("OPENAI")
	require.NoError(t, err)

	// ── Real SKILL.md ──────────────────────────────────────────────────────────
	skilslList := skills.Skills{
		{
			Name:        "vulnerability-triage",
			Description: "Use when user asks to triage, prioritize, or assess risk of vulnerabilities in their cloud environment.",
			Body: `
## How to handle this question

Follow these steps in order:

1. **Severity gate** — focus only on findings with CVSS score >= 7 (high or critical).
2. **Exploit check** — cross-reference each finding against CISA KEV; mark those with a known exploit as "exploit-available".
3. **Exposure check** — determine whether the affected asset is internet-facing or has public exposure.
4. **Blast radius** — assess potential lateral movement if the asset were compromised.
5. **Prioritized list** — rank findings: internet-facing + known exploit > internet-facing > internal + known exploit > internal.
6. **Output format** — for each finding output: rank, CVE ID, CVSS, exploit-available (yes/no), exposure (internet/internal), brief remediation suggestion.
`,
		},
	}

	// ── Build assistant ────────────────────────────────────────────────────────
	sysprompt := prompts.PromptTemplate{
		Template: `You are a cloud security assistant that helps analysts investigate and triage vulnerabilities.
When the user's request matches a skill's description, activate it with the activate_skill tool before answering.
`,
		InputVariables: []string{},
		TemplateFormat: prompts.TemplateFormatGoTemplate,
	}

	memstore := gstore.NewMemoryStore()
	var buf strings.Builder

	ag := assistants.NewAssistant[chatmodel.String](
		llmModel,
		sysprompt,
		assistants.WithMode(encoding.ModePlainText),
		assistants.WithMessageStore(memstore),
		assistants.WithCallback(callbacks.NewPrinter(&buf, callbacks.ModeVerbose)),
	).WithSkills(skilslList)

	// Print system prompt so you can see the catalog injection
	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	sysPrompt, err := ag.GetSystemPrompt(ctx, "", nil)
	require.NoError(t, err)
	fmt.Println("=== SYSTEM PROMPT ===")
	fmt.Println(sysPrompt)
	fmt.Println()

	// ── Run ────────────────────────────────────────────────────────────────────
	var output chatmodel.String
	_, err = ag.Run(ctx, &assistants.CallInput{
		Input: "I have 47 open vulnerabilities in my Kubernetes cluster. Which ones should I fix first?",
	}, &output)
	require.NoError(t, err)

	// ── Print full conversation ────────────────────────────────────────────────
	history := memstore.Messages(ctx)
	fmt.Println("=== CONVERSATION ===")
	var conv strings.Builder
	llmutils.PrintMessages(&conv, history)
	fmt.Println(conv.String())

	fmt.Println("=== FINAL ANSWER ===")
	fmt.Println(output.GetContent())

	// ── Assertions ─────────────────────────────────────────────────────────────
	assert.NotEmpty(t, output.GetContent(), "expected a non-empty plan from the LLM")

	// Verify activate_skill was called somewhere in the conversation
	skillActivated := false
	for _, msg := range history {
		for _, part := range msg.Parts {
			if tc, ok := part.(llms.ToolCall); ok {
				if tc.FunctionCall != nil && tc.FunctionCall.Name == skills.ActivateSkillToolName {
					skillActivated = true
					t.Logf("activate_skill called with: %s", tc.FunctionCall.Arguments)
				}
			}
			if tr, ok := part.(llms.ToolCallResponse); ok && tr.Name == skills.ActivateSkillToolName {
				assert.Contains(t, tr.Content, "<skill_content", "tool response should contain skill content")
				assert.Contains(t, tr.Content, "CVSS score", "skill body should be in tool response")
			}
		}
	}
	assert.True(t, skillActivated, "LLM should have called activate_skill given the matching question")
}

// Test_Real_Skills_NoActivationForUnrelatedQuery verifies that the LLM does
// NOT activate a security skill when the question is unrelated to it.
func Test_Real_Skills_NoActivationForUnrelatedQuery(t *testing.T) {
	cfg := loadOpenAIConfigOrSkipRealTest(t)

	f := llmfactory.New(cfg)
	llmModel, err := f.ModelByType("OPENAI")
	require.NoError(t, err)

	skilslList := skills.Skills{
		{
			Name:        "vulnerability-triage",
			Description: "Use ONLY when user asks to triage or prioritize security vulnerabilities in their cloud environment.",
			Body:        `Step 1: Check CVSS.`,
		},
	}

	sysprompt := prompts.PromptTemplate{
		Template:       `You are a helpful assistant.`,
		InputVariables: []string{},
		TemplateFormat: prompts.TemplateFormatGoTemplate,
	}

	memstore := gstore.NewMemoryStore()
	ag := assistants.NewAssistant[chatmodel.String](
		llmModel,
		sysprompt,
		assistants.WithMode(encoding.ModePlainText),
		assistants.WithMessageStore(memstore),
	).WithSkills(skilslList)

	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	var output chatmodel.String
	_, err = ag.Run(ctx, &assistants.CallInput{
		Input: "What is the capital of France?",
	}, &output)
	require.NoError(t, err)
	assert.NotEmpty(t, output.GetContent())

	skillActivated := false
	for _, msg := range memstore.Messages(ctx) {
		for _, part := range msg.Parts {
			if tc, ok := part.(llms.ToolCall); ok && tc.FunctionCall != nil {
				if tc.FunctionCall.Name == skills.ActivateSkillToolName {
					skillActivated = true
				}
			}
		}
	}
	assert.False(t, skillActivated, "LLM should NOT activate a security skill for an unrelated question")

	fmt.Printf("Answer: %s\n", output.GetContent())
}

func Test_DefaultPromptProvider(t *testing.T) {
	skillsList := skills.Skills{
		{
			Name:        "vulnerability-triage",
			Description: "Use ONLY when user asks to triage or prioritize security vulnerabilities in their cloud environment.",
			Body:        `Check CVSS.`,
		},
		{
			Name:        "search-web",
			Description: "Use ONLY when user asks to search the web for information.",
			Body:        `Search the web for information.`,
		},
	}
	prompt, err := assistants.DefaultPromptProvider(context.Background(), skillsList)
	require.NoError(t, err)

	exp := `## SKILLS

The ` + "`activate_skill`" + ` tool can load specialized instructions on demand,
when the user's request matches a skill description.

Decision Process:

- Determine whether the user's request matches a Skill;
- If a Skill would significantly improve the answer, call ` + "`activate_skill`" + ` tool;
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

- Name: vulnerability-triage
  Description: Use ONLY when user asks to triage or prioritize security vulnerabilities in their cloud environment.
- Name: search-web
  Description: Use ONLY when user asks to search the web for information.

Use exact skill names when calling the ` + "`activate_skill`" + ` tool.
`
	assert.Equal(t, exp, prompt)
}

func Test_CustomPromptProvider(t *testing.T) {
	skillsList := skills.Skills{
		{
			Name:        "vulnerability-triage",
			Description: "Use ONLY when user asks to triage or prioritize security vulnerabilities in their cloud environment.",
			Body:        `Check CVSS.`,
		},
		{
			Name:        "search-web",
			Description: "Use ONLY when user asks to search the web for information.",
			Body:        `Search the web for information.`,
		},
	}

	sysprompt := prompts.PromptTemplate{
		Template: `You are a cloud security assistant that helps analysts investigate and triage vulnerabilities.
When the user's request matches a skill's description, activate it with the activate_skill tool before answering.`,
		InputVariables: []string{},
		TemplateFormat: prompts.TemplateFormatGoTemplate,
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create a mock LLM
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).AnyTimes()

	ag := assistants.NewAssistant[chatmodel.String](
		mockLLM,
		sysprompt,
		assistants.WithMode(encoding.ModePlainText),
	).
		WithSkills(skillsList).
		WithSkillsPromptProvider(func(ctx context.Context, skills skills.Skills) (string, error) {
			templ := fmt.Sprintf("Skill instructions.\nAvailable skills: %s\n", strings.Join(skills.Names(), ", "))
			return templ, nil
		})

	sysPrompt, err := ag.GetSystemPrompt(context.Background(), "", nil)
	require.NoError(t, err)

	exp := `You are a cloud security assistant that helps analysts investigate and triage vulnerabilities.
When the user's request matches a skill's description, activate it with the activate_skill tool before answering.

Skill instructions.
Available skills: vulnerability-triage, search-web`
	assert.Equal(t, exp, sysPrompt)
}

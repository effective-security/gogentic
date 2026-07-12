// Package prompts contains prompt templates and utilities for working with LLMs.
// It provides string and chat prompt types, message-level templates (system/AI/
// human), and few-shot helpers with pluggable template engines (Go template,
// Jinja2, and f-string).
//
// # Quick examples
//
// 1) Format a simple string prompt
//
//	pt := NewPromptTemplate("Hello, {{.name}}!", []string{"name"})
//	out, _ := pt.Format(map[string]any{"name": "gopher"})
//	// out == "Hello, gopher!"
//
// 2) Build a chat prompt with system + human messages
//
//	cpt := NewChatPromptTemplate([]MessageFormatter{
//		NewSystemMessagePromptTemplate("You are a helpful assistant.", nil),
//		NewHumanMessagePromptTemplate("Translate '{{.text}}' to French", []string{"text"}),
//	})
//	pv, _ := cpt.FormatPrompt(map[string]any{"text": "good morning"})
//	_ = pv.Messages() // []llms.Message usable with chat LLMs
//
// 3) Few-shot assembly
//
//	exPrompt := NewPromptTemplate("Q: {{.q}}\nA: {{.a}}", []string{"q", "a"})
//	fs, _ := NewFewShotPrompt(
//		exPrompt,
//		[]map[string]string{{"q": "2+2?", "a": "4"}},
//		nil,
//		"Answer the question.",
//		"Provide only the final answer.",
//		[]string{"q"},
//		nil,
//		"\n\n",
//		TemplateFormatGoTemplate,
//		true,
//	)
//	out, _ = fs.Format(map[string]any{"q": "3+5?"})
package prompts

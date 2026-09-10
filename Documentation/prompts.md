# Prompts

`pkg/prompts` renders system prompts. An assistant takes a
`prompts.FormatPrompter`, so anything implementing that interface works —
including your own.

Source: [`pkg/prompts/`](../pkg/prompts).

## Interfaces

```go
// Renders values into a string.
type Formatter interface {
    Format(values map[string]any) (string, error)
}

// Renders values into chat messages.
type MessageFormatter interface {
    FormatMessages(values map[string]any) ([]llms.Message, error)
    GetInputVariables() []string
}

// What Assistant requires.
type FormatPrompter interface {
    FormatPrompt(values map[string]any) (llms.PromptValue, error)
    GetInputVariables() []string
}
```

`llms.PromptValue` is `String() string` + `Messages() []llms.Message`, so a
prompt can be consumed either way. The assistant uses `String()`: whatever your
prompter renders becomes the single system message.

## `PromptTemplate`

The everyday case.

```go
p := prompts.NewPromptTemplate(
    "You are a {{.role}} assistant for {{.org}}.",
    []string{"role", "org"})

out, err := p.Format(map[string]any{"role": "security", "org": "Acme"})
// "You are a security assistant for Acme."
```

`NewPromptTemplate` defaults `TemplateFormat` to `TemplateFormatGoTemplate`.
Set the field directly for another engine:

```go
p := prompts.PromptTemplate{
    Template:       "You are a {{ role }} assistant.",
    InputVariables: []string{"role"},
    TemplateFormat: prompts.TemplateFormatJinja2,
}
```

### Template engines

| Format | Syntax | Notes |
|--------|--------|-------|
| `TemplateFormatGoTemplate` (default) | `{{.name}}` | `text/template` with the full [sprig](https://masterminds.github.io/sprig/) function set; `missingkey=error` |
| `TemplateFormatJinja2` | `{{ name }}` | via `gonja` |
| `TemplateFormatFString` | `{name}` | Python-style, minimal |

`missingkey=error` on the Go engine means **every referenced variable must be
supplied**, otherwise `Format` fails. That is deliberate: a silently empty
`{{.org}}` in a system prompt is worse than an error.

Validate a template up front:

```go
if err := prompts.CheckValidTemplate(tmpl, prompts.TemplateFormatGoTemplate,
    []string{"role", "org"}); err != nil {
    return err
}
```

One-shot rendering without building a template value:

```go
s, err := prompts.RenderTemplate(tmpl, prompts.TemplateFormatGoTemplate, values)
```

### Partial variables

Values known at construction time, or computed lazily at render time. A partial
may be a value or a `func() (string, error)`.

```go
p := prompts.PromptTemplate{
    Template:       "You are {{.name}}. Today is {{.today}}.",
    InputVariables: []string{"name"},
    TemplateFormat: prompts.TemplateFormatGoTemplate,
    PartialVariables: map[string]any{
        "today": func() (string, error) { return time.Now().Format("2006-01-02"), nil },
    },
}

out, err := p.Format(map[string]any{"name": "Ada"})
```

Partials are resolved first, then the caller's values are merged over them, so a
caller can override a partial.

## `ChatPromptTemplate`

Builds a multi-message prompt. Useful when you want an explicit few-shot
exchange or a placeholder for prior history.

```go
cpt := prompts.NewChatPromptTemplate([]prompts.MessageFormatter{
    prompts.NewSystemMessagePromptTemplate(
        "You are a translator into {{.lang}}.", []string{"lang"}),
    prompts.NewHumanMessagePromptTemplate(
        "Translate: {{.text}}", []string{"text"}),
})

pv, err := cpt.FormatPrompt(map[string]any{"lang": "French", "text": "good morning"})
msgs := pv.Messages() // []llms.Message
```

Message formatters:

| Constructor | Role |
|-------------|------|
| `NewSystemMessagePromptTemplate` | `RoleSystem` |
| `NewHumanMessagePromptTemplate` | `RoleHuman` |
| `NewAIMessagePromptTemplate` | `RoleAI` |
| `NewGenericMessagePromptTemplate(role, tmpl, vars)` | arbitrary role |
| `MessagesPlaceholder{VariableName: "history"}` | injects a `[]llms.Message` passed in the values |

`MessagesPlaceholder` requires the value to be exactly `[]llms.Message`, else it
returns `ErrNeedChatMessageList`.

`ChatPromptTemplate.GetInputVariables()` is the union of its messages'
variables, so `Assistant.GetPromptInputVariables()` reports everything the
prompt needs.

Note: since the assistant renders the prompt to a **string** for the system
message, a `ChatPromptTemplate` used as a system prompt collapses to its
`String()` form. Use it when you want that combined text, and use
`CallInput.Messages` when you need genuinely separate messages in the request.

## Few-shot prompts

```go
examplePrompt := prompts.NewPromptTemplate("Q: {{.q}}\nA: {{.a}}", []string{"q", "a"})

fs, err := prompts.NewFewShotPrompt(
    examplePrompt,
    []map[string]string{
        {"q": "2+2?", "a": "4"},
        {"q": "3*3?", "a": "9"},
    },
    nil,                                  // ExampleSelector (nil => use all examples)
    "Answer arithmetic questions.",       // prefix
    "Q: {{.q}}\nA:",                      // suffix
    []string{"q"},                        // input variables
    nil,                                  // partial variables
    "\n\n",                               // example separator
    prompts.TemplateFormatGoTemplate,
    true,                                 // validate the template
)
if err != nil {
    return err
}

out, err := fs.Format(map[string]any{"q": "10-4?"})
```

Exactly one of `examples` or `exampleSelector` must be provided; supplying both
or neither is an error. Implement `ExampleSelector` to choose examples per
input (e.g. nearest-neighbour retrieval).

For simple prompt/completion pairs, the assistant's
`assistants.WithExamples(chatmodel.FewShotExamples{...})` option is easier: it
injects each pair as a `RoleHuman`/`RoleAI` message annotated as an example,
without touching your template.

## Prompts in an assistant

```go
sysPrompt := prompts.NewPromptTemplate(
    "You are a {{.role}} assistant.\n\n# RULES\n{{.rules}}",
    []string{"role", "rules"})

agent := assistants.NewAssistant[Answer](fac, sysPrompt,
    // static: same for every call
    assistants.WithPromptInput(map[string]any{
        "role":  "security",
        "rules": "Never speculate. Cite sources.",
    }),
)

// per call: merged over the static values
resp, err := agent.Run(ctx, &assistants.CallInput{
    Input:        question,
    PromptInputs: map[string]any{"rules": tenantRules},
}, &out)
```

Resolution order for prompt values (later wins):

1. `Config.PromptInput` (`WithPromptInput`)
2. `CallInput.PromptInputs`
3. Whatever `WithPromptInputProvider` returns

Then the assistant appends the [skills catalog](skills.md) and, when needed, the
[output schema](structured-output.md#the-two-routes-to-structured-output).

## Writing a custom prompter

Anything satisfying `FormatPrompter` works — for example a prompt loaded from a
database or assembled from retrieved documents:

```go
type ragPrompt struct{ retriever Retriever }

func (p ragPrompt) GetInputVariables() []string { return []string{"question"} }

func (p ragPrompt) FormatPrompt(values map[string]any) (llms.PromptValue, error) {
    q, ok := values["question"].(string)
    if !ok {
        return nil, errors.New("question is required")
    }
    docs, err := p.retriever.Search(q)
    if err != nil {
        return nil, errors.WithMessage(err, "failed to retrieve context")
    }
    return prompts.StringPromptValue(
        "Answer using only this context:\n" + strings.Join(docs, "\n---\n")), nil
}
```

Keep in mind the assistant renders the prompt on **every** run, so anything
expensive inside `FormatPrompt` runs per call — cache it, or use
`WithPromptInputProvider` where the intent is clearer.

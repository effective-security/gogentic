# Skills

gogentic implements the [Agent Skills](https://agentskills.io) standard: a
skill is a directory holding a `SKILL.md` file with YAML front-matter and
markdown instructions.

Skills use **progressive disclosure**: only a compact catalog (name +
description) goes into the system prompt; the full instructions are loaded on
demand when the model calls the `activate_skill` tool. That keeps the prompt
small no matter how many skills are installed.

Source: [`skills/`](../skills) — `skills.go` (types), `loader.go`
(discovery/parsing), `activate_skill_tool.go` (the tool);
[`assistants/skills.go`](../assistants/skills.go) (the catalog prompt).

## `SKILL.md`

```markdown
---
name: pdf-processing
description: Extract text and tables from PDF documents, including scanned pages.
license: Apache-2.0
allowed-tools: read_file, run_script
agents: analyzer, orchestrator
tags: documents, extraction
metadata:
  owner: platform-team
---

# PDF Processing

## When to use

Use this skill when the user supplies a PDF and needs its content...

## Steps

1. Run `scripts/extract.py` on the input file.
2. ...

## OUTPUT FORMAT

Return a markdown table with one row per extracted table.
```

Front-matter fields:

| Field | Required | Meaning |
|-------|----------|---------|
| `name` | no | Skill name. Defaults to the directory name |
| `description` | **yes** | What the skill is for. This is the only text the model sees before activating — write it as a selection criterion |
| `license` | no | Informational |
| `compatibility` | no | Informational |
| `allowed-tools` | no | Space/comma separated tool names the skill may use |
| `agents` | no | Space/comma separated agent names allowed to use this skill |
| `tags` | no | Space/comma separated tags, for filtering |
| `metadata` | no | Free-form `map[string]string` |

A missing `description` is a parse error. A file without front-matter is
accepted, with the whole file as the body and the directory name as the name.

Everything after the closing `---` is the body — the instructions returned on
activation.

## Directory layout

```
skills/
├── pdf-processing/
│   ├── SKILL.md          ← discovered
│   └── scripts/
│       └── extract.py    ← bundled resource
├── data-analysis/
│   └── SKILL.md          ← discovered
├── .analyzer/            ← agent-scoped directory ("analyzer")
│   └── chart-review/
│       └── SKILL.md      ← only for the "analyzer" agent
└── README.md             ← ignored (not a skill directory)
```

Two conventions:

- A directory containing `SKILL.md` is a skill.
- A directory whose name starts with `.` is an **agent scope**; its name minus
  the dot is the agent, and the skills inside belong to that agent.

Skills can also be loaded from a `.tar` or `.tar.gz` archive with the same
internal layout — pass the archive path as a search path. Path-traversal
entries are rejected; a `SKILL.md` at the archive root is skipped because it
has no enclosing skill directory.

## Configuration

```yaml
skills:
  # Fail loading instead of logging when a SKILL.md is invalid.
  strict: false
  # Include skills that are not scoped to any agent.
  enable_default_skills: true
  # Also search the conventional locations (see below).
  enable_standard_paths: true
  # Explicit paths; later paths win on name collision.
  paths:
    - ./skills
    - /opt/myapp/skills.tar.gz
  # Per-agent overrides.
  agents:
    orchestrator:
      paths:
        - ~/.agents/skills/.orchestrator
    remediations:
      disabled: true
```

Standard paths, searched when `enable_standard_paths` is true (in increasing
priority, so a project-level skill overrides a user-level one of the same
name):

```
~/.agents/skills/                      (clientName == "")
<cwd>/.agents/skills/
~/.agents/.<clientName>/skills/        (clientName != "")
<cwd>/.agents/.<clientName>/skills/
```

Explicit `paths` are always highest priority.

`enable_default_skills` matters more than it looks: when false, a skill that is
neither inside a `.<agent>` directory nor declares `agents:` in its
front-matter is **dropped entirely**. Enable it if you keep unscoped skills.

Loading is embedded in the LLM factory config, so the usual path is:

```go
fac, err := llmfactory.Load("llm.yaml")  // parses the `skills:` section
if err != nil {
    return err
}
list := fac.Skills("orchestrator")       // agent name
```

Or build a loader directly:

```go
cfg, err := skills.LoadConfig("skills.yaml")
if err != nil {
    return err
}
loader, err := skills.NewLoader(cfg, "myclient") // "" to skip client-specific paths
if err != nil {
    return err
}
list := loader.Skills("orchestrator", "documents") // agent + required tags
agents := loader.Agents()                          // agents with skills loaded
```

`Loader.Skills(agent, tags...)` merges the `default` agent's skills (when
`enable_default_skills` is on) with the named agent's, filters by *all*
supplied tags, and sorts by name. A disabled agent gets `nil`.

## Attaching skills to an assistant

```go
agent := assistants.NewAssistant[chatmodel.OutputResult](fac, sysPrompt).
    WithName("orchestrator").
    WithSkills(fac.Skills("orchestrator"))
```

`WithSkills` does two things:

1. Records the list, so the catalog can be rendered into the system prompt.
2. Registers the `activate_skill` tool, whose `name` parameter carries a JSON
   Schema `enum` of the available skill names — the model gets concrete
   choices, not a free-form string.

It is a no-op for an empty list, and if the tool cannot be built the failure is
logged and the assistant continues without skills.

## The generated prompt

`assistants.DefaultPromptProvider` renders a `## SKILLS` section listing each
skill's name and description, plus decision rules telling the model when to
activate one, to treat activated instructions as trusted system-prompt content,
and to follow any `OUTPUT FORMAT` the skill specifies.

The section is rendered **once** and cached on the assistant, so the cost is
paid on the first run only. Replace it entirely if you need different wording:

```go
agent = agent.WithSkillsPromptProvider(
    func(ctx context.Context, list skills.Skills) (string, error) {
        return prompts.RenderTemplate(myTemplate, prompts.TemplateFormatGoTemplate,
            map[string]any{
                "Skills":                list,
                "ActivateSkillToolName": skills.ActivateSkillToolName,
            })
    })
```

Because the catalog is cached, call `WithSkillsPromptProvider` before the first
run.

## Activation

The model calls `activate_skill` with `{"name": "pdf-processing"}` and receives:

```json
{
  "skill": "pdf-processing",
  "location": "/.analyzer/pdf-processing",
  "instructions": "# PDF Processing\n\n## When to use\n..."
}
```

An unknown name returns a structured error instead of failing the call, so the
model can retry with a valid name:

```json
{"error": {"code": "skill_not_found", "message": "skill \"pdf\" not found",
           "available_skills": "data-analysis, pdf-processing"}}
```

`location` is the skill directory relative to its search root — give it to the
model so file-reading tools can resolve `scripts/extract.py` and similar
bundled paths.

## Bundled resources

```go
names := skill.ListResources()      // e.g. ["scripts/extract.py", "references/spec.md"]
blobs := skill.LoadResources()      // map[name][]byte, lazily read and cached
```

`ListResources` walks the skill directory and one level of subdirectories,
excluding `SKILL.md`. Resources are **not** returned by `activate_skill` —
they are for your own tools to read. A skill that needs a script expects the
agent to have a file-reading or script-running tool; list those in
`allowed-tools` and enforce it in your application.

## Security

Activated skill content is injected into the model's context and the default
prompt instructs the model to treat it as trusted. Skills are therefore as
privileged as your system prompt:

- Load skills only from directories you control. Do not point `paths` at
  user-writable locations.
- `allowed-tools` is metadata; nothing in this package enforces it. If a skill
  should not reach a tool, do not attach that tool to the assistant.
- Prefer `strict: true` in production so a malformed skill fails loudly at
  startup instead of silently disappearing.

// Package skills implements the Agent Skills open standard (https://agentskills.io)
// for AI assistants.
//
// A skill is a directory containing a SKILL.md file with YAML front-matter
// (name, description) and markdown instructions. Skills are discovered at
// session start via filesystem scan, a compact catalog is injected into the
// system prompt, and full instructions are loaded on demand when the model
// activates a skill via the activate_skill tool.
package skills

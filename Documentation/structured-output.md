# Structured Output

Getting a typed Go value out of an LLM involves two packages:

- **`pkg/schema`** — reflects a Go type into a JSON Schema, and shapes that
  schema into either function parameters or a provider response format.
- **`encoding`** — wraps a schema in "format instructions" for the prompt, and
  parses model output back into the type.

Sources: [`pkg/schema/`](../pkg/schema), [`encoding/`](../encoding).

## The two routes to structured output

An assistant picks one of these automatically, based on the `encoding.Mode` and
the provider's capabilities:

```
                    encoding.Mode                provider supports
                                                 json_schema?
ModeJSONSchema  ───────────────────────► yes ──► ResponseFormat on the request
ModeJSONSchemaStrict                             (schema enforced by provider)
                                          no ──► "# OUTPUT SCHEMA" appended
                                                 to the system prompt
ModeJSON / ModeYAML / ModeTOML ───────────────► format instructions in prompt
ModePlainText ────────────────────────────────► no instructions, no parsing
```

`Assistant.setResponseFormat` decides this:

- `ModeJSONSchema` or `ModeJSONSchemaStrict` **and**
  `CapabilityJSONSchema` → a `schema.ResponseFormat` is attached to the call.
- `ModeJSONSchemaStrict` additionally requires `CapabilityJSONSchemaStrict`
  for `strict: true`; otherwise the schema is sent non-strict.
- If no response format was negotiated, `GetSystemPrompt` appends
  `# OUTPUT SCHEMA` with the parser's instructions.

Either way, the model's text output is still parsed by the `OutputParser`, so
the typed result is validated locally even when the provider enforced a schema.

The default is `encoding.ModeDefault`, which is `ModeJSONSchema`. It is a
package-level `var`, so an application can change the global default.

## Describing a type

Use `jsonschema` struct tags. They are what the model reads, so write them as
instructions.

```go
type Finding struct {
    Severity string   `json:"severity" jsonschema:"required,title=Severity,description=How serious the finding is,enum=low,enum=medium,enum=high"`
    Summary  string   `json:"summary"  jsonschema:"required,title=Summary,description=One sentence describing the issue."`
    Files    []string `json:"files"    jsonschema:"title=Files,description=Repo-relative paths involved."`
    Score    int      `json:"score"    jsonschema:"title=Score,description=0-100 confidence,default=50"`
}
```

Commonly used tag keys: `required`, `title`, `description`, `enum=` (repeat per
value), `default=`, `example=`, `minimum=`, `maximum=`.

For schema-level metadata, implement `JSONSchemaExtend`:

```go
func (Finding) JSONSchemaExtend(s *jsonschema.Schema) {
    s.Title = "Finding"
    s.Description = "A single issue discovered during review."
}
```

## `pkg/schema`

```go
sc, err := schema.New(reflect.TypeOf(Finding{}))
if err != nil {
    return err
}
sc.RawSchema  // the full reflected draft-07 schema, with $defs
sc.Parameters // flattened: top-level properties, $refs resolved
sc.String()   // Parameters as indented JSON
```

`Parameters` is what tools return from `ITool.Parameters()`: a single object
schema with first-level properties inlined and local `$ref`s resolved, which is
what provider function-calling APIs expect.

`schema.New` results are **cached by `reflect.Type`** for the process lifetime.
Schemas are therefore effectively immutable — do not mutate a returned
`*Schema`.

The reflector is configured for LLM use (`pkg/schema/schema.go`): draft-07
(broader tooling support), expanded structs, no references, and a namer that
hashes the full package path into the struct name so identically-named structs
in different packages cannot collide.

Response formats:

```go
rf, err := schema.NewResponseFormat(reflect.TypeOf(Finding{}), true /* strict */)
// rf.Type == "json_schema"
// rf.JSONSchema.Name == "Finding"
// rf.JSONSchema.Strict == true
```

Strict mode sets `additionalProperties: false` on objects. Note the practical
consequence: providers implementing strict schemas typically require *every*
property to be listed in `required`, so optional fields and strict mode do not
mix well. Use `ModeJSONSchema` (non-strict) for types with optional fields.

`schema.FromAny` / `schema.MustFromAny` build a schema from an arbitrary value
(e.g. a hand-written `map[string]any`) when reflection is not what you want.

## `encoding`

### `SchemaEncoder`

```go
type SchemaEncoder interface {
    Marshal(req any) ([]byte, error)
    Unmarshal([]byte, any) error
    GetFormatInstructions() string
}
```

Implementations: `encoding/json`, `encoding/yaml`, `encoding/toml`,
`encoding/dummy` (plain text, no structure). `PredefinedSchemaEncoder(mode, v)`
returns the right one for a `Mode`.

The JSON encoder additionally implements `Validator` (via
`go-playground/validator`), so `validate` struct tags can be enforced.

### `TypedOutputParser[T]`

This is what assistants use. `NewAssistant[O]` constructs one for `O`
automatically; you rarely build it by hand.

```go
parser, err := encoding.NewTypedOutputParser(Finding{}, encoding.ModeJSONSchema)
if err != nil {
    return err
}

instructions := parser.GetFormatInstructions() // embed in a prompt
// "Respond with JSON in the following JSON schema: ```json { ... } ```
//  Make sure to return an instance of the JSON, not the schema itself. ..."

out, err := parser.Parse(`Sure! {"severity":"high","summary":"SQL injection"}`)
if err != nil {
    return err
}
_ = out // *Finding
```

Two things make parsing robust in practice:

- Input goes through `llmutils.CleanJSON`, which discards prose before the
  first `{`/`[` and after the matching close, and strips ``` fences.
- Decoding uses a lenient JSON reader (`bububa/ljson`), which tolerates
  trailing commas and similar model slips.

Enable validation explicitly:

```go
parser.WithValidation(true) // runs validator.Struct on the decoded value
```

Validation errors are returned wrapped ("failed to validate"), *not* as
`ErrFailedUnmarshalOutput`, so they do not trigger the assistant's JSON retry.

### Custom parsers

Supply your own `chatmodel.OutputParser[O]` when the shape is not JSON-ish:

```go
type OutputParser[T any] interface {
    Parse(text string) (*T, error)
    GetFormatInstructions() string
    Type() string
}

agent := assistants.NewAssistant[MyType](fac, sysPrompt).
    WithOutputParser(myParser)
```

`encoding.NewSimpleOutputParser()` is a ready-made no-op parser over
`chatmodel.String` that just trims whitespace — the right choice for prose
assistants that should not be constrained.

## Failure modes

| Symptom | Cause | Fix |
|---------|-------|-----|
| `chatmodel.ErrFailedUnmarshalOutput` | Output did not decode into `O` | Already retried once with tools off and a JSON nudge. Simplify the type, or switch to `ModeJSONSchema` if you were using strict |
| Provider rejects the request with a schema error | Strict mode with optional fields, or an unsupported construct | Use `ModeJSONSchema`, or mark all fields required |
| Prompt contains `# OUTPUT SCHEMA` unexpectedly | Provider lacks `CapabilityJSONSchema` | Expected — the schema has to go somewhere. Pick a provider with the capability if you need it out of the prompt |
| Model returns the schema instead of an instance | Weak model, long schema | The JSON encoder's instructions already warn against this; shorten the type or add a few-shot example |
| `chatmodel.ErrFailedUnmarshalInput` from a tool | The model's arguments JSON did not match the tool schema | Expected and recoverable; the model is told to fix it. Check the tool's `jsonschema` tags are unambiguous |
| Fields silently missing | `json` tag mismatch between prompt schema and struct | The instructions say "use the exact field names"; ensure `json` tags are present on every field |

Keep output types small. Every field is prompt tokens on every call, and deep
nesting is the most common cause of parse failures.

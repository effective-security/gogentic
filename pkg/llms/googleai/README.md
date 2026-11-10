This directory contains the provider for Google's Gemini models served by the
Gemini API (https://ai.google.dev/), built on `google.golang.org/genai`.

* `googleai.New` creates the `llms.Model`; configure it with the `With*`
  options in `option.go` (API key or credentials, default model, generation
  defaults, harm threshold, effort policy).
* `Infer` / `ValidateInference` in `inference.go` implement the portable
  inference contract used by the router.
* `NewModelLister` and `GoogleAI.ListModels` list the models an account can
  call.
* `internal/genaiutils` converts `llms.Tool` definitions and JSON Schemas to
  `genai` types.

----

Testing:

Unit tests need no credentials; model listing runs against recorded
responses in `testdata/`. `TestIntegration*` tests call the live API and are
skipped under `make test`.

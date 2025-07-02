# TODO

This file tracks potential improvements and bug fixes for the `googleai` package.

## Error Handling

- [ ] Introduce more specific error types instead of relying on `errors.New`. For example, `ErrUnsupportedRole` in `convertContent`.

## Configuration and Hardcoded Values

- [ ] Make the batch size in `CreateEmbedding` configurable via `Options`. The current value of 100 is hardcoded.
- [ ] Expose more of the `genai` client options through the `Options` struct.

## Extensibility

- [ ] In `convertContent`, consider allowing a configurable mapping of `llms.ChatMessageType` to `genai` roles to support custom roles.
- [ ] The tool use implementation could be made more robust by adding support for parallel function calls.

## Code Duplication

- [ ] Review the `googleai` and `vertex` packages to identify any other opportunities for code sharing, beyond what is already done with the code generation script.

## Clarity and Comments

- [ ] Add more detailed comments to `convertAndStreamFromIterator` to explain the logic for handling multiple candidates and streaming.
- [ ] Add package-level documentation to explain the overall architecture and how the different parts of the package work together.

## Missing Features

- [ ] Add support for grounding to allow the model to provide citations from a specified corpus.
- [ ] Add support for structured outputs to allow the model to generate responses in a specific format, such as JSON.

  ### Detailed Explanation for Structured Output Support

  **Current Capabilities:**

  The current implementation has basic support for JSON output. In `GenerateContent`, if `opts.ResponseFormat.Type` is set to `"json_object"` and no tools are provided, it sets `model.ResponseMIMEType = "application/json"`. This tells the Gemini model to output a JSON formatted string in its response.

  **Limitations:**

  This approach has two main limitations:

  1.  **No Schema Enforcement:** It does not enforce any specific JSON schema. The model is simply instructed to return JSON, but the structure of that JSON is not guaranteed.
  2.  **Incompatible with Tools:** This feature is explicitly disabled when tools are present. The line `if len(model.Tools) == 0 && opts.ResponseFormat != nil && ...` shows this. This means you cannot get a structured JSON response when you are also using tools.

  **Required Changes for Full Support:**

  To properly support structured outputs, especially in conjunction with tool calls (similar to OpenAI's `response_format` with a JSON schema), we need to leverage more advanced features of the `generative-ai-go` library. Specifically, we need to use `genai.Tool` and `genai.ToolConfig` to define and force the use of a tool with a specific output schema.

  Here is a breakdown of the necessary changes:

  1.  **Extend `llms.Tool`:** The `llms.Tool` struct in `pkg/llms/llms.go` needs to be extended to include an optional field for the JSON schema of the expected output. This could be a `map[string]any` or a struct that can be marshaled into a JSON schema.

  2.  **Update `genaiutils.ConvertTools`:** The `genaiutils.ConvertTools` function in `pkg/llms/googleai/internal/genaiutils/convert.go` needs to be updated to handle the new schema field. When converting an `llms.Tool` to a `genai.Tool`, it should:

      - If a schema is present in the `llms.Tool`, create a `genai.Schema` from it.
      - Set this schema as the `Parameters` of the `genai.FunctionDeclaration`.

  3.  **Update `GenerateContent`:** The `GenerateContent` function in `pkg/llms/googleai/googleai.go` needs to be modified to handle the case where a structured output is requested along with tools.
      - It should check if the `llms.CallOptions` includes a request for a specific tool's output (e.g., via a new field in `llms.ResponseFormat`).
      - If so, it should create a `genai.ToolConfig` that forces the model to call the specified tool. This is done by setting the `ToolConfig` on the `genai.GenerativeModel`.
      - The `ToolConfig` would look something like this:
        ```go
        model.ToolConfig = &genai.ToolConfig{
            FunctionCallingConfig: &genai.FunctionCallingConfig{
                Mode: genai.FunctionCallingModeAny,
                AllowedFunctionNames: []string{"tool_name_to_force"},
            },
        }
        ```

  By making these changes, we can support structured outputs in a way that is consistent with how the `generative-ai-go` library is designed to be used, and it will work seamlessly with tool calls. This will allow users to specify a JSON schema for the output of a function call and be confident that the model's response will conform to that schema.

- [ ] Implement a mechanism for automatic retries with backoff for transient network errors.

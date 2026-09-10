package openai

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/router"
	"github.com/effective-security/gogentic/router/dialect"
)

// Request field names shared by both endpoints.
const (
	keyModel             = "model"
	keyMessages          = "messages"
	keyInput             = "input"
	keyInstructions      = "instructions"
	keyRole              = "role"
	keyContent           = "content"
	keyType              = "type"
	keyText              = "text"
	keyName              = "name"
	keyID                = "id"
	keyStatus            = "status"
	keyDescription       = "description"
	keyParameters        = "parameters"
	keyStrict            = "strict"
	keySchema            = "schema"
	keyFunction          = "function"
	keyArguments         = "arguments"
	keyCallID            = "call_id"
	keyOutput            = "output"
	keyToolCalls         = "tool_calls"
	keyToolCallID        = "tool_call_id"
	keyTools             = "tools"
	keyToolChoice        = "tool_choice"
	keyParallelToolCalls = "parallel_tool_calls"
	keyTemperature       = "temperature"
	keyTopP              = "top_p"
	keyMaxTokens         = "max_tokens"
	keyMaxCompletion     = "max_completion_tokens"
	keyMaxOutputTokens   = "max_output_tokens"
	keyStop              = "stop"
	keySeed              = "seed"
	keyResponseFormat    = "response_format"
	keyJSONSchema        = "json_schema"
	keyFormat            = "format"
	keyVerbosity         = "verbosity"
	keyReasoningEffort   = "reasoning_effort"
	keyReasoning         = "reasoning"
	keyEffort            = "effort"
	keySummary           = "summary"
	keyInclude           = "include"
	keyMetadata          = "metadata"
	keyUser              = "user"
	keySafetyIdentifier  = "safety_identifier"
	keyN                 = "n"
	keyStream            = "stream"
	keyStreamOptions     = "stream_options"
	keyStore             = "store"
	keyLogprobs          = "logprobs"
	keyTopLogprobs       = "top_logprobs"
	keyLogitBias         = "logit_bias"
	keyFrequencyPenalty  = "frequency_penalty"
	keyPresencePenalty   = "presence_penalty"
	keyModalities        = "modalities"
	keyAudio             = "audio"
	keyPrediction        = "prediction"
	keyFunctions         = "functions"
	keyFunctionCall      = "function_call"
	keyWebSearchOptions  = "web_search_options"
	keyServiceTier       = "service_tier"
	keyPreviousResponse  = "previous_response_id"
	keyConversation      = "conversation"
	keyBackground        = "background"
	keyTruncation        = "truncation"
	keyPrompt            = "prompt"
	keyMaxToolCalls      = "max_tool_calls"
	keyRefusal           = "refusal"
	keyAnnotations       = "annotations"
	keyReasoningDetails  = "reasoning_details"
	keyData              = "data"
	keyEncryptedContent  = "encrypted_content"
	keyIncompleteDetails = "incomplete_details"
	keyReason            = "reason"
	keyError             = "error"
	keyMessage           = "message"
	keyParam             = "param"
	keyCode              = "code"
	keyObject            = "object"
	keyCreated           = "created"
	keyCreatedAt         = "created_at"
	keyChoices           = "choices"
	keyIndex             = "index"
	keyFinishReason      = "finish_reason"
	keyUsage             = "usage"
	keyCompletionTokens  = "completion_tokens"
	keyPromptTokens      = "prompt_tokens"
	keyTotalTokens       = "total_tokens"
	keyPromptDetails     = "prompt_tokens_details"
	keyCompletionDetails = "completion_tokens_details"
	keyInputTokens       = "input_tokens"
	keyOutputTokens      = "output_tokens"
	keyInputDetails      = "input_tokens_details"
	keyOutputDetails     = "output_tokens_details"
	keyCachedTokens      = "cached_tokens"
	keyReasoningTokens   = "reasoning_tokens"
)

// Wire values.
const (
	partText           = "text"
	partRefusal        = "refusal"
	partInputText      = "input_text"
	partOutputText     = "output_text"
	partSummaryText    = "summary_text"
	itemMessage        = "message"
	itemFunctionCall   = "function_call"
	itemFunctionOutput = "function_call_output"
	itemReasoning      = "reasoning"
	formatTypeText     = "text"
	formatTypeJSON     = "json_object"
	formatTypeSchema   = "json_schema"
	choiceAuto         = "auto"
	choiceNone         = "none"
	choiceRequired     = "required"
	effortMinimal      = "minimal"
	effortLow          = "low"
	effortMedium       = "medium"
	effortHigh         = "high"
	verbosityDefault   = "medium"
	serviceTierAuto    = "auto"
	serviceTierDefault = "default"
	truncationDisabled = "disabled"
	modalityText       = "text"
	includeReasoning   = "reasoning.encrypted_content"
	statusCompleted    = "completed"
	statusIncomplete   = "incomplete"
	reasonMaxTokens    = "max_output_tokens"
	reasonFilter       = "content_filter"
	objectChat         = "chat.completion"
	objectResponse     = "response"
	roleAssistant      = "assistant"
	roleTool           = "tool"

	detailEncrypted = "reasoning.encrypted"
	detailSummary   = "reasoning.summary"
	detailText      = "reasoning.text"
	detailFormat    = "gogentic-router"

	chatIDPrefix      = "chatcmpl_"
	responseIDPrefix  = "resp_"
	reasoningIDPrefix = "rs_"
	messageIDPrefix   = "msg_"
	callIDPrefix      = "fc_"

	errorTypeInvalid = "invalid_request_error"
	errorTypeServer  = "server_error"

	paramRequest = "request"

	summarySeparator = "\n"
)

// emptyParameters is the schema for a parameterless function.
var emptyParameters = json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)

// decodeRoot validates raw JSON and parses the top-level request object.
func decodeRoot(raw []byte, opts dialect.DecodeOptions) (*dialect.Object, error) {
	if err := dialect.Inspect(raw, opts); err != nil {
		return nil, err
	}
	if !dialect.IsObject(raw) {
		return nil, router.Invalid(paramRequest, "expected a JSON object")
	}
	return dialect.ParseObject(raw, "", opts)
}

// decodeCommon reads fields with identical meaning in both endpoints.
func decodeCommon(root *dialect.Object, req *router.Request) error {
	in := &req.Input
	model, _, err := root.String(keyModel)
	if err != nil {
		return err
	}
	req.Model = model
	if v, ok, err := root.Float(keyTemperature); err != nil {
		return err
	} else if ok {
		in.Temperature = &v
	}
	if v, ok, err := root.Float(keyTopP); err != nil {
		return err
	} else if ok {
		in.TopP = &v
	}
	if v, ok, err := root.Bool(keyParallelToolCalls); err != nil {
		return err
	} else if ok {
		in.ParallelToolCalls = &v
	}
	if metadata, ok, err := root.StringMap(keyMetadata); err != nil {
		return err
	} else if ok {
		req.Metadata = metadata
	}
	for _, key := range []string{keyUser, keySafetyIdentifier} {
		user, ok, err := root.String(key)
		if err != nil {
			return err
		}
		if ok && user != "" {
			if req.Metadata == nil {
				req.Metadata = map[string]string{}
			}
			req.Metadata[dialect.MetadataUser] = user
		}
	}
	if err := rejectBool(root, keyStream, false); err != nil {
		return err
	}
	if err := rejectBool(root, keyStore, false); err != nil {
		return err
	}
	if err := rejectPresent(root, keyStreamOptions); err != nil {
		return err
	}
	if err := rejectInt(root, keyTopLogprobs, 0); err != nil {
		return err
	}
	if tier, ok, err := root.String(keyServiceTier); err != nil {
		return err
	} else if ok && tier != serviceTierAuto && tier != serviceTierDefault {
		return root.Unsupported(keyServiceTier)
	}
	return nil
}

// rejectBool accepts only the default boolean value.
func rejectBool(o *dialect.Object, key string, want bool) error {
	v, ok, err := o.Bool(key)
	if err != nil {
		return err
	}
	if ok && v != want {
		return o.Unsupported(key)
	}
	return nil
}

// rejectInt accepts only the default integer value.
func rejectInt(o *dialect.Object, key string, want int64) error {
	v, ok, err := o.Int(key)
	if err != nil {
		return err
	}
	if ok && v != want {
		return o.Unsupported(key)
	}
	return nil
}

// rejectFloat accepts only the default numeric value.
func rejectFloat(o *dialect.Object, key string, want float64) error {
	v, ok, err := o.Float(key)
	if err != nil {
		return err
	}
	if ok && v != want {
		return o.Unsupported(key)
	}
	return nil
}

// rejectPresent rejects any non-null value.
func rejectPresent(o *dialect.Object, key string) error {
	if o.Raw(key) != nil {
		return o.Unsupported(key)
	}
	return nil
}

// rejectNonEmptyObject accepts only an empty object.
func rejectNonEmptyObject(o *dialect.Object, key string) error {
	raw := o.Raw(key)
	if raw == nil {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return router.Invalid(o.Field(key), "expected an object")
	}
	if len(m) > 0 {
		return o.Unsupported(key)
	}
	return nil
}

// decodeTools parses function tools. nested selects the Chat shape
// ({"type":"function","function":{...}}) over the flat Responses shape.
func decodeTools(root *dialect.Object, nested bool) ([]llms.InferenceTool, error) {
	items, ok, err := root.Array(keyTools)
	if err != nil || !ok {
		return nil, err
	}
	out := make([]llms.InferenceTool, 0, len(items))
	for i, item := range items {
		path := dialect.Index(root.Field(keyTools), i)
		tool, err := dialect.ParseObject(item, path, root.Options())
		if err != nil {
			return nil, err
		}
		typ, _, err := tool.String(keyType)
		if err != nil {
			return nil, err
		}
		if typ != llms.InferenceFunction {
			return nil, tool.Unsupported(keyType)
		}
		def := tool
		if nested {
			fn, ok, err := tool.Object(keyFunction)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, router.Invalid(tool.Field(keyFunction), "function definition is required")
			}
			def = fn
		}
		name, _, err := def.String(keyName)
		if err != nil {
			return nil, err
		}
		description, _, err := def.String(keyDescription)
		if err != nil {
			return nil, err
		}
		parameters := def.Raw(keyParameters)
		if dialect.IsNull(parameters) {
			parameters = emptyParameters
		} else if !dialect.IsObject(parameters) {
			return nil, router.Invalid(def.Field(keyParameters), "expected a JSON Schema object")
		}
		var strict *bool
		if v, ok, err := def.Bool(keyStrict); err != nil {
			return nil, err
		} else if ok {
			strict = &v
		}
		if err := def.Finish(); err != nil {
			return nil, err
		}
		if nested {
			if err := tool.Finish(); err != nil {
				return nil, err
			}
		}
		out = append(out, llms.InferenceTool{
			Name:        name,
			Description: description,
			Parameters:  parameters,
			Strict:      strict,
		})
	}
	return out, nil
}

// decodeToolChoice parses a string mode or a named function choice.
func decodeToolChoice(root *dialect.Object, nested bool) (*llms.InferenceToolChoice, error) {
	raw := root.Raw(keyToolChoice)
	if raw == nil {
		return nil, nil
	}
	path := root.Field(keyToolChoice)
	if dialect.IsString(raw) {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, router.Invalid(path, "expected a string")
		}
		switch s {
		case choiceAuto:
			return &llms.InferenceToolChoice{Mode: llms.ToolChoiceAuto}, nil
		case choiceNone:
			return &llms.InferenceToolChoice{Mode: llms.ToolChoiceNone}, nil
		case choiceRequired:
			return &llms.InferenceToolChoice{Mode: llms.ToolChoiceRequired}, nil
		}
		return nil, router.Unsupported(path, "unsupported tool choice")
	}
	choice, err := dialect.ParseObject(raw, path, root.Options())
	if err != nil {
		return nil, err
	}
	typ, _, err := choice.String(keyType)
	if err != nil {
		return nil, err
	}
	if typ != llms.InferenceFunction {
		return nil, choice.Unsupported(keyType)
	}
	def := choice
	if nested {
		fn, ok, err := choice.Object(keyFunction)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, router.Invalid(choice.Field(keyFunction), "function reference is required")
		}
		def = fn
	}
	name, ok, err := def.String(keyName)
	if err != nil {
		return nil, err
	}
	if !ok || name == "" {
		return nil, router.Invalid(def.Field(keyName), "function name is required")
	}
	if err := def.Finish(); err != nil {
		return nil, err
	}
	if nested {
		if err := choice.Finish(); err != nil {
			return nil, err
		}
	}
	return &llms.InferenceToolChoice{
		Mode: llms.ToolChoiceFunction,
		Name: name,
	}, nil
}

// decodeFormat parses a response format object. nested selects the Chat shape
// with a json_schema sub-object over the inline Responses shape.
func decodeFormat(raw json.RawMessage, path string, opts dialect.DecodeOptions, nested bool) (*llms.InferenceFormat, error) {
	if dialect.IsNull(raw) {
		return nil, nil
	}
	format, err := dialect.ParseObject(raw, path, opts)
	if err != nil {
		return nil, err
	}
	typ, _, err := format.String(keyType)
	if err != nil {
		return nil, err
	}
	out := &llms.InferenceFormat{}
	switch typ {
	case formatTypeText:
		out.Type = llms.FormatText
	case formatTypeJSON:
		out.Type = llms.FormatJSONObject
	case formatTypeSchema:
		out.Type = llms.FormatJSONSchema
	default:
		return nil, format.Unsupported(keyType)
	}
	def := format
	if nested {
		schema, ok, err := format.Object(keyJSONSchema)
		if err != nil {
			return nil, err
		}
		if ok {
			if out.Type != llms.FormatJSONSchema {
				return nil, router.Invalid(format.Field(keyJSONSchema), "json_schema requires type json_schema")
			}
			def = schema
		} else if out.Type == llms.FormatJSONSchema {
			return nil, router.Invalid(format.Field(keyJSONSchema), "json_schema definition is required")
		}
	}
	if out.Type == llms.FormatJSONSchema {
		name, _, err := def.String(keyName)
		if err != nil {
			return nil, err
		}
		out.Name = name
		if _, _, err := def.String(keyDescription); err != nil {
			return nil, err
		}
		schema := def.Raw(keySchema)
		if !dialect.IsObject(schema) {
			return nil, router.Invalid(def.Field(keySchema), "expected a JSON Schema object")
		}
		out.Schema = schema
		if v, _, err := def.Bool(keyStrict); err != nil {
			return nil, err
		} else {
			out.Strict = v
		}
		if err := def.Finish(); err != nil {
			return nil, err
		}
	}
	if err := format.Finish(); err != nil {
		return nil, err
	}
	return out, nil
}

// decodeEffort maps a reasoning effort string.
func decodeEffort(o *dialect.Object, key string) (llms.Effort, bool, error) {
	s, ok, err := o.String(key)
	if err != nil || !ok {
		return "", false, err
	}
	switch s {
	case effortMinimal:
		return llms.EffortMinimal, true, nil
	case effortLow:
		return llms.EffortLow, true, nil
	case effortMedium:
		return llms.EffortMedium, true, nil
	case effortHigh:
		return llms.EffortHigh, true, nil
	}
	return "", true, o.Unsupported(key)
}

// textKinds maps accepted part types to block types for one role.
type textKinds map[string]llms.BlockType

// decodeTextParts accepts a string or an array of text-like parts. Absent or
// null content returns nil blocks without error; callers decide whether that
// is acceptable for the role.
func decodeTextParts(raw json.RawMessage, path string, opts dialect.DecodeOptions, kinds textKinds) ([]llms.InferenceBlock, error) {
	if dialect.IsNull(raw) {
		return nil, nil
	}
	if dialect.IsString(raw) {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, router.Invalid(path, "expected a string")
		}
		return []llms.InferenceBlock{{
			Type: llms.BlockText,
			Text: s,
		}}, nil
	}
	if !dialect.IsArray(raw) {
		return nil, router.Invalid(path, "expected a string or an array of content parts")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, router.Invalid(path, "expected an array of content parts")
	}
	out := make([]llms.InferenceBlock, 0, len(items))
	for i, item := range items {
		part, err := dialect.ParseObject(item, dialect.Index(path, i), opts)
		if err != nil {
			return nil, err
		}
		typ, _, err := part.String(keyType)
		if err != nil {
			return nil, err
		}
		blockType, ok := kinds[typ]
		if !ok {
			return nil, part.Unsupported(keyType)
		}
		textKey := keyText
		if blockType == llms.BlockRefusal {
			textKey = keyRefusal
		}
		text, _, err := part.String(textKey)
		if err != nil {
			return nil, err
		}
		// Annotations are legitimate output metadata; they carry no content.
		part.Raw(keyAnnotations)
		if err := part.Finish(); err != nil {
			return nil, err
		}
		out = append(out, llms.InferenceBlock{
			Type: blockType,
			Text: text,
		})
	}
	return out, nil
}

// joinText concatenates text blocks for fields that require a single string.
func joinText(blocks []llms.InferenceBlock) string {
	var b strings.Builder
	for _, block := range blocks {
		b.WriteString(block.Text)
	}
	return b.String()
}

// openReasoning turns a sealed opaque string into block fields.
func openReasoning(sealed string, text string) (llms.InferenceBlock, error) {
	block := llms.InferenceBlock{
		Type: llms.BlockReasoning,
		Text: text,
	}
	if sealed == "" {
		return block, nil
	}
	source, state, err := dialect.OpenOpaque(sealed)
	if err != nil {
		return block, err
	}
	block.Source = source
	block.Opaque = state
	return block, nil
}

// usageMap encodes usage for one endpoint's field names.
func usageMap(u llms.Usage, inputKey, outputKey, inputDetailsKey, outputDetailsKey string) map[string]any {
	return map[string]any{
		inputKey:       u.InputTokens,
		outputKey:      u.OutputTokens,
		keyTotalTokens: u.TotalTokens,
		inputDetailsKey: map[string]any{
			keyCachedTokens: u.CacheReadTokens,
		},
		outputDetailsKey: map[string]any{
			keyReasoningTokens: u.ReasoningTokens,
		},
	}
}

// marshal serializes a response payload.
func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, router.NewError(router.KindInternal, "", "serialize response")
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// publicMetadata returns request metadata without the router-internal user key.
func publicMetadata(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		if k == dialect.MetadataUser {
			continue
		}
		out[k] = v
	}
	return out
}

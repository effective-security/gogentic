package anthropic

import (
	"encoding/json"
	"strings"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/router"
	"github.com/effective-security/gogentic/router/dialect"
)

// Request field names.
const (
	fieldModel             = "model"
	fieldMaxTokens         = "max_tokens"
	fieldSystem            = "system"
	fieldMessages          = "messages"
	fieldTools             = "tools"
	fieldToolChoice        = "tool_choice"
	fieldTemperature       = "temperature"
	fieldTopP              = "top_p"
	fieldTopK              = "top_k"
	fieldStopSequences     = "stop_sequences"
	fieldThinking          = "thinking"
	fieldOutputConfig      = "output_config"
	fieldMetadata          = "metadata"
	fieldStream            = "stream"
	fieldServiceTier       = "service_tier"
	fieldMCPServers        = "mcp_servers"
	fieldContainer         = "container"
	fieldContextManagement = "context_management"
	fieldBetas             = "betas"
	fieldRole              = "role"
	fieldContent           = "content"
	fieldType              = "type"
	fieldText              = "text"
	fieldID                = "id"
	fieldName              = "name"
	fieldInput             = "input"
	fieldToolUseID         = "tool_use_id"
	fieldIsError           = "is_error"
	fieldCacheControl      = "cache_control"
	fieldThinkingText      = "thinking"
	fieldSignature         = "signature"
	fieldData              = "data"
	fieldDescription       = "description"
	fieldInputSchema       = "input_schema"
	fieldStrict            = "strict"
	fieldDisableParallel   = "disable_parallel_tool_use"
	fieldBudgetTokens      = "budget_tokens"
	fieldFormat            = "format"
	fieldSchema            = "schema"
	fieldEffort            = "effort"
	fieldUserID            = "user_id"
)

// Wire vocabulary.
const (
	roleUser      = "user"
	roleAssistant = "assistant"

	blockText             = "text"
	blockToolUse          = "tool_use"
	blockToolResult       = "tool_result"
	blockThinking         = "thinking"
	blockRedactedThinking = "redacted_thinking"

	toolTypeCustom = "custom"

	choiceAuto = "auto"
	choiceAny  = "any"
	choiceTool = "tool"
	choiceNone = "none"

	thinkingEnabled  = "enabled"
	thinkingAdaptive = "adaptive"
	thinkingDisabled = "disabled"

	formatJSONSchema  = "json_schema"
	defaultFormatName = "output"

	serviceTierAuto         = "auto"
	serviceTierStandardOnly = "standard_only"

	effortLow    = "low"
	effortMedium = "medium"
	effortHigh   = "high"

	emptyObjectSchema = `{"type":"object","properties":{},"additionalProperties":false}`
	emptyArguments    = `{}`
)

// DecodeMessages converts an Anthropic Messages request body into a router
// request. Unsupported controls are rejected by name; see the package ledger.
func DecodeMessages(raw []byte, opts dialect.DecodeOptions) (router.Request, error) {
	var req router.Request
	if err := dialect.Inspect(raw, opts); err != nil {
		return req, err
	}
	o, err := dialect.ParseObject(raw, "", opts)
	if err != nil {
		return req, err
	}
	if req.Model, _, err = o.String(fieldModel); err != nil {
		return req, err
	}
	if err := decodeControls(o, &req); err != nil {
		return req, err
	}
	if err := decodeSystem(o, &req); err != nil {
		return req, err
	}
	if err := decodeMessages(o, &req); err != nil {
		return req, err
	}
	if err := decodeTools(o, &req); err != nil {
		return req, err
	}
	if err := decodeToolChoice(o, &req); err != nil {
		return req, err
	}
	if err := decodeThinking(o, &req); err != nil {
		return req, err
	}
	if err := decodeOutputConfig(o, &req); err != nil {
		return req, err
	}
	if err := decodeMetadata(o, &req); err != nil {
		return req, err
	}
	if err := rejectUnsupportedTopLevel(o); err != nil {
		return req, err
	}
	return req, o.Finish()
}

func decodeControls(o *dialect.Object, req *router.Request) error {
	maxTokens, present, err := o.Int(fieldMaxTokens)
	if err != nil {
		return err
	}
	if !present {
		return router.Invalid(fieldMaxTokens, "max_tokens is required")
	}
	v := int(maxTokens)
	req.Input.MaxTokens = &v
	if t, present, err := o.Float(fieldTemperature); err != nil {
		return err
	} else if present {
		req.Input.Temperature = &t
	}
	if p, present, err := o.Float(fieldTopP); err != nil {
		return err
	} else if present {
		req.Input.TopP = &p
	}
	if o.Has(fieldTopK) {
		o.Raw(fieldTopK)
		return o.Unsupported(fieldTopK)
	}
	stops, _, err := o.Array(fieldStopSequences)
	if err != nil {
		return err
	}
	for i, s := range stops {
		var stop string
		if err := json.Unmarshal(s, &stop); err != nil {
			return router.Invalid(dialect.Index(fieldStopSequences, i), "expected a string")
		}
		req.Input.Stop = append(req.Input.Stop, stop)
	}
	if stream, present, err := o.Bool(fieldStream); err != nil {
		return err
	} else if present && stream {
		return o.Unsupported(fieldStream)
	}
	if tier, present, err := o.String(fieldServiceTier); err != nil {
		return err
	} else if present && tier != serviceTierAuto && tier != serviceTierStandardOnly {
		return o.Unsupported(fieldServiceTier)
	}
	return nil
}

func rejectUnsupportedTopLevel(o *dialect.Object) error {
	for _, key := range []string{fieldMCPServers, fieldBetas} {
		items, present, err := o.Array(key)
		if err != nil {
			return o.Unsupported(key)
		}
		if present && len(items) > 0 {
			return o.Unsupported(key)
		}
	}
	for _, key := range []string{fieldContainer, fieldContextManagement} {
		if o.Raw(key) != nil {
			return o.Unsupported(key)
		}
	}
	return nil
}

func decodeSystem(o *dialect.Object, req *router.Request) error {
	raw := o.Raw(fieldSystem)
	if raw == nil {
		return nil
	}
	blocks, err := textBlocks(raw, o.Field(fieldSystem), o.Options())
	if err != nil {
		return err
	}
	if len(blocks) == 0 {
		return nil
	}
	req.Input.Messages = append(req.Input.Messages, llms.InferenceMessage{
		Role:    llms.InferenceRoleSystem,
		Content: blocks,
	})
	return nil
}

// textBlocks decodes a string or an array of text blocks into text blocks.
func textBlocks(raw json.RawMessage, path string, opts dialect.DecodeOptions) ([]llms.InferenceBlock, error) {
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
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, router.Invalid(path, "expected a string or an array of text blocks")
	}
	out := make([]llms.InferenceBlock, 0, len(items))
	for i, item := range items {
		b, err := dialect.ParseObject(item, dialect.Index(path, i), opts)
		if err != nil {
			return nil, err
		}
		typ, _, err := b.String(fieldType)
		if err != nil {
			return nil, err
		}
		if typ != blockText {
			return nil, router.Unsupported(b.Field(fieldType), "only text blocks are supported here")
		}
		text, _, err := b.String(fieldText)
		if err != nil {
			return nil, err
		}
		if err := checkCacheControl(b); err != nil {
			return nil, err
		}
		if err := b.Finish(); err != nil {
			return nil, err
		}
		out = append(out, llms.InferenceBlock{
			Type: llms.BlockText,
			Text: text,
		})
	}
	return out, nil
}

// checkCacheControl ignores cache_control in lenient mode and rejects it in strict.
func checkCacheControl(o *dialect.Object) error {
	if o.Raw(fieldCacheControl) != nil && o.Options().Strict {
		return o.Unsupported(fieldCacheControl)
	}
	return nil
}

func decodeMessages(o *dialect.Object, req *router.Request) error {
	items, present, err := o.Array(fieldMessages)
	if err != nil {
		return err
	}
	if !present {
		return router.Invalid(fieldMessages, "messages is required")
	}
	for i, item := range items {
		m, err := dialect.ParseObject(item, dialect.Index(fieldMessages, i), o.Options())
		if err != nil {
			return err
		}
		role, _, err := m.String(fieldRole)
		if err != nil {
			return err
		}
		raw := m.Raw(fieldContent)
		if raw == nil {
			return router.Invalid(m.Field(fieldContent), "content is required")
		}
		switch role {
		case roleUser:
			msgs, err := decodeUserContent(raw, m.Field(fieldContent), o.Options())
			if err != nil {
				return err
			}
			req.Input.Messages = append(req.Input.Messages, msgs...)
		case roleAssistant:
			blocks, err := decodeAssistantContent(raw, m.Field(fieldContent), o.Options())
			if err != nil {
				return err
			}
			req.Input.Messages = append(req.Input.Messages, llms.InferenceMessage{
				Role:    llms.InferenceRoleAssistant,
				Content: blocks,
			})
		default:
			return router.Invalid(m.Field(fieldRole), "role must be user or assistant")
		}
		if err := m.Finish(); err != nil {
			return err
		}
	}
	return nil
}

// decodeUserContent splits tool results (one tool-role message) from user text.
func decodeUserContent(raw json.RawMessage, path string, opts dialect.DecodeOptions) ([]llms.InferenceMessage, error) {
	if dialect.IsString(raw) {
		blocks, err := textBlocks(raw, path, opts)
		if err != nil {
			return nil, err
		}
		return []llms.InferenceMessage{{
			Role:    llms.InferenceRoleUser,
			Content: blocks,
		}}, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, router.Invalid(path, "expected a string or an array of content blocks")
	}
	var results, texts []llms.InferenceBlock
	for i, item := range items {
		b, err := dialect.ParseObject(item, dialect.Index(path, i), opts)
		if err != nil {
			return nil, err
		}
		typ, _, err := b.String(fieldType)
		if err != nil {
			return nil, err
		}
		switch typ {
		case blockText:
			text, _, err := b.String(fieldText)
			if err != nil {
				return nil, err
			}
			texts = append(texts, llms.InferenceBlock{
				Type: llms.BlockText,
				Text: text,
			})
		case blockToolResult:
			block, err := decodeToolResult(b)
			if err != nil {
				return nil, err
			}
			results = append(results, block)
		default:
			return nil, router.Unsupported(b.Field(fieldType), "unsupported content block type")
		}
		if err := checkCacheControl(b); err != nil {
			return nil, err
		}
		if err := b.Finish(); err != nil {
			return nil, err
		}
	}
	var out []llms.InferenceMessage
	if len(results) > 0 {
		out = append(out, llms.InferenceMessage{
			Role:    llms.InferenceRoleTool,
			Content: results,
		})
	}
	if len(texts) > 0 || len(results) == 0 {
		out = append(out, llms.InferenceMessage{
			Role:    llms.InferenceRoleUser,
			Content: texts,
		})
	}
	return out, nil
}

func decodeToolResult(b *dialect.Object) (llms.InferenceBlock, error) {
	id, _, err := b.String(fieldToolUseID)
	if err != nil {
		return llms.InferenceBlock{}, err
	}
	if isError, present, err := b.Bool(fieldIsError); err != nil {
		return llms.InferenceBlock{}, err
	} else if present && isError && b.Options().Strict {
		return llms.InferenceBlock{}, b.Unsupported(fieldIsError)
	}
	var text strings.Builder
	if raw := b.Raw(fieldContent); raw != nil {
		blocks, err := textBlocks(raw, b.Field(fieldContent), b.Options())
		if err != nil {
			return llms.InferenceBlock{}, err
		}
		for _, tb := range blocks {
			text.WriteString(tb.Text)
		}
	}
	return llms.InferenceBlock{
		Type: llms.BlockFunctionOutput,
		ID:   id,
		Text: text.String(),
	}, nil
}

func decodeAssistantContent(raw json.RawMessage, path string, opts dialect.DecodeOptions) ([]llms.InferenceBlock, error) {
	if dialect.IsString(raw) {
		return textBlocks(raw, path, opts)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, router.Invalid(path, "expected a string or an array of content blocks")
	}
	out := make([]llms.InferenceBlock, 0, len(items))
	for i, item := range items {
		b, err := dialect.ParseObject(item, dialect.Index(path, i), opts)
		if err != nil {
			return nil, err
		}
		typ, _, err := b.String(fieldType)
		if err != nil {
			return nil, err
		}
		var block llms.InferenceBlock
		switch typ {
		case blockText:
			text, _, err := b.String(fieldText)
			if err != nil {
				return nil, err
			}
			block = llms.InferenceBlock{
				Type: llms.BlockText,
				Text: text,
			}
		case blockToolUse:
			block, err = decodeToolUse(b)
		case blockThinking:
			block, err = decodeThinkingBlock(b)
		case blockRedactedThinking:
			block, err = decodeRedactedThinking(b)
		default:
			return nil, router.Unsupported(b.Field(fieldType), "unsupported content block type")
		}
		if err != nil {
			return nil, err
		}
		if err := checkCacheControl(b); err != nil {
			return nil, err
		}
		if err := b.Finish(); err != nil {
			return nil, err
		}
		out = append(out, block)
	}
	return out, nil
}

func decodeToolUse(b *dialect.Object) (llms.InferenceBlock, error) {
	id, _, err := b.String(fieldID)
	if err != nil {
		return llms.InferenceBlock{}, err
	}
	name, _, err := b.String(fieldName)
	if err != nil {
		return llms.InferenceBlock{}, err
	}
	arguments := emptyArguments
	if input := b.Raw(fieldInput); input != nil {
		if !dialect.IsObject(input) {
			return llms.InferenceBlock{}, router.Invalid(b.Field(fieldInput), "expected an object")
		}
		arguments = string(input)
	}
	return llms.InferenceBlock{
		Type:      llms.BlockFunctionCall,
		ID:        id,
		Name:      name,
		Arguments: arguments,
	}, nil
}

func decodeThinkingBlock(b *dialect.Object) (llms.InferenceBlock, error) {
	text, _, err := b.String(fieldThinkingText)
	if err != nil {
		return llms.InferenceBlock{}, err
	}
	signature, _, err := b.String(fieldSignature)
	if err != nil {
		return llms.InferenceBlock{}, err
	}
	block := llms.InferenceBlock{
		Type: llms.BlockReasoning,
		Text: text,
	}
	if signature != "" {
		if block.Source, block.Opaque, err = dialect.OpenOpaque(signature); err != nil {
			return llms.InferenceBlock{}, router.Invalid(b.Field(fieldSignature), "signature is not a router-issued value")
		}
	}
	return block, nil
}

func decodeRedactedThinking(b *dialect.Object) (llms.InferenceBlock, error) {
	data, _, err := b.String(fieldData)
	if err != nil {
		return llms.InferenceBlock{}, err
	}
	block := llms.InferenceBlock{
		Type: llms.BlockReasoning,
	}
	if block.Source, block.Opaque, err = dialect.OpenOpaque(data); err != nil {
		return llms.InferenceBlock{}, router.Invalid(b.Field(fieldData), "data is not a router-issued value")
	}
	return block, nil
}

func decodeTools(o *dialect.Object, req *router.Request) error {
	items, _, err := o.Array(fieldTools)
	if err != nil {
		return err
	}
	for i, item := range items {
		t, err := dialect.ParseObject(item, dialect.Index(fieldTools, i), o.Options())
		if err != nil {
			return err
		}
		typ, present, err := t.String(fieldType)
		if err != nil {
			return err
		}
		if present && typ != toolTypeCustom {
			return router.Unsupported(t.Field(fieldType), "only custom function tools are supported")
		}
		name, _, err := t.String(fieldName)
		if err != nil {
			return err
		}
		description, _, err := t.String(fieldDescription)
		if err != nil {
			return err
		}
		tool := llms.InferenceTool{
			Name:        name,
			Description: description,
			Parameters:  json.RawMessage(emptyObjectSchema),
		}
		if schema := t.Raw(fieldInputSchema); schema != nil {
			if !dialect.IsObject(schema) {
				return router.Invalid(t.Field(fieldInputSchema), "expected an object")
			}
			tool.Parameters = schema
		}
		if strict, present, err := t.Bool(fieldStrict); err != nil {
			return err
		} else if present {
			tool.Strict = &strict
		}
		if err := checkCacheControl(t); err != nil {
			return err
		}
		if err := t.Finish(); err != nil {
			return err
		}
		req.Input.Tools = append(req.Input.Tools, tool)
	}
	return nil
}

func decodeToolChoice(o *dialect.Object, req *router.Request) error {
	c, present, err := o.Object(fieldToolChoice)
	if err != nil || !present {
		return err
	}
	typ, _, err := c.String(fieldType)
	if err != nil {
		return err
	}
	choice := &llms.InferenceToolChoice{}
	switch typ {
	case choiceAuto:
		choice.Mode = llms.ToolChoiceAuto
	case choiceAny:
		choice.Mode = llms.ToolChoiceRequired
	case choiceNone:
		choice.Mode = llms.ToolChoiceNone
	case choiceTool:
		choice.Mode = llms.ToolChoiceFunction
		if choice.Name, _, err = c.String(fieldName); err != nil {
			return err
		}
	default:
		return router.Invalid(c.Field(fieldType), "tool_choice type must be auto, any, tool or none")
	}
	if disable, present, err := c.Bool(fieldDisableParallel); err != nil {
		return err
	} else if present {
		parallel := !disable
		req.Input.ParallelToolCalls = &parallel
	}
	req.Input.ToolChoice = choice
	return c.Finish()
}

func decodeThinking(o *dialect.Object, req *router.Request) error {
	t, present, err := o.Object(fieldThinking)
	if err != nil || !present {
		return err
	}
	typ, _, err := t.String(fieldType)
	if err != nil {
		return err
	}
	switch typ {
	case thinkingEnabled:
		reasoning := &llms.InferenceReasoning{}
		if budget, present, err := t.Int(fieldBudgetTokens); err != nil {
			return err
		} else if present {
			v := int(budget)
			reasoning.BudgetTokens = &v
		}
		req.Input.Reasoning = reasoning
	case thinkingAdaptive:
		req.Input.Reasoning = &llms.InferenceReasoning{
			Effort: llms.EffortMedium,
		}
	case thinkingDisabled:
	default:
		return router.Invalid(t.Field(fieldType), "thinking type must be enabled, adaptive or disabled")
	}
	return t.Finish()
}

func decodeOutputConfig(o *dialect.Object, req *router.Request) error {
	c, present, err := o.Object(fieldOutputConfig)
	if err != nil || !present {
		return err
	}
	if f, present, err := c.Object(fieldFormat); err != nil {
		return err
	} else if present {
		typ, _, err := f.String(fieldType)
		if err != nil {
			return err
		}
		if typ != formatJSONSchema {
			return router.Unsupported(f.Field(fieldType), "only json_schema output is supported")
		}
		schema := f.Raw(fieldSchema)
		if !dialect.IsObject(schema) {
			return router.Invalid(f.Field(fieldSchema), "expected a schema object")
		}
		name, present, err := f.String(fieldName)
		if err != nil {
			return err
		}
		if !present || name == "" {
			name = defaultFormatName
		}
		req.Input.Format = &llms.InferenceFormat{
			Type:   llms.FormatJSONSchema,
			Name:   name,
			Schema: schema,
			Strict: true,
		}
		if err := f.Finish(); err != nil {
			return err
		}
	}
	if effort, present, err := c.String(fieldEffort); err != nil {
		return err
	} else if present {
		mapped, ok := mapEffort(effort)
		if !ok {
			return router.Invalid(c.Field(fieldEffort), "effort must be low, medium or high")
		}
		if req.Input.Reasoning == nil {
			req.Input.Reasoning = &llms.InferenceReasoning{
				Effort: mapped,
			}
		}
	}
	return c.Finish()
}

func mapEffort(effort string) (llms.Effort, bool) {
	switch effort {
	case effortLow:
		return llms.EffortLow, true
	case effortMedium:
		return llms.EffortMedium, true
	case effortHigh:
		return llms.EffortHigh, true
	}
	return "", false
}

func decodeMetadata(o *dialect.Object, req *router.Request) error {
	m, present, err := o.Object(fieldMetadata)
	if err != nil || !present {
		return err
	}
	if user, present, err := m.String(fieldUserID); err != nil {
		return err
	} else if present {
		req.Metadata = map[string]string{
			dialect.MetadataUser: user,
		}
	}
	return m.Finish()
}

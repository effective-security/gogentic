package openai

import (
	"encoding/json"
	"strings"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/router"
	"github.com/effective-security/gogentic/router/dialect"
	"github.com/google/uuid"
)

var (
	responsesInputKinds  = textKinds{partInputText: llms.BlockText}
	responsesOutputKinds = textKinds{
		partOutputText: llms.BlockText,
		partRefusal:    llms.BlockRefusal,
	}
)

// DecodeResponses translates a Responses request body into a router request.
func DecodeResponses(raw []byte, opts dialect.DecodeOptions) (router.Request, error) {
	var req router.Request
	root, err := decodeRoot(raw, opts)
	if err != nil {
		return req, err
	}
	if err := decodeCommon(root, &req); err != nil {
		return req, err
	}
	in := &req.Input
	if instructions, ok, err := root.String(keyInstructions); err != nil {
		return req, err
	} else if ok {
		in.Messages = append(in.Messages, llms.InferenceMessage{
			Role: llms.InferenceRoleSystem,
			Content: []llms.InferenceBlock{{
				Type: llms.BlockText,
				Text: instructions,
			}},
		})
	}
	input := root.Raw(keyInput)
	inputPath := root.Field(keyInput)
	switch {
	case input == nil:
		return req, router.Invalid(inputPath, "input is required")
	case dialect.IsString(input):
		var s string
		if err := json.Unmarshal(input, &s); err != nil {
			return req, router.Invalid(inputPath, "expected a string")
		}
		in.Messages = append(in.Messages, llms.InferenceMessage{
			Role: llms.InferenceRoleUser,
			Content: []llms.InferenceBlock{{
				Type: llms.BlockText,
				Text: s,
			}},
		})
	case dialect.IsArray(input):
		var items []json.RawMessage
		if err := json.Unmarshal(input, &items); err != nil {
			return req, router.Invalid(inputPath, "expected an array of input items")
		}
		for i, item := range items {
			if err := decodeResponsesItem(in, item, dialect.Index(inputPath, i), opts); err != nil {
				return req, err
			}
		}
	default:
		return req, router.Invalid(inputPath, "expected a string or an array of input items")
	}
	if in.Tools, err = decodeTools(root, false); err != nil {
		return req, err
	}
	if in.ToolChoice, err = decodeToolChoice(root, false); err != nil {
		return req, err
	}
	if v, ok, err := root.Int(keyMaxOutputTokens); err != nil {
		return req, err
	} else if ok {
		n := int(v)
		in.MaxTokens = &n
	}
	if text, ok, err := root.Object(keyText); err != nil {
		return req, err
	} else if ok {
		if in.Format, err = decodeFormat(text.Raw(keyFormat), text.Field(keyFormat), opts, false); err != nil {
			return req, err
		}
		if verbosity, ok, err := text.String(keyVerbosity); err != nil {
			return req, err
		} else if ok && verbosity != verbosityDefault {
			return req, text.Unsupported(keyVerbosity)
		}
		if err := text.Finish(); err != nil {
			return req, err
		}
	}
	if reasoning, ok, err := root.Object(keyReasoning); err != nil {
		return req, err
	} else if ok {
		effort, hasEffort, err := decodeEffort(reasoning, keyEffort)
		if err != nil {
			return req, err
		}
		if hasEffort {
			in.Reasoning = &llms.InferenceReasoning{Effort: effort}
		}
		// Summary verbosity is a display preference; connectors always request
		// whatever summary the provider offers.
		reasoning.Raw(keySummary)
		if err := reasoning.Finish(); err != nil {
			return req, err
		}
	}
	if err := rejectResponsesControls(root); err != nil {
		return req, err
	}
	return req, root.Finish()
}

// rejectResponsesControls rejects stateful, streaming and unsupported controls.
func rejectResponsesControls(root *dialect.Object) error {
	if include, ok, err := root.Array(keyInclude); err != nil {
		return err
	} else if ok {
		for _, item := range include {
			var s string
			if json.Unmarshal(item, &s) != nil || s != includeReasoning {
				return root.Unsupported(keyInclude)
			}
		}
	}
	if err := rejectBool(root, keyBackground, false); err != nil {
		return err
	}
	if truncation, ok, err := root.String(keyTruncation); err != nil {
		return err
	} else if ok && truncation != truncationDisabled {
		return root.Unsupported(keyTruncation)
	}
	for _, key := range []string{keyPreviousResponse, keyConversation, keyPrompt, keyMaxToolCalls} {
		if err := rejectPresent(root, key); err != nil {
			return err
		}
	}
	return nil
}

// decodeResponsesItem appends one input item to the portable history, merging
// consecutive assistant items and consecutive function outputs into one message.
func decodeResponsesItem(in *llms.InferenceRequest, raw json.RawMessage, path string, opts dialect.DecodeOptions) error {
	item, err := dialect.ParseObject(raw, path, opts)
	if err != nil {
		return err
	}
	typ, ok, err := item.String(keyType)
	if err != nil {
		return err
	}
	if !ok {
		typ = itemMessage
	}
	// Item IDs and statuses label completed history; they never retrieve state.
	item.Raw(keyID)
	item.Raw(keyStatus)
	switch typ {
	case itemMessage:
		role, _, err := item.String(keyRole)
		if err != nil {
			return err
		}
		if role == roleTool {
			return router.Invalid(item.Field(keyRole), "tool results use function_call_output items")
		}
		kinds := responsesInputKinds
		if role == roleAssistant {
			kinds = responsesOutputKinds
		}
		blocks, err := decodeTextParts(item.Raw(keyContent), item.Field(keyContent), opts, kinds)
		if err != nil {
			return err
		}
		if blocks == nil {
			return router.Invalid(item.Field(keyContent), "content is required")
		}
		if err := item.Finish(); err != nil {
			return err
		}
		appendBlocks(in, llms.InferenceRole(role), blocks, role == roleAssistant)
	case itemFunctionCall:
		callID, _, err := item.String(keyCallID)
		if err != nil {
			return err
		}
		name, _, err := item.String(keyName)
		if err != nil {
			return err
		}
		arguments, _, err := item.String(keyArguments)
		if err != nil {
			return err
		}
		if err := item.Finish(); err != nil {
			return err
		}
		appendBlocks(in, llms.InferenceRoleAssistant, []llms.InferenceBlock{{
			Type:      llms.BlockFunctionCall,
			ID:        callID,
			Name:      name,
			Arguments: arguments,
		}}, true)
	case itemFunctionOutput:
		callID, _, err := item.String(keyCallID)
		if err != nil {
			return err
		}
		output := item.Raw(keyOutput)
		blocks, err := decodeTextParts(output, item.Field(keyOutput), opts, responsesInputKinds)
		if err != nil {
			return err
		}
		if blocks == nil {
			return router.Invalid(item.Field(keyOutput), "output is required")
		}
		if err := item.Finish(); err != nil {
			return err
		}
		appendBlocks(in, llms.InferenceRoleTool, []llms.InferenceBlock{{
			Type: llms.BlockFunctionOutput,
			ID:   callID,
			Text: joinText(blocks),
		}}, true)
	case itemReasoning:
		block, err := decodeReasoningItem(item, opts)
		if err != nil {
			return err
		}
		appendBlocks(in, llms.InferenceRoleAssistant, []llms.InferenceBlock{block}, true)
	default:
		return item.Unsupported(keyType)
	}
	return nil
}

func decodeReasoningItem(item *dialect.Object, opts dialect.DecodeOptions) (llms.InferenceBlock, error) {
	var texts []string
	summaries, ok, err := item.Array(keySummary)
	if err != nil {
		return llms.InferenceBlock{}, err
	}
	if ok {
		for i, raw := range summaries {
			part, err := dialect.ParseObject(raw, dialect.Index(item.Field(keySummary), i), opts)
			if err != nil {
				return llms.InferenceBlock{}, err
			}
			typ, _, err := part.String(keyType)
			if err != nil {
				return llms.InferenceBlock{}, err
			}
			if typ != partSummaryText {
				return llms.InferenceBlock{}, part.Unsupported(keyType)
			}
			text, _, err := part.String(keyText)
			if err != nil {
				return llms.InferenceBlock{}, err
			}
			if err := part.Finish(); err != nil {
				return llms.InferenceBlock{}, err
			}
			texts = append(texts, text)
		}
	}
	// Raw reasoning content, when a provider exposes it, duplicates the summary.
	item.Raw(keyContent)
	sealed, _, err := item.String(keyEncryptedContent)
	if err != nil {
		return llms.InferenceBlock{}, err
	}
	if err := item.Finish(); err != nil {
		return llms.InferenceBlock{}, err
	}
	return openReasoning(sealed, strings.Join(texts, summarySeparator))
}

// appendBlocks adds blocks to the last message when it has the same role and
// merging is allowed, otherwise starts a new message.
func appendBlocks(in *llms.InferenceRequest, role llms.InferenceRole, blocks []llms.InferenceBlock, merge bool) {
	if n := len(in.Messages); merge && n > 0 && in.Messages[n-1].Role == role {
		in.Messages[n-1].Content = append(in.Messages[n-1].Content, blocks...)
		return
	}
	in.Messages = append(in.Messages, llms.InferenceMessage{
		Role:    role,
		Content: blocks,
	})
}

// EncodeResponses renders a router result as a response object. The original
// request supplies echoed controls and metadata.
func EncodeResponses(res *router.Result, req router.Request, opts dialect.EncodeOptions) ([]byte, error) {
	if res == nil {
		return nil, router.NewError(router.KindInternal, "", "nil result")
	}
	status := statusCompleted
	var incomplete any
	switch res.Response.FinishReason {
	case llms.FinishLength:
		status = statusIncomplete
		incomplete = map[string]any{keyReason: reasonMaxTokens}
	case llms.FinishContentFilter:
		status = statusIncomplete
		incomplete = map[string]any{keyReason: reasonFilter}
	}
	output := make([]any, 0, len(res.Response.Content))
	var message map[string]any
	var parts []any
	flushMessage := func() {
		if message != nil {
			message[keyContent] = parts
			output = append(output, message)
			message = nil
			parts = nil
		}
	}
	for _, b := range res.Response.Content {
		switch b.Type {
		case llms.BlockText, llms.BlockRefusal:
			if message == nil {
				message = map[string]any{
					keyID:     messageIDPrefix + uuid.NewString(),
					keyType:   itemMessage,
					keyRole:   roleAssistant,
					keyStatus: status,
				}
			}
			if b.Type == llms.BlockRefusal {
				parts = append(parts, map[string]any{
					keyType:    partRefusal,
					keyRefusal: b.Text,
				})
			} else {
				parts = append(parts, map[string]any{
					keyType:        partOutputText,
					keyText:        b.Text,
					keyAnnotations: []any{},
				})
			}
		case llms.BlockFunctionCall:
			flushMessage()
			output = append(output, map[string]any{
				keyID:        callIDPrefix + uuid.NewString(),
				keyType:      itemFunctionCall,
				keyCallID:    b.ID,
				keyName:      b.Name,
				keyArguments: b.Arguments,
				keyStatus:    status,
			})
		case llms.BlockReasoning:
			if opts.OmitReasoning {
				continue
			}
			flushMessage()
			item, err := encodeReasoningItem(res.Model, b)
			if err != nil {
				return nil, err
			}
			output = append(output, item)
		}
	}
	flushMessage()
	var usage any
	if res.Response.UsageKnown {
		usage = usageMap(res.Response.Usage, keyInputTokens, keyOutputTokens, keyInputDetails, keyOutputDetails)
	}
	out := map[string]any{
		keyID:                res.ID,
		keyObject:            objectResponse,
		keyCreatedAt:         res.CreatedAt,
		keyModel:             res.Model,
		keyStatus:            status,
		keyError:             nil,
		keyIncompleteDetails: incomplete,
		keyOutput:            output,
		keyUsage:             usage,
		keyStore:             false,
		keyBackground:        false,
		keyTruncation:        truncationDisabled,
		keyMetadata:          publicMetadata(req.Metadata),
		keyTools:             encodeTools(req.Input.Tools),
		keyToolChoice:        encodeToolChoice(req.Input.ToolChoice),
		keyParallelToolCalls: req.Input.ParallelToolCalls == nil || *req.Input.ParallelToolCalls,
		keyTemperature:       req.Input.Temperature,
		keyTopP:              req.Input.TopP,
		keyMaxOutputTokens:   req.Input.MaxTokens,
		keyText:              encodeTextConfig(req.Input.Format),
	}
	if req.Input.Reasoning != nil {
		out[keyReasoning] = map[string]any{
			keyEffort: string(req.Input.Reasoning.Effort),
		}
	}
	return marshal(out)
}

func encodeReasoningItem(model string, b llms.InferenceBlock) (map[string]any, error) {
	summary := []any{}
	if b.Text != "" {
		summary = append(summary, map[string]any{
			keyType: partSummaryText,
			keyText: b.Text,
		})
	}
	item := map[string]any{
		keyID:      reasoningIDPrefix + uuid.NewString(),
		keyType:    itemReasoning,
		keySummary: summary,
	}
	if len(b.Opaque) > 0 {
		sealed, err := dialect.SealOpaque(model, b.Opaque)
		if err != nil {
			return nil, router.NewError(router.KindInternal, "", "seal reasoning state")
		}
		item[keyEncryptedContent] = sealed
	}
	return item, nil
}

func encodeTools(tools []llms.InferenceTool) []any {
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		tool := map[string]any{
			keyType:        llms.InferenceFunction,
			keyName:        t.Name,
			keyDescription: t.Description,
			keyParameters:  t.Parameters,
		}
		if t.Strict != nil {
			tool[keyStrict] = *t.Strict
		}
		out = append(out, tool)
	}
	return out
}

func encodeToolChoice(c *llms.InferenceToolChoice) any {
	if c == nil {
		return choiceAuto
	}
	switch c.Mode {
	case llms.ToolChoiceFunction:
		return map[string]any{
			keyType: llms.InferenceFunction,
			keyName: c.Name,
		}
	case llms.ToolChoiceNone:
		return choiceNone
	case llms.ToolChoiceRequired:
		return choiceRequired
	}
	return choiceAuto
}

func encodeTextConfig(f *llms.InferenceFormat) map[string]any {
	format := map[string]any{keyType: formatTypeText}
	if f != nil {
		switch f.Type {
		case llms.FormatJSONObject:
			format[keyType] = formatTypeJSON
		case llms.FormatJSONSchema:
			format = map[string]any{
				keyType:   formatTypeSchema,
				keyName:   f.Name,
				keySchema: f.Schema,
				keyStrict: f.Strict,
			}
		}
	}
	return map[string]any{keyFormat: format}
}

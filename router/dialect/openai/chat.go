package openai

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/router"
	"github.com/effective-security/gogentic/router/dialect"
)

var (
	chatInstructionKinds = textKinds{partText: llms.BlockText}
	chatAssistantKinds   = textKinds{
		partText:    llms.BlockText,
		partRefusal: llms.BlockRefusal,
	}
)

// DecodeChat translates a Chat Completions request body into a router request.
func DecodeChat(raw []byte, opts dialect.DecodeOptions) (router.Request, error) {
	var req router.Request
	root, err := decodeRoot(raw, opts)
	if err != nil {
		return req, err
	}
	if err := decodeCommon(root, &req); err != nil {
		return req, err
	}
	in := &req.Input
	messages, ok, err := root.Array(keyMessages)
	if err != nil {
		return req, err
	}
	if !ok {
		return req, router.Invalid(keyMessages, "expected an array of messages")
	}
	for i, item := range messages {
		msg, err := decodeChatMessage(item, dialect.Index(root.Field(keyMessages), i), opts)
		if err != nil {
			return req, err
		}
		in.Messages = append(in.Messages, msg)
	}
	if in.Tools, err = decodeTools(root, true); err != nil {
		return req, err
	}
	if in.ToolChoice, err = decodeToolChoice(root, true); err != nil {
		return req, err
	}
	maxTokens, hasMax, err := root.Int(keyMaxTokens)
	if err != nil {
		return req, err
	}
	maxCompletion, hasCompletion, err := root.Int(keyMaxCompletion)
	if err != nil {
		return req, err
	}
	switch {
	case hasMax && hasCompletion:
		return req, router.Invalid(keyMaxTokens, "max_tokens and max_completion_tokens are mutually exclusive")
	case hasMax:
		v := int(maxTokens)
		in.MaxTokens = &v
	case hasCompletion:
		v := int(maxCompletion)
		in.MaxTokens = &v
	}
	if in.Stop, err = decodeStop(root); err != nil {
		return req, err
	}
	if v, ok, err := root.Int(keySeed); err != nil {
		return req, err
	} else if ok {
		in.Seed = &v
	}
	if in.Format, err = decodeFormat(root.Raw(keyResponseFormat), root.Field(keyResponseFormat), opts, true); err != nil {
		return req, err
	}
	if effort, ok, err := decodeEffort(root, keyReasoningEffort); err != nil {
		return req, err
	} else if ok {
		in.Reasoning = &llms.InferenceReasoning{Effort: effort}
	}
	if err := rejectChatControls(root); err != nil {
		return req, err
	}
	return req, root.Finish()
}

// rejectChatControls rejects known Chat fields whose values are not the default.
func rejectChatControls(root *dialect.Object) error {
	if err := rejectInt(root, keyN, 1); err != nil {
		return err
	}
	if err := rejectBool(root, keyLogprobs, false); err != nil {
		return err
	}
	if err := rejectNonEmptyObject(root, keyLogitBias); err != nil {
		return err
	}
	if err := rejectFloat(root, keyFrequencyPenalty, 0); err != nil {
		return err
	}
	if err := rejectFloat(root, keyPresencePenalty, 0); err != nil {
		return err
	}
	if modalities, ok, err := root.Array(keyModalities); err != nil {
		return err
	} else if ok {
		var s string
		if len(modalities) != 1 || json.Unmarshal(modalities[0], &s) != nil || s != modalityText {
			return root.Unsupported(keyModalities)
		}
	}
	for _, key := range []string{keyAudio, keyPrediction, keyFunctions, keyFunctionCall, keyWebSearchOptions} {
		if err := rejectPresent(root, key); err != nil {
			return err
		}
	}
	if verbosity, ok, err := root.String(keyVerbosity); err != nil {
		return err
	} else if ok && verbosity != verbosityDefault {
		return root.Unsupported(keyVerbosity)
	}
	return nil
}

func decodeStop(root *dialect.Object) ([]string, error) {
	raw := root.Raw(keyStop)
	if raw == nil {
		return nil, nil
	}
	path := root.Field(keyStop)
	if dialect.IsString(raw) {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, router.Invalid(path, "expected a string")
		}
		return []string{s}, nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, router.Invalid(path, "expected a string or an array of strings")
	}
	return list, nil
}

func decodeChatMessage(item json.RawMessage, path string, opts dialect.DecodeOptions) (llms.InferenceMessage, error) {
	var out llms.InferenceMessage
	msg, err := dialect.ParseObject(item, path, opts, keyContent, keyRefusal)
	if err != nil {
		return out, err
	}
	role, _, err := msg.String(keyRole)
	if err != nil {
		return out, err
	}
	out.Role = llms.InferenceRole(role)
	content := msg.Raw(keyContent)
	toolCallID, hasToolCallID, err := msg.String(keyToolCallID)
	if err != nil {
		return out, err
	}
	if hasToolCallID && role != roleTool {
		return out, router.Invalid(msg.Field(keyToolCallID), "tool_call_id requires the tool role")
	}
	// Participant names carry no content for the router.
	msg.Raw(keyName)
	switch out.Role {
	case llms.InferenceRoleSystem, llms.InferenceRoleDeveloper, llms.InferenceRoleUser:
		blocks, err := decodeTextParts(content, msg.Field(keyContent), opts, chatInstructionKinds)
		if err != nil {
			return out, err
		}
		if blocks == nil {
			return out, router.Invalid(msg.Field(keyContent), "content is required")
		}
		out.Content = blocks
	case llms.InferenceRoleAssistant:
		blocks, err := decodeChatAssistant(msg, content, opts)
		if err != nil {
			return out, err
		}
		out.Content = blocks
	case llms.InferenceRoleTool:
		if !hasToolCallID || toolCallID == "" {
			return out, router.Invalid(msg.Field(keyToolCallID), "tool messages require tool_call_id")
		}
		if msg.Raw(keyToolCalls) != nil {
			return out, router.Invalid(msg.Field(keyToolCalls), "tool messages cannot carry tool calls")
		}
		blocks, err := decodeTextParts(content, msg.Field(keyContent), opts, chatInstructionKinds)
		if err != nil {
			return out, err
		}
		if blocks == nil {
			return out, router.Invalid(msg.Field(keyContent), "content is required")
		}
		out.Content = []llms.InferenceBlock{{
			Type: llms.BlockFunctionOutput,
			ID:   toolCallID,
			Text: joinText(blocks),
		}}
	default:
		return out, router.Invalid(msg.Field(keyRole), "unsupported role")
	}
	return out, msg.Finish()
}

// decodeChatAssistant orders blocks as reasoning, text/refusal, then tool calls.
func decodeChatAssistant(msg *dialect.Object, content json.RawMessage, opts dialect.DecodeOptions) ([]llms.InferenceBlock, error) {
	var blocks []llms.InferenceBlock
	details, ok, err := msg.Array(keyReasoningDetails)
	if err != nil {
		return nil, err
	}
	if ok {
		for i, item := range details {
			block, err := decodeReasoningDetail(item, dialect.Index(msg.Field(keyReasoningDetails), i), opts)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		}
	}
	text, err := decodeTextParts(content, msg.Field(keyContent), opts, chatAssistantKinds)
	if err != nil {
		return nil, err
	}
	blocks = append(blocks, text...)
	if !dialect.IsNull(msg.Raw(keyRefusal)) {
		refusal, _, err := msg.String(keyRefusal)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, llms.InferenceBlock{
			Type: llms.BlockRefusal,
			Text: refusal,
		})
	}
	msg.Raw(keyAnnotations)
	if err := rejectPresent(msg, keyAudio); err != nil {
		return nil, err
	}
	if err := rejectPresent(msg, keyFunctionCall); err != nil {
		return nil, err
	}
	calls, ok, err := msg.Array(keyToolCalls)
	if err != nil {
		return nil, err
	}
	if ok {
		for i, item := range calls {
			block, err := decodeChatToolCall(item, dialect.Index(msg.Field(keyToolCalls), i), opts)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		}
	}
	if len(blocks) == 0 {
		return nil, router.Invalid(msg.Field(keyContent), "assistant messages require content or tool calls")
	}
	return blocks, nil
}

func decodeReasoningDetail(item json.RawMessage, path string, opts dialect.DecodeOptions) (llms.InferenceBlock, error) {
	detail, err := dialect.ParseObject(item, path, opts)
	if err != nil {
		return llms.InferenceBlock{}, err
	}
	typ, _, err := detail.String(keyType)
	if err != nil {
		return llms.InferenceBlock{}, err
	}
	detail.Raw(keyID)
	detail.Raw(keyFormat)
	detail.Raw(keyIndex)
	var block llms.InferenceBlock
	switch typ {
	case detailEncrypted:
		data, _, err := detail.String(keyData)
		if err != nil {
			return block, err
		}
		if block, err = openReasoning(data, ""); err != nil {
			return block, err
		}
	case detailSummary:
		summary, _, err := detail.String(keySummary)
		if err != nil {
			return block, err
		}
		block = llms.InferenceBlock{
			Type: llms.BlockReasoning,
			Text: summary,
		}
	case detailText:
		text, _, err := detail.String(keyText)
		if err != nil {
			return block, err
		}
		block = llms.InferenceBlock{
			Type: llms.BlockReasoning,
			Text: text,
		}
	default:
		return block, detail.Unsupported(keyType)
	}
	return block, detail.Finish()
}

func decodeChatToolCall(item json.RawMessage, path string, opts dialect.DecodeOptions) (llms.InferenceBlock, error) {
	var block llms.InferenceBlock
	call, err := dialect.ParseObject(item, path, opts)
	if err != nil {
		return block, err
	}
	typ, _, err := call.String(keyType)
	if err != nil {
		return block, err
	}
	if typ != llms.InferenceFunction {
		return block, call.Unsupported(keyType)
	}
	id, _, err := call.String(keyID)
	if err != nil {
		return block, err
	}
	fn, ok, err := call.Object(keyFunction)
	if err != nil {
		return block, err
	}
	if !ok {
		return block, router.Invalid(call.Field(keyFunction), "function call is required")
	}
	name, _, err := fn.String(keyName)
	if err != nil {
		return block, err
	}
	arguments, _, err := fn.String(keyArguments)
	if err != nil {
		return block, err
	}
	if err := fn.Finish(); err != nil {
		return block, err
	}
	if err := call.Finish(); err != nil {
		return block, err
	}
	return llms.InferenceBlock{
		Type:      llms.BlockFunctionCall,
		ID:        id,
		Name:      name,
		Arguments: arguments,
	}, nil
}

// EncodeChat renders a router result as a chat.completion object.
func EncodeChat(res *router.Result, opts dialect.EncodeOptions) ([]byte, error) {
	if res == nil {
		return nil, router.NewError(router.KindInternal, "", "nil result")
	}
	var text strings.Builder
	var refusal *string
	var calls []any
	var details []any
	for _, b := range res.Response.Content {
		switch b.Type {
		case llms.BlockText:
			text.WriteString(b.Text)
		case llms.BlockRefusal:
			v := b.Text
			refusal = &v
		case llms.BlockFunctionCall:
			calls = append(calls, map[string]any{
				keyID:   b.ID,
				keyType: llms.InferenceFunction,
				keyFunction: map[string]any{
					keyName:      b.Name,
					keyArguments: b.Arguments,
				},
			})
		case llms.BlockReasoning:
			if opts.OmitReasoning {
				continue
			}
			items, err := encodeReasoningDetails(res.Model, b, len(details))
			if err != nil {
				return nil, err
			}
			details = append(details, items...)
		}
	}
	var content any
	if text.Len() > 0 || (len(calls) == 0 && refusal == nil) {
		content = text.String()
	}
	message := map[string]any{
		keyRole:    roleAssistant,
		keyContent: content,
		keyRefusal: refusal,
	}
	if len(calls) > 0 {
		message[keyToolCalls] = calls
	}
	if len(details) > 0 {
		message[keyReasoningDetails] = details
	}
	out := map[string]any{
		keyID:      chatIDPrefix + strings.TrimPrefix(res.ID, responseIDPrefix),
		keyObject:  objectChat,
		keyCreated: res.CreatedAt,
		keyModel:   res.Model,
		keyChoices: []any{map[string]any{
			keyIndex:        0,
			keyMessage:      message,
			keyFinishReason: string(res.Response.FinishReason),
			keyLogprobs:     nil,
		}},
	}
	if res.Response.UsageKnown {
		out[keyUsage] = usageMap(res.Response.Usage, keyPromptTokens, keyCompletionTokens, keyPromptDetails, keyCompletionDetails)
	}
	return marshal(out)
}

// encodeReasoningDetails renders one reasoning block as OpenRouter-compatible
// reasoning_details items: an encrypted item for opaque state and a summary item
// for visible text.
func encodeReasoningDetails(model string, b llms.InferenceBlock, index int) ([]any, error) {
	var items []any
	id := reasoningIDPrefix + strconv.Itoa(index)
	if len(b.Opaque) > 0 {
		sealed, err := dialect.SealOpaque(model, b.Opaque)
		if err != nil {
			return nil, router.NewError(router.KindInternal, "", "seal reasoning state")
		}
		items = append(items, map[string]any{
			keyType:   detailEncrypted,
			keyID:     id,
			keyData:   sealed,
			keyFormat: detailFormat,
		})
	}
	if b.Text != "" {
		items = append(items, map[string]any{
			keyType:    detailSummary,
			keyID:      id,
			keySummary: b.Text,
			keyFormat:  detailFormat,
		})
	}
	return items, nil
}

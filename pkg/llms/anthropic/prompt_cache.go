package anthropic

import (
	"strings"

	sdkanthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
)

// Terminology used in this file:
//   - "breakpoint": Anthropic prompt-cache breakpoint (a cacheable block/tool marker).
//   - "target": User-facing selector from llms.PromptCachePolicy (message part or tool).
//   - "original message/part indexes": Indexes into the caller-provided []llms.Message and
//     message.Parts before Anthropic conversion.
//   - "Anthropic indexes": Indexes into MessageNewParams.System, MessageNewParams.Messages,
//     and MessageParam.Content after conversion.
//
// The helpers below build and use a mapping between those two index spaces so callers can
// target prompt cache breakpoints using gogentic's generic message model.
const maxAnthropicPromptCacheBreakpoints = 4

// promptCachePartKey is a user-facing selector key in the original gogentic message space.
type promptCachePartKey struct {
	MessageIndex int
	PartIndex    int
}

// promptCachePartLocation maps an original gogentic message part to its Anthropic SDK location.
// A part can end up either in the top-level System blocks or in a chat message content block.
type promptCachePartLocation struct {
	IsSystem     bool
	SystemIndex  int
	MessageIndex int
	ContentIndex int
}

// promptCacheTargetDedupKey normalizes breakpoint targets for duplicate detection.
type promptCacheTargetDedupKey struct {
	Kind         llms.PromptCacheTargetKind
	MessageIndex int
	PartIndex    int
	ToolIndex    int
}

// processMessagesForRequest converts gogentic messages into Anthropic request params while also
// returning a reverse lookup map from original message/part indexes to Anthropic block indexes.
func processMessagesForRequest(messages []llms.Message) ([]sdkanthropic.MessageParam, []sdkanthropic.TextBlockParam,
	map[promptCachePartKey]promptCachePartLocation, error,
) {
	chatMessages := make([]sdkanthropic.MessageParam, 0, len(messages))
	systemBlocks := make([]sdkanthropic.TextBlockParam, 0)
	partLocations := make(map[promptCachePartKey]promptCachePartLocation)

	for msgIndex, msg := range messages {
		if len(msg.Parts) == 0 {
			continue
		}

		switch msg.Role {
		case llms.RoleSystem:
			for partIndex, part := range msg.Parts {
				text, err := HandleSystemMessage(llms.Message{
					Parts: []llms.ContentPart{part},
				})
				if err != nil {
					return nil, nil, nil, errors.Wrap(err, "anthropic: failed to handle system message")
				}

				systemBlocks = append(systemBlocks, sdkanthropic.TextBlockParam{
					Type: "text",
					Text: text,
				})
				partLocations[promptCachePartKey{MessageIndex: msgIndex, PartIndex: partIndex}] = promptCachePartLocation{
					IsSystem:    true,
					SystemIndex: len(systemBlocks) - 1,
				}
			}

		case llms.RoleHuman:
			chatMessage, err := HandleHumanMessage(msg)
			if err != nil {
				return nil, nil, nil, errors.Wrap(err, "anthropic: failed to handle human message")
			}
			if len(chatMessage.Content) != len(msg.Parts) {
				return nil, nil, nil, errors.Errorf("anthropic: unexpected content mapping length for human message: parts=%d content=%d", len(msg.Parts), len(chatMessage.Content))
			}
			chatMessages = append(chatMessages, chatMessage)
			messagePos := len(chatMessages) - 1
			for partIndex := range msg.Parts {
				partLocations[promptCachePartKey{MessageIndex: msgIndex, PartIndex: partIndex}] = promptCachePartLocation{
					MessageIndex: messagePos,
					ContentIndex: partIndex,
				}
			}

		case llms.RoleAI, llms.RoleGeneric:
			chatMessage, err := HandleAIMessage(msg)
			if err != nil {
				return nil, nil, nil, errors.Wrap(err, "anthropic: failed to handle AI message")
			}
			if len(chatMessage.Content) != len(msg.Parts) {
				return nil, nil, nil, errors.Errorf("anthropic: unexpected content mapping length for AI message: parts=%d content=%d", len(msg.Parts), len(chatMessage.Content))
			}
			chatMessages = append(chatMessages, chatMessage)
			messagePos := len(chatMessages) - 1
			for partIndex := range msg.Parts {
				partLocations[promptCachePartKey{MessageIndex: msgIndex, PartIndex: partIndex}] = promptCachePartLocation{
					MessageIndex: messagePos,
					ContentIndex: partIndex,
				}
			}

		case llms.RoleTool:
			chatMessage, err := HandleToolMessage(msg)
			if err != nil {
				return nil, nil, nil, errors.WithMessage(err, "anthropic: failed to handle tool message")
			}
			if len(chatMessage.Content) != len(msg.Parts) {
				return nil, nil, nil, errors.Errorf("anthropic: unexpected content mapping length for tool message: parts=%d content=%d", len(msg.Parts), len(chatMessage.Content))
			}
			chatMessages = append(chatMessages, chatMessage)
			messagePos := len(chatMessages) - 1
			for partIndex := range msg.Parts {
				partLocations[promptCachePartKey{MessageIndex: msgIndex, PartIndex: partIndex}] = promptCachePartLocation{
					MessageIndex: messagePos,
					ContentIndex: partIndex,
				}
			}

		default:
			return nil, nil, nil, errors.WithMessagef(ErrUnsupportedMessageType, "anthropic: %v", msg.Role)
		}
	}

	return chatMessages, systemBlocks, partLocations, nil
}

// applyPromptCachePolicyToRequest resolves user-facing cache breakpoint targets (message parts/tools)
// into concrete Anthropic request fields and applies cache_control markers in-place.
func applyPromptCachePolicyToRequest(o *LLM, params *sdkanthropic.MessageNewParams, opts *llms.CallOptions,
	partLocations map[promptCachePartKey]promptCachePartLocation,
) ([]option.RequestOption, error) {
	if opts == nil || opts.PromptCachePolicy == nil || len(opts.PromptCachePolicy.Breakpoints) == 0 {
		return nil, nil
	}

	breakpoints := opts.PromptCachePolicy.Breakpoints
	if len(breakpoints) > maxAnthropicPromptCacheBreakpoints {
		return nil, errors.Errorf("anthropic: too many prompt cache breakpoints: %d (max %d)", len(breakpoints), maxAnthropicPromptCacheBreakpoints)
	}

	seen := make(map[promptCacheTargetDedupKey]struct{}, len(breakpoints))
	needsExtendedCacheTTLBeta := false

	for _, bp := range breakpoints {
		cacheControl, needsExtendedTTL, err := newAnthropicCacheControl(bp.TTL)
		if err != nil {
			return nil, err
		}
		if needsExtendedTTL {
			needsExtendedCacheTTLBeta = true
		}

		switch bp.Target.Kind {
		case llms.PromptCacheTargetMessagePart:
			if bp.Target.MessageIndex < 0 || bp.Target.PartIndex < 0 {
				return nil, errors.Errorf("anthropic: invalid prompt cache message_part target: message=%d part=%d", bp.Target.MessageIndex, bp.Target.PartIndex)
			}
			dupKey := promptCacheTargetDedupKey{
				Kind:         bp.Target.Kind,
				MessageIndex: bp.Target.MessageIndex,
				PartIndex:    bp.Target.PartIndex,
			}
			if _, exists := seen[dupKey]; exists {
				return nil, errors.Errorf("anthropic: duplicate prompt cache breakpoint for message[%d].part[%d]", bp.Target.MessageIndex, bp.Target.PartIndex)
			}
			seen[dupKey] = struct{}{}

			loc, ok := partLocations[promptCachePartKey{
				MessageIndex: bp.Target.MessageIndex,
				PartIndex:    bp.Target.PartIndex,
			}]
			if !ok {
				return nil, errors.Errorf("anthropic: prompt cache target not found for message[%d].part[%d]", bp.Target.MessageIndex, bp.Target.PartIndex)
			}

			if loc.IsSystem {
				if loc.SystemIndex < 0 || loc.SystemIndex >= len(params.System) {
					return nil, errors.Errorf("anthropic: invalid system prompt cache target index: %d", loc.SystemIndex)
				}
				params.System[loc.SystemIndex].CacheControl = cacheControl
				continue
			}

			if loc.MessageIndex < 0 || loc.MessageIndex >= len(params.Messages) {
				return nil, errors.Errorf("anthropic: invalid message prompt cache target index: %d", loc.MessageIndex)
			}
			if loc.ContentIndex < 0 || loc.ContentIndex >= len(params.Messages[loc.MessageIndex].Content) {
				return nil, errors.Errorf("anthropic: invalid message content prompt cache target index: %d", loc.ContentIndex)
			}

			cacheControlPtr := params.Messages[loc.MessageIndex].Content[loc.ContentIndex].GetCacheControl()
			if cacheControlPtr == nil {
				return nil, errors.Errorf("anthropic: prompt cache unsupported for message[%d].part[%d]", bp.Target.MessageIndex, bp.Target.PartIndex)
			}
			*cacheControlPtr = cacheControl

		case llms.PromptCacheTargetTool:
			if bp.Target.ToolIndex < 0 {
				return nil, errors.Errorf("anthropic: invalid prompt cache tool target: tool=%d", bp.Target.ToolIndex)
			}
			dupKey := promptCacheTargetDedupKey{
				Kind:      bp.Target.Kind,
				ToolIndex: bp.Target.ToolIndex,
			}
			if _, exists := seen[dupKey]; exists {
				return nil, errors.Errorf("anthropic: duplicate prompt cache breakpoint for tool[%d]", bp.Target.ToolIndex)
			}
			seen[dupKey] = struct{}{}

			if bp.Target.ToolIndex >= len(params.Tools) {
				return nil, errors.Errorf("anthropic: prompt cache tool target out of range: tool[%d]", bp.Target.ToolIndex)
			}
			cacheControlPtr := params.Tools[bp.Target.ToolIndex].GetCacheControl()
			if cacheControlPtr == nil {
				return nil, errors.Errorf("anthropic: prompt cache unsupported for tool[%d]", bp.Target.ToolIndex)
			}
			*cacheControlPtr = cacheControl

		default:
			return nil, errors.Errorf("anthropic: unsupported prompt cache target kind: %q", bp.Target.Kind)
		}
	}

	return promptCacheRequestOptions(o, needsExtendedCacheTTLBeta), nil
}

// newAnthropicCacheControl maps gogentic TTL values to Anthropic SDK cache_control params.
// The bool return value indicates whether the extended-cache-ttl beta header is required.
func newAnthropicCacheControl(ttl llms.PromptCacheTTL) (sdkanthropic.CacheControlEphemeralParam, bool, error) {
	cacheControl := sdkanthropic.NewCacheControlEphemeralParam()
	switch ttl {
	case "":
		return cacheControl, false, nil
	case llms.PromptCacheTTL5m:
		cacheControl.TTL = sdkanthropic.CacheControlEphemeralTTLTTL5m
		return cacheControl, false, nil
	case llms.PromptCacheTTL1h:
		cacheControl.TTL = sdkanthropic.CacheControlEphemeralTTLTTL1h
		return cacheControl, true, nil
	default:
		return sdkanthropic.CacheControlEphemeralParam{}, false, errors.Errorf("anthropic: unsupported prompt cache TTL: %q", ttl)
	}
}

// promptCacheRequestOptions appends per-request Anthropic beta headers needed by selected TTLs.
// This is request-scoped so we don't mutate the client-level default header configuration.
func promptCacheRequestOptions(o *LLM, needsExtendedCacheTTLBeta bool) []option.RequestOption {
	if o == nil || o.Options == nil || !needsExtendedCacheTTLBeta {
		return nil
	}

	betaToken := string(sdkanthropic.AnthropicBetaExtendedCacheTTL2025_04_11)
	if containsBetaHeaderToken(o.Options.AnthropicBetaHeader, betaToken) {
		return nil
	}

	headerValue := betaToken
	if strings.TrimSpace(o.Options.AnthropicBetaHeader) != "" {
		headerValue = strings.TrimSpace(o.Options.AnthropicBetaHeader) + "," + betaToken
	}
	return []option.RequestOption{
		option.WithHeader("anthropic-beta", headerValue),
	}
}

// containsBetaHeaderToken checks whether a comma-separated anthropic-beta header already contains
// the required token (whitespace-insensitive).
func containsBetaHeaderToken(headerValue, token string) bool {
	for _, part := range strings.Split(headerValue, ",") {
		if strings.TrimSpace(part) == token {
			return true
		}
	}
	return false
}

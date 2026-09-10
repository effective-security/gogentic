package router

import "time"

const (
	defaultSelectorTimeout  = 5 * time.Second
	maxModelNameBytes       = 256
	maxBackendNameBytes     = 1024
	maxMessages             = 1024
	maxBlocksPerMessage     = 256
	maxTools                = 128
	maxMetadataFields       = 16
	maxMetadataKeyBytes     = 64
	maxMetadataValueBytes   = 512
	maxTags                 = 32
	maxTagKeyBytes          = 64
	maxTagValueBytes        = 256
	maxToolDescriptionBytes = 4096
	maxSchemaBytes          = 64 << 10
	maxContentBytes         = 2 << 20
	maxOpaqueBytes          = 256 << 10
	maxOutputTokens         = 1 << 20
	maxStopSequences        = 4
	maxStopBytes            = 1024
	maxClassificationBytes  = 128
	maxAccountingTokens     = 1 << 53
	maxTemperature          = 2
	requestIDPrefix         = "resp_"
)

// Parameter names used in classified errors. Codecs may translate them.
const (
	paramModel          = "model"
	paramMessages       = "messages"
	paramMessagesRole   = "messages.role"
	paramContent        = "messages.content"
	paramToolCalls      = "messages.tool_calls"
	paramToolCallID     = "messages.tool_call_id"
	paramReasoning      = "messages.reasoning"
	paramTools          = "tools"
	paramToolChoice     = "tool_choice"
	paramToolsStrict    = "tools.strict"
	paramToolParameters = "tools.parameters"
	paramToolDesc       = "tools.description"
	paramMetadata       = "metadata"
	paramTemperature    = "temperature"
	paramTopP           = "top_p"
	paramMaxTokens      = "max_tokens"
	paramStop           = "stop"
	paramSeed           = "seed"
	paramFormat         = "response_format"
	paramReasoningCfg   = "reasoning"
)

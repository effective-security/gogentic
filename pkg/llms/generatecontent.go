package llms

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
)

// ErrUnexpectedRole is returned when a message role is of an unexpected type.
var ErrUnexpectedRole = errors.New("unexpected role")

// Role is the type of chat message.
type Role string

const (
	// RoleAI is a message sent by an AI.
	RoleAI Role = "ai"
	// RoleHuman is a message sent by a human.
	RoleHuman Role = "human"
	// RoleSystem is a message sent by the system.
	RoleSystem Role = "system"
	// RoleGeneric is a message sent by a generic user.
	RoleGeneric Role = "generic"
	// RoleTool is a message sent by a tool.
	RoleTool Role = "tool"
)

// Message is the message sent to a LLM. It has a role and a
// sequence of parts. For example, it can represent one message in a chat
// session sent by the user, in which case Role will be
// ChatMessageTypeHuman and Parts will be the sequence of items sent in
// this specific message.
type Message struct {
	Role  Role          `json:"role"`
	Parts []ContentPart `json:"parts"`
}

// TextPart creates TextContent from a given string.
func TextPart(s string) TextContent {
	return TextContent{Text: s}
}

// BinaryPart creates a new BinaryContent from the given MIME type (e.g.
// "image/png" and binary data).
func BinaryPart(mime string, data []byte) BinaryContent {
	return BinaryContent{
		MIMEType: mime,
		Data:     data,
	}
}

// ImageURLPart creates a new ImageURLContent from the given URL.
func ImageURLPart(url string) ImageURLContent {
	return ImageURLContent{
		URL: url,
	}
}

// ImageURLWithDetailPart creates a new ImageURLContent from the given URL and detail.
func ImageURLWithDetailPart(url string, detail string) ImageURLContent {
	return ImageURLContent{
		URL:    url,
		Detail: detail,
	}
}

// ContentPart is an interface all parts of content have to implement.
type ContentPart interface {
	isPart()
}

// TextContent is content with some text.
type TextContent struct {
	Text string `json:"text"`
}

func (tc TextContent) String() string {
	return tc.Text
}

func (TextContent) isPart() {}

// ImageURLContent is content with an URL pointing to an image.
type ImageURLContent struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // Detail is the detail of the image, e.g. "low", "high".
}

func (iuc ImageURLContent) String() string {
	return iuc.URL
}

func (ImageURLContent) isPart() {}

// BinaryContent is content holding some binary data with a MIME type.
type BinaryContent struct {
	MIMEType string `json:"mime_type"`
	Data     []byte `json:"data"`
}

func (bc BinaryContent) String() string {
	base64Encoded := base64.StdEncoding.EncodeToString(bc.Data)
	return "data:" + bc.MIMEType + ";base64," + base64Encoded
}

func (BinaryContent) isPart() {}

// FunctionCall is the name and arguments of a function call.
type FunctionCall struct {
	// The name of the function to call.
	Name string `json:"name"`
	// The arguments to pass to the function, as a JSON string.
	Arguments string `json:"arguments"`
}

// ToolCall is a call to a tool (as requested by the model) that should be executed.
type ToolCall struct {
	// ID is the unique identifier of the tool call.
	ID string `json:"id"`
	// Type is the type of the tool call. Typically, this would be "function".
	Type string `json:"type"`
	// FunctionCall is the function call to be executed.
	FunctionCall *FunctionCall `json:"function,omitempty"`
}

func (tc ToolCall) String() string {
	return fmt.Sprintf("ToolCall: %s (%s), input: %s", tc.ID, tc.FunctionCall.Name, tc.FunctionCall.Arguments)
}

func (ToolCall) isPart() {}

// ToolCallResponse is the response returned by a tool call.
type ToolCallResponse struct {
	// ToolCallID is the ID of the tool call this response is for.
	ToolCallID string `json:"tool_call_id"`
	// Name is the name of the tool that was called.
	Name string `json:"name"`
	// Content is the textual content of the response.
	Content string `json:"content"`
}

func (tc ToolCallResponse) String() string {
	return fmt.Sprintf("ToolCallResponse: %s (%s), response size: %d", tc.ToolCallID, tc.Name, len(tc.Content))
}

func (ToolCallResponse) isPart() {}

// ContentResponse is the response returned by a GenerateContent call.
// It can potentially return multiple content choices.
type ContentResponse struct {
	Choices []*ContentChoice
}

// ContentChoice is one of the response choices returned by GenerateContent
// calls.
type ContentChoice struct {
	// Content is the textual content of a response
	Content string `json:"content"`

	// StopReason is the reason the model stopped generating output.
	StopReason string `json:"stop_reason"`

	// GenerationInfo is arbitrary information the model adds to the response.
	GenerationInfo map[string]any `json:"generation_info"`

	// FuncCall is non-nil when the model asks to invoke a function/tool.
	// If a model invokes more than one function/tool, this field will only
	// contain the first one.
	FuncCall *FunctionCall `json:"func_call"`

	// ToolCalls is a list of tool calls the model asks to invoke.
	ToolCalls []ToolCall `json:"tool_calls"`

	// This field is only used with the deepseek-reasoner model and represents the reasoning contents of the assistant message before the final answer.
	ReasoningContent string `json:"reasoning_content"`
}

// MessageFromParts is a helper function to create a Message with a role and a
// list of parts.
func MessageFromParts(role Role, parts ...ContentPart) Message {
	result := Message{
		Role:  role,
		Parts: parts,
	}
	return result
}

// MessageFromTextParts is a helper function to create a Message with a role and a
// list of text parts.
func MessageFromTextParts(role Role, parts ...string) Message {
	result := Message{
		Role:  role,
		Parts: make([]ContentPart, 0, len(parts)),
	}
	for _, part := range parts {
		result.Parts = append(result.Parts, TextPart(part))
	}
	return result
}

// MessageFromToolCalls is a helper function to create a Message with a role and a
// list of tool calls.
func MessageFromToolCalls(role Role, toolCalls ...ToolCall) Message {
	result := Message{
		Role:  role,
		Parts: make([]ContentPart, 0, len(toolCalls)),
	}
	for _, toolCall := range toolCalls {
		result.Parts = append(result.Parts, ToolCall{
			ID:   toolCall.ID,
			Type: toolCall.Type,
			FunctionCall: &FunctionCall{
				Name:      toolCall.FunctionCall.Name,
				Arguments: toolCall.FunctionCall.Arguments,
			},
		})
	}
	return result
}

// MessageFromToolResponse is a helper function to create a Message with a role and a
// tool response.
func MessageFromToolResponse(role Role, toolResponse ToolCallResponse) Message {
	return MessageFromParts(role, ToolCallResponse{
		ToolCallID: toolResponse.ToolCallID,
		Name:       toolResponse.Name,
		Content:    toolResponse.Content,
	})
}

func (m Message) GetContent() string {
	var buf strings.Builder
	lastNewLine := true
	for _, p := range m.Parts {
		if !lastNewLine {
			buf.WriteString("\n")
		}
		switch typ := p.(type) {
		case TextContent:
			buf.WriteString(typ.Text)
			lastNewLine = strings.HasSuffix(typ.Text, "\n")
		case ImageURLContent:
			buf.WriteString("URL: ")
			buf.WriteString(typ.URL)
			lastNewLine = false
		case BinaryContent:
			buf.WriteString("Binary: ")
			buf.WriteString(typ.MIMEType)
			buf.WriteString("\n")
			buf.WriteString(base64.StdEncoding.EncodeToString(typ.Data))
			lastNewLine = false
		case ToolCall:
			buf.WriteString("Tool Call: ")
			js, _ := json.Marshal(typ)
			buf.Write(js)
			buf.WriteString("\n")
			lastNewLine = true
		case ToolCallResponse:
			buf.WriteString("Response: ")
			js, _ := json.Marshal(typ)
			buf.Write(js)
			buf.WriteString("\n")
			lastNewLine = true
		}
	}
	if !lastNewLine {
		buf.WriteString("\n")
	}
	return buf.String()
}

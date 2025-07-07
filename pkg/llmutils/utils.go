package llmutils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/x/values"
	"gopkg.in/yaml.v3"
)

// CleanJSON returns JSON by trimming prefixes and postfixes,
// this is more useful than TrimBackticks,
// ass LLM can reply like,
// `Here you go: {json}`
func CleanJSON(bs []byte) []byte {
	trimmedPrefix := trimPrefixBeforeJSON(bs)
	trimmedJSON := trimPostfixAfterJSON(trimmedPrefix)
	return trimmedJSON
}

// Removes any prefixes before the JSON (like "Sure, here you go:")
func trimPrefixBeforeJSON(bs []byte) []byte {
	startObject := bytes.IndexByte(bs, '{')
	startArray := bytes.IndexByte(bs, '[')

	var start int
	if startObject == -1 && startArray == -1 {
		return bs // No opening brace or bracket found, return the original string
	} else if startObject == -1 {
		start = startArray
	} else if startArray == -1 {
		start = startObject
	} else {
		start = min(startObject, startArray)
	}

	return bs[start:]
}

// Removes any postfixes after the JSON
func trimPostfixAfterJSON(bs []byte) []byte {
	endObject := bytes.LastIndexByte(bs, '}')
	endArray := bytes.LastIndexByte(bs, ']')

	var end int
	if endObject == -1 && endArray == -1 {
		return bs // No closing brace or bracket found, return the original string
	} else if endObject == -1 {
		end = endArray
	} else if endArray == -1 {
		end = endObject
	} else {
		end = max(endObject, endArray)
	}

	return bs[:end+1]
}

// TrimBackticks removes ```json or ```
func TrimBackticks(text string) string {
	return string(BytesTrimBackticks([]byte(text)))
}

var backtick = []byte("```")

// BytesTrimBackticks removes ```json or ```
func BytesTrimBackticks(bs []byte) []byte {
	size := len(bs)
	startIndex := bytes.Index(bs, backtick)
	if startIndex == -1 {
		// If the start marker is not found, return the original string directly
		return bs
	}
	startIndex += len(backtick)

	for i := startIndex; i < size && bs[i] != '{' && bs[i] != '['; i++ {
		if bs[i] == '\n' {
			startIndex = i + 1
			break
		}
	}

	// Calculate the string after removing the start marker and its preceding content
	contentAfterStart := bs[startIndex:]

	// Find the position of the last "```"
	endIndex := bytes.LastIndex(contentAfterStart, backtick)
	if endIndex == -1 {
		// If the end marker is not found, return the content after the start marker
		return contentAfterStart
	}

	// Extract the valid content in the middle
	result := contentAfterStart[:endIndex]

	return bytes.TrimSpace(result)
}

// StripComments removes <!--  --> comments from the LLM output
func StripComments(text string) string {
	// Remove the <!--
	before, after, ok := strings.Cut(text, "<!--")
	if ok {
		_, after2, ok := strings.Cut(after, "-->")
		if ok {
			if len(after2) > 1 && after2[0] == '\n' {
				after2 = after2[1:]
			}
			return before + after2
		}
	}
	// return as is
	return text
}

// RemoveAllComments removes all <!--  --> comments from the LLM output
func RemoveAllComments(input string) string {
	result := input
	for {
		// Keep removing comments until no more are found
		cleaned := StripComments(result)
		if cleaned == result {
			// No more comments found, we're done
			return cleaned
		}
		result = cleaned
	}
}

func AddComment(role, name, typ, content string) string {
	return fmt.Sprintf("<!-- @role=%s @name=%s @content=%s -->\n", role, name, typ) + content
}

func JSONIndent(body string) string {
	var buf bytes.Buffer
	_ = json.Indent(&buf, []byte(body), "", "\t")
	return buf.String()
}

func ToJSON(val any) string {
	js, _ := json.Marshal(val)
	return string(js)
}

func ToJSONIndent(val any) string {
	js, _ := json.MarshalIndent(val, "", "\t")
	return string(js)
}

func ToYAML(val any) string {
	js, _ := yaml.Marshal(val)
	return string(js)
}

func BackticksJSON(js string) string {
	return "\n```json\n" + strings.TrimSpace(js) + "\n```\n"
}

func BackticksYAM(js string) string {
	return "\n```yaml\n" + strings.TrimSpace(js) + "\n```\n"
}

type Stringer interface {
	String() string
}

func Stringify(s any) string {
	if v, ok := s.(Stringer); ok {
		return v.String()
	}
	if v, ok := s.(string); ok {
		return v
	}
	js, _ := json.MarshalIndent(s, "", "\t")
	return BackticksJSON(string(js))
}

func NewContentResponse(val any) *llms.ContentResponse {
	// Create a new ContentResponse with the given idRes1
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: Stringify(val),
			},
		},
	}
}

func MergeInputs(configInputs map[string]any, userInputs map[string]any) map[string]any {
	res := map[string]any{}
	for k, v := range configInputs {
		res[k] = v
	}
	// user input may override config default inputs
	for k, v := range userInputs {
		res[k] = v
	}
	return res
}

// PrintMessageContents is a debugging helper for MessageContent.
func PrintMessageContents(w io.Writer, msgs []llms.MessageContent) {
	for _, mc := range msgs {
		fmt.Fprintf(w, "%s: ", strings.ToUpper(string(mc.Role)))
		for _, p := range mc.Parts {
			switch pp := p.(type) {
			case llms.TextContent:
				fmt.Fprintln(w, pp.Text)
			case llms.ImageURLContent:
				fmt.Fprintln(w, pp.URL)
			case llms.BinaryContent:
				//fmt.Fprintf(w, "BinaryContent MIME=%q, size=%d\n", pp.MIMEType, len(pp.Data))
			case llms.ToolCall:
				fmt.Fprintf(w, "ToolCall ID=%s, Type=%s, Func=%s(%s)\n", pp.ID, pp.Type, pp.FunctionCall.Name, pp.FunctionCall.Arguments)
			case llms.ToolCallResponse:
				fmt.Fprintf(w, "ToolCallResponse ID=%s, Name=%s, Content=%s\n", pp.ToolCallID, pp.Name, pp.Content)
			default:
				//fmt.Fprintf(w, "unknown type %T\n", pp)
			}
		}
	}
}

// CountMessagesContentSize counts the size of the content in the messages
func CountMessagesContentSize(msgs []llms.MessageContent) uint64 {
	var size uint64
	for _, mc := range msgs {
		size += uint64(len(mc.Role))
		for _, p := range mc.Parts {
			switch pp := p.(type) {
			case llms.TextContent:
				size += uint64(len(pp.Text))
			case llms.ImageURLContent:
				size += uint64(len(pp.URL))
				size += uint64(len(pp.Detail))
			case llms.BinaryContent:
				size += uint64(len(pp.MIMEType))
				size += uint64(len(pp.Data))
			case llms.ToolCall:
				size += uint64(len(pp.ID))
				size += uint64(len(pp.Type))
				if pp.FunctionCall != nil {
					size += uint64(len(pp.FunctionCall.Name))
					size += uint64(len(pp.FunctionCall.Arguments))
				}
			case llms.ToolCallResponse:
				size += uint64(len(pp.ToolCallID))
				size += uint64(len(pp.Name))
				size += uint64(len(pp.Content))
			default:
				//fmt.Fprintf(w, "unknown type %T\n", pp)
			}
		}
	}
	return size
}

// CountResponseContentSize counts the size of the content in the content response
func CountResponseContentSize(resp *llms.ContentResponse) uint64 {
	var size uint64
	for _, choice := range resp.Choices {
		size += uint64(len(choice.Content))
		size += uint64(len(choice.ReasoningContent))
		if choice.FuncCall != nil {
			size += uint64(len(choice.FuncCall.Name))
			size += uint64(len(choice.FuncCall.Arguments))
		}
		for _, toolCall := range choice.ToolCalls {
			size += uint64(len(toolCall.ID))
			size += uint64(len(toolCall.Type))
			if toolCall.FunctionCall != nil {
				size += uint64(len(toolCall.FunctionCall.Name))
				size += uint64(len(toolCall.FunctionCall.Arguments))
			}
		}
	}
	return size
}

func CountTokens(resp *llms.ContentResponse) (in, out, total int64) {
	for _, choice := range resp.Choices {
		ma := values.MapAny(choice.GenerationInfo)
		in += ma.Int64("InputTokens")
		out += ma.Int64("OutputTokens")
		total += ma.Int64("TotalTokens")
	}
	return
}

func PrintChatMessages(w io.Writer, msgs []llms.ChatMessage, filter ...llms.ChatMessageType) {
	for _, mc := range msgs {
		if len(filter) > 0 && !slices.Contains(filter, mc.GetType()) {
			continue
		}
		fmt.Fprintf(w, "%s: ", strings.ToUpper(string(mc.GetType())))
		fmt.Fprintln(w, mc.GetContent())
	}
}

func FindLastUserQuestion(messages []llms.MessageContent) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == llms.ChatMessageTypeHuman {
			for _, part := range msg.Parts {
				if textPart, ok := part.(llms.TextContent); ok {
					return textPart.Text
				}
			}
		}
	}
	return ""
}

// ExtractTag extracts the content of a specific tag from the input string.
// The tag prefix can be # or @, and the content is extracted until the next space or newline.
func ExtractTag(input string, tagPrefix string) string {
	// Find the index of the tag prefix
	startIndex := strings.Index(input, tagPrefix)
	if startIndex == -1 {
		return "" // Tag not found
	}

	// Move the start index to the end of the tag prefix
	startIndex += len(tagPrefix)

	// Find the end index of the tag content (next space or newline)
	endIndex := strings.IndexAny(input[startIndex:], " \n")
	if endIndex == -1 {
		endIndex = len(input) // No space or newline found, take the rest of the string
	} else {
		endIndex += startIndex // Adjust for the substring
	}

	return input[startIndex:endIndex]
}

// EnsureEndsWithNewline ensures the message ends with a newline,
// it also removes any extra leading and trailing spaces.
func EnsureEndsWithNewline(s string) string {
	s = strings.TrimSpace(s)
	c := len(s)
	if c == 0 {
		return s
	}
	if s[c-1] != '\n' {
		return s + "\n"
	}
	return s
}

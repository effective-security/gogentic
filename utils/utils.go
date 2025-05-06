package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/tmc/langchaingo/llms"
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

func ToolClarificationComment(tool, clarification string) string {
	return fmt.Sprintf("<!-- @type=tool @name=%s @reason=clarification -->\n%s\n", tool, clarification)
}

func AssistantClarificationComment(agent, clarification string) string {
	return fmt.Sprintf("<!-- @type=assistant @name=%s @reason=clarification -->\n%s\n", agent, clarification)
}

func ToolErrorComment(tool, err string) string {
	return fmt.Sprintf("<!-- @type=tool @name=%s @reason=error -->\n%s\n", tool, err)
}

func AssistantErrorComment(agent, err string) string {
	return fmt.Sprintf("<!-- @type=assistant @name=%s @reason=error -->\n%s\n", agent, err)
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

// ShowMessageContents is a debugging helper for MessageContent.
func ShowMessageContents(w io.Writer, msgs []llms.MessageContent) {
	for i, mc := range msgs {
		fmt.Fprintf(w, "[%d] Role: %s\n", i, mc.Role)
		for _, p := range mc.Parts {
			switch pp := p.(type) {
			case llms.TextContent:
				fmt.Fprintln(w, pp.Text)
			case llms.ImageURLContent:
				fmt.Fprintln(w, pp.URL)
			case llms.BinaryContent:
				//fmt.Fprintf(w, "BinaryContent MIME=%q, size=%d\n", pp.MIMEType, len(pp.Data))
			case llms.ToolCall:
				fmt.Fprintf(w, "ToolCall ID=%v, Type=%v, Func=%v(%v)\n", pp.ID, pp.Type, pp.FunctionCall.Name, pp.FunctionCall.Arguments)
			case llms.ToolCallResponse:
				fmt.Fprintf(w, "ToolCallResponse ID=%v, Name=%v, Content=%v\n", pp.ToolCallID, pp.Name, pp.Content)
			default:
				//fmt.Fprintf(w, "unknown type %T\n", pp)
			}
		}
	}
}

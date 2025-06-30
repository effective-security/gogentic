package llms

import (
	"encoding/base64"
	"encoding/json"

	"github.com/cockroachdb/errors"
)

// JSON models following OpenAI schema

// MessageContentJSON represents the JSON structure for MessageContent
type MessageContentJSON struct {
	Role ChatMessageType `json:"role"`
	Text string          `json:"text,omitempty"`
}

// ContentPartJSON represents the JSON structure for content parts
type ContentPartJSON struct {
	Type         string            `json:"type"`
	Text         string            `json:"text,omitempty"`
	ImageURL     *ImageURLJSON     `json:"image_url,omitempty"`
	Binary       *BinaryJSON       `json:"binary,omitempty"`
	ToolCall     *ToolCallJSON     `json:"tool_call,omitempty"`
	ToolResponse *ToolResponseJSON `json:"tool_response,omitempty"`
}

// ImageURLJSON represents the JSON structure for image URL content
type ImageURLJSON struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// BinaryJSON represents the JSON structure for binary content
type BinaryJSON struct {
	Data     string `json:"data"`
	MIMEType string `json:"mime_type"`
}

// ToolCallJSON represents the JSON structure for tool call content
type ToolCallJSON struct {
	ID           string        `json:"id"`
	Type         string        `json:"type"`
	FunctionCall *FunctionCall `json:"function"`
}

// ToolResponseJSON represents the JSON structure for tool response content
type ToolResponseJSON struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Content    string `json:"content"`
}

// TextContentJSON represents the JSON structure for text content
type TextContentJSON struct {
	Text string `json:"text"`
	Type string `json:"type"`
}

// ImageURLContentJSON represents the JSON structure for image URL content
type ImageURLContentJSON struct {
	Type     string       `json:"type"`
	ImageURL ImageURLJSON `json:"image_url"`
}

// BinaryContentJSON represents the JSON structure for binary content
type BinaryContentJSON struct {
	Type   string     `json:"type"`
	Binary BinaryJSON `json:"binary"`
}

// ToolCallContentJSON represents the JSON structure for tool call content
type ToolCallContentJSON struct {
	Type     string       `json:"type"`
	ToolCall ToolCallJSON `json:"tool_call"`
}

// ToolResponseContentJSON represents the JSON structure for tool response content
type ToolResponseContentJSON struct {
	Type         string           `json:"type"`
	ToolResponse ToolResponseJSON `json:"tool_response"`
}

// MessageContentWithPartsJSON represents the JSON structure for MessageContent with parts
type MessageContentWithPartsJSON struct {
	Role  ChatMessageType `json:"role"`
	Parts []ContentPart   `json:"parts"`
}

// ToMessageContentWithPartsJSON converts MessageContent to MessageContentWithPartsJSON
func (mc *MessageContent) ToMessageContentWithPartsJSON() *MessageContentWithPartsJSON {
	return &MessageContentWithPartsJSON{
		Role:  mc.Role,
		Parts: mc.Parts,
	}
}

// ToToolResponseJSONOrdered converts ToolCallResponse to ToolResponseJSONOrdered
func (tc *ToolCallResponse) ToToolResponseJSONOrdered() *ToolResponseJSONOrdered {
	return &ToolResponseJSONOrdered{
		ToolCallID: tc.ToolCallID,
		Name:       tc.Name,
		Content:    tc.Content,
	}
}

// MarshalJSON implements json.Marshaler for MessageContent
func (mc MessageContent) MarshalJSON() ([]byte, error) {
	// Special case: single text part can be simplified
	if len(mc.Parts) == 1 {
		if tp, hasSingleTextPart := mc.Parts[0].(TextContent); hasSingleTextPart {
			return json.Marshal(MessageContentJSON{
				Role: mc.Role,
				Text: tp.Text,
			})
		}
	}

	// Multiple parts or non-text parts
	return json.Marshal(mc.ToMessageContentWithPartsJSON())
}

// UnmarshalJSON implements json.Unmarshaler for MessageContent
func (mc *MessageContent) UnmarshalJSON(data []byte) error {
	var msgJSON MessageContentJSON
	if err := json.Unmarshal(data, &msgJSON); err != nil {
		return err
	}

	mc.Role = msgJSON.Role

	// Handle special case: single text field
	if msgJSON.Text != "" {
		mc.Parts = []ContentPart{TextContent{Text: msgJSON.Text}}
		return nil
	}

	// Process parts - we need to unmarshal them manually since they're polymorphic
	var rawMsg map[string]any
	if err := json.Unmarshal(data, &rawMsg); err != nil {
		return err
	}

	if partsRaw, ok := rawMsg["parts"]; ok {
		partsArray, ok := partsRaw.([]any)
		if !ok {
			return errors.New("parts field must be an array")
		}

		for _, partRaw := range partsArray {
			partData, err := json.Marshal(partRaw)
			if err != nil {
				return err
			}

			var partJSON ContentPartJSON
			if err := json.Unmarshal(partData, &partJSON); err != nil {
				return err
			}

			part, err := unmarshalContentPart(partJSON)
			if err != nil {
				return err
			}
			mc.Parts = append(mc.Parts, part)
		}
	}

	return nil
}

// unmarshalContentPart converts ContentPartJSON to ContentPart
func unmarshalContentPart(partJSON ContentPartJSON) (ContentPart, error) {
	switch partJSON.Type {
	case "text", "":
		return TextContent{Text: partJSON.Text}, nil
	case "image_url":
		if partJSON.ImageURL == nil {
			return nil, errors.New("image_url field is required for image_url type")
		}
		return ImageURLContent{
			URL:    partJSON.ImageURL.URL,
			Detail: partJSON.ImageURL.Detail,
		}, nil
	case "binary":
		if partJSON.Binary == nil {
			return nil, errors.New("binary field is required for binary type")
		}
		decoded, err := base64.StdEncoding.DecodeString(partJSON.Binary.Data)
		if err != nil {
			return nil, errors.Wrap(err, "failed to decode binary data")
		}
		return BinaryContent{
			MIMEType: partJSON.Binary.MIMEType,
			Data:     decoded,
		}, nil
	case "tool_call":
		if partJSON.ToolCall == nil {
			return nil, errors.New("tool_call field is required for tool_call type")
		}
		return ToolCall{
			ID:           partJSON.ToolCall.ID,
			Type:         partJSON.ToolCall.Type,
			FunctionCall: partJSON.ToolCall.FunctionCall,
		}, nil
	case "tool_response":
		if partJSON.ToolResponse == nil {
			return nil, errors.New("tool_response field is required for tool_response type")
		}
		return ToolCallResponse{
			ToolCallID: partJSON.ToolResponse.ToolCallID,
			Name:       partJSON.ToolResponse.Name,
			Content:    partJSON.ToolResponse.Content,
		}, nil
	default:
		return nil, errors.Newf("unknown content type: '%s'", partJSON.Type)
	}
}

// MarshalJSON implements json.Marshaler for TextContent
func (tc TextContent) MarshalJSON() ([]byte, error) {
	return json.Marshal(TextContentJSON{
		Text: tc.Text,
		Type: "text",
	})
}

// UnmarshalJSON implements json.Unmarshaler for TextContent
func (tc *TextContent) UnmarshalJSON(data []byte) error {
	var textJSON TextContentJSON
	if err := json.Unmarshal(data, &textJSON); err != nil {
		return err
	}
	if textJSON.Type != "text" {
		return errors.Newf("invalid type for TextContent: %v", textJSON.Type)
	}
	tc.Text = textJSON.Text
	return nil
}

// MarshalJSON implements json.Marshaler for ImageURLContent
func (iuc ImageURLContent) MarshalJSON() ([]byte, error) {
	imageURLJSON := ImageURLJSON{URL: iuc.URL}
	if iuc.Detail != "" {
		imageURLJSON.Detail = iuc.Detail
	}

	return json.Marshal(ImageURLContentJSON{
		Type:     "image_url",
		ImageURL: imageURLJSON,
	})
}

// UnmarshalJSON implements json.Unmarshaler for ImageURLContent
func (iuc *ImageURLContent) UnmarshalJSON(data []byte) error {
	var imageJSON ImageURLContentJSON
	if err := json.Unmarshal(data, &imageJSON); err != nil {
		return err
	}
	if imageJSON.Type != "image_url" {
		return errors.Newf("invalid type for ImageURLContent: %v", imageJSON.Type)
	}
	if imageJSON.ImageURL.URL == "" {
		return errors.New("missing url field in ImageURLContent")
	}
	iuc.URL = imageJSON.ImageURL.URL
	iuc.Detail = imageJSON.ImageURL.Detail
	return nil
}

// MarshalJSON implements json.Marshaler for BinaryContent
func (bc BinaryContent) MarshalJSON() ([]byte, error) {
	return json.Marshal(BinaryContentJSON{
		Type: "binary",
		Binary: BinaryJSON{
			MIMEType: bc.MIMEType,
			Data:     base64.StdEncoding.EncodeToString(bc.Data),
		},
	})
}

// UnmarshalJSON implements json.Unmarshaler for BinaryContent
func (bc *BinaryContent) UnmarshalJSON(data []byte) error {
	var binaryJSON BinaryContentJSON
	if err := json.Unmarshal(data, &binaryJSON); err != nil {
		return err
	}
	if binaryJSON.Type != "binary" {
		return errors.Newf("invalid type for BinaryContent: %v", binaryJSON.Type)
	}
	if binaryJSON.Binary.Data == "" {
		return errors.New("missing data field in BinaryContent")
	}
	if binaryJSON.Binary.MIMEType == "" {
		return errors.New("missing mime_type field in BinaryContent")
	}
	decoded, err := base64.StdEncoding.DecodeString(binaryJSON.Binary.Data)
	if err != nil {
		return errors.Wrap(err, "error decoding base64 data")
	}
	bc.MIMEType = binaryJSON.Binary.MIMEType
	bc.Data = decoded
	return nil
}

// ToolCallJSONOrdered matches the expected field order for marshaling
// function, id, type
// This is only for marshaling
// (UnmarshalJSON still uses ToolCallJSON for flexibility)
type ToolCallJSONOrdered struct {
	FunctionCall *FunctionCall `json:"function"`
	ID           string        `json:"id"`
	Type         string        `json:"type"`
}

// MarshalJSON implements json.Marshaler for ToolCall
func (tc ToolCall) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type     string              `json:"type"`
		ToolCall ToolCallJSONOrdered `json:"tool_call"`
	}{
		Type: "tool_call",
		ToolCall: ToolCallJSONOrdered{
			FunctionCall: tc.FunctionCall,
			ID:           tc.ID,
			Type:         tc.Type,
		},
	})
}

// UnmarshalJSON implements json.Unmarshaler for ToolCall
func (tc *ToolCall) UnmarshalJSON(data []byte) error {
	var rawMsg map[string]any
	if err := json.Unmarshal(data, &rawMsg); err != nil {
		return err
	}

	// Check type
	if rawType, ok := rawMsg["type"].(string); !ok || rawType != "tool_call" {
		return errors.Newf("invalid type for ToolCall: %v", rawMsg["type"])
	}

	// Get tool_call object
	toolCallRaw, ok := rawMsg["tool_call"].(map[string]any)
	if !ok {
		return errors.New("invalid tool_call field in ToolCall")
	}

	// Get required fields
	id, ok := toolCallRaw["id"].(string)
	if !ok || id == "" {
		return errors.New("missing id field in ToolCall")
	}

	typ, ok := toolCallRaw["type"].(string)
	if !ok || typ == "" {
		return errors.New("missing type field in ToolCall")
	}

	// Handle function field - if it's missing or invalid, create empty struct
	var functionCall *FunctionCall
	if functionRaw, exists := toolCallRaw["function"]; exists {
		if functionMap, ok := functionRaw.(map[string]any); ok {
			// Valid function object
			name, _ := functionMap["name"].(string)
			arguments, _ := functionMap["arguments"].(string)
			functionCall = &FunctionCall{
				Name:      name,
				Arguments: arguments,
			}
		} else {
			// Invalid function (string, number, etc.) - create empty struct
			functionCall = &FunctionCall{}
		}
	} else {
		// Missing function - create empty struct
		functionCall = &FunctionCall{}
	}

	tc.ID = id
	tc.Type = typ
	tc.FunctionCall = functionCall
	return nil
}

// ToolResponseJSONOrdered matches the expected field order for marshaling
// tool_call_id, name, content
// This is only for marshaling
// (UnmarshalJSON still uses ToolResponseJSON for flexibility)
type ToolResponseJSONOrdered struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Content    string `json:"content"`
}

// MarshalJSON implements json.Marshaler for ToolCallResponse
func (tc ToolCallResponse) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type         string                  `json:"type"`
		ToolResponse ToolResponseJSONOrdered `json:"tool_response"`
	}{
		Type:         "tool_response",
		ToolResponse: *tc.ToToolResponseJSONOrdered(),
	})
}

// UnmarshalJSON implements json.Unmarshaler for ToolCallResponse
func (tc *ToolCallResponse) UnmarshalJSON(data []byte) error {
	var toolResponseJSON ToolResponseContentJSON
	if err := json.Unmarshal(data, &toolResponseJSON); err != nil {
		return err
	}
	if toolResponseJSON.Type != "tool_response" {
		return errors.Newf("invalid type for ToolCallResponse: %v", toolResponseJSON.Type)
	}
	if toolResponseJSON.ToolResponse.ToolCallID == "" {
		return errors.New("missing tool_call_id field in ToolCallResponse")
	}
	if toolResponseJSON.ToolResponse.Name == "" {
		return errors.New("missing name field in ToolCallResponse")
	}
	if toolResponseJSON.ToolResponse.Content == "" {
		return errors.New("missing content field in ToolCallResponse")
	}
	tc.ToolCallID = toolResponseJSON.ToolResponse.ToolCallID
	tc.Name = toolResponseJSON.ToolResponse.Name
	tc.Content = toolResponseJSON.ToolResponse.Content
	return nil
}

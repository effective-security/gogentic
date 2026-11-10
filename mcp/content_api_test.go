package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	contentURI  = "file:///a.txt"
	contentText = "some text"
	contentBlob = "YmxvYg=="
)

func TestContentConstructors(t *testing.T) {
	text := NewTextContent(contentText)
	assert.Equal(t, &Content{
		Type: ContentTypeText,
		TextContent: &TextContent{
			Text: contentText,
		},
	}, text)

	img := NewImageContent(contentBlob, mimeImagePNG)
	assert.Equal(t, &Content{
		Type: ContentTypeImage,
		ImageContent: &ImageContent{
			Data:     contentBlob,
			MimeType: mimeImagePNG,
		},
	}, img)

	textRes := NewTextResourceContent(contentURI, contentText, mimeTextPlain)
	assert.Equal(t, ContentTypeEmbeddedResource, textRes.Type)
	require.NotNil(t, textRes.EmbeddedResource)
	assert.Equal(t, embeddedResourceTypeText, textRes.EmbeddedResource.EmbeddedResourceType)
	assert.Nil(t, textRes.EmbeddedResource.BlobResourceContents)
	require.NotNil(t, textRes.EmbeddedResource.TextResourceContents)
	assert.Equal(t, contentURI, textRes.EmbeddedResource.TextResourceContents.Uri)
	assert.Equal(t, contentText, textRes.EmbeddedResource.TextResourceContents.Text)
	require.NotNil(t, textRes.EmbeddedResource.TextResourceContents.MimeType)
	assert.Equal(t, mimeTextPlain, *textRes.EmbeddedResource.TextResourceContents.MimeType)

	blobRes := NewBlobResourceContent(contentURI, contentBlob, mimeImagePNG)
	assert.Equal(t, ContentTypeEmbeddedResource, blobRes.Type)
	require.NotNil(t, blobRes.EmbeddedResource)
	assert.Equal(t, embeddedResourceTypeBlob, blobRes.EmbeddedResource.EmbeddedResourceType)
	assert.Nil(t, blobRes.EmbeddedResource.TextResourceContents)
	require.NotNil(t, blobRes.EmbeddedResource.BlobResourceContents)
	assert.Equal(t, contentURI, blobRes.EmbeddedResource.BlobResourceContents.Uri)
	assert.Equal(t, contentBlob, blobRes.EmbeddedResource.BlobResourceContents.Blob)
	require.NotNil(t, blobRes.EmbeddedResource.BlobResourceContents.MimeType)
	assert.Equal(t, mimeImagePNG, *blobRes.EmbeddedResource.BlobResourceContents.MimeType)

	priority := 0.5
	annotated := NewTextContent(contentText).WithAnnotations(Annotations{
		Audience: []Role{RoleUser, RoleAssistant},
		Priority: &priority,
	})
	require.NotNil(t, annotated.Annotations)
	assert.Equal(t, []Role{RoleUser, RoleAssistant}, annotated.Annotations.Audience)
	require.NotNil(t, annotated.Annotations.Priority)
	assert.InDelta(t, priority, *annotated.Annotations.Priority, 0)
}

func TestContent_MarshalJSON(t *testing.T) {
	tcs := []struct {
		name    string
		content *Content
		want    string
		wantErr string
	}{
		{
			name:    "text",
			content: NewTextContent(contentText),
			want:    `{"type":"text","text":"some text"}`,
		},
		{
			name:    "image",
			content: NewImageContent(contentBlob, mimeImagePNG),
			want:    `{"type":"image","data":"YmxvYg==","mimeType":"image/png"}`,
		},
		{
			// NOTE: the resource fields are emitted inline next to "type";
			// the MCP schema nests them under a "resource" key.
			name:    "text resource",
			content: NewTextResourceContent(contentURI, contentText, mimeTextPlain),
			want:    `{"type":"resource","uri":"file:///a.txt","text":"some text","mimeType":"text/plain"}`,
		},
		{
			name:    "blob resource",
			content: NewBlobResourceContent(contentURI, contentBlob, mimeImagePNG),
			want:    `{"type":"resource","uri":"file:///a.txt","blob":"YmxvYg==","mimeType":"image/png"}`,
		},
		{
			name: "unknown content type",
			content: &Content{
				Type: "audio",
			},
			wantErr: "json: error calling MarshalJSON for type *mcp.Content: unknown content type: audio",
		},
		{
			name: "invalid embedded resource",
			content: &Content{
				Type:             ContentTypeEmbeddedResource,
				EmbeddedResource: &EmbeddedResource{},
			},
			wantErr: "json: error calling MarshalJSON for type *mcp.Content: json: error calling MarshalJSON for type *mcp.EmbeddedResource: unknown embedded resource type: ",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.content)
			if tc.wantErr != "" {
				assert.EqualError(t, err, tc.wantErr)
				assert.Nil(t, b)
				return
			}
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(b))
		})
	}
}

func TestContent_UnmarshalJSON(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		var c Content
		require.NoError(t, json.Unmarshal([]byte(`{"type":"text","text":"hello"}`), &c))
		assert.Equal(t, Content{
			Type: ContentTypeText,
			TextContent: &TextContent{
				Text: "hello",
			},
		}, c)
	})

	t.Run("round trip", func(t *testing.T) {
		b, err := json.Marshal(NewTextContent(contentText))
		require.NoError(t, err)
		var c Content
		require.NoError(t, json.Unmarshal(b, &c))
		assert.Equal(t, *NewTextContent(contentText), c)
	})

	tcs := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "invalid json",
			input:   `[]`,
			wantErr: "json: cannot unmarshal array into Go value of type mcp.typeWrapper",
		},
		{
			name:    "unknown type",
			input:   `{"type":"audio"}`,
			wantErr: "unknown content type: audio",
		},
		{
			// Decoding is only implemented for text content.
			name:    "image is not decodable",
			input:   `{"type":"image","data":"YmxvYg==","mimeType":"image/png"}`,
			wantErr: "unknown content type: image",
		},
		{
			name:    "resource is not decodable",
			input:   `{"type":"resource","resource":{"uri":"x","text":"y"}}`,
			wantErr: "unknown content type: resource",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var c Content
			assert.EqualError(t, json.Unmarshal([]byte(tc.input), &c), tc.wantErr)
		})
	}
}

func TestEmbeddedResource_JSON(t *testing.T) {
	t.Run("marshal text", func(t *testing.T) {
		b, err := json.Marshal(NewTextEmbeddedResource(contentURI, contentText, mimeTextPlain))
		require.NoError(t, err)
		assert.JSONEq(t, `{"uri":"file:///a.txt","text":"some text","mimeType":"text/plain"}`, string(b))
	})

	t.Run("marshal blob", func(t *testing.T) {
		b, err := json.Marshal(NewBlobEmbeddedResource(contentURI, contentBlob, mimeImagePNG))
		require.NoError(t, err)
		assert.JSONEq(t, `{"uri":"file:///a.txt","blob":"YmxvYg==","mimeType":"image/png"}`, string(b))
	})

	t.Run("marshal unknown type", func(t *testing.T) {
		_, err := EmbeddedResource{EmbeddedResourceType: "video"}.MarshalJSON()
		assert.EqualError(t, err, "unknown embedded resource type: video")
	})

	t.Run("unmarshal text", func(t *testing.T) {
		var r EmbeddedResource
		require.NoError(t, json.Unmarshal([]byte(`{"uri":"u","text":"t"}`), &r))
		assert.Equal(t, embeddedResourceTypeText, r.EmbeddedResourceType)
		assert.Nil(t, r.BlobResourceContents)
		assert.Equal(t, &TextResourceContents{
			Uri:  "u",
			Text: "t",
		}, r.TextResourceContents)
	})

	t.Run("unmarshal blob", func(t *testing.T) {
		var r EmbeddedResource
		require.NoError(t, json.Unmarshal([]byte(`{"uri":"u","blob":"b","mimeType":"image/png"}`), &r))
		assert.Equal(t, embeddedResourceTypeBlob, r.EmbeddedResourceType)
		assert.Nil(t, r.TextResourceContents)
		mime := mimeImagePNG
		assert.Equal(t, &BlobResourceContents{
			Uri:      "u",
			Blob:     "b",
			MimeType: &mime,
		}, r.BlobResourceContents)
	})

	t.Run("unmarshal invalid", func(t *testing.T) {
		var r EmbeddedResource
		err := json.Unmarshal([]byte(`[1]`), &r)
		assert.EqualError(t, err, "failed to unmarshal embedded resource: json: cannot unmarshal array into Go value of type mcp.BlobResourceContents")
		assert.Equal(t, EmbeddedResource{}, r)
	})
}

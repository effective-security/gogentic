// package vertex implements a langchaingo provider for Google Vertex AI LLMs,
// including the new Gemini models.
// See https://cloud.google.com/vertex-ai for more details.
package vertex

import (
	"context"

	"cloud.google.com/go/vertexai/genai"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/googleai"
	"github.com/effective-security/gogentic/pkg/llms/googleai/internal/palmclient"
)

// Vertex is a type that represents a Vertex AI API client.
//
// Right now, the Vertex Gemini SDK doesn't support embeddings; therefore,
// for embeddings we also hold a palmclient.
type Vertex struct {
	client     *genai.Client
	opts       googleai.Options
	palmClient *palmclient.PaLMClient
}

var (
	_ llms.Model    = (*Vertex)(nil)
	_ llms.Embedder = (*Vertex)(nil)
)

// GetName implements the Model interface.
func (v *Vertex) GetName() string {
	return v.opts.DefaultModel
}

// GetProviderType implements the Model interface.
func (v *Vertex) GetProviderType() llms.ProviderType {
	return llms.ProviderGoogleAI
}

// New creates a new Vertex client.
func New(ctx context.Context, opts ...googleai.Option) (*Vertex, error) {
	clientOptions := googleai.DefaultOptions()
	for _, opt := range opts {
		opt(&clientOptions)
	}

	client, err := genai.NewClient(
		ctx,
		clientOptions.CloudProject,
		clientOptions.CloudLocation,
		clientOptions.ClientOptions...)
	if err != nil {
		return nil, err
	}

	palmClient, err := palmclient.New(
		ctx,
		clientOptions.CloudProject,
		clientOptions.CloudLocation,
		clientOptions.ClientOptions...)
	if err != nil {
		return nil, err
	}

	v := &Vertex{
		opts:       clientOptions,
		client:     client,
		palmClient: palmClient,
	}
	return v, nil
}

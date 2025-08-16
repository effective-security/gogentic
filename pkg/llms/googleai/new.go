// package googleai implements a langchaingo provider for Google AI LLMs.
// See https://ai.google.dev/ for more details.
package googleai

import (
	"context"

	"github.com/effective-security/gogentic/pkg/llms"
	"google.golang.org/genai"
)

// GoogleAI is a type that represents a Google AI API client.
type GoogleAI struct {
	client *genai.Client
	// generativeModel *genai.Model
	// embeddingModel  *genai.Model
	opts Options
}

var (
	_ llms.Model = (*GoogleAI)(nil)
	//_ llms.Embedder = (*GoogleAI)(nil)
)

// New creates a new GoogleAI client.
func New(ctx context.Context, opts ...Option) (*GoogleAI, error) {
	clientOptions := DefaultOptions()
	for _, opt := range opts {
		opt(&clientOptions)
	}
	clientOptions.EnsureAuthPresent()

	gi := &GoogleAI{
		opts: clientOptions,
	}

	cfg := &genai.ClientConfig{
		Project:     clientOptions.CloudProject,
		Location:    clientOptions.CloudLocation,
		APIKey:      clientOptions.APIKey,
		Credentials: clientOptions.Credentials,
		HTTPClient:  clientOptions.HTTPClient,
		Backend:     genai.BackendGeminiAPI,
	}

	client, err := genai.NewClient(ctx, cfg)
	if err != nil {
		return gi, err
	}
	gi.client = client
	// gi.generativeModel = &genai.Model{
	// 	Name: clientOptions.DefaultModel,
	// }
	// gi.embeddingModel = &genai.Model{
	// 	Name: clientOptions.DefaultEmbeddingModel,
	// }

	return gi, nil
}

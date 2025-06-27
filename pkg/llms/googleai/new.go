// package googleai implements a langchaingo provider for Google AI LLMs.
// See https://ai.google.dev/ for more details.
package googleai

import (
	"context"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/google/generative-ai-go/genai"
)

// GoogleAI is a type that represents a Google AI API client.
type GoogleAI struct {
	client *genai.Client
	opts   Options
}

var _ llms.Model = &GoogleAI{}

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

	client, err := genai.NewClient(ctx, clientOptions.ClientOptions...)
	if err != nil {
		return gi, err
	}

	gi.client = client
	return gi, nil
}

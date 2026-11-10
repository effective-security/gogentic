package googleai

import (
	"net/http"
	"testing"

	"cloud.google.com/go/auth"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"google.golang.org/genai"
)

const (
	// envGeminiAPIKey is the other variable the SDK reads a key from.
	envGeminiAPIKey = "GEMINI_API_KEY" //nolint:gosec
	// envKey is the key the environment holds in a test.
	envKey = "env-key"
	// explicitKey is the key a test passes with WithAPIKey.
	explicitKey = "explicit-key"
)

// Every option sets its field, and only that one.
func TestOptions(t *testing.T) {
	creds := auth.NewCredentials(&auth.CredentialsOptions{})
	httpClient := &http.Client{}
	effort := llms.EffortPolicy{
		Default: llms.ReasoningEffortLow,
		Max:     llms.ReasoningEffortHigh,
	}

	got := DefaultOptions()
	for _, opt := range []Option{
		WithAPIKey("key"),
		WithCredentials(nil),
		WithCredentials(creds),
		WithHTTPClient(httpClient),
		WithCloudProject("project"),
		WithCloudLocation("location"),
		WithDefaultModel("gemini-model"),
		WithDefaultEmbeddingModel("embedding-model"),
		WithDefaultCandidateCount(3),
		WithDefaultMaxTokens(256),
		WithDefaultTemperature(0.75),
		WithDefaultTopK(7),
		WithDefaultTopP(0.8),
		WithHarmThreshold(genai.HarmBlockThresholdBlockLowAndAbove),
		WithEffortPolicy(effort),
	} {
		opt(&got)
	}

	want := Options{
		CloudProject:          "project",
		CloudLocation:         "location",
		DefaultModel:          "gemini-model",
		DefaultEmbeddingModel: "embedding-model",
		DefaultCandidateCount: 3,
		DefaultMaxTokens:      256,
		DefaultTemperature:    0.75,
		DefaultTopK:           7,
		DefaultTopP:           0.8,
		HarmThreshold:         genai.HarmBlockThresholdBlockLowAndAbove,
		APIKey:                "key",
		Credentials:           creds,
		HTTPClient:            httpClient,
		Effort:                effort,
	}
	assert.Equal(t, want, got)
}

// Nil credentials leave the credentials already set.
func TestWithCredentials_NilKeepsCredentials(t *testing.T) {
	creds := auth.NewCredentials(&auth.CredentialsOptions{})
	opts := DefaultOptions()
	WithCredentials(creds)(&opts)
	WithCredentials(nil)(&opts)
	assert.Same(t, creds, opts.Credentials)
}

func TestEnsureAuthPresent(t *testing.T) {
	t.Run("key from the environment", func(t *testing.T) {
		t.Setenv(envGoogleAPIKey, envKey)
		opts := DefaultOptions()
		opts.EnsureAuthPresent()
		assert.Equal(t, envKey, opts.APIKey)
	})

	t.Run("explicit key wins over the environment", func(t *testing.T) {
		// F-009: the environment must not replace a key given by option.
		t.Setenv(envGoogleAPIKey, envKey)
		opts := DefaultOptions()
		WithAPIKey(explicitKey)(&opts)
		opts.EnsureAuthPresent()
		assert.Equal(t, explicitKey, opts.APIKey)
	})

	t.Run("credentials keep the environment out", func(t *testing.T) {
		t.Setenv(envGoogleAPIKey, envKey)
		opts := DefaultOptions()
		WithCredentials(auth.NewCredentials(&auth.CredentialsOptions{}))(&opts)
		opts.EnsureAuthPresent()
		assert.Empty(t, opts.APIKey)
	})

	t.Run("nothing in the environment", func(t *testing.T) {
		t.Setenv(envGoogleAPIKey, "")
		opts := DefaultOptions()
		opts.EnsureAuthPresent()
		assert.Empty(t, opts.APIKey)
	})
}

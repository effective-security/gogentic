package googleai

import (
	"testing"

	"cloud.google.com/go/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The options the Gemini API backend rejects fail the construction.
func TestNew_ClientErrors(t *testing.T) {
	tests := []struct {
		name    string
		opts    []Option
		wantErr string
	}{
		{
			name: "project and location",
			opts: []Option{
				WithAPIKey("key"),
				WithCloudProject("project"),
				WithCloudLocation("location"),
			},
			wantErr: "project and location are not supported for Gemini API backend",
		},
		{
			name: "credentials and API key",
			opts: []Option{
				WithAPIKey("key"),
				WithCredentials(auth.NewCredentials(&auth.CredentialsOptions{})),
			},
			wantErr: "credentials and API key are mutually exclusive",
		},
		{
			name:    "no API key",
			wantErr: "api key is required for Google AI backend",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envGoogleAPIKey, "")
			t.Setenv(envGeminiAPIKey, "")

			_, err := New(t.Context(), tt.opts...)
			require.ErrorContains(t, err, tt.wantErr)

			lister, err := NewModelLister(t.Context(), tt.opts...)
			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, lister)
		})
	}
}

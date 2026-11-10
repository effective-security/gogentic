package llmfactory

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	// yamlSample matches a fenced YAML block in a Markdown file.
	yamlSample = regexp.MustCompile("(?s)```yaml\n(.*?)```")
	// envReference matches the name of an environment variable in a sample.
	envReference = regexp.MustCompile(`env://([A-Z0-9_]+)`)
)

// The configuration samples in the documentation must load and validate.
func TestDocumentationSamples(t *testing.T) {
	const providersKey = "providers:"
	files := []string{
		"../../Documentation/llm-factory.md",
		"../../Documentation/RELEASE_NOTES_1.0.md",
		"../../README.md",
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		require.NoError(t, err)

		var checked int
		for _, match := range yamlSample.FindAllStringSubmatch(string(raw), -1) {
			sample := match[1]
			if !strings.Contains(sample, providersKey) {
				continue
			}
			checked++
			for _, env := range envReference.FindAllStringSubmatch(sample, -1) {
				t.Setenv(env[1], "fixture")
			}
			path := filepath.Join(t.TempDir(), "sample.yaml")
			require.NoError(t, os.WriteFile(path, []byte(sample), 0o600))

			cfg, err := LoadConfig(path)
			require.NoError(t, err, "sample in %s does not load:\n%s", file, sample)
			assert.NotEmpty(t, cfg.Providers)
		}
		assert.Positive(t, checked, "no configuration sample found in %s", file)
	}
}

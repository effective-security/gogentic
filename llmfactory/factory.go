package llmfactory

import (
	"strings"
	"sync"

	"github.com/effective-security/xlog"
	"github.com/pkg/errors"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

var logger = xlog.NewPackageLogger("github.com/effective-security/gogentic", "llmfactory")

type Factory interface {
	DefaultModel() (llms.Model, error)
	ModelByType(typ string) (llms.Model, error)
	ModelByName(name string) (llms.Model, error)
}

// Load returns OpenAI factory
func Load(location string) (Factory, error) {
	cfg, err := LoadConfig(location)
	if err != nil {
		return nil, err
	}
	return New(cfg), nil
}

type factory struct {
	cfg *Config

	byType map[string]llms.Model
	byName map[string]llms.Model
	lock   sync.Mutex
}

// New creates a new LLM factory
func New(cfg *Config) Factory {
	f := &factory{
		cfg:    cfg,
		byType: make(map[string]llms.Model),
		byName: make(map[string]llms.Model),
	}
	return f
}

func NewLLM(cfg *ProviderConfig) (llms.Model, error) {
	var opts []openai.Option
	if cfg.Token != "" {
		opts = append(opts, openai.WithToken(cfg.Token))
	}

	model := cfg.DefaultModel
	switch typ := strings.ToUpper(cfg.OpenAI.APIType); typ {
	case "AZURE", "AZURE_AD":
		if typ == "AZURE" {
			opts = append(opts, openai.WithAPIType(openai.APITypeAzure))
		} else {
			opts = append(opts, openai.WithAPIType(openai.APITypeAzureAD))
		}
		opts = append(opts, openai.WithAPIVersion(cfg.OpenAI.APIVersion))
	case "OPENAI", "OPEN_AI":
		opts = append(opts, openai.WithAPIType(openai.APITypeOpenAI))
	}

	opts = append(opts, openai.WithModel(model))

	if cfg.OpenAI.BaseURL != "" {
		opts = append(opts, openai.WithBaseURL(cfg.OpenAI.BaseURL))
	}
	return openai.New(opts...)
}

// Default returns the default OpenAI client
func (f *factory) DefaultModel() (llms.Model, error) {
	if len(f.cfg.Providers) == 0 {
		return nil, errors.New("no providers configured")
	}
	return f.ModelByName(f.cfg.Providers[0].Name)
}

func (f *factory) ModelByType(typ string) (llms.Model, error) {
	f.lock.Lock()
	defer f.lock.Unlock()

	if client, ok := f.byType[typ]; ok {
		return client, nil
	}

	for _, cfg := range f.cfg.Providers {
		if cfg.OpenAI.APIType == typ {
			model, err := NewLLM(cfg)
			if err != nil {
				return nil, err
			}

			logger.KV(xlog.DEBUG,
				"status", "created_llm",
				"type", cfg.OpenAI.APIType,
				"version", cfg.OpenAI.APIVersion,
				"name", cfg.Name)

			f.byType[typ] = model
			return model, nil
		}
	}
	return nil, errors.Errorf("provider not found for type: %s", typ)
}

func (f *factory) ModelByName(name string) (llms.Model, error) {
	f.lock.Lock()
	defer f.lock.Unlock()

	if client, ok := f.byName[name]; ok {
		return client, nil
	}

	for _, cfg := range f.cfg.Providers {
		if cfg.Name == name {
			model, err := NewLLM(cfg)
			if err != nil {
				return nil, err
			}

			logger.KV(xlog.DEBUG,
				"status", "created_llm",
				"type", cfg.OpenAI.APIType,
				"version", cfg.OpenAI.APIVersion,
				"name", cfg.Name)

			f.byName[name] = model
			return model, nil
		}
	}
	return nil, errors.Errorf("provider not found for name: %s", name)
}

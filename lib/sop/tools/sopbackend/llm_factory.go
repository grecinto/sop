package sopbackend

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/generator"
)

type llmSettings struct {
	Provider string
	APIKey   string
	URL      string
	Model    string
}

func resolveLLMSettings(r *http.Request) llmSettings {
	settings := llmSettings{
		Provider: "openai",
		Model:    "gpt-5.4",
	}

	if r != nil {
		if h := r.Header.Get("X-LLM-Provider"); h != "" {
			settings.Provider = h
		}
		if h := r.Header.Get("X-LLM-API-Key"); h != "" {
			if decrypted, err := DecryptSecretValue(h); err == nil {
				settings.APIKey = decrypted
			} else {
				settings.APIKey = h
			}
		}
		if h := r.Header.Get("X-LLM-URL"); h != "" {
			settings.URL = h
		}
		if h := r.Header.Get("X-LLM-Model"); h != "" {
			settings.Model = h
		}
	}

	return settings
}

func resolveFallbackSettings(r *http.Request) *llmSettings {
	if r == nil {
		return nil
	}

	provider := r.Header.Get("X-LLM-Fallback-Provider")
	if provider == "" {
		return nil
	}

	settings := &llmSettings{
		Provider: provider,
	}

	if h := r.Header.Get("X-LLM-Fallback-API-Key"); h != "" {
		if decrypted, err := DecryptSecretValue(h); err == nil {
			settings.APIKey = decrypted
		} else {
			settings.APIKey = h
		}
	}
	if h := r.Header.Get("X-LLM-Fallback-URL"); h != "" {
		settings.URL = h
	}
	if h := r.Header.Get("X-LLM-Fallback-Model"); h != "" {
		settings.Model = h
	}

	return settings
}

func newConfiguredLLM(settings llmSettings) (ai.Generator, error) {
	provider := settings.Provider
	options := make(map[string]any)

	switch provider {
	case "gemini":
		if settings.APIKey != "" {
			options["api_key"] = settings.APIKey
		}
		options["model"] = settings.Model
		if settings.URL != "" {
			options["api_url"] = settings.URL
		}
	case "ollama":
		if settings.URL != "" {
			options["base_url"] = settings.URL
		}
		options["model"] = settings.Model
	case "chatgpt", "openai":
		provider = "chatgpt"
		if settings.APIKey != "" {
			options["api_key"] = settings.APIKey
		}
		options["model"] = settings.Model
		if settings.URL != "" {
			options["api_url"] = settings.URL
		}
	case "anthropic":
		if settings.APIKey != "" {
			options["api_key"] = settings.APIKey
		}
		options["model"] = settings.Model
	default:
		return nil, fmt.Errorf("unsupported provider %q", provider)
	}

	return generator.New(provider, options)
}

// GetConfiguredLLM returns an AI Generator based on the server configuration.
// It checks the HTTP request headers first, then falls back to server config defaults.
// If a fallback provider is configured via X-LLM-Fallback-* headers, it wraps the
// primary and fallback generators in a FallbackGenerator for automatic failover.
func GetConfiguredLLM(r *http.Request) ai.Generator {
	if !config.ProductionMode {
		mockGen, _ := generator.New("perceptron", nil)
		return mockGen
	}

	primarySettings := resolveLLMSettings(r)
	primary, err := newConfiguredLLM(primarySettings)
	if err != nil {
		mockGen, _ := generator.New("perceptron", nil)
		return mockGen
	}

	// Check for fallback configuration
	fallbackSettings := resolveFallbackSettings(r)
	if fallbackSettings == nil {
		return primary
	}

	fallback, err := newConfiguredLLM(*fallbackSettings)
	if err != nil {
		// If fallback fails to initialize, just return primary
		return primary
	}

	// Wrap in FallbackGenerator for automatic failover
	return generator.NewFallbackGenerator(primary, []ai.Generator{fallback})
}

// summarizerLLMClient is an adapter that makes ai.Generator implement LLMClient
type summarizerLLMClient struct {
	gen ai.Generator
}

func (c *summarizerLLMClient) Generate(prompt string) (string, error) {
	out, err := c.gen.Generate(context.Background(), prompt, ai.GenOptions{})
	if err != nil {
		return "", err
	}
	return out.Text, nil
}

// GetConfiguredLLMClient returns an LLMClient for the Summarizer
func GetConfiguredLLMClient(r *http.Request) LLMClient {
	gen := GetConfiguredLLM(r)
	if gen == nil {
		return nil
	}
	return &summarizerLLMClient{gen: gen}
}

package sopbackend

import (
	"net/http"
	"testing"

	"github.com/sharedcode/sop/ai/generator"
)

func TestResolveLLMSettingsDefaultsToPreferredProviders(t *testing.T) {
	config = Config{}

	settings := resolveLLMSettings(nil)

	if settings.Provider != "openai" {
		t.Fatalf("expected default provider %q, got %q", "openai", settings.Provider)
	}

	if settings.Model != "gpt-5.4" {
		t.Fatalf("expected default model %q, got %q", "gpt-5.4", settings.Model)
	}
}

func TestResolveEmbedderSettingsDefaultsToKelindar(t *testing.T) {
	config = Config{}

	settings := resolveEmbedderSettings(nil)

	if settings.Provider != "local" {
		t.Fatalf("expected default embedder provider %q, got %q", "local", settings.Provider)
	}

	if settings.Model != "kelindar" {
		t.Fatalf("expected default embedder model %q, got %q", "kelindar", settings.Model)
	}
}

func TestResolveFallbackSettings_NoHeaders(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	settings := resolveFallbackSettings(req)
	if settings != nil {
		t.Errorf("Expected nil settings when no fallback headers present, got %+v", settings)
	}
}

func TestResolveFallbackSettings_WithProvider(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-LLM-Fallback-Provider", "chatgpt")
	req.Header.Set("X-LLM-Fallback-Model", "gpt-4o")
	req.Header.Set("X-LLM-Fallback-API-Key", "sk-test-key")

	settings := resolveFallbackSettings(req)
	if settings == nil {
		t.Fatal("Expected non-nil settings")
	}
	if settings.Provider != "chatgpt" {
		t.Errorf("Expected provider 'chatgpt', got '%s'", settings.Provider)
	}
	if settings.Model != "gpt-4o" {
		t.Errorf("Expected model 'gpt-4o', got '%s'", settings.Model)
	}
	if settings.APIKey != "sk-test-key" {
		t.Errorf("Expected API key 'sk-test-key', got '%s'", settings.APIKey)
	}
}

func TestGetConfiguredLLM_ReturnsFallbackGenerator(t *testing.T) {
	if !config.ProductionMode {
		t.Skip("Skipping test in non-production mode")
	}

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-LLM-Provider", "anthropic")
	req.Header.Set("X-LLM-Model", "claude-3-5-sonnet-20241022")
	req.Header.Set("X-LLM-API-Key", "sk-ant-test")
	req.Header.Set("X-LLM-Fallback-Provider", "chatgpt")
	req.Header.Set("X-LLM-Fallback-Model", "gpt-4o")
	req.Header.Set("X-LLM-Fallback-API-Key", "sk-test")

	gen := GetConfiguredLLM(req)
	if gen == nil {
		t.Fatal("Expected non-nil generator")
	}

	// Check if it's a FallbackGenerator by type assertion
	if _, ok := gen.(*generator.FallbackGenerator); !ok {
		t.Errorf("Expected *generator.FallbackGenerator, got %T", gen)
	}
}

func TestGetConfiguredLLM_NoFallbackReturnsPrimary(t *testing.T) {
	if !config.ProductionMode {
		t.Skip("Skipping test in non-production mode")
	}

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-LLM-Provider", "anthropic")
	req.Header.Set("X-LLM-Model", "claude-3-5-sonnet-20241022")
	req.Header.Set("X-LLM-API-Key", "sk-ant-test")

	gen := GetConfiguredLLM(req)
	if gen == nil {
		t.Fatal("Expected non-nil generator")
	}

	// Should NOT be a FallbackGenerator when no fallback configured
	if _, ok := gen.(*generator.FallbackGenerator); ok {
		t.Errorf("Expected non-FallbackGenerator when no fallback configured, got %T", gen)
	}
}

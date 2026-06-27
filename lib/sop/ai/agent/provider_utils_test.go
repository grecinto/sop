package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sharedcode/sop/ai"
)

type stubGenerator struct {
	name string
}

func (s stubGenerator) Name() string { return s.name }

func (s stubGenerator) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	return ai.GenOutput{}, nil
}

func (s stubGenerator) PrewarmCache(ctx context.Context, opts ai.GenOptions) error { return nil }

func (s stubGenerator) EstimateCost(inTokens, outTokens int) float64 { return 0 }

func TestNormalizeThinkingLevelForGenerator_UpgradesAnthropicLowToMedium(t *testing.T) {
	got := NormalizeThinkingLevelForGenerator(stubGenerator{name: "anthropic"}, "anthropic", "claude-sonnet-4-5", "low")
	if got != "medium" {
		t.Fatalf("expected anthropic low to map to medium, got %q", got)
	}
}

func TestNormalizeThinkingLevelForGenerator_PreservesNonAnthropicLow(t *testing.T) {
	got := NormalizeThinkingLevelForGenerator(stubGenerator{name: "openai"}, "openai", "gpt-5.4", "low")
	if got != "low" {
		t.Fatalf("expected non-anthropic low to stay low, got %q", got)
	}
}

func TestNormalizeThinkingLevelForGenerator_UsesModelAwareHighForAnthropicSonnet(t *testing.T) {
	got := NormalizeThinkingLevelForGenerator(stubGenerator{name: "anthropic"}, "anthropic", "claude-sonnet-4-5", "")
	if got != "high" {
		t.Fatalf("expected sonnet 4.5 to use high reasoning, got %q", got)
	}
}

func TestNormalizeThinkingLevelForGenerator_UsesModelAwareLowForGeminiFlashLite(t *testing.T) {
	got := NormalizeThinkingLevelForGenerator(stubGenerator{name: "gemini"}, "gemini", "gemini-3.1-flash-lite", "")
	if got != "low" {
		t.Fatalf("expected flash-lite to use low reasoning, got %q", got)
	}
}

func TestNormalizeThinkingLevelForGenerator_UsesCatalogMetadataWhenPresent(t *testing.T) {
	tempDir := t.TempDir()
	catalogPath := filepath.Join(tempDir, "model_catalog.json")
	if err := os.WriteFile(catalogPath, []byte(`{"llm":[{"label":"OpenAI","options":[{"value":"openai:gpt-5.4","label":"GPT-5.4","default_thinking_level":"high"}]}]}`), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(originalWD)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	got := NormalizeThinkingLevelForGenerator(stubGenerator{name: "openai"}, "openai", "gpt-5.4", "")
	if got != "high" {
		t.Fatalf("expected catalog default thinking level to override heuristics, got %q", got)
	}
}

func TestNormalizeThinkingLevelForGenerator_UsesCatalogRange(t *testing.T) {
	tempDir := t.TempDir()
	catalogPath := filepath.Join(tempDir, "model_catalog.json")
	if err := os.WriteFile(catalogPath, []byte(`{"llm":[{"label":"Anthropic","options":[{"value":"anthropic:claude-sonnet-4-5","label":"Claude Sonnet 4.5","thinking_level_range":["medium","high"]}]}]}`), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(originalWD)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	got := NormalizeThinkingLevelForGenerator(stubGenerator{name: "anthropic"}, "anthropic", "claude-sonnet-4-5", "low")
	if got != "medium" {
		t.Fatalf("expected catalog range to clamp low to medium for anthropic, got %q", got)
	}
}

func TestModelCatalogProvidesDefaultThinkingLevelsForCoreModels(t *testing.T) {
	var catalogPath string
	for _, candidate := range modelCatalogCandidatePaths() {
		if _, err := os.Stat(candidate); err == nil {
			catalogPath = candidate
			break
		}
	}
	if catalogPath == "" {
		t.Fatal("model catalog not found")
	}

	data, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}

	var catalog modelCatalogFile
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}

	checks := map[string]string{
		"openai:gpt-5.4":              "medium",
		"anthropic:claude-sonnet-4-5": "high",
		"gemini:gemini-2.5-pro":       "high",
	}

	for _, group := range catalog.LLM {
		for _, option := range group.Options {
			if expected, ok := checks[option.Value]; ok {
				if option.DefaultThinkingLevel != expected {
					t.Fatalf("expected %s default thinking level %q, got %q", option.Value, expected, option.DefaultThinkingLevel)
				}
				delete(checks, option.Value)
			}
		}
	}

	if len(checks) > 0 {
		t.Fatalf("missing catalog defaults for models: %v", checks)
	}
}

func TestResolveThinkingLevelForContext_UsesExplicitOverrideOverCatalogDefault(t *testing.T) {
	tempDir := t.TempDir()
	catalogPath := filepath.Join(tempDir, "model_catalog.json")
	if err := os.WriteFile(catalogPath, []byte(`{"llm":[{"label":"Anthropic","options":[{"value":"anthropic:claude-sonnet-4-5","label":"Claude Sonnet 4.5","default_thinking_level":"low"}]}]}`), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(originalWD)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	ctx := context.WithValue(context.Background(), ctxKeyThinkingLevel, "high")
	got := ResolveThinkingLevelForContext(ctx, stubGenerator{name: "anthropic"}, "anthropic", "claude-sonnet-4-5", "")
	if got != "high" {
		t.Fatalf("expected explicit thinking-level override to win, got %q", got)
	}
}

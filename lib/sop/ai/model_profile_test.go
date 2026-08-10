package ai

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveModelProfile_UsesConfiguredProfile(t *testing.T) {
	profile := ResolveModelProfile("gemini", "gemini-2.5-flash", map[string]any{
		"model_profile": map[string]any{
			"tier":              "cheap",
			"max_output_tokens": 512,
			"temperature":       0.1,
			"compact_prompt":    true,
			"max_tool_turns":    3,
		},
	})

	if profile.Tier != "cheap" {
		t.Fatalf("expected cheap tier, got %q", profile.Tier)
	}
	if profile.MaxOutputTokens != 512 {
		t.Fatalf("expected max output tokens 512, got %d", profile.MaxOutputTokens)
	}
	if profile.Temperature != 0.1 {
		t.Fatalf("expected temperature 0.1, got %v", profile.Temperature)
	}
	if !profile.CompactPrompt {
		t.Fatal("expected compact prompt to be enabled")
	}
	if profile.MaxToolTurns != 3 {
		t.Fatalf("expected max tool turns 3, got %d", profile.MaxToolTurns)
	}
}

func TestResolveModelProfile_InfersCheapTierFromModelName(t *testing.T) {
	profile := ResolveModelProfile("anthropic", "claude-3-haiku-20240307", nil)

	if profile.Tier != "cheap" {
		t.Fatalf("expected cheap tier for haiku model, got %q", profile.Tier)
	}
	if profile.MaxOutputTokens <= 0 {
		t.Fatalf("expected positive max output tokens for cheap profile, got %d", profile.MaxOutputTokens)
	}
}

func TestResolveModelProfile_UsesCatalogCapabilityProfile(t *testing.T) {
	tempDir := t.TempDir()
	catalogPath := filepath.Join(tempDir, "model_catalog.json")
	catalog := `{"llm":[{"label":"Test","options":[{"value":"test:alpha","label":"Alpha","capability_tier":"strong","reasoning_strength":5,"max_output_tokens":4096,"max_tool_turns":8,"compact_prompt":false,"supported_task_types":["reasoning","architecture"]}]}]}`
	if err := os.WriteFile(catalogPath, []byte(catalog), 0o644); err != nil {
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

	profile := ResolveModelProfile("test", "alpha", nil)
	if profile.Tier != "strong" {
		t.Fatalf("expected strong tier from catalog, got %q", profile.Tier)
	}
	if profile.ReasoningStrength != 5 {
		t.Fatalf("expected reasoning strength 5 from catalog, got %d", profile.ReasoningStrength)
	}
	if profile.MaxOutputTokens != 4096 {
		t.Fatalf("expected max output tokens 4096 from catalog, got %d", profile.MaxOutputTokens)
	}
	if profile.MaxToolTurns != 8 {
		t.Fatalf("expected max tool turns 8 from catalog, got %d", profile.MaxToolTurns)
	}
	if len(profile.SupportedTaskTypes) != 2 || profile.SupportedTaskTypes[0] != "reasoning" || profile.SupportedTaskTypes[1] != "architecture" {
		t.Fatalf("expected supported task types from catalog, got %#v", profile.SupportedTaskTypes)
	}
}

func TestResolveModelProfile_UsesCatalogMediaCapabilityProfile(t *testing.T) {
	tempDir := t.TempDir()
	catalogPath := filepath.Join(tempDir, "model_catalog.json")
	catalog := `{"llm":[{"label":"Test","options":[{"value":"test:media","label":"Media Alpha","capability_tier":"strong","reasoning_strength":5,"max_output_tokens":4096,"max_tool_turns":8,"compact_prompt":false,"supported_task_types":["reasoning","multimodal"],"media_capability":3,"supported_media_types":["image","audio","video"]}]}]}`
	if err := os.WriteFile(catalogPath, []byte(catalog), 0o644); err != nil {
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

	profile := ResolveModelProfile("test", "media", nil)
	if profile.MediaCapability != 3 {
		t.Fatalf("expected media capability 3 from catalog, got %d", profile.MediaCapability)
	}
	if len(profile.SupportedMediaTypes) != 3 || profile.SupportedMediaTypes[0] != "image" || profile.SupportedMediaTypes[2] != "video" {
		t.Fatalf("expected supported media types from catalog, got %#v", profile.SupportedMediaTypes)
	}
}

func TestModelCatalogRegistry_SelectsStrongMediaModel(t *testing.T) {
	tempDir := t.TempDir()
	catalogPath := filepath.Join(tempDir, "model_catalog.json")
	catalog := `{"llm":[{"label":"Test","options":[{"value":"test:cheap","label":"Cheap","capability_tier":"cheap","reasoning_strength":2,"max_output_tokens":1024,"max_tool_turns":3,"compact_prompt":true,"supported_task_types":["mechanical","validation"],"media_capability":1,"supported_media_types":["image"]},{"value":"test:strong","label":"Strong","capability_tier":"strong","reasoning_strength":5,"max_output_tokens":4096,"max_tool_turns":8,"compact_prompt":false,"supported_task_types":["reasoning","implementation","multimodal"],"media_capability":3,"supported_media_types":["image","audio","video"]}]}]}`
	if err := os.WriteFile(catalogPath, []byte(catalog), 0o644); err != nil {
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

	registry := loadModelCatalogRegistry()
	req := ModelSelectionRequest{Provider: "test", TaskType: "implementation", RequiredMediaTypes: []string{"video"}, NeedMultimodal: true}
	entry, ok := registry.SelectBest(req)
	if !ok {
		t.Fatal("expected registry to return a matching model")
	}
	if entry.Model != "strong" {
		t.Fatalf("expected strong model for multimodal task, got %q", entry.Model)
	}
}

func TestModelCatalogRegistry_SelectsCheaperModelWhenBudgetIsTight(t *testing.T) {
	tempDir := t.TempDir()
	catalogPath := filepath.Join(tempDir, "model_catalog.json")
	catalog := `{"llm":[{"label":"Test","options":[{"value":"test:cheap","label":"Cheap","capability_tier":"cheap","reasoning_strength":2,"max_output_tokens":1024,"max_tool_turns":3,"compact_prompt":true,"supported_task_types":["implementation"],"input_cost_per_1m_tokens":0.1,"output_cost_per_1m_tokens":0.2},{"value":"test:expensive","label":"Expensive","capability_tier":"strong","reasoning_strength":5,"max_output_tokens":4096,"max_tool_turns":8,"compact_prompt":false,"supported_task_types":["implementation"],"input_cost_per_1m_tokens":3.0,"output_cost_per_1m_tokens":12.0}]}]}`
	if err := os.WriteFile(catalogPath, []byte(catalog), 0o644); err != nil {
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

	registry := loadModelCatalogRegistry()
	entry, ok := registry.SelectBest(ModelSelectionRequest{Provider: "test", TaskType: "implementation", EstimatedInputTokens: 6000, EstimatedOutputTokens: 1000, MaxCostUSD: 0.02})
	if !ok {
		t.Fatal("expected registry to return a matching model")
	}
	if entry.Model != "cheap" {
		t.Fatalf("expected cheap model under a tight budget, got %q", entry.Model)
	}
}

func TestModelCatalogRegistry_LoadsOfficialPricingMetadata(t *testing.T) {
	registry := loadModelCatalogRegistry()
	var found bool
	for _, entry := range registry.entries {
		if entry.Provider == "gemini" && entry.Model == "gemini-2.5-flash" {
			found = true
			if entry.PricingStatus == "unknown" && (entry.LastKnownInputCostUSD <= 0 && entry.LastKnownOutputCostUSD <= 0) {
				t.Fatalf("expected pricing metadata or a last-known price for gemini-2.5-flash, got status=%q input=%v output=%v", entry.PricingStatus, entry.InputCostPer1MTokens, entry.OutputCostPer1MTokens)
			}
			if entry.InputCostPer1MTokens <= 0 && entry.LastKnownInputCostUSD <= 0 {
				t.Fatalf("expected official pricing metadata or a last-known input price for gemini-2.5-flash, got input=%v last_known_input=%v", entry.InputCostPer1MTokens, entry.LastKnownInputCostUSD)
			}
			if entry.OutputCostPer1MTokens <= 0 && entry.LastKnownOutputCostUSD <= 0 {
				t.Fatalf("expected official pricing metadata or a last-known output price for gemini-2.5-flash, got output=%v last_known_output=%v", entry.OutputCostPer1MTokens, entry.LastKnownOutputCostUSD)
			}
			if entry.InputCostPer1MTokens < 0.2 || entry.OutputCostPer1MTokens < 1.0 {
				t.Fatalf("expected pricing to reflect official Gemini rates for gemini-2.5-flash, got input=%v output=%v", entry.InputCostPer1MTokens, entry.OutputCostPer1MTokens)
			}
		}
	}
	if !found {
		t.Fatal("expected gemini-2.5-flash entry in registry")
	}
}

func TestModelCatalogRegistry_PrefersCheapestCompatibleFlashLiteForBudgetSensitiveValidation(t *testing.T) {
	registry := loadModelCatalogRegistry()
	entry, ok := registry.SelectBest(ModelSelectionRequest{
		Provider:              "gemini",
		TaskType:              "validation",
		EstimatedInputTokens:  6000,
		EstimatedOutputTokens: 1000,
		MaxCostUSD:            0.01,
		PreferCheap:           true,
	})
	if !ok {
		t.Fatal("expected registry to return a matching model")
	}
	if entry.Tier != "cheap" {
		t.Fatalf("expected a cheap-tier model for budget-sensitive validation, got %q (%q)", entry.Model, entry.Tier)
	}
	if entry.Model != "gemini-2.0-flash-lite" && entry.Model != "gemini-2.5-flash-lite" {
		t.Fatalf("expected a flash-lite model for budget-sensitive validation, got %q", entry.Model)
	}
}

func TestModelCatalogRegistry_UsesLastKnownPricingWhenOfficialPricingIsMissing(t *testing.T) {
	tempDir := t.TempDir()
	catalogPath := filepath.Join(tempDir, "model_catalog.json")
	catalog := `{"llm":[{"label":"Test","options":[{"value":"test:legacy","label":"Legacy","capability_tier":"cheap","reasoning_strength":2,"max_output_tokens":1024,"max_tool_turns":3,"compact_prompt":true,"supported_task_types":["implementation"],"pricing_status":"last_known","pricing_source":"manual","last_known_input_cost_usd":0.2,"last_known_output_cost_usd":0.4}]}]}`
	if err := os.WriteFile(catalogPath, []byte(catalog), 0o644); err != nil {
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

	registry := loadModelCatalogRegistry()
	entry, ok := registry.SelectBest(ModelSelectionRequest{Provider: "test", TaskType: "implementation", EstimatedInputTokens: 6000, EstimatedOutputTokens: 1000, MaxCostUSD: 0.01, PreferCheap: true})
	if !ok {
		t.Fatal("expected registry to return a matching model")
	}
	if entry.Model != "legacy" {
		t.Fatalf("expected legacy model to be selected from last-known pricing, got %q", entry.Model)
	}
}

func TestApplyModelProfile_AppliesDefaultsOnlyWhenUnset(t *testing.T) {
	opts := ApplyModelProfile(GenOptions{}, ModelProfile{MaxOutputTokens: 768, Temperature: 0.2, CompactPrompt: true})
	if opts.MaxTokens != 768 {
		t.Fatalf("expected max tokens 768, got %d", opts.MaxTokens)
	}
	if opts.Temperature != 0.2 {
		t.Fatalf("expected temperature 0.2, got %v", opts.Temperature)
	}
	if !opts.CompactPrompt {
		t.Fatal("expected compact prompt to be enabled")
	}

	opts2 := GenOptions{MaxTokens: 1024, Temperature: 0.9}
	applied := ApplyModelProfile(opts2, ModelProfile{MaxOutputTokens: 256, Temperature: 0.1})
	if applied.MaxTokens != 1024 {
		t.Fatalf("expected explicit max tokens to remain 1024, got %d", applied.MaxTokens)
	}
	if applied.Temperature != 0.9 {
		t.Fatalf("expected explicit temperature to remain 0.9, got %v", applied.Temperature)
	}
}

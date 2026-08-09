package ai

import "testing"

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

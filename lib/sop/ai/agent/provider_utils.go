package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/sharedcode/sop/ai"
)

const ctxKeyThinkingLevel contextKey = "thinking_level"

// NormalizeProviderAndModel converts provider:model shorthand and standardizes
// known aliases to a single provider value used across the runtime.
func NormalizeProviderAndModel(provider, model string) (string, string) {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" {
		return "", model
	}

	if providerPart, modelPart, ok := strings.Cut(provider, ":"); ok && strings.TrimSpace(modelPart) != "" && model == "" {
		provider = strings.TrimSpace(providerPart)
		model = strings.TrimSpace(modelPart)
	}

	if strings.EqualFold(provider, "kelindar") {
		return "local", model
	}

	return provider, model
}

type modelCatalogFile struct {
	LLM []modelCatalogGroup `json:"llm,omitempty"`
}

type modelCatalogGroup struct {
	Label   string               `json:"label"`
	Options []modelCatalogOption `json:"options,omitempty"`
}

type modelCatalogOption struct {
	Value                string   `json:"value"`
	Label                string   `json:"label"`
	DefaultThinkingLevel string   `json:"default_thinking_level,omitempty"`
	ThinkingLevelRange   []string `json:"thinking_level_range,omitempty"`
}

func normalizeProviderName(provider string) string {
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "openai" {
		return "chatgpt"
	}
	return provider
}

func inferDefaultThinkingLevel(provider, model string) string {
	provider = normalizeProviderName(provider)
	model = strings.ToLower(strings.TrimSpace(model))

	if level := resolveDefaultThinkingLevelFromCatalog(provider, model); level != "" {
		return level
	}

	switch provider {
	case "anthropic":
		switch {
		case strings.Contains(model, "opus"):
			return "high"
		case strings.Contains(model, "sonnet"):
			return "high"
		case strings.Contains(model, "haiku"):
			return "medium"
		}
	case "gemini":
		switch {
		case strings.Contains(model, "flash-lite"):
			return "low"
		case strings.Contains(model, "flash"):
			return "medium"
		case strings.Contains(model, "pro"):
			return "high"
		}
	case "chatgpt":
		switch {
		case strings.Contains(model, "mini") || strings.Contains(model, "nano"):
			return "low"
		case strings.Contains(model, "pro"):
			return "high"
		default:
			return "medium"
		}
	}

	return ""
}

func resolveDefaultThinkingLevelFromCatalog(provider, model string) string {
	provider = normalizeProviderName(provider)
	model = strings.ToLower(strings.TrimSpace(model))
	if provider == "" || model == "" {
		return ""
	}

	for _, candidate := range modelCatalogCandidatePaths() {
		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}

		var catalog modelCatalogFile
		if err := json.Unmarshal(data, &catalog); err != nil {
			continue
		}

		for _, group := range catalog.LLM {
			for _, option := range group.Options {
				if option.DefaultThinkingLevel == "" {
					continue
				}
				if modelCatalogOptionMatches(provider, model, option.Value) {
					return strings.ToLower(strings.TrimSpace(option.DefaultThinkingLevel))
				}
			}
		}
	}
	return ""
}

func modelCatalogCandidatePaths() []string {
	seen := map[string]struct{}{}
	var paths []string
	add := func(path string) {
		if path == "" {
			return
		}
		cleaned := filepath.Clean(path)
		if _, ok := seen[cleaned]; ok {
			return
		}
		seen[cleaned] = struct{}{}
		paths = append(paths, cleaned)
	}

	if cwd, err := os.Getwd(); err == nil {
		current := filepath.Clean(cwd)
		for {
			add(filepath.Join(current, "model_catalog.json"))
			add(filepath.Join(current, "lib", "sop", "model_catalog.json"))
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}

	if exe, err := os.Executable(); err == nil {
		current := filepath.Dir(filepath.Clean(exe))
		for {
			add(filepath.Join(current, "model_catalog.json"))
			add(filepath.Join(current, "lib", "sop", "model_catalog.json"))
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}

	add("model_catalog.json")
	return paths
}

func modelCatalogOptionMatches(provider, model, optionValue string) bool {
	provider = normalizeProviderName(provider)
	model = strings.ToLower(strings.TrimSpace(model))
	optionValue = strings.TrimSpace(optionValue)
	if optionValue == "" {
		return false
	}

	if providerPart, modelPart, ok := strings.Cut(optionValue, ":"); ok {
		if normalizeProviderName(providerPart) != provider {
			return false
		}
		return strings.ToLower(strings.TrimSpace(modelPart)) == model
	}

	return strings.ToLower(optionValue) == model
}

func ResolveThinkingLevelForContext(ctx context.Context, gen ai.Generator, provider, model, thinkingLevel string) string {
	if ctx != nil {
		if override, ok := ctx.Value(ctxKeyThinkingLevel).(string); ok {
			override = strings.TrimSpace(override)
			if override != "" {
				if normalized := NormalizeThinkingLevelForGenerator(gen, provider, model, override); normalized != "" {
					return normalized
				}
			}
		}
	}
	return NormalizeThinkingLevelForGenerator(gen, provider, model, thinkingLevel)
}

func resolveThinkingLevelRangeFromCatalog(provider, model string) []string {
	provider = normalizeProviderName(provider)
	model = strings.ToLower(strings.TrimSpace(model))
	if provider == "" || model == "" {
		return nil
	}

	for _, candidate := range modelCatalogCandidatePaths() {
		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}

		var catalog modelCatalogFile
		if err := json.Unmarshal(data, &catalog); err != nil {
			continue
		}

		for _, group := range catalog.LLM {
			for _, option := range group.Options {
				if len(option.ThinkingLevelRange) == 0 {
					continue
				}
				if modelCatalogOptionMatches(provider, model, option.Value) {
					values := make([]string, 0, len(option.ThinkingLevelRange))
					for _, value := range option.ThinkingLevelRange {
						normalized := strings.ToLower(strings.TrimSpace(value))
						if normalized != "" {
							values = append(values, normalized)
						}
					}
					if len(values) > 0 {
						return values
					}
				}
			}
		}
	}
	return nil
}

func normalizeThinkingLevelToRange(level string, allowed []string) (string, bool) {
	level = strings.ToLower(strings.TrimSpace(level))
	if level == "" {
		return "", false
	}

	allowedLevels := make([]string, 0, len(allowed))
	for _, candidate := range allowed {
		normalized := strings.ToLower(strings.TrimSpace(candidate))
		if normalized != "" {
			allowedLevels = append(allowedLevels, normalized)
		}
	}
	if len(allowedLevels) == 0 {
		return "", false
	}
	for _, candidate := range allowedLevels {
		if candidate == level {
			return candidate, true
		}
	}

	order := map[string]int{"low": 0, "medium": 1, "high": 2}
	requestedIndex, hasRequested := order[level]
	if !hasRequested {
		return allowedLevels[0], true
	}

	bestLevel := allowedLevels[0]
	bestIndex := 0
	bestDistance := 0
	for _, candidate := range allowedLevels {
		candidateIndex, ok := order[candidate]
		if !ok {
			continue
		}
		distance := requestedIndex - candidateIndex
		if distance < 0 {
			distance = -distance
		}
		if bestLevel == "" || distance < bestDistance || (distance == bestDistance && candidateIndex < bestIndex) {
			bestLevel = candidate
			bestIndex = candidateIndex
			bestDistance = distance
		}
	}
	return bestLevel, true
}

// NormalizeThinkingLevelForGenerator adapts provider-specific thinking levels to
// values that are accepted by the underlying generator implementation.
func NormalizeThinkingLevelForGenerator(gen ai.Generator, provider, model, thinkingLevel string) string {
	level := strings.TrimSpace(thinkingLevel)
	if level == "" {
		level = inferDefaultThinkingLevel(provider, model)
	}
	if level == "" {
		return ""
	}

	level = strings.ToLower(strings.TrimSpace(level))
	if normalized, ok := normalizeThinkingLevelToRange(level, resolveThinkingLevelRangeFromCatalog(provider, model)); ok {
		level = normalized
	}

	if gen != nil && strings.EqualFold(gen.Name(), "anthropic") {
		if level == "low" {
			return "medium"
		}
	}
	return level
}

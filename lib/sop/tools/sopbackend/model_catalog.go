package sopbackend

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ModelCatalog struct {
	LLM      []ModelCatalogGroup `json:"llm,omitempty"`
	Embedder []ModelCatalogGroup `json:"embedder,omitempty"`
}

type ModelCatalogGroup struct {
	Label   string               `json:"label"`
	Options []ModelCatalogOption `json:"options,omitempty"`
}

type ModelCatalogOption struct {
	Value                string   `json:"value"`
	Label                string   `json:"label"`
	DefaultThinkingLevel string   `json:"default_thinking_level,omitempty"`
	ThinkingLevelRange   []string `json:"thinking_level_range,omitempty"`
}

func defaultLocalEmbedderOptions() []ModelCatalogOption {
	candidates := []string{
		filepath.Join("ai", "models"),
		filepath.Join("..", "ai", "models"),
		filepath.Join("..", "..", "ai", "models"),
	}

	seen := map[string]struct{}{}
	var options []ModelCatalogOption

	for _, dir := range candidates {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}

			var raw map[string]json.RawMessage
			if err := json.Unmarshal(data, &raw); err != nil {
				continue
			}
			for modelKey, rawEntry := range raw {
				if _, ok := seen[modelKey]; ok {
					continue
				}
				seen[modelKey] = struct{}{}

				var entryInfo struct {
					DisplayName string `json:"display_name"`
				}
				if err := json.Unmarshal(rawEntry, &entryInfo); err != nil {
					continue
				}
				value := "kelindar:" + modelKey
				label := "Kelindar / Gemini embedder"
				if strings.Contains(modelKey, "gemini") {
					value = "gemini:" + modelKey
					label = "Google embedding model"
				} else {
					label = "Kelindar embedded library"
				}
				if entryInfo.DisplayName != "" {
					label += " (" + entryInfo.DisplayName + ")"
				} else {
					label += " (" + modelKey + ")"
				}

				options = append(options, ModelCatalogOption{
					Value: value,
					Label: label,
				})
			}
		}
	}

	sort.Slice(options, func(i, j int) bool {
		return options[i].Label < options[j].Label
	})
	return options
}

func DefaultModelCatalog() ModelCatalog {
	localOptions := defaultLocalEmbedderOptions()
	if len(localOptions) == 0 || !containsLocalEmbedderOption(localOptions) {
		localOptions = append([]ModelCatalogOption{{Value: "kelindar:kelindar", Label: "Local Kelindar (default)"}}, localOptions...)
	}
	return ModelCatalog{
		LLM: []ModelCatalogGroup{
			{
				Label: "Google",
				Options: []ModelCatalogOption{
					{Value: "gemini:gemini-2.5-pro", Label: "Gemini 2.5 Pro", DefaultThinkingLevel: "high", ThinkingLevelRange: []string{"low", "medium", "high"}},
				},
			},
			{
				Label: "Anthropic",
				Options: []ModelCatalogOption{
					{Value: "anthropic:claude-sonnet-4-5", Label: "Claude Sonnet 4.5", DefaultThinkingLevel: "high", ThinkingLevelRange: []string{"medium", "high"}},
				},
			},
			{
				Label: "OpenAI",
				Options: []ModelCatalogOption{
					{Value: "openai:gpt-5.4", Label: "GPT-5.4", DefaultThinkingLevel: "medium", ThinkingLevelRange: []string{"low", "medium", "high"}},
				},
			},
		},
		Embedder: []ModelCatalogGroup{
			{
				Label:   "Local",
				Options: localOptions,
			},
		},
	}
}

const modelCatalogFilename = "model_catalog.json"

func containsLocalEmbedderOption(options []ModelCatalogOption) bool {
	for _, option := range options {
		value := strings.ToLower(option.Value)
		label := strings.ToLower(option.Label)
		if strings.Contains(value, "kelindar") || strings.Contains(value, "local") || strings.Contains(label, "kelindar") || strings.Contains(label, "local") {
			return true
		}
	}
	return false
}

func ensureModelCatalogDefaults(catalog *ModelCatalog) bool {
	defaults := DefaultModelCatalog()
	changed := false

	if len(catalog.LLM) == 0 {
		catalog.LLM = defaults.LLM
		changed = true
	}
	if len(catalog.Embedder) == 0 {
		catalog.Embedder = defaults.Embedder
		changed = true
	}

	return changed
}

func resolveModelCatalogPath(configPath string) string {
	if configPath != "" {
		return filepath.Join(filepath.Dir(configPath), modelCatalogFilename)
	}

	if cwd, err := os.Getwd(); err == nil {
		return filepath.Join(cwd, modelCatalogFilename)
	}

	return modelCatalogFilename
}

func modelCatalogCandidatePaths(configPath string) []string {
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

	addFromDir := func(start string) {
		if start == "" {
			return
		}
		current := filepath.Clean(start)
		for {
			add(filepath.Join(current, modelCatalogFilename))
			add(filepath.Join(current, "lib", "sop", modelCatalogFilename))
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}

	if configPath != "" {
		addFromDir(filepath.Dir(configPath))
		if exePath, err := os.Executable(); err == nil {
			addFromDir(filepath.Dir(exePath))
		}
		if cwd, err := os.Getwd(); err == nil {
			addFromDir(cwd)
		}
		add(modelCatalogFilename)
		return paths
	}

	if cwd, err := os.Getwd(); err == nil {
		addFromDir(cwd)
	}

	add(modelCatalogFilename)

	return paths
}

func LoadModelCatalog(configPath string) (bool, error) {
	modelCatalog = DefaultModelCatalog()

	for _, path := range modelCatalogCandidatePaths(configPath) {
		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false, err
		}

		var loaded ModelCatalog
		decodeErr := json.NewDecoder(f).Decode(&loaded)
		f.Close()
		if decodeErr != nil {
			return false, decodeErr
		}

		seededDefaults := ensureModelCatalogDefaults(&loaded)
		modelCatalog = loaded
		return seededDefaults, nil
	}

	return false, nil
}

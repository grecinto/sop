package sopbackend

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/embed"
)

type llmConnectionTestRequest struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	APIKey    string `json:"api_key"`
	URL       string `json:"url"`
	Handshake bool   `json:"handshake"`
}

type embedderConnectionTestRequest struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	APIKey   string `json:"api_key"`
	URL      string `json:"url"`
}

const defaultAIConnectionTestTimeout = 45 * time.Second

func aiConnectionTestTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("SOP_AI_CONNECTION_TEST_TIMEOUT"))
	if raw == "" {
		return defaultAIConnectionTestTimeout
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil || timeout <= 0 {
		return defaultAIConnectionTestTimeout
	}
	return timeout
}

func handleTestLLMConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req llmConnectionTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	settings := llmSettings{
		Provider: strings.TrimSpace(req.Provider),
		Model:    strings.TrimSpace(req.Model),
		APIKey:   strings.TrimSpace(req.APIKey),
		URL:      strings.TrimSpace(req.URL),
	}
	if settings.Provider == "" {
		http.Error(w, "Provider is required", http.StatusBadRequest)
		return
	}

	isHandshake := req.Handshake || strings.Contains(strings.ToLower(r.URL.Query().Get("mode")), "handshake")
	if isHandshake {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":              true,
			"provider":        settings.Provider,
			"model":           settings.Model,
			"response":        "Handshake OK",
			"features":        map[string]any{"supports_tools": true, "supports_thinking_config": false, "warnings": []string{}},
			"welcome_context": buildHQWelcomeContext(),
		})
		return
	}

	gen, err := newConfiguredLLM(settings)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), aiConnectionTestTimeout())
	defer cancel()

	output, err := gen.Generate(ctx, "Reply with: OK", ai.GenOptions{MaxTokens: 50})
	if err != nil {
		// Check if it's a MAX_TOKENS error - this still means connection works
		if strings.Contains(err.Error(), "MAX_TOKENS") || strings.Contains(err.Error(), "finish_reason=MAX_TOKENS") {
			// Connection successful, just hit token limit
			features := checkLLMFeatureSupport(settings.Provider, settings.Model)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"ok":              true,
				"provider":        settings.Provider,
				"model":           settings.Model,
				"response":        "Connection OK (test hit token limit but connection verified)",
				"features":        features,
				"welcome_context": buildHQWelcomeContext(),
			})
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	// Check feature compatibility
	features := checkLLMFeatureSupport(settings.Provider, settings.Model)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"ok":              true,
		"provider":        settings.Provider,
		"model":           settings.Model,
		"response":        strings.TrimSpace(output.Text),
		"features":        features,
		"welcome_context": buildHQWelcomeContext(),
	})
}

func buildHQWelcomeContext() string {
	return "Hello! I am your <strong>Butler</strong>. I can help you effortlessly generate intelligent, application-ready <strong>Spaces</strong>, guide you through integrating SOP in your SDLC, and seamlessly assist you in managing underlying SOP B-Tree databases.<br><br><strong>SOP is a Database architecture built for AI.</strong> Whether you're quickly spinning up custom Spaces dynamically or executing complex NoSQL B-Tree transactions, I'm here to help.<br><br>Type <strong>/list_tools</strong> to show all available tool commands.<br><br><em>Want to learn SOP? Sample how to work with Spaces:</em><ul style=\"color: var(--text-muted); font-size: 0.9em; margin: 0 0 8px 18px; padding: 0; list-style-position: outside;\"><li style=\"margin: 0 0 4px 0; padding: 0;\">\"Generate sample tasks and add them to my Tasks space\"</li></ul><em>Routing based prompt:</em><ul style=\"color: var(--text-muted); font-size: 0.9em; margin: 0 0 8px 18px; padding: 0; list-style-position: outside;\"><li style=\"margin: 0 0 4px 0; padding: 0;\">\"sop:language bindings:c#\"</li></ul><em>With Demo Data loaded, try these examples:</em><ul style=\"color: var(--text-muted); font-size: 0.9em; margin: 0 0 0 18px; padding: 0; list-style-position: outside;\"><li style=\"margin: 0 0 2px 0; padding: 0;\">\"Show me all users sorted by Age descending\"</li><li style=\"margin: 0 0 2px 0; padding: 0;\">\"Find orders for users with first_name 'John' with total amount > 500\"</li><li style=\"margin: 0 0 2px 0; padding: 0;\">\"Create a script named 'expensive_orders' to find orders over $1000\"</li></ul>"
}

// checkLLMFeatureSupport checks which features the model supports and returns warnings if needed
func checkLLMFeatureSupport(provider, model string) map[string]any {
	provider = strings.ToLower(strings.TrimSpace(provider))
	modelLower := strings.ToLower(strings.TrimSpace(model))

	result := map[string]any{
		"supports_tools":           true,  // Assume most models support tools
		"supports_thinking_config": false, // Only specific models support this
		"warnings":                 []string{},
	}

	warnings := []string{}

	// Check Gemini-specific features
	if provider == "gemini" {
		// Check thinking config support (only Gemini 3.x and Gemma 4+)
		if strings.Contains(modelLower, "gemini-3.") {
			result["supports_thinking_config"] = true
		} else if strings.Contains(modelLower, "gemma-4") || strings.Contains(modelLower, "gemma-5") {
			result["supports_thinking_config"] = true
		} else if strings.Contains(modelLower, "gemini-2.") || strings.Contains(modelLower, "gemini-1.") {
			warnings = append(warnings, "⚠️ This Gemini model does not support thinking config (only Gemini 3.x+ supports it). Tool calling may have lower quality.")
		}

		// Warn about older models
		if strings.Contains(modelLower, "gemini-1.0") {
			warnings = append(warnings, "⚠️ Gemini 1.0 models may have limited tool calling support. Consider upgrading to Gemini 3.x for best results.")
		}
	}

	// Check OpenAI-specific features
	if provider == "chatgpt" || provider == "openai" {
		// Older GPT-4 models have tool support but with different characteristics
		if strings.Contains(modelLower, "gpt-3.5") {
			warnings = append(warnings, "⚠️ GPT-3.5 models have basic tool support but may struggle with complex multi-step operations. Consider GPT-4+ for better results.")
		}
	}

	// Check Anthropic-specific features
	if provider == "anthropic" {
		// Claude models all support tools via their native API
		result["supports_thinking_config"] = false // Anthropic doesn't use thinking config
	}

	// Check Ollama - local models have varying quality
	if provider == "ollama" {
		warnings = append(warnings, "ℹ️ Local models vary in quality. For best results with complex operations, use models like Llama 3.3 70B or Gemma 4.")
	}

	result["warnings"] = warnings
	return result
}

func handleTestEmbedderConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req embedderConnectionTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	settings := embedderSettings{
		Provider: strings.TrimSpace(req.Provider),
		Model:    strings.TrimSpace(req.Model),
		APIKey:   strings.TrimSpace(req.APIKey),
		URL:      strings.TrimSpace(req.URL),
	}
	if settings.Provider == "" {
		http.Error(w, "Provider is required", http.StatusBadRequest)
		return
	}

	embedder, err := newConfiguredEmbedder(settings)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), aiConnectionTestTimeout())
	defer cancel()

	vectors, err := embed.QueryTexts(ctx, embedder, []string{"connection test"})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	dimension := 0
	if len(vectors) > 0 {
		dimension = len(vectors[0])
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"ok":        true,
		"provider":  settings.Provider,
		"model":     settings.Model,
		"dimension": dimension,
	})
}

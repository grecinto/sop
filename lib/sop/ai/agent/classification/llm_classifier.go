package classification

import (
	"context"
	"encoding/json"
	"fmt"
)

// LLMClassifier uses an LLM for classification (fallback for ambiguous queries)
type LLMClassifier struct {
	generator LLMGenerator
}

// LLMGenerator is an interface for generating text with an LLM
type LLMGenerator interface {
	Generate(ctx context.Context, prompt string) (string, error)
	Name() string
}

// NewLLMClassifier creates a new LLM-based classifier
func NewLLMClassifier(generator LLMGenerator) *LLMClassifier {
	return &LLMClassifier{
		generator: generator,
	}
}

// Classify uses an LLM to classify ambiguous queries
func (l *LLMClassifier) Classify(ctx context.Context, query string) (ClassificationResult, error) {
	if l.generator == nil {
		return ClassificationResult{}, fmt.Errorf("generator not configured")
	}

	prompt := fmt.Sprintf(`Classify this query into one of the following capability hints:
- coding_workflow: code edits, implementations, refactoring, architecture, documentation
- desktop_automation: shell commands, git operations, local tasks, file operations
- web_research: web searches, crawling, online research
- stores: database operations, table queries, data management
- general: everything else

Query: %s

Return JSON only: {"capability_hint": "...", "intent_family": "...", "confidence": 0.0-1.0}`, query)

	output, err := l.generator.Generate(ctx, prompt)
	if err != nil {
		return ClassificationResult{}, fmt.Errorf("LLM generation failed: %w", err)
	}

	var response struct {
		CapabilityHint string  `json:"capability_hint"`
		IntentFamily   string  `json:"intent_family"`
		Confidence     float64 `json:"confidence"`
	}

	if err := json.Unmarshal([]byte(output), &response); err != nil {
		return ClassificationResult{}, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	return ClassificationResult{
		IntentFamily:   response.IntentFamily,
		CapabilityHint: response.CapabilityHint,
		Confidence:     response.Confidence,
		Method:         "llm",
		Metadata: map[string]any{
			"model": l.generator.Name(),
		},
	}, nil
}

func (l *LLMClassifier) Name() string  { return "llm" }
func (l *LLMClassifier) Priority() int { return 3 } // Lowest priority (slowest)

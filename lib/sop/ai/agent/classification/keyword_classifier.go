package classification

import (
	"context"
	"fmt"
	"strings"
)

// KeywordClassifier uses lexical analysis for fast classification
type KeywordClassifier struct {
	rules []KeywordRule
}

// KeywordRule defines a classification rule based on keyword matching
type KeywordRule struct {
	Keywords       []string
	IntentFamily   string
	CapabilityHint string
	Confidence     float64
}

// NewKeywordClassifier creates a new keyword-based classifier with default rules
func NewKeywordClassifier() *KeywordClassifier {
	return &KeywordClassifier{
		rules: []KeywordRule{
			{
				Keywords: []string{
					"fix", "refactor", "patch", "edit", "implement",
					"modify", "add test", "update", "repo", "code",
					"parser", "function", "class", "module", "generate",
					"design", "plan", "architect", "propose", "document",
					"readme", "spec", "requirement",
				},
				IntentFamily:   "repo_edit",
				CapabilityHint: "coding_workflow",
				Confidence:     0.8,
			},
			{
				Keywords: []string{
					"git", "cmd", "shell", "postgres", "docker",
					"terminal", "file system", "filesystem", "local",
					"command", "exec", "execute", "run",
				},
				IntentFamily:   "local_ops",
				CapabilityHint: "desktop_automation",
				Confidence:     0.85,
			},
			{
				Keywords: []string{
					"search web", "crawl", "research online",
					"find information", "scrape", "gather data",
					"lookup", "browse", "fetch",
				},
				IntentFamily:   "web_research",
				CapabilityHint: "web_research",
				Confidence:     0.8,
			},
			{
				Keywords: []string{
					"store", "stores", "database", "table",
					"record", "query", "data", "sql",
				},
				IntentFamily:   "data_ops",
				CapabilityHint: "stores",
				Confidence:     0.75,
			},
		},
	}
}

// AddRule adds a custom classification rule
func (k *KeywordClassifier) AddRule(rule KeywordRule) {
	k.rules = append(k.rules, rule)
}

// Classify analyzes query using keyword matching
func (k *KeywordClassifier) Classify(ctx context.Context, query string) (ClassificationResult, error) {
	q := strings.ToLower(strings.TrimSpace(query))

	// Try each rule
	for _, rule := range k.rules {
		for _, keyword := range rule.Keywords {
			if strings.Contains(q, strings.ToLower(keyword)) {
				return ClassificationResult{
					IntentFamily:   rule.IntentFamily,
					CapabilityHint: rule.CapabilityHint,
					Confidence:     rule.Confidence,
					Method:         "keyword",
					Metadata: map[string]any{
						"matched_keyword": keyword,
					},
				}, nil
			}
		}
	}

	// No match
	return ClassificationResult{}, fmt.Errorf("no keyword match found")
}

func (k *KeywordClassifier) Name() string  { return "keyword" }
func (k *KeywordClassifier) Priority() int { return 2 } // After MRU

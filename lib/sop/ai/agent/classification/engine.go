package classification

import (
	"context"
	log "log/slog"
	"sort"
)

// ClassificationResult captures the outcome of query classification
type ClassificationResult struct {
	IntentFamily   string
	CapabilityHint string
	Domain         string
	Confidence     float64
	Method         string // "keyword", "llm", "mru", "chained"
	Metadata       map[string]any
}

// ClassificationEngine defines the interface for query classification
type ClassificationEngine interface {
	// Classify analyzes a query and returns classification result
	Classify(ctx context.Context, query string) (ClassificationResult, error)

	// Name returns the classifier name for logging/debugging
	Name() string

	// Priority returns the priority (lower = higher priority in chain)
	Priority() int
}

// ChainedClassifier tries multiple classifiers in priority order
type ChainedClassifier struct {
	classifiers []ClassificationEngine
}

// NewChainedClassifier creates a classifier that chains multiple engines
func NewChainedClassifier(classifiers ...ClassificationEngine) *ChainedClassifier {
	// Sort by priority (lower number = higher priority)
	sorted := make([]ClassificationEngine, len(classifiers))
	copy(sorted, classifiers)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Priority() < sorted[j].Priority()
	})

	return &ChainedClassifier{
		classifiers: sorted,
	}
}

// Classify tries each classifier in order until one succeeds with sufficient confidence
func (c *ChainedClassifier) Classify(ctx context.Context, query string) (ClassificationResult, error) {
	for _, classifier := range c.classifiers {
		result, err := classifier.Classify(ctx, query)
		if err != nil {
			log.Debug("Classifier failed", "name", classifier.Name(), "error", err)
			continue
		}

		// If confidence is high enough, use this result
		if result.Confidence > 0.5 {
			log.Info("Classification succeeded",
				"method", result.Method,
				"capability", result.CapabilityHint,
				"confidence", result.Confidence)
			return result, nil
		}

		log.Debug("Classifier returned low confidence",
			"name", classifier.Name(),
			"confidence", result.Confidence)
	}

	// Fallback to general classification
	log.Warn("All classifiers failed, falling back to general classification")
	return ClassificationResult{
		IntentFamily:   "general",
		CapabilityHint: "general",
		Confidence:     0.0,
		Method:         "fallback",
	}, nil
}

func (c *ChainedClassifier) Name() string   { return "chained" }
func (c *ChainedClassifier) Priority() int  { return 0 }
func (c *ChainedClassifier) String() string { return "ChainedClassifier" }

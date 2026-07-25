package classification

import (
	"context"
	"fmt"
)

// MRUClassifier uses session history (Most Recently Used) for classification
type MRUClassifier struct {
	mruGetter func(ctx context.Context) (string, string, bool) // Returns (capabilityHint, intentFamily, ok)
}

// NewMRUClassifier creates a new MRU-based classifier
func NewMRUClassifier(mruGetter func(ctx context.Context) (string, string, bool)) *MRUClassifier {
	return &MRUClassifier{
		mruGetter: mruGetter,
	}
}

// Classify uses session history to classify queries
func (m *MRUClassifier) Classify(ctx context.Context, query string) (ClassificationResult, error) {
	if m.mruGetter == nil {
		return ClassificationResult{}, fmt.Errorf("mru getter not configured")
	}

	capabilityHint, intentFamily, ok := m.mruGetter(ctx)
	if !ok {
		return ClassificationResult{}, fmt.Errorf("no MRU context available")
	}

	return ClassificationResult{
		IntentFamily:   intentFamily,
		CapabilityHint: capabilityHint,
		Confidence:     0.9, // High confidence for MRU
		Method:         "mru",
		Metadata: map[string]any{
			"source": "session_history",
		},
	}, nil
}

func (m *MRUClassifier) Name() string  { return "mru" }
func (m *MRUClassifier) Priority() int { return 1 } // Highest priority

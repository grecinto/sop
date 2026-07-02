package agent

import (
	"strings"
	"testing"
)

func TestBuildSelectedSpaceResearchContextPartsUsesPriorSuccesses(t *testing.T) {
	state := &ResearchWorkflowState{
		SuccessfulCategoryPaths: []string{"Engineering/Code"},
		SuccessfulItemPaths:     []string{"Engineering/Code/Parsing"},
	}

	parts := buildSelectedSpaceResearchContextParts("fix parser bugs", state)
	if len(parts) == 0 {
		t.Fatalf("expected research context parts, got none")
	}

	joined := strings.Join(parts, "\n")
	if !strings.Contains(joined, "prior successful category paths") {
		t.Fatalf("expected prior category paths in research context, got %q", joined)
	}
	if !strings.Contains(joined, "Engineering/Code/Parsing") {
		t.Fatalf("expected prior item paths in research context, got %q", joined)
	}
	if !strings.Contains(joined, "fix parser bugs") {
		t.Fatalf("expected current ask to be included in research context, got %q", joined)
	}
}

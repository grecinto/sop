package ideworker

import "testing"

func TestShouldEnrichContinueGuidance(t *testing.T) {
	if !ShouldEnrichContinueGuidance("planning", map[string]any{"coding_task_type": "implementation"}) {
		t.Fatal("expected planning implementation tasks to enrich guidance")
	}

	if ShouldEnrichContinueGuidance("execution", map[string]any{"coding_task_type": "review"}) {
		t.Fatal("expected non-implementation tasks to skip enrichment")
	}

	if ShouldEnrichContinueGuidance("delivery", nil) {
		t.Fatal("expected non-planning or non-execution phases to skip enrichment")
	}
}

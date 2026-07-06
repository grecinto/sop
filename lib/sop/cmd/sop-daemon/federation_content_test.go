package main

import "testing"

func TestSelectRelevantCategoriesForSecurityQuery(t *testing.T) {
	categories := selectRelevantCategories("security vulnerability in auth middleware")
	if len(categories) == 0 {
		t.Fatal("expected security-related categories")
	}

	seen := map[FederatedContentCategory]bool{}
	for _, category := range categories {
		seen[category] = true
	}

	if !seen[CategorySecurityPattern] {
		t.Fatalf("expected security category, got %v", categories)
	}
	if !seen[CategorySafetyHeuristic] {
		t.Fatalf("expected safety category, got %v", categories)
	}
}

func TestSelectRelevantCategoriesForPerformanceQuery(t *testing.T) {
	categories := selectRelevantCategories("slow API response and caching strategy")
	if len(categories) == 0 {
		t.Fatal("expected performance-related categories")
	}

	seen := map[FederatedContentCategory]bool{}
	for _, category := range categories {
		seen[category] = true
	}

	if !seen[CategoryPerformanceOptimization] {
		t.Fatalf("expected performance category, got %v", categories)
	}
	if !seen[CategoryToolPreference] {
		t.Fatalf("expected tool preference category, got %v", categories)
	}
}

func TestAnalyzeToolSequenceDetectsVerificationPattern(t *testing.T) {
	artifact := analyzeToolSequence("fix bug in parser", []string{"replace_string_in_file", "runTests"})
	if !artifact.ShouldShare {
		t.Fatal("expected verification artifact to be promoted")
	}
	if artifact.Category != CategoryWorkflowPattern {
		t.Fatalf("expected workflow category, got %s", artifact.Category)
	}
}

func TestPromoteSharedMemoryArtifactForAskSkipsLowValueQueries(t *testing.T) {
	if err := promoteSharedMemoryArtifactForAsk(nil, "hello", "hi", nil); err != nil {
		t.Fatalf("expected low-value query to be skipped, got %v", err)
	}
}

func TestCategorizeFederationArtifactDetectsErrorResolution(t *testing.T) {
	category, confidence, shouldShare := categorizeFederationArtifact("fix the failing auth bug", "I fixed the auth issue and resolved the bug")
	if !shouldShare {
		t.Fatal("expected error-resolution artifact to be promoted")
	}
	if category != CategoryErrorResolution {
		t.Fatalf("expected error resolution category, got %s", category)
	}
	if confidence < 0.8 {
		t.Fatalf("expected high confidence, got %f", confidence)
	}
}

func TestShouldPromoteFederationArtifactRejectsLowConfidenceOrVagueContent(t *testing.T) {
	if shouldPromoteFederationArtifact("", "some answer", CategoryWorkflowPattern, 0.8) {
		t.Fatal("expected empty query to be rejected")
	}
	if shouldPromoteFederationArtifact("fix bug", "not sure", CategoryAntiPattern, 0.8) {
		t.Fatal("expected uncertain answer to be rejected")
	}
	if shouldPromoteFederationArtifact("fix bug", "this resolves the issue after a careful change", CategoryErrorResolution, 0.6) {
		t.Fatal("expected low-confidence artifact to be rejected")
	}
}

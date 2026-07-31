package teampatterns

import (
	"testing"
	"time"
)

func TestTeamPatternToMapFromMap(t *testing.T) {
	// Create a pattern with all fields populated
	original := &TeamPattern{
		DocumentID:      "test-doc-123",
		PatternName:     "JWT Validation",
		PatternType:     "authentication",
		Convention:      "Use auth.ValidateRequestToken(r)",
		AntiPattern:     "DO NOT use jwt.Parse()",
		Reasoning:       "Our helper includes tenant validation",
		Example:         "userID, err := auth.ValidateRequestToken(r)",
		References:      []string{"lib/auth/jwt.go:45", "examples/api/handlers.go:23"},
		WhenToUse:       "All authenticated HTTP endpoints",
		RelatedPatterns: []string{"Error Response Format", "Auth Middleware"},
		Tags:            []string{"security", "http", "authentication"},
		ScopeType:       "team",
		ScopeID:         "engineering",
		Status:          "active",
		CreatedAt:       time.Now().UnixMilli(),
		UpdatedAt:       time.Now().UnixMilli(),
		LastUsedAt:      time.Now().UnixMilli(),
		UsageCount:      42,
		EmbeddingHash:   "hash123",
	}

	// Convert to map
	data := original.ToMap()

	// Verify payload_type is set
	if payloadType, ok := data["payload_type"].(string); !ok || payloadType != "team_pattern" {
		t.Errorf("Expected payload_type=team_pattern, got: %v", data["payload_type"])
	}

	// Convert back from map
	restored := FromMap(data)

	// Verify all fields match
	if restored.DocumentID != original.DocumentID {
		t.Errorf("DocumentID mismatch: got %s, want %s", restored.DocumentID, original.DocumentID)
	}
	if restored.PatternName != original.PatternName {
		t.Errorf("PatternName mismatch: got %s, want %s", restored.PatternName, original.PatternName)
	}
	if restored.PatternType != original.PatternType {
		t.Errorf("PatternType mismatch: got %s, want %s", restored.PatternType, original.PatternType)
	}
	if restored.Convention != original.Convention {
		t.Errorf("Convention mismatch: got %s, want %s", restored.Convention, original.Convention)
	}
	if restored.AntiPattern != original.AntiPattern {
		t.Errorf("AntiPattern mismatch: got %s, want %s", restored.AntiPattern, original.AntiPattern)
	}
	if restored.Reasoning != original.Reasoning {
		t.Errorf("Reasoning mismatch: got %s, want %s", restored.Reasoning, original.Reasoning)
	}
	if restored.Example != original.Example {
		t.Errorf("Example mismatch: got %s, want %s", restored.Example, original.Example)
	}
	if len(restored.References) != len(original.References) {
		t.Errorf("References length mismatch: got %d, want %d", len(restored.References), len(original.References))
	}
	if restored.WhenToUse != original.WhenToUse {
		t.Errorf("WhenToUse mismatch: got %s, want %s", restored.WhenToUse, original.WhenToUse)
	}
	if len(restored.RelatedPatterns) != len(original.RelatedPatterns) {
		t.Errorf("RelatedPatterns length mismatch: got %d, want %d", len(restored.RelatedPatterns), len(original.RelatedPatterns))
	}
	if len(restored.Tags) != len(original.Tags) {
		t.Errorf("Tags length mismatch: got %d, want %d", len(restored.Tags), len(original.Tags))
	}
	if restored.ScopeType != original.ScopeType {
		t.Errorf("ScopeType mismatch: got %s, want %s", restored.ScopeType, original.ScopeType)
	}
	if restored.ScopeID != original.ScopeID {
		t.Errorf("ScopeID mismatch: got %s, want %s", restored.ScopeID, original.ScopeID)
	}
	if restored.Status != original.Status {
		t.Errorf("Status mismatch: got %s, want %s", restored.Status, original.Status)
	}
	if restored.UsageCount != original.UsageCount {
		t.Errorf("UsageCount mismatch: got %d, want %d", restored.UsageCount, original.UsageCount)
	}
	if restored.EmbeddingHash != original.EmbeddingHash {
		t.Errorf("EmbeddingHash mismatch: got %s, want %s", restored.EmbeddingHash, original.EmbeddingHash)
	}
}

func TestBuildCategoryPath(t *testing.T) {
	tests := []struct {
		name         string
		pattern      *TeamPattern
		expectedPath string
	}{
		{
			name: "Authentication pattern",
			pattern: &TeamPattern{
				PatternType: "authentication",
				PatternName: "JWT Validation",
			},
			expectedPath: "Team Patterns/Authentication/JWT Validation",
		},
		{
			name: "Error handling pattern",
			pattern: &TeamPattern{
				PatternType: "error_handling",
				PatternName: "Error Wrapper Pattern",
			},
			expectedPath: "Team Patterns/Error Handling/Error Wrapper Pattern",
		},
		{
			name: "Database pattern",
			pattern: &TeamPattern{
				PatternType: "database",
				PatternName: "Query Builder Usage",
			},
			expectedPath: "Team Patterns/Database/Query Builder Usage",
		},
		{
			name: "Pattern without type",
			pattern: &TeamPattern{
				PatternName: "Generic Pattern",
			},
			expectedPath: "Team Patterns/Generic Pattern",
		},
		{
			name: "Pattern with underscore type",
			pattern: &TeamPattern{
				PatternType: "background_jobs",
				PatternName: "Temporal Worker Pattern",
			},
			expectedPath: "Team Patterns/Background Jobs/Temporal Worker Pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.pattern.BuildCategoryPath()
			if path != tt.expectedPath {
				t.Errorf("BuildCategoryPath() = %s, want %s", path, tt.expectedPath)
			}
		})
	}
}

func TestBuildSummaryText(t *testing.T) {
	pattern := &TeamPattern{
		PatternName: "JWT Validation",
		PatternType: "authentication",
		Convention:  "Use auth.ValidateRequestToken(r)",
		AntiPattern: "DO NOT use jwt.Parse()",
		WhenToUse:   "All authenticated HTTP endpoints",
		Tags:        []string{"security", "http"},
	}

	summary := pattern.BuildSummaryText()

	// Verify summary contains key information
	expectedSubstrings := []string{
		"Pattern: JWT Validation",
		"Type: authentication",
		"Convention: Use auth.ValidateRequestToken(r)",
		"Anti-pattern: DO NOT use jwt.Parse()",
		"Use when: All authenticated HTTP endpoints",
		"Tags: security, http",
	}

	for _, expected := range expectedSubstrings {
		if !contains(summary, expected) {
			t.Errorf("BuildSummaryText() missing expected substring: %s", expected)
		}
	}
}

func TestFormatPatternForDisplay(t *testing.T) {
	pattern := &TeamPattern{
		PatternName:     "JWT Validation",
		PatternType:     "authentication",
		Convention:      "Use auth.ValidateRequestToken(r)",
		AntiPattern:     "DO NOT use jwt.Parse()",
		Reasoning:       "Our helper includes tenant validation + audit logging",
		Example:         "userID, err := auth.ValidateRequestToken(r)\nif err != nil {\n    return auth.ErrorResponse(w, err)\n}",
		WhenToUse:       "All authenticated HTTP endpoints",
		References:      []string{"lib/auth/jwt.go:45", "examples/api/handlers.go:23"},
		RelatedPatterns: []string{"Error Response Format", "Auth Middleware"},
		Tags:            []string{"security", "http", "authentication"},
	}

	formatted := FormatPatternForDisplay(pattern, 1)

	// Verify formatted output contains key sections
	expectedSubstrings := []string{
		"## Pattern 1: JWT Validation",
		"**Convention**: Use auth.ValidateRequestToken(r)",
		"**Anti-Pattern**: DO NOT use jwt.Parse()",
		"**Reasoning**: Our helper includes tenant validation",
		"**Example**:",
		"**When to Use**: All authenticated HTTP endpoints",
		"**References**: lib/auth/jwt.go:45, examples/api/handlers.go:23",
		"**Related Patterns**: Error Response Format, Auth Middleware",
		"**Tags**: security, http, authentication",
	}

	for _, expected := range expectedSubstrings {
		if !contains(formatted, expected) {
			t.Errorf("FormatPatternForDisplay() missing expected substring: %s", expected)
		}
	}
}

func TestStoreAndSearchTeamPatterns(t *testing.T) {
	// This is an integration test that would require a real LTM instance
	// For now, we verify the pattern structure is correct

	pattern := &TeamPattern{
		PatternName:     "JWT Validation",
		PatternType:     "authentication",
		Convention:      "Use auth.ValidateRequestToken(r)",
		AntiPattern:     "DO NOT use jwt.Parse()",
		Reasoning:       "Our helper includes tenant validation",
		Example:         "userID, err := auth.ValidateRequestToken(r)",
		References:      []string{"lib/auth/jwt.go:45"},
		WhenToUse:       "All authenticated HTTP endpoints",
		RelatedPatterns: []string{"Error Response Format"},
		Tags:            []string{"security", "http"},
		ScopeType:       "team",
		ScopeID:         "engineering",
	}

	// Verify pattern has valid structure
	if pattern.PatternName == "" {
		t.Error("PatternName should not be empty")
	}
	if pattern.PatternType == "" {
		t.Error("PatternType should not be empty")
	}
	if pattern.Convention == "" {
		t.Error("Convention should not be empty")
	}

	// Verify category path is valid
	categoryPath := pattern.BuildCategoryPath()
	if categoryPath != "Team Patterns/Authentication/JWT Validation" {
		t.Errorf("Unexpected category path: %s", categoryPath)
	}

	// Verify summary text is not empty
	summary := pattern.BuildSummaryText()
	if summary == "" {
		t.Error("BuildSummaryText() should not return empty string")
	}

	// Verify map conversion works
	data := pattern.ToMap()
	if data["payload_type"] != "team_pattern" {
		t.Error("Expected payload_type to be team_pattern")
	}

	// Verify we can restore from map
	restored := FromMap(data)
	if restored.PatternName != pattern.PatternName {
		t.Error("Failed to restore PatternName from map")
	}
}

func TestPatternLifecycle(t *testing.T) {
	pattern := &TeamPattern{
		PatternName: "Test Pattern",
		PatternType: "testing",
		Convention:  "Use test helpers",
		Status:      "active",
		UsageCount:  0,
	}

	// Verify default status
	if pattern.Status != "active" {
		t.Errorf("Expected default status 'active', got: %s", pattern.Status)
	}

	// Simulate usage tracking
	pattern.UsageCount++
	pattern.LastUsedAt = time.Now().UnixMilli()

	if pattern.UsageCount != 1 {
		t.Errorf("Expected UsageCount=1, got: %d", pattern.UsageCount)
	}
	if pattern.LastUsedAt == 0 {
		t.Error("Expected LastUsedAt to be set")
	}

	// Verify lifecycle transitions
	pattern.Status = "deprecated"
	if pattern.Status != "deprecated" {
		t.Errorf("Expected status 'deprecated', got: %s", pattern.Status)
	}

	pattern.Status = "archived"
	if pattern.Status != "archived" {
		t.Errorf("Expected status 'archived', got: %s", pattern.Status)
	}
}

func TestEmptyPattern(t *testing.T) {
	pattern := &TeamPattern{}

	// Should handle empty pattern gracefully
	path := pattern.BuildCategoryPath()
	if path != "Team Patterns" {
		t.Errorf("Empty pattern should have base path 'Team Patterns', got: %s", path)
	}

	summary := pattern.BuildSummaryText()
	if summary != "" {
		t.Errorf("Empty pattern should have empty summary, got: %s", summary)
	}

	data := pattern.ToMap()
	if data["payload_type"] != "team_pattern" {
		t.Error("Empty pattern should still have payload_type")
	}

	// Should be able to restore empty pattern
	restored := FromMap(data)
	if restored == nil {
		t.Error("Should be able to restore empty pattern")
	}
}

// Helper function to check if string contains substring
func contains(str, substr string) bool {
	return len(str) >= len(substr) && (str == substr || len(substr) == 0 ||
		(len(str) > 0 && len(substr) > 0 && findSubstring(str, substr)))
}

func findSubstring(str, substr string) bool {
	for i := 0; i <= len(str)-len(substr); i++ {
		if str[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

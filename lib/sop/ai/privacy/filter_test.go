package privacy

import (
	"context"
	"testing"
)

func TestPrivacyFilter_GlobalOptOut(t *testing.T) {
	settings := NewDefaultPrivacySettings("test-user")
	settings.GlobalShareEnabled = false

	detector := NewPIIDetector()
	filter := NewPrivacyFilter(settings, detector, &NoOpAuditLogger{})

	result, err := filter.FilterBeforeShare(context.Background(), "workflow_pattern", "test content", "test-user")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Action != FilterActionBlock {
		t.Errorf("Expected block action, got %s", result.Action)
	}

	if result.Reason != "user opted out globally" {
		t.Errorf("Expected 'user opted out globally', got %s", result.Reason)
	}
}

func TestPrivacyFilter_CategoryOptOut(t *testing.T) {
	settings := NewDefaultPrivacySettings("test-user")
	settings.GlobalShareEnabled = true
	settings.SetCategoryEnabled("workflow_pattern", false)

	detector := NewPIIDetector()
	filter := NewPrivacyFilter(settings, detector, &NoOpAuditLogger{})

	result, err := filter.FilterBeforeShare(context.Background(), "workflow_pattern", "test content", "test-user")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Action != FilterActionBlock {
		t.Errorf("Expected block action, got %s", result.Action)
	}

	if result.Reason != "category opted out" {
		t.Errorf("Expected 'category opted out', got %s", result.Reason)
	}
}

func TestPrivacyFilter_PrivateSession(t *testing.T) {
	settings := NewDefaultPrivacySettings("test-user")
	settings.PrivateSessionMode = true

	detector := NewPIIDetector()
	filter := NewPrivacyFilter(settings, detector, &NoOpAuditLogger{})

	result, err := filter.FilterBeforeShare(context.Background(), "workflow_pattern", "test content", "test-user")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Action != FilterActionBlock {
		t.Errorf("Expected block action, got %s", result.Action)
	}

	if result.Reason != "private session active" {
		t.Errorf("Expected 'private session active', got %s", result.Reason)
	}
}

func TestPrivacyFilter_PIIDetection(t *testing.T) {
	settings := NewDefaultPrivacySettings("test-user")
	settings.AutoRedactPII = true

	detector := NewPIIDetector()
	filter := NewPrivacyFilter(settings, detector, &NoOpAuditLogger{})

	content := "Contact me at john@example.com for more info"
	result, err := filter.FilterBeforeShare(context.Background(), "workflow_pattern", content, "test-user")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Action != FilterActionRedact && result.Action != FilterActionAllow {
		t.Errorf("Expected redact or allow action, got %s", result.Action)
	}

	if len(result.PIIMatches) == 0 {
		t.Error("Expected PII matches to be recorded")
	}
}

func TestPrivacyFilter_HighSeverityPII(t *testing.T) {
	settings := NewDefaultPrivacySettings("test-user")
	settings.AutoRedactPII = true
	settings.RequireApproval = false

	detector := NewPIIDetector()
	filter := NewPrivacyFilter(settings, detector, &NoOpAuditLogger{})

	content := "API key: api_key=sk-1234567890abcdef1234567890abcdef"
	result, err := filter.FilterBeforeShare(context.Background(), "workflow_pattern", content, "test-user")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Action != FilterActionRedact {
		t.Errorf("Expected redact action for high-severity PII, got %s", result.Action)
	}

	if result.RedactedContent == "" {
		t.Error("Expected redacted content to be provided")
	}

	// Redacted content should not contain PII
	if detector.ContainsPII(result.RedactedContent) {
		t.Error("Redacted content still contains PII")
	}
}

func TestPrivacyFilter_RequireApproval(t *testing.T) {
	settings := NewDefaultPrivacySettings("test-user")
	settings.RequireApproval = true

	detector := NewPIIDetector()
	filter := NewPrivacyFilter(settings, detector, &NoOpAuditLogger{})

	content := "Clean content with no PII"
	result, err := filter.FilterBeforeShare(context.Background(), "workflow_pattern", content, "test-user")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Action != FilterActionPending {
		t.Errorf("Expected pending action, got %s", result.Action)
	}

	if !result.RequiresApproval {
		t.Error("Expected RequiresApproval to be true")
	}
}

func TestPrivacyFilter_AllowClean(t *testing.T) {
	settings := NewDefaultPrivacySettings("test-user")
	settings.RequireApproval = false

	detector := NewPIIDetector()
	filter := NewPrivacyFilter(settings, detector, &NoOpAuditLogger{})

	content := "This is a clean workflow pattern with no sensitive data"
	result, err := filter.FilterBeforeShare(context.Background(), "workflow_pattern", content, "test-user")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Action != FilterActionAllow {
		t.Errorf("Expected allow action, got %s", result.Action)
	}
}

func TestPrivacyFilter_SensitiveKeywords(t *testing.T) {
	settings := NewDefaultPrivacySettings("test-user")

	detector := NewPIIDetector()
	filter := NewPrivacyFilter(settings, detector, &NoOpAuditLogger{})

	content := "This is CONFIDENTIAL internal documentation"
	result, err := filter.FilterBeforeShare(context.Background(), "workflow_pattern", content, "test-user")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Action != FilterActionPending {
		t.Errorf("Expected pending action for sensitive keywords, got %s", result.Action)
	}
}

func TestPrivacyFilter_Batch(t *testing.T) {
	settings := NewDefaultPrivacySettings("test-user")

	detector := NewPIIDetector()
	filter := NewPrivacyFilter(settings, detector, &NoOpAuditLogger{})

	items := []string{
		"Clean content 1",
		"Clean content 2",
		"Email: test@example.com",
	}

	results, err := filter.FilterBatch(context.Background(), "workflow_pattern", items, "test-user")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(results) != len(items) {
		t.Errorf("Expected %d results, got %d", len(items), len(results))
	}
}

func TestPrivacyFilter_GetContentForSharing(t *testing.T) {
	settings := NewDefaultPrivacySettings("test-user")
	detector := NewPIIDetector()
	filter := NewPrivacyFilter(settings, detector, &NoOpAuditLogger{})

	testCases := []struct {
		name           string
		action         FilterAction
		original       string
		redacted       string
		expectedOutput string
	}{
		{
			name:           "Allow returns original",
			action:         FilterActionAllow,
			original:       "original content",
			redacted:       "",
			expectedOutput: "original content",
		},
		{
			name:           "Redact returns redacted",
			action:         FilterActionRedact,
			original:       "original content",
			redacted:       "redacted content",
			expectedOutput: "redacted content",
		},
		{
			name:           "Block returns empty",
			action:         FilterActionBlock,
			original:       "original content",
			redacted:       "",
			expectedOutput: "",
		},
		{
			name:           "Pending returns empty",
			action:         FilterActionPending,
			original:       "original content",
			redacted:       "",
			expectedOutput: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := &FilterResult{
				Action:          tc.action,
				RedactedContent: tc.redacted,
			}

			output := filter.GetContentForSharing(result, tc.original)
			if output != tc.expectedOutput {
				t.Errorf("Expected %q, got %q", tc.expectedOutput, output)
			}
		})
	}
}

func TestGenerateSummary(t *testing.T) {
	results := []*FilterResult{
		{Action: FilterActionAllow},
		{Action: FilterActionAllow},
		{Action: FilterActionBlock, Reason: "PII detected"},
		{Action: FilterActionBlock, Reason: "PII detected"},
		{Action: FilterActionPending},
		{Action: FilterActionRedact, PIIMatches: []PIIMatch{{Type: PIITypeEmail}}},
	}

	summary := GenerateSummary(results)

	if summary.TotalItems != 6 {
		t.Errorf("Expected 6 total items, got %d", summary.TotalItems)
	}

	if summary.Allowed != 2 {
		t.Errorf("Expected 2 allowed, got %d", summary.Allowed)
	}

	if summary.Blocked != 2 {
		t.Errorf("Expected 2 blocked, got %d", summary.Blocked)
	}

	if summary.Pending != 1 {
		t.Errorf("Expected 1 pending, got %d", summary.Pending)
	}

	if summary.Redacted != 1 {
		t.Errorf("Expected 1 redacted, got %d", summary.Redacted)
	}

	if summary.PIIDetected != 1 {
		t.Errorf("Expected 1 with PII detected, got %d", summary.PIIDetected)
	}
}

func TestPrivacyFilter_UpdateSettings(t *testing.T) {
	settings := NewDefaultPrivacySettings("test-user")
	detector := NewPIIDetector()
	filter := NewPrivacyFilter(settings, detector, &NoOpAuditLogger{})

	// Initial state
	if !filter.GetSettings().GlobalShareEnabled {
		t.Error("Expected GlobalShareEnabled to be true initially")
	}

	// Update settings
	newSettings := NewDefaultPrivacySettings("test-user")
	newSettings.GlobalShareEnabled = false
	filter.UpdateSettings(newSettings)

	// Verify update
	if filter.GetSettings().GlobalShareEnabled {
		t.Error("Expected GlobalShareEnabled to be false after update")
	}
}

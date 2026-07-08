package privacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// FilterAction represents the outcome of privacy filtering.
type FilterAction string

const (
	FilterActionAllow   FilterAction = "allow"   // Content can be shared
	FilterActionBlock   FilterAction = "block"   // Content must not be shared
	FilterActionPending FilterAction = "pending" // Content requires manual approval
	FilterActionRedact  FilterAction = "redact"  // Content can be shared after redaction
)

// FilterResult contains the outcome of privacy filtering.
type FilterResult struct {
	Action           FilterAction      `json:"action"`
	Reason           string            `json:"reason"`
	RedactedContent  string            `json:"redacted_content,omitempty"`
	PIIMatches       []PIIMatch        `json:"pii_matches,omitempty"`
	RequiresApproval bool              `json:"requires_approval"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// NewFilterResult creates a new filter result.
func NewFilterResult(action FilterAction, reason string) *FilterResult {
	return &FilterResult{
		Action:   action,
		Reason:   reason,
		Metadata: make(map[string]string),
	}
}

// PrivacyFilter implements the privacy filter pipeline.
type PrivacyFilter struct {
	Policy   *SharePolicy
	Detector *PIIDetector
	AuditLog AuditLogger
}

// AuditLogger interface for logging privacy decisions.
type AuditLogger interface {
	Log(ctx context.Context, entry *AuditLogEntry) error
}

// NewPrivacyFilter creates a new privacy filter.
func NewPrivacyFilter(settings *PrivacySettings, detector *PIIDetector, auditLog AuditLogger) *PrivacyFilter {
	return &PrivacyFilter{
		Policy:   NewSharePolicy(settings, detector),
		Detector: detector,
		AuditLog: auditLog,
	}
}

// FilterBeforeShare executes the privacy filter pipeline before sharing content.
// Pipeline order:
// 1. Global user opt-out check
// 2. Category-specific opt-out check
// 3. Private session mode check
// 4. PII detection (always runs)
// 5. User approval requirement check
func (f *PrivacyFilter) FilterBeforeShare(ctx context.Context, category string, content string, userID string) (*FilterResult, error) {
	settings := f.Policy.Settings

	// 1. Check global opt-out (highest priority)
	if !settings.GlobalShareEnabled {
		result := NewFilterResult(FilterActionBlock, "user opted out globally")
		f.logAudit(ctx, userID, category, result, content)
		return result, nil
	}

	// 2. Check category-specific opt-out
	if !settings.IsCategoryEnabled(category) {
		result := NewFilterResult(FilterActionBlock, "category opted out")
		f.logAudit(ctx, userID, category, result, content)
		return result, nil
	}

	// 3. Check private session flag
	if settings.PrivateSessionMode {
		result := NewFilterResult(FilterActionBlock, "private session active")
		f.logAudit(ctx, userID, category, result, content)
		return result, nil
	}

	// 4. PII detection (always runs)
	piiMatches := f.Detector.Detect(content)
	hasPII := len(piiMatches) > 0

	if hasPII {
		// Check if any high-severity PII detected
		hasHighSeverityPII := false
		for _, match := range piiMatches {
			if match.Severity == "high" {
				hasHighSeverityPII = true
				break
			}
		}

		// Block high-severity PII unless auto-redact is enabled
		if hasHighSeverityPII {
			if settings.AutoRedactPII {
				redactedContent := f.Detector.Redact(content)
				result := &FilterResult{
					Action:           FilterActionRedact,
					Reason:           "high-severity PII detected and redacted",
					RedactedContent:  redactedContent,
					PIIMatches:       piiMatches,
					RequiresApproval: settings.RequireApproval,
					Metadata:         make(map[string]string),
				}
				result.Metadata["pii_count"] = fmt.Sprintf("%d", len(piiMatches))
				f.logAudit(ctx, userID, category, result, content)

				// If approval required, mark as pending
				if settings.RequireApproval {
					result.Action = FilterActionPending
					result.Reason = "PII detected, requires approval after redaction"
				}

				return result, nil
			}

			// Block if auto-redact disabled
			result := &FilterResult{
				Action:           FilterActionBlock,
				Reason:           "high-severity PII detected",
				PIIMatches:       piiMatches,
				RequiresApproval: false,
				Metadata:         make(map[string]string),
			}
			result.Metadata["pii_count"] = fmt.Sprintf("%d", len(piiMatches))
			f.logAudit(ctx, userID, category, result, content)
			return result, nil
		}

		// Medium/low severity PII: log but allow with optional redaction
		if settings.AutoRedactPII {
			redactedContent := f.Detector.Redact(content)
			result := &FilterResult{
				Action:           FilterActionRedact,
				Reason:           "PII detected and redacted",
				RedactedContent:  redactedContent,
				PIIMatches:       piiMatches,
				RequiresApproval: settings.RequireApproval,
				Metadata:         make(map[string]string),
			}
			result.Metadata["pii_count"] = fmt.Sprintf("%d", len(piiMatches))
			f.logAudit(ctx, userID, category, result, content)

			if settings.RequireApproval {
				result.Action = FilterActionPending
			}

			return result, nil
		}
	}

	// 5. Check if manual approval required (based on policy)
	if settings.RequireApproval {
		result := &FilterResult{
			Action:           FilterActionPending,
			Reason:           "manual approval required by policy",
			RequiresApproval: true,
			Metadata:         make(map[string]string),
		}
		f.logAudit(ctx, userID, category, result, content)
		return result, nil
	}

	// Check for sensitive keywords
	if ContainsSensitiveKeywords(content) {
		result := &FilterResult{
			Action:           FilterActionPending,
			Reason:           "sensitive keywords detected",
			RequiresApproval: true,
			Metadata:         make(map[string]string),
		}
		result.Metadata["sensitive_keywords"] = "detected"
		f.logAudit(ctx, userID, category, result, content)
		return result, nil
	}

	// All checks passed - allow sharing
	result := NewFilterResult(FilterActionAllow, "passed all privacy checks")
	f.logAudit(ctx, userID, category, result, content)
	return result, nil
}

// logAudit logs the filter decision to the audit log.
func (f *PrivacyFilter) logAudit(ctx context.Context, userID, category string, result *FilterResult, content string) {
	if f.AuditLog == nil {
		return
	}

	entry := NewAuditLogEntry(userID, category, string(result.Action), result.Reason)
	entry.ContentHash = hashContent(content)

	if len(result.PIIMatches) > 0 {
		piiTypes := make([]string, 0, len(result.PIIMatches))
		for _, match := range result.PIIMatches {
			piiTypes = append(piiTypes, string(match.Type))
		}
		entry.PIIDetected = piiTypes
	}

	if err := f.AuditLog.Log(ctx, entry); err != nil {
		// Don't fail the filter operation if audit logging fails
		// Log to stderr or system logger
		fmt.Printf("Failed to log audit entry: %v\n", err)
	}
}

// hashContent creates a SHA-256 hash of content for audit trail.
func hashContent(content string) string {
	h := sha256.New()
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))[:16] // First 16 chars
}

// FilterBatch processes multiple items through the filter pipeline.
func (f *PrivacyFilter) FilterBatch(ctx context.Context, category string, items []string, userID string) ([]*FilterResult, error) {
	results := make([]*FilterResult, len(items))
	for i, item := range items {
		result, err := f.FilterBeforeShare(ctx, category, item, userID)
		if err != nil {
			return nil, fmt.Errorf("filter item %d: %w", i, err)
		}
		results[i] = result
	}
	return results, nil
}

// GetContentForSharing returns the content to share based on filter result.
// Returns empty string if content should not be shared.
func (f *PrivacyFilter) GetContentForSharing(result *FilterResult, originalContent string) string {
	switch result.Action {
	case FilterActionAllow:
		return originalContent
	case FilterActionRedact:
		if result.RedactedContent != "" {
			return result.RedactedContent
		}
		return ""
	case FilterActionBlock, FilterActionPending:
		return ""
	default:
		return ""
	}
}

// UpdateSettings updates the filter's privacy settings.
func (f *PrivacyFilter) UpdateSettings(settings *PrivacySettings) {
	f.Policy.Settings = settings
}

// GetSettings returns the current privacy settings.
func (f *PrivacyFilter) GetSettings() *PrivacySettings {
	return f.Policy.Settings
}

// NoOpAuditLogger is a no-op implementation of AuditLogger.
type NoOpAuditLogger struct{}

// Log does nothing.
func (l *NoOpAuditLogger) Log(ctx context.Context, entry *AuditLogEntry) error {
	return nil
}

// FilterSummary provides statistics about filtering decisions.
type FilterSummary struct {
	TotalItems     int            `json:"total_items"`
	Allowed        int            `json:"allowed"`
	Blocked        int            `json:"blocked"`
	Pending        int            `json:"pending"`
	Redacted       int            `json:"redacted"`
	PIIDetected    int            `json:"pii_detected"`
	BlockedReasons map[string]int `json:"blocked_reasons"`
}

// GenerateSummary creates a summary from filter results.
func GenerateSummary(results []*FilterResult) *FilterSummary {
	summary := &FilterSummary{
		TotalItems:     len(results),
		BlockedReasons: make(map[string]int),
	}

	for _, result := range results {
		switch result.Action {
		case FilterActionAllow:
			summary.Allowed++
		case FilterActionBlock:
			summary.Blocked++
			summary.BlockedReasons[result.Reason]++
		case FilterActionPending:
			summary.Pending++
		case FilterActionRedact:
			summary.Redacted++
		}

		if len(result.PIIMatches) > 0 {
			summary.PIIDetected++
		}
	}

	return summary
}

// SanitizeContentForLogging returns a safe version of content for logging.
func (f *PrivacyFilter) SanitizeContentForLogging(content string) string {
	return f.Detector.SanitizeForLogging(content)
}

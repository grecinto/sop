package privacy

import (
	"time"
)

// PolicyType defines the privacy policy enforcement level.
type PolicyType string

const (
	// PolicyStrict requires manual approval for everything.
	PolicyStrict PolicyType = "strict"

	// PolicyBalanced auto-shares safe categories with PII checks (default).
	PolicyBalanced PolicyType = "balanced"

	// PolicyOpen auto-shares all enabled categories with PII checks.
	PolicyOpen PolicyType = "open"
)

// PrivacySettings holds user privacy preferences for fleet learning.
type PrivacySettings struct {
	UserID               string                  `json:"user_id"`
	GlobalShareEnabled   bool                    `json:"global_share_enabled"`
	PolicyType           PolicyType              `json:"policy_type"`
	CategoryPreferences  map[string]bool         `json:"category_preferences"`
	PrivateSessionMode   bool                    `json:"private_session_mode"`
	RequireApproval      bool                    `json:"require_approval"`
	AutoRedactPII        bool                    `json:"auto_redact_pii"`
	CustomPIIPatterns    map[string]string       `json:"custom_pii_patterns,omitempty"`
	CreatedAt            time.Time               `json:"created_at"`
	UpdatedAt            time.Time               `json:"updated_at"`
}

// NewDefaultPrivacySettings creates default privacy settings for a user.
// Default: opt-in by default with balanced policy.
func NewDefaultPrivacySettings(userID string) *PrivacySettings {
	return &PrivacySettings{
		UserID:             userID,
		GlobalShareEnabled: true, // Opt-in by default
		PolicyType:         PolicyBalanced,
		CategoryPreferences: map[string]bool{
			"workflow_pattern":         true,  // enabled
			"anti_pattern":             true,  // enabled
			"repair_strategy":          true,  // enabled
			"tool_preference":          true,  // enabled
			"performance_optimization": true,  // enabled
			"testing_strategy":         true,  // enabled
			"error_resolution":         true,  // enabled
			"security_pattern":         false, // disabled (sensitive)
			"code_quality":             true,  // enabled
			"api_design":               true,  // enabled
			"safety_heuristic":         true,  // enabled
			"verified_playbook":        true,  // enabled
			"context_gathering":        true,  // enabled
		},
		PrivateSessionMode: false,
		RequireApproval:    false,
		AutoRedactPII:      true,
		CustomPIIPatterns:  make(map[string]string),
		CreatedAt:          time.Now().UTC(),
		UpdatedAt:          time.Now().UTC(),
	}
}

// IsCategoryEnabled checks if a category is enabled for sharing.
func (s *PrivacySettings) IsCategoryEnabled(category string) bool {
	if s.CategoryPreferences == nil {
		return false
	}
	enabled, exists := s.CategoryPreferences[category]
	if !exists {
		return false // Default to disabled if not configured
	}
	return enabled
}

// SetCategoryEnabled enables or disables a category.
func (s *PrivacySettings) SetCategoryEnabled(category string, enabled bool) {
	if s.CategoryPreferences == nil {
		s.CategoryPreferences = make(map[string]bool)
	}
	s.CategoryPreferences[category] = enabled
	s.UpdatedAt = time.Now().UTC()
}

// ShouldShare determines if content should be shared based on global setting.
func (s *PrivacySettings) ShouldShare() bool {
	return s.GlobalShareEnabled && !s.PrivateSessionMode
}

// ApplyStrictPolicy configures strict privacy policy.
func (s *PrivacySettings) ApplyStrictPolicy() {
	s.PolicyType = PolicyStrict
	s.RequireApproval = true
	s.AutoRedactPII = true
	s.UpdatedAt = time.Now().UTC()
}

// ApplyBalancedPolicy configures balanced privacy policy (default).
func (s *PrivacySettings) ApplyBalancedPolicy() {
	s.PolicyType = PolicyBalanced
	s.RequireApproval = false
	s.AutoRedactPII = true
	s.UpdatedAt = time.Now().UTC()
}

// ApplyOpenPolicy configures open privacy policy.
func (s *PrivacySettings) ApplyOpenPolicy() {
	s.PolicyType = PolicyOpen
	s.RequireApproval = false
	s.AutoRedactPII = true
	s.UpdatedAt = time.Now().UTC()
}

// SharePolicy encapsulates sharing decision logic.
type SharePolicy struct {
	Settings *PrivacySettings
	Detector *PIIDetector
}

// NewSharePolicy creates a new share policy with settings and detector.
func NewSharePolicy(settings *PrivacySettings, detector *PIIDetector) *SharePolicy {
	return &SharePolicy{
		Settings: settings,
		Detector: detector,
	}
}

// ShouldShareCategory checks if a specific category should be shared.
func (p *SharePolicy) ShouldShareCategory(category string) bool {
	if !p.Settings.ShouldShare() {
		return false
	}
	return p.Settings.IsCategoryEnabled(category)
}

// GetDefaultCategories returns the default category preferences.
func GetDefaultCategories() map[string]bool {
	return map[string]bool{
		"workflow_pattern":         true,
		"anti_pattern":             true,
		"repair_strategy":          true,
		"tool_preference":          true,
		"performance_optimization": true,
		"testing_strategy":         true,
		"error_resolution":         true,
		"security_pattern":         false, // disabled by default
		"code_quality":             true,
		"api_design":               true,
		"safety_heuristic":         true,
		"verified_playbook":        true,
		"context_gathering":        true,
	}
}

// AuditLogEntry records a privacy filter decision.
type AuditLogEntry struct {
	Timestamp    time.Time         `json:"timestamp"`
	UserID       string            `json:"user_id"`
	Category     string            `json:"category"`
	Action       string            `json:"action"` // "allowed", "blocked", "pending", "redacted"
	Reason       string            `json:"reason"`
	ContentHash  string            `json:"content_hash"`
	PIIDetected  []string          `json:"pii_detected,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// NewAuditLogEntry creates a new audit log entry.
func NewAuditLogEntry(userID, category, action, reason string) *AuditLogEntry {
	return &AuditLogEntry{
		Timestamp:   time.Now().UTC(),
		UserID:      userID,
		Category:    category,
		Action:      action,
		Reason:      reason,
		Metadata:    make(map[string]string),
	}
}

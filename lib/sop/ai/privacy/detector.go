package privacy

import (
	"regexp"
	"strings"
)

// PIIDetector detects personally identifiable information in text.
type PIIDetector struct {
	patterns map[PIIType]*regexp.Regexp
}

// PIIType represents different types of PII.
type PIIType string

const (
	PIITypeEmail       PIIType = "email"
	PIITypePhone       PIIType = "phone"
	PIITypeSSN         PIIType = "ssn"
	PIITypeIPAddress   PIIType = "ip_address"
	PIITypeAPIKey      PIIType = "api_key"
	PIITypeCreditCard  PIIType = "credit_card"
	PIITypeAWSKey      PIIType = "aws_key"
	PIITypeGitHubToken PIIType = "github_token"
	PIITypePrivateKey  PIIType = "private_key"
	PIITypePassword    PIIType = "password"
	PIITypeBearerToken PIIType = "bearer_token"
	PIITypeJWT         PIIType = "jwt"
)

// PIIMatch represents a detected PII instance.
type PIIMatch struct {
	Type     PIIType
	Value    string
	Start    int
	End      int
	Severity string // "high", "medium", "low"
}

// NewPIIDetector creates a new PII detector with default patterns.
func NewPIIDetector() *PIIDetector {
	return &PIIDetector{
		patterns: map[PIIType]*regexp.Regexp{
			// Email pattern
			PIITypeEmail: regexp.MustCompile(`(?i)\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`),

			// Phone patterns (US, international formats)
			PIITypePhone: regexp.MustCompile(`(?i)(?:\+?1[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`),

			// SSN pattern (XXX-XX-XXXX)
			PIITypeSSN: regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),

			// IP address (IPv4)
			PIITypeIPAddress: regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`),

			// Generic API key patterns
			PIITypeAPIKey: regexp.MustCompile(`(?i)(?:api[_-]?key|apikey|api[_-]?secret)[=:\s]+["']?([a-zA-Z0-9_\-]{16,})["']?`),

			// Credit card patterns (basic Luhn algorithm would be better)
			PIITypeCreditCard: regexp.MustCompile(`\b(?:\d{4}[-\s]?){3}\d{4}\b`),

			// AWS Access Key ID pattern
			PIITypeAWSKey: regexp.MustCompile(`(?i)(?:AKIA|ASIA)[A-Z0-9]{16}`),

			// GitHub token patterns
			PIITypeGitHubToken: regexp.MustCompile(`(?i)(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{36,}`),

			// Private key headers
			PIITypePrivateKey: regexp.MustCompile(`(?i)-----BEGIN\s+(?:RSA|EC|OPENSSH|PGP)\s+PRIVATE\s+KEY-----`),

			// Password in assignments or configs
			PIITypePassword: regexp.MustCompile(`(?i)(?:password|passwd|pwd)[=:\s]+["']?([^\s"']{6,})["']?`),

			// Bearer tokens
			PIITypeBearerToken: regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9\-._~+/]+=*`),

			// JWT pattern (three base64 segments separated by dots)
			PIITypeJWT: regexp.MustCompile(`\beyJ[a-zA-Z0-9_-]+\.eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\b`),
		},
	}
}

// AddCustomPattern adds a custom PII detection pattern.
func (d *PIIDetector) AddCustomPattern(piiType PIIType, pattern *regexp.Regexp) {
	d.patterns[piiType] = pattern
}

// Detect finds all PII matches in the given text.
func (d *PIIDetector) Detect(text string) []PIIMatch {
	var matches []PIIMatch

	for piiType, pattern := range d.patterns {
		found := pattern.FindAllStringIndex(text, -1)
		for _, loc := range found {
			value := text[loc[0]:loc[1]]
			matches = append(matches, PIIMatch{
				Type:     piiType,
				Value:    value,
				Start:    loc[0],
				End:      loc[1],
				Severity: d.getSeverity(piiType),
			})
		}
	}

	// Sort by start position
	sortMatches(matches)

	return matches
}

// ContainsPII checks if text contains any PII without returning matches.
func (d *PIIDetector) ContainsPII(text string) bool {
	for _, pattern := range d.patterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

// Redact replaces detected PII with placeholder tokens.
func (d *PIIDetector) Redact(text string) string {
	matches := d.Detect(text)
	if len(matches) == 0 {
		return text
	}

	// Build redacted string by replacing matches
	result := []rune(text)
	offset := 0

	for _, match := range matches {
		placeholder := d.getPlaceholder(match.Type)
		start := match.Start + offset
		end := match.End + offset

		// Replace match with placeholder
		before := string(result[:start])
		after := string(result[end:])
		result = []rune(before + placeholder + after)

		// Adjust offset for next replacement
		offset += len([]rune(placeholder)) - (match.End - match.Start)
	}

	return string(result)
}

// getSeverity returns the severity level for a PII type.
func (d *PIIDetector) getSeverity(piiType PIIType) string {
	switch piiType {
	case PIITypeSSN, PIITypeCreditCard, PIITypePrivateKey, PIITypePassword:
		return "high"
	case PIITypeAPIKey, PIITypeAWSKey, PIITypeGitHubToken, PIITypeBearerToken, PIITypeJWT:
		return "high"
	case PIITypeEmail, PIITypePhone:
		return "medium"
	case PIITypeIPAddress:
		return "low"
	default:
		return "medium"
	}
}

// getPlaceholder returns a placeholder token for a PII type.
func (d *PIIDetector) getPlaceholder(piiType PIIType) string {
	switch piiType {
	case PIITypeEmail:
		return "[EMAIL]"
	case PIITypePhone:
		return "[PHONE]"
	case PIITypeSSN:
		return "[SSN]"
	case PIITypeIPAddress:
		return "[IP_ADDRESS]"
	case PIITypeAPIKey:
		return "[API_KEY]"
	case PIITypeCreditCard:
		return "[CREDIT_CARD]"
	case PIITypeAWSKey:
		return "[AWS_KEY]"
	case PIITypeGitHubToken:
		return "[GITHUB_TOKEN]"
	case PIITypePrivateKey:
		return "[PRIVATE_KEY]"
	case PIITypePassword:
		return "[PASSWORD]"
	case PIITypeBearerToken:
		return "[BEARER_TOKEN]"
	case PIITypeJWT:
		return "[JWT_TOKEN]"
	default:
		return "[REDACTED]"
	}
}

// GetHighSeverityTypes returns PII types considered high severity.
func (d *PIIDetector) GetHighSeverityTypes() []PIIType {
	return []PIIType{
		PIITypeSSN,
		PIITypeCreditCard,
		PIITypePrivateKey,
		PIITypePassword,
		PIITypeAPIKey,
		PIITypeAWSKey,
		PIITypeGitHubToken,
		PIITypeBearerToken,
		PIITypeJWT,
	}
}

// GetMatchSummary returns a summary of PII matches by type.
func (d *PIIDetector) GetMatchSummary(matches []PIIMatch) map[PIIType]int {
	summary := make(map[PIIType]int)
	for _, match := range matches {
		summary[match.Type]++
	}
	return summary
}

// sortMatches sorts PIIMatch slice by start position.
func sortMatches(matches []PIIMatch) {
	for i := 0; i < len(matches)-1; i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[i].Start > matches[j].Start {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}
}

// SanitizeForLogging returns a version of text safe for logging (all PII redacted).
func (d *PIIDetector) SanitizeForLogging(text string) string {
	// Use redaction but truncate if too long
	redacted := d.Redact(text)
	if len(redacted) > 200 {
		return redacted[:200] + "..."
	}
	return redacted
}

// ContainsSensitiveKeywords checks for sensitive context keywords.
func ContainsSensitiveKeywords(text string) bool {
	lower := strings.ToLower(text)
	sensitiveKeywords := []string{
		"confidential",
		"secret",
		"internal only",
		"do not share",
		"proprietary",
		"classified",
		"restricted",
		"private",
		"ssn",
		"social security",
		"credit card",
		"password",
		"authentication",
		"customer data",
		"patient record",
		"medical record",
		"financial data",
	}

	for _, keyword := range sensitiveKeywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

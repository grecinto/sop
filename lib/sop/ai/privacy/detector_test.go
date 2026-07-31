package privacy

import (
	"regexp"
	"testing"
)

func TestPIIDetector_DetectEmail(t *testing.T) {
	detector := NewPIIDetector()

	testCases := []struct {
		name     string
		text     string
		expected bool
	}{
		{"Valid email", "Contact me at john.doe@example.com", true},
		{"No email", "This is just plain text", false},
		{"Multiple emails", "Send to alice@example.com or bob@test.org", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := detector.ContainsPII(tc.text)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v for text: %s", tc.expected, result, tc.text)
			}
		})
	}
}

func TestPIIDetector_DetectAPIKey(t *testing.T) {
	detector := NewPIIDetector()

	text := `export API_KEY="sk-1234567890abcdef1234567890abcdef"`
	matches := detector.Detect(text)

	if len(matches) == 0 {
		t.Fatal("Expected to detect API key")
	}

	found := false
	for _, match := range matches {
		if match.Type == PIITypeAPIKey {
			found = true
			if match.Severity != "high" {
				t.Errorf("Expected high severity for API key, got %s", match.Severity)
			}
		}
	}

	if !found {
		t.Error("API key not detected in matches")
	}
}

func TestPIIDetector_DetectAWSKey(t *testing.T) {
	detector := NewPIIDetector()

	text := "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE"
	matches := detector.Detect(text)

	found := false
	for _, match := range matches {
		if match.Type == PIITypeAWSKey {
			found = true
		}
	}

	if !found {
		t.Error("AWS key not detected")
	}
}

func TestPIIDetector_DetectGitHubToken(t *testing.T) {
	detector := NewPIIDetector()

	text := "token: ghp_1234567890abcdefghijklmnopqrstuvwxyz"
	matches := detector.Detect(text)

	found := false
	for _, match := range matches {
		if match.Type == PIITypeGitHubToken {
			found = true
		}
	}

	if !found {
		t.Error("GitHub token not detected")
	}
}

func TestPIIDetector_DetectSSN(t *testing.T) {
	detector := NewPIIDetector()

	text := "Patient SSN: 123-45-6789"
	matches := detector.Detect(text)

	found := false
	for _, match := range matches {
		if match.Type == PIITypeSSN {
			found = true
			if match.Severity != "high" {
				t.Errorf("Expected high severity for SSN, got %s", match.Severity)
			}
		}
	}

	if !found {
		t.Error("SSN not detected")
	}
}

func TestPIIDetector_DetectIPAddress(t *testing.T) {
	detector := NewPIIDetector()

	text := "Server IP: 192.168.1.100"
	matches := detector.Detect(text)

	found := false
	for _, match := range matches {
		if match.Type == PIITypeIPAddress {
			found = true
		}
	}

	if !found {
		t.Error("IP address not detected")
	}
}

func TestPIIDetector_Redact(t *testing.T) {
	detector := NewPIIDetector()

	text := "Email me at john@example.com or call 555-123-4567"
	redacted := detector.Redact(text)

	if redacted == text {
		t.Error("Text was not redacted")
	}

	if detector.ContainsPII(redacted) {
		t.Error("Redacted text still contains PII")
	}

	// Should contain placeholders
	if len(redacted) == 0 {
		t.Error("Redacted text is empty")
	}
}

func TestPIIDetector_GetMatchSummary(t *testing.T) {
	detector := NewPIIDetector()

	text := `
		Contact: john@example.com
		Phone: 555-123-4567
		API Key: api_key=sk-1234567890abcdef1234567890abcdef
		IP: 192.168.1.1
	`

	matches := detector.Detect(text)
	summary := detector.GetMatchSummary(matches)

	if len(summary) == 0 {
		t.Fatal("Expected non-empty summary")
	}

	expectedTypes := []PIIType{PIITypeEmail, PIITypePhone, PIITypeAPIKey, PIITypeIPAddress}
	for _, expectedType := range expectedTypes {
		if count, ok := summary[expectedType]; !ok || count == 0 {
			t.Errorf("Expected to find %s in summary", expectedType)
		}
	}
}

func TestPIIDetector_SanitizeForLogging(t *testing.T) {
	detector := NewPIIDetector()

	text := "Secret API key: sk-1234567890abcdef and email john@example.com"
	sanitized := detector.SanitizeForLogging(text)

	if sanitized == text {
		t.Error("Text was not sanitized")
	}

	if detector.ContainsPII(sanitized) {
		t.Error("Sanitized text still contains PII")
	}
}

func TestContainsSensitiveKeywords(t *testing.T) {
	testCases := []struct {
		name     string
		text     string
		expected bool
	}{
		{"Contains confidential", "This is CONFIDENTIAL information", true},
		{"Contains secret", "Secret data here", true},
		{"Contains password", "User password: xxx", true},
		{"Contains SSN", "SSN required", true},
		{"No keywords", "This is public information", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ContainsSensitiveKeywords(tc.text)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v for text: %s", tc.expected, result, tc.text)
			}
		})
	}
}

func TestPIIDetector_AddCustomPattern(t *testing.T) {
	detector := NewPIIDetector()

	// Add custom pattern for internal employee IDs (format: EMP-12345)
	customPattern := regexp.MustCompile(`\bEMP-\d{5}\b`)
	detector.AddCustomPattern("employee_id", customPattern)

	text := "Employee ID: EMP-12345"
	matches := detector.Detect(text)

	found := false
	for _, match := range matches {
		if match.Type == "employee_id" {
			found = true
		}
	}

	if !found {
		t.Error("Custom pattern not detected")
	}
}

func TestPIIDetector_PrivateKey(t *testing.T) {
	detector := NewPIIDetector()

	text := `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA...
-----END RSA PRIVATE KEY-----`

	matches := detector.Detect(text)

	found := false
	for _, match := range matches {
		if match.Type == PIITypePrivateKey {
			found = true
			if match.Severity != "high" {
				t.Errorf("Expected high severity for private key, got %s", match.Severity)
			}
		}
	}

	if !found {
		t.Error("Private key not detected")
	}
}

func TestPIIDetector_BearerToken(t *testing.T) {
	detector := NewPIIDetector()

	text := "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"
	matches := detector.Detect(text)

	found := false
	for _, match := range matches {
		if match.Type == PIITypeBearerToken {
			found = true
		}
	}

	if !found {
		t.Error("Bearer token not detected")
	}
}

func TestPIIDetector_JWT(t *testing.T) {
	detector := NewPIIDetector()

	text := "token: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	matches := detector.Detect(text)

	found := false
	for _, match := range matches {
		if match.Type == PIITypeJWT {
			found = true
		}
	}

	if !found {
		t.Error("JWT not detected")
	}
}

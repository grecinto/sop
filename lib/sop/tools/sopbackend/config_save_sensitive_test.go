package sopbackend

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveConfig_OmitsSensitiveAISelectionFields(t *testing.T) {
	t.Setenv("SOP_CONFIG_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	defer func() {
		config = Config{}
	}()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	config = Config{
		ConfigFile: configFile,
		Port:       8080,
	}

	if err := saveConfig(); err != nil {
		t.Fatalf("saveConfig returned error: %v", err)
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}

	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("unmarshal saved config: %v", err)
	}

	for _, key := range []string{
		"llm_api_key",
		"brain_provider", "brain_model", "brain_url", "brain_api_key",
		"embedder_provider", "embedder_model", "embedder_url", "embedder_api_key",
	} {
		if _, ok := saved[key]; ok {
			t.Fatalf("expected %q to be omitted from config.json, but it was present", key)
		}
	}

	if got := saved["port"]; got != float64(8080) {
		t.Fatalf("expected non-sensitive config fields to remain, got %v", got)
	}
}

func TestSaveConfig_EncryptsSensitiveFieldsAndRestoresThemOnLoad(t *testing.T) {
	t.Setenv("SOP_CONFIG_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")

	defer func() {
		config = Config{}
	}()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	config = Config{
		ConfigFile:    configFile,
		Port:          8080,
		SessionSecret: SecuredString("super-secret-session"),
		Users: []UserRecord{{
			Username:         "root",
			PasswordHash:     SecuredString("bcrypt-hash"),
			Email:            SecuredString("root@example.com"),
			StripeCustomerID: SecuredString("cus_123"),
			StripeSubID:      SecuredString("sub_123"),
		}},
	}

	if err := saveConfig(); err != nil {
		t.Fatalf("saveConfig returned error: %v", err)
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	fileText := string(data)
	for _, needle := range []string{"super-secret-session", "bcrypt-hash", "root@example.com", "cus_123", "sub_123"} {
		if strings.Contains(fileText, needle) {
			t.Fatalf("expected encrypted config file to not contain plaintext %q, got %s", needle, fileText)
		}
	}

	var loaded Config
	if err := loadConfigInto(configFile, &loaded); err != nil {
		t.Fatalf("loadConfigInto returned error: %v", err)
	}

	if got := loaded.SessionSecret.Plaintext(); got != "super-secret-session" {
		t.Fatalf("expected session secret to be restored, got %q", got)
	}
	if got := loaded.Users[0].PasswordHash.Plaintext(); got != "bcrypt-hash" {
		t.Fatalf("expected password hash to be restored, got %q", got)
	}
	if got := loaded.Users[0].Email.Plaintext(); got != "root@example.com" {
		t.Fatalf("expected email to be restored, got %q", got)
	}
	if got := loaded.Users[0].StripeCustomerID.Plaintext(); got != "cus_123" {
		t.Fatalf("expected stripe customer id to be restored, got %q", got)
	}
	if got := loaded.Users[0].StripeSubID.Plaintext(); got != "sub_123" {
		t.Fatalf("expected stripe subscription id to be restored, got %q", got)
	}
}

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/sharedcode/sop/ai/privacy"
)

var (
	globalPrivacySettings   *privacy.PrivacySettings
	globalPrivacySettingsMu sync.RWMutex
	privacyConfigPath       string
)

// initPrivacyConfig initializes privacy configuration for the daemon.
func initPrivacyConfig(daemonDataDir string) error {
	privacyConfigPath = filepath.Join(daemonDataDir, "privacy_settings.json")

	// Load existing settings or create defaults
	settings, err := loadPrivacySettings()
	if err != nil {
		// Create default settings for first-time use
		settings = privacy.NewDefaultPrivacySettings("daemon-user")
		if err := savePrivacySettings(settings); err != nil {
			return fmt.Errorf("failed to save default privacy settings: %w", err)
		}
	}

	globalPrivacySettingsMu.Lock()
	globalPrivacySettings = settings
	globalPrivacySettingsMu.Unlock()

	return nil
}

// loadPrivacySettings loads privacy settings from disk.
func loadPrivacySettings() (*privacy.PrivacySettings, error) {
	data, err := os.ReadFile(privacyConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to read privacy settings: %w", err)
	}

	var settings privacy.PrivacySettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("failed to parse privacy settings: %w", err)
	}

	return &settings, nil
}

// savePrivacySettings saves privacy settings to disk.
func savePrivacySettings(settings *privacy.PrivacySettings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal privacy settings: %w", err)
	}

	if err := os.WriteFile(privacyConfigPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write privacy settings: %w", err)
	}

	return nil
}

// getPrivacySettings returns the current privacy settings (thread-safe).
func getPrivacySettings() *privacy.PrivacySettings {
	globalPrivacySettingsMu.RLock()
	defer globalPrivacySettingsMu.RUnlock()
	return globalPrivacySettings
}

// updatePrivacySettings updates privacy settings and persists to disk.
func updatePrivacySettings(settings *privacy.PrivacySettings) error {
	globalPrivacySettingsMu.Lock()
	globalPrivacySettings = settings
	globalPrivacySettingsMu.Unlock()

	return savePrivacySettings(settings)
}

// handleGetPrivacySettings returns current privacy settings (API endpoint).
func handleGetPrivacySettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	settings := getPrivacySettings()
	if settings == nil {
		http.Error(w, "Privacy settings not initialized", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

// handleUpdatePrivacySettings updates privacy settings (API endpoint).
func handleUpdatePrivacySettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var settings privacy.PrivacySettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := updatePrivacySettings(&settings); err != nil {
		http.Error(w, fmt.Sprintf("Failed to update settings: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// handleTogglePrivateSession toggles private session mode (API endpoint).
func handleTogglePrivateSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	settings := getPrivacySettings()
	if settings == nil {
		http.Error(w, "Privacy settings not initialized", http.StatusInternalServerError)
		return
	}

	settings.PrivateSessionMode = req.Enabled
	if err := updatePrivacySettings(settings); err != nil {
		http.Error(w, fmt.Sprintf("Failed to update settings: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":              "updated",
		"private_session_mode": req.Enabled,
	})
}

// handleSetCategoryPreference sets sharing preference for a category (API endpoint).
func handleSetCategoryPreference(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Category string `json:"category"`
		Enabled  bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Category == "" {
		http.Error(w, "category is required", http.StatusBadRequest)
		return
	}

	settings := getPrivacySettings()
	if settings == nil {
		http.Error(w, "Privacy settings not initialized", http.StatusInternalServerError)
		return
	}

	settings.SetCategoryEnabled(req.Category, req.Enabled)
	if err := updatePrivacySettings(settings); err != nil {
		http.Error(w, fmt.Sprintf("Failed to update settings: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "updated",
		"category": req.Category,
		"enabled":  req.Enabled,
	})
}

package memory

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestKBQualityAPI_HandleReport_JSON(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForAPI(t, tmpDir)

	// Add test data
	recordOutcomes(tracker, "test-kb", "implementation", []bool{
		true, true, true, false, false,
		true, true, true, false, false,
	})

	api := NewKBQualityAPI(tracker, DefaultPruningThresholds())

	req := httptest.NewRequest(http.MethodGet, "/api/kb/quality/report", nil)
	w := httptest.NewRecorder()

	api.HandleReport(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Error("Expected JSON content type")
	}

	var report PruningReport
	if err := json.NewDecoder(w.Body).Decode(&report); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if report.TotalKBs == 0 {
		t.Error("Expected report to contain KB data")
	}
}

func TestKBQualityAPI_HandleReport_Text(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForAPI(t, tmpDir)

	recordOutcomes(tracker, "test-kb", "implementation", []bool{
		true, true, true, false, false,
		true, true, true, false, false,
	})

	api := NewKBQualityAPI(tracker, DefaultPruningThresholds())

	req := httptest.NewRequest(http.MethodGet, "/api/kb/quality/report?format=text", nil)
	w := httptest.NewRecorder()

	api.HandleReport(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if !strings.Contains(w.Header().Get("Content-Type"), "text/plain") {
		t.Error("Expected text/plain content type")
	}

	body := w.Body.String()
	if !strings.Contains(body, "KB Quality") {
		t.Error("Expected report to contain title")
	}
}

func TestKBQualityAPI_HandleAnalyze(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForAPI(t, tmpDir)

	// Add critical and warning performers
	recordOutcomes(tracker, "critical-kb", "implementation", []bool{
		false, false, false, false, false,
		false, false, false, false, false,
	})
	recordOutcomes(tracker, "warning-kb", "implementation", []bool{
		true, false, true, false, false,
		true, false, false, true, false,
	})

	api := NewKBQualityAPI(tracker, DefaultPruningThresholds())

	req := httptest.NewRequest(http.MethodGet, "/api/kb/quality/analyze", nil)
	w := httptest.NewRecorder()

	api.HandleAnalyze(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if _, ok := response["poor_performers"]; !ok {
		t.Error("Expected poor_performers in response")
	}
}

func TestKBQualityAPI_HandleAnalyze_FilterBySeverity(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForAPI(t, tmpDir)

	recordOutcomes(tracker, "critical-kb", "implementation", []bool{
		false, false, false, false, false,
		false, false, false, false, false,
	})

	api := NewKBQualityAPI(tracker, DefaultPruningThresholds())

	req := httptest.NewRequest(http.MethodGet, "/api/kb/quality/analyze?severity=critical", nil)
	w := httptest.NewRecorder()

	api.HandleAnalyze(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	json.NewDecoder(w.Body).Decode(&response)

	performers := response["poor_performers"].([]interface{})
	if len(performers) == 0 {
		t.Error("Expected at least one critical performer")
	}
}

func TestKBQualityAPI_HandlePruningCandidates(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForAPI(t, tmpDir)

	recordOutcomes(tracker, "critical-kb", "implementation", []bool{
		false, false, false, false, false,
		false, false, false, false, false,
	})

	api := NewKBQualityAPI(tracker, DefaultPruningThresholds())

	req := httptest.NewRequest(http.MethodGet, "/api/kb/quality/candidates", nil)
	w := httptest.NewRecorder()

	api.HandlePruningCandidates(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var candidates []PruningCandidate
	if err := json.NewDecoder(w.Body).Decode(&candidates); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(candidates) == 0 {
		t.Error("Expected at least one pruning candidate")
	}
}

func TestKBQualityAPI_HandleListAll(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForAPI(t, tmpDir)

	recordOutcomes(tracker, "kb1", "implementation", []bool{true, true, true, true, true, true, true, true, true, true})
	recordOutcomes(tracker, "kb2", "debugging", []bool{true, true, false, false, true, true, false, true, true, true})

	api := NewKBQualityAPI(tracker, DefaultPruningThresholds())

	req := httptest.NewRequest(http.MethodGet, "/api/kb/effectiveness/all", nil)
	w := httptest.NewRecorder()

	api.HandleListAll(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var records []KBEffectiveness
	if err := json.NewDecoder(w.Body).Decode(&records); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(records) != 2 {
		t.Errorf("Expected 2 records, got %d", len(records))
	}
}

func TestKBQualityAPI_HandleBestKBs(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForAPI(t, tmpDir)

	recordOutcomes(tracker, "kb1", "implementation", []bool{true, true, true, true, true, true, true, true, true, true})
	recordOutcomes(tracker, "kb2", "implementation", []bool{true, true, false, false, true, true, false, true, true, true})

	api := NewKBQualityAPI(tracker, DefaultPruningThresholds())

	req := httptest.NewRequest(http.MethodGet, "/api/kb/effectiveness/best?task_type=implementation&limit=5", nil)
	w := httptest.NewRecorder()

	api.HandleBestKBs(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var rankings []KBRanking
	if err := json.NewDecoder(w.Body).Decode(&rankings); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(rankings) == 0 {
		t.Error("Expected at least one ranking")
	}
}

func TestKBQualityAPI_HandleBestKBs_MissingTaskType(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForAPI(t, tmpDir)

	api := NewKBQualityAPI(tracker, DefaultPruningThresholds())

	req := httptest.NewRequest(http.MethodGet, "/api/kb/effectiveness/best", nil)
	w := httptest.NewRecorder()

	api.HandleBestKBs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestKBQualityAPI_HandlePoorPerformers(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForAPI(t, tmpDir)

	recordOutcomes(tracker, "poor-kb", "implementation", []bool{
		false, false, false, false, false,
		false, false, false, false, false,
	})

	api := NewKBQualityAPI(tracker, DefaultPruningThresholds())

	req := httptest.NewRequest(http.MethodGet, "/api/kb/effectiveness/poor?min_uses=5&max_success_rate=0.50", nil)
	w := httptest.NewRecorder()

	api.HandlePoorPerformers(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var performers []KBEffectiveness
	if err := json.NewDecoder(w.Body).Decode(&performers); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(performers) == 0 {
		t.Error("Expected at least one poor performer")
	}
}

func TestKBQualityAPI_HandleKBDetails(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForAPI(t, tmpDir)

	recordOutcomes(tracker, "specific-kb", "implementation", []bool{
		true, true, true, false, false,
		true, true, true, false, false,
	})

	api := NewKBQualityAPI(tracker, DefaultPruningThresholds())

	req := httptest.NewRequest(http.MethodGet, "/api/kb/effectiveness/kb?task_type=implementation&kb_name=specific-kb", nil)
	w := httptest.NewRecorder()

	api.HandleKBDetails(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var effectiveness KBEffectiveness
	if err := json.NewDecoder(w.Body).Decode(&effectiveness); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if effectiveness.KBName != "specific-kb" {
		t.Errorf("Expected KB name 'specific-kb', got '%s'", effectiveness.KBName)
	}
}

func TestKBQualityAPI_HandleKBDetails_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForAPI(t, tmpDir)

	api := NewKBQualityAPI(tracker, DefaultPruningThresholds())

	req := httptest.NewRequest(http.MethodGet, "/api/kb/effectiveness/kb?task_type=implementation&kb_name=nonexistent", nil)
	w := httptest.NewRecorder()

	api.HandleKBDetails(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestKBQualityAPI_HandleTriggerCheck_NoJob(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForAPI(t, tmpDir)

	api := NewKBQualityAPI(tracker, DefaultPruningThresholds())

	req := httptest.NewRequest(http.MethodPost, "/api/kb/quality/trigger", nil)
	w := httptest.NewRecorder()

	api.HandleTriggerCheck(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", w.Code)
	}
}

func TestKBQualityAPI_HandleJobStatus_NoJob(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForAPI(t, tmpDir)

	api := NewKBQualityAPI(tracker, DefaultPruningThresholds())

	req := httptest.NewRequest(http.MethodGet, "/api/kb/quality/status", nil)
	w := httptest.NewRecorder()

	api.HandleJobStatus(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", w.Code)
	}
}

func TestKBQualityAPI_RegisterRoutes(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForAPI(t, tmpDir)

	api := NewKBQualityAPI(tracker, DefaultPruningThresholds())
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	// Test that routes are registered by making a request
	req := httptest.NewRequest(http.MethodGet, "/api/kb/effectiveness/all", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected route to be registered, got status %d", w.Code)
	}
}

func TestKBQualityAPI_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForAPI(t, tmpDir)

	api := NewKBQualityAPI(tracker, DefaultPruningThresholds())

	// Try POST on GET-only endpoint
	req := httptest.NewRequest(http.MethodPost, "/api/kb/quality/report", nil)
	w := httptest.NewRecorder()

	api.HandleReport(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

// Helper function to set up test tracker
func setupTestTrackerForAPI(t *testing.T, tmpDir string) *KBEffectivenessTracker {
	t.Helper()
	persistPath := filepath.Join(tmpDir, "kb_effectiveness.json")
	tracker, err := NewKBEffectivenessTracker(persistPath, false)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	return tracker
}

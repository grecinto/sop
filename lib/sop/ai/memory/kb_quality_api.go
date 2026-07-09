package memory

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// KBQualityAPI provides HTTP handlers for KB quality metrics.
type KBQualityAPI struct {
	tracker  *KBEffectivenessTracker
	analyzer *PruningAnalyzer
	job      *QualityCheckJob
}

// NewKBQualityAPI creates a new API handler for KB quality endpoints.
func NewKBQualityAPI(tracker *KBEffectivenessTracker, thresholds PruningThresholds) *KBQualityAPI {
	return &KBQualityAPI{
		tracker:  tracker,
		analyzer: NewPruningAnalyzer(tracker, thresholds),
	}
}

// SetQualityJob registers a quality check job with the API (optional).
func (api *KBQualityAPI) SetQualityJob(job *QualityCheckJob) {
	api.job = job
}

// RegisterRoutes registers all KB quality API routes to the provided mux.
func (api *KBQualityAPI) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/kb/quality/report", api.HandleReport)
	mux.HandleFunc("/api/kb/quality/analyze", api.HandleAnalyze)
	mux.HandleFunc("/api/kb/quality/candidates", api.HandlePruningCandidates)
	mux.HandleFunc("/api/kb/effectiveness/all", api.HandleListAll)
	mux.HandleFunc("/api/kb/effectiveness/best", api.HandleBestKBs)
	mux.HandleFunc("/api/kb/effectiveness/poor", api.HandlePoorPerformers)
	mux.HandleFunc("/api/kb/effectiveness/kb", api.HandleKBDetails)
	mux.HandleFunc("/api/kb/quality/trigger", api.HandleTriggerCheck)
	mux.HandleFunc("/api/kb/quality/status", api.HandleJobStatus)
}

// HandleReport returns a comprehensive quality analysis report.
// GET /api/kb/quality/report?format=json|text
func (api *KBQualityAPI) HandleReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	report, err := api.analyzer.Analyze()
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Analysis failed: %v", err))
		return
	}

	if format == "text" {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		formatted := api.analyzer.FormatReport(report)
		w.Write([]byte(formatted))
		return
	}

	respondJSON(w, http.StatusOK, report)
}

// HandleAnalyze returns analysis results with optional filtering.
// GET /api/kb/quality/analyze?severity=critical|warning|all
func (api *KBQualityAPI) HandleAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	severity := r.URL.Query().Get("severity")
	if severity == "" {
		severity = "all"
	}

	report, err := api.analyzer.Analyze()
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Analysis failed: %v", err))
		return
	}

	// Filter by severity if requested
	filteredCandidates := report.PoorPerformers
	if severity != "all" {
		filteredCandidates = []PruningCandidate{}
		for _, candidate := range report.PoorPerformers {
			if candidate.Severity == severity {
				filteredCandidates = append(filteredCandidates, candidate)
			}
		}
	}

	response := map[string]interface{}{
		"generated_at":         report.GeneratedAt,
		"total_kbs":            report.TotalKBs,
		"active_kbs":           report.ActiveKBs,
		"stale_kbs":            report.StaleKBs,
		"average_success_rate": report.AverageSuccessRate,
		"poor_performers":      filteredCandidates,
		"recommendations":      report.Recommendations,
	}

	respondJSON(w, http.StatusOK, response)
}

// HandlePruningCandidates returns KBs that are candidates for pruning.
// GET /api/kb/quality/candidates
func (api *KBQualityAPI) HandlePruningCandidates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	report, err := api.analyzer.Analyze()
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Analysis failed: %v", err))
		return
	}

	// Only return critical candidates
	criticalCandidates := []PruningCandidate{}
	for _, candidate := range report.PoorPerformers {
		if candidate.Severity == "critical" {
			criticalCandidates = append(criticalCandidates, candidate)
		}
	}

	respondJSON(w, http.StatusOK, criticalCandidates)
}

// HandleListAll returns all KB effectiveness records.
// GET /api/kb/effectiveness/all
func (api *KBQualityAPI) HandleListAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	records := api.tracker.GetAllRecords()
	respondJSON(w, http.StatusOK, records)
}

// HandleBestKBs returns top-performing KBs for a given task type.
// GET /api/kb/effectiveness/best?task_type=implementation&limit=10
func (api *KBQualityAPI) HandleBestKBs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskType := r.URL.Query().Get("task_type")
	if taskType == "" {
		respondError(w, http.StatusBadRequest, "task_type parameter required")
		return
	}

	limit := 10
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	rankings := api.tracker.GetBestKBsForTask(taskType, limit)
	respondJSON(w, http.StatusOK, rankings)
}

// HandlePoorPerformers returns underperforming KBs.
// GET /api/kb/effectiveness/poor?min_uses=10&max_success_rate=0.30
func (api *KBQualityAPI) HandlePoorPerformers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	minUses := 10
	if minStr := r.URL.Query().Get("min_uses"); minStr != "" {
		if parsed, err := strconv.Atoi(minStr); err == nil && parsed > 0 {
			minUses = parsed
		}
	}

	maxSuccessRate := 0.30
	if maxStr := r.URL.Query().Get("max_success_rate"); maxStr != "" {
		if parsed, err := strconv.ParseFloat(maxStr, 64); err == nil && parsed > 0 {
			maxSuccessRate = parsed
		}
	}

	poorPerformers := api.tracker.GetPoorPerformers(minUses, maxSuccessRate)
	respondJSON(w, http.StatusOK, poorPerformers)
}

// HandleKBDetails returns detailed effectiveness data for a specific KB.
// GET /api/kb/effectiveness/kb?task_type=implementation&kb_name=go-best-practices
func (api *KBQualityAPI) HandleKBDetails(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskType := r.URL.Query().Get("task_type")
	kbName := r.URL.Query().Get("kb_name")

	if taskType == "" || kbName == "" {
		respondError(w, http.StatusBadRequest, "task_type and kb_name parameters required")
		return
	}

	effectiveness := api.tracker.GetKBEffectiveness(taskType, kbName)
	if effectiveness == nil {
		respondError(w, http.StatusNotFound, "KB not found")
		return
	}

	respondJSON(w, http.StatusOK, effectiveness)
}

// HandleTriggerCheck triggers an immediate quality check (if job is registered).
// POST /api/kb/quality/trigger
func (api *KBQualityAPI) HandleTriggerCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if api.job == nil {
		respondError(w, http.StatusServiceUnavailable, "Quality check job not configured")
		return
	}

	api.job.RunNow()

	respondJSON(w, http.StatusAccepted, map[string]string{
		"status":  "triggered",
		"message": "Quality check initiated",
	})
}

// HandleJobStatus returns the status of the quality check job.
// GET /api/kb/quality/status
func (api *KBQualityAPI) HandleJobStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if api.job == nil {
		respondError(w, http.StatusServiceUnavailable, "Quality check job not configured")
		return
	}

	report, lastRun := api.job.GetLastReport()

	status := map[string]interface{}{
		"last_run": lastRun,
		"next_run": lastRun.Add(api.job.config.Interval),
	}

	if report != nil {
		status["last_report"] = map[string]interface{}{
			"total_kbs":            report.TotalKBs,
			"active_kbs":           report.ActiveKBs,
			"stale_kbs":            report.StaleKBs,
			"average_success_rate": report.AverageSuccessRate,
			"poor_performer_count": len(report.PoorPerformers),
		}
	}

	respondJSON(w, http.StatusOK, status)
}

// Helper functions

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{
		"error": message,
	})
}

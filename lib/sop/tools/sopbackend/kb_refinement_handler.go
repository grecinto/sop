package sopbackend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/sharedcode/sop/ai/agent/codingworkflow"
	"github.com/sharedcode/sop/ai/memory"
)

// ============================================================================
// Revalidation Endpoint
// ============================================================================

// KBRevalidateRequest represents a request to re-validate with stricter standards
type KBRevalidateRequest struct {
	MessageID  string `json:"message_id"`          // Original message ID
	Task       string `json:"task,omitempty"`      // Task description
	TaskType   string `json:"task_type,omitempty"` // Task type for compatibility
	Code       string `json:"code"`                // Code to validate
	KBName     string `json:"kb_name"`             // KB to validate against
	StrictMode bool   `json:"strict_mode"`         // Enable strict validation
}

// KBRevalidateResponse represents the validation result
type KBRevalidateResponse struct {
	Success          bool                       `json:"success"`
	ValidationPassed bool                       `json:"validation_passed"`
	ComplianceScore  float64                    `json:"compliance_score"`
	Issues           []codingworkflow.Violation `json:"issues,omitempty"`
	Message          string                     `json:"message,omitempty"`
}

// HandleKBRevalidate re-validates code with stricter standards
func HandleKBRevalidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req KBRevalidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, KBRevalidateResponse{
			Success: false,
			Message: "Invalid request body: " + err.Error(),
		})
		return
	}

	// Validate required fields
	if req.MessageID == "" {
		respondJSON(w, http.StatusBadRequest, KBRevalidateResponse{
			Success: false,
			Message: "message_id is required",
		})
		return
	}

	if req.Code == "" {
		respondJSON(w, http.StatusBadRequest, KBRevalidateResponse{
			Success: false,
			Message: "code is required",
		})
		return
	}

	if req.KBName == "" {
		respondJSON(w, http.StatusBadRequest, KBRevalidateResponse{
			Success: false,
			Message: "kb_name is required",
		})
		return
	}

	// For now, create a simple expertise capsule from the KB name
	// In a full implementation, this would query the KB for actual rules
	// Create a simple patch object to validate
	patch := &codingworkflow.Patch{
		FilePath:   "generated_code.go",
		OldContent: "",
		NewContent: req.Code,
	}

	// Create validator with appropriate config
	validatorConfig := codingworkflow.DefaultValidatorConfig()
	validatorConfig.StrictMode = req.StrictMode

	if req.StrictMode {
		// Enable all checks in strict mode
		validatorConfig.EnableComments = true
	}

	validator := codingworkflow.NewValidator(validatorConfig)

	// Perform validation
	kbRules := []string{} // Could be populated from actual KB rules
	result := validator.ValidatePatch(patch, kbRules)

	// Build response
	response := KBRevalidateResponse{
		Success:          true,
		ValidationPassed: result.Passed,
		ComplianceScore:  result.ComplianceScore,
		Issues:           result.Violations,
	}

	if result.Passed {
		response.Message = "Code passed validation"
		if req.StrictMode {
			response.Message = "Code passed strict validation"
		}
	} else {
		response.Message = "Code failed validation"
		if req.StrictMode {
			response.Message = "Code failed strict validation"
		}
	}

	logInfo("KB revalidation: kb=%s strict=%v passed=%v score=%.2f issues=%d msg=%s",
		req.KBName, req.StrictMode, result.Passed, result.ComplianceScore, len(result.Violations), req.MessageID)

	respondJSON(w, http.StatusOK, response)
}

// ============================================================================
// Alternative Spaces Endpoint
// ============================================================================

// KBAlternativesRequest represents a request for alternative Spaces
type KBAlternativesRequest struct {
	TaskType    string   `json:"task_type,omitempty"` // Task classification
	Task        string   `json:"task,omitempty"`      // Backward-compatible task field
	CurrentKB   string   `json:"current_kb"`          // Currently used KB
	ExcludedKBs []string `json:"excluded_kbs"`        // KBs to exclude (blocklist)
}

// KBAlternative represents an alternative Space suggestion
type KBAlternative struct {
	Name       string  `json:"name"`       // KB name
	Confidence float64 `json:"confidence"` // Confidence score (0.0-1.0)
	Reason     string  `json:"reason"`     // Why this KB is suggested
}

// KBAlternativesResponse represents alternative Space suggestions
type KBAlternativesResponse struct {
	Success      bool            `json:"success"`
	Alternatives []KBAlternative `json:"alternatives,omitempty"`
	Message      string          `json:"message,omitempty"`
}

// HandleKBAlternatives suggests alternative Spaces for a task
func HandleKBAlternatives(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req KBAlternativesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, KBAlternativesResponse{
			Success: false,
			Message: "Invalid request body: " + err.Error(),
		})
		return
	}

	// Normalize task type
	taskType := strings.ToLower(strings.TrimSpace(req.TaskType))
	if taskType == "" {
		taskType = strings.ToLower(strings.TrimSpace(req.Task))
	}
	if taskType == "" {
		taskType = "general"
	}

	// Get KB effectiveness tracker
	tracker := GetKBEffectivenessTracker()
	if tracker == nil {
		respondJSON(w, http.StatusInternalServerError, KBAlternativesResponse{
			Success: false,
			Message: "KB effectiveness tracker not available",
		})
		return
	}

	// Build exclusion list (current KB + blocklist)
	excludedKBs := make(map[string]bool)
	if req.CurrentKB != "" {
		excludedKBs[req.CurrentKB] = true
	}
	for _, kb := range req.ExcludedKBs {
		excludedKBs[kb] = true
	}

	// Get best KBs for the task type
	allKBs := tracker.GetBestKBsForTask(taskType, 5) // Get top 5

	// Filter out excluded KBs and build alternatives list
	var alternatives []KBAlternative
	for _, kb := range allKBs {
		if excludedKBs[kb.KBName] {
			continue
		}

		// Calculate confidence based on effectiveness score
		confidence := kb.EffectivenessScore

		// Generate reason based on statistics
		reason := buildAlternativeReason(kb, taskType)

		alternatives = append(alternatives, KBAlternative{
			Name:       kb.KBName,
			Confidence: confidence,
			Reason:     reason,
		})
	}

	// Build response
	response := KBAlternativesResponse{
		Success:      true,
		Alternatives: alternatives,
	}

	if len(alternatives) == 0 {
		response.Message = "No alternative Spaces found for this task type"
	} else {
		response.Message = "Found alternative Spaces"
	}

	logInfo("KB alternatives: task=%s current=%s excluded=%d found=%d",
		taskType, req.CurrentKB, len(req.ExcludedKBs), len(alternatives))

	respondJSON(w, http.StatusOK, response)
}

// buildAlternativeReason generates a human-readable reason for suggesting a KB
func buildAlternativeReason(kb memory.KBRanking, taskType string) string {
	if kb.UsageCount == 0 {
		return "New Space, not yet used for this task type"
	}

	successRate := kb.SuccessRate * 100
	avgCompliance := kb.AvgCompliance * 100

	if successRate >= 80 && avgCompliance >= 80 {
		return "Strong track record: " + formatPercent(successRate) + " success rate"
	} else if successRate >= 70 {
		return "Good track record: " + formatPercent(successRate) + " success rate"
	} else if kb.UsageCount < 5 {
		return "Limited data: " + formatInt(kb.UsageCount) + " uses"
	} else {
		return "Alternative option: " + formatPercent(successRate) + " success rate"
	}
}

// formatPercent formats a percentage value
func formatPercent(val float64) string {
	return strings.TrimRight(strings.TrimRight(formatFloat(val, 1), "0"), ".") + "%"
}

// formatFloat formats a float with specified precision
func formatFloat(val float64, precision int) string {
	switch precision {
	case 0:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.0f", val), "0"), ".")
	case 1:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", val), "0"), ".")
	case 2:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", val), "0"), ".")
	default:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", val), "0"), ".")
	}
}

// formatInt formats an integer
func formatInt(val int) string {
	return fmt.Sprintf("%d", val)
}

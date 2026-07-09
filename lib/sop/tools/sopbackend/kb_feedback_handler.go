package sopbackend

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/sharedcode/sop/ai/memory"
)

// KBFeedbackRequest represents user feedback on KB-generated code
type KBFeedbackRequest struct {
	MessageID string `json:"message_id"` // Chat message ID
	KBName    string `json:"kb_name"`    // KB that was used
	TaskType  string `json:"task_type"`  // Task classification
	Feedback  string `json:"feedback"`   // "positive" or "negative"
	Comment   string `json:"comment"`    // Optional user comment
	UserID    string `json:"user_id"`    // User identifier
	SessionID string `json:"session_id"` // Chat session identifier
}

// KBFeedbackResponse represents the response to feedback submission
type KBFeedbackResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// HandleKBFeedback processes user feedback on KB-generated code
func HandleKBFeedback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req KBFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, KBFeedbackResponse{
			Success: false,
			Message: "Invalid request body: " + err.Error(),
		})
		return
	}

	// Validate required fields
	if req.MessageID == "" {
		respondJSON(w, http.StatusBadRequest, KBFeedbackResponse{
			Success: false,
			Message: "message_id is required",
		})
		return
	}

	if req.KBName == "" {
		respondJSON(w, http.StatusBadRequest, KBFeedbackResponse{
			Success: false,
			Message: "kb_name is required",
		})
		return
	}

	if req.Feedback != "positive" && req.Feedback != "negative" {
		respondJSON(w, http.StatusBadRequest, KBFeedbackResponse{
			Success: false,
			Message: "feedback must be 'positive' or 'negative'",
		})
		return
	}

	// Normalize task type
	taskType := strings.ToLower(strings.TrimSpace(req.TaskType))
	if taskType == "" {
		taskType = "general"
	}

	// Record outcome to KB effectiveness tracker
	success := req.Feedback == "positive"

	// Get or initialize KB effectiveness tracker
	tracker := GetKBEffectivenessTracker()
	if tracker == nil {
		// Fallback: log the feedback but don't fail
		logInfo("KB feedback received but tracker not available: kb=%s task=%s feedback=%s",
			req.KBName, taskType, req.Feedback)
		respondJSON(w, http.StatusOK, KBFeedbackResponse{
			Success: true,
			Message: "Feedback recorded (tracker not available)",
		})
		return
	}

	// Record the outcome
	// Use compliance score of 1.0 for positive feedback, 0.0 for negative
	complianceScore := 0.0
	if success {
		complianceScore = 1.0
	}
	tracker.RecordOutcome(taskType, req.KBName, success, complianceScore)

	// Log the feedback for audit
	logInfo("KB feedback recorded: kb=%s task=%s feedback=%s user=%s session=%s msg=%s comment=%q",
		req.KBName, taskType, req.Feedback, req.UserID, req.SessionID, req.MessageID, req.Comment)

	respondJSON(w, http.StatusOK, KBFeedbackResponse{
		Success: true,
		Message: "Feedback recorded successfully",
	})
}

// GetKBEffectivenessTracker returns the global KB effectiveness tracker instance
func GetKBEffectivenessTracker() *memory.KBEffectivenessTracker {
	// Use the package-level tracker from exports.go
	if kbEffectivenessTracker != nil {
		return kbEffectivenessTracker
	}

	// Initialize if not yet done
	initKBQuality()
	return kbEffectivenessTracker
}

// respondJSON writes a JSON response
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logError("Failed to encode JSON response: %v", err)
	}
}

// logInfo logs an informational message
func logInfo(format string, args ...interface{}) {
	// Use existing logging infrastructure if available
	// For now, using standard log package
	log.Printf("[INFO] "+format, args...)
}

// logError logs an error message
func logError(format string, args ...interface{}) {
	log.Printf("[ERROR] "+format, args...)
}

// KBMetadata represents KB usage metadata to include in chat responses
type KBMetadata struct {
	KBsUsed      []string `json:"kbs_used,omitempty"`
	TaskType     string   `json:"task_type,omitempty"`
	Confidence   float64  `json:"confidence,omitempty"`
	Explanation  string   `json:"explanation,omitempty"`
	RulesApplied []string `json:"rules_applied,omitempty"`
}

// EnhancedAskResponse extends AskResponse with KB metadata
type EnhancedAskResponse struct {
	Answer     string      `json:"answer"`
	Error      string      `json:"error,omitempty"`
	KBMetadata *KBMetadata `json:"kb_metadata,omitempty"`
	Timestamp  time.Time   `json:"timestamp"`
}

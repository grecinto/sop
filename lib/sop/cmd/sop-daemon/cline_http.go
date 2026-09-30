package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sharedcode/sop/ai/agent/domains/ideworker"
)

type clineHTTPExecuteRequest struct {
	RequestID     string                       `json:"request_id,omitempty"`
	SessionID     string                       `json:"session_id"`
	Task          string                       `json:"task"`
	Provider      string                       `json:"provider,omitempty"`
	Model         string                       `json:"model,omitempty"`
	APIKey        string                       `json:"api_key,omitempty"`
	BaseURL       string                       `json:"base_url,omitempty"`
	Mode          string                       `json:"mode,omitempty"`
	WorkspaceRoot string                       `json:"workspace_root"`
	ActiveFile    string                       `json:"active_file,omitempty"`
	SelectedText  string                       `json:"selected_text,omitempty"`
	OpenFiles     []string                     `json:"open_files,omitempty"`
	Guidance      ideworker.DelegationGuidance `json:"guidance,omitempty"`
}

func (s *localRAGService) handleClineExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s == nil || s.clineBackend == nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "Cline backend is unavailable"})
		return
	}
	var input clineHTTPExecuteRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid Cline request: " + err.Error()})
		return
	}
	if strings.TrimSpace(input.SessionID) == "" || strings.TrimSpace(input.Task) == "" || strings.TrimSpace(input.WorkspaceRoot) == "" {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "session_id, task, and workspace_root are required"})
		return
	}
	result, err := s.clineBackend.ExecuteTask(r.Context(), ideworker.TaskRequest{
		RequestID: input.RequestID, SessionID: input.SessionID, Task: input.Task,
		Provider: input.Provider, Model: input.Model, APIKey: input.APIKey, BaseURL: input.BaseURL, Mode: input.Mode,
		WorkspaceRoot: input.WorkspaceRoot, ActiveFile: input.ActiveFile,
		SelectedText: input.SelectedText, OpenFiles: input.OpenFiles, Guidance: input.Guidance,
	})
	if err != nil {
		respondJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	var structured ideworker.ExecutionResult
	if err := json.Unmarshal([]byte(result), &structured); err != nil {
		respondJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true, "result": structured})
}

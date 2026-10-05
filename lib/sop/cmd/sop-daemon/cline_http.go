package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
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

func (s *localRAGService) decodeClineRequest(r *http.Request) (clineHTTPExecuteRequest, error) {
	var input clineHTTPExecuteRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, fmt.Errorf("invalid Cline request: %w", err)
	}
	if strings.TrimSpace(input.SessionID) == "" || strings.TrimSpace(input.Task) == "" || strings.TrimSpace(input.WorkspaceRoot) == "" {
		return input, fmt.Errorf("session_id, task, and workspace_root are required")
	}
	return input, nil
}

func clineTaskRequest(input clineHTTPExecuteRequest) ideworker.TaskRequest {
	return ideworker.TaskRequest{
		RequestID: input.RequestID, SessionID: input.SessionID, Task: input.Task,
		Provider: input.Provider, Model: input.Model, APIKey: input.APIKey, BaseURL: input.BaseURL, Mode: input.Mode,
		WorkspaceRoot: input.WorkspaceRoot, ActiveFile: input.ActiveFile,
		SelectedText: input.SelectedText, OpenFiles: input.OpenFiles, Guidance: input.Guidance,
	}
}

func populateClineMCPContext(r *http.Request, taskRequest *ideworker.TaskRequest) {
	if r == nil || taskRequest == nil {
		return
	}
	taskRequest.MCPURL, taskRequest.MCPBearerToken = clineMCPContext(r)
}

func clineMCPContext(r *http.Request) (string, string) {
	if r == nil {
		return "", ""
	}
	if configured := strings.TrimSpace(os.Getenv("SOP_AI_STUDIO_MCP_URL")); configured != "" {
		if parsed, err := url.ParseRequestURI(configured); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" {
			return configured, clineMCPBearerToken(r)
		}
	}
	host := strings.TrimSpace(r.Host)
	if !isLoopbackHTTPHost(host) {
		host = "127.0.0.1:9090"
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + host + "/mcp", clineMCPBearerToken(r)
}

func isLoopbackHTTPHost(host string) bool {
	hostname := host
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		hostname = parsedHost
	}
	if strings.EqualFold(strings.TrimSpace(hostname), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(hostname, "[]"))
	return ip != nil && ip.IsLoopback()
}

func clineMCPBearerToken(r *http.Request) string {
	if token := extractBearerToken(r.Header.Get("Authorization")); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv("SOP_AI_STUDIO_MCP_TOKEN"))
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
	input, err := s.decodeClineRequest(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	taskRequest := clineTaskRequest(input)
	populateClineMCPContext(r, &taskRequest)
	result, err := s.clineBackend.ExecuteTask(r.Context(), taskRequest)
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

func (s *localRAGService) handleClineTaskStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s == nil || s.clineBackend == nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "Cline backend is unavailable"})
		return
	}
	input, err := s.decodeClineRequest(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	taskRequest := clineTaskRequest(input)
	populateClineMCPContext(r, &taskRequest)
	handle, err := s.clineBackend.StartTask(r.Context(), taskRequest)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	respondJSON(w, http.StatusAccepted, map[string]any{"ok": true, "task": handle})
}

func (s *localRAGService) handleClineTaskStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s == nil || s.clineBackend == nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "Cline backend is unavailable"})
		return
	}
	taskID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/cline/tasks/"), "/")
	if taskID == "" || strings.Contains(taskID, "/") {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "task id is required"})
		return
	}
	status, err := s.clineBackend.GetTaskStatus(r.Context(), taskID)
	if err != nil {
		respondJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true, "status": status})
}

func (s *localRAGService) handleClineTaskCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s == nil || s.clineBackend == nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "Cline backend is unavailable"})
		return
	}
	taskID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/cline/tasks/"), "/")
	taskID = strings.TrimSuffix(taskID, "/cancel")
	if taskID == "" || strings.Contains(taskID, "/") {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "task id is required"})
		return
	}
	status, err := s.clineBackend.CancelTask(r.Context(), taskID)
	if err != nil {
		respondJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true, "status": status})
}

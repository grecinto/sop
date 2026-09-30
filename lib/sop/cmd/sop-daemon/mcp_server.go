package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sharedcode/sop/ai/agent/codingworkflow"
)

type mcpJSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpJSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpRPCError    `json:"error,omitempty"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

func registerMCPRoute(mux *http.ServeMux, authHandler *DaemonAuthHandler, service *localRAGService) {
	if mux == nil {
		return
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		mcpHandleRequest(service, w, r)
	})
	if authHandler != nil {
		handler = authHandler.RequireAuth(handler)
	}
	mux.HandleFunc("/mcp", handleOptionsAndCORS(handler))
}

func mcpHandleRequest(service *localRAGService, w http.ResponseWriter, r *http.Request) {
	var request mcpJSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeMCPError(w, nil, -32700, "invalid JSON")
		return
	}
	if request.JSONRPC != "2.0" {
		writeMCPError(w, request.ID, -32600, "jsonrpc must be 2.0")
		return
	}
	switch request.Method {
	case "initialize":
		writeMCPResult(w, request.ID, map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "ai-studio", "version": "0.1.0"},
		})
	case "notifications/initialized":
		writeMCPResult(w, request.ID, map[string]any{})
	case "tools/list":
		writeMCPResult(w, request.ID, map[string]any{"tools": mcpToolDefinitions()})
	case "tools/call":
		var params mcpToolCallParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			writeMCPError(w, request.ID, -32602, "invalid tools/call parameters")
			return
		}
		if params.Name != "get_task_guidance" && params.Name != "search_memory" && params.Name != "search_undo_history" && params.Name != "record_validation" && params.Name != "restore_undo_transaction" {
			writeMCPError(w, request.ID, -32602, fmt.Sprintf("unknown tool %q", params.Name))
			return
		}
		var result string
		var err error
		if params.Name == "get_task_guidance" {
			result, err = mcpTaskGuidance(service, params.Arguments)
		} else if params.Name == "search_memory" {
			result, err = mcpSearchMemory(r.Context(), service, params.Arguments)
		} else if params.Name == "search_undo_history" {
			result, err = mcpSearchUndoHistory(r.Context(), params.Arguments)
		} else if params.Name == "record_validation" {
			result, err = mcpRecordValidation(service, params.Arguments)
		} else {
			result, err = mcpRestoreUndoTransaction(r.Context(), service, params.Arguments)
		}
		if err != nil {
			writeMCPError(w, request.ID, -32000, err.Error())
			return
		}
		writeMCPResult(w, request.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": result}}})
	default:
		writeMCPError(w, request.ID, -32601, "method not found")
	}
}

func mcpToolDefinitions() []any {
	return []any{
		map[string]any{
			"name":        "get_task_guidance",
			"description": "Return AI Studio guidance and current delegated task context for a Cline session.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"session_id": map[string]any{"type": "string"}}},
		},
		map[string]any{
			"name":        "search_memory",
			"description": "Search AI Studio semantic memory and KnowledgeBase context for the current coding task.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20}}, "required": []string{"query"}},
		},
		map[string]any{
			"name":        "search_undo_history",
			"description": "Search AI Studio's canonical undo transactions without modifying the workspace.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"workspace_root": map[string]any{"type": "string"}, "session_id": map[string]any{"type": "string"}, "query": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20}}, "required": []string{"workspace_root"}},
		},
		map[string]any{
			"name":        "record_validation",
			"description": "Record test, build, or validation results in the AI Studio task context.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"session_id": map[string]any{"type": "string"}, "status": map[string]any{"type": "string"}, "summary": map[string]any{"type": "string"}, "command": map[string]any{"type": "string"}}, "required": []string{"session_id", "status", "summary"}},
		},
		map[string]any{
			"name":        "restore_undo_transaction",
			"description": "Restore a canonical AI Studio undo transaction. This mutates workspace files and requires explicit confirmation.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"workspace_root": map[string]any{"type": "string"}, "session_id": map[string]any{"type": "string"}, "transaction_id": map[string]any{"type": "string"}, "confirm": map[string]any{"type": "boolean"}}, "required": []string{"workspace_root", "transaction_id", "confirm"}},
		},
	}
}

func mcpSearchMemory(ctx context.Context, service *localRAGService, arguments map[string]any) (string, error) {
	query, _ := arguments["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	limit := 5
	if raw, ok := arguments["limit"].(float64); ok && int(raw) > 0 {
		limit = int(raw)
	}
	if limit > 20 {
		limit = 20
	}
	if service == nil || service.service == nil {
		return "", fmt.Errorf("AI Studio memory service is unavailable")
	}
	hits, err := service.service.Search(ctx, query, limit)
	if err != nil {
		return "", fmt.Errorf("search AI Studio memory: %w", err)
	}
	results := make([]map[string]any, 0, len(hits))
	for _, hit := range hits {
		results = append(results, map[string]any{"id": hit.ID, "score": hit.Score, "source": hit.SourceLabel, "content": hit.DocumentContent, "payload": hit.Payload})
	}
	encoded, err := json.MarshalIndent(map[string]any{"query": query, "results": results}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode memory results: %w", err)
	}
	return string(encoded), nil
}

func mcpSearchUndoHistory(ctx context.Context, arguments map[string]any) (string, error) {
	workspaceRoot, _ := arguments["workspace_root"].(string)
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if err := validateMCPWorkspaceRoot(workspaceRoot); err != nil {
		return "", err
	}
	sessionID, _ := arguments["session_id"].(string)
	userID := firstNonEmpty(strings.TrimSpace(sessionID), "daemon")
	manager, err := getContinueUndoManager(userID, workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("open undo history: %w", err)
	}
	query, _ := arguments["query"].(string)
	limit := 10
	if raw, ok := arguments["limit"].(float64); ok && int(raw) > 0 {
		limit = int(raw)
	}
	if limit > 20 {
		limit = 20
	}
	transactions, err := manager.SemanticSearch(ctx, codingworkflow.UndoQuery{SemanticQuery: strings.TrimSpace(query), Limit: limit, RankBy: codingworkflow.RankByHybrid})
	if err != nil {
		return "", fmt.Errorf("search undo history: %w", err)
	}
	encoded, err := json.MarshalIndent(map[string]any{"workspace_root": workspaceRoot, "transactions": transactions}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode undo history: %w", err)
	}
	return string(encoded), nil
}

func mcpTaskGuidance(service *localRAGService, arguments map[string]any) (string, error) {
	sessionID, _ := arguments["session_id"].(string)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "No session_id was provided; no task-scoped guidance is available.", nil
	}
	snapshot, ok := service.ensureContinueSessionStore().Get(sessionID)
	if !ok {
		return fmt.Sprintf("No AI Studio task context is available for session %q.", sessionID), nil
	}
	payload := map[string]any{
		"session_id":       snapshot.SessionID,
		"request_id":       snapshot.RequestID,
		"status":           snapshot.Status,
		"phase":            snapshot.Phase,
		"mode":             snapshot.Mode,
		"workflow_profile": snapshot.WorkflowProfile,
		"task":             snapshot.Message,
		"workspace_root":   snapshot.WorkspaceRoot,
		"active_file":      snapshot.ActiveFile,
		"open_files":       snapshot.OpenFiles,
		"validation":       snapshot.Validation,
	}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode task guidance: %w", err)
	}
	return string(encoded), nil
}

func mcpRecordValidation(service *localRAGService, arguments map[string]any) (string, error) {
	sessionID, _ := arguments["session_id"].(string)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}
	status, _ := arguments["status"].(string)
	summary, _ := arguments["summary"].(string)
	if strings.TrimSpace(status) == "" || strings.TrimSpace(summary) == "" {
		return "", fmt.Errorf("status and summary are required")
	}
	snapshot, ok := service.ensureContinueSessionStore().Get(sessionID)
	if !ok {
		return "", fmt.Errorf("session %q was not found", sessionID)
	}
	validation := map[string]any{"status": strings.TrimSpace(status), "summary": strings.TrimSpace(summary)}
	if command, ok := arguments["command"].(string); ok && strings.TrimSpace(command) != "" {
		validation["command"] = strings.TrimSpace(command)
	}
	snapshot.Validation = validation
	service.ensureContinueSessionStore().Put(snapshot)
	encoded, err := json.MarshalIndent(validation, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode validation: %w", err)
	}
	return string(encoded), nil
}

func mcpRestoreUndoTransaction(ctx context.Context, service *localRAGService, arguments map[string]any) (string, error) {
	workspaceRoot, _ := arguments["workspace_root"].(string)
	transactionID, _ := arguments["transaction_id"].(string)
	confirm, _ := arguments["confirm"].(bool)
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	transactionID = strings.TrimSpace(transactionID)
	if transactionID == "" {
		return "", fmt.Errorf("workspace_root and transaction_id are required")
	}
	if err := validateMCPWorkspaceRoot(workspaceRoot); err != nil {
		return "", err
	}
	if !confirm {
		return "", fmt.Errorf("restore requires explicit confirm=true")
	}
	sessionID, _ := arguments["session_id"].(string)
	manager, err := getContinueUndoManager(firstNonEmpty(strings.TrimSpace(sessionID), "daemon"), workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("open undo history: %w", err)
	}
	if err := manager.RestoreTransaction(ctx, transactionID, codingworkflow.RestoreOptions{Mode: codingworkflow.RestoreFull, CreateBackup: true, VerifyHashes: true, ConfirmPrompt: true}); err != nil {
		return "", fmt.Errorf("restore undo transaction: %w", err)
	}
	if service != nil && strings.TrimSpace(sessionID) != "" {
		if snapshot, ok := service.ensureContinueSessionStore().Get(strings.TrimSpace(sessionID)); ok {
			snapshot.Status = "stale"
			snapshot.Message = "Workspace restored by AI Studio; refresh the Cline session before continuing."
			service.ensureContinueSessionStore().Put(snapshot)
		}
	}
	return fmt.Sprintf("restored undo transaction %s; associated Cline session is stale and must be refreshed", transactionID), nil
}

func validateMCPWorkspaceRoot(workspaceRoot string) error {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return fmt.Errorf("workspace_root is required")
	}
	resolved, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return fmt.Errorf("resolve workspace_root: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("workspace_root is unavailable: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace_root is not a directory")
	}
	return nil
}

func writeMCPResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeMCPResponse(w, mcpJSONRPCResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func writeMCPError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	writeMCPResponse(w, mcpJSONRPCResponse{JSONRPC: "2.0", ID: id, Error: &mcpRPCError{Code: code, Message: message}})
}

func writeMCPResponse(w http.ResponseWriter, response mcpJSONRPCResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

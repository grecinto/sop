package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent"
	aidb "github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/tools/sopbackend"
)

const defaultDaemonConfigFileName = "config.json"

// isValidDaemonConfig checks if the config file has the required daemon structure
func isValidDaemonConfig(configPath string) bool {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}

	var config struct {
		SystemDB *struct {
			Name string `json:"name"`
			Path string `json:"path"`
			Mode string `json:"mode"`
		} `json:"system_db"`
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return false
	}

	// Valid daemon config must have system_db with path
	return config.SystemDB != nil && config.SystemDB.Path != ""
}

func ensureConfigPath(explicitPath string) (string, bool, error) {
	if strings.TrimSpace(explicitPath) != "" {
		resolved := resolveConfigPath(explicitPath)
		return resolved, false, nil
	}

	candidates := []string{}
	if v := os.Getenv("XDG_CONFIG_HOME"); strings.TrimSpace(v) != "" {
		candidates = append(candidates, filepath.Join(v, "sop", defaultDaemonConfigFileName))
	} else {
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, ".config", "sop", defaultDaemonConfigFileName))
			candidates = append(candidates, filepath.Join(home, "sop", defaultDaemonConfigFileName))
		}
		candidates = append(candidates, filepath.Join(cwd(), defaultDaemonConfigFileName))
		candidates = append(candidates, filepath.Join(cwd(), "sop", defaultDaemonConfigFileName))
	}

	var defaultPath string
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			if isValidDaemonConfig(candidate) {
				return candidate, false, nil
			}
			log.Printf("Found invalid daemon config at %s, will recreate with proper structure", candidate)
			defaultPath = candidate
			break
		}
	}

	if defaultPath == "" {
		if v := os.Getenv("XDG_CONFIG_HOME"); strings.TrimSpace(v) != "" {
			defaultPath = filepath.Join(v, "sop", defaultDaemonConfigFileName)
		} else if home, err := os.UserHomeDir(); err == nil {
			defaultPath = filepath.Join(home, ".config", "sop", defaultDaemonConfigFileName)
		} else {
			defaultPath = filepath.Join(cwd(), defaultDaemonConfigFileName)
		}
	}

	if err := os.MkdirAll(filepath.Dir(defaultPath), 0o755); err != nil {
		return "", false, fmt.Errorf("create config directory: %w", err)
	}

	systemDBPath := filepath.Join(filepath.Dir(defaultPath), "system")
	bootstrap := fmt.Sprintf(`{
  "port": 9090,
  "system_db": {
    "name": "system",
    "path": "%s",
    "mode": "standalone",
    "is_system": true
  },
  "databases": [],
  "pageSize": 40,
  "production_mode": true
}
`, systemDBPath)
	if err := os.WriteFile(defaultPath, []byte(bootstrap), 0o644); err != nil {
		return "", false, fmt.Errorf("write default config: %w", err)
	}

	log.Printf("Created default daemon config at %s with standalone SystemDB", defaultPath)
	return defaultPath, true, nil
}

type AskRequest struct {
	Query string `json:"query"`
	Auth  struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	} `json:"auth"`
	HQ struct {
		BaseURL   string `json:"base_url"`
		UserID    string `json:"user_id"`
		SessionID string `json:"session_id"`
	} `json:"hq"`
	LLMProvider   string `json:"llm_provider,omitempty"`
	LLMAPIKey     string `json:"llm_api_key,omitempty"`
	LLMModel      string `json:"llm_model,omitempty"`
	LLMURL        string `json:"llm_url,omitempty"`
	ThinkingLevel string `json:"thinking_level,omitempty"`
}

type AskResponse struct {
	Answer string `json:"answer"`
	Error  string `json:"error,omitempty"`
}

type localRAGService struct {
	service       ai.Agent[map[string]any]
	shellSessions *shellSessionState
}

type shellSessionState struct {
	mu       sync.Mutex
	sessions map[string]*shellSession
}

type shellSession struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	process  *os.Process
	cwd      string
	busy     bool
	lastErr  error
	output   bytes.Buffer
	exitCode int
}

func newShellSessionState() *shellSessionState {
	return &shellSessionState{sessions: make(map[string]*shellSession)}
}

func newShellSessionManager() *shellSessionState {
	return newShellSessionState()
}

func (s *shellSessionState) run(sessionID, command string, timeout time.Duration) (string, bool, error) {
	if s == nil {
		return "", false, nil
	}
	trimmed := strings.TrimSpace(command)
	if trimmed == "" || strings.HasPrefix(trimmed, "/") {
		return "", false, nil
	}

	s.mu.Lock()
	session, ok := s.sessions[sessionID]
	if !ok {
		session = newShellSession()
		s.sessions[sessionID] = session
	}
	s.mu.Unlock()

	return session.run(trimmed, timeout)
}

func newShellSession() *shellSession {
	return &shellSession{cwd: "."}
}

func (s *shellSession) run(command string, timeout time.Duration) (string, bool, error) {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return "", false, nil
	}

	s.mu.Lock()
	if s.cwd == "" {
		s.cwd = "."
	}
	cwd := s.cwd
	s.mu.Unlock()

	if strings.HasPrefix(trimmed, "cd ") {
		target := strings.TrimSpace(strings.TrimPrefix(trimmed, "cd"))
		if target == "" || target == "~" {
			target = os.Getenv("HOME")
		}
		if target == "" {
			return "", false, nil
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(cwd, target)
		}
		absPath, err := filepath.Abs(target)
		if err != nil {
			return "", false, err
		}
		s.mu.Lock()
		s.cwd = absPath
		s.mu.Unlock()
		return "", false, nil
	}

	if strings.EqualFold(trimmed, "status") {
		s.mu.Lock()
		output := strings.TrimSpace(s.output.String())
		running := s.busy
		exitCode := s.exitCode
		s.mu.Unlock()
		if running {
			return fmt.Sprintf("running=true\nexit_code=%d\n%s", exitCode, output), true, nil
		}
		return fmt.Sprintf("running=false\nexit_code=%d\n%s", exitCode, output), false, nil
	}

	if strings.EqualFold(trimmed, "stop") {
		s.mu.Lock()
		if s.busy && s.process != nil {
			_ = s.process.Kill()
		}
		s.busy = false
		s.exitCode = 1
		output := strings.TrimSpace(s.output.String())
		s.mu.Unlock()
		return fmt.Sprintf("stopped\n%s", output), false, nil
	}

	if s.busy && s.process != nil {
		return strings.TrimSpace(s.output.String()), true, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", trimmed)
	cmd.Dir = cwd
	cmd.Env = os.Environ()

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stdout

	s.mu.Lock()
	s.busy = true
	s.cmd = cmd
	s.process = nil
	s.output.Reset()
	s.exitCode = 0
	s.mu.Unlock()

	if err := cmd.Start(); err != nil {
		s.mu.Lock()
		s.busy = false
		s.lastErr = err
		s.mu.Unlock()
		return "", false, err
	}

	s.mu.Lock()
	s.process = cmd.Process
	s.mu.Unlock()

	err := cmd.Wait()
	s.mu.Lock()
	s.lastErr = err
	s.cmd = nil
	s.process = nil
	if ctx.Err() == context.DeadlineExceeded {
		s.busy = true
		s.exitCode = -1
	} else {
		s.busy = false
		if err != nil {
			s.exitCode = 1
		} else {
			s.exitCode = 0
		}
	}
	if stdout.Len() > 0 {
		s.output.Write(stdout.Bytes())
	}
	output := strings.TrimSpace(s.output.String())
	s.mu.Unlock()

	if ctx.Err() == context.DeadlineExceeded {
		return output, true, nil
	}
	if err != nil {
		return output, false, err
	}
	return output, false, nil
}

func newLocalRAGService(configPath string) (*localRAGService, error) {
	if configPath == "" {
		return nil, fmt.Errorf("config path is required")
	}

	if svc, err := autoProvisionSharedCopilotService(context.Background()); err == nil {
		log.Printf("daemon newLocalRAGService: using auto-provisioned shared service")
		return &localRAGService{service: svc, shellSessions: newShellSessionState()}, nil
	} else {
		log.Printf("daemon newLocalRAGService: shared RAG runtime unavailable: %v", err)
		return nil, fmt.Errorf("shared RAG runtime unavailable: %w", err)
	}
}

func autoProvisionSharedCopilotService(ctx context.Context) (ai.Agent[map[string]any], error) {
	log.Printf("daemon autoProvisionSharedCopilotService: starting")
	for _, agentID := range []string{"copilot", "sql_core"} {
		if agentSvc, ok := sopbackend.GetLoadedAgents()[agentID]; ok && agentSvc != nil {
			return agentSvc, nil
		}
	}
	for _, agentSvc := range sopbackend.GetLoadedAgents() {
		if agentSvc != nil {
			log.Printf("daemon autoProvisionSharedCopilotService: using fallback loaded agent")
			return agentSvc, nil
		}
	}

	policy := agent.AutomationPolicy{
		Enabled:               true,
		AllowShellCommands:    true,
		AllowEmail:            false,
		RequireApproval:       true,
		DefaultTimeoutSeconds: 600,
		MaxTimeoutSeconds:     1800,
	}
	if err := sopbackend.Initialize(ctx, sopbackend.RuntimeOptions{EnableLocalAutomation: true, AutomationPolicy: &policy}); err != nil {
		log.Printf("daemon autoProvisionSharedCopilotService: shared backend bootstrap failed: %v", err)
	} else {
		log.Printf("daemon autoProvisionSharedCopilotService: shared backend initialized")
		for _, agentID := range []string{"copilot", "sql_core"} {
			if agentSvc, ok := sopbackend.GetLoadedAgents()[agentID]; ok && agentSvc != nil {
				log.Printf("daemon autoProvisionSharedCopilotService: using initialized agent: %s", agentID)
				return agentSvc, nil
			}
		}
		for _, agentSvc := range sopbackend.GetLoadedAgents() {
			if agentSvc != nil {
				log.Printf("daemon autoProvisionSharedCopilotService: using initialized fallback agent")
				return agentSvc, nil
			}
		}
	}

	candidates := []string{
		"ai/data/copilot_pipeline.json",
		filepath.Join(cwd(), "ai/data/copilot_pipeline.json"),
		filepath.Join(cwd(), "..", "ai/data/copilot_pipeline.json"),
		filepath.Join(cwd(), "..", "..", "ai/data/copilot_pipeline.json"),
		filepath.Join("/Volumes/T7/sop-cloud/lib/sop", "ai/data/copilot_pipeline.json"),
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		cfg, err := agent.LoadConfigFromFile(candidate)
		if err != nil {
			return nil, fmt.Errorf("load shared pipeline config %s: %w", candidate, err)
		}
		svc, err := agent.NewFromConfig(ctx, *cfg, agent.Dependencies{})
		if err != nil {
			return nil, fmt.Errorf("build shared pipeline agent %s: %w", candidate, err)
		}
		return svc, nil
	}

	return nil, fmt.Errorf("shared copilot pipeline config not found")
}

func (s *localRAGService) handleAsk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req AskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, AskResponse{Error: "Invalid JSON body: " + err.Error()})
		return
	}
	if req.Query == "" {
		respondJSON(w, http.StatusBadRequest, AskResponse{Error: "Query cannot be empty"})
		return
	}
	if s == nil || s.service == nil {
		respondJSON(w, http.StatusServiceUnavailable, AskResponse{Error: "local RAG service is not initialized"})
		return
	}

	ctx := r.Context()
	sessionKey := strings.TrimSpace(req.HQ.SessionID)
	if sessionKey == "" {
		sessionKey = strings.TrimSpace(req.HQ.UserID)
	}
	if sessionKey == "" {
		sessionKey = "local-daemon"
	}

	var storedSessionContext daemonSessionContext
	daemonSessionStore.mu.Lock()
	if stored, ok := daemonSessionStore.data[sessionKey]; ok {
		storedSessionContext = stored
	}
	daemonSessionStore.mu.Unlock()

	payload := &ai.SessionPayload{
		CurrentUserQuery: req.Query,
		UserID:           strings.TrimSpace(req.HQ.UserID),
		SessionID:        strings.TrimSpace(req.HQ.SessionID),
	}
	if payload.UserID == "" && storedSessionContext.HQUserID != "" {
		payload.UserID = storedSessionContext.HQUserID
	}
	if payload.SessionID == "" {
		if storedSessionContext.HQSessionID != "" {
			payload.SessionID = storedSessionContext.HQSessionID
		} else if payload.UserID != "" {
			payload.SessionID = payload.UserID
		} else {
			payload.SessionID = sessionKey
		}
	}
	if payload.SessionID == "" {
		payload.SessionID = "local-daemon"
	}
	if s.shellSessions == nil {
		s.shellSessions = newShellSessionState()
	}
	sessionID := strings.TrimSpace(strings.TrimSpace(r.Header.Get("X-SOP-Session-ID")))
	if sessionID == "" {
		sessionID = payload.SessionID
	}

	// Update session store with latest tokens from this request
	// This ensures daemon always has fresh tokens for renewal
	accessToken := strings.TrimSpace(req.Auth.AccessToken)
	refreshToken := strings.TrimSpace(req.Auth.RefreshToken)
	hqBaseURL := strings.TrimSpace(req.HQ.BaseURL)
	if accessToken == "" && refreshToken == "" {
		accessToken = strings.TrimSpace(storedSessionContext.AccessToken)
		refreshToken = strings.TrimSpace(storedSessionContext.RefreshToken)
	}
	if hqBaseURL == "" {
		hqBaseURL = strings.TrimSpace(storedSessionContext.HQBaseURL)
	}
	if accessToken != "" || refreshToken != "" || hqBaseURL != "" || storedSessionContext.LLMProvider != "" || storedSessionContext.ThinkingLevel != "" {
		ctxKey := payload.SessionID
		daemonSessionStore.mu.Lock()
		daemonSessionStore.data[ctxKey] = daemonSessionContext{
			AccessToken:   accessToken,
			RefreshToken:  refreshToken,
			HQBaseURL:     hqBaseURL,
			HQUserID:      strings.TrimSpace(req.HQ.UserID),
			HQSessionID:   payload.SessionID,
			LLMProvider:   strings.TrimSpace(storedSessionContext.LLMProvider),
			LLMAPIKey:     strings.TrimSpace(storedSessionContext.LLMAPIKey),
			LLMModel:      strings.TrimSpace(storedSessionContext.LLMModel),
			LLMURL:        strings.TrimSpace(storedSessionContext.LLMURL),
			ThinkingLevel: strings.TrimSpace(storedSessionContext.ThinkingLevel),
			LastUpdated:   time.Now().UTC(),
		}
		daemonSessionStore.mu.Unlock()
	}

	ctx = context.WithValue(ctx, "session_payload", payload)
	ctx = sopbackend.InjectSharedRuntimeContext(ctx, nil, nil)

	cfg := ai.NewConfigMap()
	cfg.Set("payload", payload)
	cfg.Set("access_token", accessToken)
	cfg.Set("refresh_token", refreshToken)
	trimmedThinkingLevel := strings.TrimSpace(req.ThinkingLevel)
	if trimmedThinkingLevel == "" {
		trimmedThinkingLevel = strings.TrimSpace(storedSessionContext.ThinkingLevel)
	}
	if trimmedThinkingLevel != "" {
		cfg.Set("thinking_level", trimmedThinkingLevel)
	}
	cfg.Set("hq_base_url", hqBaseURL)
	cfg.Set("hq_user_id", strings.TrimSpace(req.HQ.UserID))
	cfg.Set("hq_session_id", strings.TrimSpace(req.HQ.SessionID))
	provider := strings.TrimSpace(req.LLMProvider)
	apiKey := strings.TrimSpace(req.LLMAPIKey)
	model := strings.TrimSpace(req.LLMModel)
	url := strings.TrimSpace(req.LLMURL)
	if provider == "" && apiKey == "" && model == "" && url == "" {
		provider = strings.TrimSpace(storedSessionContext.LLMProvider)
		apiKey = strings.TrimSpace(storedSessionContext.LLMAPIKey)
		model = strings.TrimSpace(storedSessionContext.LLMModel)
		url = strings.TrimSpace(storedSessionContext.LLMURL)
	}
	if provider != "" || apiKey != "" || model != "" || url != "" {
		providerDetails := &agent.ProviderDetails{
			Provider: provider,
			APIKey:   apiKey,
			Model:    model,
			BaseURL:  url,
		}
		cfg.Set("provider_details", providerDetails)
		providerName, modelName := agent.NormalizeProviderAndModel(providerDetails.Provider, providerDetails.Model)
		if providerName != "" {
			providerCtxValue := providerName
			if modelName != "" {
				providerCtxValue = providerName + ":" + modelName
			}
			ctx = context.WithValue(ctx, ai.CtxKeyProvider, providerCtxValue)
		}
		if providerDetails.APIKey != "" {
			ctx = context.WithValue(ctx, ai.CtxKeyAPIKey, providerDetails.APIKey)
		}
		if providerDetails.BaseURL != "" {
			ctx = context.WithValue(ctx, ai.CtxKeyBaseURL, providerDetails.BaseURL)
		}
	}

	normalizedQuery := normalizeAgentQuery(req.Query)
	routingQuery := normalizedQuery
	payload.CurrentUserQuery = req.Query
	cfg.Set("payload", payload)

	askQuery := normalizedQuery
	if sharedContext, err := trySharedMemoryContext(ctx, normalizedQuery, payload.UserID); err == nil && strings.TrimSpace(sharedContext) != "" {
		cfg.Set("shared_memory_context", sharedContext)
		cfg.Set("shared_memory_enabled", true)
	}

	log.Printf("daemon handleAsk: request received query=%q session=%q normalized_query=%q", req.Query, payload.SessionID, normalizedQuery)
	if hasSharedBackendAgents() {
		if strings.HasPrefix(routingQuery, "/") || !shouldUseShellFallback(routingQuery) {
			answer, err := sopbackend.AskSharedAgent(ctx, routingQuery, cfg)
			if err != nil {
				log.Printf("daemon ask request failed: query=%q error=%v", req.Query, err)
				respondJSON(w, http.StatusInternalServerError, AskResponse{Error: err.Error()})
				return
			}
			if err := promoteSharedMemoryArtifactForAsk(ctx, req.Query, answer, payload); err != nil {
				log.Printf("daemon shared-memory promotion failed: query=%q error=%v", req.Query, err)
			}
			log.Printf("daemon handleAsk: completed via shared backend handler query=%q answer_len=%d", req.Query, len(answer))
			respondJSON(w, http.StatusOK, AskResponse{Answer: answer})
			return
		}
	}
	if fallbackAnswer, handled, fallbackErr := s.trySimpleShellCommandFallback(routingQuery, sessionID); handled {
		if fallbackErr != nil {
			log.Printf("daemon shell fallback failed: query=%q error=%v", normalizedQuery, fallbackErr)
			respondJSON(w, http.StatusInternalServerError, AskResponse{Error: fallbackErr.Error()})
			return
		}
		if err := promoteSharedMemoryArtifactForAsk(ctx, req.Query, fallbackAnswer, payload); err != nil {
			log.Printf("daemon shared-memory promotion failed: query=%q error=%v", req.Query, err)
		}
		log.Printf("daemon handleAsk: completed via shell fallback query=%q answer_len=%d", normalizedQuery, len(fallbackAnswer))
		respondJSON(w, http.StatusOK, AskResponse{Answer: fallbackAnswer})
		return
	}
	ctx = sopbackend.InjectSharedRuntimeContext(ctx, nil, nil)
	if s.service != nil {
		answer, err := s.service.Ask(ctx, askQuery, cfg)
		if err != nil {
			log.Printf("daemon ask request failed: query=%q error=%v", req.Query, err)
			respondJSON(w, http.StatusInternalServerError, AskResponse{Error: err.Error()})
			return
		}
		if err := promoteSharedMemoryArtifactForAsk(ctx, req.Query, answer, payload); err != nil {
			log.Printf("daemon shared-memory promotion failed: query=%q error=%v", req.Query, err)
		}
		log.Printf("daemon handleAsk: completed via agent query=%q answer_len=%d", req.Query, len(answer))
		respondJSON(w, http.StatusOK, AskResponse{Answer: answer})
		return
	}

	answer, err := sopbackend.AskSharedAgent(ctx, askQuery, cfg)
	if err != nil {
		log.Printf("daemon ask request failed: query=%q error=%v", req.Query, err)
		respondJSON(w, http.StatusInternalServerError, AskResponse{Error: err.Error()})
		return
	}
	if err := promoteSharedMemoryArtifactForAsk(ctx, req.Query, answer, payload); err != nil {
		log.Printf("daemon shared-memory promotion failed: query=%q error=%v", req.Query, err)
	}
	log.Printf("daemon handleAsk: completed via agent query=%q answer_len=%d", req.Query, len(answer))
	respondJSON(w, http.StatusOK, AskResponse{Answer: answer})
}

func trySharedMemoryContext(ctx context.Context, query, userID string) (string, error) {
	if strings.TrimSpace(query) == "" {
		return "", nil
	}

	// Determine relevant categories based on query intent
	categories := selectRelevantCategories(query)

	// Use category-based retrieval for better context
	context, err := RetrieveFederatedContext(ctx, query, categories, 5)
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(context) == "" {
		return "", nil
	}

	return fmt.Sprintf("Relevant patterns from fleet knowledge:\n%s", context), nil
}

func promoteSharedMemoryArtifactForAsk(ctx context.Context, query, answer string, payload *ai.SessionPayload) error {
	if strings.TrimSpace(query) == "" || strings.TrimSpace(answer) == "" {
		return nil
	}

	category, confidence, shouldShare := categorizeFederationArtifact(query, answer)
	if !shouldShare {
		shouldShare = shouldPromoteFederationArtifact(query, answer, category, confidence)
	}

	metadata := map[string]any{
		"source": "daemon_ask",
	}
	if payload != nil {
		if payload.SessionID != "" {
			metadata["session_id"] = strings.TrimSpace(payload.SessionID)
		}
		if payload.UserID != "" {
			metadata["user_id"] = strings.TrimSpace(payload.UserID)
		}
	}

	artifact := FederatedArtifact{
		Category:    category,
		Summary:     fmt.Sprintf("Query: %s", truncate(query, 100)),
		Pattern:     fmt.Sprintf("Approach: %s", truncate(answer, 500)),
		Context:     "",
		Confidence:  confidence,
		ShouldShare: shouldShare,
		Metadata:    metadata,
	}

	return PromoteFederatedArtifact(ctx, artifact)
}

func sharedMemoryDatabase() (*aidb.Database, error) {
	sysOpts, err := sopbackend.GetSystemDBOptions(context.Background())
	if err != nil {
		return nil, err
	}
	return aidb.NewDatabase(sysOpts), nil
}

func looksLikeAgentRoutingQuery(query string) bool {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(lower, "sop:") ||
		strings.HasPrefix(lower, "omni:") ||
		strings.HasPrefix(lower, "omni/sop/") ||
		strings.HasPrefix(lower, "omni->sop->") ||
		lower == "sop"
}

func hasSharedBackendAgents() bool {
	for _, agentSvc := range sopbackend.GetLoadedAgents() {
		if agentSvc != nil {
			return true
		}
	}
	return false
}

func normalizeAgentQuery(query string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "/") {
		return trimmed
	}
	lower := strings.ToLower(trimmed)
	switch lower {
	case "list_tools", "list_databases", "list_stores", "switch_database", "last-tool", "last_tool",
		"create", "step", "save", "show", "parameterize", "refine", "save_as", "run",
		"insert_step", "delete_step", "update_step", "reorder_steps", "delete", "select", "add", "update", "delete_record",
		"list_scripts", "get_script_details", "save_script", "save_step", "save_last_step", "refactor_last_interaction",
		"set_verbose", "execute_script", "begin_tx", "commit_tx", "rollback_tx", "open_store", "scan", "filter", "join", "join_right", "project", "limit", "sort",
		"manage_transaction", "mint_to_space", "delete_space", "enrich_space", "update_space_config", "read_space_config",
		"vectorize_space", "vectorize_space_categories", "vectorize_space_items", "list_directory", "read_file", "write_file",
		"copy_file", "rename_file", "delete_file", "delete_directory", "create_directory", "path_exists", "stat_path",
		"search_repo_files", "grep_repo_text", "run_repo_tests", "execute_local_command", "send_email", "git_status", "git_branch",
		"git_checkout", "git_commit", "git_diff", "git_log", "git_pull", "git_push", "postgres_list_tables", "postgres_query",
		"splunk_list_saved_searches", "splunk_search", "handoff_to_avatar":
		return "/" + trimmed
	default:
		return trimmed
	}
}

func (s *localRAGService) trySimpleShellCommandFallback(query, sessionID string) (string, bool, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" || strings.HasPrefix(trimmed, "/") || looksLikeAgentRoutingQuery(trimmed) || normalizeAgentQuery(trimmed) != trimmed {
		return "", false, nil
	}
	if strings.Contains(trimmed, "\n") || strings.Contains(trimmed, "\r") {
		return "", false, nil
	}
	if !looksLikeSimpleShellCommand(trimmed) {
		return "", false, nil
	}
	if s.shellSessions == nil {
		s.shellSessions = newShellSessionState()
	}
	output, stillRunning, err := s.shellSessions.run(sessionID, trimmed, 5*time.Second)
	if err != nil {
		return "", true, err
	}
	if output == "" && stillRunning {
		return "", true, nil
	}
	if output == "" {
		return "", true, nil
	}
	return output, true, nil
}

func shouldUseShellFallback(query string) bool {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return false
	}
	if strings.ContainsAny(trimmed, "|&;<>()$`") {
		return false
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}
	first := strings.ToLower(fields[0])
	if !isKnownSimpleShellCommand(first) {
		return false
	}
	for _, field := range fields[1:] {
		if strings.HasPrefix(field, "-") {
			continue
		}
		if strings.Contains(field, " ") || strings.Contains(field, "\t") {
			return false
		}
	}
	return true
}

func looksLikeSimpleShellCommand(query string) bool {
	return shouldUseShellFallback(query)
}

func isKnownSimpleShellCommand(command string) bool {
	switch command {
	case "echo", "pwd", "ls", "cd", "cat", "head", "tail", "grep", "find", "uname", "date", "whoami", "env", "git", "python", "python3", "node", "bash", "sh", "mkdir", "rm", "cp", "mv", "touch", "open":
		return true
	default:
		return false
	}
}

func resolveConfigPath(path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Clean(filepath.Join(cwd(), path))
}

func cwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

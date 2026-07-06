package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sharedcode/sop/ai"

	"github.com/sharedcode/sop"
	sharedagent "github.com/sharedcode/sop/ai/agent"
	"github.com/sharedcode/sop/database"
	"github.com/sharedcode/sop/tools/sopbackend"
)

type ExecuteRequest struct {
	Command string `json:"command"`
}

type ExecuteResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

type UIHandoffRequest struct {
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

type UIHandoffResponse struct {
	Accepted       bool   `json:"accepted"`
	Message        string `json:"message"`
	WelcomeContext string `json:"welcome_context,omitempty"`
	Error          string `json:"error,omitempty"`
}

type daemonSessionContext struct {
	AccessToken   string
	RefreshToken  string
	HQBaseURL     string
	HQUserID      string
	HQSessionID   string
	LLMProvider   string
	LLMAPIKey     string
	LLMModel      string
	LLMURL        string
	ThinkingLevel string
	LastUpdated   time.Time
}

var daemonSessionStore = struct {
	mu   sync.Mutex
	data map[string]daemonSessionContext
}{
	data: make(map[string]daemonSessionContext),
}

var localRuntimeConfigPath string

func main() {
	port := flag.Int("port", 9090, "Port to run the local SOP daemon on")
	configPath := flag.String("config", "", "Path to the SOP daemon configuration file")
	policyMaxRequests := flag.Int("rate-limit-max-requests", 20, "Maximum local requests per session before enforcement kicks in")
	policyWindow := flag.Duration("rate-limit-window", time.Minute, "Rate limit window for local daemon requests")
	accessTokenTTL := flag.Duration("access-token-ttl", 15*time.Minute, "Access token expiry duration")
	refreshTokenTTL := flag.Duration("refresh-token-ttl", 48*time.Hour, "Refresh token expiry duration (2 days default)")
	flag.Parse()

	// Keep the daemon-specific command executor hook, but let sopbackend own the shared bootstrap.
	sharedagent.SetLocalCommandExecutor(runLocalCommand)

	// Load configuration using sopbackend
	resolvedConfig, created, err := ensureConfigPath(*configPath)
	if err != nil {
		log.Fatalf("Failed to resolve daemon config: %v", err)
	}
	if created {
		log.Printf("Created initial local daemon config at %s", resolvedConfig)
	}
	localRuntimeConfigPath = resolvedConfig

	// Load sopbackend configuration exactly like the httpserver does and initialize it once.
	policy := sharedagent.AutomationPolicy{
		Enabled:               true,
		AllowShellCommands:    true,
		AllowEmail:            false,
		RequireApproval:       true,
		DefaultTimeoutSeconds: 600,
		MaxTimeoutSeconds:     1800,
	}
	if _, err := sopbackend.InitializeRuntime(context.Background(), resolvedConfig, sopbackend.RuntimeOptions{EnableLocalAutomation: true, AutomationPolicy: &policy}, *port, nil); err != nil {
		log.Printf("Warning: backend bootstrap failed: %v", err)
	} else {
		log.Printf("Loaded daemon configuration from: %s", resolvedConfig)
	}

	// Auto-provision system DB if it doesn't exist
	cfg := sopbackend.GetConfig()
	if cfg.SystemDB != nil {
		cfg.SystemDB.IsSystem = true
		if cfg.SystemDB.Path != "" {
			dbOptionsPath := filepath.Join(cfg.SystemDB.Path, "dboptions.json")
			if _, err := os.Stat(dbOptionsPath); os.IsNotExist(err) {
				log.Printf("System DB not found at %s, auto-provisioning...", cfg.SystemDB.Path)

				// Setup the system database with standalone mode
				options := sop.DatabaseOptions{
					Type:                 sop.Standalone,
					StoresFolders:        []string{cfg.SystemDB.Path},
					RegistryHashModValue: 0, // Default for system DB
				}

				ctx := context.Background()
				if _, err := database.Setup(ctx, options); err != nil {
					log.Printf("Warning: failed to auto-provision system DB: %v", err)
				} else {
					log.Printf("System DB auto-provisioned at %s", cfg.SystemDB.Path)
				}
			}
		}
	}

	// Initialize daemon authentication with JWT
	authConfig := DaemonAuthConfig{
		AccessTokenTTL:  *accessTokenTTL,
		RefreshTokenTTL: *refreshTokenTTL,
	}
	authHandler, err := NewDaemonAuthHandler(authConfig)
	if err != nil {
		log.Fatalf("Failed to initialize daemon authentication: %v", err)
	}
	log.Printf("Daemon authentication initialized (access_ttl=%v, refresh_ttl=%v)", *accessTokenTTL, *refreshTokenTTL)

	// Disable sopbackend's auth since we handle it ourselves
	sopbackend.SetDisableAuth(true)

	// Load model catalog from sopbackend
	catalog := sopbackend.GetModelCatalog()
	log.Printf("Loaded model catalog: %d LLM groups, %d Embedder groups", len(catalog.LLM), len(catalog.Embedder))

	// Initialize local session registry for rate limiting
	localRegistry := NewSessionRegistry(SubscriptionPolicy{MaxRequests: *policyMaxRequests, Window: *policyWindow})

	// Initialize the shared RAG service for daemon asks.
	var ragSvc *localRAGService
	rag, err := newLocalRAGService(resolvedConfig)
	if err != nil {
		log.Printf("warning: shared RAG runtime unavailable: %v", err)
	} else {
		ragSvc = rag
	}

	// Seed initial federated content for fleet-wide intelligence
	log.Printf("Seeding initial federated content...")
	if err := SeedInitialFederatedContent(context.Background()); err != nil {
		log.Printf("Warning: failed to seed federated content: %v", err)
	} else {
		log.Printf("Federated content seeded successfully")
	}

	// Register all sopbackend handlers (provides full backend API)
	mux := http.NewServeMux()
	sopbackend.RegisterAllHandlers(mux)
	sopbackend.RegisterMonitorabilityHandlers(mux)

	// Register daemon authentication endpoints (NO auth required)
	mux.HandleFunc("/api/daemon/auth", handleOptionsAndCORS(http.HandlerFunc(authHandler.HandleBootstrap)))
	mux.HandleFunc("/api/daemon/refresh", handleOptionsAndCORS(http.HandlerFunc(authHandler.HandleRefresh)))
	mux.HandleFunc("/api/daemon/hq-auth", handleOptionsAndCORS(http.HandlerFunc(authHandler.HandleHQAuth)))

	// Override/Add daemon-specific handlers WITH authentication
	mux.HandleFunc("/api/execute", handleOptionsAndCORS(authHandler.RequireAuth(withRateLimit(localRegistry, http.HandlerFunc(executeHandler)))))
	mux.HandleFunc("/api/handoff", handleOptionsAndCORS(authHandler.RequireAuth(withRateLimit(localRegistry, http.HandlerFunc(handleUIHandoff)))))

	// Override /api/ask to use the shared RAG implementation with authentication.
	mux.HandleFunc("/api/ask", handleOptionsAndCORS(authHandler.RequireAuth(withRateLimit(localRegistry, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ragSvc == nil {
			respondJSON(w, http.StatusServiceUnavailable, AskResponse{Error: "official RAG is not configured"})
			return
		}
		ragSvc.handleAsk(w, r)
	})))))

	go startHQSyncLoop(authHandler)

	// Wrap all handlers with request logging and CORS support
	loggingHandler := addLoggingToAll(mux)
	corsHandler := addCORSToAll(loggingHandler)

	addr := buildListenAddr("127.0.0.1", *port)
	log.Printf("Starting SOP Daemon on %s", addr)
	log.Printf("Authentication: JWT with bootstrap secret")
	log.Printf("Token expiry: access=%v, refresh=%v", *accessTokenTTL, *refreshTokenTTL)
	log.Printf("Auth endpoints: /api/daemon/auth, /api/daemon/refresh")
	log.Printf("Full backend API available (via sopbackend)")
	if ragSvc != nil {
		log.Printf("Official RAG endpoint enabled at /api/ask (authenticated)")
	} else {
		log.Printf("Official RAG endpoint disabled")
	}
	log.Printf("Daemon-specific endpoints: /api/execute, /api/handoff (authenticated)")
	log.Printf("Waiting for UI authentication...")

	if err := http.ListenAndServe(addr, sopbackend.NewObservabilityHandler(corsHandler)); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func addLoggingToAll(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
		isDBOptions := r.URL.Path == "/api/db/options"
		if isDBOptions {
			log.Printf("[daemon-dboptions] request database=%q from=%s auth=%t", r.URL.Query().Get("database"), r.RemoteAddr, authHeader != "")
		}
		log.Printf("[daemon-request] %s %s from=%s auth=%t", r.Method, r.URL.RequestURI(), r.RemoteAddr, authHeader != "")
		handler.ServeHTTP(wrapped, r)
		if isDBOptions {
			log.Printf("[daemon-dboptions] completed database=%q status=%d duration=%s", r.URL.Query().Get("database"), wrapped.statusCode, time.Since(start))
		}
		log.Printf("[daemon-request] completed %s %s status=%d duration=%s", r.Method, r.URL.RequestURI(), wrapped.statusCode, time.Since(start))
	})
}

func setLocalCORSHeaders(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")

	// Only allow localhost origins for security
	if origin != "" &&
		(strings.HasPrefix(origin, "http://localhost") ||
			strings.HasPrefix(origin, "http://127.0.0.1") ||
			strings.HasPrefix(origin, "https://localhost") ||
			strings.HasPrefix(origin, "https://127.0.0.1")) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		requestedHeaders := strings.TrimSpace(r.Header.Get("Access-Control-Request-Headers"))
		if requestedHeaders != "" {
			w.Header().Set("Access-Control-Allow-Headers", requestedHeaders)
		} else {
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-LLM-Provider, X-LLM-API-Key, X-LLM-URL, X-LLM-Model, X-Embedder-Provider, X-Embedder-API-Key, X-Embedder-URL, X-Embedder-Model")
		}
		return true
	}

	if origin != "" {
		http.Error(w, "Forbidden: daemon only accepts localhost origins", http.StatusForbidden)
		return false
	}

	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-LLM-Provider, X-LLM-API-Key, X-LLM-URL, X-LLM-Model, X-Embedder-Provider, X-Embedder-API-Key, X-Embedder-URL, X-Embedder-Model")
	return true
}

// addCORSToAll wraps the handler with CORS support for localhost only
func addCORSToAll(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !setLocalCORSHeaders(w, r) {
			return
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		handler.ServeHTTP(w, r)
	})
}

func handleOptionsAndCORS(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !setLocalCORSHeaders(w, r) {
			return
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		h(w, r)
	}
}

type localCommandSession struct {
	workingDir string
	lastCmd    *exec.Cmd
}

var localCommandSessionState = struct {
	mu       sync.Mutex
	sessions map[string]*localCommandSession
}{
	sessions: make(map[string]*localCommandSession),
}

func runLocalCommand(ctx context.Context, req sharedagent.LocalCommandRequest) (string, error) {
	result, err := runLocalCommandWithWorkingDir(ctx, req.Command, req.Reasoning, req.WorkingDir, req.TimeoutSeconds, req.SessionID)
	if err == nil && req.StreamOutput != nil {
		req.StreamOutput(result)
	}
	return result, err
}

func runLocalCommandWithWorkingDir(ctx context.Context, command, reasoning, workingDir string, timeoutSeconds int, sessionID string) (string, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 600
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	if strings.TrimSpace(command) == "" {
		return `{"stdout":"","stderr":"","exit_code":0}`, nil
	}

	trimmedCommand := strings.TrimSpace(command)
	if strings.HasPrefix(trimmedCommand, "cd") || strings.EqualFold(trimmedCommand, "cd") {
		if sessionID != "" {
			localCommandSessionState.mu.Lock()
			sessionState := localCommandSessionState.sessions[sessionID]
			if sessionState == nil {
				sessionState = &localCommandSession{}
				localCommandSessionState.sessions[sessionID] = sessionState
			}
			if sessionState.workingDir == "" {
				sessionState.workingDir = workingDir
				if sessionState.workingDir == "" {
					sessionState.workingDir = "."
				}
			}
			localCommandSessionState.mu.Unlock()
		}
		if strings.EqualFold(trimmedCommand, "cd") || strings.TrimSpace(strings.TrimPrefix(trimmedCommand, "cd")) == "" {
			if sessionID != "" {
				localCommandSessionState.mu.Lock()
				sessionState := localCommandSessionState.sessions[sessionID]
				if sessionState != nil {
					if home, err := os.UserHomeDir(); err == nil {
						sessionState.workingDir = home
					} else {
						sessionState.workingDir = "."
					}
				}
				localCommandSessionState.mu.Unlock()
			}
			return `{"stdout":"","stderr":"","exit_code":0}`, nil
		}
		if sessionID != "" {
			localCommandSessionState.mu.Lock()
			sessionState := localCommandSessionState.sessions[sessionID]
			if sessionState != nil {
				target := strings.TrimSpace(strings.TrimPrefix(trimmedCommand, "cd"))
				if target == "~" {
					if home, err := os.UserHomeDir(); err == nil {
						target = home
					}
				}
				if !filepath.IsAbs(target) {
					target = filepath.Join(sessionState.workingDir, target)
				}
				if absPath, err := filepath.Abs(target); err == nil {
					sessionState.workingDir = absPath
				}
			}
			localCommandSessionState.mu.Unlock()
		}
		return `{"stdout":"","stderr":"","exit_code":0}`, nil
	}

	if strings.EqualFold(trimmedCommand, "pwd") {
		workingDirForPwd := workingDir
		if sessionID != "" {
			localCommandSessionState.mu.Lock()
			if sessionState := localCommandSessionState.sessions[sessionID]; sessionState != nil && sessionState.workingDir != "" {
				workingDirForPwd = sessionState.workingDir
			}
			localCommandSessionState.mu.Unlock()
		}
		if workingDirForPwd == "" {
			workingDirForPwd = "."
		}
		return fmt.Sprintf(`{"stdout":"%s\n","stderr":"","exit_code":0}`, strings.ReplaceAll(workingDirForPwd, "\n", "\\n")), nil
	}

	if sessionID != "" {
		localCommandSessionState.mu.Lock()
		if existing, ok := localCommandSessionState.sessions[sessionID]; ok && existing != nil {
			if workingDir == "" && existing.workingDir != "" {
				workingDir = existing.workingDir
			}
		}
		localCommandSessionState.mu.Unlock()
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	if workingDir != "" {
		cmd.Dir = workingDir
	} else if sessionID != "" {
		localCommandSessionState.mu.Lock()
		if existing, ok := localCommandSessionState.sessions[sessionID]; ok && existing != nil {
			cmd.Dir = existing.workingDir
		}
		localCommandSessionState.mu.Unlock()
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if sessionID != "" {
		localCommandSessionState.mu.Lock()
		sessionState := localCommandSessionState.sessions[sessionID]
		if sessionState == nil {
			sessionState = &localCommandSession{}
			localCommandSessionState.sessions[sessionID] = sessionState
		}
		if workingDir != "" {
			sessionState.workingDir = workingDir
		} else if sessionState.workingDir == "" {
			sessionState.workingDir = cmd.Dir
		}
		sessionState.lastCmd = cmd
		localCommandSessionState.mu.Unlock()
	}
	resp := ExecuteResponse{Stdout: stdout.String(), Stderr: stderr.String()}
	if streamer, ok := ctx.Value(ai.CtxKeyEventStreamer).(func(string, any)); ok && streamer != nil {
		streamer("content", fmt.Sprintf("🛠️ Result from local_command:\n%s", strings.TrimSpace(resp.Stdout)))
	}
	if err != nil {
		resp.Error = err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			resp.ExitCode = exitErr.ExitCode()
		} else {
			resp.ExitCode = -1
		}
	} else {
		resp.ExitCode = 0
	}

	payload, marshalErr := json.Marshal(resp)
	if marshalErr != nil {
		return "", marshalErr
	}
	return string(payload), nil
}

func startHQSyncLoop(authHandler *DaemonAuthHandler) {
	if authHandler == nil {
		return
	}
	for {
		time.Sleep(nextHQSyncDelay(1*time.Hour, 15*time.Minute, time.Now().UnixNano()))
		syncErr := authHandler.syncWithHQ(context.Background())
		if syncErr != nil {
			log.Printf("HQ sync failed: %v", syncErr)
		}
	}
}

func nextHQSyncDelay(baseInterval, jitter time.Duration, seed int64) time.Duration {
	if baseInterval <= 0 {
		baseInterval = time.Hour
	}
	if jitter < 0 {
		jitter = 0
	}
	if jitter == 0 {
		return baseInterval
	}
	rng := rand.New(rand.NewSource(seed))
	return baseInterval + time.Duration(rng.Int63n(int64(jitter)))
}

func handleUIHandoff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req UIHandoffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, UIHandoffResponse{Error: "Invalid JSON body: " + err.Error()})
		return
	}

	if strings.TrimSpace(req.Query) == "" {
		respondJSON(w, http.StatusBadRequest, UIHandoffResponse{Error: "Query cannot be empty"})
		return
	}

	accessToken := strings.TrimSpace(req.Auth.AccessToken)
	refreshToken := strings.TrimSpace(req.Auth.RefreshToken)
	if accessToken == "" && refreshToken == "" {
		log.Printf("Received local handoff without auth tokens; accepting for local runtime")
	}

	hqBaseURL := strings.TrimSpace(req.HQ.BaseURL)
	if hqBaseURL == "" {
		respondJSON(w, http.StatusBadRequest, UIHandoffResponse{Error: "HQ base URL is required"})
		return
	}

	ctxKey := strings.TrimSpace(req.HQ.SessionID)
	if ctxKey == "" {
		ctxKey = strings.TrimSpace(req.HQ.UserID)
	}
	if ctxKey == "" {
		ctxKey = "default"
	}

	daemonSessionStore.mu.Lock()
	daemonSessionStore.data[ctxKey] = daemonSessionContext{
		AccessToken:   accessToken,
		RefreshToken:  refreshToken,
		HQBaseURL:     hqBaseURL,
		HQUserID:      strings.TrimSpace(req.HQ.UserID),
		HQSessionID:   strings.TrimSpace(req.HQ.SessionID),
		LLMProvider:   strings.TrimSpace(req.LLMProvider),
		LLMAPIKey:     strings.TrimSpace(req.LLMAPIKey),
		LLMModel:      strings.TrimSpace(req.LLMModel),
		LLMURL:        strings.TrimSpace(req.LLMURL),
		ThinkingLevel: strings.TrimSpace(req.ThinkingLevel),
		LastUpdated:   time.Now().UTC(),
	}
	daemonSessionStore.mu.Unlock()

	log.Printf("Accepted UI handoff for session %s", ctxKey)
	respondJSON(w, http.StatusOK, UIHandoffResponse{
		Accepted:       true,
		Message:        "handoff accepted",
		WelcomeContext: buildLocalRAGWelcomeContext(),
	})
}

func buildLocalRAGWelcomeContext() string {
	return "Hello! I am your local <strong>Daemon Assistant</strong>. I'm here to help you work with the local shell and embedded RAG runtime directly from this desktop experience.<br><br>I can help you:<br><ul style=\"color: var(--text-muted); font-size: 0.9em; margin: 0 0 8px 18px; padding: 0; list-style-position: outside;\"><li style=\"margin: 0 0 4px 0; padding: 0;\">run shell commands and inspect command output</li><li style=\"margin: 0 0 4px 0; padding: 0;\">answer questions using the local RAG knowledge you configured</li><li style=\"margin: 0 0 4px 0; padding: 0;\">help you troubleshoot local workflows from the command line</li></ul>Type <strong>/list_tools</strong> to see the built-in SOP commands available in this session.<br><br><em style=\"color: var(--text-muted); font-size: 0.9em; display: block; margin-bottom: 4px;\">Shell-style commands such as <strong>ls</strong>, <strong>pwd</strong>, and <strong>cd /Volumes</strong> can be run directly in the local shell, and slash-prefixed commands such as <strong>/list_tools</strong> are available for built-in SOP actions.</em><br><em style=\"color: var(--text-muted); font-size: 0.9em; display: block; margin-bottom: 4px;\">Try asking things like:</em><ul style=\"color: var(--text-muted); font-size: 0.9em; margin: 0 0 0 18px; padding: 0; list-style-position: outside;\"><li style=\"margin: 0 0 2px 0; padding: 0;\">\"Show me the current directory and files\"</li><li style=\"margin: 0 0 2px 0; padding: 0;\">\"Explain how to run the local daemon\"</li><li style=\"margin: 0 0 2px 0; padding: 0;\">\"Help me inspect my local project structure\"</li></ul>"
}

func executeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, ExecuteResponse{Error: "Invalid JSON body: " + err.Error()})
		return
	}

	if req.Command == "" {
		respondJSON(w, http.StatusBadRequest, ExecuteResponse{Error: "Command cannot be empty"})
		return
	}

	log.Printf("Executing command: %s", req.Command)

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", req.Command)
	} else {
		cmd = exec.Command("sh", "-c", req.Command)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	resp := ExecuteResponse{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if err != nil {
		resp.Error = err.Error()
		if exitError, ok := err.(*exec.ExitError); ok {
			resp.ExitCode = exitError.ExitCode()
		} else {
			resp.ExitCode = -1
		}
		log.Printf("Command finished with error: %v", err)
	} else {
		resp.ExitCode = 0
		log.Printf("Command executed successfully")
	}

	respondJSON(w, http.StatusOK, resp)
}

func buildListenAddr(host string, port int) string {
	if host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func respondJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}

func withRateLimit(reg *SessionRegistry, next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := strings.TrimSpace(r.Header.Get("X-SOP-Session-ID"))
		if sessionID == "" {
			sessionID = strings.TrimSpace(r.URL.Query().Get("session_id"))
		}
		if sessionID == "" {
			// Allow requests without an explicit session to still work, but use a simple fallback session key.
			sessionID = "default"
		}
		sess, err := reg.RegisterSession("local-host", sessionID)
		if err != nil {
			respondJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit unavailable"})
			return
		}
		if err := reg.ConsumeRequest(sess.ID); err != nil {
			respondJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
			return
		}
		next.ServeHTTP(w, r)
	}
}

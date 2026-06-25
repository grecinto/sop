package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

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
}

type UIHandoffResponse struct {
	Accepted       bool   `json:"accepted"`
	Message        string `json:"message"`
	WelcomeContext string `json:"welcome_context,omitempty"`
	Error          string `json:"error,omitempty"`
}

type daemonSessionContext struct {
	AccessToken  string
	RefreshToken string
	HQBaseURL    string
	HQUserID     string
	HQSessionID  string
	LastUpdated  time.Time
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

	// Set local command executor for agent tools
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

	// Load sopbackend configuration exactly like the httpserver does
	if _, err := os.Stat(resolvedConfig); err == nil {
		if err := sopbackend.LoadConfig(resolvedConfig); err != nil {
			log.Printf("Warning: could not load sopbackend config: %v", err)
		} else {
			sopbackend.SetConfigFile(resolvedConfig)
			if _, err := sopbackend.LoadModelCatalog(resolvedConfig); err != nil {
				log.Printf("Warning: could not load model catalog: %v", err)
			}
			log.Printf("Loaded daemon configuration from: %s", resolvedConfig)
		}
	} else if _, err := sopbackend.LoadModelCatalog(""); err != nil {
		log.Printf("Warning: could not load model catalog: %v", err)
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

	// Initialize agents from sopbackend exactly like httpserver does
	if len(cfg.Databases) > 0 || cfg.SystemDB != nil {
		if err := sopbackend.InitAgents(context.Background()); err != nil {
			log.Printf("Warning: agent initialization failed: %v", err)
		}
	}

	// Load model catalog from sopbackend
	catalog := sopbackend.GetModelCatalog()
	log.Printf("Loaded model catalog: %d LLM groups, %d Embedder groups", len(catalog.LLM), len(catalog.Embedder))

	// Initialize local session registry for rate limiting
	localRegistry := NewSessionRegistry(SubscriptionPolicy{MaxRequests: *policyMaxRequests, Window: *policyWindow})

	// Initialize local RAG service (daemon-specific)
	var ragSvc *localRAGService
	rag, err := newLocalRAGService(resolvedConfig)
	if err != nil {
		log.Printf("warning: local RAG runtime disabled: %v", err)
	} else {
		ragSvc = rag
	}

	// Register all sopbackend handlers (provides full backend API)
	mux := http.NewServeMux()
	sopbackend.RegisterAllHandlers(mux)

	// Register daemon authentication endpoints (NO auth required)
	mux.HandleFunc("/api/daemon/auth", handleOptionsAndCORS(http.HandlerFunc(authHandler.HandleBootstrap)))
	mux.HandleFunc("/api/daemon/refresh", handleOptionsAndCORS(http.HandlerFunc(authHandler.HandleRefresh)))

	// Override/Add daemon-specific handlers WITH authentication
	mux.HandleFunc("/api/execute", handleOptionsAndCORS(authHandler.RequireAuth(withRateLimit(localRegistry, http.HandlerFunc(executeHandler)))))
	mux.HandleFunc("/api/handoff", handleOptionsAndCORS(authHandler.RequireAuth(withRateLimit(localRegistry, http.HandlerFunc(handleUIHandoff)))))

	// Override /api/ask with local RAG implementation WITH authentication
	mux.HandleFunc("/api/ask", handleOptionsAndCORS(authHandler.RequireAuth(withRateLimit(localRegistry, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ragSvc == nil {
			respondJSON(w, http.StatusServiceUnavailable, AskResponse{Error: "local RAG is not configured"})
			return
		}
		ragSvc.handleAsk(w, r)
	})))))

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
		log.Printf("Local RAG endpoint enabled at /api/ask (authenticated)")
	} else {
		log.Printf("Local RAG endpoint disabled")
	}
	log.Printf("Daemon-specific endpoints: /api/execute, /api/handoff (authenticated)")
	log.Printf("Waiting for UI authentication...")

	if err := http.ListenAndServe(addr, corsHandler); err != nil {
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

func runLocalCommand(ctx context.Context, command, reasoning string) (string, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	resp := ExecuteResponse{Stdout: stdout.String(), Stderr: stderr.String()}
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
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		HQBaseURL:    hqBaseURL,
		HQUserID:     strings.TrimSpace(req.HQ.UserID),
		HQSessionID:  strings.TrimSpace(req.HQ.SessionID),
		LastUpdated:  time.Now().UTC(),
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
	return "You are now connected to the local SOP daemon runtime. This experience is responsible for local shell access, local RAG answers, and direct workflow help on this machine. The daemon owns the local execution context, while the HQ connection details are carried along for identity and continuity."
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

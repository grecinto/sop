package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	sharedagent "github.com/sharedcode/sop/ai/agent"
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

	// Load sopbackend configuration
	if err := sopbackend.LoadConfig(resolvedConfig); err != nil {
		log.Printf("Warning: could not load sopbackend config: %v", err)
	}

	// Disable authentication for local daemon (it's local-only, no auth needed)
	sopbackend.SetDisableAuth(true)

	// Initialize agents from sopbackend
	if err := sopbackend.InitAgents(context.Background()); err != nil {
		log.Printf("Warning: agent initialization failed: %v", err)
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

	// Wrap all handlers with CORS support
	corsHandler := addCORSToAll(mux)

	// Override/Add daemon-specific handlers on the mux (before CORS wrapping)
	mux.HandleFunc("/api/execute", handleOptionsAndCORS(withRateLimit(localRegistry, http.HandlerFunc(executeHandler))))
	mux.HandleFunc("/api/handoff", handleOptionsAndCORS(withRateLimit(localRegistry, http.HandlerFunc(handleUIHandoff))))

	// Override /api/ask with local RAG implementation
	mux.HandleFunc("/api/ask", handleOptionsAndCORS(withRateLimit(localRegistry, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ragSvc == nil {
			respondJSON(w, http.StatusServiceUnavailable, AskResponse{Error: "local RAG is not configured"})
			return
		}
		ragSvc.handleAsk(w, r)
	}))))

	addr := buildListenAddr("127.0.0.1", *port)
	log.Printf("Starting SOP Desktop Daemon on %s", addr)
	log.Printf("Full backend API available (via sopbackend)")
	if ragSvc != nil {
		log.Printf("Local RAG endpoint enabled at /api/ask")
	} else {
		log.Printf("Local RAG endpoint disabled")
	}
	log.Printf("Daemon-specific endpoints: /api/execute, /api/handoff")
	log.Printf("Waiting for requests from SOP UI...")

	if err := http.ListenAndServe(addr, corsHandler); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// addCORSToAll wraps the handler with CORS support for all requests
func addCORSToAll(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow CORS from any origin (since UI might be hosted anywhere)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		handler.ServeHTTP(w, r)
	})
}

func handleOptionsAndCORS(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Allow CORS from any origin (since UI might be hosted anywhere, e.g. sop.yourdomain.com)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

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

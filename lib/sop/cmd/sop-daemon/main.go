package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sharedcode/sop/ai"

	"github.com/sharedcode/sop"
	sharedagent "github.com/sharedcode/sop/ai/agent"
	aidb "github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/database"
	sopfs "github.com/sharedcode/sop/fs"
	"github.com/sharedcode/sop/tools/sopbackend"
)

var (
	globalSyncQueue *SyncQueue
	syncQueueOnce   sync.Once
)

type runtimeVectorizationFailurePolicyHandle interface {
	aidb.VectorizationFailurePolicy
	RecoveryState() runtimeVectorizationRecoveryState
	RecoveryStates() []runtimeVectorizationRecoveryState
	NeedsRecovery() bool
	MarkIgnored(kbName, targetType, targetID string)
	MarkScheduled(kbName, targetType, targetID string)
	MarkAbandoned(kbName, targetType, targetID string)
	MarkRecovered(kbName, targetType, targetID string)
	RecordRetryAttempt(kbName, targetType, targetID string)
	RecordStoragePathFailure(operation, targetPath string, err error)
	StoragePathRecoveryState() runtimeDataPathRecoveryState
	StoragePathRecoveryStates() []runtimeDataPathRecoveryState
}

var activeRuntimeVectorizationFailurePolicy runtimeVectorizationFailurePolicyHandle

const vectorizationRecoveryRetryBackoff = 5 * time.Minute

var vectorizationResetExecutor = func(ctx context.Context, kbName string) error {
	dbOpts, err := sopbackend.GetDBOptions(ctx, "")
	if err != nil {
		dbOpts, err = sopbackend.GetSystemDBOptions(ctx)
	}
	if err != nil {
		return fmt.Errorf("resolve database options for semantic index reset: %w", err)
	}

	db := aidb.NewDatabase(dbOpts)
	return db.ResetSemanticIndex(ctx, kbName)
}

var vectorizationRecoveryExecutor = func(ctx context.Context, kbName, targetType, targetID string) error {
	if strings.TrimSpace(kbName) == "" {
		return fmt.Errorf("kb name is required")
	}

	dbOpts, err := sopbackend.GetDBOptions(ctx, "")
	if err != nil {
		dbOpts, err = sopbackend.GetSystemDBOptions(ctx)
	}
	if err != nil {
		return fmt.Errorf("resolve database options for recovery: %w", err)
	}

	db := aidb.NewDatabase(dbOpts)
	embedder := sopbackend.GetConfiguredEmbedder(nil)
	llm := sopbackend.GetConfiguredLLM(nil)
	if embedder == nil {
		return fmt.Errorf("embedder unavailable for recovery")
	}

	switch strings.ToLower(strings.TrimSpace(targetType)) {
	case "category", "categories":
		if parsedID, err := sop.ParseUUID(targetID); err == nil && parsedID != sop.NilUUID {
			return db.VectorizeCategories(ctx, kbName, llm, embedder, 50, []sop.UUID{parsedID})
		}
		return db.Vectorize(ctx, kbName, llm, embedder, 50)
	case "item", "items":
		return db.Vectorize(ctx, kbName, llm, embedder, 50)
	default:
		return db.Vectorize(ctx, kbName, llm, embedder, 50)
	}
}

func configureRecoveryStatePersistence(cfg sopbackend.Config) func(path string) error {
	sopfs.SetRecoveryStateCleanupPath(cfg.VectorizationRecoveryStoragePath)
	updater := func(path string) error {
		if strings.TrimSpace(path) == "" {
			return nil
		}
		currentCfg := sopbackend.GetConfig()
		currentCfg.VectorizationRecoveryStoragePath = path
		sopbackend.SetConfig(currentCfg)
		return sopbackend.SaveConfig(currentCfg.ConfigFile, currentCfg)
	}
	sopfs.SetRecoveryStateConfigUpdater(updater)
	return updater
}

func resolveVectorizationRecoveryStateDir(configPath string, cfg sopbackend.Config) string {
	candidates := []string{}
	if cfg.VectorizationRecoveryStoragePath != "" {
		candidates = append(candidates, cfg.VectorizationRecoveryStoragePath)
	}
	if strings.TrimSpace(configPath) != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(configPath), "state"))
	}
	candidates = append(candidates, filepath.Join(os.TempDir(), "sop", "state"))
	candidates = append(candidates, filepath.Join(filepath.Dir(configPath), "state"))

	for _, candidate := range candidates {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		info, err := os.Stat(trimmed)
		if err == nil {
			if info.IsDir() {
				return trimmed
			}
			continue
		}
		if os.IsNotExist(err) {
			if parent := filepath.Dir(trimmed); parent != trimmed {
				if err := os.MkdirAll(trimmed, 0o755); err == nil {
					return trimmed
				}
			}
		}
	}

	if strings.TrimSpace(configPath) != "" {
		fallback := filepath.Join(filepath.Dir(configPath), "state")
		if err := os.MkdirAll(fallback, 0o755); err == nil {
			return fallback
		}
	}
	return filepath.Join(os.TempDir(), "sop", "state")
}

func shouldPrintHelp(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--h" || arg == "-help" || arg == "--help" {
			return true
		}
	}
	return false
}

func printDaemonHelpTo(w io.Writer) {
	resolvedPath, err := resolveDaemonConfigLocation("", nil)
	if err != nil {
		resolvedPath = ""
	}
	if strings.TrimSpace(resolvedPath) != "" {
		if _, statErr := os.Stat(resolvedPath); statErr != nil {
			resolvedPath = ""
		}
	}

	var configSection string
	if strings.TrimSpace(resolvedPath) != "" {
		configSection = fmt.Sprintf("Config file:\n  %s\n\n", resolvedPath)
	} else {
		configSection = "Config file:\n  (not created yet)\n\n"
	}

	fmt.Fprintf(w, `SOP daemon

Usage:
  sop-daemon [flags]

%sFlags:
  -config string
        Path to the SOP daemon configuration file. If omitted, the daemon will look in the user config directory and fall back to the current working directory.
  -print-config-path
        Print the resolved daemon config path and exit.
  -port int
        Port to run the local SOP daemon on (default 9090)
  -rate-limit-max-requests int
        Maximum local requests per session before enforcement kicks in (default 20)
  -rate-limit-window duration
        Rate limit window for local daemon requests (default 1m0s)
  -access-token-ttl duration
        Access token expiry duration (default 15m0s)
  -refresh-token-ttl duration
        Refresh token expiry duration (2 days default) (default 48h0m0s)

Examples:
  sop-daemon --print-config-path
  sop-daemon --config <path>
  sop-daemon --help
`, configSection)
}

func printDaemonHelp() {
	printDaemonHelpTo(os.Stdout)
}

// GetGlobalSyncQueue returns the global sync queue instance.
func GetGlobalSyncQueue() *SyncQueue {
	syncQueueOnce.Do(func() {
		globalSyncQueue = NewSyncQueue()
	})
	return globalSyncQueue
}

type ExecuteRequest struct {
	Command        string `json:"command"`
	WorkingDir     string `json:"working_dir,omitempty"`
	Reasoning      string `json:"reasoning,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	ExecutionMode  string `json:"execution_mode,omitempty"` // "background" or "terminal"
}

type ExecuteResponse struct {
	Stdout             string `json:"stdout"`
	Stderr             string `json:"stderr"`
	ExitCode           int    `json:"exit_code"`
	Error              string `json:"error,omitempty"`
	Cancelled          bool   `json:"cancelled,omitempty"` // If true, command was cancelled by user
	LaunchedInTerminal bool   `json:"launched_in_terminal,omitempty"`
	Suggestion         string `json:"suggestion,omitempty"`
	Streaming          bool   `json:"streaming,omitempty"` // If true, output streamed separately

	// Phase 2 Enhancement: Process info
	PID int `json:"pid,omitempty"` // Process ID for signal control

	// Phase 3 Enhancement: Background job info
	Background bool   `json:"background,omitempty"` // If true, running in background
	JobID      string `json:"job_id,omitempty"`     // Unique job identifier
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
	Accepted            bool                              `json:"accepted"`
	Message             string                            `json:"message"`
	WelcomeContext      string                            `json:"welcome_context,omitempty"`
	Error               string                            `json:"error,omitempty"`
	NeedsRecovery       bool                              `json:"needs_recovery,omitempty"`
	Recovery            runtimeVectorizationRecoveryState `json:"recovery,omitempty"`
	StoragePathRecovery runtimeDataPathRecoveryState      `json:"storage_path_recovery,omitempty"`
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

const daemonVerboseLoggingEnv = "SOP_DAEMON_VERBOSE"

func shouldLogVerbose() bool {
	value := strings.TrimSpace(os.Getenv(daemonVerboseLoggingEnv))
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func logVerbose(format string, args ...any) {
	if shouldLogVerbose() {
		log.Printf(format, args...)
	}
}

func main() {
	port := flag.Int("port", 9090, "Port to run the local SOP daemon on")
	configPath := flag.String("config", "", "Path to the SOP daemon configuration file")
	printConfigPath := flag.Bool("print-config-path", false, "Print the resolved daemon config path and exit")
	policyMaxRequests := flag.Int("rate-limit-max-requests", 20, "Maximum local requests per session before enforcement kicks in")
	policyWindow := flag.Duration("rate-limit-window", time.Minute, "Rate limit window for local daemon requests")
	accessTokenTTL := flag.Duration("access-token-ttl", 15*time.Minute, "Access token expiry duration")
	refreshTokenTTL := flag.Duration("refresh-token-ttl", 48*time.Hour, "Refresh token expiry duration (2 days default)")
	if shouldPrintHelp(os.Args[1:]) {
		printDaemonHelp()
		return
	}

	flag.CommandLine.Init(flag.CommandLine.Name(), flag.ContinueOnError)
	flag.CommandLine.SetOutput(os.Stdout)
	if err := flag.CommandLine.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printDaemonHelp()
			return
		}
		log.Fatalf("Failed to parse flags: %v", err)
	}

	// Keep the daemon-specific command executor hook, but let sopbackend own the shared bootstrap.
	sharedagent.SetLocalCommandExecutor(runLocalCommand)

	// Phase 2: Signal control
	sharedagent.SetLocalSendSignalToProcess(sendSignalToProcess)

	// Phase 3: Background job control
	sharedagent.SetLocalGetBackgroundJob(func(jobID string) (*sharedagent.BackgroundJob, error) {
		job, ok := getBackgroundJob(jobID)
		if !ok {
			return nil, fmt.Errorf("job not found: %s", jobID)
		}

		job.mu.Lock()
		defer job.mu.Unlock()

		return &sharedagent.BackgroundJob{
			JobID:     job.jobID,
			Command:   job.command,
			PID:       job.pid,
			StartTime: job.startTime,
			Stdout:    job.stdout,
			Stderr:    job.stderr,
			ExitCode:  job.exitCode,
			Error:     job.error,
		}, nil
	})
	sharedagent.SetLocalKillBackgroundJob(killBackgroundJob)

	if *printConfigPath {
		if _, err := resolveDaemonConfigLocation(*configPath, os.Stdout); err != nil {
			log.Fatalf("Failed to resolve daemon config: %v", err)
		}
		return
	}

	// Load configuration using sopbackend
	resolvedConfig, created, err := ensureConfigPath(*configPath)
	if err != nil {
		log.Fatalf("Failed to resolve daemon config: %v", err)
	}
	if created {
		log.Printf("Created initial local daemon config at %s", resolvedConfig)
	}
	localRuntimeConfigPath = resolvedConfig

	stateDir := resolveVectorizationRecoveryStateDir(resolvedConfig, sopbackend.GetConfig())
	policyImpl := newRuntimeVectorizationFailurePolicy(stateDir)
	activeRuntimeVectorizationFailurePolicy = policyImpl
	aidb.RegisterVectorizationFailurePolicy(policyImpl)
	sop.RegisterStoragePathFailureHandler(func(operation, targetPath string, err error) {
		if activeRuntimeVectorizationFailurePolicy != nil {
			activeRuntimeVectorizationFailurePolicy.RecordStoragePathFailure(operation, targetPath, err)
		}
	})
	sopbackend.RegisterBackgroundServiceHook(func(ctx context.Context) {
		if activeRuntimeVectorizationFailurePolicy == nil {
			return
		}
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if count := retryPendingVectorizationRecovery(ctx, false); count > 0 {
			log.Printf("auto-retried %d pending vectorization recovery target(s)", count)
		}
	})

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
	cfg := sopbackend.GetConfig()
	configureRecoveryStatePersistence(cfg)

	// Auto-provision system DB if it doesn't exist
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

	// Initialize privacy configuration and filter
	daemonDataDir := filepath.Dir(resolvedConfig)
	if err := initPrivacyConfig(daemonDataDir); err != nil {
		log.Printf("Warning: failed to initialize privacy config: %v", err)
	} else {
		log.Printf("Privacy configuration initialized")
	}
	if err := initPrivacyFilter(daemonDataDir); err != nil {
		log.Printf("Warning: failed to initialize privacy filter: %v", err)
	} else {
		settings := getPrivacySettings()
		log.Printf("Privacy filter initialized (fleet_learning=%v, policy=%s)",
			settings.GlobalShareEnabled, settings.PolicyType)
	}

	// Seed initial federated content for fleet-wide intelligence
	log.Printf("Seeding initial federated content...")
	if err := SeedInitialFederatedContent(context.Background()); err != nil {
		log.Printf("Warning: failed to seed federated content: %v", err)
	} else {
		log.Printf("Federated content seeded successfully")
	}

	// Register all sopbackend handlers (provides full backend API)
	hostCtx, stopHost := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopHost()
	sopbackend.StartBackgroundServiceWithContext(hostCtx, sopbackend.DefaultSessionCleanupInterval(), 2*time.Hour)

	// Start PTY shell session cleanup (30 minute idle timeout, check every 5 minutes)
	shellMgr := GetGlobalShellSessionManager()
	if shellMgr != nil {
		shellMgr.StartCleanup(hostCtx, 5*time.Minute, 30*time.Minute)
		log.Printf("PTY session cleanup started (idle timeout: 30m, check interval: 5m)")
	}

	mux := http.NewServeMux()
	sopbackend.RegisterAllHandlers(mux)
	sopbackend.RegisterMonitorabilityHandlers(mux)

	// Register daemon authentication endpoints (NO auth required)
	mux.HandleFunc("/api/daemon/auth", handleOptionsAndCORS(http.HandlerFunc(authHandler.HandleBootstrap)))
	mux.HandleFunc("/api/daemon/refresh", handleOptionsAndCORS(http.HandlerFunc(authHandler.HandleRefresh)))
	mux.HandleFunc("/api/daemon/hq-auth", handleOptionsAndCORS(http.HandlerFunc(authHandler.HandleHQAuth)))
	mux.HandleFunc("/api/daemon/receive-space", handleOptionsAndCORS(http.HandlerFunc(handleReceiveSpaceFromHQ)))
	mux.HandleFunc("/api/daemon/vectorization/recovery", handleOptionsAndCORS(http.HandlerFunc(handleGetVectorizationRecoveryStatus)))

	// Privacy governance endpoints WITH authentication
	mux.HandleFunc("/api/daemon/privacy/settings", handleOptionsAndCORS(authHandler.RequireAuth(http.HandlerFunc(handleGetPrivacySettings))))
	mux.HandleFunc("/api/daemon/privacy/settings/update", handleOptionsAndCORS(authHandler.RequireAuth(http.HandlerFunc(handleUpdatePrivacySettings))))
	mux.HandleFunc("/api/daemon/privacy/session/toggle", handleOptionsAndCORS(authHandler.RequireAuth(http.HandlerFunc(handleTogglePrivateSession))))
	mux.HandleFunc("/api/daemon/privacy/category", handleOptionsAndCORS(authHandler.RequireAuth(http.HandlerFunc(handleSetCategoryPreference))))
	mux.HandleFunc("/api/daemon/privacy/audit", handleOptionsAndCORS(authHandler.RequireAuth(http.HandlerFunc(handleGetAuditLog))))
	mux.HandleFunc("/api/daemon/privacy/approval-queue", handleOptionsAndCORS(authHandler.RequireAuth(http.HandlerFunc(handleGetApprovalQueue))))
	mux.HandleFunc("/api/daemon/privacy/approve", handleOptionsAndCORS(authHandler.RequireAuth(http.HandlerFunc(handleApproveItem))))
	mux.HandleFunc("/api/daemon/privacy/reject", handleOptionsAndCORS(authHandler.RequireAuth(http.HandlerFunc(handleRejectItem))))
	mux.HandleFunc("/api/daemon/privacy/apply-template", handleOptionsAndCORS(authHandler.RequireAuth(http.HandlerFunc(handleApplyPolicyTemplate))))
	mux.HandleFunc("/api/daemon/privacy/advanced-settings", handleOptionsAndCORS(authHandler.RequireAuth(http.HandlerFunc(handleAdvancedSettings))))

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
	logVerbose("Waiting for UI authentication...")

	server := &http.Server{Addr: addr, Handler: sopbackend.NewObservabilityHandler(corsHandler)}
	go func() {
		<-hostCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
		if shouldLogVerbose() {
			if isDBOptions {
				log.Printf("[daemon-dboptions] request database=%q from=%s auth=%t", r.URL.Query().Get("database"), r.RemoteAddr, authHeader != "")
			}
			log.Printf("[daemon-request] %s %s from=%s auth=%t", r.Method, r.URL.RequestURI(), r.RemoteAddr, authHeader != "")
		}
		handler.ServeHTTP(wrapped, r)
		if shouldLogVerbose() {
			if isDBOptions {
				log.Printf("[daemon-dboptions] completed database=%q status=%d duration=%s", r.URL.Query().Get("database"), wrapped.statusCode, time.Since(start))
			}
			log.Printf("[daemon-request] completed %s %s status=%d duration=%s", r.Method, r.URL.RequestURI(), wrapped.statusCode, time.Since(start))
		}
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

// Phase 3: Background job tracking
type backgroundJob struct {
	jobID     string
	command   string
	pid       int
	startTime time.Time
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	stdout    strings.Builder
	stderr    strings.Builder
	exitCode  *int
	error     string
	mu        sync.Mutex
	done      bool
}

var backgroundJobs = struct {
	mu   sync.RWMutex
	jobs map[string]*backgroundJob
}{
	jobs: make(map[string]*backgroundJob),
}

// generateJobID creates a unique job ID
func generateJobID() string {
	return fmt.Sprintf("job-%d-%d", time.Now().Unix(), rand.Intn(10000))
}

// getBackgroundJob retrieves a background job by ID
func getBackgroundJob(jobID string) (*backgroundJob, bool) {
	backgroundJobs.mu.RLock()
	defer backgroundJobs.mu.RUnlock()
	job, ok := backgroundJobs.jobs[jobID]
	return job, ok
}

// killBackgroundJob terminates a background job
func killBackgroundJob(jobID string) error {
	backgroundJobs.mu.RLock()
	job, ok := backgroundJobs.jobs[jobID]
	backgroundJobs.mu.RUnlock()

	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}

	job.mu.Lock()
	defer job.mu.Unlock()

	if job.done {
		return fmt.Errorf("job already completed: %s", jobID)
	}

	// Cancel context first
	if job.cancel != nil {
		job.cancel()
	}

	// Send SIGTERM to process
	if job.cmd != nil && job.cmd.Process != nil {
		if err := job.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			// If SIGTERM fails, force kill
			return job.cmd.Process.Kill()
		}
	}

	return nil
}

// sendSignalToProcess sends a signal to a running process
func sendSignalToProcess(pid int, sig os.Signal) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process not found: %w", err)
	}
	return process.Signal(sig)
}

func runLocalCommand(ctx context.Context, req sharedagent.LocalCommandRequest) (string, error) {
	result, err := runLocalCommandWithWorkingDir(ctx, req)

	// Backward compatibility: call legacy StreamOutput if new callbacks not provided
	if err == nil && req.StreamOutput != nil && req.StreamStdout == nil && req.StreamStderr == nil {
		req.StreamOutput(result)
	}

	return result, err
}

func directExecutableCommand(ctx context.Context, command, workingDir string) (*exec.Cmd, bool) {
	trimmedCommand := strings.TrimSpace(command)
	if trimmedCommand == "" {
		return nil, false
	}
	if strings.ContainsAny(trimmedCommand, "|&;<>()`$\\\"'") {
		return nil, false
	}

	parts := strings.Fields(trimmedCommand)
	if len(parts) == 0 {
		return nil, false
	}

	candidate := parts[0]
	args := parts[1:]
	if filepath.IsAbs(candidate) || strings.Contains(candidate, "/") || strings.Contains(candidate, `\`) {
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(workingDir, candidate)
		}
		if resolved, err := filepath.Abs(candidate); err == nil {
			if info, err := os.Stat(resolved); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
				return exec.CommandContext(ctx, resolved, args...), true
			}
		}
		return nil, false
	}

	if workingDir != "" {
		localCandidate := filepath.Join(workingDir, candidate)
		if info, err := os.Stat(localCandidate); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return exec.CommandContext(ctx, localCandidate, args...), true
		}
	}

	if resolved, err := exec.LookPath(candidate); err == nil {
		return exec.CommandContext(ctx, resolved, args...), true
	}

	return nil, false
}

func runLocalCommandWithWorkingDir(ctx context.Context, req sharedagent.LocalCommandRequest) (string, error) {
	command := req.Command
	workingDir := req.WorkingDir
	sessionID := req.SessionID
	timeoutSeconds := req.TimeoutSeconds

	if timeoutSeconds <= 0 {
		timeoutSeconds = 600
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	if strings.TrimSpace(command) == "" {
		return `{"stdout":"","stderr":"","exit_code":0}`, nil
	}

	trimmedCommand := strings.TrimSpace(command)

	// Route foreground commands through PTY for state persistence + streaming
	// Background jobs need exec.Command for proper lifecycle management
	if !req.Background && sessionID != "" {
		shellMgr := GetGlobalShellSessionManager()
		if shellMgr != nil {
			var stdoutBuf strings.Builder

			// Wrap streaming callbacks to capture output
			streamCallback := func(chunk string) {
				stdoutBuf.WriteString(chunk)
				if req.StreamStdout != nil {
					req.StreamStdout(chunk)
				}
			}

			// Note: PTY doesn't separate stdout/stderr - they're combined
			// This is standard PTY behavior (same as interactive terminals)

			// Handle cd command - execute and return new directory
			if strings.HasPrefix(trimmedCommand, "cd ") || trimmedCommand == "cd" {
				output, stillRunning, err := shellMgr.RunWithStreaming(sessionID, trimmedCommand, time.Duration(timeoutSeconds)*time.Second, streamCallback)
				if err != nil {
					resp := ExecuteResponse{
						Stderr:   err.Error(),
						Error:    err.Error(),
						ExitCode: 1,
					}
					payload, _ := json.Marshal(resp)
					return string(payload), nil
				}
				if stillRunning {
					resp := ExecuteResponse{
						Stdout: output,
						Error:  "command still running",
					}
					payload, _ := json.Marshal(resp)
					return string(payload), nil
				}
				// Get new working directory
				pwdOutput, _, _ := shellMgr.Run(sessionID, "pwd", 5*time.Second)
				resp := ExecuteResponse{
					Stdout:   strings.TrimSpace(pwdOutput),
					ExitCode: 0,
				}
				payload, _ := json.Marshal(resp)
				return string(payload), nil
			}

			// Execute command through PTY with streaming
			output, stillRunning, err := shellMgr.RunWithStreaming(sessionID, trimmedCommand, time.Duration(timeoutSeconds)*time.Second, streamCallback)

			// Call completion callback if provided
			exitCode := 0
			if err != nil {
				exitCode = 1
			}
			if req.OnComplete != nil {
				// PTY combines stdout/stderr, so pass output as stdout and empty stderr
				req.OnComplete(output, "", exitCode)
			}

			if err != nil {
				resp := ExecuteResponse{
					Stderr:   err.Error(),
					Error:    err.Error(),
					ExitCode: 1,
				}
				payload, _ := json.Marshal(resp)
				return string(payload), nil
			}

			resp := ExecuteResponse{
				Stdout:   output,
				ExitCode: exitCode,
			}
			if stillRunning {
				resp.Error = "command still running (use status command to check)"
			}
			payload, _ := json.Marshal(resp)
			return string(payload), nil
		}
	}

	// Background jobs and special cases still use exec.Command
	// They need independent process lifecycle management

	// For background jobs, inherit working directory from PTY session if available
	if req.Background && sessionID != "" && workingDir == "" {
		shellMgr := GetGlobalShellSessionManager()
		if shellMgr != nil {
			if pwdOutput, _, err := shellMgr.Run(sessionID, "pwd", 2*time.Second); err == nil {
				workingDir = strings.TrimSpace(pwdOutput)
			}
		}
	}

	// Phase 3: Background job execution (handle early to use different context)
	if req.Background {
		jobID := req.JobID
		if jobID == "" {
			jobID = generateJobID()
		}

		// Create background job with long-running context
		jobCtx, jobCancel := context.WithCancel(context.Background())

		// Build command with background context
		var bgCmd *exec.Cmd
		if directCmd, ok := directExecutableCommand(jobCtx, trimmedCommand, workingDir); ok {
			bgCmd = directCmd
		} else if runtime.GOOS == "windows" {
			bgCmd = exec.CommandContext(jobCtx, "cmd", "/C", command)
		} else {
			// Prepend COLUMNS environment variable for proper multi-column output
			commandWithEnv := "COLUMNS=100 " + command
			bgCmd = exec.CommandContext(jobCtx, "sh", "-c", commandWithEnv)
		}

		if workingDir != "" {
			bgCmd.Dir = workingDir
		} else if sessionID != "" {
			localCommandSessionState.mu.Lock()
			if existing, ok := localCommandSessionState.sessions[sessionID]; ok && existing != nil {
				bgCmd.Dir = existing.workingDir
			}
			localCommandSessionState.mu.Unlock()
		}

		if req.Stdin != "" {
			bgCmd.Stdin = strings.NewReader(req.Stdin)
		}

		if req.ProcessGroup && runtime.GOOS != "windows" {
			bgCmd.SysProcAttr = &syscall.SysProcAttr{
				Setpgid: true,
			}
		}

		job := &backgroundJob{
			jobID:     jobID,
			command:   command,
			startTime: time.Now(),
			cmd:       bgCmd,
			cancel:    jobCancel,
		}

		backgroundJobs.mu.Lock()
		backgroundJobs.jobs[jobID] = job
		backgroundJobs.mu.Unlock()

		// Launch in goroutine
		go runBackgroundJob(jobCtx, job, req)

		// Return immediately with job info
		resp := ExecuteResponse{
			Background: true,
			JobID:      jobID,
		}
		payload, _ := json.Marshal(resp)
		return string(payload), nil
	}

	// Fallback to exec.Command for:
	// - Commands without sessionID (one-shot execution)
	// - Stdin input (not supported by PTY)
	// - Process groups (special syscall features)
	// - OnProcessStart callbacks (need individual command PID)
	// Note: These lose state persistence but gain fine-grained control

	// Build command for foreground execution
	var cmd *exec.Cmd
	if directCmd, ok := directExecutableCommand(ctx, trimmedCommand, workingDir); ok {
		cmd = directCmd
	} else if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		// Prepend COLUMNS environment variable to command for proper multi-column output
		commandWithEnv := "COLUMNS=100 " + command
		cmd = exec.CommandContext(ctx, "sh", "-c", commandWithEnv)
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

	// Setup stdin if provided
	if req.Stdin != "" {
		cmd.Stdin = strings.NewReader(req.Stdin)
	}

	// Phase 2: Process group support for signal isolation
	if req.ProcessGroup {
		if runtime.GOOS != "windows" {
			cmd.SysProcAttr = &syscall.SysProcAttr{
				Setpgid: true,
			}
		}
	}

	// Real-time streaming setup (foreground execution)
	var stdoutBuf, stderrBuf bytes.Buffer
	var wg sync.WaitGroup

	// Create pipes for real-time streaming
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start command
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start command: %w", err)
	}

	// Phase 2: Capture PID and notify callback
	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
		if req.OnProcessStart != nil {
			req.OnProcessStart(pid)
		}
	}

	// Stream stdout in real-time
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		for {
			n, err := stdoutPipe.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				stdoutBuf.WriteString(chunk)

				// Real-time streaming callbacks
				if req.StreamStdout != nil {
					req.StreamStdout(chunk)
				}

				// Legacy event streamer
				if streamer, ok := ctx.Value(ai.CtxKeyEventStreamer).(func(string, any)); ok && streamer != nil {
					streamer("content", chunk)
				}
			}
			if err != nil {
				break
			}
		}
	}()

	// Stream stderr in real-time
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		for {
			n, err := stderrPipe.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				stderrBuf.WriteString(chunk)

				// Real-time stderr streaming with visual distinction
				if req.StreamStderr != nil {
					req.StreamStderr(chunk)
				}

				// Legacy event streamer with error indicator
				if streamer, ok := ctx.Value(ai.CtxKeyEventStreamer).(func(string, any)); ok && streamer != nil {
					streamer("content", "⚠️ "+chunk)
				}
			}
			if err != nil {
				break
			}
		}
	}()

	// Wait for command and streams to complete
	cmdErr := cmd.Wait()
	wg.Wait()

	// Update session state
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

	// Build response
	stdoutStr := stdoutBuf.String()
	stderrStr := stderrBuf.String()

	resp := ExecuteResponse{
		Stdout:    stdoutStr,
		Stderr:    stderrStr,
		Streaming: req.StreamStdout != nil || req.StreamStderr != nil,
		PID:       pid,
	}

	if cmdErr != nil {
		resp.Error = cmdErr.Error()
		if exitErr, ok := cmdErr.(*exec.ExitError); ok {
			resp.ExitCode = exitErr.ExitCode()
		} else {
			resp.ExitCode = -1
		}
	} else {
		resp.ExitCode = 0
	}

	// Call completion callback
	if req.OnComplete != nil {
		req.OnComplete(stdoutStr, stderrStr, resp.ExitCode)
	}

	payload, marshalErr := json.Marshal(resp)
	if marshalErr != nil {
		return "", marshalErr
	}
	return string(payload), nil
}

// handleCdCommand handles directory change commands
func handleCdCommand(trimmedCommand, workingDir, sessionID string) (string, error) {
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

// runBackgroundJob executes a command in the background
func runBackgroundJob(ctx context.Context, job *backgroundJob, req sharedagent.LocalCommandRequest) {
	defer func() {
		job.mu.Lock()
		job.done = true
		job.mu.Unlock()
	}()

	// Create pipes for output capture
	stdoutPipe, err := job.cmd.StdoutPipe()
	if err != nil {
		job.mu.Lock()
		job.error = fmt.Sprintf("failed to create stdout pipe: %v", err)
		job.mu.Unlock()
		return
	}

	stderrPipe, err := job.cmd.StderrPipe()
	if err != nil {
		job.mu.Lock()
		job.error = fmt.Sprintf("failed to create stderr pipe: %v", err)
		job.mu.Unlock()
		return
	}

	// Start command
	if err := job.cmd.Start(); err != nil {
		job.mu.Lock()
		job.error = fmt.Sprintf("failed to start command: %v", err)
		job.mu.Unlock()
		return
	}

	// Capture PID
	if job.cmd.Process != nil {
		job.mu.Lock()
		job.pid = job.cmd.Process.Pid
		job.mu.Unlock()

		if req.OnProcessStart != nil {
			req.OnProcessStart(job.pid)
		}
	}

	var wg sync.WaitGroup

	// Capture stdout
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		for {
			n, err := stdoutPipe.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				job.mu.Lock()
				job.stdout.WriteString(chunk)
				job.mu.Unlock()

				if req.StreamStdout != nil {
					req.StreamStdout(chunk)
				}
			}
			if err != nil {
				break
			}
		}
	}()

	// Capture stderr
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		for {
			n, err := stderrPipe.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				job.mu.Lock()
				job.stderr.WriteString(chunk)
				job.mu.Unlock()

				if req.StreamStderr != nil {
					req.StreamStderr(chunk)
				}
			}
			if err != nil {
				break
			}
		}
	}()

	// Wait for command
	cmdErr := job.cmd.Wait()
	wg.Wait()

	// Update job with results
	job.mu.Lock()
	if cmdErr != nil {
		job.error = cmdErr.Error()
		if exitErr, ok := cmdErr.(*exec.ExitError); ok {
			exitCode := exitErr.ExitCode()
			job.exitCode = &exitCode
		} else {
			exitCode := -1
			job.exitCode = &exitCode
		}
	} else {
		exitCode := 0
		job.exitCode = &exitCode
	}

	stdout := job.stdout.String()
	stderr := job.stderr.String()
	exitCode := 0
	if job.exitCode != nil {
		exitCode = *job.exitCode
	}
	job.mu.Unlock()

	// Call completion callback
	if req.OnComplete != nil {
		req.OnComplete(stdout, stderr, exitCode)
	}
}

func startHQSyncLoop(authHandler *DaemonAuthHandler) {
	if authHandler == nil {
		return
	}
	for {
		// Use the queue's adaptive delay so HQ throttle hints can change the cadence.
		delay := authHandler.syncQueue.GetNextSyncDelay()
		if authHandler.pollClient != nil {
			log.Printf("HQ sync: waiting %s until next poll", delay)
		} else {
			log.Printf("HQ sync: waiting %s until next attempt", delay)
		}
		time.Sleep(delay)

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
		Accepted:            true,
		Message:             "handoff accepted",
		WelcomeContext:      buildLocalRAGWelcomeContext(),
		NeedsRecovery:       currentVectorizationNeedsRecovery(),
		Recovery:            currentVectorizationRecoveryState(),
		StoragePathRecovery: currentStoragePathRecoveryState(),
	})
}

func buildLocalRAGWelcomeContext() string {
	welcomeContext := "Hello! I am your local <strong>Daemon Assistant</strong>. I'm here to help you work with the local shell and embedded RAG runtime directly from this desktop experience.<br><br>I can help you:<br><ul style=\"color: var(--text-muted); font-size: 0.9em; margin: 0 0 8px 18px; padding: 0; list-style-position: outside;\"><li style=\"margin: 0 0 4px 0; padding: 0;\">run shell commands and inspect command output</li><li style=\"margin: 0 0 4px 0; padding: 0;\">answer questions using the local RAG knowledge you configured</li><li style=\"margin: 0 0 4px 0; padding: 0;\">help you troubleshoot local workflows from the command line</li></ul>Type <strong>/list_tools</strong> to see the built-in SOP commands available in this session.<br><br><em style=\"color: var(--text-muted); font-size: 0.9em; display: block; margin-bottom: 4px;\">Shell-style commands such as <strong>ls</strong>, <strong>pwd</strong>, and <strong>cd /Volumes</strong> can be run directly in the local shell, and slash-prefixed commands such as <strong>/list_tools</strong> are available for built-in SOP actions.</em><br><em style=\"color: var(--text-muted); font-size: 0.9em; display: block; margin-bottom: 4px;\">Try asking things like:</em><ul style=\"color: var(--text-muted); font-size: 0.9em; margin: 0 0 0 18px; padding: 0; list-style-position: outside;\"><li style=\"margin: 0 0 2px 0; padding: 0;\">\"Show me the current directory and files\"</li><li style=\"margin: 0 0 2px 0; padding: 0;\">\"Explain how to run the local daemon\"</li><li style=\"margin: 0 0 2px 0; padding: 0;\">\"Help me inspect my local project structure\"</li></ul>"

	recovery := currentVectorizationRecoveryState()
	storagePathRecovery := currentStoragePathRecoveryState()
	if (recovery.Status != "healthy" && strings.TrimSpace(recovery.Prompt) != "") || (storagePathRecovery.Status != "healthy" && strings.TrimSpace(storagePathRecovery.Prompt) != "") {
		statusText := "needs attention"
		if recovery.Status == "recovered" {
			statusText = "recovered"
		} else if recovery.Status == "ignored" {
			statusText = "ignored"
		}
		if recovery.RetryCount > 0 {
			statusText = fmt.Sprintf("%s • retry %d", statusText, recovery.RetryCount)
		}
		if recovery.LastAttemptAt > 0 {
			statusText = fmt.Sprintf("%s • last attempt %s", statusText, time.Unix(recovery.LastAttemptAt, 0).Format("2006-01-02 15:04:05"))
		}
		if storagePathRecovery.Status != "healthy" && strings.TrimSpace(storagePathRecovery.Prompt) != "" {
			welcomeContext += fmt.Sprintf("<br><br><strong>Storage notice:</strong> %s", storagePathRecovery.Prompt)
			welcomeContext += fmt.Sprintf("<br><span style=\"color: var(--text-muted); font-size: 0.9em;\">Path: %s</span>", htmlEscape(storagePathRecovery.Path))
		} else if recovery.Status != "healthy" && strings.TrimSpace(recovery.Prompt) != "" {
			welcomeContext += fmt.Sprintf("<br><br><strong>Recovery notice:</strong> %s", recovery.Prompt)
		}
		welcomeContext += fmt.Sprintf("<br><span style=\"color: var(--text-muted); font-size: 0.9em;\">Status: %s</span>", htmlEscape(statusText))
		if recovery.Status != "healthy" && strings.TrimSpace(recovery.Prompt) != "" {
			welcomeContext += fmt.Sprintf("<br><br><button class=\"recovery-action-button\" data-action=\"recover\" data-kb-name=\"%s\" data-target-type=\"%s\" data-target-id=\"%s\">Recover semantic index</button>", htmlEscape(recovery.KBName), htmlEscape(recovery.TargetType), htmlEscape(recovery.TargetID))
			welcomeContext += fmt.Sprintf("<button class=\"recovery-action-button\" data-action=\"clear_and_rebuild\" data-kb-name=\"%s\" data-target-type=\"%s\" data-target-id=\"%s\">Clear and rebuild</button>", htmlEscape(recovery.KBName), htmlEscape(recovery.TargetType), htmlEscape(recovery.TargetID))
			welcomeContext += fmt.Sprintf("<button class=\"recovery-action-button\" data-action=\"ignore\" data-kb-name=\"%s\" data-target-type=\"%s\" data-target-id=\"%s\">Ignore for now</button>", htmlEscape(recovery.KBName), htmlEscape(recovery.TargetType), htmlEscape(recovery.TargetID))
		}
	}
	return welcomeContext
}

func htmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
	).Replace(s)
}

func currentVectorizationRecoveryState() runtimeVectorizationRecoveryState {
	if activeRuntimeVectorizationFailurePolicy == nil {
		return runtimeVectorizationRecoveryState{Status: "healthy"}
	}
	return activeRuntimeVectorizationFailurePolicy.RecoveryState()
}

func currentVectorizationNeedsRecovery() bool {
	policy := sop.DefaultStorageHealthPolicy()
	sharedNeedsRecovery := false
	if policy != nil {
		states := policy.StoragePathStates()
		for _, state := range states {
			if !policy.ShouldAutoRetry(state.Operation, state.Path) {
				sharedNeedsRecovery = true
				break
			}
		}
	}

	if activeRuntimeVectorizationFailurePolicy != nil {
		return activeRuntimeVectorizationFailurePolicy.NeedsRecovery() || sharedNeedsRecovery
	}
	return sharedNeedsRecovery
}

func currentStoragePathRecoveryState() runtimeDataPathRecoveryState {
	policy := sop.DefaultStorageHealthPolicy()
	if policy != nil {
		states := policy.StoragePathStates()
		if len(states) > 0 {
			for _, state := range states {
				if state.Status != sop.StorageHealthHealthy {
					return newRuntimeDataPathRecoveryStateFromPolicy(state)
				}
			}
		}
	}

	if activeRuntimeVectorizationFailurePolicy != nil {
		return activeRuntimeVectorizationFailurePolicy.StoragePathRecoveryState()
	}

	return runtimeDataPathRecoveryState{Status: "healthy"}
}

func newRuntimeDataPathRecoveryStateFromPolicy(state sop.StoragePathHealthState) runtimeDataPathRecoveryState {
	return runtimeDataPathRecoveryState{
		Operation:        state.Operation,
		Path:             state.Path,
		Count:            state.Count,
		Status:           string(state.Status),
		Prompt:           state.Prompt,
		LastUpdatedAt:    state.LastUpdatedAt,
		LastError:        state.LastError,
		SuggestedAction:  state.SuggestedAction,
		NeedsAttention:   state.NeedsAttention,
		AutoRetryEnabled: state.AutoRetryEnabled,
		MaxRetryCount:    state.MaxRetryCount,
	}
}

func storageHealthAllowsHigherLevelAutoRetry() bool {
	policy := sop.DefaultStorageHealthPolicy()
	if policy != nil {
		states := policy.StoragePathStates()
		if len(states) > 0 {
			for _, state := range states {
				if !policy.ShouldAutoRetry(state.Operation, state.Path) {
					return false
				}
			}
			return true
		}
	}

	if activeRuntimeVectorizationFailurePolicy != nil {
		states := activeRuntimeVectorizationFailurePolicy.StoragePathRecoveryStates()
		if len(states) == 0 {
			return true
		}
		for _, state := range states {
			if strings.EqualFold(state.Status, "read_only") || (state.NeedsAttention && !state.AutoRetryEnabled) {
				return false
			}
		}
		return true
	}

	return true
}

func resetVectorizationIndexForRecovery(ctx context.Context, kbName string) error {
	return vectorizationResetExecutor(ctx, kbName)
}

func retryPendingVectorizationRecovery(ctx context.Context, force bool) int {
	if activeRuntimeVectorizationFailurePolicy == nil {
		return 0
	}
	if !storageHealthAllowsHigherLevelAutoRetry() {
		return 0
	}

	states := activeRuntimeVectorizationFailurePolicy.RecoveryStates()
	count := 0
	for _, state := range states {
		if state.Status == "healthy" || state.Status == "recovered" || state.Status == "ignored" || state.Status == "scheduled" || state.Status == "abandoned" {
			continue
		}
		if !force && state.RetryCount > 0 && state.LastAttemptAt > 0 {
			if state.NextRetryAt > 0 {
				if nextRetryAt := time.Unix(state.NextRetryAt, 0); time.Now().Before(nextRetryAt) {
					continue
				}
			} else if elapsed := time.Since(time.Unix(state.LastAttemptAt, 0)); elapsed < vectorizationRecoveryRetryBackoff {
				continue
			}
		}
		activeRuntimeVectorizationFailurePolicy.RecordRetryAttempt(state.KBName, state.TargetType, state.TargetID)
		if err := vectorizationRecoveryExecutor(ctx, state.KBName, state.TargetType, state.TargetID); err == nil {
			activeRuntimeVectorizationFailurePolicy.MarkRecovered(state.KBName, state.TargetType, state.TargetID)
			count++
		}
	}
	return count
}

func handleGetVectorizationRecoveryStatus(w http.ResponseWriter, r *http.Request) {
	payload := map[string]any{
		"has_recovery":          false,
		"needs_recovery":        false,
		"latest":                runtimeVectorizationRecoveryState{},
		"states":                []runtimeVectorizationRecoveryState{},
		"storage_path_recovery": runtimeDataPathRecoveryState{},
		"storage_path_states":   []runtimeDataPathRecoveryState{},
		"action":                "",
		"updated":               false,
		"recovered_count":       0,
		"message":               "",
		"error":                 "",
	}
	if r.Method == http.MethodPost {
		var req struct {
			Action     string `json:"action"`
			KBName     string `json:"kb_name"`
			TargetType string `json:"target_type"`
			TargetID   string `json:"target_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON body: " + err.Error()})
			return
		}
		if activeRuntimeVectorizationFailurePolicy == nil {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": "vectorization recovery policy unavailable"})
			return
		}

		switch strings.ToLower(strings.TrimSpace(req.Action)) {
		case "ignore":
			activeRuntimeVectorizationFailurePolicy.MarkIgnored(req.KBName, req.TargetType, req.TargetID)
			payload["action"] = "ignore"
			payload["updated"] = true
			payload["message"] = "Recovery request ignored for now."
		case "schedule_retry", "schedule", "retry_later":
			activeRuntimeVectorizationFailurePolicy.MarkScheduled(req.KBName, req.TargetType, req.TargetID)
			payload["action"] = "schedule_retry"
			payload["updated"] = true
			payload["message"] = "Recovery was scheduled for later."
		case "give_up", "abandon", "stop":
			activeRuntimeVectorizationFailurePolicy.MarkAbandoned(req.KBName, req.TargetType, req.TargetID)
			payload["action"] = "give_up"
			payload["updated"] = true
			payload["message"] = "Recovery was abandoned."
		case "recover", "recovered":
			activeRuntimeVectorizationFailurePolicy.RecordRetryAttempt(req.KBName, req.TargetType, req.TargetID)
			if err := vectorizationRecoveryExecutor(r.Context(), req.KBName, req.TargetType, req.TargetID); err != nil {
				log.Printf("vectorization recovery failed for kb=%s target=%s/%s: %v", req.KBName, req.TargetType, req.TargetID, err)
				payload["action"] = "recover"
				payload["error"] = err.Error()
				payload["updated"] = false
				payload["message"] = "Recovery attempt failed. The daemon will keep the target marked for follow-up."
			} else {
				activeRuntimeVectorizationFailurePolicy.MarkRecovered(req.KBName, req.TargetType, req.TargetID)
				payload["action"] = "recover"
				payload["updated"] = true
				payload["message"] = "Semantic index rebuilt successfully."
			}
		case "clear_and_rebuild":
			activeRuntimeVectorizationFailurePolicy.RecordRetryAttempt(req.KBName, req.TargetType, req.TargetID)
			if err := resetVectorizationIndexForRecovery(r.Context(), req.KBName); err != nil {
				log.Printf("vectorization clear-and-rebuild reset failed for kb=%s target=%s/%s: %v", req.KBName, req.TargetType, req.TargetID, err)
				payload["action"] = "clear_and_rebuild"
				payload["error"] = err.Error()
				payload["updated"] = false
				payload["message"] = "Clear-and-rebuild reset failed. The daemon will keep the target marked for follow-up."
			} else if err := vectorizationRecoveryExecutor(r.Context(), req.KBName, req.TargetType, req.TargetID); err != nil {
				log.Printf("vectorization clear-and-rebuild failed for kb=%s target=%s/%s: %v", req.KBName, req.TargetType, req.TargetID, err)
				payload["action"] = "clear_and_rebuild"
				payload["error"] = err.Error()
				payload["updated"] = false
				payload["message"] = "Clear-and-rebuild failed. The daemon will keep the target marked for follow-up."
			} else {
				activeRuntimeVectorizationFailurePolicy.MarkRecovered(req.KBName, req.TargetType, req.TargetID)
				payload["action"] = "clear_and_rebuild"
				payload["updated"] = true
				payload["message"] = "Semantic index cleared and rebuilt successfully."
			}
		case "recover_all":
			states := activeRuntimeVectorizationFailurePolicy.RecoveryStates()
			count := 0
			for _, state := range states {
				if state.Status == "healthy" || state.Status == "recovered" || state.Status == "ignored" || state.Status == "scheduled" || state.Status == "abandoned" {
					continue
				}
				activeRuntimeVectorizationFailurePolicy.RecordRetryAttempt(state.KBName, state.TargetType, state.TargetID)
				if err := vectorizationRecoveryExecutor(r.Context(), state.KBName, state.TargetType, state.TargetID); err == nil {
					activeRuntimeVectorizationFailurePolicy.MarkRecovered(state.KBName, state.TargetType, state.TargetID)
					count++
				}
			}
			payload["action"] = "recover_all"
			payload["updated"] = true
			payload["recovered_count"] = count
			payload["message"] = fmt.Sprintf("Attempted recovery for %d degraded target(s).", count)
		default:
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action"})
			return
		}
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if activeRuntimeVectorizationFailurePolicy != nil {
		latest := activeRuntimeVectorizationFailurePolicy.RecoveryState()
		states := activeRuntimeVectorizationFailurePolicy.RecoveryStates()
		storagePathRecovery := activeRuntimeVectorizationFailurePolicy.StoragePathRecoveryState()
		storagePathStates := activeRuntimeVectorizationFailurePolicy.StoragePathRecoveryStates()
		payload["has_recovery"] = latest.Status != "healthy" || len(states) > 0 || storagePathRecovery.Status != "healthy" || len(storagePathStates) > 0
		payload["needs_recovery"] = activeRuntimeVectorizationFailurePolicy.NeedsRecovery()
		payload["latest"] = latest
		payload["states"] = states
		payload["storage_path_recovery"] = storagePathRecovery
		payload["storage_path_states"] = storagePathStates
	}

	policy := sop.DefaultStorageHealthPolicy()
	sharedStates := []runtimeDataPathRecoveryState{}
	if policy != nil {
		states := policy.StoragePathStates()
		for _, state := range states {
			if state.Status != sop.StorageHealthHealthy {
				sharedStates = append(sharedStates, newRuntimeDataPathRecoveryStateFromPolicy(state))
			}
		}
	}
	if len(sharedStates) > 0 {
		payload["has_recovery"] = true
		payload["needs_recovery"] = true
		if len(sharedStates) > 0 {
			payload["storage_path_recovery"] = sharedStates[0]
		}
		payload["storage_path_states"] = sharedStates
	}
	respondJSON(w, http.StatusOK, payload)
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

	// Determine execution mode
	executionMode := req.ExecutionMode
	if executionMode == "" {
		// Auto-detect if not specified
		if shouldUseTerminal(req.Command) {
			executionMode = "terminal"
		} else {
			executionMode = "background"
		}
	}

	log.Printf("Executing command in %s mode: %s", executionMode, req.Command)

	// Terminal mode: launch visible terminal window
	if executionMode == "terminal" {
		if err := launchInTerminal(req.Command, req.WorkingDir); err != nil {
			log.Printf("Failed to launch terminal, falling back to background: %v", err)
			// Fall back to background execution
			executionMode = "background"
		} else {
			log.Printf("Command launched in terminal successfully")
			resp := ExecuteResponse{
				Stdout:             "Command launched in terminal window.",
				ExitCode:           0,
				LaunchedInTerminal: true,
			}
			respondJSON(w, http.StatusOK, resp)
			return
		}
	}

	// Background mode: capture output and return
	// Use request context for cancellation support
	ctx := r.Context()
	if req.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(req.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", req.Command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", req.Command)
		// Set process group so we can kill the entire group on cancellation
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setpgid: true,
		}
	}

	if req.WorkingDir != "" {
		cmd.Dir = req.WorkingDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Start command and monitor for cancellation
	if err := cmd.Start(); err != nil {
		resp := ExecuteResponse{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: -1,
			Error:    fmt.Sprintf("Failed to start command: %v", err),
		}
		respondJSON(w, http.StatusOK, resp)
		return
	}

	// Monitor context cancellation in a goroutine
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		// Context cancelled - kill the process
		if cmd.Process != nil {
			if runtime.GOOS == "windows" {
				cmd.Process.Kill()
			} else {
				// Kill the entire process group
				syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		}
		<-done // Wait for process to finish

		resp := ExecuteResponse{
			Stdout:    stdout.String(),
			Stderr:    stderr.String(),
			Cancelled: true,
			ExitCode:  -1,
			Error:     "Command cancelled by user",
		}
		log.Printf("Command cancelled by user")
		respondJSON(w, http.StatusOK, resp)
		return

	case err := <-done:
		// Command completed normally
		resp := ExecuteResponse{
			Stdout: stdout.String(),
			Stderr: stderr.String(),
		}

		// Add suggestion if command would work better in terminal
		if shouldUseTerminal(req.Command) && executionMode == "background" {
			resp.Suggestion = getTerminalSuggestion(req.Command)
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

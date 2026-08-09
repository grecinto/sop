package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
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
	sopfs "github.com/sharedcode/sop/fs"
	"github.com/sharedcode/sop/tools/sopbackend"
)

var (
	globalSyncQueue *SyncQueue
	syncQueueOnce   sync.Once
)

func bootstrapSOPKnowledgeBaseIfNeeded(ctx context.Context, databaseName string, spaceExists func(context.Context, string) (bool, error), preload func(context.Context, string) error) error {
	if strings.TrimSpace(databaseName) == "" {
		return nil
	}

	exists, err := spaceExists(ctx, ai.DefaultKBName)
	if err != nil {
		return fmt.Errorf("check SOP space existence: %w", err)
	}
	if exists {
		log.Printf("SOP knowledge base already exists in database %s; skipping preload", databaseName)
		return nil
	}

	return preload(ctx, databaseName)
}

func shouldSeedFederatedContent(cfg sopbackend.Config, decision sopbackend.ProvisioningDecision) (bool, string) {
	if decision.Required {
		return false, fmt.Sprintf("provisioning is still required (%s)", decision.Reason)
	}
	if cfg.SystemDB == nil {
		return false, "system database is not configured"
	}
	return true, ""
}

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

func flagWasProvided(flagName string) bool {
	for i := 0; i < len(os.Args)-1; i++ {
		arg := os.Args[i+1]
		if arg == "--" {
			break
		}
		if arg == "-"+flagName || arg == "--"+flagName {
			return true
		}
		if strings.HasPrefix(arg, "-"+flagName+"=") || strings.HasPrefix(arg, "--"+flagName+"=") {
			return true
		}
		if (arg == "-"+flagName[:1] || arg == "--"+flagName[:1]) && i+2 < len(os.Args) {
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
  -log-level string
        Set daemon log verbosity (debug, info, warn, error). Default is info.
  -port int
        Port to run the local SOP daemon on (default 9090)
  -rate-limit-max-requests int
        Maximum local requests per session before enforcement kicks in (default 20)
  -rate-limit-window duration
        Rate limit window for local daemon requests (default 1m0s)
  -access-token-ttl duration
        Access token expiry duration (default 1h0m0s)
  -refresh-token-ttl duration
        Refresh token expiry duration (1 week default) (default 168h0m0s)

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
		// Fleet Learning disabled (2026-07-30) - Sync enqueuer registration disabled
		// Phase system doesn't need static workflow federation
		// Infrastructure preserved for potential future use
		// sharedagent.RegisterSyncEnqueuer(func(text, category string, metadata map[string]any) error {
		// 	if globalSyncQueue != nil {
		// 		return globalSyncQueue.Enqueue(text, category, metadata)
		// 	}
		// 	return nil
		// })
	})
	return globalSyncQueue
}

type ExecuteRequest struct {
	Command        string `json:"command"`
	WorkingDir     string `json:"working_dir,omitempty"`
	Reasoning      string `json:"reasoning,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	ExecutionMode  string `json:"execution_mode,omitempty"` // "background" or "terminal"
	Stdin          string `json:"stdin,omitempty"`
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
		Port      string `json:"port,omitempty"`
		UserID    string `json:"user_id"`
		SessionID string `json:"session_id"`
	} `json:"hq"`
	LLMProvider        string                   `json:"llm_provider,omitempty"`
	LLMAPIKey          string                   `json:"llm_api_key,omitempty"`
	LLMModel           string                   `json:"llm_model,omitempty"`
	LLMURL             string                   `json:"llm_url,omitempty"`
	LLMCredentials     map[string]llmCredential `json:"llm_credentials,omitempty"`
	LLMPoolVersion     int64                    `json:"llm_pool_version,omitempty"`
	LLMPoolUpdatedAtMS int64                    `json:"llm_pool_updated_at_ms,omitempty"`
	LLMPoolUpdatedBy   string                   `json:"llm_pool_updated_by,omitempty"`
	ThinkingLevel      string                   `json:"thinking_level,omitempty"`
}

type llmCredential struct {
	APIKey      string `json:"api_key,omitempty"`
	Model       string `json:"model,omitempty"`
	URL         string `json:"url,omitempty"`
	UpdatedAtMS int64  `json:"updated_at_ms,omitempty"`
	DeletedAtMS int64  `json:"deleted_at_ms,omitempty"`
}

type daemonSecretEncryptRequest struct {
	Value string `json:"value"`
}

type daemonSecretEncryptResponse struct {
	EncryptedValue string `json:"encrypted_value"`
}

type UIHandoffResponse struct {
	Accepted             bool                              `json:"accepted"`
	Message              string                            `json:"message"`
	SessionID            string                            `json:"session_id,omitempty"`
	LLMProvider          string                            `json:"llm_provider,omitempty"`
	LLMModel             string                            `json:"llm_model,omitempty"`
	LLMURL               string                            `json:"llm_url,omitempty"`
	LLMCredentials       map[string]llmCredential          `json:"llm_credentials,omitempty"`
	LLMPoolVersion       int64                             `json:"llm_pool_version,omitempty"`
	LLMPoolUpdatedAtMS   int64                             `json:"llm_pool_updated_at_ms,omitempty"`
	LLMPoolUpdatedBy     string                            `json:"llm_pool_updated_by,omitempty"`
	LLMPoolSyncDirection string                            `json:"llm_pool_sync_direction,omitempty"`
	WelcomeContext       string                            `json:"welcome_context,omitempty"`
	Error                string                            `json:"error,omitempty"`
	NeedsRecovery        bool                              `json:"needs_recovery,omitempty"`
	Recovery             runtimeVectorizationRecoveryState `json:"recovery,omitempty"`
	StoragePathRecovery  runtimeDataPathRecoveryState      `json:"storage_path_recovery,omitempty"`
}

type daemonSessionContext struct {
	AccessToken        string
	RefreshToken       string
	HQBaseURL          string
	HQPort             string
	HQUserID           string
	HQSessionID        string
	LLMProvider        string
	LLMAPIKey          string
	LLMModel           string
	LLMURL             string
	LLMCredentials     map[string]llmCredential
	LLMPoolVersion     int64
	LLMPoolUpdatedAtMS int64
	LLMPoolUpdatedBy   string
	ThinkingLevel      string
	LastUpdated        time.Time
}

type persistedDaemonLLMSessionState struct {
	HQBaseURL          string                   `json:"hq_base_url,omitempty"`
	HQPort             string                   `json:"hq_port,omitempty"`
	HQUserID           string                   `json:"hq_user_id,omitempty"`
	HQSessionID        string                   `json:"hq_session_id,omitempty"`
	LLMProvider        string                   `json:"llm_provider,omitempty"`
	LLMAPIKey          string                   `json:"llm_api_key,omitempty"`
	LLMModel           string                   `json:"llm_model,omitempty"`
	LLMURL             string                   `json:"llm_url,omitempty"`
	LLMCredentials     map[string]llmCredential `json:"llm_credentials,omitempty"`
	LLMPoolVersion     int64                    `json:"llm_pool_version,omitempty"`
	LLMPoolUpdatedAtMS int64                    `json:"llm_pool_updated_at_ms,omitempty"`
	LLMPoolUpdatedBy   string                   `json:"llm_pool_updated_by,omitempty"`
	ThinkingLevel      string                   `json:"thinking_level,omitempty"`
	LastUpdatedUnixMS  int64                    `json:"last_updated_unix_ms,omitempty"`
}

type persistedDaemonLLMState struct {
	SchemaVersion int                                       `json:"schema_version"`
	Sessions      map[string]persistedDaemonLLMSessionState `json:"sessions,omitempty"`
}

type persistedDaemonLLMEnvelope struct {
	EncryptedPayload string `json:"encrypted_payload"`
}

func normalizeCredentialProviderKey(provider string) string {
	key := strings.ToLower(strings.TrimSpace(provider))
	switch key {
	case "chatgpt":
		return "openai"
	case "google":
		return "gemini"
	default:
		return key
	}
}

func cloneLLMCredentialsMap(in map[string]llmCredential) map[string]llmCredential {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]llmCredential, len(in))
	for provider, cred := range in {
		out[provider] = cred
	}
	return out
}

func normalizePoolVersion(version int64) int64 {
	if version < 0 {
		return 0
	}
	return version
}

func normalizePoolTimestamp(ts int64) int64 {
	if ts < 0 {
		return 0
	}
	return ts
}

func normalizePoolUpdatedBy(updatedBy string) string {
	trimmed := strings.TrimSpace(updatedBy)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

func credentialLogicalTimestamp(cred llmCredential) int64 {
	updated := normalizePoolTimestamp(cred.UpdatedAtMS)
	deleted := normalizePoolTimestamp(cred.DeletedAtMS)
	if deleted > updated {
		return deleted
	}
	return updated
}

func normalizeCredentialForComparison(cred llmCredential) llmCredential {
	return llmCredential{
		APIKey:      strings.TrimSpace(cred.APIKey),
		Model:       strings.TrimSpace(cred.Model),
		URL:         strings.TrimSpace(cred.URL),
		UpdatedAtMS: normalizePoolTimestamp(cred.UpdatedAtMS),
		DeletedAtMS: normalizePoolTimestamp(cred.DeletedAtMS),
	}
}

func llmCredentialsEqual(a, b map[string]llmCredential) bool {
	if len(a) != len(b) {
		return false
	}
	for provider, av := range a {
		bv, ok := b[provider]
		if !ok {
			return false
		}
		if normalizeCredentialForComparison(av) != normalizeCredentialForComparison(bv) {
			return false
		}
	}
	return true
}

func comparePoolFreshness(existingVersion, existingUpdatedAt int64, existingUpdatedBy string, incomingVersion, incomingUpdatedAt int64, incomingUpdatedBy string) int {
	existingVersion = normalizePoolVersion(existingVersion)
	incomingVersion = normalizePoolVersion(incomingVersion)
	if existingVersion > incomingVersion {
		return 1
	}
	if incomingVersion > existingVersion {
		return -1
	}

	existingUpdatedAt = normalizePoolTimestamp(existingUpdatedAt)
	incomingUpdatedAt = normalizePoolTimestamp(incomingUpdatedAt)
	if existingUpdatedAt > incomingUpdatedAt {
		return 1
	}
	if incomingUpdatedAt > existingUpdatedAt {
		return -1
	}

	existingUpdatedBy = normalizePoolUpdatedBy(existingUpdatedBy)
	incomingUpdatedBy = normalizePoolUpdatedBy(incomingUpdatedBy)
	if existingUpdatedBy > incomingUpdatedBy {
		return 1
	}
	if incomingUpdatedBy > existingUpdatedBy {
		return -1
	}
	return 0
}

func mergeCredentialEntry(existing, incoming llmCredential, preferIncoming bool) llmCredential {
	existing = normalizeCredentialForComparison(existing)
	incoming = normalizeCredentialForComparison(incoming)

	existingTS := credentialLogicalTimestamp(existing)
	incomingTS := credentialLogicalTimestamp(incoming)

	switch {
	case incomingTS > existingTS:
		return incoming
	case existingTS > incomingTS:
		return existing
	default:
		if preferIncoming {
			return incoming
		}
		return existing
	}
}

func mergeCredentialPool(existing, incoming map[string]llmCredential, preferIncoming bool) map[string]llmCredential {
	if len(existing) == 0 && len(incoming) == 0 {
		return nil
	}
	merged := cloneLLMCredentialsMap(existing)
	if merged == nil {
		merged = map[string]llmCredential{}
	}
	for _, provider := range []string{"openai", "anthropic", "gemini"} {
		existingCred, hasExisting := merged[provider]
		incomingCred, hasIncoming := incoming[provider]
		switch {
		case hasExisting && hasIncoming:
			merged[provider] = mergeCredentialEntry(existingCred, incomingCred, preferIncoming)
		case hasIncoming:
			merged[provider] = normalizeCredentialForComparison(incomingCred)
		case hasExisting:
			merged[provider] = normalizeCredentialForComparison(existingCred)
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func mergeLLMPoolState(existing daemonSessionContext, incoming map[string]llmCredential, incomingVersion, incomingUpdatedAt int64, incomingUpdatedBy string) (map[string]llmCredential, int64, int64, string, string) {
	nowMS := time.Now().UTC().UnixMilli()
	existingPool := cloneLLMCredentialsMap(existing.LLMCredentials)
	incomingPool := cloneLLMCredentialsMap(incoming)

	if len(existingPool) == 0 && len(incomingPool) == 0 {
		return nil, normalizePoolVersion(existing.LLMPoolVersion), normalizePoolTimestamp(existing.LLMPoolUpdatedAtMS), normalizePoolUpdatedBy(existing.LLMPoolUpdatedBy), "no_change"
	}

	freshness := comparePoolFreshness(existing.LLMPoolVersion, existing.LLMPoolUpdatedAtMS, existing.LLMPoolUpdatedBy, incomingVersion, incomingUpdatedAt, incomingUpdatedBy)
	preferIncoming := freshness < 0
	direction := "no_change"
	if freshness > 0 {
		direction = "daemon_to_client"
	} else if freshness < 0 {
		direction = "client_to_daemon"
	}

	merged := mergeCredentialPool(existingPool, incomingPool, preferIncoming)

	if llmCredentialsEqual(merged, existingPool) {
		return merged, normalizePoolVersion(existing.LLMPoolVersion), normalizePoolTimestamp(existing.LLMPoolUpdatedAtMS), normalizePoolUpdatedBy(existing.LLMPoolUpdatedBy), direction
	}

	nextVersion := normalizePoolVersion(existing.LLMPoolVersion)
	incomingNormalizedVersion := normalizePoolVersion(incomingVersion)
	if incomingNormalizedVersion > nextVersion {
		nextVersion = incomingNormalizedVersion
	}
	nextVersion++

	return merged, nextVersion, nowMS, "daemon", direction
}

func normalizeAndValidateLLMCredentials(raw map[string]llmCredential) (map[string]llmCredential, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	normalized := make(map[string]llmCredential, len(raw))
	for provider, cred := range raw {
		providerKey := normalizeCredentialProviderKey(provider)
		switch providerKey {
		case "openai", "anthropic", "gemini":
		default:
			return nil, fmt.Errorf("unsupported llm_credentials provider %q", provider)
		}

		trimmedAPIKey := strings.TrimSpace(cred.APIKey)
		if trimmedAPIKey != "" {
			if sopbackend.IsEncryptedSecretValue(trimmedAPIKey) {
				encoded := strings.TrimPrefix(trimmedAPIKey, "enc:v1:")
				if _, decodeErr := base64.StdEncoding.DecodeString(encoded); decodeErr != nil {
					return nil, fmt.Errorf("invalid llm_credentials[%s].api_key: decode secured string: %w", providerKey, decodeErr)
				}
			}
			if _, err := resolveDaemonSecretValue(trimmedAPIKey); err != nil {
				return nil, fmt.Errorf("invalid llm_credentials[%s].api_key: %w", providerKey, err)
			}
		}

		normalized[providerKey] = llmCredential{
			APIKey:      trimmedAPIKey,
			Model:       strings.TrimSpace(cred.Model),
			URL:         strings.TrimSpace(cred.URL),
			UpdatedAtMS: normalizePoolTimestamp(cred.UpdatedAtMS),
			DeletedAtMS: normalizePoolTimestamp(cred.DeletedAtMS),
		}
	}
	return normalized, nil
}

func resolveDaemonSecretValue(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	decrypted, err := sopbackend.DecryptSecretValue(trimmed)
	if err != nil {
		return "", fmt.Errorf("decrypt daemon secret: %w", err)
	}
	return strings.TrimSpace(decrypted), nil
}

func normalizeHQBaseURL(baseURL, port string) string {
	baseURL = strings.TrimSpace(baseURL)
	port = strings.TrimSpace(port)
	if baseURL == "" {
		return ""
	}
	if port == "" {
		return strings.TrimRight(baseURL, "/")
	}

	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		if strings.Contains(baseURL, "://") {
			return strings.TrimRight(baseURL, "/")
		}
		return strings.TrimRight(baseURL, "/") + ":" + port
	}
	if parsedURL.Port() != "" {
		return strings.TrimRight(parsedURL.String(), "/")
	}

	parsedURL.Host = net.JoinHostPort(parsedURL.Hostname(), port)
	return strings.TrimRight(parsedURL.String(), "/")
}

var daemonSessionStore = struct {
	mu   sync.Mutex
	data map[string]daemonSessionContext
}{
	data: make(map[string]daemonSessionContext),
}

var localRuntimeConfigPath string
var daemonLLMPoolStatePath string

func daemonLLMPoolStateFilePathFromConfig(configPath string) string {
	trimmed := strings.TrimSpace(configPath)
	if trimmed == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(trimmed), "daemon_llm_pool_state.json")
}

func hasPersistableLLMState(ctx daemonSessionContext) bool {
	if strings.TrimSpace(ctx.LLMProvider) != "" || strings.TrimSpace(ctx.LLMAPIKey) != "" || strings.TrimSpace(ctx.LLMModel) != "" || strings.TrimSpace(ctx.LLMURL) != "" {
		return true
	}
	if len(ctx.LLMCredentials) > 0 {
		return true
	}
	if normalizePoolVersion(ctx.LLMPoolVersion) > 0 || normalizePoolTimestamp(ctx.LLMPoolUpdatedAtMS) > 0 || strings.TrimSpace(ctx.LLMPoolUpdatedBy) != "" {
		return true
	}
	return false
}

func capturePersistedDaemonLLMState() persistedDaemonLLMState {
	state := persistedDaemonLLMState{
		SchemaVersion: 1,
		Sessions:      map[string]persistedDaemonLLMSessionState{},
	}

	daemonSessionStore.mu.Lock()
	defer daemonSessionStore.mu.Unlock()

	for sessionKey, session := range daemonSessionStore.data {
		if strings.TrimSpace(sessionKey) == "" || !hasPersistableLLMState(session) {
			continue
		}
		state.Sessions[sessionKey] = persistedDaemonLLMSessionState{
			HQBaseURL:          strings.TrimSpace(session.HQBaseURL),
			HQPort:             strings.TrimSpace(session.HQPort),
			HQUserID:           strings.TrimSpace(session.HQUserID),
			HQSessionID:        strings.TrimSpace(session.HQSessionID),
			LLMProvider:        strings.TrimSpace(session.LLMProvider),
			LLMAPIKey:          strings.TrimSpace(session.LLMAPIKey),
			LLMModel:           strings.TrimSpace(session.LLMModel),
			LLMURL:             strings.TrimSpace(session.LLMURL),
			LLMCredentials:     cloneLLMCredentialsMap(session.LLMCredentials),
			LLMPoolVersion:     normalizePoolVersion(session.LLMPoolVersion),
			LLMPoolUpdatedAtMS: normalizePoolTimestamp(session.LLMPoolUpdatedAtMS),
			LLMPoolUpdatedBy:   normalizePoolUpdatedBy(session.LLMPoolUpdatedBy),
			ThinkingLevel:      strings.TrimSpace(session.ThinkingLevel),
			LastUpdatedUnixMS:  session.LastUpdated.UTC().UnixMilli(),
		}
	}

	if len(state.Sessions) == 0 {
		state.Sessions = nil
	}
	return state
}

func persistDaemonLLMPoolState() error {
	path := strings.TrimSpace(daemonLLMPoolStatePath)
	if path == "" {
		return nil
	}

	state := capturePersistedDaemonLLMState()
	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal daemon llm pool state: %w", err)
	}

	encryptedPayload, err := sopbackend.EncryptSecretValue(string(payload))
	if err != nil {
		return fmt.Errorf("encrypt daemon llm pool state: %w", err)
	}

	envelopeBytes, err := json.Marshal(persistedDaemonLLMEnvelope{EncryptedPayload: encryptedPayload})
	if err != nil {
		return fmt.Errorf("marshal daemon llm pool envelope: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create daemon llm pool state dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, "daemon-llm-pool-*.tmp")
	if err != nil {
		return fmt.Errorf("create daemon llm pool temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, writeErr := tmpFile.Write(envelopeBytes); writeErr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write daemon llm pool state: %w", writeErr)
	}
	if err := tmpFile.Chmod(0o600); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod daemon llm pool state temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close daemon llm pool state temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("commit daemon llm pool state: %w", err)
	}

	return nil
}

func loadDaemonLLMPoolState() error {
	path := strings.TrimSpace(daemonLLMPoolStatePath)
	if path == "" {
		return nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read daemon llm pool state: %w", err)
	}

	var envelope persistedDaemonLLMEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("decode daemon llm pool envelope: %w", err)
	}
	if strings.TrimSpace(envelope.EncryptedPayload) == "" {
		return fmt.Errorf("decode daemon llm pool envelope: encrypted_payload is required")
	}

	decryptedPayload, err := sopbackend.DecryptSecretValue(strings.TrimSpace(envelope.EncryptedPayload))
	if err != nil {
		return fmt.Errorf("decrypt daemon llm pool state: %w", err)
	}

	var state persistedDaemonLLMState
	if err := json.Unmarshal([]byte(decryptedPayload), &state); err != nil {
		return fmt.Errorf("decode daemon llm pool state: %w", err)
	}
	if state.SchemaVersion <= 0 {
		state.SchemaVersion = 1
	}

	daemonSessionStore.mu.Lock()
	defer daemonSessionStore.mu.Unlock()
	for sessionKey, persisted := range state.Sessions {
		key := strings.TrimSpace(sessionKey)
		if key == "" {
			continue
		}
		existing := daemonSessionStore.data[key]
		existing.HQBaseURL = firstNonEmpty(strings.TrimSpace(persisted.HQBaseURL), existing.HQBaseURL)
		existing.HQPort = firstNonEmpty(strings.TrimSpace(persisted.HQPort), existing.HQPort)
		existing.HQUserID = firstNonEmpty(strings.TrimSpace(persisted.HQUserID), existing.HQUserID)
		existing.HQSessionID = firstNonEmpty(strings.TrimSpace(persisted.HQSessionID), existing.HQSessionID)
		existing.LLMProvider = firstNonEmpty(strings.TrimSpace(persisted.LLMProvider), existing.LLMProvider)
		existing.LLMAPIKey = firstNonEmpty(strings.TrimSpace(persisted.LLMAPIKey), existing.LLMAPIKey)
		existing.LLMModel = firstNonEmpty(strings.TrimSpace(persisted.LLMModel), existing.LLMModel)
		existing.LLMURL = firstNonEmpty(strings.TrimSpace(persisted.LLMURL), existing.LLMURL)
		existing.ThinkingLevel = firstNonEmpty(strings.TrimSpace(persisted.ThinkingLevel), existing.ThinkingLevel)
		if len(persisted.LLMCredentials) > 0 {
			normalizedCredentials, normalizeErr := normalizeAndValidateLLMCredentials(persisted.LLMCredentials)
			if normalizeErr == nil {
				existing.LLMCredentials = cloneLLMCredentialsMap(normalizedCredentials)
			} else {
				log.Printf("daemon llm pool load: ignoring invalid credentials for session %s: %v", key, normalizeErr)
			}
		}
		existing.LLMPoolVersion = normalizePoolVersion(persisted.LLMPoolVersion)
		existing.LLMPoolUpdatedAtMS = normalizePoolTimestamp(persisted.LLMPoolUpdatedAtMS)
		existing.LLMPoolUpdatedBy = normalizePoolUpdatedBy(persisted.LLMPoolUpdatedBy)
		if persisted.LastUpdatedUnixMS > 0 {
			existing.LastUpdated = time.UnixMilli(persisted.LastUpdatedUnixMS).UTC()
		}
		daemonSessionStore.data[key] = existing
	}

	return nil
}

type daemonLogLevel string

const (
	daemonLogLevelDebug daemonLogLevel = "debug"
	daemonLogLevelInfo  daemonLogLevel = "info"
	daemonLogLevelWarn  daemonLogLevel = "warn"
	daemonLogLevelError daemonLogLevel = "error"
)

const daemonVerboseLoggingEnv = "SOP_DAEMON_VERBOSE"

var activeDaemonLogLevel = daemonLogLevelInfo

func parseDaemonLogLevel(value string) (daemonLogLevel, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", "info":
		return daemonLogLevelInfo, nil
	case "debug":
		return daemonLogLevelDebug, nil
	case "warn":
		return daemonLogLevelWarn, nil
	case "error":
		return daemonLogLevelError, nil
	default:
		return "", fmt.Errorf("unsupported log level %q (expected one of: debug, info, warn, error)", value)
	}
}

func shouldLogVerbose() bool {
	return activeDaemonLogLevel == daemonLogLevelDebug
}

func logVerbose(format string, args ...any) {
	if shouldLogVerbose() {
		log.Printf(format, args...)
	}
}

func main() {
	// Redirect daemon operational logs to stderr so they don't pollute command output (stdout)
	// This ensures only PTY command output goes to stdout and gets returned to the LLM
	log.SetOutput(os.Stderr)

	// Check for worker mode BEFORE flag parsing (worker mode needs minimal setup)
	// Worker mode flag must be first argument to avoid conflicts with other flags
	if len(os.Args) > 1 && os.Args[1] == "--worker-mode" {
		if err := runPTYWorker(); err != nil {
			log.Fatalf("[PTY Worker] Fatal error: %v", err)
		}
		os.Exit(0)
	}

	port := flag.Int("port", 9090, "Port to run the local SOP daemon on")
	configPath := flag.String("config", "", "Path to the SOP daemon configuration file")
	printConfigPath := flag.Bool("print-config-path", false, "Print the resolved daemon config path and exit")
	logLevelFlag := flag.String("log-level", "info", "Set daemon log verbosity (debug, info, warn, error)")
	policyMaxRequests := flag.Int("rate-limit-max-requests", 20, "Maximum local requests per session before enforcement kicks in")
	policyWindow := flag.Duration("rate-limit-window", time.Minute, "Rate limit window for local daemon requests")
	accessTokenTTL := flag.Duration("access-token-ttl", time.Hour, "Access token expiry duration")
	refreshTokenTTL := flag.Duration("refresh-token-ttl", 7*24*time.Hour, "Refresh token expiry duration (1 week default)")
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

	resolvedLogLevel, err := parseDaemonLogLevel(*logLevelFlag)
	if err != nil {
		log.Fatalf("Invalid log level: %v", err)
	}
	if !flagWasProvided("log-level") {
		value := strings.TrimSpace(os.Getenv(daemonVerboseLoggingEnv))
		switch strings.ToLower(value) {
		case "1", "true", "yes", "on":
			resolvedLogLevel = daemonLogLevelDebug
		}
	}
	activeDaemonLogLevel = resolvedLogLevel

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

	// Resolve the daemon config path, but do not auto-create it here.
	resolvedConfig, err := resolveDaemonConfigLocation(*configPath, nil)
	if err != nil {
		log.Fatalf("Failed to resolve daemon config: %v", err)
	}
	localRuntimeConfigPath = resolvedConfig
	daemonLLMPoolStatePath = daemonLLMPoolStateFilePathFromConfig(resolvedConfig)
	sopbackend.SetConfigFile(resolvedConfig)

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

	// WARNING FOR FUTURE COPILOTS / MAINTAINERS:
	//
	// Treat the daemon as a separate lightweight app, not as a thin variant of
	// httpserver. It may reuse shared libraries from sopbackend, but its product
	// requirements are different:
	//   - local-first runtime
	//   - lightweight app behavior
	//   - daemon-specific provisioning flow
	//   - daemon-specific auth/bootstrap lifecycle
	//
	// Do NOT use httpserver as the default architectural template for daemon
	// changes. Shared code reuse is allowed, but daemon behavior must be decided
	// from daemon requirements first.
	//
	// In particular, avoid reintroducing assumptions that daemon should:
	//   - share HQ/httpserver provisioning handlers
	//   - auto-mutate missing config/state just because httpserver does
	//   - grow toward the heavier HQ control-plane model when a lightweight local
	//     behavior is sufficient
	//
	// Initialize shared runtime dependencies here only as reusable building
	// blocks for the daemon app.
	policy := sharedagent.AutomationPolicy{
		Enabled:               true,
		AllowShellCommands:    true,
		AllowEmail:            false,
		RequireApproval:       true,
		DefaultTimeoutSeconds: 600,
		MaxTimeoutSeconds:     1800,
	}
	if _, err := sopbackend.InitializeRuntime(context.Background(), resolvedConfig, sopbackend.RuntimeOptions{
		EnableLocalAutomation: true,
		AutomationPolicy:      &policy,
		EnableStoresFeature:   true,
		HostKind:              sopbackend.HostKindDaemon,
	}, *port, nil); err != nil {
		log.Printf("Warning: backend bootstrap failed: %v", err)
	} else {
		log.Printf("Loaded daemon configuration from: %s", resolvedConfig)
	}
	if err := loadDaemonLLMPoolState(); err != nil {
		log.Printf("Warning: failed to load daemon LLM pool state from disk: %v", err)
	}
	cfg := sopbackend.GetConfig()
	configureRecoveryStatePersistence(cfg)
	if cfg.SystemDB != nil {
		cfg.SystemDB.IsSystem = true
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

	if cfg.SystemDB != nil {
		if err := bootstrapSOPKnowledgeBaseIfNeeded(
			context.Background(),
			cfg.SystemDB.Name,
			func(ctx context.Context, spaceName string) (bool, error) {
				dbOpts, err := sopbackend.GetDBOptions(ctx, cfg.SystemDB.Name)
				if err != nil {
					return false, fmt.Errorf("resolve DB options for bootstrap: %w", err)
				}
				db := aidb.NewDatabase(dbOpts)
				return db.KnowledgeBaseExists(ctx, spaceName)
			},
			func(ctx context.Context, dbName string) error {
				log.Printf("Bootstrapping built-in SOP knowledge base into database %s", dbName)
				return sopbackend.PreloadSpaceFromTemplate(ctx, ai.DefaultKBName, dbName, nil)
			},
		); err != nil {
			log.Printf("Warning: failed to bootstrap built-in SOP knowledge base: %v", err)
		}
	}

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

	// Seed initial federated content only after daemon provisioning creates a usable system DB.
	provisioningDecision := sopbackend.CurrentProvisioningDecision()
	if shouldSeed, skipReason := shouldSeedFederatedContent(cfg, provisioningDecision); shouldSeed {
		log.Printf("Seeding initial federated content...")
		if err := SeedInitialFederatedContent(context.Background()); err != nil {
			log.Printf("Warning: failed to seed federated content: %v", err)
		} else {
			log.Printf("Federated content seeded successfully")
		}
	} else {
		log.Printf("Skipping federated content seeding: %s", skipReason)
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
	mux.HandleFunc("/api/daemon/bootstrap-secret", handleOptionsAndCORS(http.HandlerFunc(sopbackend.HandleDaemonBootstrapSecret)))
	mux.HandleFunc("/api/daemon/auth", handleOptionsAndCORS(http.HandlerFunc(authHandler.HandleBootstrap)))
	mux.HandleFunc("/api/daemon/challenge", handleOptionsAndCORS(http.HandlerFunc(authHandler.HandleBootstrapChallenge)))
	mux.HandleFunc("/api/daemon/refresh", handleOptionsAndCORS(http.HandlerFunc(authHandler.HandleRefresh)))
	mux.HandleFunc("/api/daemon/hq-auth", handleOptionsAndCORS(http.HandlerFunc(authHandler.HandleHQAuth)))
	mux.HandleFunc("/api/daemon/provision/status", handleOptionsAndCORS(authHandler.RequireAuth(http.HandlerFunc(handleDaemonProvisionStatus))))
	mux.HandleFunc("/api/daemon/provision/simple", handleOptionsAndCORS(authHandler.RequireAuth(http.HandlerFunc(handleDaemonProvisionSimple))))
	mux.HandleFunc("/api/daemon/provision/system", handleOptionsAndCORS(authHandler.RequireAuth(http.HandlerFunc(handleDaemonProvisionSystem))))
	mux.HandleFunc("/api/daemon/receive-space", handleOptionsAndCORS(http.HandlerFunc(handleReceiveSpaceFromHQ)))
	mux.HandleFunc("/api/daemon/vectorization/recovery", handleOptionsAndCORS(http.HandlerFunc(handleGetVectorizationRecoveryStatus)))
	mux.HandleFunc("/api/daemon/secrets/encrypt", handleOptionsAndCORS(authHandler.RequireAuth(http.HandlerFunc(handleEncryptDaemonSecret))))

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

	// Terminal output endpoint for AI command monitoring
	mux.HandleFunc("/api/terminal/status", handleOptionsAndCORS(authHandler.RequireAuth(http.HandlerFunc(handleTerminalStatus))))
	mux.HandleFunc("/api/terminal/stop", handleOptionsAndCORS(authHandler.RequireAuth(http.HandlerFunc(handleTerminalStop))))

	// Override/Add daemon-specific handlers WITH authentication
	mux.HandleFunc("/api/execute", handleOptionsAndCORS(authHandler.RequireAuth(withRateLimit(localRegistry, http.HandlerFunc(executeHandler)))))
	mux.HandleFunc("/api/handoff", handleOptionsAndCORS(authHandler.RequireAuth(withRateLimit(localRegistry, http.HandlerFunc(handleUIHandoff)))))

	ragSvc.registerBridgeRoutes(mux, localRegistry, authHandler)

	// IMPORTANT: /api/ask (daemon) vs /api/ai/chat (HQ) - Separate by Design
	//
	// The daemon provides /api/ask with JWT auth for LOCAL runtime mode.
	// This is SEPARATE from HQ's /api/ai/chat because daemon is now treated as a
	// fully separate lightweight app, not as a reduced copy of httpserver.
	// Shared agent/runtime pieces may still be reused internally, but endpoint
	// behavior, provisioning, auth, and lifecycle rules are allowed to diverge.
	//
	// Keep this boundary explicit when modifying daemon. If a future change makes
	// sense for HQ/httpserver but adds weight or coupling to daemon, prefer a
	// daemon-specific implementation.
	//
	// This is SEPARATE from HQ's /api/ai/chat for concrete reasons:
	//
	// 1. DIFFERENT AUTH MODELS:
	//    - /api/ask: JWT bearer tokens (bootstrap file → JWT)
	//    - /api/ai/chat: Session cookies (internet login)
	//
	// 2. DIFFERENT PAYLOAD STRUCTURES:
	//    - /api/ask: {query, auth{access_token, refresh_token}, hq{base_url, user_id}, llm_provider, ...}
	//    - /api/ai/chat: {message, database, store, agent, provider, domain, ...}
	//
	// 3. DAEMON-SPECIFIC INTEGRATION:
	//    - HQ token handoff and renewal in payload
	//    - Daemon session store (daemonSessionStore) for context persistence
	//    - PTY shell session management
	//    - Federation sync state
	//
	// 4. INDEPENDENCE:
	//    - Daemon evolves for local-first features
	//    - HQ evolves for multi-tenant features
	//    - Separate endpoints = clearer boundaries, less coupling risk
	//
	// Some Ask internals are shared through sopbackend.AskSharedAgent(), but that
	// shared implementation detail must not be used as justification to collapse
	// daemon and HQ into one app model.
	//
	// STREAMING: /api/ask now supports NDJSON streaming (same as /api/ai/chat)!
	//            UI sends Accept: application/x-ndjson and gets real-time progress.
	//            Falls back gracefully to plain JSON for backward compatibility.
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
	log.Printf("SOP Daemon v%s", sop.Version)
	log.Printf("Starting on %s", addr)
	log.Printf("Authentication: JWT with bootstrap secret")
	log.Printf("Token expiry: access=%v, refresh=%v", *accessTokenTTL, *refreshTokenTTL)
	log.Printf("Auth endpoints: /api/daemon/auth, /api/daemon/refresh")
	log.Printf("Full backend API available (via sopbackend)")
	log.Printf("AI chat endpoint: /api/ask (JWT auth, NDJSON streaming enabled)")
	log.Printf("Daemon-specific endpoints: /api/execute, /api/handoff (authenticated)")
	logVerbose("Waiting for UI authentication...")

	server := &http.Server{Addr: addr, Handler: sopbackend.NewObservabilityHandler(corsHandler)}
	go func() {
		<-hostCtx.Done()
		log.Printf("Shutting down daemon...")

		// Cleanup workers before shutting down server
		workerMgr := getGlobalWorkerManager()
		if workerMgr != nil {
			workerMgr.cleanup()
		}

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

// Flush implements http.Flusher to support streaming responses
func (w *statusResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
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
			// IMPORTANT: Combine cd && pwd into single command to avoid PTY sync issues
			if strings.HasPrefix(trimmedCommand, "cd ") || trimmedCommand == "cd" {
				// Execute "cd dir && pwd" as a single command to get the new directory
				combinedCmd := trimmedCommand + " && pwd"
				output, stillRunning, err := shellMgr.RunWithStreaming(sessionID, combinedCmd, time.Duration(timeoutSeconds)*time.Second, streamCallback)
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
				// The output is the new directory from pwd
				resp := ExecuteResponse{
					Stdout:   strings.TrimSpace(output),
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

func handleTerminalStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get session_id from query params (e.g., "copilot-default")
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		sessionID = "default"
	}

	// Get the shell session manager
	shellMgr := GetGlobalShellSessionManager()
	if shellMgr == nil {
		respondJSON(w, http.StatusOK, map[string]any{
			"session_id": sessionID,
			"output":     "",
			"available":  false,
		})
		return
	}

	// Get accumulated output and running status from the session
	output, exists, isRunning := shellMgr.GetSessionStatus(sessionID)

	respondJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"output":     output,
		"available":  exists,
		"running":    isRunning,
	})
}

func handleTerminalStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get session_id from query params
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		sessionID = "default"
	}

	// Get the shell session manager
	shellMgr := GetGlobalShellSessionManager()
	if shellMgr == nil {
		respondJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "shell manager not available",
		})
		return
	}

	// Send interrupt signal to the session
	if err := shellMgr.SendSessionInterrupt(sessionID); err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]any{
			"error": err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"message":    "interrupt signal sent",
		"session_id": sessionID,
	})
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
	hqPort := strings.TrimSpace(req.HQ.Port)
	if hqBaseURL == "" {
		respondJSON(w, http.StatusBadRequest, UIHandoffResponse{Error: "HQ base URL is required"})
		return
	}

	normalizedHQBaseURL := normalizeHQBaseURL(hqBaseURL, hqPort)
	if _, err := resolveDaemonSecretValue(req.LLMAPIKey); err != nil {
		respondJSON(w, http.StatusBadRequest, UIHandoffResponse{Error: "Invalid llm_api_key: " + err.Error()})
		return
	}
	normalizedCredentials, credentialsErr := normalizeAndValidateLLMCredentials(req.LLMCredentials)
	if credentialsErr != nil {
		respondJSON(w, http.StatusBadRequest, UIHandoffResponse{Error: credentialsErr.Error()})
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
	storedSessionContext := daemonSessionStore.data[ctxKey]
	mergedCredentials, mergedPoolVersion, mergedPoolUpdatedAtMS, mergedPoolUpdatedBy, syncDirection := mergeLLMPoolState(
		storedSessionContext,
		normalizedCredentials,
		req.LLMPoolVersion,
		req.LLMPoolUpdatedAtMS,
		req.LLMPoolUpdatedBy,
	)
	nextProvider := strings.TrimSpace(req.LLMProvider)
	if nextProvider == "" {
		nextProvider = strings.TrimSpace(storedSessionContext.LLMProvider)
	}
	nextAPIKey := strings.TrimSpace(req.LLMAPIKey)
	if nextAPIKey == "" {
		nextAPIKey = strings.TrimSpace(storedSessionContext.LLMAPIKey)
	}
	nextModel := strings.TrimSpace(req.LLMModel)
	if nextModel == "" {
		nextModel = strings.TrimSpace(storedSessionContext.LLMModel)
	}
	nextURL := strings.TrimSpace(req.LLMURL)
	if nextURL == "" {
		nextURL = strings.TrimSpace(storedSessionContext.LLMURL)
	}
	nextThinkingLevel := strings.TrimSpace(req.ThinkingLevel)
	if nextThinkingLevel == "" {
		nextThinkingLevel = strings.TrimSpace(storedSessionContext.ThinkingLevel)
	}
	daemonSessionStore.data[ctxKey] = daemonSessionContext{
		AccessToken:        accessToken,
		RefreshToken:       refreshToken,
		HQBaseURL:          normalizedHQBaseURL,
		HQPort:             hqPort,
		HQUserID:           strings.TrimSpace(req.HQ.UserID),
		HQSessionID:        strings.TrimSpace(req.HQ.SessionID),
		LLMProvider:        nextProvider,
		LLMAPIKey:          nextAPIKey,
		LLMModel:           nextModel,
		LLMURL:             nextURL,
		LLMCredentials:     cloneLLMCredentialsMap(mergedCredentials),
		LLMPoolVersion:     mergedPoolVersion,
		LLMPoolUpdatedAtMS: mergedPoolUpdatedAtMS,
		LLMPoolUpdatedBy:   mergedPoolUpdatedBy,
		ThinkingLevel:      nextThinkingLevel,
		LastUpdated:        time.Now().UTC(),
	}
	daemonSessionStore.mu.Unlock()
	if err := persistDaemonLLMPoolState(); err != nil {
		log.Printf("Warning: failed to persist daemon LLM pool state: %v", err)
	}

	log.Printf("Accepted UI handoff for session %s", ctxKey)
	respondJSON(w, http.StatusOK, UIHandoffResponse{
		Accepted:             true,
		Message:              "handoff accepted",
		SessionID:            ctxKey,
		LLMProvider:          nextProvider,
		LLMModel:             nextModel,
		LLMURL:               nextURL,
		LLMCredentials:       cloneLLMCredentialsMap(mergedCredentials),
		LLMPoolVersion:       mergedPoolVersion,
		LLMPoolUpdatedAtMS:   mergedPoolUpdatedAtMS,
		LLMPoolUpdatedBy:     mergedPoolUpdatedBy,
		LLMPoolSyncDirection: syncDirection,
		WelcomeContext:       buildLocalRAGWelcomeContext(),
		NeedsRecovery:        currentVectorizationNeedsRecovery(),
		Recovery:             currentVectorizationRecoveryState(),
		StoragePathRecovery:  currentStoragePathRecoveryState(),
	})
}

func buildLocalRAGWelcomeContext() string {
	welcomeContext := "Hello! I am your local <strong>Daemon Assistant</strong>. I'm here to help you work with the local shell and embedded RAG runtime directly from this desktop experience.<br><br>I can help you:<br><ul style=\"color: var(--text-muted); font-size: 0.9em; margin: 0 0 8px 18px; padding: 0; list-style-position: outside;\"><li style=\"margin: 0 0 4px 0; padding: 0;\">run shell commands and inspect command output</li><li style=\"margin: 0 0 4px 0; padding: 0;\">answer questions using the local RAG knowledge you configured</li><li style=\"margin: 0 0 4px 0; padding: 0;\">help you troubleshoot local workflows from the command line</li></ul>Type <strong>/list_tools</strong> to see the available commands in this session.<br><br><em style=\"color: var(--text-muted); font-size: 0.9em; display: block; margin-bottom: 4px;\">Shell-style commands such as <strong>ls</strong>, <strong>pwd</strong>, and <strong>cd /Volumes</strong> can be run directly in the local shell, and slash-prefixed commands such as <strong>/list_tools</strong> are available for built-in SOP actions.</em><br><em style=\"color: var(--text-muted); font-size: 0.9em; display: block; margin-bottom: 4px;\">Try asking things like:</em><ul style=\"color: var(--text-muted); font-size: 0.9em; margin: 0 0 0 18px; padding: 0; list-style-position: outside;\"><li style=\"margin: 0 0 2px 0; padding: 0;\">\"Show me the current directory and files\"</li><li style=\"margin: 0 0 2px 0; padding: 0;\">\"Explain how to run the local daemon\"</li><li style=\"margin: 0 0 2px 0; padding: 0;\">\"Help me inspect my local project structure\"</li></ul>"

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

func handleEncryptDaemonSecret(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req daemonSecretEncryptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON body: " + err.Error()})
		return
	}

	trimmed := strings.TrimSpace(req.Value)
	if trimmed == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "value is required"})
		return
	}

	encrypted, err := sopbackend.EncryptSecretValue(trimmed)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encrypt secret"})
		return
	}

	respondJSON(w, http.StatusOK, daemonSecretEncryptResponse{EncryptedValue: encrypted})
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
			}
			if err := vectorizationRecoveryExecutor(r.Context(), req.KBName, req.TargetType, req.TargetID); err != nil {
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
	if req.Stdin != "" {
		cmd.Stdin = strings.NewReader(req.Stdin)
	}
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

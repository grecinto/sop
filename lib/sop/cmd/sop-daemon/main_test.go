package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	sharedagent "github.com/sharedcode/sop/ai/agent"
	sopfs "github.com/sharedcode/sop/fs"
	"github.com/sharedcode/sop/tools/sopbackend"
)

type stubSharedAgent struct {
	name string
}

type recordingQueryAgent struct {
	queries []string
}

type executorAwareAgent struct {
	gotExecutor bool
}

var workerModeEnabled bool

func init() {
	flag.BoolVar(&workerModeEnabled, "worker-mode", false, "run the PTY worker and exit")
}

func TestMain(m *testing.M) {
	// CRITICAL: Check for worker mode BEFORE any test framework initialization
	// We check os.Args directly (not flags) because flags haven't been parsed yet
	if len(os.Args) > 1 && os.Args[1] == "--worker-mode" {
		// Save real stdout for JSON protocol BEFORE any redirection
		realStdout := os.Stdout
		// Redirect global stdout to stderr to prevent test framework pollution
		os.Stdout = os.Stderr
		// Also redirect logs to stderr
		log.SetOutput(os.Stderr)

		// Run worker with the saved real stdout for JSON
		if err := runPTYWorkerWithOutput(realStdout); err != nil {
			log.Fatalf("[PTY Worker] Fatal error: %v", err)
		}
		os.Exit(0)
	}

	// Redirect logs to stderr (same as production main())
	// This prevents test framework logs from polluting worker stdout JSON protocol
	log.SetOutput(os.Stderr)

	os.Exit(m.Run())
}

func (a *stubSharedAgent) Open(ctx context.Context) error  { return nil }
func (a *stubSharedAgent) Close(ctx context.Context) error { return nil }
func (a *stubSharedAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}
func (a *stubSharedAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	return "shared-agent", nil
}
func (a *stubSharedAgent) Execute(ctx context.Context, toolName string, args map[string]any) (string, error) {
	return "", nil
}
func (a *stubSharedAgent) ListTools(ctx context.Context) ([]ai.ToolDefinition, error) {
	return nil, nil
}

func (a *recordingQueryAgent) Open(ctx context.Context) error  { return nil }
func (a *recordingQueryAgent) Close(ctx context.Context) error { return nil }
func (a *recordingQueryAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}
func (a *recordingQueryAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	a.queries = append(a.queries, query)
	return "ok", nil
}
func (a *recordingQueryAgent) Execute(ctx context.Context, toolName string, args map[string]any) (string, error) {
	return "", nil
}
func (a *recordingQueryAgent) ListTools(ctx context.Context) ([]ai.ToolDefinition, error) {
	return nil, nil
}

func (a *executorAwareAgent) Open(ctx context.Context) error  { return nil }
func (a *executorAwareAgent) Close(ctx context.Context) error { return nil }
func (a *executorAwareAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}
func (a *executorAwareAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	_, a.gotExecutor = ctx.Value(ai.CtxKeyExecutor).(*sopbackend.DefaultToolExecutor)
	return "ok", nil
}
func (a *executorAwareAgent) Execute(ctx context.Context, toolName string, args map[string]any) (string, error) {
	return "", nil
}
func (a *executorAwareAgent) ListTools(ctx context.Context) ([]ai.ToolDefinition, error) {
	return nil, nil
}

func resetDefaultStorageHealthPolicyForTest(t *testing.T) {
	t.Helper()
	restore := sop.SetDefaultStorageHealthPolicy(sop.NewStorageHealthPolicy())
	t.Cleanup(restore)
}

func TestBuildListenAddr(t *testing.T) {
	if got, want := buildListenAddr("127.0.0.1", 9090), "127.0.0.1:9090"; got != want {
		t.Fatalf("buildListenAddr() = %q, want %q", got, want)
	}

	if got, want := buildListenAddr("0.0.0.0", 8080), "0.0.0.0:8080"; got != want {
		t.Fatalf("buildListenAddr() = %q, want %q", got, want)
	}
}

func TestParseDaemonLogLevel(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    daemonLogLevel
		wantErr bool
	}{
		{name: "default", input: "", want: daemonLogLevelInfo},
		{name: "debug", input: "debug", want: daemonLogLevelDebug},
		{name: "info", input: "info", want: daemonLogLevelInfo},
		{name: "warn", input: "warn", want: daemonLogLevelWarn},
		{name: "error", input: "error", want: daemonLogLevelError},
		{name: "invalid", input: "verbosez", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDaemonLogLevel(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseDaemonLogLevel(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDaemonLogLevel(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("parseDaemonLogLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCurrentStorageHealthStateUsesSharedPolicyWhenRuntimePolicyIsUnavailable(t *testing.T) {
	resetDefaultStorageHealthPolicyForTest(t)

	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = nil
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	policy := sop.DefaultStorageHealthPolicy()
	policy.RecordFailure("write", "/tmp/shared-store", syscall.EROFS)

	if !currentVectorizationNeedsRecovery() {
		t.Fatal("expected shared storage-health policy to surface recovery need")
	}

	state := currentStoragePathRecoveryState()
	if state.Status != "read_only" {
		t.Fatalf("expected shared storage-health policy status to be read_only, got %q", state.Status)
	}
	if state.Path != "/tmp/shared-store" {
		t.Fatalf("expected shared storage-health policy path to be preserved, got %q", state.Path)
	}
}

func TestCurrentStorageHealthStateFallsBackToSharedPolicyWhenRuntimePolicyHasNoState(t *testing.T) {
	resetDefaultStorageHealthPolicyForTest(t)

	tmpDir := t.TempDir()
	runtimePolicy := newRuntimeVectorizationFailurePolicy(tmpDir)
	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = runtimePolicy.(runtimeVectorizationFailurePolicyHandle)
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	policy := sop.DefaultStorageHealthPolicy()
	policy.RecordFailure("write", "/tmp/shared-store", syscall.EROFS)

	if !currentVectorizationNeedsRecovery() {
		t.Fatal("expected shared storage-health policy to surface recovery need even when the runtime policy is present but empty")
	}

	state := currentStoragePathRecoveryState()
	if state.Status != "read_only" {
		t.Fatalf("expected shared storage-health policy status to be read_only when the runtime policy has no state, got %q", state.Status)
	}
	if state.Path != "/tmp/shared-store" {
		t.Fatalf("expected shared storage-health policy path to be preserved when the runtime policy has no state, got %q", state.Path)
	}
}

func TestHandleUIHandoffIncludesStoragePathRecoveryState(t *testing.T) {
	resetDefaultStorageHealthPolicyForTest(t)

	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.RecordStoragePathFailure("write", "/tmp/fake-store", os.ErrNotExist)
	}

	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	body := strings.NewReader(`{"query":"hello","hq":{"base_url":"https://example.test"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/handoff", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleUIHandoff(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleUIHandoff() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload UIHandoffResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode handoff payload: %v", err)
	}
	if !payload.NeedsRecovery {
		t.Fatal("expected handoff payload to report storage-path recovery needs")
	}
	if payload.StoragePathRecovery.Status != "degraded" {
		t.Fatalf("expected storage path recovery status to be degraded, got %q", payload.StoragePathRecovery.Status)
	}
	if payload.StoragePathRecovery.Path != "/tmp/fake-store" {
		t.Fatalf("expected storage path recovery path to be preserved, got %q", payload.StoragePathRecovery.Path)
	}
}

func TestRetryPendingVectorizationRecoveryHonorsBackoff(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-backoff", os.ErrNotExist)
	}

	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	previousHandler := vectorizationRecoveryExecutor
	called := 0
	vectorizationRecoveryExecutor = func(ctx context.Context, kbName, targetType, targetID string) error {
		called++
		return nil
	}
	defer func() { vectorizationRecoveryExecutor = previousHandler }()

	activeRuntimeVectorizationFailurePolicy.RecordRetryAttempt("kb", "category", "cat-backoff")

	if count := retryPendingVectorizationRecovery(context.Background(), false); count != 0 {
		t.Fatalf("expected backoff to suppress immediate retry, got %d", count)
	}
	if called != 0 {
		t.Fatalf("expected recovery executor to be skipped while backoff is active, got %d calls", called)
	}
}

func TestRetryPendingVectorizationRecoverySkipsWhenStorageHealthDisablesAutoRetry(t *testing.T) {
	resetDefaultStorageHealthPolicyForTest(t)

	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-storage-gate", os.ErrNotExist)
	}
	policy.RecordStoragePathFailure("write", "/tmp/fake-store", syscall.EROFS)

	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	previousHandler := vectorizationRecoveryExecutor
	called := 0
	vectorizationRecoveryExecutor = func(ctx context.Context, kbName, targetType, targetID string) error {
		called++
		return nil
	}
	defer func() { vectorizationRecoveryExecutor = previousHandler }()

	if count := retryPendingVectorizationRecovery(context.Background(), false); count != 0 {
		t.Fatalf("expected storage-health policy to block auto-retry, got %d", count)
	}
	if called != 0 {
		t.Fatalf("expected recovery executor to be skipped when auto-retry is disabled, got %d calls", called)
	}
}

func TestStorageHealthAllowsHigherLevelAutoRetryUsesSharedPolicyState(t *testing.T) {
	resetDefaultStorageHealthPolicyForTest(t)

	tmpDir := t.TempDir()
	runtimePolicy := newRuntimeVectorizationFailurePolicy(tmpDir)
	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = runtimePolicy.(runtimeVectorizationFailurePolicyHandle)
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	restore := sop.SetDefaultStorageHealthPolicy(sop.NewStorageHealthPolicy())
	defer restore()

	policy := sop.DefaultStorageHealthPolicy()
	policy.RecordFailure("write", "/tmp/shared-store", syscall.EROFS)

	if storageHealthAllowsHigherLevelAutoRetry() {
		t.Fatal("expected shared storage-health policy to disable higher-level auto-retry for read-only storage")
	}
}

func TestResolveVectorizationRecoveryStateDirUsesConfiguredPathWhenWritable(t *testing.T) {
	configDir := t.TempDir()
	customDir := filepath.Join(configDir, "recovery-store")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom dir: %v", err)
	}

	cfg := sopbackend.Config{VectorizationRecoveryStoragePath: customDir}
	stateDir := resolveVectorizationRecoveryStateDir(filepath.Join(configDir, "config.json"), cfg)
	if stateDir != customDir {
		t.Fatalf("expected configured path %q, got %q", customDir, stateDir)
	}
}

func TestResolveVectorizationRecoveryStateDirFallsBackWhenConfiguredPathIsUnavailable(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.json")
	customDir := filepath.Join(configDir, "recovery-store")
	if err := os.WriteFile(filepath.Join(customDir), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cfg := sopbackend.Config{VectorizationRecoveryStoragePath: customDir}
	stateDir := resolveVectorizationRecoveryStateDir(configPath, cfg)
	want := filepath.Join(configDir, "state")
	if stateDir != want {
		t.Fatalf("expected fallback path %q, got %q", want, stateDir)
	}
}

func TestConfigureRecoveryStatePersistenceUpdatesConfigFile(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.json")
	initialCfg := sopbackend.Config{ConfigFile: configPath, VectorizationRecoveryStoragePath: filepath.Join(configDir, "old-recovery")}
	sopbackend.SetConfig(initialCfg)

	t.Cleanup(func() {
		sopfs.SetRecoveryStateConfigUpdater(nil)
		sopfs.SetRecoveryStateCleanupPath("")
		sopbackend.SetConfig(sopbackend.Config{})
	})

	updater := configureRecoveryStatePersistence(initialCfg)
	if updater == nil {
		t.Fatal("expected recovery-state persistence hook to be registered")
	}

	newPath := filepath.Join(configDir, "new-recovery")
	if err := updater(newPath); err != nil {
		t.Fatalf("update recovery path: %v", err)
	}

	cfg := sopbackend.GetConfig()
	if cfg.VectorizationRecoveryStoragePath != newPath {
		t.Fatalf("expected config to record %q, got %q", newPath, cfg.VectorizationRecoveryStoragePath)
	}

	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read persisted config: %v", err)
	}

	var persisted sopbackend.Config
	if err := json.Unmarshal(contents, &persisted); err != nil {
		t.Fatalf("decode persisted config: %v", err)
	}
	if persisted.VectorizationRecoveryStoragePath != newPath {
		t.Fatalf("expected persisted config to record %q, got %q", newPath, persisted.VectorizationRecoveryStoragePath)
	}
}

func TestBuildLocalRAGWelcomeContextIncludesRecoveryActions(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-9", os.ErrNotExist)
	}

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	t.Cleanup(func() {
		activeRuntimeVectorizationFailurePolicy = previous
	})

	contextHTML := buildLocalRAGWelcomeContext()
	if !strings.Contains(contextHTML, "recovery-action-button") {
		t.Fatalf("expected recovery action buttons in welcome context, got %q", contextHTML)
	}
	if !strings.Contains(contextHTML, `data-action="recover"`) {
		t.Fatalf("expected recover action metadata in welcome context, got %q", contextHTML)
	}
	if !strings.Contains(contextHTML, `data-action="ignore"`) {
		t.Fatalf("expected ignore action metadata in welcome context, got %q", contextHTML)
	}
	if !strings.Contains(contextHTML, `data-action="clear_and_rebuild"`) {
		t.Fatalf("expected clear-and-rebuild action metadata in welcome context, got %q", contextHTML)
	}
}

func TestHandleVectorizationRecoveryStatusClearAndRebuildActionInvokesRecoveryHandler(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-clear", os.ErrNotExist)
	}

	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	previousHandler := vectorizationRecoveryExecutor
	called := false
	vectorizationRecoveryExecutor = func(ctx context.Context, kbName, targetType, targetID string) error {
		called = true
		if kbName != "kb" || targetType != "category" || targetID != "cat-clear" {
			t.Fatalf("unexpected recovery target: kb=%q targetType=%q targetID=%q", kbName, targetType, targetID)
		}
		return nil
	}
	defer func() { vectorizationRecoveryExecutor = previousHandler }()

	body := strings.NewReader(`{"action":"clear_and_rebuild","kb_name":"kb","target_type":"category","target_id":"cat-clear"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/vectorization/recovery", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !called {
		t.Fatal("expected clear_and_rebuild action to invoke recovery handler")
	}

	var payload struct {
		Updated bool   `json:"updated"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode clear-and-rebuild payload: %v", err)
	}
	if !payload.Updated {
		t.Fatal("expected updated flag to be true")
	}
	if !strings.Contains(strings.ToLower(payload.Message), "clear") {
		t.Fatalf("expected clear-and-rebuild message, got %q", payload.Message)
	}
}

func TestHandleVectorizationRecoveryStatusReturnsPersistedState(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-4", os.ErrNotExist)
	}

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	t.Cleanup(func() {
		activeRuntimeVectorizationFailurePolicy = previous
	})

	req := httptest.NewRequest(http.MethodGet, "/api/daemon/vectorization/recovery", nil)
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload struct {
		HasRecovery bool                                `json:"has_recovery"`
		Latest      runtimeVectorizationRecoveryState   `json:"latest"`
		States      []runtimeVectorizationRecoveryState `json:"states"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode recovery status payload: %v", err)
	}
	if !payload.HasRecovery {
		t.Fatalf("expected recovery payload to report recovery")
	}
	if len(payload.States) == 0 {
		t.Fatalf("expected recovery states to be listed")
	}
	if payload.Latest.TargetID != "cat-4" {
		t.Fatalf("expected latest target id to be cat-4, got %q", payload.Latest.TargetID)
	}
	if payload.Latest.Prompt == "" {
		t.Fatalf("expected recovery prompt to be present")
	}
}

func TestHandleVectorizationRecoveryStatusIncludesNeedsRecoveryFlag(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-8", os.ErrNotExist)
	}

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	t.Cleanup(func() {
		activeRuntimeVectorizationFailurePolicy = previous
	})

	req := httptest.NewRequest(http.MethodGet, "/api/daemon/vectorization/recovery", nil)
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload struct {
		NeedsRecovery bool `json:"needs_recovery"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode recovery needs payload: %v", err)
	}
	if !payload.NeedsRecovery {
		t.Fatalf("expected needs_recovery to be true")
	}
}

func TestHandleVectorizationRecoveryStatusUsesSharedStorageHealthPolicyWhenRuntimePolicyUnavailable(t *testing.T) {
	resetDefaultStorageHealthPolicyForTest(t)

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = nil
	defer func() { activeRuntimeVectorizationFailurePolicy = previous }()

	policy := sop.DefaultStorageHealthPolicy()
	policy.RecordFailure("write", "/tmp/shared-store", syscall.EROFS)

	req := httptest.NewRequest(http.MethodGet, "/api/daemon/vectorization/recovery", nil)
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload struct {
		HasRecovery         bool                         `json:"has_recovery"`
		NeedsRecovery       bool                         `json:"needs_recovery"`
		StoragePathRecovery runtimeDataPathRecoveryState `json:"storage_path_recovery"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode shared storage-health payload: %v", err)
	}
	if !payload.HasRecovery {
		t.Fatalf("expected shared storage-health policy to mark recovery as present")
	}
	if !payload.NeedsRecovery {
		t.Fatalf("expected shared storage-health policy to require recovery")
	}
	if payload.StoragePathRecovery.Status != "read_only" {
		t.Fatalf("expected shared storage-health policy status to be read_only, got %q", payload.StoragePathRecovery.Status)
	}
	if payload.StoragePathRecovery.Path != "/tmp/shared-store" {
		t.Fatalf("expected shared storage-health policy path to be preserved, got %q", payload.StoragePathRecovery.Path)
	}
}

func TestHandleVectorizationRecoveryStatusIncludesAllSharedStoragePathStates(t *testing.T) {
	resetDefaultStorageHealthPolicyForTest(t)

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = nil
	defer func() { activeRuntimeVectorizationFailurePolicy = previous }()

	policy := sop.DefaultStorageHealthPolicy()
	policy.RecordFailure("write", "/tmp/first-store", os.ErrNotExist)
	policy.RecordFailure("write", "/tmp/second-store", syscall.EROFS)

	req := httptest.NewRequest(http.MethodGet, "/api/daemon/vectorization/recovery", nil)
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload struct {
		StoragePathStates []runtimeDataPathRecoveryState `json:"storage_path_states"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode shared storage-path state payload: %v", err)
	}
	if len(payload.StoragePathStates) != 2 {
		t.Fatalf("expected 2 shared storage-path states, got %d", len(payload.StoragePathStates))
	}
}

func TestHandleVectorizationRecoveryStatusAcceptsRecoveryAction(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-5", os.ErrNotExist)
	}

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	t.Cleanup(func() {
		activeRuntimeVectorizationFailurePolicy = previous
	})

	body := strings.NewReader(`{"action":"ignore","kb_name":"kb","target_type":"category","target_id":"cat-5"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/vectorization/recovery", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload struct {
		Latest runtimeVectorizationRecoveryState `json:"latest"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode recovery action payload: %v", err)
	}
	if payload.Latest.Status != "ignored" {
		t.Fatalf("expected ignored status after action, got %q", payload.Latest.Status)
	}
}

func TestHandleVectorizationRecoveryStatusScheduleRetryActionMarksScheduledState(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-schedule", os.ErrNotExist)
	}

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	t.Cleanup(func() {
		activeRuntimeVectorizationFailurePolicy = previous
	})

	body := strings.NewReader(`{"action":"schedule_retry","kb_name":"kb","target_type":"category","target_id":"cat-schedule"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/vectorization/recovery", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload struct {
		Latest  runtimeVectorizationRecoveryState `json:"latest"`
		Updated bool                              `json:"updated"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode schedule-retry payload: %v", err)
	}
	if !payload.Updated {
		t.Fatal("expected updated flag to be true")
	}
	if payload.Latest.Status != "scheduled" {
		t.Fatalf("expected scheduled status after schedule_retry action, got %q", payload.Latest.Status)
	}
}

func TestHandleVectorizationRecoveryStatusGiveUpActionMarksAbandonedState(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-giveup", os.ErrNotExist)
	}

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	t.Cleanup(func() {
		activeRuntimeVectorizationFailurePolicy = previous
	})

	body := strings.NewReader(`{"action":"give_up","kb_name":"kb","target_type":"category","target_id":"cat-giveup"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/vectorization/recovery", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload struct {
		Latest  runtimeVectorizationRecoveryState `json:"latest"`
		Updated bool                              `json:"updated"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode give-up payload: %v", err)
	}
	if !payload.Updated {
		t.Fatal("expected updated flag to be true")
	}
	if payload.Latest.Status != "abandoned" {
		t.Fatalf("expected abandoned status after give_up action, got %q", payload.Latest.Status)
	}
}

func TestHandleVectorizationRecoveryStatusRecoverActionInvokesRecoveryHandler(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-6", os.ErrNotExist)
	}

	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	previousHandler := vectorizationRecoveryExecutor
	called := false
	vectorizationRecoveryExecutor = func(ctx context.Context, kbName, targetType, targetID string) error {
		called = true
		if kbName != "kb" || targetType != "category" || targetID != "cat-6" {
			t.Fatalf("unexpected recovery target: kb=%q targetType=%q targetID=%q", kbName, targetType, targetID)
		}
		return nil
	}
	defer func() { vectorizationRecoveryExecutor = previousHandler }()

	body := strings.NewReader(`{"action":"recover","kb_name":"kb","target_type":"category","target_id":"cat-6"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/vectorization/recovery", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !called {
		t.Fatal("expected recover action to invoke recovery handler")
	}

	var payload struct {
		Latest  runtimeVectorizationRecoveryState `json:"latest"`
		Updated bool                              `json:"updated"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode recovery action payload: %v", err)
	}
	if !payload.Updated {
		t.Fatal("expected updated flag to be true")
	}
	if payload.Latest.Status != "recovered" {
		t.Fatalf("expected recovered status after recovery action, got %q", payload.Latest.Status)
	}
}

func TestHandleVectorizationRecoveryStatusRecoverAllActionRebuildsAllDegradedTargets(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-7", os.ErrNotExist)
	}
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-8", os.ErrNotExist)
	}

	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	previousHandler := vectorizationRecoveryExecutor
	var attempted []string
	vectorizationRecoveryExecutor = func(ctx context.Context, kbName, targetType, targetID string) error {
		attempted = append(attempted, kbName+":"+targetType+":"+targetID)
		return nil
	}
	defer func() { vectorizationRecoveryExecutor = previousHandler }()

	body := strings.NewReader(`{"action":"recover_all"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/vectorization/recovery", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(attempted) != 2 {
		t.Fatalf("expected recover_all to attempt 2 targets, got %d", len(attempted))
	}

	var payload struct {
		RecoveredCount int  `json:"recovered_count"`
		Updated        bool `json:"updated"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode recovery-all payload: %v", err)
	}
	if payload.RecoveredCount != 2 {
		t.Fatalf("expected recovered_count to be 2, got %d", payload.RecoveredCount)
	}
	if !payload.Updated {
		t.Fatal("expected updated flag to be true")
	}
}

func TestTrySimpleShellCommandFallbackSkipsAgentCommandsWithoutSlash(t *testing.T) {
	t.Skip("Bare command execution removed to avoid ambiguity with natural language prompts. Commands must use slash prefixes like /c, /cmd")
	svc := &localRAGService{shellSessions: newPTYShellSessionManager()}
	for _, query := range []string{"list_tools", "list_databases", "last-tool"} {
		got, handled, err := svc.trySimpleShellCommandFallback(query, "session")
		if err != nil {
			t.Fatalf("trySimpleShellCommandFallback(%q) returned error: %v", query, err)
		}
		if handled {
			t.Fatalf("trySimpleShellCommandFallback(%q) should bypass shell fallback for agent-style commands", query)
		}
		if got != "" {
			t.Fatalf("trySimpleShellCommandFallback(%q) returned unexpected output %q", query, got)
		}
	}
}

func TestTrySimpleShellCommandFallbackSkipsNaturalLanguageQuestions(t *testing.T) {
	t.Skip("Bare command execution removed to avoid ambiguity with natural language prompts. Commands must use slash prefixes like /c, /cmd")
	svc := &localRAGService{shellSessions: newPTYShellSessionManager()}
	query := "show me list of changed files in this repo in git"

	got, handled, err := svc.trySimpleShellCommandFallback(query, "session")
	if err != nil {
		t.Fatalf("trySimpleShellCommandFallback(%q) returned error: %v", query, err)
	}
	if handled {
		t.Fatalf("trySimpleShellCommandFallback(%q) should bypass shell fallback for natural-language prompts", query)
	}
	if got != "" {
		t.Fatalf("trySimpleShellCommandFallback(%q) returned unexpected output %q", query, got)
	}
}

func TestHandleAskRoutesPlainListToolsToAgentWithSlashPrefix(t *testing.T) {
	agent := &recordingQueryAgent{}
	svc := &localRAGService{service: agent, shellSessions: newPTYShellSessionManager()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"list_tools"}`))
	rr := httptest.NewRecorder()

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(agent.queries) != 1 {
		t.Fatalf("expected one agent query, got %d", len(agent.queries))
	}
	if agent.queries[0] != "/list_tools" {
		t.Fatalf("expected agent query to be /list_tools, got %q", agent.queries[0])
	}
}

func TestHandleAskInjectsBackendExecutorIntoContext(t *testing.T) {
	agent := &executorAwareAgent{}
	svc := &localRAGService{service: &stubSharedAgent{name: "shared"}, shellSessions: newPTYShellSessionManager()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"list_tools"}`))
	rr := httptest.NewRecorder()

	sopbackend.SetLoadedAgents(map[string]ai.Agent[map[string]any]{"copilot": agent})
	defer sopbackend.SetLoadedAgents(nil)

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !agent.gotExecutor {
		t.Fatal("expected handleAsk to inject the shared backend DefaultToolExecutor into the shared agent context")
	}
}

func TestHandleAskUsesSharedBackendHandlerForSlashCommands(t *testing.T) {
	localAgent := &recordingQueryAgent{}
	sharedAgent := &recordingQueryAgent{}
	svc := &localRAGService{service: localAgent, shellSessions: newPTYShellSessionManager()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"list_tools"}`))
	rr := httptest.NewRecorder()

	sopbackend.SetLoadedAgents(map[string]ai.Agent[map[string]any]{"copilot": sharedAgent})
	defer sopbackend.SetLoadedAgents(nil)

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(localAgent.queries) != 0 {
		t.Fatalf("expected local service agent to be bypassed for slash commands, got %v", localAgent.queries)
	}
	if len(sharedAgent.queries) != 1 || sharedAgent.queries[0] != "/list_tools" {
		t.Fatalf("expected shared backend handler to receive /list_tools, got %v", sharedAgent.queries)
	}
}

func TestHandleAskUsesSharedBackendHandlerForNaturalLanguageQueries(t *testing.T) {
	localAgent := &recordingQueryAgent{}
	sharedAgent := &recordingQueryAgent{}
	svc := &localRAGService{service: localAgent, shellSessions: newPTYShellSessionManager()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"show me list of changed files in this repo in git"}`))
	rr := httptest.NewRecorder()

	sopbackend.SetLoadedAgents(map[string]ai.Agent[map[string]any]{"copilot": sharedAgent})
	defer sopbackend.SetLoadedAgents(nil)

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(localAgent.queries) != 0 {
		t.Fatalf("expected local service agent to be bypassed for natural-language queries, got %v", localAgent.queries)
	}
	if len(sharedAgent.queries) != 1 {
		t.Fatalf("expected shared backend handler to receive the natural-language query, got %v", sharedAgent.queries)
	}
	if sharedAgent.queries[0] != "show me list of changed files in this repo in git" {
		t.Fatalf("expected shared backend handler to receive the original query, got %q", sharedAgent.queries[0])
	}
}

func TestHandleAskNormalizesKelindarProviderUsingSharedBackendLogic(t *testing.T) {
	agent := &recordingAskAgent{}
	svc := &localRAGService{service: agent, shellSessions: newPTYShellSessionManager()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"hello","llm_provider":"kelindar","llm_model":"nomic-embed-text-v1.5-q8_0"}`))
	rr := httptest.NewRecorder()

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("expected one agent call, got %d", len(agent.calls))
	}
	if agent.calls[0].cfg == nil {
		t.Fatal("expected agent config to be populated")
	}
	value, ok := agent.calls[0].cfg.Get("provider_details")
	if !ok {
		t.Fatal("expected provider_details in agent config")
	}
	providerDetails, ok := value.(*sharedagent.ProviderDetails)
	if !ok {
		t.Fatalf("expected provider_details of type *sharedagent.ProviderDetails, got %T", value)
	}
	if providerDetails.Provider != "kelindar" {
		t.Fatalf("provider = %q, want %q", providerDetails.Provider, "kelindar")
	}
	if providerDetails.Model != "nomic-embed-text-v1.5-q8_0" {
		t.Fatalf("model = %q, want %q", providerDetails.Model, "nomic-embed-text-v1.5-q8_0")
	}
}

func TestHandleAskPassesProviderDetailsToAgentConfig(t *testing.T) {
	agent := &recordingAskAgent{}
	svc := &localRAGService{service: agent, shellSessions: newPTYShellSessionManager()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"hello","llm_provider":"gemini","llm_api_key":"secret-key","llm_model":"gemini-2.5-pro","llm_url":"https://example.test"}`))
	rr := httptest.NewRecorder()

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("expected one agent call, got %d", len(agent.calls))
	}
	if agent.calls[0].cfg == nil {
		t.Fatal("expected agent config to be populated")
	}
	value, ok := agent.calls[0].cfg.Get("provider_details")
	if !ok {
		t.Fatal("expected provider_details in agent config")
	}
	providerDetails, ok := value.(*sharedagent.ProviderDetails)
	if !ok {
		t.Fatalf("expected provider_details of type *sharedagent.ProviderDetails, got %T", value)
	}
	if providerDetails.Provider != "gemini" {
		t.Fatalf("provider = %q, want %q", providerDetails.Provider, "gemini")
	}
	if providerDetails.APIKey != "secret-key" {
		t.Fatalf("api key = %q, want %q", providerDetails.APIKey, "secret-key")
	}
	if providerDetails.Model != "gemini-2.5-pro" {
		t.Fatalf("model = %q, want %q", providerDetails.Model, "gemini-2.5-pro")
	}
	if providerDetails.BaseURL != "https://example.test" {
		t.Fatalf("base URL = %q, want %q", providerDetails.BaseURL, "https://example.test")
	}
}

func TestAutoProvisionSharedCopilotServiceUsesBackendLoadedAgents(t *testing.T) {
	oldAgents := sopbackend.GetLoadedAgents()
	defer sopbackend.SetLoadedAgents(oldAgents)

	backend := sopbackend.NewBackend(&sopbackend.Config{}, &sopbackend.ModelCatalog{})
	backend.LoadedAgents = map[string]ai.Agent[map[string]any]{"copilot": &stubSharedAgent{name: "shared"}}
	if err := backend.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	svc, err := autoProvisionSharedCopilotService(context.Background())
	if err != nil {
		t.Fatalf("autoProvisionSharedCopilotService() error = %v", err)
	}
	if _, ok := svc.(*stubSharedAgent); !ok {
		t.Fatalf("autoProvisionSharedCopilotService() = %T, want *stubSharedAgent", svc)
	}
}

func TestExecuteHandlerRunsShellCommand(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(`{"command":"printf 'hello from daemon'"}`))
	rr := httptest.NewRecorder()

	executeHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("executeHandler() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp ExecuteResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0, stderr=%q", resp.ExitCode, resp.Stderr)
	}
	if !strings.Contains(resp.Stdout, "hello from daemon") {
		t.Fatalf("stdout = %q, want it to contain command output", resp.Stdout)
	}
}

func TestExecuteHandlerHandlesCancellation(t *testing.T) {
	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Start a long-running command
	reqBody := `{"command":"sleep 10 && echo done"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(reqBody))
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	// Cancel the context after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	executeHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("executeHandler() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp ExecuteResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !resp.Cancelled {
		t.Fatalf("expected command to be marked as cancelled, got cancelled=%v", resp.Cancelled)
	}

	if resp.ExitCode != -1 {
		t.Fatalf("expected exit code -1 for cancelled command, got %d", resp.ExitCode)
	}

	if !strings.Contains(resp.Error, "cancelled") {
		t.Fatalf("expected error to mention cancellation, got %q", resp.Error)
	}
}

func TestExecuteHandlerShowsPartialOutputOnError(t *testing.T) {
	// Command that produces output before failing
	reqBody := `{"command":"echo 'partial output' && exit 1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(reqBody))
	rr := httptest.NewRecorder()

	executeHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("executeHandler() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp ExecuteResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ExitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", resp.ExitCode)
	}

	// Verify partial output is captured
	if !strings.Contains(resp.Stdout, "partial output") {
		t.Fatalf("expected stdout to contain partial output, got %q", resp.Stdout)
	}

	// Should not be marked as cancelled
	if resp.Cancelled {
		t.Fatalf("command failed naturally, should not be marked as cancelled")
	}
}

func TestLocalAgentExecuteToolPassesWorkingDirAndTimeout(t *testing.T) {
	sharedagent.SetLocalCommandExecutor(func(ctx context.Context, req sharedagent.LocalCommandRequest) (string, error) {
		if req.Command != "pwd" {
			t.Fatalf("command = %q, want %q", req.Command, "pwd")
		}
		if req.WorkingDir != "/tmp" {
			t.Fatalf("working dir = %q, want %q", req.WorkingDir, "/tmp")
		}
		if req.TimeoutSeconds != 45 {
			t.Fatalf("timeout = %d, want 45", req.TimeoutSeconds)
		}
		return `{"stdout":"/tmp","stderr":"","exit_code":0}`, nil
	})
	sharedagent.SetLocalDesktopAutomationEnabled(true)
	defer sharedagent.SetLocalDesktopAutomationEnabled(false)
	defer sharedagent.SetLocalCommandExecutor(nil)

	ag := sharedagent.NewCopilotAgent(sharedagent.Config{}, nil, nil)
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{CurrentDB: "system"})

	out, err := ag.Execute(ctx, "execute_local_command", map[string]any{"command": "pwd", "reasoning": "inspect repo state", "working_dir": "/tmp", "timeout_seconds": 45})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out, "/tmp") {
		t.Fatalf("Execute() output = %q, want it to contain repo working directory output", out)
	}
}

func TestRunLocalCommandUsesSessionStateForFollowUpCommands(t *testing.T) {
	first, err := runLocalCommand(context.Background(), sharedagent.LocalCommandRequest{Command: "cd /tmp", SessionID: "repo-session", TimeoutSeconds: 10})
	if err != nil {
		t.Fatalf("first runLocalCommand() error = %v", err)
	}
	var firstResp ExecuteResponse
	if err := json.Unmarshal([]byte(first), &firstResp); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if firstResp.Error != "" {
		t.Fatalf("first response error = %q", firstResp.Error)
	}

	second, err := runLocalCommand(context.Background(), sharedagent.LocalCommandRequest{Command: "pwd", SessionID: "repo-session", TimeoutSeconds: 10})
	if err != nil {
		t.Fatalf("second runLocalCommand() error = %v", err)
	}
	var secondResp ExecuteResponse
	if err := json.Unmarshal([]byte(second), &secondResp); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if !strings.Contains(secondResp.Stdout, "/tmp") {
		t.Fatalf("expected second command output to reflect session cwd, got %q", secondResp.Stdout)
	}
}

func TestRunLocalCommandExecutesRunnableScriptFromWorkingDir(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "hello.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho hello-from-script\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:        "hello.sh",
		Reasoning:      "run a local script",
		WorkingDir:     tmpDir,
		TimeoutSeconds: 10,
		SessionID:      "",
	})
	if err != nil {
		t.Fatalf("runLocalCommandWithWorkingDir() error = %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr=%q", resp.ExitCode, resp.Stderr)
	}
	if !strings.Contains(resp.Stdout, "hello-from-script") {
		t.Fatalf("stdout = %q, want runnable script output", resp.Stdout)
	}
}

func TestRunLocalCommandWithStdoutStreaming(t *testing.T) {
	var streamedLines []string
	var mu sync.Mutex

	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:        "echo line1 && echo line2 && echo line3",
		Reasoning:      "test stdout streaming",
		TimeoutSeconds: 10,
		StreamStdout: func(line string) {
			mu.Lock()
			streamedLines = append(streamedLines, line)
			mu.Unlock()
		},
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr=%q", resp.ExitCode, resp.Stderr)
	}

	// Verify streaming occurred
	mu.Lock()
	defer mu.Unlock()
	if len(streamedLines) == 0 {
		t.Fatalf("expected streaming to occur, but got 0 chunks")
	}

	// Verify all content was streamed
	allStreamed := strings.Join(streamedLines, "")
	if !strings.Contains(allStreamed, "line1") || !strings.Contains(allStreamed, "line2") || !strings.Contains(allStreamed, "line3") {
		t.Fatalf("streamed content = %q, missing expected lines", allStreamed)
	}

	// Verify final output contains all lines
	if !strings.Contains(resp.Stdout, "line1") || !strings.Contains(resp.Stdout, "line2") || !strings.Contains(resp.Stdout, "line3") {
		t.Fatalf("stdout = %q, missing expected lines", resp.Stdout)
	}
}

func TestRunLocalCommandWithStderrStreaming(t *testing.T) {
	var stderrLines []string
	var mu sync.Mutex

	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:        "echo error1 >&2 && echo error2 >&2",
		Reasoning:      "test stderr streaming",
		TimeoutSeconds: 10,
		StreamStderr: func(line string) {
			mu.Lock()
			stderrLines = append(stderrLines, line)
			mu.Unlock()
		},
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Verify stderr was captured
	mu.Lock()
	defer mu.Unlock()
	if len(stderrLines) == 0 {
		t.Fatalf("expected stderr streaming to occur, but got 0 chunks")
	}

	// Verify all stderr content was streamed
	allStreamed := strings.Join(stderrLines, "")
	if !strings.Contains(allStreamed, "error1") || !strings.Contains(allStreamed, "error2") {
		t.Fatalf("streamed stderr = %q, missing expected errors", allStreamed)
	}

	if !strings.Contains(resp.Stderr, "error1") || !strings.Contains(resp.Stderr, "error2") {
		t.Fatalf("stderr = %q, missing expected errors", resp.Stderr)
	}
}

func TestRunLocalCommandWithStdinInput(t *testing.T) {
	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:        "cat",
		Reasoning:      "test stdin support",
		Stdin:          "hello from stdin\n",
		TimeoutSeconds: 5,
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr=%q", resp.ExitCode, resp.Stderr)
	}

	if !strings.Contains(resp.Stdout, "hello from stdin") {
		t.Fatalf("stdout = %q, expected stdin to be echoed", resp.Stdout)
	}
}

func TestRunLocalCommandWithOnCompleteCallback(t *testing.T) {
	var completedStdout, completedStderr string
	var completedExitCode int

	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:        "echo success && echo warning >&2",
		Reasoning:      "test completion callback",
		TimeoutSeconds: 10,
		OnComplete: func(stdout, stderr string, exitCode int) {
			completedStdout = stdout
			completedStderr = stderr
			completedExitCode = exitCode
		},
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Verify callback was invoked
	if !strings.Contains(completedStdout, "success") {
		t.Fatalf("OnComplete stdout = %q, expected success message", completedStdout)
	}

	if !strings.Contains(completedStderr, "warning") {
		t.Fatalf("OnComplete stderr = %q, expected warning message", completedStderr)
	}

	if completedExitCode != 0 {
		t.Fatalf("OnComplete exitCode = %d, expected 0", completedExitCode)
	}
}

// Phase 2 Tests: Process Control
func TestRunLocalCommandWithProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Process groups not supported on Windows")
	}

	var capturedPID int

	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:      "echo test",
		ProcessGroup: true,
		OnProcessStart: func(pid int) {
			capturedPID = pid
		},
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if capturedPID == 0 {
		t.Fatalf("OnProcessStart not called or PID not captured")
	}

	if resp.PID != capturedPID {
		t.Fatalf("response PID = %d, callback PID = %d", resp.PID, capturedPID)
	}
}

func TestRunLocalCommandWithPIDCapture(t *testing.T) {
	var capturedPID int

	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command: "echo test",
		OnProcessStart: func(pid int) {
			capturedPID = pid
		},
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if capturedPID == 0 {
		t.Fatalf("OnProcessStart not called")
	}

	if resp.PID != capturedPID {
		t.Fatalf("response PID = %d, callback PID = %d", resp.PID, capturedPID)
	}
}

// Phase 3 Tests: Background Jobs
func TestRunLocalCommandInBackground(t *testing.T) {
	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:    "sleep 1 && echo background-test",
		Background: true,
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !resp.Background {
		t.Fatalf("Background flag not set in response")
	}

	if resp.JobID == "" {
		t.Fatalf("JobID not returned")
	}

	// Wait a bit for command to complete
	time.Sleep(2 * time.Second)

	// Check job status
	job, ok := getBackgroundJob(resp.JobID)
	if !ok {
		t.Fatalf("Job not found: %s", resp.JobID)
	}

	job.mu.Lock()
	defer job.mu.Unlock()

	if !job.done {
		t.Fatalf("Job should be done by now")
	}

	if job.exitCode == nil || *job.exitCode != 0 {
		t.Fatalf("Job exit code: %v, expected 0", job.exitCode)
	}

	if !strings.Contains(job.stdout.String(), "background-test") {
		t.Fatalf("Job stdout = %q, expected 'background-test'", job.stdout.String())
	}
}

func TestBackgroundJobWithCustomID(t *testing.T) {
	customID := "my-custom-job-123"

	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:    "echo custom-job",
		Background: true,
		JobID:      customID,
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.JobID != customID {
		t.Fatalf("JobID = %s, expected %s", resp.JobID, customID)
	}

	// Wait for completion
	time.Sleep(500 * time.Millisecond)

	job, ok := getBackgroundJob(customID)
	if !ok {
		t.Fatalf("Job not found: %s", customID)
	}

	job.mu.Lock()
	defer job.mu.Unlock()

	if !strings.Contains(job.stdout.String(), "custom-job") {
		t.Fatalf("Job output = %q, expected 'custom-job'", job.stdout.String())
	}
}

func TestKillBackgroundJob(t *testing.T) {
	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:    "sleep 30",
		Background: true,
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Wait a bit to ensure job started
	time.Sleep(200 * time.Millisecond)

	// Kill the job
	if err := killBackgroundJob(resp.JobID); err != nil {
		t.Fatalf("failed to kill job: %v", err)
	}

	// Wait for termination
	time.Sleep(500 * time.Millisecond)

	job, ok := getBackgroundJob(resp.JobID)
	if !ok {
		t.Fatalf("Job not found: %s", resp.JobID)
	}

	job.mu.Lock()
	defer job.mu.Unlock()

	if !job.done {
		t.Fatalf("Job should be terminated")
	}

	// Exit code should be non-zero (killed)
	if job.exitCode != nil && *job.exitCode == 0 {
		t.Fatalf("Job exit code = 0, expected non-zero (killed)")
	}
}

func TestBackgroundJobWithStreaming(t *testing.T) {
	var stdoutLines []string
	var mu sync.Mutex

	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:    "echo line1 && echo line2 && echo line3",
		Background: true,
		StreamStdout: func(line string) {
			mu.Lock()
			stdoutLines = append(stdoutLines, line)
			mu.Unlock()
		},
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Wait for completion
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(stdoutLines) == 0 {
		t.Fatalf("Expected streaming to occur, but got 0 chunks")
	}

	// Verify all content was streamed
	allStreamed := strings.Join(stdoutLines, "")
	if !strings.Contains(allStreamed, "line1") || !strings.Contains(allStreamed, "line2") || !strings.Contains(allStreamed, "line3") {
		t.Fatalf("streamed content = %q, missing expected lines", allStreamed)
	}
}

func TestRetryPendingVectorizationRecoveryAttemptsPersistedDegradedTargets(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-9", os.ErrNotExist)
	}

	resetDefaultStorageHealthPolicyForTest(t)

	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	previousHandler := vectorizationRecoveryExecutor
	called := false
	vectorizationRecoveryExecutor = func(ctx context.Context, kbName, targetType, targetID string) error {
		called = true
		if kbName != "kb" || targetType != "category" || targetID != "cat-9" {
			t.Fatalf("unexpected recovery target: kb=%q targetType=%q targetID=%q", kbName, targetType, targetID)
		}
		return nil
	}
	defer func() { vectorizationRecoveryExecutor = previousHandler }()

	count := retryPendingVectorizationRecovery(context.Background(), true)
	if count != 1 {
		t.Fatalf("expected one pending recovery to be retried, got %d", count)
	}
	if !called {
		t.Fatal("expected pending recovery to invoke the recovery executor")
	}

	state := activeRuntimeVectorizationFailurePolicy.RecoveryState()
	if state.Status != "recovered" {
		t.Fatalf("expected recovered status after auto-retry, got %q", state.Status)
	}
}

func TestHandleUIHandoffIncludesVectorizationRecoveryState(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-6", os.ErrNotExist)
	}

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	t.Cleanup(func() {
		activeRuntimeVectorizationFailurePolicy = previous
	})

	req := httptest.NewRequest(http.MethodPost, "/api/handoff", strings.NewReader(`{"query":"hello","hq":{"base_url":"http://example.test"}}`))
	rr := httptest.NewRecorder()
	handleUIHandoff(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleUIHandoff() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp UIHandoffResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode handoff response: %v", err)
	}
	if resp.Recovery.TargetID != "cat-6" {
		t.Fatalf("expected handoff recovery target id to be cat-6, got %q", resp.Recovery.TargetID)
	}
	if resp.Recovery.Prompt == "" {
		t.Fatalf("expected handoff recovery prompt to be present")
	}
}

func TestHandleUIHandoffIncludesRecoveryPromptInWelcomeContext(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-7", os.ErrNotExist)
	}

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	t.Cleanup(func() {
		activeRuntimeVectorizationFailurePolicy = previous
	})

	req := httptest.NewRequest(http.MethodPost, "/api/handoff", strings.NewReader(`{"query":"hello","hq":{"base_url":"http://example.test"}}`))
	rr := httptest.NewRecorder()
	handleUIHandoff(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleUIHandoff() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp UIHandoffResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode handoff response: %v", err)
	}
	if !strings.Contains(resp.WelcomeContext, "Vectorization") {
		t.Fatalf("expected welcome context to mention vectorization recovery, got %q", resp.WelcomeContext)
	}
}

func TestHandleUIHandoffReturnsWelcomeContextAndStoresSessionInfo(t *testing.T) {
	ctxKey := "handoff-test"
	daemonSessionStore.mu.Lock()
	delete(daemonSessionStore.data, ctxKey)
	daemonSessionStore.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/handoff", strings.NewReader(`{"query":"ping","auth":{"access_token":"token-123","refresh_token":"refresh-123"},"hq":{"base_url":"https://hq.example","user_id":"user-1","session_id":"handoff-test"}}`))
	rr := httptest.NewRecorder()

	handleUIHandoff(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleUIHandoff() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp UIHandoffResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.WelcomeContext == "" {
		t.Fatalf("expected welcome context in handoff response, got %+v", resp)
	}
	if !strings.Contains(strings.ToLower(resp.WelcomeContext), "local") {
		t.Fatalf("welcome context should describe the local RAG experience, got %q", resp.WelcomeContext)
	}
	if !strings.Contains(resp.WelcomeContext, "Shell-style commands") || !strings.Contains(resp.WelcomeContext, "ls") || !strings.Contains(resp.WelcomeContext, "/list_tools") {
		t.Fatalf("welcome context should include shell-command guidance, got %q", resp.WelcomeContext)
	}

	daemonSessionStore.mu.Lock()
	ctx, ok := daemonSessionStore.data[ctxKey]
	daemonSessionStore.mu.Unlock()
	if !ok {
		t.Fatalf("expected handoff context to be stored for %q", ctxKey)
	}
	if ctx.AccessToken != "token-123" || ctx.RefreshToken != "refresh-123" {
		t.Fatalf("unexpected stored tokens: %+v", ctx)
	}
	if ctx.HQBaseURL != "https://hq.example" || ctx.HQUserID != "user-1" || ctx.HQSessionID != ctxKey {
		t.Fatalf("unexpected stored HQ context: %+v", ctx)
	}
}

func TestHandleUIHandoffAllowsMissingAuthTokensForLocalRuntime(t *testing.T) {
	ctxKey := "handoff-local-runtime"
	daemonSessionStore.mu.Lock()
	delete(daemonSessionStore.data, ctxKey)
	daemonSessionStore.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/handoff", strings.NewReader(`{"query":"ping","auth":{"access_token":"","refresh_token":""},"hq":{"base_url":"https://hq.example","user_id":"user-2","session_id":"handoff-local-runtime"}}`))
	rr := httptest.NewRecorder()

	handleUIHandoff(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleUIHandoff() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp UIHandoffResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Accepted {
		t.Fatalf("expected handoff to be accepted without auth tokens, got %+v", resp)
	}
	if resp.Error != "" {
		t.Fatalf("expected no error, got %q", resp.Error)
	}
}

type recordingAskAgent struct {
	response string
	calls    []struct {
		ctx context.Context
		cfg *ai.ConfigMap
	}
}

func (a *recordingAskAgent) Open(ctx context.Context) error  { return nil }
func (a *recordingAskAgent) Close(ctx context.Context) error { return nil }
func (a *recordingAskAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}
func (a *recordingAskAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	a.calls = append(a.calls, struct {
		ctx context.Context
		cfg *ai.ConfigMap
	}{ctx: ctx, cfg: cfg})
	if a.response != "" {
		return a.response, nil
	}
	return "ok", nil
}

func TestLocalRAGAskUsesShellFallbackForSimpleCommands(t *testing.T) {
	t.Skip("Bare command execution removed - commands must use slash prefixes like /c, /cmd")
	agent := &recordingAskAgent{response: "⚠️ **AI Copilot Disabled**: No valid API Key found."}
	svc := &localRAGService{service: agent, shellSessions: newPTYShellSessionManager()}

	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"echo hello"}`))
	rr := httptest.NewRecorder()

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp AskResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("unexpected error response: %q", resp.Error)
	}
	if !strings.Contains(resp.Answer, "hello") {
		t.Fatalf("expected shell fallback output in answer, got %q", resp.Answer)
	}
}

func TestShellSessionManagerPersistsDirectoryAcrossCommands(t *testing.T) {
	mgr := newShellSessionManager()

	if _, _, err := mgr.Run("shell-pty", "cd /tmp", 2*time.Second); err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	out, stillRunning, err := mgr.Run("shell-pty", "pwd", 2*time.Second)
	if err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	if stillRunning {
		t.Fatalf("expected pwd to finish promptly, got still-running output")
	}
	if !strings.Contains(out, "/tmp") {
		t.Fatalf("expected pwd output to reflect /tmp, got %q", out)
	}
}

func TestShellSessionManagerSupportsLongRunningProcessAndStatus(t *testing.T) {
	mgr := newShellSessionManager()

	out, stillRunning, err := mgr.Run("long-run", "sh -c 'printf start; sleep 1; printf end'", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if !stillRunning {
		t.Fatalf("expected first run to stay running beyond the timeout, got output %q", out)
	}
	if !strings.Contains(out, "start") {
		t.Fatalf("expected partial output from long-running process, got %q", out)
	}

	status, stillRunning, err := mgr.Run("long-run", "status", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("status query failed: %v", err)
	}
	if !stillRunning {
		t.Fatalf("expected status query to report the process is still running, got %q", status)
	}
	if !strings.Contains(status, "start") {
		t.Fatalf("expected status output to include buffered output, got %q", status)
	}
}

func TestPTYShellSessionManagerSpawnsBackgroundSessions(t *testing.T) {
	mgr := newPTYShellSessionManager()
	defer func() {
		mgr.mu.Lock()
		for _, sess := range mgr.sessions {
			sess.Close()
		}
		mgr.mu.Unlock()
	}()

	// Spawn a background session
	bgSessionID, err := mgr.SpawnBackgroundSession("")
	if err != nil {
		t.Fatalf("SpawnBackgroundSession() failed: %v", err)
	}

	if bgSessionID == "" {
		t.Fatal("expected non-empty background session ID")
	}

	// Verify it's tracked as background
	mgr.mu.Lock()
	bgSession := mgr.sessions[bgSessionID]
	mgr.mu.Unlock()

	if bgSession == nil {
		t.Fatal("background session not found in manager")
	}

	if !bgSession.isBackground {
		t.Error("expected session to be marked as background")
	}

	// Can run commands in background session
	output, _, err := mgr.Run(bgSessionID, "echo 'background'", 5*time.Second)
	if err != nil {
		t.Fatalf("Run() in background session failed: %v", err)
	}

	if !strings.Contains(output, "background") {
		t.Errorf("expected output to contain 'background', got %q", output)
	}
}

func TestPTYShellSessionManagerCleansUpIdleSessions(t *testing.T) {
	mgr := newPTYShellSessionManager()
	defer mgr.StopCleanup()

	// Create a session and mark it as old
	sessionID := "test-session"
	_, _, err := mgr.Run(sessionID, "echo test", 5*time.Second)
	if err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	// Force session to appear idle
	mgr.mu.Lock()
	if sess, ok := mgr.sessions[sessionID]; ok {
		sess.lastUsed = time.Now().Add(-1 * time.Hour) // 1 hour ago
	}
	mgr.mu.Unlock()

	// Cleanup with 30 minute timeout should remove the session
	cleaned := mgr.cleanupIdleSessions(30 * time.Minute)
	if cleaned != 1 {
		t.Errorf("expected 1 session cleaned, got %d", cleaned)
	}

	// Verify session was removed
	mgr.mu.Lock()
	_, exists := mgr.sessions[sessionID]
	mgr.mu.Unlock()

	if exists {
		t.Error("expected session to be removed after cleanup")
	}
}

func TestPTYShellSessionHealthCheckDetectsStuckSession(t *testing.T) {
	mgr := newPTYShellSessionManager()
	defer func() {
		mgr.mu.Lock()
		for _, sess := range mgr.sessions {
			sess.Close()
		}
		mgr.mu.Unlock()
	}()

	sessionID := "test-health"

	// Create a session and run a valid command
	_, _, err := mgr.Run(sessionID, "echo test", 5*time.Second)
	if err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	// Session should be healthy
	mgr.mu.Lock()
	session := mgr.sessions[sessionID]
	mgr.mu.Unlock()

	if !session.IsHealthy() {
		t.Error("expected healthy session to report healthy")
	}

	// Close the session (simulates stuck/crashed session)
	session.Close()

	// Health check should now fail
	if session.IsHealthy() {
		t.Error("expected closed session to report unhealthy")
	}
}

func TestPTYShellSessionAutoRecoveryFromStuckSession(t *testing.T) {
	mgr := newPTYShellSessionManager()
	defer func() {
		mgr.mu.Lock()
		for _, sess := range mgr.sessions {
			sess.Close()
		}
		mgr.mu.Unlock()
	}()

	sessionID := "test-recovery"

	// Create a session
	_, _, err := mgr.Run(sessionID, "echo first", 5*time.Second)
	if err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	// Get the session and mark it as stuck
	mgr.mu.Lock()
	session := mgr.sessions[sessionID]
	session.stillRunning = true
	mgr.mu.Unlock()

	// Close the session to simulate unresponsive state
	session.Close()

	// Next command should trigger auto-recovery
	output, _, err := mgr.Run(sessionID, "echo recovered", 5*time.Second)
	if err != nil {
		t.Fatalf("Run() after recovery failed: %v", err)
	}

	if !strings.Contains(output, "recovered") {
		t.Errorf("expected recovered session to work, got output: %q", output)
	}

	// Verify new session was created
	mgr.mu.Lock()
	newSession := mgr.sessions[sessionID]
	mgr.mu.Unlock()

	if newSession == session {
		t.Error("expected new session to be created after recovery")
	}
}

func TestPTYShellSessionForceReset(t *testing.T) {
	mgr := newPTYShellSessionManager()
	defer func() {
		mgr.mu.Lock()
		for _, sess := range mgr.sessions {
			sess.Close()
		}
		mgr.mu.Unlock()
	}()

	sessionID := "test-force-reset"

	// Create a session
	_, _, err := mgr.Run(sessionID, "echo before", 5*time.Second)
	if err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	mgr.mu.Lock()
	oldSession := mgr.sessions[sessionID]
	mgr.mu.Unlock()

	// Force reset
	if err := mgr.ForceResetSession(sessionID); err != nil {
		t.Fatalf("ForceResetSession() failed: %v", err)
	}

	// Verify new session exists
	mgr.mu.Lock()
	newSession := mgr.sessions[sessionID]
	mgr.mu.Unlock()

	if newSession == nil {
		t.Fatal("expected session to exist after reset")
	}

	if newSession == oldSession {
		t.Error("expected new session after force reset")
	}

	// New session should work
	output, _, err := mgr.Run(sessionID, "echo after", 5*time.Second)
	if err != nil {
		t.Fatalf("Run() after force reset failed: %v", err)
	}

	if !strings.Contains(output, "after") {
		t.Errorf("expected reset session to work, got: %q", output)
	}
}

func TestLocalRAGAskPersistsShellSessionDirectoryAcrossRequests(t *testing.T) {
	t.Skip("Bare command execution removed - commands must use slash prefixes like /c, /cmd")
	svc := &localRAGService{service: &recordingAskAgent{}, shellSessions: newPTYShellSessionManager()}

	firstReq := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"cd /tmp"}`))
	firstReq.Header.Set("X-SOP-Session-ID", "shell-session")
	firstRR := httptest.NewRecorder()
	svc.handleAsk(firstRR, firstReq)
	if firstRR.Code != http.StatusOK {
		t.Fatalf("first handleAsk() status = %d, want %d", firstRR.Code, http.StatusOK)
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"pwd"}`))
	secondReq.Header.Set("X-SOP-Session-ID", "shell-session")
	secondRR := httptest.NewRecorder()
	svc.handleAsk(secondRR, secondReq)
	if secondRR.Code != http.StatusOK {
		t.Fatalf("second handleAsk() status = %d, want %d", secondRR.Code, http.StatusOK)
	}

	var secondResp AskResponse
	if err := json.NewDecoder(secondRR.Body).Decode(&secondResp); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if !strings.Contains(secondResp.Answer, "/tmp") {
		t.Fatalf("expected pwd output to reflect the updated directory, got %q", secondResp.Answer)
	}
}

func TestLocalRAGAskPropagatesHandoffContextIntoAgentRequest(t *testing.T) {
	agent := &recordingAskAgent{}
	svc := &localRAGService{service: agent}

	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"sop:","auth":{"access_token":"token-123","refresh_token":"refresh-123"},"hq":{"base_url":"https://hq.example","user_id":"user-42","session_id":"sess-42"}}`))
	rr := httptest.NewRecorder()

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("expected 1 ask call, got %d", len(agent.calls))
	}

	payload := ai.GetSessionPayload(agent.calls[0].ctx)
	if payload == nil {
		t.Fatal("expected session payload in ask context")
	}
	if payload.UserID != "user-42" || payload.SessionID != "sess-42" {
		t.Fatalf("unexpected session payload: %+v", payload)
	}
	if agent.calls[0].cfg == nil {
		t.Fatal("expected config map to be passed to agent ask")
	}
	if val, ok := agent.calls[0].cfg.Get("payload"); !ok {
		t.Fatal("expected payload in config map")
	} else if mappedPayload, ok := val.(*ai.SessionPayload); !ok || mappedPayload != payload {
		t.Fatalf("unexpected payload in config map: %v", val)
	}
}

func TestLocalRAGAskUsesHandoffProviderDetailsWhenAskBodyOmitsThem(t *testing.T) {
	agent := &recordingAskAgent{}
	svc := &localRAGService{service: agent}
	ctxKey := "handoff-provider-session"

	handoffReq := httptest.NewRequest(http.MethodPost, "/api/handoff", strings.NewReader(`{"query":"ping","auth":{"access_token":"token-123","refresh_token":"refresh-123"},"hq":{"base_url":"https://hq.example","user_id":"user-42","session_id":"handoff-provider-session"},"llm_provider":"anthropic","llm_api_key":"test-key","llm_model":"claude-3-5-sonnet-20241022","llm_url":"https://api.anthropic.com","thinking_level":"medium"}`))
	handoffRR := httptest.NewRecorder()
	handleUIHandoff(handoffRR, handoffReq)
	if handoffRR.Code != http.StatusOK {
		t.Fatalf("handoff status = %d, want %d", handoffRR.Code, http.StatusOK)
	}

	askReq := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"hello","hq":{"base_url":"https://hq.example","user_id":"user-42","session_id":"handoff-provider-session"}}`))
	askRR := httptest.NewRecorder()
	svc.handleAsk(askRR, askReq)
	if askRR.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", askRR.Code, http.StatusOK)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("expected 1 ask call, got %d", len(agent.calls))
	}

	if agent.calls[0].cfg == nil {
		t.Fatal("expected config map to be passed to agent ask")
	}
	val, ok := agent.calls[0].cfg.Get("provider_details")
	if !ok {
		t.Fatal("expected provider details to be propagated from handoff")
	}
	providerDetails, ok := val.(*sharedagent.ProviderDetails)
	if !ok {
		t.Fatalf("expected *sharedagent.ProviderDetails, got %T", val)
	}
	if providerDetails.Provider != "anthropic" || providerDetails.Model != "claude-3-5-sonnet-20241022" || providerDetails.APIKey != "test-key" || providerDetails.BaseURL != "https://api.anthropic.com" {
		t.Fatalf("unexpected provider details: %+v", providerDetails)
	}
	if ctxVal, ok := agent.calls[0].ctx.Value(ai.CtxKeyProvider).(string); !ok || ctxVal != "anthropic:claude-3-5-sonnet-20241022" {
		t.Fatalf("expected provider context to be set from handoff, got %v", ctxVal)
	}
	if ctxKey != "" {
		_ = ctxKey
	}
}

func TestWithRateLimitRejectsRequestsAfterLocalBudgetIsExhausted(t *testing.T) {
	reg := NewSessionRegistry(SubscriptionPolicy{MaxRequests: 1, Window: time.Minute})
	handler := withRateLimit(reg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.Header.Set("X-SOP-Session-ID", "session-a")
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusNoContent {
		t.Fatalf("first request status = %d, want %d", rr1.Code, http.StatusNoContent)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("X-SOP-Session-ID", "session-a")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want %d", rr2.Code, http.StatusTooManyRequests)
	}
}

package sopbackend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestResolvePreloadFilePathSearchesRepoStyleRoots(t *testing.T) {
	tmpRoot := t.TempDir()
	repoRoot := filepath.Join(tmpRoot, "lib", "sop")
	assetPath := filepath.Join(repoRoot, "ai", "sop_base_knowledge.json")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		t.Fatalf("mkdir asset dir: %v", err)
	}
	if err := os.WriteFile(assetPath, []byte(`{"items":[]}`), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	got := resolvePreloadFilePath("SOP", []string{repoRoot})
	if got != assetPath {
		t.Fatalf("resolvePreloadFilePath() = %q, want %q", got, assetPath)
	}
}

func TestResolvePreloadFilePathFindsCorePlaybookAsset(t *testing.T) {
	tmpRoot := t.TempDir()
	repoRoot := filepath.Join(tmpRoot, "lib", "sop")
	assetPath := filepath.Join(repoRoot, "ai", "sop_base_knowledge.json")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		t.Fatalf("mkdir asset dir: %v", err)
	}
	if err := os.WriteFile(assetPath, []byte(`{"items":[]}`), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	got := resolvePreloadFilePath("core_playbook", []string{repoRoot})
	if got != assetPath {
		t.Fatalf("resolvePreloadFilePath() = %q, want %q", got, assetPath)
	}
}

func TestRepositoryConfigAllowsSOPKBPreloadByDefault(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	candidate := filepath.Dir(filename)
	var foundConfig string
	for {
		configPath := filepath.Join(candidate, "config.json")
		if _, err := os.Stat(configPath); err == nil {
			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("read config: %v", err)
			}
			var cfg map[string]any
			if err := json.Unmarshal(data, &cfg); err != nil {
				t.Fatalf("decode config: %v", err)
			}
			if _, ok := cfg["disable_sop_kb_preload"].(bool); ok {
				foundConfig = configPath
				break
			}
		}

		parent := filepath.Dir(candidate)
		if parent == candidate {
			break
		}
		candidate = parent
	}

	if foundConfig == "" {
		// The current repository config omits this legacy opt-out field; omission
		// is the enabled-by-default contract.
		return
	}

	data, err := os.ReadFile(foundConfig)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	disabled, ok := cfg["disable_sop_kb_preload"].(bool)
	if !ok {
		t.Fatalf("expected disable_sop_kb_preload bool in %s", foundConfig)
	}
	if disabled {
		t.Fatalf("expected repository config to allow SOP KB preload, got disable_sop_kb_preload=true in %s", foundConfig)
	}
}

func TestShouldUseImportPathForPreload(t *testing.T) {
	tests := []struct {
		name        string
		templateID  string
		preloadPath string
		want        bool
	}{
		{name: "core playbook uses import path", templateID: "core_playbook", preloadPath: "ai/sop_base_knowledge.json", want: true},
		{name: "medical stays on ingest path", templateID: "medical", preloadPath: "medical.json", want: false},
		{name: "medical kb alias stays on ingest path", templateID: "Medical KB", preloadPath: "medical.json", want: false},
		{name: "custom template stays on ingest path", templateID: "custom", preloadPath: "custom.json", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldUseImportPathForPreload(tt.templateID, tt.preloadPath); got != tt.want {
				t.Fatalf("shouldUseImportPathForPreload(%q, %q) = %v, want %v", tt.templateID, tt.preloadPath, got, tt.want)
			}
		})
	}
}

func TestShouldForceLocalBuiltinEmbedderForInternalSpaces(t *testing.T) {
	if !shouldForceLocalBuiltinEmbedder("core_playbook", "ai/sop_base_knowledge.json") {
		t.Fatal("expected core_playbook to force the built-in local embedder")
	}
	if !shouldForceLocalBuiltinEmbedder("Medical", "medical.json") {
		t.Fatal("expected Medical KB to force the built-in local embedder")
	}
}

func TestShouldForceLocalBuiltinEmbedderSkipsCustomSpaces(t *testing.T) {
	if shouldForceLocalBuiltinEmbedder("Custom", "custom.json") {
		t.Fatal("expected custom spaces to keep the configured embedder")
	}
}

func TestBuildImportSpaceRequestFromPreloadUsesMultipartPayload(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "space_export.json")
	if err := os.WriteFile(filePath, []byte(`{"items":[]}`), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	req, err := buildImportSpaceRequest(context.Background(), filePath, "test-db", "SOP")
	if err != nil {
		t.Fatalf("buildImportSpaceRequest returned error: %v", err)
	}

	if req.Method != http.MethodPost {
		t.Fatalf("expected POST method, got %q", req.Method)
	}

	if err := req.ParseMultipartForm(32 << 20); err != nil {
		t.Fatalf("ParseMultipartForm returned error: %v", err)
	}

	if got := req.FormValue("database"); got != "test-db" {
		t.Fatalf("expected database form value %q, got %q", "test-db", got)
	}
	if got := req.FormValue("name"); got != "SOP" {
		t.Fatalf("expected name form value %q, got %q", "SOP", got)
	}

	fh, header, err := req.FormFile("file")
	if err != nil {
		t.Fatalf("FormFile returned error: %v", err)
	}
	defer fh.Close()
	if got := header.Filename; got != filepath.Base(filePath) {
		t.Fatalf("expected uploaded filename %q, got %q", filepath.Base(filePath), got)
	}
}

func TestHandlePreloadSpaceSkipsSOPPreloadWhenDisabled(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	config = Config{
		Databases:           []DatabaseConfig{{Name: "testdb", Path: filepath.Join(t.TempDir(), "db")}},
		DisableSOPKBPreload: true,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/spaces/preload", strings.NewReader(`{"template_id":"core_playbook","database_name":"testdb"}`))
	rec := httptest.NewRecorder()

	handlePreloadSpace(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, ok := resp["disabled"].(bool); !ok || !got {
		t.Fatalf("expected disabled=true in preload response, got %#v", resp)
	}
}

func TestHandlePreloadSpaceReturnsTaskIDForBackgroundPreload(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	tmpDir := t.TempDir()
	assetPath := filepath.Join(tmpDir, "sop_base_knowledge.json")
	if err := os.WriteFile(assetPath, []byte(`{"config":{},"categories":[],"items":[]}`), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	config = Config{Databases: []DatabaseConfig{{Name: "testdb", Path: filepath.Join(tmpDir, "db")}}}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/spaces/preload", strings.NewReader(`{"template_id":"SOP","database_name":"testdb"}`))
	rec := httptest.NewRecorder()

	handlePreloadSpace(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	taskID, ok := resp["task_id"].(string)
	if !ok || taskID == "" {
		t.Fatalf("expected task_id in preload response, got %#v", resp)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		task := GetTask(taskID)
		if task != nil && (task.Status == "completed" || task.Status == "error") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestHandlePreloadSpaceCompletesSOPAssetPreload(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	tmpDir := t.TempDir()
	config = Config{Databases: []DatabaseConfig{{Name: "testdb", Path: filepath.Join(tmpDir, "db")}}}

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot, err := filepath.Abs(filepath.Join(filepath.Dir(filename), "..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("abs repo root: %v", err)
	}
	assetPath := filepath.Join(repoRoot, "ai", "sop_base_knowledge.json")
	if _, err := os.Stat(assetPath); err != nil {
		t.Fatalf("expected SOP asset at %s: %v", assetPath, err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("chdir repo root: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/spaces/preload", strings.NewReader(`{"template_id":"SOP","database_name":"testdb"}`))
	rec := httptest.NewRecorder()

	handlePreloadSpace(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	taskID, ok := resp["task_id"].(string)
	if !ok || taskID == "" {
		t.Fatalf("expected task_id in preload response, got %#v", resp)
	}

	deadline := time.Now().Add(15 * time.Second)
	var task *Task
	for time.Now().Before(deadline) {
		task = GetTask(taskID)
		if task != nil && task.Status == "completed" {
			return
		}
		if task != nil && task.Status == "error" {
			t.Fatalf("SOP preload task failed: %+v", task)
		}
		time.Sleep(100 * time.Millisecond)
	}

	if task == nil {
		t.Fatalf("SOP preload task never registered")
	}
	t.Fatalf("SOP preload task did not complete in time: %+v", task)
}

func TestResolvePreloadFilePathFindsNestedRepoStyleAsset(t *testing.T) {
	tmpDir := t.TempDir()
	repoRoot := filepath.Join(tmpDir, "workspace")
	assetDir := filepath.Join(repoRoot, "lib", "sop", "ai")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatalf("mkdir asset dir: %v", err)
	}
	assetPath := filepath.Join(assetDir, "sop_base_knowledge.json")
	if err := os.WriteFile(assetPath, []byte(`{"config":{},"categories":[],"items":[]}`), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	workDir := filepath.Join(repoRoot, "tools", "httpserver")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir work dir: %v", err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("chdir work dir: %v", err)
	}

	got := resolvePreloadFilePath("SOP", []string{workDir, repoRoot, filepath.Join(repoRoot, "tools")})
	if got == "" {
		t.Fatalf("expected nested repo-style asset to resolve")
	}
	if got != assetPath {
		t.Fatalf("expected resolved path %q, got %q", assetPath, got)
	}
}

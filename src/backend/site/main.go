package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sharedcode/sop"

	siteaccount "github.com/grecinto/sop-cloud/src/backend/siteaccount"
	"github.com/grecinto/sop-cloud/src/backend/siteapp"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
	"github.com/sharedcode/sop/tools/sopbackend"
)

func resolveFrontendAssetPath(requestPath string) (string, bool) {
	trimmed := strings.TrimSpace(requestPath)
	if trimmed == "" || trimmed == "/" {
		return "index.html", true
	}
	if strings.HasPrefix(trimmed, "/site/") || trimmed == "/site" {
		if trimmed == "/site" {
			return "index.html", true
		}
		candidate := strings.TrimPrefix(trimmed, "/site")
		if candidate == "" || candidate == "/" {
			return "index.html", true
		}
		if strings.HasPrefix(candidate, "/") {
			candidate = strings.TrimPrefix(candidate, "/")
		}
		if candidate == "" {
			return "index.html", true
		}
		if strings.Contains(candidate, ".") {
			return path.Clean(candidate), false
		}
		return "index.html", true
	}
	if strings.HasPrefix(trimmed, "/assets/") || strings.HasPrefix(trimmed, "/favicon.ico") {
		return strings.TrimPrefix(path.Clean(trimmed), "/"), false
	}
	if strings.Contains(trimmed, ".") {
		return strings.TrimPrefix(path.Clean(trimmed), "/"), false
	}
	return "index.html", true
}

func defaultFrontendDir() string {
	if dir := strings.TrimSpace(os.Getenv("FRONTEND_DIR")); dir != "" {
		return dir
	}

	_, filename, _, ok := runtime.Caller(0)
	if ok {
		candidate := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "frontend", "dist"))
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return filepath.Clean("./src/frontend/dist")
}

func defaultDataDir() string {
	if dir := strings.TrimSpace(os.Getenv("SOP_CLOUD_DATA_DIR")); dir != "" {
		return dir
	}
	if dir := strings.TrimSpace(os.Getenv("datapath")); dir != "" {
		return dir
	}
	if dir := strings.TrimSpace(os.Getenv("SOP_DATA_DIR")); dir != "" {
		return dir
	}
	return filepath.Clean("./.sop-data")
}

func canonicalPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	if absPath, err := filepath.Abs(trimmed); err == nil {
		if resolvedPath, err := filepath.EvalSymlinks(absPath); err == nil {
			return resolvedPath
		}
		return absPath
	}
	return filepath.Clean(trimmed)
}

func resolveRuntimeDataDir(dataDir string) string {
	trimmed := strings.TrimSpace(dataDir)
	if trimmed == "" {
		return ""
	}
	if filepath.IsAbs(trimmed) {
		return canonicalPath(trimmed)
	}
	if cwd, err := os.Getwd(); err == nil {
		return canonicalPath(filepath.Join(cwd, trimmed))
	}
	return canonicalPath(trimmed)
}

func resolveRuntimeConfigPath(dataDir string) string {
	resolvedDataDir := resolveRuntimeDataDir(dataDir)
	if resolvedDataDir == "" {
		return ""
	}

	candidates := []string{}
	if envPath := strings.TrimSpace(os.Getenv("SOP_CONFIG_FILE")); envPath != "" {
		candidates = append(candidates, envPath)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "config.json"),
			filepath.Join(cwd, ".sop-data", "config.json"),
		)
	}
	candidates = append(candidates, filepath.Join(resolvedDataDir, "config.json"))

	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		if _, err := os.Stat(candidate); err == nil {
			return canonicalPath(candidate)
		}
	}
	return filepath.Join(resolvedDataDir, "config.json")
}

func configureRuntimeEnvironment(dataDir string) {
	resolvedDataDir := resolveRuntimeDataDir(dataDir)
	if resolvedDataDir == "" {
		return
	}
	if err := os.MkdirAll(resolvedDataDir, 0o755); err != nil {
		log.Printf("failed to create runtime data dir %s: %v", resolvedDataDir, err)
		return
	}
	if err := os.Setenv("datapath", resolvedDataDir); err != nil {
		log.Printf("failed to set datapath env var: %v", err)
	}
	if err := os.Setenv("SOP_CLOUD_DATA_DIR", resolvedDataDir); err != nil {
		log.Printf("failed to set SOP_CLOUD_DATA_DIR env var: %v", err)
	}
	if err := os.Setenv("SOP_DATA_DIR", resolvedDataDir); err != nil {
		log.Printf("failed to set SOP_DATA_DIR env var: %v", err)
	}
	if err := os.Setenv("SOP_CODING_WORKFLOW_MEMORY_PATH", filepath.Join(resolvedDataDir, "coding-workflow-memory.json")); err != nil {
		log.Printf("failed to set SOP_CODING_WORKFLOW_MEMORY_PATH env var: %v", err)
	}

	configPath := resolveRuntimeConfigPath(resolvedDataDir)
	sopbackend.SetConfigFile(configPath)
	if err := os.Setenv("SOP_CONFIG_FILE", configPath); err != nil {
		log.Printf("failed to set SOP_CONFIG_FILE env var: %v", err)
	}
	if _, err := os.Stat(configPath); err == nil {
		if err := sopbackend.LoadConfig(configPath); err != nil {
			log.Printf("failed to load existing config from %s: %v", configPath, err)
		}
	}
}

func registerSiteHandlers(mux *http.ServeMux, frontendDir string) {
	if mux == nil {
		return
	}

	// The React site is a separate product website and is hosted by this standalone backend.
	// The shared SOP UI/backend is also mounted here so site-admin/root users can manage
	// the hosted environment through the same shared handlers used by the standalone httpserver/daemon.
	siteaccount.InstallSopbackendUserStoreOverride()
	sopbackend.RegisterAllHandlersWithoutRoot(mux)
	siteaccount.RegisterHandlers(mux)
	siteaccount.InstallSopbackendUserStoreOverride()
	mux.HandleFunc("/api/daemon/bootstrap-secret", handleDaemonBootstrapSecret)
	mux.HandleFunc("/api/coding-workflow/execute", handleCodingWorkflowExecution)
	registerFrontendHandler(mux, frontendDir)
}

type siteAppAccessHandler struct {
	next http.Handler
}

func (h *siteAppAccessHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/app" || strings.HasPrefix(r.URL.Path, "/app/") {
		auth := sop.GetAuthFromContext(r.Context())
		if auth.UserID == "" && len(auth.Roles) == 0 && !auth.IsSystem {
			if !sopbackend.HasConfigFileOnDisk() {
				h.next.ServeHTTP(w, r)
				return
			}
			h.next.ServeHTTP(w, r)
			return
		}
		if isSiteAdminAuthContext(auth) {
			h.next.ServeHTTP(w, r)
			return
		}
		if hasActiveSiteSubscription(r.Context(), auth) {
			http.Redirect(w, r, "http://127.0.0.1:9090", http.StatusFound)
			return
		}
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}
	h.next.ServeHTTP(w, r)
}

func newSiteAppAccessHandler(mux *http.ServeMux) http.Handler {
	return &siteAppAccessHandler{next: sopbackend.NewObservabilityHandler(mux)}
}

func isSiteAdminAuthContext(auth sop.AuthContext) bool {
	for _, role := range auth.Roles {
		if sop.IsAdminRole(role) || strings.EqualFold(role, sop.RoleSystemAdmin) {
			return true
		}
	}
	return auth.IsSystem || strings.EqualFold(strings.TrimSpace(auth.UserID), "root")
}

func hasActiveSiteSubscription(ctx context.Context, auth sop.AuthContext) bool {
	_ = ctx
	return siteAppAccessAllowed(auth)
}

func siteAppAccessAllowed(auth sop.AuthContext) bool {
	return siteapp.HasSiteAppAccess(auth)
}

const siteAppPermissionName = siteapp.PermissionSiteAppAccess

func handleAppProxy(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/app" || r.URL.Path == "/app/" {
		http.Redirect(w, r, "/site/", http.StatusFound)
		return
	}
	http.NotFound(w, r)
}

func serveFrontendAsset(w http.ResponseWriter, r *http.Request, frontendDir string) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	assetPath, spa := resolveFrontendAssetPath(r.URL.Path)
	if spa {
		http.ServeFile(w, r, path.Join(frontendDir, "index.html"))
		return
	}
	if assetPath == "" {
		assetPath = "index.html"
	}
	pathToServe := path.Join(frontendDir, assetPath)
	if _, err := os.Stat(pathToServe); err == nil {
		http.ServeFile(w, r, pathToServe)
		return
	}
	http.ServeFile(w, r, path.Join(frontendDir, "index.html"))
}

type codingWorkflowRequest struct {
	WorkingDir       string            `json:"working_dir"`
	ActiveFile       string            `json:"active_file"`
	Description      string            `json:"description"`
	AllowDestructive bool              `json:"allow_destructive"`
	Patch            *cw.Patch         `json:"patch,omitempty"`
	Patches          []*cw.Patch       `json:"patches,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	Mode             string            `json:"mode,omitempty"`
}

type codingWorkflowResponse struct {
	Message              string   `json:"message"`
	TaskType             string   `json:"task_type"`
	Plan                 []string `json:"plan,omitempty"`
	ActiveFile           string   `json:"active_file,omitempty"`
	RepositorySummary    string   `json:"repository_summary,omitempty"`
	Provider             string   `json:"provider,omitempty"`
	ProviderSource       string   `json:"provider_source,omitempty"`
	ProviderModel        string   `json:"provider_model,omitempty"`
	ExpertiseSummary     string   `json:"expertise_summary,omitempty"`
	ExpertiseSources     []string `json:"expertise_sources,omitempty"`
	VisualizationSummary string   `json:"visualization_summary,omitempty"`
	RelatedFiles         []string `json:"related_files,omitempty"`
	TestFiles            []string `json:"test_files,omitempty"`
	AppliedPatches       int      `json:"applied_patches"`
}

func handleCodingWorkflowExecution(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req codingWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	state := cw.RunState{Metadata: map[string]string{}}
	if req.Metadata != nil {
		state.Metadata = req.Metadata
	}
	if strings.TrimSpace(req.Mode) != "" {
		state.Metadata["mode"] = req.Mode
	}

	task := cw.Task{
		WorkingDir:       req.WorkingDir,
		ActiveFile:       req.ActiveFile,
		Description:      req.Description,
		Patch:            req.Patch,
		Patches:          req.Patches,
		AllowDestructive: req.AllowDestructive,
		State:            state,
	}
	orchestrator := &cw.Orchestrator{
		Policy:        &cw.DefaultSafetyPolicy{},
		MaxIterations: 3,
	}
	result, err := orchestrator.Execute(task)
	if err != nil {
		log.Printf("coding workflow execution failed: %v", err)
		http.Error(w, fmt.Sprintf("workflow execution failed: %v", err), http.StatusInternalServerError)
		return
	}

	resp := codingWorkflowResponse{
		Message:              result.Message,
		TaskType:             result.TaskType,
		Plan:                 result.Plan,
		ActiveFile:           result.ContextPack.ActiveFile,
		RepositorySummary:    result.ContextPack.RepositorySummary,
		Provider:             result.ContextPack.Provider,
		ProviderSource:       result.ContextPack.ProviderSource,
		ProviderModel:        result.ContextPack.ProviderModel,
		ExpertiseSummary:     result.ContextPack.ExpertiseSummary,
		ExpertiseSources:     result.ContextPack.ExpertiseSources,
		VisualizationSummary: result.ContextPack.VisualizationSummary,
		RelatedFiles:         result.ContextPack.RelatedFiles,
		TestFiles:            result.ContextPack.TestFiles,
		AppliedPatches:       len(result.AppliedPatches),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func handleDaemonBootstrapSecret(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var configDir string
	if v := os.Getenv("XDG_CONFIG_HOME"); strings.TrimSpace(v) != "" {
		configDir = filepath.Join(v, "sop")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Printf("failed to get user home directory: %v", err)
			http.Error(w, "Failed to get config directory", http.StatusInternalServerError)
			return
		}
		configDir = filepath.Join(home, ".config", "sop")
	}

	bootstrapPath := filepath.Join(configDir, "daemon.bootstrap")
	secretBytes, err := os.ReadFile(bootstrapPath)
	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Bootstrap file not found. Daemon may not be running."})
			return
		}
		log.Printf("failed to read bootstrap file: %v", err)
		http.Error(w, "Failed to read bootstrap file", http.StatusInternalServerError)
		return
	}

	secret := strings.TrimSpace(string(secretBytes))
	if secret == "" {
		log.Printf("bootstrap file is empty")
		http.Error(w, "Bootstrap file is empty", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"bootstrap_secret": secret})
}

func registerFrontendHandler(mux *http.ServeMux, frontendDir string) {
	frontendHandler := func(w http.ResponseWriter, r *http.Request) {
		// Routing contract for the new site:
		// - If no runtime config exists (or provisioning is not complete), redirect to /app so the
		//   shared SetupWizard can provision the site as SA.
		// - If config is present and provisioning is complete, serve the landing page so visitors can
		//   log in or subscribe.
		if r.URL.Path == "/" && !sopbackend.HasConfigFileOnDisk() {
			http.Redirect(w, r, "/app", http.StatusFound)
			return
		}
		serveFrontendAsset(w, r, frontendDir)
	}
	mux.HandleFunc("/", frontendHandler)
	mux.HandleFunc("/site", frontendHandler)
	mux.HandleFunc("/site/", frontendHandler)
}

func main() {
	dataDir := defaultDataDir()
	configureRuntimeEnvironment(dataDir)
	frontendDir := defaultFrontendDir()

	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	addr := ":" + port
	log.Printf("site backend listening on %s", addr)
	if err := http.ListenAndServe(addr, newSiteAppAccessHandler(mux)); err != nil {
		panic(fmt.Sprintf("listen failed: %v", err))
	}
}

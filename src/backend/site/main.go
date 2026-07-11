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
	"time"

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

func defaultDocsDir() string {
	if dir := strings.TrimSpace(os.Getenv("DOCS_DIR")); dir != "" {
		return dir
	}

	_, filename, _, ok := runtime.Caller(0)
	if ok {
		candidate := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "docs-site", "build"))
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return filepath.Clean("./src/docs-site/build")
}

func defaultKnowledgeManifestPath() string {
	if path := strings.TrimSpace(os.Getenv("SOP_KB_CURATION_MANIFEST_PATH")); path != "" {
		return path
	}

	_, filename, _, ok := runtime.Caller(0)
	if ok {
		candidate := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", "lib", "sop", "ai", "KB_CURATION_MANIFEST.md"))
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return filepath.Clean("./lib/sop/ai/KB_CURATION_MANIFEST.md")
}

func resolveKnowledgeAssetPath(requestPath string) (string, bool) {
	trimmed := strings.TrimSpace(requestPath)
	if trimmed == "" || trimmed == "/" || trimmed == "/kb" {
		return "", false
	}
	if strings.HasPrefix(trimmed, "/kb/") {
		trimmed = strings.TrimPrefix(trimmed, "/kb/")
	} else if strings.HasPrefix(trimmed, "/kb") {
		trimmed = strings.TrimPrefix(trimmed, "/kb")
	}
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed == "" {
		return "", false
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || strings.HasPrefix(cleaned, "..") {
		return "", false
	}
	return cleaned, true
}

func serveKnowledgeAsset(w http.ResponseWriter, r *http.Request, knowledgeBaseDir string) {
	assetPath, ok := resolveKnowledgeAssetPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	baseDir := filepath.Clean(knowledgeBaseDir)
	targetPath := filepath.Clean(filepath.Join(baseDir, assetPath))
	relPath, err := filepath.Rel(baseDir, targetPath)
	if err != nil || relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	contentType := http.DetectContentType(data)
	if strings.HasSuffix(strings.ToLower(targetPath), ".md") {
		contentType = "text/markdown; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(data)
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

func registerSiteHandlers(mux *http.ServeMux, frontendDir, docsDir string) {
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
	registerDocsHandler(mux, docsDir)
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

func serveDocsAsset(w http.ResponseWriter, r *http.Request, docsDir string) {
	w.Header().Set("Cache-Control", "public, max-age=3600")
	// Remove /docs prefix from path
	assetPath := strings.TrimPrefix(r.URL.Path, "/docs")
	if assetPath == "" || assetPath == "/" || assetPath == "/intro" || assetPath == "/intro/" || assetPath == "/intro/index.html" || assetPath == "/intro.html" {
		http.Redirect(w, r, "/docs/sdk/intro", http.StatusFound)
		return
	}
	assetPath = path.Clean(assetPath)
	pathToServe := path.Join(docsDir, assetPath)

	// Check if file exists
	if info, err := os.Stat(pathToServe); err == nil {
		// If it's a directory, serve index.html from it
		if info.IsDir() {
			indexPath := path.Join(pathToServe, "index.html")
			if _, err := os.Stat(indexPath); err == nil {
				http.ServeFile(w, r, indexPath)
				return
			}
		}
		// Serve the file
		http.ServeFile(w, r, pathToServe)
		return
	}

	// If file not found, serve root index.html for SPA routing
	http.ServeFile(w, r, path.Join(docsDir, "index.html"))
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

func startSiteBackgroundServices(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	go sopbackend.StartBackgroundServiceWithContext(ctx, sopbackend.DefaultSessionCleanupInterval(), 2*time.Hour)
}

func registerDocsHandler(mux *http.ServeMux, docsDir string) {
	docsHandler := func(w http.ResponseWriter, r *http.Request) {
		// Authentication is enforced by siteAppAccessHandler wrapper
		// Docs are only accessible to authenticated users
		serveDocsAsset(w, r, docsDir)
	}
	mux.HandleFunc("/docs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/docs" {
			http.Redirect(w, r, "/docs/", http.StatusFound)
			return
		}
		docsHandler(w, r)
	})
	mux.HandleFunc("/docs/", docsHandler)
	mux.HandleFunc("/kb", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/kb" {
			http.Redirect(w, r, "/kb/", http.StatusFound)
			return
		}
		serveKnowledgeAsset(w, r, filepath.Dir(defaultKnowledgeManifestPath()))
	})
	mux.HandleFunc("/kb/", func(w http.ResponseWriter, r *http.Request) {
		serveKnowledgeAsset(w, r, filepath.Dir(defaultKnowledgeManifestPath()))
	})
}

func applySopbackendPreloadPolicy() {
	cfg := sopbackend.GetConfig()
	if cfg.DisableSOPKBPreload {
		return
	}

	sopbackend.SetConfig(sopbackend.Config{
		Port:                   cfg.Port,
		Databases:              cfg.Databases,
		PageSize:               cfg.PageSize,
		SystemDB:               cfg.SystemDB,
		ProductionMode:         cfg.ProductionMode,
		SessionTokenTTLMinutes: cfg.SessionTokenTTLMinutes,
		SessionSecret:          cfg.SessionSecret,
		AuthProviderName:       cfg.AuthProviderName,
		Users:                  cfg.Users,
		Subscriptions:          cfg.Subscriptions,
		Payments:               cfg.Payments,
		ObfuscationMode:        cfg.ObfuscationMode,
		DatabasePath:           cfg.DatabasePath,
		Mode:                   cfg.Mode,
		ConfigFile:             cfg.ConfigFile,
		RedisURL:               cfg.RedisURL,
		DisableAuth:            cfg.DisableAuth,
		DisableSOPKBPreload:    true,
	})
}

func main() {
	dataDir := defaultDataDir()
	configureRuntimeEnvironment(dataDir)
	applySopbackendPreloadPolicy()
	frontendDir := defaultFrontendDir()
	docsDir := defaultDocsDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startSiteBackgroundServices(ctx)

	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir, docsDir)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	addr := ":" + port
	log.Printf("site backend listening on %s", addr)
	log.Printf("frontend: %s, docs: %s", frontendDir, docsDir)
	if err := http.ListenAndServe(addr, newSiteAppAccessHandler(mux)); err != nil {
		panic(fmt.Sprintf("listen failed: %v", err))
	}
}

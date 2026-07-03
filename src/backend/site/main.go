package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	siteaccount "github.com/grecinto/sop-cloud/src/backend/siteaccount"
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
	sopbackend.RegisterAllHandlers(mux)
	siteaccount.InstallSopbackendUserStoreOverride()
	mux.HandleFunc("/api/daemon/bootstrap-secret", handleDaemonBootstrapSecret)
	registerFrontendHandler(mux, frontendDir)
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
	mux.HandleFunc("/site", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, path.Join(frontendDir, "index.html"))
	})
	mux.HandleFunc("/site/", func(w http.ResponseWriter, r *http.Request) {
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
	})
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
	if err := http.ListenAndServe(addr, sopbackend.NewObservabilityHandler(mux)); err != nil {
		panic(fmt.Sprintf("listen failed: %v", err))
	}
}

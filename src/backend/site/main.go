package main

import (
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

func configureRuntimeEnvironment(dataDir string) {
	if strings.TrimSpace(dataDir) == "" {
		return
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Printf("failed to create runtime data dir %s: %v", dataDir, err)
		return
	}
	if err := os.Setenv("datapath", dataDir); err != nil {
		log.Printf("failed to set datapath env var: %v", err)
	}
	if err := os.Setenv("SOP_CLOUD_DATA_DIR", dataDir); err != nil {
		log.Printf("failed to set SOP_CLOUD_DATA_DIR env var: %v", err)
	}
	if err := os.Setenv("SOP_DATA_DIR", dataDir); err != nil {
		log.Printf("failed to set SOP_DATA_DIR env var: %v", err)
	}

	configPath := filepath.Join(dataDir, "config.json")
	if _, err := os.Stat(configPath); err != nil && os.IsNotExist(err) {
		if err := os.WriteFile(configPath, []byte(`{"port":8081}`), 0o644); err != nil {
			log.Printf("failed to seed config at %s: %v", configPath, err)
		}
	}
	sopbackend.SetConfigFile(configPath)
	if err := os.Setenv("SOP_CONFIG_FILE", configPath); err != nil {
		log.Printf("failed to set SOP_CONFIG_FILE env var: %v", err)
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
	sopbackend.RegisterSwaggerHandlers(mux, "")
	sopbackend.RegisterMonitorabilityHandlers(mux)
	siteaccount.RegisterHandlers(mux)
	registerFrontendHandler(mux, frontendDir)
}

func registerFrontendHandler(mux *http.ServeMux, frontendDir string) {
	filesystem := http.Dir(frontendDir)
	fileServer := http.FileServer(filesystem)
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
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/site" || strings.HasPrefix(r.URL.Path, "/site/") {
			http.ServeFile(w, r, path.Join(frontendDir, "index.html"))
			return
		}
		fileServer.ServeHTTP(w, r)
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

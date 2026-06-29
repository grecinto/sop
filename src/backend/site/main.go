package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"strings"

	siteapi "github.com/grecinto/sop-cloud/src/backend/siteapi"
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
	frontendDir := os.Getenv("FRONTEND_DIR")
	if frontendDir == "" {
		frontendDir = "./src/frontend/dist"
	}

	mux := http.NewServeMux()
	sopbackend.RegisterAllHandlers(mux)
	siteapi.RegisterHandlers(mux)
	sopbackend.RegisterMonitorabilityHandlers(mux)
	registerFrontendHandler(mux, frontendDir)

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

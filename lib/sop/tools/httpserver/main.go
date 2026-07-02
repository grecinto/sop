package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	log "log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sharedcode/sop"
	sharedagent "github.com/sharedcode/sop/ai/agent"
	"github.com/sharedcode/sop/tools/siteapi"
	"github.com/sharedcode/sop/tools/sopbackend"
)

var content = sopbackend.SharedUIAssets()

func setupWizardAIConfigJSON() template.JS {
	return template.JS("{}")
}

func modelCatalogJSON() template.JS {
	catalog := sopbackend.GetModelCatalog()
	b, err := json.Marshal(catalog)
	if err != nil {
		return template.JS("{}")
	}
	return template.JS(b)
}

func defaultRuntimeAutomationPolicy() sharedagent.AutomationPolicy {
	policy := sharedagent.DefaultAutomationPolicy()
	policy.Enabled = false
	policy.AllowShellCommands = false
	policy.AllowEmail = false
	policy.RequireApproval = true
	policy.DefaultTimeoutSeconds = 600
	policy.MaxTimeoutSeconds = 1800
	return policy
}

func applyRedisOverride(cfg *sopbackend.Config, redisURL string, wasFlagPassed bool) {
	if cfg == nil || !wasFlagPassed {
		return
	}
	adjusted := strings.TrimSpace(redisURL)
	if adjusted == "" {
		return
	}
	cfg.RedisURL = adjusted
	if cfg.SystemDB != nil {
		cfg.SystemDB.RedisURL = adjusted
	}
	for i := range cfg.Databases {
		cfg.Databases[i].RedisURL = adjusted
	}
}

func main() {
	var showVersion bool
	var openBrowserFlag bool
	var logLevelFlag string
	var port int
	var databasePath string
	var mode string
	var configFile string
	var redisURL string
	var pageSize int
	var productionMode bool

	flag.BoolVar(&showVersion, "version", false, "Show version and exit")
	flag.BoolVar(&openBrowserFlag, "open-browser", true, "Open browser on startup")
	flag.StringVar(&logLevelFlag, "log-level", "info", "Set log level (debug, info, warn, error). Default is info.")
	flag.IntVar(&port, "port", 8080, "Port to run the server on")
	flag.StringVar(&databasePath, "database", "/tmp/sop_data", "Path to the SOP database/data directory")
	flag.StringVar(&mode, "mode", "standalone", "SOP mode: 'standalone' or 'clustered'")
	flag.StringVar(&configFile, "config", "", "Path to configuration file (optional)")
	flag.StringVar(&redisURL, "redis", "localhost:6379", "Redis URL for clustered mode (e.g. localhost:6379)")
	flag.IntVar(&pageSize, "pageSize", 40, "Number of items to display per page")
	flag.BoolVar(&productionMode, "production", false, "Enable Production mode (use real Embedder and LLM)")
	flag.Parse()

	var wasProductionFlagPassed bool
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "production" {
			wasProductionFlagPassed = true
		}
	})

	if !wasProductionFlagPassed {
		arg0 := filepath.ToSlash(os.Args[0])
		if !strings.Contains(arg0, "go-build") && !strings.Contains(arg0, "/Temp/") && !strings.Contains(arg0, "/tmp/") && !strings.HasSuffix(arg0, ".test") {
			productionMode = true
		}
	}

	var lvl log.Level
	logLevelFlagUpper := strings.ToUpper(logLevelFlag)
	switch logLevelFlagUpper {
	case "DEBUG":
		lvl = log.LevelDebug
	case "WARN":
		lvl = log.LevelWarn
	case "ERROR":
		lvl = log.LevelError
	default:
		lvl = log.LevelInfo
	}

	l := log.New(log.NewJSONHandler(os.Stdout, &log.HandlerOptions{
		Level: lvl,
	}))
	log.SetDefault(l)

	if showVersion {
		fmt.Printf("SOP Data Manager v%s\n", sop.Version)
		os.Exit(0)
	}

	// Load configuration and initialize the shared backend once.
	effectiveProductionMode := productionMode
	if !wasProductionFlagPassed {
		effectiveProductionMode = true
	}
	targetConfigPath, err := sopbackend.InitializeRuntime(context.Background(), configFile, sopbackend.RuntimeOptions{}, port, &effectiveProductionMode)
	if err != nil {
		log.Error(fmt.Sprintf("Failed to initialize shared backend: %v", err))
	} else if _, statErr := os.Stat(targetConfigPath); statErr == nil {
		if sopbackend.HasConfigFileOnDisk() {
			log.Debug(fmt.Sprintf("Loaded configuration from: %s", targetConfigPath))
			config := sopbackend.GetConfig()
			if len(config.Databases) == 0 && config.SystemDB == nil {
				log.Warn(fmt.Sprintf("Configuration file '%s' does not define a usable database setup.", targetConfigPath))
			}
		} else {
			log.Info(fmt.Sprintf("Configuration file exists but does not define a usable setup: %s", targetConfigPath))
		}
	} else {
		log.Info(fmt.Sprintf("No configuration file found; using defaults: %s", targetConfigPath))
	}

	// Path validation and warning checks
	config := sopbackend.GetConfig()
	for i := range config.Databases {
		if abs, err := filepath.Abs(config.Databases[i].Path); err == nil {
			config.Databases[i].Path = abs
		}
		if _, err := os.Stat(config.Databases[i].Path); os.IsNotExist(err) {
			msg := fmt.Sprintf("Warning: Database '%s' does not exist at path '%s'.", config.Databases[i].Name, config.Databases[i].Path)
			log.Warn(msg)
			config.Databases[i].Warning = msg
		} else {
			for _, ec := range config.Databases[i].ErasureConfigs {
				for _, bp := range ec.BasePaths {
					if _, err := os.Stat(bp); os.IsNotExist(err) {
						msg := fmt.Sprintf("Warning: EC data path '%s' does not exist for database '%s'.", bp, config.Databases[i].Name)
						log.Warn(msg)
						if config.Databases[i].Warning == "" {
							config.Databases[i].Warning = msg
						} else {
							config.Databases[i].Warning += " " + msg
						}
					}
				}
			}
		}
	}

	if config.SystemDB != nil {
		if abs, err := filepath.Abs(config.SystemDB.Path); err == nil {
			config.SystemDB.Path = abs
		}
		if _, err := os.Stat(config.SystemDB.Path); os.IsNotExist(err) {
			msg := fmt.Sprintf("Warning: System DB does not exist at path '%s'.", config.SystemDB.Path)
			log.Warn(msg)
			config.SystemDB.Warning = msg
		} else {
			for _, ec := range config.SystemDB.ErasureConfigs {
				for _, bp := range ec.BasePaths {
					if _, err := os.Stat(bp); os.IsNotExist(err) {
						msg := fmt.Sprintf("Warning: System DB EC data path does not exist at '%s'.", bp)
						log.Warn(msg)
						if config.SystemDB.Warning == "" {
							config.SystemDB.Warning = msg
						} else {
							config.SystemDB.Warning += " " + msg
						}
					}
				}
			}
		}
	} else if len(config.Databases) > 0 {
		implicitPath := "."
		if config.ConfigFile != "" {
			implicitPath = filepath.Dir(config.ConfigFile)
		}
		implPath := filepath.Join(implicitPath, sopbackend.SystemDBName)
		if _, err := os.Stat(implPath); os.IsNotExist(err) {
			log.Warn(fmt.Sprintf("Warning: Implicit System DB does not exist at path '%s'.", implPath))
		}
	}

	// Daemon Bootstrap API (for UI to authenticate with local daemon)
	http.HandleFunc("/api/daemon/bootstrap-secret", handleDaemonBootstrapSecret)

	// Register backend API handlers
	sopbackend.RegisterAllHandlers(http.DefaultServeMux)
	siteapi.RegisterHandlers(http.DefaultServeMux)
	registerSiteFrontendRoutes(http.DefaultServeMux)
	sopbackend.RegisterSwaggerHandlers(http.DefaultServeMux, "")
	sopbackend.RegisterMonitorabilityHandlers(http.DefaultServeMux)

	// Start shared background services
	sopbackend.StartBackgroundService(sopbackend.DefaultSessionCleanupInterval(), 2*time.Hour)

	// Start Server
	addr := fmt.Sprintf(":%d", config.Port)
	log.Debug(fmt.Sprintf("SOP Data Manager v%s running at http://localhost%s", sop.Version, addr))

	// Open Browser
	if openBrowserFlag {
		go func() {
			time.Sleep(500 * time.Millisecond)
			openBrowser(fmt.Sprintf("http://localhost:%d", config.Port))
		}()
	}

	// Dev bypass for localhost
	var handler http.Handler = http.DefaultServeMux
	isGoRun := strings.Contains(os.Args[0], "go-build")

	if isGoRun {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			isLocalhost := strings.HasPrefix(r.RemoteAddr, "127.0.0.1:") || strings.HasPrefix(r.RemoteAddr, "[::1]:")
			if isGoRun && isLocalhost {
				http.DefaultServeMux.ServeHTTP(w, r)
				return
			}
			http.DefaultServeMux.ServeHTTP(w, r)
		})
	}

	if err := http.ListenAndServe(addr, sopbackend.NewObservabilityHandler(handler)); err != nil {
		log.Error(err.Error())
	}
}

func registerSiteFrontendRoutes(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.HandleFunc("/site", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/site" {
			http.Redirect(w, r, "/site/", http.StatusFound)
			return
		}
		serveSiteFrontend(w, r)
	})
	mux.HandleFunc("/site/", serveSiteFrontend)
}

func serveSiteFrontend(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/site/subscribe") {
		if distDir := frontendDistDir(); distDir != "" {
			indexPath := filepath.Join(distDir, "index.html")
			if content, err := os.ReadFile(indexPath); err == nil {
				html := string(content)
				fallback := `<div id="site-shell-fallback"><h1>Secure checkout</h1><p>Choose your plan and complete the monthly subscription securely.</p></div>`
				html = strings.Replace(html, "</body>", `<script>window.__SITE_INITIAL_ROUTE__ = "/site/subscribe";</script>`+fallback+"</body>", 1)
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = io.WriteString(w, html)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html lang="en"><head><meta charset="utf-8" /><meta name="viewport" content="width=device-width, initial-scale=1" /><title>SOP Cloud</title></head><body><div id="root"><h1>Secure checkout</h1><p>Choose your plan and complete the monthly subscription securely.</p></div></body></html>`)
		return
	}

	if distDir := frontendDistDir(); distDir != "" {
		indexPath := filepath.Join(distDir, "index.html")
		if info, err := os.Stat(indexPath); err == nil && !info.IsDir() {
			pathPrefix := strings.TrimPrefix(r.URL.Path, "/site")
			if pathPrefix != "" && pathPrefix != "/" {
				assetPath := filepath.Join(distDir, filepath.FromSlash(strings.TrimPrefix(pathPrefix, "/")))
				if info, err := os.Stat(assetPath); err == nil && !info.IsDir() {
					http.ServeFile(w, r, assetPath)
					return
				}
			}
			http.ServeFile(w, r, indexPath)
			return
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>SOP Cloud</title>
    <style>
      body { font-family: Inter, system-ui, sans-serif; margin: 0; background: #07111f; color: #f5f7fb; }
      main { max-width: 900px; margin: 0 auto; padding: 4rem 1.5rem; }
      .card { background: rgba(255,255,255,0.08); border: 1px solid rgba(255,255,255,0.12); border-radius: 20px; padding: 2rem; }
      a { color: #7dd3fc; }
    </style>
  </head>
  <body>
    <main>
      <div class="card">
        <h1>SOP Cloud</h1>
        <p>The new product experience is now served from the dedicated site route.</p>
        <p>This shell is a placeholder until the React build is generated for the full experience.</p>
      </div>
    </main>
  </body>
</html>`)
}

func frontendDistDir() string {
	candidates := []string{}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "frontend", "dist"))
		candidates = append(candidates, filepath.Join(cwd, "..", "frontend", "dist"))
		candidates = append(candidates, filepath.Join(cwd, "..", "..", "frontend", "dist"))
		candidates = append(candidates, filepath.Join(cwd, "..", "..", "..", "frontend", "dist"))
		candidates = append(candidates, filepath.Join(cwd, "..", "..", "..", "..", "frontend", "dist"))
	}
	if _, filename, _, ok := runtime.Caller(0); ok {
		base := filepath.Dir(filename)
		candidates = append(candidates, filepath.Join(base, "..", "..", "..", "..", "frontend", "dist"))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		log.Error(fmt.Sprintf("Failed to open browser: %v", err))
	}
}

// handleDaemonBootstrapSecret provides the daemon bootstrap secret to the UI
// This allows the browser to authenticate with the local daemon
func handleDaemonBootstrapSecret(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get daemon config directory
	var configDir string
	if v := os.Getenv("XDG_CONFIG_HOME"); strings.TrimSpace(v) != "" {
		configDir = filepath.Join(v, "sop")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Error("Failed to get user home directory", "error", err)
			http.Error(w, "Failed to get config directory", http.StatusInternalServerError)
			return
		}
		configDir = filepath.Join(home, ".config", "sop")
	}

	bootstrapPath := filepath.Join(configDir, "daemon.bootstrap")

	// Read bootstrap secret
	secretBytes, err := os.ReadFile(bootstrapPath)
	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Bootstrap file not found. Daemon may not be running.",
			})
			return
		}
		log.Error("Failed to read bootstrap file", "error", err)
		http.Error(w, "Failed to read bootstrap file", http.StatusInternalServerError)
		return
	}

	secret := strings.TrimSpace(string(secretBytes))
	if secret == "" {
		log.Error("Bootstrap file is empty")
		http.Error(w, "Bootstrap file is empty", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"bootstrap_secret": secret,
	})
}

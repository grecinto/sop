package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	log "log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sharedcode/sop"
	aidb "github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/tools/sopbackend"
)

//go:embed templates/*
var content embed.FS

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

	// Load configuration
	targetConfigPath := configFile
	if targetConfigPath == "" {
		targetConfigPath = sopbackend.FindExistingConfigFile()
	}
	if targetConfigPath == "" {
		if cwd, err := os.Getwd(); err == nil {
			targetConfigPath = filepath.Join(cwd, "config.json")
		} else {
			targetConfigPath = "config.json"
		}
	}

	if _, err := os.Stat(targetConfigPath); err == nil {
		err := sopbackend.LoadConfig(targetConfigPath)
		if err != nil {
			log.Error(fmt.Sprintf("Failed to load config file: %v", err))
		} else {
			sopbackend.SetConfigFile(targetConfigPath)
			if _, err := sopbackend.LoadModelCatalog(targetConfigPath); err != nil {
				log.Error(fmt.Sprintf("Failed to load model catalog: %v", err))
			}
			log.Debug(fmt.Sprintf("Loaded configuration from: %s", targetConfigPath))
			config := sopbackend.GetConfig()
			if len(config.Databases) == 0 && config.SystemDB == nil {
				log.Warn(fmt.Sprintf("Loaded configuration file '%s' but found 0 databases defined.", targetConfigPath))
			}
		}
	} else if _, err := sopbackend.LoadModelCatalog(""); err != nil {
		log.Error(fmt.Sprintf("Failed to load model catalog: %v", err))
	}

	// Set CLI flags if provided
	if port != 0 {
		sopbackend.SetPort(port)
	}
	if !wasProductionFlagPassed {
		sopbackend.SetProductionMode(true)
	} else {
		sopbackend.SetProductionMode(productionMode)
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

	// Initialize backend (agents, RBAC, sessions)
	if len(config.Databases) > 0 || config.SystemDB != nil {
		if err := sopbackend.InitAgents(context.Background()); err != nil {
			log.Error(fmt.Sprintf("Failed to initialize agents: %v", err))
		}
	}

	// UI Routes
	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/login", handleLoginPage)
	http.HandleFunc("/app", sopbackend.RequireAuth(handleIndex))
	http.HandleFunc("/viewer", sopbackend.RequireAuth(handleViewer))
	http.HandleFunc("/file-preview", sopbackend.RequireAuth(handleFilePreview))

	// Daemon Bootstrap API (for UI to authenticate with local daemon)
	http.HandleFunc("/api/daemon/bootstrap-secret", handleDaemonBootstrapSecret)

	// Register backend API handlers
	sopbackend.RegisterAllHandlers(http.DefaultServeMux)

	// Start session cleanup
	sopbackend.StartSessionCleanup(1*time.Minute, 2*time.Hour)

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

	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Error(err.Error())
	}
}

func shouldShowEnvironmentManager(r *http.Request) bool {
	if r == nil {
		return true
	}

	auth := sop.GetAuthFromContext(r.Context())
	return sopbackend.CanManageEnvironments(auth)
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

func handleRoot(w http.ResponseWriter, r *http.Request) {
	if sopbackend.HasConfigFileOnDisk() {
		handleLoginPage(w, r)
		return
	}
	http.Redirect(w, r, "/app", http.StatusFound)
}

func handleLoginPage(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFS(content, "templates/*.html")
	if err != nil {
		http.Error(w, "Could not load login template: "+err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Version": sop.Version,
	}
	if err := tmpl.ExecuteTemplate(w, "login.html", data); err != nil {
		log.Error("Failed to execute login template", "error", err)
		http.Error(w, "Login template execution failed", http.StatusInternalServerError)
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFS(content, "templates/*.html")
	if err != nil {
		http.Error(w, "Could not load template: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Check if "users" store exists
	hasDemo := false
	ctx := r.Context()
	config := sopbackend.GetConfig()
	for _, dbCfg := range config.Databases {
		if dbOpts, err := sopbackend.GetDBOptionsFromConfig(ctx, &dbCfg); err == nil {
			db := aidb.NewDatabase(dbOpts)
			if exists, err := db.StoreExists(ctx, "users"); err == nil && exists {
				hasDemo = true
				break
			}
		}
	}

	// Check if SystemDB exists
	isEnterprise := config.SystemDB != nil
	if !isEnterprise && len(config.Databases) > 0 {
		isEnterprise = true
	}

	data := map[string]any{
		"Version":                 sop.Version,
		"Mode":                    config.Mode,
		"ModelCatalog":            sopbackend.GetModelCatalog(),
		"AllowInvalidMapKey":      os.Getenv("SOP_ALLOW_INVALID_MAP_KEY") == "true",
		"HasDemo":                 hasDemo,
		"LLMProvider":             "openai",
		"IsEnterprise":            isEnterprise,
		"SystemDBName":            sopbackend.SystemDBName,
		"ConfigFile":              config.ConfigFile,
		"SetupWizardAIConfigJSON": setupWizardAIConfigJSON(),
		"ModelCatalogJSON":        modelCatalogJSON(),
		"MinHashMod":              1,
		"MaxHashMod":              256,
		"CanManageEnvironments":   shouldShowEnvironmentManager(r),
	}
	if err := tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		log.Error("Failed to execute template", "error", err)
		http.Error(w, "Template execution failed", http.StatusInternalServerError)
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

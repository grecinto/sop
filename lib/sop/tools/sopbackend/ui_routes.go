package sopbackend

import (
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sharedcode/sop"
	aidb "github.com/sharedcode/sop/ai/database"
)

// UIRouter serves the shared SOP web UI from the sopbackend package so both
// httpserver and daemon-based apps can reuse the same handlers and templates.
type UIRouter struct {
	TemplateFS fs.FS
	Pattern    string
	Templates  *template.Template
}

// NewUIRouter builds a router that loads templates from the provided filesystem.
func NewUIRouter(templateFS fs.FS, pattern string) *UIRouter {
	if templateFS == nil {
		templateFS = osDirFS(sharedTemplateDir())
	}
	if pattern == "" {
		pattern = "*.html"
	}
	return &UIRouter{TemplateFS: templateFS, Pattern: pattern}
}

// Register wires the shared UI routes onto the provided mux.
func (r *UIRouter) Register(mux *http.ServeMux) {
	if r == nil || mux == nil {
		return
	}
	if r.Templates == nil {
		tmpl, err := template.ParseFS(r.TemplateFS, r.Pattern)
		if err != nil {
			mux.HandleFunc("/login", func(w http.ResponseWriter, req *http.Request) {
				http.Error(w, "template load failed", http.StatusInternalServerError)
			})
			mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
				http.Redirect(w, req, "/app", http.StatusFound)
			})
			return
		}
		r.Templates = tmpl
	}
	mux.HandleFunc("/", r.handleRoot)
	mux.HandleFunc("/login", r.handleLoginPage)
	mux.HandleFunc("/hq-login", r.handleHQLoginPage)
	mux.HandleFunc("/app", RequireAuth(r.handleIndex))
	mux.HandleFunc("/viewer", RequireAuth(r.handleViewer))
	mux.HandleFunc("/file-preview", RequireAuth(r.handleFilePreview))
}

func (r *UIRouter) handleRoot(w http.ResponseWriter, req *http.Request) {
	if HasConfigFileOnDisk() {
		r.handleLoginPage(w, req)
		return
	}
	http.Redirect(w, req, "/app", http.StatusFound)
}

func (r *UIRouter) handleLoginPage(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Templates == nil {
		http.Error(w, "templates not initialized", http.StatusInternalServerError)
		return
	}
	returnTo := req.URL.Query().Get("return")
	if returnTo == "" {
		returnTo = "/app"
	}
	data := map[string]any{
		"Version":   sop.Version,
		"ReturnTo":  returnTo,
		"HQBaseURL": strings.TrimSpace(os.Getenv("SOP_HQ_BASE_URL")),
	}
	if err := r.Templates.ExecuteTemplate(w, "login.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (r *UIRouter) handleHQLoginPage(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Templates == nil {
		http.Error(w, "templates not initialized", http.StatusInternalServerError)
		return
	}

	returnTo := req.URL.Query().Get("return")
	if returnTo == "" {
		returnTo = "/app"
	}

	data := map[string]any{
		"Version":   sop.Version,
		"ReturnTo":  returnTo,
		"HQBaseURL": strings.TrimSpace(os.Getenv("SOP_HQ_BASE_URL")),
	}
	if err := r.Templates.ExecuteTemplate(w, "hq_login.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (r *UIRouter) handleIndex(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Templates == nil {
		http.Error(w, "templates not initialized", http.StatusInternalServerError)
		return
	}

	hasDemo := false
	ctx := req.Context()
	cfg := GetConfig()
	for _, dbCfg := range cfg.Databases {
		if dbOpts, err := GetDBOptionsFromConfig(ctx, &dbCfg); err == nil {
			db := aidb.NewDatabase(dbOpts)
			if exists, err := db.StoreExists(ctx, "users"); err == nil && exists {
				hasDemo = true
				break
			}
		}
	}

	isEnterprise := cfg.SystemDB != nil
	if !isEnterprise && len(cfg.Databases) > 0 {
		isEnterprise = true
	}

	data := map[string]any{
		"Version":                 sop.Version,
		"Mode":                    cfg.Mode,
		"ModelCatalog":            GetModelCatalog(),
		"AllowInvalidMapKey":      os.Getenv("SOP_ALLOW_INVALID_MAP_KEY") == "true",
		"HasDemo":                 hasDemo,
		"LLMProvider":             "openai",
		"IsEnterprise":            isEnterprise,
		"SystemDBName":            SystemDBName,
		"ConfigFile":              cfg.ConfigFile,
		"SetupWizardAIConfigJSON": r.setupWizardAIConfigJSON(),
		"ModelCatalogJSON":        r.modelCatalogJSON(),
		"MinHashMod":              1,
		"MaxHashMod":              256,
		"CanManageEnvironments":   CanManageEnvironments(sop.GetAuthFromContext(req.Context())),
	}
	if err := r.Templates.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (r *UIRouter) handleViewer(w http.ResponseWriter, req *http.Request) {
	docID := req.URL.Query().Get("docID")
	if docID == "" {
		http.Error(w, "docID is required", http.StatusBadRequest)
		return
	}

	if isExternalDocID(docID) {
		http.Redirect(w, req, docID, http.StatusFound)
		return
	}

	dbName := req.URL.Query().Get("db")
	spaceName := req.URL.Query().Get("space")
	if dbName == "" || spaceName == "" {
		http.Error(w, "db and space parameters are required", http.StatusBadRequest)
		return
	}

	dbOpt, err := GetDBOptions(req.Context(), dbName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	dbInstance := aidb.NewDatabase(dbOpt)
	tx, err := dbInstance.BeginTransaction(req.Context(), sop.ForReading)
	if err != nil {
		http.Error(w, "failed to begin transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(req.Context())

	kb, err := dbInstance.OpenKnowledgeBase(req.Context(), spaceName, tx, nil, nil, true, false)
	if err != nil {
		http.Error(w, "failed to open knowledge base: "+err.Error(), http.StatusInternalServerError)
		return
	}

	uid, err := sop.ParseUUID(docID)
	if err != nil {
		http.Error(w, "invalid docID format", http.StatusBadRequest)
		return
	}

	docs, err := kb.Store.Documents(req.Context())
	if err != nil {
		http.Error(w, "failed to access document store: "+err.Error(), http.StatusInternalServerError)
		return
	}

	found, err := docs.Find(req.Context(), uid, false)
	if err != nil {
		http.Error(w, "document not found", http.StatusNotFound)
		return
	}
	if !found {
		http.Error(w, "document not found", http.StatusNotFound)
		return
	}

	documentVal, err := docs.GetCurrentValue(req.Context())
	if err != nil {
		http.Error(w, "failed to retrieve document content", http.StatusInternalServerError)
		return
	}

	if documentVal.URL != "" && documentVal.Content == "" && len(documentVal.Data) == 0 {
		redirectURL := documentVal.URL
		highlightText := req.URL.Query().Get("text")
		if highlightText != "" && !strings.Contains(redirectURL, "#") {
			if len(highlightText) > 60 {
				highlightText = highlightText[:60]
			}
			encodedText := url.QueryEscape(highlightText)
			encodedText = strings.ReplaceAll(encodedText, "+", "%20")
			redirectURL += "#:~:text=" + encodedText
		}
		http.Redirect(w, req, redirectURL, http.StatusFound)
		return
	}

	if documentVal.ContentType != "" {
		w.Header().Set("Content-Type", documentVal.ContentType)
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}

	w.WriteHeader(http.StatusOK)
	if len(documentVal.Data) > 0 {
		_, _ = w.Write(documentVal.Data)
	} else {
		_, _ = w.Write([]byte(documentVal.Content))
	}
}

func (r *UIRouter) handleFilePreview(w http.ResponseWriter, req *http.Request) {
	rawPath := strings.TrimSpace(req.URL.Query().Get("path"))
	cleanedPath, err := resolvePreviewPath(rawPath)
	if err != nil {
		http.Error(w, "invalid file path", http.StatusBadRequest)
		return
	}

	f, err := os.Open(cleanedPath)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.Error(w, "invalid file path", http.StatusBadRequest)
		return
	}

	contentType := http.DetectContentType([]byte{})
	if ext := strings.ToLower(filepath.Ext(cleanedPath)); ext != "" {
		if mimeType := mimeTypeByExtension(ext); mimeType != "" {
			contentType = mimeType
		}
	}
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, req, filepath.Base(cleanedPath), info.ModTime(), f)
}

func (r *UIRouter) setupWizardAIConfigJSON() template.JS {
	return template.JS("{}")
}

func (r *UIRouter) modelCatalogJSON() template.JS {
	catalog := GetModelCatalog()
	b, err := json.Marshal(catalog)
	if err != nil {
		return template.JS("{}")
	}
	return template.JS(b)
}

func mimeTypeByExtension(ext string) string {
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".json":
		return "application/json"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".pdf":
		return "application/pdf"
	default:
		return ""
	}
}

func resolvePreviewPath(rawPath string) (string, error) {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return "", os.ErrInvalid
	}
	if strings.HasPrefix(trimmed, "file://") {
		if parsed, err := url.Parse(trimmed); err == nil && parsed.Path != "" {
			trimmed = parsed.Path
		}
	}
	if strings.HasPrefix(trimmed, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		trimmed = filepath.Join(homeDir, strings.TrimPrefix(trimmed, "~/"))
	}
	candidatePath := trimmed
	if !filepath.IsAbs(candidatePath) {
		if cwd, err := os.Getwd(); err == nil {
			candidatePath = filepath.Join(cwd, candidatePath)
		}
	}
	cleanedPath := filepath.Clean(candidatePath)
	if cleanedPath == "." || strings.Contains(cleanedPath, "..") {
		return "", os.ErrInvalid
	}
	return cleanedPath, nil
}

func isExternalDocID(docID string) bool {
	trimmed := strings.TrimSpace(docID)
	return strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") || strings.HasPrefix(trimmed, "file://") || strings.HasPrefix(trimmed, "mailto:")
}

// sharedTemplateDir returns the shared UI template directory regardless of the current working directory.
func sharedTemplateDir() string {
	_, currentFile, _, ok := runtime.Caller(0)
	if ok {
		baseDir := filepath.Dir(currentFile)
		candidates := []string{
			filepath.Join(baseDir, "..", "httpserver", "templates"),
			filepath.Join(baseDir, "..", "..", "httpserver", "templates"),
			filepath.Join(baseDir, "..", "..", "..", "httpserver", "templates"),
		}
		for _, candidate := range candidates {
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate
			}
		}
	}
	return filepath.Join("..", "httpserver", "templates")
}

// osDirFS returns an os filesystem rooted at the provided path.
func osDirFS(path string) fs.FS {
	return os.DirFS(path)
}

// SharedUIAssets returns the embedded template filesystem used by the shared UI.
func SharedUIAssets() fs.FS {
	return osDirFS(sharedTemplateDir())
}

// RegisterSharedUIRoutes mounts the shared UI routing on the provided mux.
func RegisterSharedUIRoutes(mux *http.ServeMux) {
	router := NewUIRouter(SharedUIAssets(), "*.html")
	router.Register(mux)
}

// RegisterUIRoutes mounts the shared UI routing using the same registration shape.
func RegisterUIRoutes(mux *http.ServeMux) {
	RegisterSharedUIRoutes(mux)
}

// TemplatePattern returns the default template glob for shared UI assets.
func TemplatePattern() string {
	return "*.html"
}

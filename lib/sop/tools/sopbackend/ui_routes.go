package sopbackend

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sharedcode/sop"
	aidb "github.com/sharedcode/sop/ai/database"
)

// DefaultDaemonURL is the default local daemon endpoint.
// CRITICAL: This must match constants in JavaScript/TypeScript:
//   - templates/scripts_daemon.html
//   - templates/scripts_part01.html
//   - templates/scripts_part05.html
//   - src/frontend/src/App.tsx
const DefaultDaemonURL = "http://127.0.0.1:9090"

var (
	lookPathForDaemonUninstall = exec.LookPath
	runDaemonUninstallCommand  = func(name string, args ...string) *exec.Cmd {
		return exec.Command(name, args...)
	}
	daemonUninstallServiceUnitPaths = []string{
		"/etc/systemd/system/sop-daemon.service",
		"/lib/systemd/system/sop-daemon.service",
	}
	daemonUninstallBinaryPaths = []string{
		"/usr/local/bin/sop-daemon",
		"/opt/sop/bin/sop-daemon",
	}
)

// UIRouter serves the shared SOP web UI from the sopbackend package so both
// httpserver and daemon-based apps can reuse the same handlers and templates.
// Product/account flows such as registration, subscription updates, and unregister
// are handled by the site backend rather than by this shared UI layer.
type UIRouter struct {
	TemplateFS fs.FS
	Pattern    string
	Templates  *template.Template
}

// NewUIRouter builds a router that loads templates from the provided filesystem.
func NewUIRouter(templateFS fs.FS, pattern string) *UIRouter {
	router := &UIRouter{}
	if templateFS == nil {
		templateFS = osDirFS(sharedTemplateDir())
	}
	if pattern == "" {
		pattern = "*.html"
	}
	router.TemplateFS = templateFS
	router.Pattern = pattern
	_ = router.ensureTemplates()
	return router
}

func (r *UIRouter) ensureTemplates() error {
	if r == nil {
		return os.ErrInvalid
	}
	if r.Templates != nil {
		return nil
	}
	if r.TemplateFS == nil {
		r.TemplateFS = osDirFS(sharedTemplateDir())
	}
	if r.Pattern == "" {
		r.Pattern = "*.html"
	}
	tmpl, err := template.ParseFS(r.TemplateFS, r.Pattern)
	if err != nil {
		return err
	}
	r.Templates = tmpl
	return nil
}

// Register wires the shared platform UI routes onto the provided mux.
func (r *UIRouter) Register(mux *http.ServeMux) {
	r.RegisterWithOptions(mux, true)
}

// RegisterWithOptions wires the shared platform UI routes onto the provided mux.
// When includeRoot is false, the catch-all / handler is skipped so callers can mount
// the shared UI on a sub-app without conflicting with a separate root frontend handler.
func (r *UIRouter) RegisterWithOptions(mux *http.ServeMux, includeRoot bool) {
	if r == nil || mux == nil {
		return
	}
	if err := r.ensureTemplates(); err != nil {
		mux.HandleFunc("/login", func(w http.ResponseWriter, req *http.Request) {
			http.Error(w, "template load failed", http.StatusInternalServerError)
		})
		if includeRoot {
			mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
				http.Redirect(w, req, "/app", http.StatusFound)
			})
		}
		return
	}
	if includeRoot {
		mux.HandleFunc("/", r.handleRoot)
	}
	mux.HandleFunc("/login", r.handleLoginPage)
	mux.HandleFunc("/privacy/settings", RequireAuth(HandlePrivacySettingsPage))
	mux.HandleFunc("/privacy/audit", RequireAuth(HandlePrivacyAuditLogPage))
	mux.HandleFunc("/privacy/approval-queue", RequireAuth(HandlePrivacyApprovalQueuePage))
	mux.HandleFunc("/privacy/dashboard", RequireAuth(HandlePrivacyDashboardPage))
	mux.HandleFunc("/privacy/advanced", RequireAuth(HandlePrivacyAdvancedPage))
	mux.HandleFunc("/hq-login", r.handleHQLoginPage)
	mux.HandleFunc("/app", RequireAuth(r.handleIndex))
	mux.HandleFunc("/app/subscribe", RequireAuth(r.handleSubscribe))
	mux.HandleFunc("/app/unsubscribe", RequireAuth(r.handleUnsubscribe))
	mux.HandleFunc("/app/unregister", RequireAuth(r.handleUnregister))
	mux.HandleFunc("/app/hq", RequireAuth(r.handleHQPreview))
	mux.HandleFunc("/logout", r.handleLogout)
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

func (r *UIRouter) executeTemplateWithFallback(w http.ResponseWriter, data map[string]any, names ...string) error {
	if r == nil || r.Templates == nil {
		return os.ErrNotExist
	}
	for _, name := range names {
		if name == "" || r.Templates.Lookup(name) == nil {
			continue
		}
		if err := r.Templates.ExecuteTemplate(w, name, data); err != nil {
			return err
		}
		return nil
	}
	return os.ErrNotExist
}

func (r *UIRouter) handleLoginPage(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Templates == nil {
		http.Error(w, "templates not initialized", http.StatusInternalServerError)
		return
	}
	returnTo := req.URL.Query().Get("return")
	defaultReturnURL := "/app"
	if provisioningCompleteCache.Load() {
		defaultReturnURL = "/site/"
	}
	if returnTo == "" {
		returnTo = defaultReturnURL
	}
	data := map[string]any{
		"Version":          sop.Version,
		"ReturnTo":         returnTo,
		"DefaultReturnURL": defaultReturnURL,
		"HQBaseURL":        strings.TrimSpace(os.Getenv("SOP_HQ_BASE_URL")),
	}
	if err := r.executeTemplateWithFallback(w, data, "login.html", "index.html"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (r *UIRouter) handleHQLoginPage(w http.ResponseWriter, req *http.Request) {
	returnTo := req.URL.Query().Get("return")
	if returnTo == "" {
		returnTo = "/app"
	}

	loginURL := "/login?return=" + url.QueryEscape(returnTo)
	if message := req.URL.Query().Get("message"); message != "" {
		loginURL += "&message=" + url.QueryEscape(message)
	}
	http.Redirect(w, req, loginURL, http.StatusFound)
}

// runtimeSwitchEnabledForContext is the single RBAC gate for the chat runtime toggle.
// It intentionally uses the policy layer rather than feature flags so the UI stays aligned
// with the same authorization rules that govern the rest of the app.
func runtimeSwitchEnabledForContext(ctx context.Context) bool {
	auth := sop.GetAuthFromContext(ctx)
	if auth.IsSystem {
		return true
	}
	for _, role := range auth.Roles {
		if sop.IsAdminRole(role) {
			return true
		}
	}
	if userID := strings.TrimSpace(auth.UserID); userID != "" {
		if strings.EqualFold(userID, "root") {
			return true
		}
		cfg := GetConfig()
		if user, ok, err := cfg.findUserRecord(ctx, userID); err == nil && ok && user != nil {
			if sop.IsAdminRole(user.Role) || strings.EqualFold(user.Role, sop.RoleSystemAdmin) {
				return true
			}
		}
	}
	return sop.Authorize(ctx, sop.ResourceAccess{
		Visibility: sop.VisibilityPrivate,
		Roles: map[string][]string{
			sop.RoleSystemAdmin: {string(sop.ActionWrite)},
			sop.RoleAdmin:       {string(sop.ActionWrite)},
		},
	}, sop.ActionWrite)
}

func (r *UIRouter) handleIndex(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Templates == nil {
		http.Error(w, "templates not initialized", http.StatusInternalServerError)
		return
	}

	data := r.landingPageData(req)
	if err := r.executeTemplateWithFallback(w, data, "landing.html", "index.html"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (r *UIRouter) handleSubscribe(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Templates == nil {
		http.Error(w, "templates not initialized", http.StatusInternalServerError)
		return
	}

	if req.Method == http.MethodPost {
		if err := req.ParseForm(); err != nil {
			http.Error(w, "invalid form submission", http.StatusBadRequest)
			return
		}
		cardNumber := strings.TrimSpace(req.Form.Get("card_number"))
		cardName := strings.TrimSpace(req.Form.Get("card_name"))
		cardExpiry := strings.TrimSpace(req.Form.Get("card_expiry"))
		cardCVC := strings.TrimSpace(req.Form.Get("card_cvc"))
		if cardNumber == "" || cardName == "" || cardExpiry == "" || cardCVC == "" {
			http.Error(w, "payment details are required", http.StatusBadRequest)
			return
		}

		auth := sop.GetAuthFromContext(req.Context())
		if userID := strings.TrimSpace(auth.UserID); userID != "" {
			_, _ = config.UpdateSubscriptionPlan(req.Context(), userID, "starter")
		}

		data := r.landingPageData(req)
		data["SubscriptionSuccess"] = true
		data["ShowDownloadStep"] = true
		data["SubscriptionMessage"] = "Your Starter Kit trial is active. We will only charge your card after the first month ends, then $5/month thereafter."
		if err := r.executeTemplateWithFallback(w, data, "landing.html", "index.html"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	data := r.landingPageData(req)
	data["ShowSubscribeForm"] = true
	if err := r.executeTemplateWithFallback(w, data, "landing.html", "index.html"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (r *UIRouter) handleUnsubscribe(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Templates == nil {
		http.Error(w, "templates not initialized", http.StatusInternalServerError)
		return
	}

	auth := sop.GetAuthFromContext(req.Context())
	if userID := strings.TrimSpace(auth.UserID); userID != "" {
		_, _ = config.UpdateSubscriptionPlan(req.Context(), userID, "none")
	}
	if err := uninstallLocalDaemonService(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to uninstall local daemon during unsubscribe: %v\n", err)
	}

	data := r.landingPageData(req)
	data["SubscriptionMessage"] = "You have been unsubscribed. You can reactivate the Starter Kit at any time."
	data["IsUnsubscribed"] = true
	if err := r.executeTemplateWithFallback(w, data, "landing.html", "index.html"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (r *UIRouter) handleUnregister(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	auth := sop.GetAuthFromContext(req.Context())
	if userID := strings.TrimSpace(auth.UserID); userID != "" {
		_ = config.UnregisterUser(req.Context(), userID)
	}
	if err := uninstallLocalDaemonService(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to uninstall local daemon during unregister: %v\n", err)
	}
	clearSessionCookies(w)
	http.Redirect(w, req, "/login?message=unregistered", http.StatusFound)
}

func (r *UIRouter) handleHQPreview(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Templates == nil {
		http.Error(w, "templates not initialized", http.StatusInternalServerError)
		return
	}

	auth := sop.GetAuthFromContext(req.Context())
	if !isAdminAuthContext(auth) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	data := map[string]any{
		"Version":          sop.Version,
		"Username":         strings.TrimSpace(auth.UserID),
		"CurrentRole":      strings.Join(auth.Roles, ", "),
		"IsAdmin":          true,
		"CurrentPlan":      "hq-local-agent",
		"CurrentPlanLabel": "HQ + Local Agent",
	}
	if err := r.executeTemplateWithFallback(w, data, "hq_preview.html", "index.html"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func isAdminAuthContext(auth sop.AuthContext) bool {
	for _, role := range auth.Roles {
		if sop.IsAdminRole(role) || strings.EqualFold(role, sop.RoleSystemAdmin) {
			return true
		}
	}
	return auth.IsSystem || strings.EqualFold(strings.TrimSpace(auth.UserID), "root")
}

func (r *UIRouter) handleLogout(w http.ResponseWriter, req *http.Request) {
	clearSessionCookies(w)
	http.Redirect(w, req, "/login", http.StatusFound)
}

func clearSessionCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "sop_access_token", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: "sop_refresh_token", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func uninstallLocalDaemonService() error {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return nil
	}

	if _, err := lookPathForDaemonUninstall("systemctl"); err == nil {
		for _, args := range [][]string{{"stop", "sop-daemon.service"}, {"disable", "sop-daemon.service"}, {"daemon-reload"}} {
			_ = runDaemonUninstallCommand("systemctl", args...).Run()
		}
	}

	var cleanupErr error
	for _, path := range daemonUninstallServiceUnitPaths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) && cleanupErr == nil {
			cleanupErr = err
		}
	}
	for _, path := range daemonUninstallBinaryPaths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) && cleanupErr == nil {
			cleanupErr = err
		}
	}
	return cleanupErr
}

func (r *UIRouter) landingPageData(req *http.Request) map[string]any {
	cfg := GetConfig()
	auth := sop.GetAuthFromContext(req.Context())
	username := strings.TrimSpace(auth.UserID)
	if username == "" {
		username = "there"
	}

	currentPlan := "starter"
	currentRole := ""
	if len(auth.Roles) > 0 {
		currentRole = strings.TrimSpace(auth.Roles[0])
	}
	if user, ok, err := cfg.findUserRecord(req.Context(), username); err == nil && ok && user != nil {
		if strings.TrimSpace(user.SubscriptionPlan) != "" {
			currentPlan = strings.TrimSpace(user.SubscriptionPlan)
		}
		if strings.TrimSpace(user.Role) != "" {
			currentRole = strings.TrimSpace(user.Role)
		}
	}

	isAdmin := false
	for _, role := range auth.Roles {
		if sop.IsAdminRole(role) || strings.EqualFold(role, sop.RoleSystemAdmin) {
			isAdmin = true
			break
		}
	}
	if strings.EqualFold(username, "root") || auth.IsSystem {
		isAdmin = true
	}

	planLabel := "Starter Kit"
	planDescription := "Everything you need to get started with SOP, including access to the core workspace experience."
	if strings.EqualFold(currentPlan, "none") || strings.TrimSpace(currentPlan) == "" {
		planLabel = "No active plan"
		planDescription = "You are currently unsubscribed. You can start the Starter Kit trial again whenever you are ready."
	} else if strings.EqualFold(currentPlan, "starter") || strings.EqualFold(currentPlan, "starter kit") {
		planLabel = "Starter Kit"
		planDescription = "Your current plan gives you access to the core SOP experience with one starter workspace."
	} else {
		planLabel = strings.Title(strings.ToLower(currentPlan))
		planDescription = "This plan is available for you today. More options will be added soon."
	}

	availablePlans := []map[string]string{{
		"name":        "Starter Kit",
		"slug":        "starter",
		"description": "The only plan available at launch, designed for getting started quickly.",
		"current":     "true",
		"disabled":    "false",
	}, {
		"name":        "HQ + Local Agent",
		"slug":        "hq-local-agent",
		"description": "A richer subscription for the HQ experience and local agent workflow, coming online soon.",
		"current":     "false",
		"disabled":    "true",
	}}

	adminCapabilities := []map[string]string{}
	if isAdmin {
		adminCapabilities = []map[string]string{
			{"title": "View all registered users", "description": "See the current user roster, account state, and role assignments.", "href": "/admin/users"},
			{"title": "Manage user roles", "description": "Adjust access and permissions for teammates and admins.", "href": "/admin/users"},
			{"title": "Review account health", "description": "Monitor the account setup and subscription status from one place.", "href": "/app"},
		}
	}

	// Only users with the corresponding RBAC permission should be able to toggle between HQ and local workflow.
	runtimeSwitchEnabled := runtimeSwitchEnabledForContext(req.Context())

	// Setup wizard detection: only pass ConfigFile if it actually exists on disk
	configFile := ""
	if cfg.ConfigFile != "" && HasConfigFileOnDisk() {
		configFile = cfg.ConfigFile
	}

	// ========================================================================
	// DAEMON MODE CONFIGURATION
	// ========================================================================
	// When ?mode=daemon query parameter is present, the Data Manager UI should
	// connect to the local daemon (http://127.0.0.1:9090) instead of HQ.
	//
	// This is used by the "AI Studio" button to launch Data Manager in daemon mode.
	//
	// Implementation:
	//   1. Check for mode=daemon query parameter
	//   2. Set DaemonRuntimeEnabled = true (enables daemon auth bootstrap)
	//   3. Set DefaultRuntimeMode = "local" (JavaScript defaults to daemon)
	//   4. Set DaemonURL to custom URL if provided, else DefaultDaemonURL
	//
	// Template Effects (index.html):
	//   {{if .DaemonRuntimeEnabled}}
	//     localStorage.setItem('sop_chat_runtime', 'local');
	//     localStorage.setItem('sop_daemon_url', '{{.DaemonURL}}');
	//     window.SOP_DAEMON_BASE_URL = '{{.DaemonURL}}';
	//   {{end}}
	//
	// JavaScript Effects (scripts_daemon.html):
	//   - Auto-bootstrap on DOMContentLoaded
	//   - Uses site session token as bootstrap_token
	//   - Falls back to bootstrap secret if no session token
	//
	// CRITICAL: Do not change DefaultDaemonURL constant without updating:
	//   - scripts_daemon.html (DEFAULT_DAEMON_URL)
	//   - scripts_part01.html (DEFAULT_DAEMON_URL)
	//   - scripts_part05.html (DEFAULT_DAEMON_URL)
	//   - src/frontend/src/App.tsx (DEFAULT_DAEMON_URL)
	//
	// See: docs/AI_STUDIO_DAEMON_MODE.md for complete specification
	// ========================================================================
	daemonRuntimeEnabled := false
	daemonURL := DefaultDaemonURL
	defaultRuntimeMode := "hq"
	if mode := strings.TrimSpace(req.URL.Query().Get("mode")); mode == "daemon" {
		daemonRuntimeEnabled = true
		defaultRuntimeMode = "local"
		// Allow custom daemon URL via query parameter
		if customURL := strings.TrimSpace(req.URL.Query().Get("daemon_url")); customURL != "" {
			daemonURL = customURL
		}
	}

	return map[string]any{
		"Version":                 sop.Version,
		"Username":                username,
		"CurrentPlan":             currentPlan,
		"CurrentPlanLabel":        planLabel,
		"CurrentPlanDescription":  planDescription,
		"AvailablePlans":          availablePlans,
		"CurrentRole":             currentRole,
		"IsAdmin":                 isAdmin,
		"AdminCapabilities":       adminCapabilities,
		"ShowSubscribeForm":       false,
		"ShowDownloadStep":        false,
		"DaemonURL":               daemonURL,
		"SubscriptionSuccess":     false,
		"SubscriptionMessage":     "",
		"IsUnsubscribed":          false,
		"RuntimeSwitchEnabled":    runtimeSwitchEnabled,
		"DaemonRuntimeEnabled":    daemonRuntimeEnabled,
		"ConfigFile":              configFile,
		"DefaultRuntimeMode":      defaultRuntimeMode,
		"SetupWizardAIConfigJSON": r.setupWizardAIConfigJSON(),
		"ModelCatalogJSON":        r.modelCatalogJSON(),
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
			filepath.Join(baseDir, "templates"),
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
	return filepath.Join("templates")
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

// RegisterUIRoutesWithoutRoot mounts the shared UI routing without the root / redirect handler.
func RegisterUIRoutesWithoutRoot(mux *http.ServeMux) {
	router := NewUIRouter(SharedUIAssets(), "*.html")
	router.RegisterWithOptions(mux, false)
}

// TemplatePattern returns the default template glob for shared UI assets.
func TemplatePattern() string {
	return "*.html"
}

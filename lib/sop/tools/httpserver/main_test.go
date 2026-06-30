package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/tools/sopbackend"
)

func TestDefaultRuntimeAutomationPolicyDisablesShellCommands(t *testing.T) {
	policy := defaultRuntimeAutomationPolicy()
	if policy.Enabled {
		t.Fatal("expected default runtime automation policy to stay disabled")
	}
	if policy.AllowShellCommands {
		t.Fatal("expected default runtime automation policy to disable shell commands")
	}
}

func TestApplyRedisOverridePropagatesToConfig(t *testing.T) {
	cfg := sopbackend.Config{
		RedisURL:  "old-host:6379",
		SystemDB:  &sopbackend.DatabaseConfig{RedisURL: "old-host:6379"},
		Databases: []sopbackend.DatabaseConfig{{RedisURL: "old-host:6379"}},
	}

	applyRedisOverride(&cfg, "localhost:6379", true)

	if cfg.RedisURL != "localhost:6379" {
		t.Fatalf("expected global Redis URL override to be applied, got %q", cfg.RedisURL)
	}
	if cfg.SystemDB == nil || cfg.SystemDB.RedisURL != "localhost:6379" {
		t.Fatalf("expected system DB Redis URL override to be applied")
	}
	if len(cfg.Databases) != 1 || cfg.Databases[0].RedisURL != "localhost:6379" {
		t.Fatalf("expected database Redis URL override to be applied")
	}
}

func TestRuntimeSwitchIsDisabledForStarterPlan(t *testing.T) {
	originalCfg := sopbackend.GetConfig()
	t.Cleanup(func() { sopbackend.SetConfig(originalCfg) })
	sopbackend.SetConfig(sopbackend.Config{Users: []sopbackend.UserRecord{{Username: "alice", SubscriptionPlan: "starter"}}})

	req := httptest.NewRequest("GET", "/app", nil)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "alice", Roles: []string{sop.RoleUser}}))

	if runtimeSwitchEnabled(req) {
		t.Fatal("expected starter plan to disable runtime switching")
	}
	if got := defaultRuntimeModeForRequest(req); got != "local" {
		t.Fatalf("expected starter plan to default to local runtime, got %q", got)
	}
}

func TestUnauthenticatedRequestsDefaultToHQRuntime(t *testing.T) {
	req := httptest.NewRequest("GET", "/app", nil)

	if !runtimeSwitchEnabled(req) {
		t.Fatal("expected unauthenticated requests to enable HQ runtime by default")
	}
	if got := defaultRuntimeModeForRequest(req); got != "hq" {
		t.Fatalf("expected unauthenticated requests to default to HQ runtime, got %q", got)
	}
}

func TestRuntimeSwitchRequiresSubscribedHQFeature(t *testing.T) {
	originalCfg := sopbackend.GetConfig()
	t.Cleanup(func() { sopbackend.SetConfig(originalCfg) })
	sopbackend.SetConfig(sopbackend.Config{Users: []sopbackend.UserRecord{{Username: "alice", SubscriptionPlan: "hq-local-agent"}}})

	req := httptest.NewRequest("GET", "/app", nil)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "alice", Roles: []string{sop.RoleUser}}))

	if runtimeSwitchEnabled(req) {
		t.Fatal("expected runtime switching to require the subscribed HQ feature")
	}
	if got := defaultRuntimeModeForRequest(req); got != "local" {
		t.Fatalf("expected runtime without the HQ feature to default to local runtime, got %q", got)
	}
}

func TestRuntimeSwitchUsesSubscribedFeatureList(t *testing.T) {
	originalCfg := sopbackend.GetConfig()
	t.Cleanup(func() { sopbackend.SetConfig(originalCfg) })
	sopbackend.SetConfig(sopbackend.Config{Users: []sopbackend.UserRecord{{Username: "alice", SubscriptionPlan: "starter", SubscriptionFeatures: []string{"hq_runtime"}}}})

	req := httptest.NewRequest("GET", "/app", nil)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "alice", Roles: []string{sop.RoleUser}}))

	if !runtimeSwitchEnabled(req) {
		t.Fatal("expected subscribed runtime feature to enable runtime switching")
	}
	if got := defaultRuntimeModeForRequest(req); got != "hq" {
		t.Fatalf("expected subscribed runtime feature to default to HQ runtime, got %q", got)
	}
}

func TestHandleIndexRendersRuntimeSwitchForSubscribedHQFeature(t *testing.T) {
	originalCfg := sopbackend.GetConfig()
	t.Cleanup(func() { sopbackend.SetConfig(originalCfg) })
	sopbackend.SetConfig(sopbackend.Config{Users: []sopbackend.UserRecord{{Username: "alice", SubscriptionFeatures: []string{"hq_runtime"}}}})

	req := httptest.NewRequest("GET", "/app", nil)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "alice", Roles: []string{sop.RoleUser}}))
	rec := httptest.NewRecorder()

	handleIndex(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleIndex() status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `id="chat-runtime-hq"`) {
		t.Fatalf("expected HQ runtime checkbox to be rendered for subscribed feature, got body=%s", rec.Body.String())
	}
}

func TestShouldShowEnvironmentManagerVisibilityRules(t *testing.T) {
	defer sopbackend.SetDisableAuth(false)

	sopbackend.SetDisableAuth(false)
	plainReq := httptest.NewRequest("GET", "/app", nil)
	if !shouldShowEnvironmentManager(plainReq) {
		t.Fatalf("expected default requests to keep environment manager visible")
	}

	sopbackend.SetDisableAuth(true)
	localAgentReq := httptest.NewRequest("GET", "/app", nil)
	if shouldShowEnvironmentManager(localAgentReq) {
		t.Fatalf("expected disabled-auth/local-agent mode to hide environment manager")
	}

	deniedReq := httptest.NewRequest("GET", "/app", nil)
	deniedReq = deniedReq.WithContext(sop.ContextWithAuth(deniedReq.Context(), sop.AuthContext{Permissions: map[string]bool{"manage_environments": false}}))
	if shouldShowEnvironmentManager(deniedReq) {
		t.Fatalf("expected explicit permission deny to hide environment manager")
	}
}

func TestFloaterUsesSharedAppShell(t *testing.T) {
	scriptContent, err := os.ReadFile("templates/scripts_part05.html")
	if err != nil {
		t.Fatalf("failed to read chat script template: %v", err)
	}
	if !strings.Contains(string(scriptContent), "/app") {
		t.Fatalf("expected floater to open the shared app shell")
	}
	if !strings.Contains(string(scriptContent), "chatFloater") {
		t.Fatalf("expected floater to open the shared app shell in floater mode")
	}
}

func TestFloaterScriptHandlesInputStateAndMainWindowHide(t *testing.T) {
	scriptContent, err := os.ReadFile("templates/scripts_part05.html")
	if err != nil {
		t.Fatalf("failed to read chat script template: %v", err)
	}
	content := string(scriptContent)
	if !strings.Contains(content, "function handleChatInput") {
		t.Fatalf("expected floater script to manage input-driven send-button state")
	}
	if !strings.Contains(content, "widget.classList.remove('active')") {
		t.Fatalf("expected floater launch to hide the main chat widget")
	}
	if !strings.Contains(content, "widget.style.display = 'none'") {
		t.Fatalf("expected floater launch to hide the main chat widget")
	}
	if strings.Contains(content, "Undo floater and return to the main window") {
		t.Fatalf("expected detached floater mode to avoid custom undo/restore behavior")
	}
	if strings.Contains(content, "function closeCurrentChatFloater") {
		t.Fatalf("expected detached floater mode to avoid custom close/restore behavior")
	}
	if !strings.Contains(content, "button.disabled = false") {
		t.Fatalf("expected main-window launcher to remain enabled after floater close")
	}
	if !strings.Contains(content, "button.style.display = isFloaterMode ? 'none' : (isLocalRuntime ? 'inline-flex' : 'none')") {
		t.Fatalf("expected floater mode to hide the launcher button in the detached window")
	}
	if !strings.Contains(content, "resetButton.style.display = isFloaterMode ? 'none' : 'inline-flex'") {
		t.Fatalf("expected floater mode to hide the reset control in the detached window")
	}
	if !strings.Contains(content, "minimizeButton.style.display = isFloaterMode ? 'none' : 'inline-flex'") {
		t.Fatalf("expected floater mode to hide the minimize control in the detached window")
	}
	if !strings.Contains(content, "Opening the chat route in a fresh tab instead") {
		t.Fatalf("expected the floater fallback to avoid popup windows that show a back-to-tab control")
	}
	if !strings.Contains(content, "window.open(floaterUrl.toString(), '_blank'") {
		t.Fatalf("expected the launcher fallback to open a fresh browser tab or window")
	}
	if !strings.Contains(content, "sop_chat_floater_cleanup") {
		t.Fatalf("expected the detached window to signal the main window to refresh on close")
	}
	if !strings.Contains(content, "localStorage.setItem(cleanupSignal") {
		t.Fatalf("expected the detached window to persist a cleanup signal for the main window")
	}
	if !strings.Contains(content, "window.addEventListener('storage'") {
		t.Fatalf("expected the main window to listen for the floater cleanup signal")
	}
	if strings.Contains(content, "setDashboardVisibility(false)") {
		t.Fatalf("expected floater mode to leave the main dashboard launcher behavior intact")
	}
}

func TestFloaterRendersToolInventoryAsPlainText(t *testing.T) {
	scriptContent, err := os.ReadFile("templates/scripts_part05.html")
	if err != nil {
		t.Fatalf("failed to read chat script template: %v", err)
	}
	content := string(scriptContent)
	if !strings.Contains(content, "function isToolInventoryContent") {
		t.Fatalf("expected chat stream renderer to detect tool inventory content")
	}
	if !strings.Contains(content, "### Available Tools") {
		t.Fatalf("expected tool inventory rendering logic to recognize the shared inventory header")
	}
}

func TestFloaterUsesSharedRuntimeResponseHandler(t *testing.T) {
	scriptContent, err := os.ReadFile("templates/scripts_part05.html")
	if err != nil {
		t.Fatalf("failed to read chat script template: %v", err)
	}
	content := string(scriptContent)
	if !strings.Contains(content, "function handleRuntimeResponse") {
		t.Fatalf("expected shared runtime response handler in chat script")
	}
	if !strings.Contains(content, "await handleRuntimeResponse(response, loadingId, 'local')") {
		t.Fatalf("expected local runtime branch to call the shared runtime response handler")
	}
	if !strings.Contains(content, "await handleRuntimeResponse(response, loadingId, 'hq')") {
		t.Fatalf("expected remote runtime branch to call the shared runtime response handler")
	}
}

func TestFloaterUsesUnifiedRuntimeRequestAdapter(t *testing.T) {
	scriptContent, err := os.ReadFile("templates/scripts_part05.html")
	if err != nil {
		t.Fatalf("failed to read chat script template: %v", err)
	}
	content := string(scriptContent)
	if !strings.Contains(content, "function buildRuntimeRequestConfig") {
		t.Fatalf("expected a shared runtime request adapter in chat script")
	}
	if !strings.Contains(content, "function executeRuntimeRequest") {
		t.Fatalf("expected a shared runtime executor in chat script")
	}
	if !strings.Contains(content, "const runtimeRequest = buildRuntimeRequestConfig") {
		t.Fatalf("expected sendChatMessage to use the shared runtime adapter")
	}
}

func TestFloaterHeaderControlsStayVisibleInNarrowWindows(t *testing.T) {
	stylesContent, err := os.ReadFile("templates/styles.html")
	if err != nil {
		t.Fatalf("failed to read styles template: %v", err)
	}
	content := string(stylesContent)
	if !strings.Contains(content, "body.sop-chat-floater-mode .chat-header") {
		t.Fatalf("expected floater mode to provide compact header layout rules")
	}
	if !strings.Contains(content, "flex: 1 1 auto") {
		t.Fatalf("expected floater header controls to use elastic stretch behavior like the original AI window")
	}
	if !strings.Contains(content, "overflow: visible") {
		t.Fatalf("expected floater header controls to remain visible instead of clipping off-screen")
	}
	if !strings.Contains(content, "body.sop-chat-floater-mode .chat-header-controls") {
		t.Fatalf("expected floater mode to override the mobile header-control hiding rule")
	}
}

func TestFloaterIsLocalRuntimeOnly(t *testing.T) {
	modalContent, err := os.ReadFile("templates/modals.html")
	if err != nil {
		t.Fatalf("failed to read modals template: %v", err)
	}
	if !strings.Contains(string(modalContent), `id="chat-floater-btn"`) {
		t.Fatalf("expected floater button in modal template")
	}

	scriptContent, err := os.ReadFile("templates/scripts_part05.html")
	if err != nil {
		t.Fatalf("failed to read chat script template: %v", err)
	}
	if !strings.Contains(string(scriptContent), "Floater is only available in local runtime") {
		t.Fatalf("expected local-runtime-only floater guard in chat script template")
	}
	if !strings.Contains(string(scriptContent), "chatFloater") {
		t.Fatalf("expected floater to open the dedicated chat page")
	}
}

func TestHTTPServerDoesNotServeSiteFrontendRoutes(t *testing.T) {
	mux := http.NewServeMux()

	req := httptest.NewRequest(http.MethodGet, "/site/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected httpserver to leave /site/ unhandled, got %d", rec.Code)
	}
}

func TestRegisterLegacyDataManagerRoutesUsesLegacyUIHandlers(t *testing.T) {
	mux := http.NewServeMux()
	registerLegacyDataManagerRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatalf("expected legacy Data Manager route /app to be registered")
	}
}

func TestLegacyHTTPServerDoesNotRegisterSiteBackendRoutes(t *testing.T) {
	mux := http.NewServeMux()

	req := httptest.NewRequest(http.MethodGet, "/api/site/auth/login", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected legacy httpserver mux to leave site API routes unhandled, got %d", rec.Code)
	}
}

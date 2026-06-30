package main

import (
	"bytes"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

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

func TestEnvironmentManagerVisibilityFollowsHQCheckboxState(t *testing.T) {
	tmpl, err := template.ParseFS(content, "templates/scripts_part01.html")
	if err != nil {
		t.Fatalf("failed to parse scripts_part01 template: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "scripts_part01", map[string]any{"RuntimeSwitchEnabled": true}); err != nil {
		t.Fatalf("failed to execute scripts_part01 template: %v", err)
	}

	rendered := buf.String()
	if !strings.Contains(rendered, "const checkbox = document.getElementById('chat-runtime-hq');") {
		t.Fatalf("expected environment manager visibility to read the HQ checkbox state, got content=%s", rendered)
	}
	if !strings.Contains(rendered, "const shouldShow = Boolean(checkbox && checkbox.checked) || (!checkbox && getCurrentRuntimeMode() === 'hq');") {
		t.Fatalf("expected environment manager visibility to show when the HQ checkbox is checked, got content=%s", rendered)
	}
}

func TestSetupWizardUsesSystemDBNameForDefaultSystemPath(t *testing.T) {
	content, err := os.ReadFile("templates/index.html")
	if err != nil {
		t.Fatalf("failed to read setup wizard template: %v", err)
	}

	text := string(content)
	if strings.Contains(text, `value="/tmp/sop_data/system_db"`) {
		t.Fatalf("expected setup wizard to use the concrete system DB default for the system path, got template contents: %s", text)
	}
	if !strings.Contains(text, `value="/tmp/sop_data/{{.SystemDBName}}"`) {
		t.Fatalf("expected setup wizard template to render the template-driven system DB default path")
	}
}

func TestSetupWizardPreloadUsesResolvedSystemDBName(t *testing.T) {
	part10Content, err := os.ReadFile("templates/scripts_part10.html")
	if err != nil {
		t.Fatalf("failed to read setup wizard preload script: %v", err)
	}
	if !strings.Contains(string(part10Content), "database_name: systemDbNameForPreload") {
		t.Fatalf("expected setup wizard preload script to derive and send the system DB name in the preload payload")
	}

	part11Content, err := os.ReadFile("templates/scripts_part11.html")
	if err != nil {
		t.Fatalf("failed to read template preload script: %v", err)
	}
	if !strings.Contains(string(part11Content), "database_name: SYSTEM_DB_NAME") {
		t.Fatalf("expected template preload script to send the system DB name in the preload payload")
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

func TestDaemonBootstrapScriptSkipsAutoBootstrapInHQMode(t *testing.T) {
	scriptContent, err := os.ReadFile("templates/scripts_daemon.html")
	if err != nil {
		t.Fatalf("failed to read daemon script template: %v", err)
	}
	content := string(scriptContent)
	if !strings.Contains(content, "const runtimeMode = (typeof getCurrentRuntimeMode === 'function' ? getCurrentRuntimeMode() : 'hq');") {
		t.Fatalf("expected daemon bootstrap script to check runtime mode before auto-bootstrap, got content=%s", content)
	}
	if !strings.Contains(content, "if (runtimeMode !== 'local') {") {
		t.Fatalf("expected daemon bootstrap script to skip auto-bootstrap outside local runtime mode, got content=%s", content)
	}
}

func TestWelcomeContextScriptSkipsDaemonHandoffOutsideLocalRuntime(t *testing.T) {
	scriptContent, err := os.ReadFile("templates/scripts_part05.html")
	if err != nil {
		t.Fatalf("failed to read chat script template: %v", err)
	}
	content := string(scriptContent)
	if !strings.Contains(content, "async function requestRuntimeWelcomeContext(runtimeMode)") {
		t.Fatalf("expected welcome-context loader to be present in the chat script")
	}
	if !strings.Contains(content, "if (runtimeMode !== 'local') {\n                return null;") {
		t.Fatalf("expected welcome-context loader to skip daemon handoff outside local runtime mode")
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

func TestLegacyHTTPServerDoesNotRegisterSiteBackendRoutes(t *testing.T) {
	mux := http.NewServeMux()

	req := httptest.NewRequest(http.MethodGet, "/api/site/auth/login", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected legacy httpserver mux to leave site API routes unhandled, got %d", rec.Code)
	}
}

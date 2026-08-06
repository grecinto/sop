package sopbackend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sharedcode/sop"
)

type testAuthProvider struct {
	name string
}

func (p *testAuthProvider) Name() string { return p.name }

func (p *testAuthProvider) Authenticate(ctx context.Context, username, password string) (bool, *UserRecord, error) {
	if username == "alice" && password == "pw" {
		return true, &UserRecord{Username: "alice", Role: sop.RoleUser}, nil
	}
	return false, nil, nil
}

func TestNewSessionStoreUsesGlobalStoreName(t *testing.T) {
	s := NewSessionStore(time.Minute)
	if s.storeName != sessionStoreName {
		t.Fatalf("NewSessionStore().storeName = %q, want %q", s.storeName, sessionStoreName)
	}
}

func TestDefaultSessionTTLIsOneHour(t *testing.T) {
	if got, want := defaultSessionTTL, time.Hour; got != want {
		t.Fatalf("defaultSessionTTL = %v, want %v", got, want)
	}
}

func TestDefaultRefreshTTLIsTwoWeeks(t *testing.T) {
	if got, want := defaultRefreshTTL, 14*24*time.Hour; got != want {
		t.Fatalf("defaultRefreshTTL = %v, want %v", got, want)
	}
}

func TestSessionStoreValidationUsesNoCheckTransaction(t *testing.T) {
	store := NewSessionStore(time.Minute)
	ctx := context.Background()

	_, tx, err := store.getStoreWithMode(ctx, sop.NoCheck)
	if err != nil {
		t.Fatalf("getStoreWithMode() error = %v", err)
	}
	defer tx.Rollback(ctx)

	if tx == nil {
		t.Fatal("getStoreWithMode() returned nil transaction")
	}
	if got := tx.GetPhasedTransaction().GetMode(); got != sop.NoCheck {
		t.Fatalf("transaction mode = %v, want %v", got, sop.NoCheck)
	}
}

func TestSessionStoreGCResumesFromLastProcessedKey(t *testing.T) {
	store := NewSessionStore(time.Hour)
	ctx := context.Background()

	storeTxn, tx, err := store.getStore(ctx)
	if err != nil {
		t.Fatalf("getStore() error = %v", err)
	}
	defer tx.Rollback(ctx)

	for {
		ok, err := storeTxn.First(ctx)
		if err != nil {
			t.Fatalf("First() error while clearing existing sessions: %v", err)
		}
		if !ok {
			break
		}
		if _, err := storeTxn.RemoveCurrentItem(ctx); err != nil {
			t.Fatalf("RemoveCurrentItem() error while clearing existing sessions: %v", err)
		}
	}

	for _, key := range []string{"k1", "k2", "k3", "k4"} {
		record := SessionRecord{
			Token:            key,
			RefreshToken:     key + "-refresh",
			Username:         "alice",
			Role:             sop.RoleUser,
			IssuedAt:         time.Now().Add(-2 * time.Hour),
			ExpiresAt:        time.Now().Add(-time.Hour),
			RefreshExpiresAt: time.Now().Add(-time.Hour),
		}
		if _, err := storeTxn.Add(ctx, key, record); err != nil {
			t.Fatalf("Add(%q) error = %v", key, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	removed, err := store.collectStaleSessions(ctx, 0, 2)
	if err != nil {
		t.Fatalf("collectStaleSessions() first run error = %v", err)
	}
	if removed != 2 {
		t.Fatalf("collectStaleSessions() first run removed = %d, want 2", removed)
	}
	if got := store.gcCursor; got != "k2" {
		t.Fatalf("gcCursor after first run = %q, want %q", got, "k2")
	}

	removed, err = store.collectStaleSessions(ctx, 0, 2)
	if err != nil {
		t.Fatalf("collectStaleSessions() second run error = %v", err)
	}
	if removed != 2 {
		t.Fatalf("collectStaleSessions() second run removed = %d, want 2", removed)
	}
	if got := store.gcCursor; got != "k4" {
		t.Fatalf("gcCursor after second run = %q, want %q", got, "k4")
	}
}

func TestRevokeTokenInvalidatesSessionToken(t *testing.T) {
	store := NewSessionStore(time.Minute)
	ctx := context.Background()
	accessToken, refreshToken, err := store.CreateSession(ctx, "alice", sop.RoleUser)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	RevokeToken(ctx, accessToken)

	if _, err := store.ValidateToken(ctx, accessToken); err == nil {
		t.Fatalf("ValidateToken(%q) succeeded after revoke, want error", accessToken)
	}
	if _, err := store.ValidateToken(ctx, refreshToken); err == nil {
		t.Fatalf("ValidateToken(%q) succeeded after revoke, want error", refreshToken)
	}
}

func TestShouldExposeStoreInRBAC_AllowsGlobalSessionsStoreForSystemAdmin(t *testing.T) {
	ctx := sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "root", Roles: []string{sop.RoleSystemAdmin}})
	if !shouldExposeStoreInRBAC(ctx, sessionStoreName) {
		t.Fatalf("shouldExposeStoreInRBAC(%q) = false, want true for system admin", sessionStoreName)
	}
}

func TestShouldExposeStoreInRBAC_HidesGlobalSessionsStoreForNonAdmin(t *testing.T) {
	ctx := sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "alice", Roles: []string{sop.RoleUser}})
	if shouldExposeStoreInRBAC(ctx, sessionStoreName) {
		t.Fatalf("shouldExposeStoreInRBAC(%q) = true, want false for non-admin", sessionStoreName)
	}
}

func TestAuthenticateUser_ValidCredentials(t *testing.T) {
	cfg := Config{}

	if err := cfg.SetUser("root", "secret-password", sop.RoleAdmin); err != nil {
		t.Fatalf("SetUser() error = %v", err)
	}

	ok, user, err := cfg.Authenticate("root", "secret-password")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if !ok {
		t.Fatal("Authenticate() = false, want true")
	}
	if user == nil || user.Username != "root" || user.Role != sop.RoleAdmin {
		t.Fatalf("Authenticate() returned unexpected user: %+v", user)
	}
}

func TestAuthenticateUser_InvalidPassword(t *testing.T) {
	cfg := Config{}

	if err := cfg.SetUser("root", "secret-password", sop.RoleAdmin); err != nil {
		t.Fatalf("SetUser() error = %v", err)
	}

	ok, user, err := cfg.Authenticate("root", "wrong-password")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if ok {
		t.Fatal("Authenticate() = true, want false")
	}
	if user != nil {
		t.Fatalf("Authenticate() returned unexpected user: %+v", user)
	}
}

func TestAuthenticateUser_TrimsWhitespaceInStoredPassword(t *testing.T) {
	cfg := Config{}

	if err := cfg.SetUser("root", "  secret-password  ", sop.RoleAdmin); err != nil {
		t.Fatalf("SetUser() error = %v", err)
	}

	ok, user, err := cfg.Authenticate("root", "  secret-password  ")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if !ok {
		t.Fatal("Authenticate() = false, want true")
	}
	if user == nil || user.Username != "root" || user.Role != sop.RoleAdmin {
		t.Fatalf("Authenticate() returned unexpected user: %+v", user)
	}
}

func TestSaveAndLoadConfig_DoesNotPersistRootUserCredentials(t *testing.T) {
	defer func() { config = Config{} }()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")

	cfg := Config{ConfigFile: configFile}
	if err := cfg.SetUser("root", "secret-password", sop.RoleAdmin); err != nil {
		t.Fatalf("SetUser() error = %v", err)
	}
	config = cfg

	if err := saveConfig(); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	config = Config{}
	if err := LoadConfig(configFile); err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	ok, user, err := config.Authenticate("root", "secret-password")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if ok {
		t.Fatal("Authenticate() = true, want false after save/load because root credentials are not persisted in config state")
	}
	if user != nil {
		t.Fatalf("Authenticate() returned unexpected user: %+v", user)
	}
}

func TestAuthenticate_UsesUsersStoreAsAuthoritativeSource(t *testing.T) {
	cfg := Config{RootPassword: SecuredString("legacy-secret")}

	ok, user, err := cfg.Authenticate("root", "legacy-secret")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if ok {
		t.Fatal("Authenticate() = true, want false when no users-store record exists")
	}
	if user != nil {
		t.Fatalf("Authenticate() returned unexpected user: %+v", user)
	}
}

func TestFindUserRecord_UsesPersistentUserStoreWhenUsersStoreFieldIsNotPrepopulated(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	tmpDir := t.TempDir()
	config = Config{SystemDB: &DatabaseConfig{Name: SystemDBName, Path: tmpDir, Mode: "standalone"}}

	cfg := Config{}
	if err := cfg.SetUser("root", "secret-password", sop.RoleAdmin); err != nil {
		t.Fatalf("SetUser() error = %v", err)
	}

	found, ok, err := cfg.findUserRecord(context.Background(), "root")
	if err != nil {
		t.Fatalf("findUserRecord() error = %v", err)
	}
	if !ok || found == nil || found.Username != "root" {
		t.Fatalf("findUserRecord() returned unexpected user: %+v, ok=%v", found, ok)
	}
}

func TestEnsureUserStorePersistsInSystemDB(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	tmpDir := t.TempDir()
	config = Config{SystemDB: &DatabaseConfig{Name: SystemDBName, Path: tmpDir, Mode: "standalone"}}

	firstCfg := Config{}
	firstStore := firstCfg.ensureUserStore()
	if firstStore == nil {
		t.Fatal("ensureUserStore() returned nil")
	}

	record := &UserRecord{Username: "root", Role: sop.RoleAdmin}
	if err := firstStore.Save(context.Background(), record); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	secondCfg := Config{}
	secondStore := secondCfg.ensureUserStore()
	if secondStore == nil {
		t.Fatal("second ensureUserStore() returned nil")
	}

	got, ok, err := secondStore.Get(context.Background(), "root")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok || got == nil || got.Username != "root" || got.Role != sop.RoleAdmin {
		t.Fatalf("Get() returned unexpected user: %+v, ok=%v", got, ok)
	}
}

func TestEnsureUserStoreSwitchesToConfiguredUserDatabase(t *testing.T) {
	firstDir := t.TempDir()
	secondDir := t.TempDir()

	cfg := Config{
		UsersStore: &DatabaseUserStore{storeName: "users", opts: sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{firstDir}}},
		Databases:  []DatabaseConfig{{Name: "users-db", Path: secondDir, Mode: "standalone"}},
	}

	store := cfg.ensureUserStore()
	dbStore, ok := store.(*DatabaseUserStore)
	if !ok {
		t.Fatalf("ensureUserStore() returned %T, want *DatabaseUserStore", store)
	}
	if len(dbStore.opts.StoresFolders) == 0 || dbStore.opts.StoresFolders[0] != secondDir {
		t.Fatalf("ensureUserStore() reused store for path %v, want %s", dbStore.opts.StoresFolders, secondDir)
	}
}

func TestRegisterUser_PersistsToStoreAndAuthenticates(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	defer os.Chdir(oldWD)

	config = Config{}
	cfg := Config{UsersStore: NewUserStore()}
	user, err := cfg.RegisterUser(context.Background(), "alice", "pw123", "alice@example.com", sop.RoleUser)
	if err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}
	if user == nil || user.Username != "alice" || user.Role != sop.RoleUser {
		t.Fatalf("RegisterUser() returned unexpected user: %+v", user)
	}

	ok, authUser, err := cfg.Authenticate("alice", "pw123")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if !ok || authUser == nil || authUser.Username != "alice" {
		t.Fatalf("Authenticate() returned unexpected user: %+v", authUser)
	}
}

func TestUnregisterUser_RemovesUserAndRevokesSessions(t *testing.T) {
	cfg := Config{UsersStore: NewUserStore()}
	store := NewSessionStore(time.Hour)
	tokenFacade = store

	user, err := cfg.RegisterUser(context.Background(), "alice", "pw123", "alice@example.com", sop.RoleUser)
	if err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}
	if user == nil {
		t.Fatal("RegisterUser() returned nil user")
	}

	accessToken, refreshToken, err := store.CreateSession(context.Background(), user.Username, user.Role)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if err := cfg.UnregisterUser(context.Background(), user.Username); err != nil {
		t.Fatalf("UnregisterUser() error = %v", err)
	}

	if _, ok, err := cfg.findUserRecord(context.Background(), user.Username); err != nil {
		t.Fatalf("findUserRecord() error = %v", err)
	} else if ok {
		t.Fatal("expected user to be removed")
	}

	if ok, _, err := cfg.Authenticate(user.Username, "pw123"); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	} else if ok {
		t.Fatal("Authenticate() = true, want false after unregister")
	}

	if _, err := store.ValidateToken(context.Background(), accessToken); err == nil {
		t.Fatal("ValidateToken() = nil, want error after unregister")
	}
	if _, _, err := store.Refresh(context.Background(), refreshToken); err == nil {
		t.Fatal("Refresh() = nil, want error after unregister")
	}
}

func TestUpdateUserRole_AllowsHigherLevelAdminToDemoteLowerRoleUser(t *testing.T) {
	cfg := Config{UsersStore: NewUserStore()}
	if _, err := cfg.RegisterUser(context.Background(), "admin", "pw123", "admin@example.com", sop.RoleAdmin); err != nil {
		t.Fatalf("RegisterUser(admin) error = %v", err)
	}
	if _, err := cfg.RegisterUser(context.Background(), "alice", "pw123", "alice@example.com", sop.RoleUser); err != nil {
		t.Fatalf("RegisterUser(alice) error = %v", err)
	}

	ctx := sop.ContextWithAuth(context.Background(), sop.AuthContext{Roles: []string{sop.RoleAdmin}, UserID: "admin"})
	updated, err := cfg.UpdateUserRole(ctx, "alice", sop.RoleGuest)
	if err != nil {
		t.Fatalf("UpdateUserRole() error = %v", err)
	}
	if updated == nil || updated.Role != sop.RoleGuest {
		t.Fatalf("UpdateUserRole() returned unexpected role: %+v", updated)
	}
}

func TestUpdateUserRole_RejectsLowerLevelUserTryingToChangeAdminRole(t *testing.T) {
	cfg := Config{UsersStore: NewUserStore()}
	if _, err := cfg.RegisterUser(context.Background(), "admin", "pw123", "admin@example.com", sop.RoleAdmin); err != nil {
		t.Fatalf("RegisterUser(admin) error = %v", err)
	}
	if _, err := cfg.RegisterUser(context.Background(), "alice", "pw123", "alice@example.com", sop.RoleUser); err != nil {
		t.Fatalf("RegisterUser(alice) error = %v", err)
	}

	ctx := sop.ContextWithAuth(context.Background(), sop.AuthContext{Roles: []string{sop.RoleUser}, UserID: "alice"})
	if _, err := cfg.UpdateUserRole(ctx, "admin", sop.RoleUser); err == nil {
		t.Fatal("UpdateUserRole() = nil error, want permission error")
	}
}

func TestRegisterAllHandlers_DoesNotExposeProductAccountRoutes(t *testing.T) {
	mux := http.NewServeMux()
	RegisterAllHandlers(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"username":"bob","password":"pw123","email":"bob@example.com","subscription_plan":"starter"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code == http.StatusCreated {
		t.Fatalf("expected /api/auth/register to be handled by the site layer, not the shared backend, got status %d", w.Code)
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meW := httptest.NewRecorder()
	mux.ServeHTTP(meW, meReq)
	if meW.Code == http.StatusUnauthorized {
		t.Fatalf("expected /api/auth/me to be handled by the site layer, not the shared backend, got status %d", meW.Code)
	}

	subReq := httptest.NewRequest(http.MethodPost, "/api/auth/subscription", strings.NewReader(`{"plan":"none"}`))
	subReq.Header.Set("Content-Type", "application/json")
	subW := httptest.NewRecorder()
	mux.ServeHTTP(subW, subReq)
	if subW.Code == http.StatusUnauthorized {
		t.Fatalf("expected /api/auth/subscription to be handled by the site layer, not the shared backend, got status %d", subW.Code)
	}

	unregisterReq := httptest.NewRequest(http.MethodPost, "/api/auth/unregister", nil)
	unregisterW := httptest.NewRecorder()
	mux.ServeHTTP(unregisterW, unregisterReq)
	if unregisterW.Code == http.StatusUnauthorized {
		t.Fatalf("expected /api/auth/unregister to be handled by the site layer, not the shared backend, got status %d", unregisterW.Code)
	}
}

func TestRegisterAllHandlers_ExposesDaemonBootstrapSecretProxy(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	bootstrapDir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "sop")
	if err := os.MkdirAll(bootstrapDir, 0o755); err != nil {
		t.Fatalf("mkdir bootstrap dir: %v", err)
	}
	bootstrapPath := filepath.Join(bootstrapDir, "daemon.bootstrap.sop-ui")
	if err := os.WriteFile(bootstrapPath, []byte("test-bootstrap-secret\n"), 0o600); err != nil {
		t.Fatalf("write bootstrap file: %v", err)
	}

	mux := http.NewServeMux()
	RegisterAllHandlers(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/daemon/bootstrap-secret?client_id=sop-ui", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected /api/daemon/bootstrap-secret to return 200, got %d with body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "test-bootstrap-secret") {
		t.Fatalf("expected bootstrap secret in response, got %s", w.Body.String())
	}
}

func TestHandleUpdateUserRole_RejectsLowerPrivilegeUser(t *testing.T) {
	config = Config{UsersStore: NewUserStore()}
	if _, err := config.RegisterUser(context.Background(), "admin", "pw123", "admin@example.com", sop.RoleAdmin); err != nil {
		t.Fatalf("RegisterUser(admin) error = %v", err)
	}
	if _, err := config.RegisterUser(context.Background(), "alice", "pw123", "alice@example.com", sop.RoleUser); err != nil {
		t.Fatalf("RegisterUser(alice) error = %v", err)
	}

	body := strings.NewReader(`{"username":"admin","role":"User"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/users/role", body)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "alice", Roles: []string{sop.RoleUser}}))
	w := httptest.NewRecorder()

	handleUpdateUserRole(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("handleUpdateUserRole() status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestHandleRegister_ReturnsCreatedUser(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	defer os.Chdir(oldWD)

	config = Config{}
	config.UsersStore = NewUserStore()

	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"username":"bob","password":"pw123","email":"bob@example.com","subscription_plan":"starter"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleRegister(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("handleRegister() status = %d, body=%s", w.Code, w.Body.String())
	}
	var payload struct {
		Status string `json:"status"`
		User   struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.Status != "ok" || payload.User.Username != "bob" || payload.User.Role != sop.RoleUser {
		t.Fatalf("unexpected response payload: %+v", payload)
	}
}

// NOTE: Test removed - sopbackend only handles auth, not account state.
// Account state (including billing) is now handled by site backend.
// See src/backend/siteaccount for account-related tests.

// NOTE: Test removed - sopbackend no longer handles subscription management.
// Billing/subscription functionality is now in site backend only.
/*
func TestHandleSubscriptionUpdate_UserCanUpdatePlan(t *testing.T) {
	...
}
*/

func TestHasConfigFileOnDisk_UsesCachedProvisioningState(t *testing.T) {
	resetProvisioningCompletionCache()
	defer resetProvisioningCompletionCache()

	config = Config{}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	defer os.Chdir(oldWd)

	config.ConfigFile = filepath.Join(tmpDir, "config.json")
	if HasConfigFileOnDisk() {
		t.Fatal("expected no config file to be detected before provisioning completes")
	}

	markProvisioningComplete()
	if !HasConfigFileOnDisk() {
		t.Fatal("expected cached provisioning state to report config availability")
	}
}

func TestHasConfigFileOnDisk_CachesProvisioningStateAfterCheckingConfigFile(t *testing.T) {
	resetProvisioningCompletionCache()
	defer resetProvisioningCompletionCache()

	config = Config{}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	defer os.Chdir(oldWd)

	config.ConfigFile = filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(config.ConfigFile, []byte(`{"databases":[{"name":"default","path":"/tmp/default"}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if !HasConfigFileOnDisk() {
		t.Fatal("expected config file to be detected")
	}
	if !provisioningCompleteCache.Load() {
		t.Fatal("expected provisioning-complete cache to be set after checking config file")
	}
}

func TestHandleRoot_RedirectsToSetupWizardWhenExplicitConfigIsMissing(t *testing.T) {
	resetProvisioningCompletionCache()
	defer resetProvisioningCompletionCache()
	config = Config{}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	defer os.Chdir(oldWd)

	config.ConfigFile = filepath.Join(tmpDir, "config.json")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handleRoot(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Location"); got != "/app" {
		t.Fatalf("expected redirect to /app, got %q", got)
	}
}

func TestFindExistingConfigFile_PrefersExplicitConfigPath(t *testing.T) {
	config = Config{}

	parentDir := t.TempDir()
	childDir := filepath.Join(parentDir, "child")
	if err := os.Mkdir(childDir, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(parentDir, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(childDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	config.ConfigFile = filepath.Join(parentDir, "config.json")

	if got := FindExistingConfigFile(); got != config.ConfigFile {
		t.Fatalf("FindExistingConfigFile() = %q, want %q", got, config.ConfigFile)
	}
}

func TestFindExistingConfigFile_IgnoresPackageLocalConfigWhenCwdHasNone(t *testing.T) {
	config = Config{}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	defer os.Chdir(oldWd)

	if got := FindExistingConfigFile(); got != "" {
		t.Fatalf("FindExistingConfigFile() = %q, want empty when cwd has no config.json", got)
	}
}

func TestHandleRoot_RedirectsToSetupWizardWhenConfigIsEmpty(t *testing.T) {
	resetProvisioningCompletionCache()
	defer resetProvisioningCompletionCache()
	config = Config{}
	config.ConfigFile = filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(config.ConfigFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handleRoot(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Location"); got != "/app" {
		t.Fatalf("expected redirect to /app, got %q", got)
	}
}

func TestHandleRoot_ShowsLoginPageWhenConfigExists(t *testing.T) {
	resetProvisioningCompletionCache()
	defer resetProvisioningCompletionCache()
	config = Config{}
	config.ConfigFile = filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(config.ConfigFile, []byte(`{"databases":[{"name":"default","path":"/tmp/default"}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handleRoot(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "SOP Login") {
		t.Fatalf("expected login page HTML, got %q", w.Body.String())
	}
}

func TestRequireAuth_RedirectsToLoginForProtectedAppPage(t *testing.T) {
	config = Config{}
	if err := config.SetUser("root", "secret-password", sop.RoleAdmin); err != nil {
		t.Fatalf("SetUser() error = %v", err)
	}
	config.ConfigFile = filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(config.ConfigFile, []byte(`{"databases":[{"name":"default","path":"/tmp/default"}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	w := httptest.NewRecorder()

	requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("protected handler should not run when not authenticated")
	}))(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Location"); got != "/login?return=%2Fapp" {
		t.Fatalf("expected redirect to /login?return=/app, got %q", got)
	}
}

func TestHandleLogin_SetsSessionCookies(t *testing.T) {
	withIsolatedSessionStore(t)
	config = Config{SystemDB: config.SystemDB}
	if err := config.SetUser("root", "secret-password", sop.RoleAdmin); err != nil {
		t.Fatalf("SetUser() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"root","password":"secret-password"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleLogin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	cookies := w.Result().Cookies()
	foundAccess := false
	foundRefresh := false
	for _, cookie := range cookies {
		switch cookie.Name {
		case "sop_access_token":
			foundAccess = true
		case "sop_refresh_token":
			foundRefresh = true
		}
	}
	if !foundAccess || !foundRefresh {
		t.Fatalf("expected both sop_access_token and sop_refresh_token cookies, got %v", cookies)
	}
}

func TestHandleLogin_ReturnsRefreshToken(t *testing.T) {
	withIsolatedSessionStore(t)
	config = Config{SystemDB: config.SystemDB}
	if err := config.SetUser("root", "secret-password", sop.RoleAdmin); err != nil {
		t.Fatalf("SetUser() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"root","password":"secret-password"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleLogin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Fatalf("expected access_token and refresh_token, got %+v", resp)
	}
}

func TestHandleRefresh_ReturnsNewAccessToken(t *testing.T) {
	withIsolatedSessionStore(t)
	config = Config{SystemDB: config.SystemDB}
	if err := config.SetUser("root", "secret-password", sop.RoleAdmin); err != nil {
		t.Fatalf("SetUser() error = %v", err)
	}

	access, refresh, err := currentTokenFacade().(*SessionStore).CreateSession(context.Background(), "root", sop.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", strings.NewReader(`{"refresh_token":"`+refresh+`"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleRefresh(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if access == "" || refresh == "" {
		t.Fatalf("unexpected empty token values")
	}

	var resp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AccessToken == "" {
		t.Fatalf("expected a renewed access_token, got %+v", resp)
	}
}

func TestHandleRefresh_SetsSessionCookie(t *testing.T) {
	withIsolatedSessionStore(t)
	config = Config{SystemDB: config.SystemDB}
	oldFacade := tokenFacade
	store := NewSessionStore(time.Minute)
	tokenFacade = store
	defer func() { tokenFacade = oldFacade }()

	_, refresh, err := store.CreateSession(context.Background(), "root", sop.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", strings.NewReader(`{"refresh_token":"`+refresh+`"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleRefresh(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	cookies := w.Result().Cookies()
	var found bool
	for _, cookie := range cookies {
		if cookie.Name == "sop_access_token" && cookie.Value != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected sop_access_token cookie to be set on refresh response, got cookies=%v", cookies)
	}
}

func TestHandleRefresh_SetsPersistentCookies(t *testing.T) {
	withIsolatedSessionStore(t)
	config = Config{SystemDB: config.SystemDB}
	oldFacade := tokenFacade
	store := NewSessionStore(time.Minute)
	tokenFacade = store
	defer func() { tokenFacade = oldFacade }()

	_, refresh, err := store.CreateSession(context.Background(), "root", sop.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", strings.NewReader(`{"refresh_token":"`+refresh+`"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleRefresh(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	for _, cookie := range w.Result().Cookies() {
		switch cookie.Name {
		case "sop_access_token", "sop_refresh_token":
			if cookie.MaxAge <= 0 {
				t.Fatalf("expected %s cookie to be persistent (MaxAge>0), got MaxAge=%d", cookie.Name, cookie.MaxAge)
			}
		}
	}
}

func TestValidateToken_AcceptsStatelessAccessTokenAfterSessionRemoval(t *testing.T) {
	withIsolatedSessionStore(t)
	store := NewSessionStore(30 * time.Minute)
	ctx := context.Background()

	access, _, err := store.CreateSession(ctx, "root", sop.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	st, tx, err := store.getStore(ctx)
	if err != nil {
		t.Fatalf("getStore() error = %v", err)
	}
	defer tx.Rollback(ctx)

	if removed, err := st.Remove(ctx, access); err != nil || !removed {
		t.Fatalf("Remove() = (%v, %v), want session record removed", removed, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	user, err := store.ValidateToken(ctx, access)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if user == nil || !strings.EqualFold(user.Username, "root") || !strings.EqualFold(user.Role, sop.RoleAdmin) {
		t.Fatalf("ValidateToken() = %+v, want root admin user", user)
	}
}

func TestValidateToken_AcceptsDaemonBootstrapSessionWithoutRegisteredUser(t *testing.T) {
	withIsolatedSessionStore(t)
	store := NewSessionStore(30 * time.Minute)
	ctx := context.Background()

	access, _, err := store.CreateSession(ctx, "ui-client", "daemon-user")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	user, err := store.ValidateToken(ctx, access)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if user == nil || !strings.EqualFold(user.Username, "ui-client") || !strings.EqualFold(user.Role, "daemon-user") {
		t.Fatalf("ValidateToken() = %+v, want daemon bootstrap user", user)
	}
}

func TestValidateToken_DoesNotExtendSessionWhenNotNearExpiry(t *testing.T) {
	withIsolatedSessionStore(t)
	store := NewSessionStore(30 * time.Minute)
	ctx := context.Background()
	access, _, err := store.CreateSession(ctx, "root", sop.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	st, tx, err := store.getStore(ctx)
	if err != nil {
		t.Fatalf("getStore() error = %v", err)
	}
	defer tx.Rollback(ctx)

	if found, err := st.Find(ctx, access, false); err != nil || !found {
		t.Fatalf("Find() = (%v, %v), want found session", err, found)
	}
	record, err := st.GetCurrentValue(ctx)
	if err != nil {
		t.Fatalf("GetCurrentValue() error = %v", err)
	}
	expectedExpiresAt := time.Now().UTC().Add(10 * time.Minute)
	record.ExpiresAt = expectedExpiresAt
	if ok, err := st.UpdateCurrentValue(ctx, record); err != nil || !ok {
		t.Fatalf("UpdateCurrentValue() = (%v, %v), want success", ok, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	oldFacade := tokenFacade
	tokenFacade = store
	defer func() { tokenFacade = oldFacade }()

	if _, err := store.ValidateToken(ctx, access); err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}

	st2, tx2, err := store.getStore(ctx)
	if err != nil {
		t.Fatalf("getStore() after validation error = %v", err)
	}
	defer tx2.Rollback(ctx)

	if found, err := st2.Find(ctx, access, false); err != nil || !found {
		t.Fatalf("Find() = (%v, %v), want session found", err, found)
	}
	updated, err := st2.GetCurrentValue(ctx)
	if err != nil {
		t.Fatalf("GetCurrentValue() after validation error = %v", err)
	}
	if !updated.ExpiresAt.Equal(expectedExpiresAt) {
		t.Fatalf("ValidateToken() extended expiry unexpectedly: got %v, want %v", updated.ExpiresAt, expectedExpiresAt)
	}
}

func TestAuthenticateBearerToken_RejectsExpiredSessionToken(t *testing.T) {
	withIsolatedSessionStore(t)
	store := NewSessionStore(time.Nanosecond)
	ctx := context.Background()
	access, _, err := store.CreateSession(ctx, "root", sop.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	st, tx, err := store.getStore(ctx)
	if err != nil {
		t.Fatalf("getStore() error = %v", err)
	}
	defer tx.Rollback(ctx)
	if found, err := st.Find(ctx, access, false); err != nil || !found {
		t.Fatalf("Find() = (%v, %v), want found session", err, found)
	}
	record, err := st.GetCurrentValue(ctx)
	if err != nil {
		t.Fatalf("GetCurrentValue() error = %v", err)
	}
	record.ExpiresAt = time.Now().UTC().Add(-time.Second)
	if ok, err := st.UpdateCurrentValue(ctx, record); err != nil || !ok {
		t.Fatalf("UpdateCurrentValue() = (%v, %v), want success", ok, err)
	}

	oldFacade := tokenFacade
	tokenFacade = store
	defer func() { tokenFacade = oldFacade }()

	ok, user, err := (&Config{}).AuthenticateBearerToken(context.Background(), access)
	if err == nil || ok || user != nil {
		t.Fatalf("AuthenticateBearerToken() = (%v, %+v, %v), want expired session rejected", ok, user, err)
	}

	st2, tx2, err := store.getStore(ctx)
	if err != nil {
		t.Fatalf("getStore() after expiry error = %v", err)
	}
	defer tx2.Rollback(ctx)
	if found, err := st2.Find(ctx, access, false); err != nil || found {
		t.Fatalf("Find() after expiry = (%v, %v), want expired session removed", err, found)
	}
}

func TestAuthenticateBearerToken_AcceptsSignedAccessTokenForRemoteClient(t *testing.T) {
	withIsolatedSessionStore(t)
	oldConfig := config
	config = Config{SessionSecret: SecuredString("remote-client-test-secret")}
	defer func() { config = oldConfig }()

	store := NewSessionStore(time.Minute)
	access, _, err := store.CreateSession(context.Background(), "root", sop.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	ok, user, err := (&Config{}).AuthenticateBearerToken(context.Background(), access)
	if err != nil {
		t.Fatalf("AuthenticateBearerToken() error = %v", err)
	}
	if !ok || user == nil || !strings.EqualFold(user.Username, "root") {
		t.Fatalf("AuthenticateBearerToken() = (%v, %+v), want authenticated remote client token", ok, user)
	}
}

func TestConfig_Authenticate_UsesRegisteredAuthProvider(t *testing.T) {
	providerName := "test-provider"

	authProviderMu.Lock()
	oldFactory, existed := authProviderRegistry[providerName]
	authProviderRegistry[providerName] = func(*Config) (AuthProvider, error) {
		return &testAuthProvider{name: providerName}, nil
	}
	authProviderMu.Unlock()
	defer func() {
		authProviderMu.Lock()
		if existed {
			authProviderRegistry[providerName] = oldFactory
		} else {
			delete(authProviderRegistry, providerName)
		}
		authProviderMu.Unlock()
	}()

	cfg := Config{AuthProviderName: providerName}
	ok, user, err := cfg.Authenticate("alice", "pw")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if !ok || user == nil || user.Username != "alice" || user.Role != sop.RoleUser {
		t.Fatalf("Authenticate() = (%v, %+v), want provider-authenticated user", ok, user)
	}
}

func TestAuthenticateBearerToken_RejectsLegacyCredentialToken(t *testing.T) {
	cfg := Config{}
	if err := cfg.SetUser("root", "secret-password", sop.RoleAdmin); err != nil {
		t.Fatalf("SetUser() error = %v", err)
	}

	ok, user, err := cfg.AuthenticateBearerToken(context.Background(), "root:secret-password")
	if err == nil && ok {
		t.Fatalf("AuthenticateBearerToken() = (%v, %+v, %v), want legacy credential bearer token rejected", ok, user, err)
	}
}

func TestHandleLogin_ReusesCurrentSessionToken(t *testing.T) {
	withIsolatedSessionStore(t)
	config = Config{SystemDB: config.SystemDB}
	if err := config.SetUser("root", "secret-password", sop.RoleAdmin); err != nil {
		t.Fatalf("SetUser() error = %v", err)
	}

	store := NewSessionStore(time.Minute)
	oldFacade := tokenFacade
	tokenFacade = store
	defer func() { tokenFacade = oldFacade }()

	existingToken, _, err := store.CreateSession(context.Background(), "root", sop.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"root","password":"secret-password"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+existingToken)
	w := httptest.NewRecorder()

	handleLogin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AccessToken != existingToken {
		t.Fatalf("expected existing session token to be reused, got %q", resp.AccessToken)
	}
}

func TestHandleLogin_ReturnsSessionToken(t *testing.T) {
	withIsolatedSessionStore(t)
	config = Config{SystemDB: config.SystemDB}
	if err := config.SetUser("root", "secret-password", sop.RoleAdmin); err != nil {
		t.Fatalf("SetUser() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"root","password":"secret-password"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleLogin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AccessToken == "" {
		t.Fatal("expected access_token in login response")
	}
	if resp.TokenType != "Bearer" {
		t.Fatalf("expected token_type Bearer, got %q", resp.TokenType)
	}

	ok, user, err := config.AuthenticateBearerToken(context.Background(), resp.AccessToken)
	if err != nil {
		t.Fatalf("AuthenticateBearerToken() error = %v", err)
	}
	if !ok || user == nil || user.Username != "root" {
		t.Fatalf("AuthenticateBearerToken() = (%v, %+v), want authenticated root user", ok, user)
	}
}

func TestSessionTTL_UsesConfigValue(t *testing.T) {
	config = Config{SessionTokenTTLMinutes: 45}
	if got := sessionTTL(); got != 45*time.Minute {
		t.Fatalf("sessionTTL() = %v, want %v", got, 45*time.Minute)
	}
}

func TestHandleLoginPage_RendersLoginScreen(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	w := httptest.NewRecorder()

	handleLoginPage(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "SOP Login") {
		t.Fatalf("expected login page HTML, got %q", w.Body.String())
	}
}

func TestHandleLogin_FormEncodedCredentials(t *testing.T) {
	withIsolatedSessionStore(t)
	config = Config{SystemDB: config.SystemDB}
	if err := config.SetUser("root", "secret-password", sop.RoleAdmin); err != nil {
		t.Fatalf("SetUser() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader("grant_type=password&username=root&password=secret-password"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handleLogin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthFromAuthorization_DoesNotAcceptLegacyRootPassword(t *testing.T) {
	cfg := Config{RootPassword: SecuredString("legacy-secret")}

	username, password := cfg.authFromAuthorization("Bearer legacy-secret")
	if username != "" || password != "" {
		t.Fatalf("authFromAuthorization() = (%q, %q), want empty values for legacy root-password fallback", username, password)
	}
}

func TestIsAdminOverride_DoesNotAcceptLegacyRootPassword(t *testing.T) {
	cfg := Config{RootPassword: SecuredString("legacy-secret")}

	if ok := cfg.IsAdminOverride(context.Background(), "", "", "legacy-secret"); ok {
		t.Fatal("IsAdminOverride() = true, want false for legacy root-password fallback")
	}
}

func TestIsAdminOverride_AcceptsSessionToken(t *testing.T) {
	withIsolatedSessionStore(t)
	cfg := Config{SystemDB: config.SystemDB}
	if err := cfg.SetUser("root", "secret-password", sop.RoleAdmin); err != nil {
		t.Fatalf("SetUser() error = %v", err)
	}

	token, err := currentTokenFacade().CreateToken(context.Background(), "root", sop.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}

	if ok := cfg.IsAdminOverride(context.Background(), "", "", token); !ok {
		t.Fatal("IsAdminOverride() = false, want true for session token")
	}
}

func TestSetUser_DoesNotCreatePersistentStoreWhenAuthIsDisabled(t *testing.T) {
	cfg := Config{DisableAuth: true, SystemDB: &DatabaseConfig{Name: SystemDBName, Path: t.TempDir(), Mode: "standalone"}}

	if err := cfg.SetUser("root", "secret-password", sop.RoleAdmin); err != nil {
		t.Fatalf("SetUser() error = %v", err)
	}

	store := cfg.ensureUserStore()
	if _, ok := store.(*SystemUserStore); ok {
		t.Fatal("SetUser() created a persistent system user store while auth was disabled")
	}
	if _, ok := store.(*DatabaseUserStore); ok {
		t.Fatal("SetUser() created a persistent database user store while auth was disabled")
	}
}

func TestSetUser_DuplicateUsername(t *testing.T) {
	cfg := Config{}

	if err := cfg.SetUser("root", "first", sop.RoleAdmin); err != nil {
		t.Fatalf("first SetUser() error = %v", err)
	}

	if err := cfg.SetUser("root", "second", sop.RoleUser); err == nil {
		t.Fatal("SetUser() expected duplicate username error, got nil")
	}
}

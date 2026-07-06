package sopbackend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai/agent"
	aidb "github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/memory"
)

func TestFederationIntegration_SyncRetrieveAndPullRoundTrip(t *testing.T) {
	env := setupFederationIntegrationEnv(t, "hqdb")

	spaceName := "RoundTripSpace"
	if err := populateFederationSpace(t, env, spaceName, []map[string]any{
		{"text": "Use replication for high availability", "category": "operations"},
		{"text": "Keep daemon syncs small and periodic", "category": "operations"},
	}); err != nil {
		t.Fatalf("populate space: %v", err)
	}

	if err := setAutoShareForSpace(t, env, spaceName, true); err != nil {
		t.Fatalf("enable auto-share: %v", err)
	}

	syncPayload := map[string]any{
		"kind":       "shared_memory_sync",
		"daemon_id":  "daemon-alpha",
		"daemon_url": "http://127.0.0.1:19090",
		"user_id":    "alice",
		"session_id": "session-alpha",
		"timestamp":  "2026-07-06T12:00:00Z",
		"shared_memory": map[string]any{
			"enabled": true,
			"artifacts": []map[string]any{{
				"text":     "Federated sync should preserve daemon context",
				"category": "shared_memory",
				"metadata": map[string]any{"source": "integration"},
			}},
		},
	}
	if err := postJSON(t, "/api/daemon/hq-sync", syncPayload, func(rr *httptest.ResponseRecorder) {
		if rr.Code != http.StatusOK {
			t.Fatalf("sync status = %d, body = %s", rr.Code, rr.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("decode sync response: %v", err)
		}
		spaces, ok := resp["auto_shared_spaces"].([]any)
		if !ok || len(spaces) == 0 {
			t.Fatalf("expected auto_shared_spaces in sync response, got %v", resp)
		}
	}); err != nil {
		t.Fatalf("sync request: %v", err)
	}

	if err := postJSON(t, "/api/daemon/hq-retrieve", map[string]any{"daemon_id": "daemon-alpha", "query": "Federated sync", "limit": 5}, func(rr *httptest.ResponseRecorder) {
		if rr.Code != http.StatusOK {
			t.Fatalf("retrieve status = %d, body = %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Artifacts []struct {
				Text string `json:"text"`
			} `json:"artifacts"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("decode retrieve response: %v", err)
		}
		if len(resp.Artifacts) == 0 {
			t.Fatalf("expected at least one artifact in retrieve response")
		}
	}); err != nil {
		t.Fatalf("retrieve request: %v", err)
	}

	if err := postJSON(t, "/api/space/pull", map[string]any{"space_name": spaceName, "database_name": env.dbName}, func(rr *httptest.ResponseRecorder) {
		if rr.Code != http.StatusOK {
			t.Fatalf("pull status = %d, body = %s", rr.Code, rr.Body.String())
		}
		body := rr.Body.String()
		if !strings.Contains(body, "Use replication for high availability") {
			t.Fatalf("pulled space body did not contain expected content: %s", body)
		}
	}); err != nil {
		t.Fatalf("pull request: %v", err)
	}
}

func TestFederationIntegration_CategoryAutoShareFiltersDaemonRetrieval(t *testing.T) {
	_ = setupFederationIntegrationEnv(t, "hqdb")

	sysDBOpts, err := GetSystemDBOptions(context.Background())
	if err != nil {
		t.Fatalf("system db options: %v", err)
	}
	sysDB := aidb.NewDatabase(sysDBOpts)

	ctx := context.Background()
	if err := agent.PromoteSharedMemoryArtifact(ctx, sysDB, nil, nil, "Allowed federation note", sharedMemoryKBName, "allowed", "daemon", "daemon-beta", map[string]any{"source": "integration"}); err != nil {
		t.Fatalf("promote allowed artifact: %v", err)
	}
	if err := agent.PromoteSharedMemoryArtifact(ctx, sysDB, nil, nil, "Restricted federation note", sharedMemoryKBName, "restricted", "daemon", "daemon-beta", map[string]any{"source": "integration"}); err != nil {
		t.Fatalf("promote restricted artifact: %v", err)
	}

	tx, err := sysDB.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	kb, err := sysDB.OpenKnowledgeBase(ctx, sharedMemoryKBName, tx, nil, nil, false)
	if err != nil {
		t.Fatalf("open shared-memory kb: %v", err)
	}

	allowed := true
	restricted := false
	for _, cfg := range []struct {
		path    string
		enabled bool
	}{
		{path: "allowed", enabled: allowed},
		{path: "restricted", enabled: restricted},
	} {
		if err := upsertCategoryAutoShareSetting(ctx, kb, cfg.path, cfg.enabled); err != nil {
			t.Fatalf("set category auto-share for %s: %v", cfg.path, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit category tx: %v", err)
	}

	if err := postJSON(t, "/api/daemon/hq-retrieve", map[string]any{"daemon_id": "daemon-beta", "query": "federation", "limit": 10}, func(rr *httptest.ResponseRecorder) {
		if rr.Code != http.StatusOK {
			t.Fatalf("retrieve status = %d, body = %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Artifacts []struct {
				Text     string         `json:"text"`
				Category string         `json:"category"`
				Metadata map[string]any `json:"metadata"`
			} `json:"artifacts"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("decode retrieve response: %v", err)
		}
		if len(resp.Artifacts) != 1 {
			t.Fatalf("expected exactly one shared artifact, got %d: %+v", len(resp.Artifacts), resp.Artifacts)
		}
		if resp.Artifacts[0].Category != "allowed" {
			t.Fatalf("expected allowed category artifact, got category=%q", resp.Artifacts[0].Category)
		}
		if !strings.Contains(resp.Artifacts[0].Text, "Allowed") {
			t.Fatalf("expected allowed artifact text, got %q", resp.Artifacts[0].Text)
		}
	}); err != nil {
		t.Fatalf("retrieve request: %v", err)
	}
}

func TestFederationIntegration_AutoShareToggleAndListing(t *testing.T) {
	env := setupFederationIntegrationEnv(t, "hqdb")

	spaceName := "ToggleSpace"
	if err := populateFederationSpace(t, env, spaceName, []map[string]any{{"text": "Use federation controls judiciously", "category": "operations"}}); err != nil {
		t.Fatalf("populate space: %v", err)
	}

	if err := postJSON(t, "/api/space/auto-share", map[string]any{"space_name": spaceName, "database_name": env.dbName, "enabled": true}, func(rr *httptest.ResponseRecorder) {
		if rr.Code != http.StatusOK {
			t.Fatalf("enable status = %d, body = %s", rr.Code, rr.Body.String())
		}
	}); err != nil {
		t.Fatalf("enable auto-share request: %v", err)
	}

	if err := getJSON(t, "/api/space/auto-shared?database="+env.dbName, func(rr *httptest.ResponseRecorder) {
		if rr.Code != http.StatusOK {
			t.Fatalf("list status = %d, body = %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			AutoShared []struct {
				Name string `json:"name"`
			} `json:"auto_shared"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("decode auto-shared response: %v", err)
		}
		if len(resp.AutoShared) == 0 {
			t.Fatalf("expected auto-shared spaces to be listed")
		}
	}); err != nil {
		t.Fatalf("list auto-shared request: %v", err)
	}

	if err := postJSON(t, "/api/space/auto-share", map[string]any{"space_name": spaceName, "database_name": env.dbName, "enabled": false}, func(rr *httptest.ResponseRecorder) {
		if rr.Code != http.StatusOK {
			t.Fatalf("disable status = %d, body = %s", rr.Code, rr.Body.String())
		}
	}); err != nil {
		t.Fatalf("disable auto-share request: %v", err)
	}

	if err := getJSON(t, "/api/space/auto-shared?database="+env.dbName, func(rr *httptest.ResponseRecorder) {
		if rr.Code != http.StatusOK {
			t.Fatalf("list-after-disable status = %d, body = %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			AutoShared []struct {
				Name string `json:"name"`
			} `json:"auto_shared"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("decode auto-shared response after disable: %v", err)
		}
		for _, entry := range resp.AutoShared {
			if entry.Name == spaceName {
				t.Fatalf("space %s should not remain auto-shared after disable", spaceName)
			}
		}
	}); err != nil {
		t.Fatalf("list auto-shared request after disable: %v", err)
	}
}

type federationIntegrationEnv struct {
	t      *testing.T
	root   string
	dbName string
}

func setupFederationIntegrationEnv(t *testing.T, dbName string) *federationIntegrationEnv {
	t.Helper()

	oldConfig := config
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working dir: %v", err)
	}
	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	config = Config{
		UsersStore: NewUserStore(),
		SystemDB:   &DatabaseConfig{Name: SystemDBName, Path: root, Mode: "standalone"},
		Databases:  []DatabaseConfig{{Name: dbName, Path: root, Mode: "standalone"}},
	}
	if err := seedFederationRootUser(t); err != nil {
		t.Fatalf("seed root user: %v", err)
	}

	t.Cleanup(func() {
		config = oldConfig
		_ = os.Chdir(oldCwd)
	})

	return &federationIntegrationEnv{t: t, root: root, dbName: dbName}
}

func seedFederationRootUser(t *testing.T) error {
	t.Helper()
	rootUser := &UserRecord{
		Username:     "ltm_root",
		Role:         sop.RoleSystemAdmin,
		PasswordHash: SecuredString("$2a$10$test"),
		Status:       "active",
	}
	return config.ensureUserStore().Save(context.Background(), rootUser)
}

func populateFederationSpace(t *testing.T, env *federationIntegrationEnv, spaceName string, items []map[string]any) error {
	t.Helper()
	ctx := context.Background()
	dbOpts, err := GetDBOptions(ctx, env.dbName)
	if err != nil {
		return fmt.Errorf("get db options: %w", err)
	}
	db := aidb.NewDatabase(dbOpts)

	trans, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer trans.Rollback(ctx)

	kb, err := db.OpenKnowledgeBase(ctx, spaceName, trans, nil, nil, true)
	if err != nil {
		return fmt.Errorf("open kb: %w", err)
	}

	for i, data := range items {
		item := memory.Item[map[string]any]{ID: sop.NewUUID(), Data: data}
		if err := kb.Store.Upsert(ctx, item, nil); err != nil {
			return fmt.Errorf("upsert item %d: %w", i, err)
		}
	}

	if err := trans.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func setAutoShareForSpace(t *testing.T, env *federationIntegrationEnv, spaceName string, enabled bool) error {
	t.Helper()
	payload := map[string]any{"space_name": spaceName, "database_name": env.dbName, "enabled": enabled}
	var status int
	if err := postJSON(t, "/api/space/auto-share", payload, func(rr *httptest.ResponseRecorder) {
		status = rr.Code
	}); err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("expected status %d, got %d", http.StatusOK, status)
	}
	return nil
}

func upsertCategoryAutoShareSetting(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], path string, enabled bool) error {
	catsByPath, err := kb.Store.CategoriesByPath(ctx)
	if err != nil {
		return fmt.Errorf("get categories by path: %w", err)
	}

	found, err := catsByPath.Find(ctx, path, false)
	if err != nil {
		return fmt.Errorf("find category path %s: %w", path, err)
	}

	if found {
		catID, err := catsByPath.GetCurrentValue(ctx)
		if err != nil {
			return fmt.Errorf("get category id for %s: %w", path, err)
		}

		cats, err := kb.Store.Categories(ctx)
		if err != nil {
			return fmt.Errorf("get categories: %w", err)
		}
		foundCat, err := cats.Find(ctx, catID, false)
		if err != nil {
			return fmt.Errorf("find category %s: %w", path, err)
		}
		if !foundCat {
			return fmt.Errorf("category %s not found", path)
		}
		cat, err := cats.GetCurrentValue(ctx)
		if err != nil {
			return fmt.Errorf("get category value for %s: %w", path, err)
		}
		cat.AutoShareToDaemons = &enabled
		if _, err := cats.Update(ctx, catID, cat); err != nil {
			return fmt.Errorf("update category %s: %w", path, err)
		}
		return nil
	}

	cat := &memory.Category{Name: path, Path: path, AutoShareToDaemons: &enabled}
	if _, err := kb.Store.AddCategory(ctx, cat); err != nil {
		return fmt.Errorf("add category %s: %w", path, err)
	}
	return nil
}

func postJSON(t *testing.T, path string, payload any, assert func(*httptest.ResponseRecorder)) error {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "root", Roles: []string{sop.RoleSystemAdmin}}))
	rr := httptest.NewRecorder()
	if strings.HasPrefix(path, "/api/space/auto-share") {
		HandleSetSpaceAutoShare(rr, req)
	} else if path == "/api/daemon/hq-sync" {
		HandleHQSync(rr, req)
	} else if path == "/api/daemon/hq-retrieve" {
		HandleHQRetrieve(rr, req)
	} else if path == "/api/space/pull" {
		HandleDaemonPullSpace(rr, req)
	} else {
		return fmt.Errorf("unsupported path: %s", path)
	}
	assert(rr)
	return nil
}

func getJSON(t *testing.T, path string, assert func(*httptest.ResponseRecorder)) error {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "root", Roles: []string{sop.RoleSystemAdmin}}))
	rr := httptest.NewRecorder()
	HandleListAutoSharedSpaces(rr, req)
	assert(rr)
	return nil
}

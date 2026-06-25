package sopbackend

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestHandleRuntimeRefreshReturnsDatabaseCatalog(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	config = Config{
		Databases: []DatabaseConfig{{Name: "local", Path: filepath.Join(t.TempDir(), "db"), Mode: "standalone"}},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/runtime/refresh", nil)
	rr := httptest.NewRecorder()

	handleRuntimeRefresh(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleRuntimeRefresh() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp struct {
		Runtime          string              `json:"runtime"`
		Databases        []DatabaseConfig    `json:"databases"`
		SelectedDatabase string              `json:"selected_database,omitempty"`
		Stores           map[string][]string `json:"stores,omitempty"`
		Spaces           map[string][]string `json:"spaces,omitempty"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Runtime != "hq" {
		t.Fatalf("runtime = %q, want %q", resp.Runtime, "hq")
	}
	if len(resp.Databases) == 0 {
		t.Fatalf("expected at least one database in response, got %+v", resp.Databases)
	}
	found := false
	for _, db := range resp.Databases {
		if db.Name == "local" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected configured database in response, got %+v", resp.Databases)
	}
	if resp.SelectedDatabase != "local" {
		t.Fatalf("selected_database = %q, want %q", resp.SelectedDatabase, "local")
	}
}

package sopbackend

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/sharedcode/sop/ai/database"
)

func TestHandleDeleteSpace_AllowsSystemDBDeletion(t *testing.T) {
	oldConfig := config
	t.Cleanup(func() { config = oldConfig })

	tmpDir := t.TempDir()
	dbName := "system"
	spaceName := "test-space-delete"

	config = Config{
		Databases: []DatabaseConfig{{Name: dbName, Path: tmpDir, Mode: "standalone"}},
		SystemDB:  &DatabaseConfig{Name: dbName, Path: tmpDir, Mode: "standalone"},
	}

	createBody, err := json.Marshal(CreateSpaceRequest{DatabaseName: dbName, SpaceName: spaceName})
	if err != nil {
		t.Fatalf("json.Marshal(create) error = %v", err)
	}
	createReq := httptest.NewRequest(http.MethodPost, "/api/spaces/create", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	handleCreateSpace(createRR, createReq)
	if createRR.Code != http.StatusOK {
		t.Fatalf("create space status = %d, body = %s", createRR.Code, createRR.Body.String())
	}

	deleteBody, err := json.Marshal(map[string]string{"database": dbName, "space": spaceName})
	if err != nil {
		t.Fatalf("json.Marshal(delete) error = %v", err)
	}
	deleteReq := httptest.NewRequest(http.MethodPost, "/api/spaces/delete", bytes.NewReader(deleteBody))
	deleteReq.Header.Set("Content-Type", "application/json")
	deleteRR := httptest.NewRecorder()
	handleDeleteSpace(deleteRR, deleteReq)
	if deleteRR.Code != http.StatusOK {
		t.Fatalf("delete space status = %d, body = %s", deleteRR.Code, deleteRR.Body.String())
	}

	dbOpts, err := GetDBOptions(context.Background(), dbName)
	if err != nil {
		t.Fatalf("GetDBOptions() error = %v", err)
	}
	aidb := database.NewDatabase(dbOpts)
	domains, err := aidb.GetDomains(context.Background())
	if err != nil {
		t.Fatalf("GetDomains() error = %v", err)
	}
	for _, domain := range domains {
		if domain == spaceName {
			t.Fatalf("expected space %q to be removed", spaceName)
		}
	}
}

func TestSpaceListUsesGlobalLabelForInternalMemorySpace(t *testing.T) {
	oldConfig := config
	t.Cleanup(func() { config = oldConfig })

	tmpDir := t.TempDir()
	dbName := "testdb_global_memory_label"
	spaceName := "ltm_testuser"

	config = Config{
		Databases: []DatabaseConfig{{Name: dbName, Path: tmpDir, Mode: "standalone"}},
	}

	createBody, err := json.Marshal(CreateSpaceRequest{DatabaseName: dbName, SpaceName: spaceName})
	if err != nil {
		t.Fatalf("json.Marshal(create) error = %v", err)
	}
	createReq := httptest.NewRequest(http.MethodPost, "/api/spaces/create", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	handleCreateSpace(createRR, createReq)
	if createRR.Code != http.StatusOK {
		t.Fatalf("create space status = %d, body = %s", createRR.Code, createRR.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/spaces?database="+dbName, nil)
	listRR := httptest.NewRecorder()
	handleListSpaces(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list spaces status = %d, body = %s", listRR.Code, listRR.Body.String())
	}

	var listResp struct {
		Data         []string          `json:"data"`
		DisplayNames map[string]string `json:"display_names"`
	}
	if err := json.NewDecoder(listRR.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list response error = %v", err)
	}
	if got := listResp.DisplayNames[spaceName]; got != "memory" {
		t.Fatalf("display_names[%q] = %q, want %q", spaceName, got, "memory")
	}
}

func TestSpaceConfigAndListExposeDisplayName(t *testing.T) {
	oldConfig := config
	t.Cleanup(func() { config = oldConfig })

	tmpDir := t.TempDir()
	dbName := "testdb_display_name"
	spaceName := "ltm_testuser"

	config = Config{
		Databases: []DatabaseConfig{{Name: dbName, Path: tmpDir, Mode: "standalone"}},
	}

	createBody, err := json.Marshal(CreateSpaceRequest{DatabaseName: dbName, SpaceName: spaceName})
	if err != nil {
		t.Fatalf("json.Marshal(create) error = %v", err)
	}
	createReq := httptest.NewRequest(http.MethodPost, "/api/spaces/create", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	handleCreateSpace(createRR, createReq)
	if createRR.Code != http.StatusOK {
		t.Fatalf("create space status = %d, body = %s", createRR.Code, createRR.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/spaces?database="+dbName, nil)
	listRR := httptest.NewRecorder()
	handleListSpaces(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list spaces status = %d, body = %s", listRR.Code, listRR.Body.String())
	}

	var listResp struct {
		Data         []string          `json:"data"`
		DisplayNames map[string]string `json:"display_names"`
	}
	if err := json.NewDecoder(listRR.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list response error = %v", err)
	}

	found := false
	for _, name := range listResp.Data {
		if name == spaceName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected space %q in response data, got %#v", spaceName, listResp.Data)
	}
	if got := listResp.DisplayNames[spaceName]; got != "memory" {
		t.Fatalf("display_names[%q] = %q, want %q", spaceName, got, "memory")
	}

	if _, err := filepath.Abs(tmpDir); err != nil {
		t.Fatalf("temp dir abs error = %v", err)
	}
}

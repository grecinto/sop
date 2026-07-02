package sopbackend

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/memory"
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

	dbOpts, err := GetDBOptions(context.Background(), dbName)
	if err != nil {
		t.Fatalf("GetDBOptions() error = %v", err)
	}
	aidb := database.NewDatabase(dbOpts)
	writeTx, err := aidb.BeginTransaction(context.Background(), sop.ForWriting)
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	defer writeTx.Rollback(context.Background())

	kb, err := aidb.OpenKnowledgeBase(context.Background(), spaceName, writeTx, nil, nil, false)
	if err != nil {
		t.Fatalf("OpenKnowledgeBase() error = %v", err)
	}
	if err := kb.SetConfig(context.Background(), &memory.KnowledgeBaseConfig{LastVectorized: time.Now().Unix()}); err != nil {
		t.Fatalf("SetConfig() error = %v", err)
	}
	if err := writeTx.Commit(context.Background()); err != nil {
		t.Fatalf("Commit() error = %v", err)
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

func TestListSpacesMarksInternalSOPAsNonAISelectable(t *testing.T) {
	oldConfig := config
	t.Cleanup(func() { config = oldConfig })

	tmpDir := t.TempDir()
	dbName := "testdb_internal_sop_rbac"
	spaceName := "SOP"

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

	dbOpts, err := GetDBOptions(context.Background(), dbName)
	if err != nil {
		t.Fatalf("GetDBOptions() error = %v", err)
	}
	aidb := database.NewDatabase(dbOpts)
	writeTx, err := aidb.BeginTransaction(context.Background(), sop.ForWriting)
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	defer writeTx.Rollback(context.Background())

	kb, err := aidb.OpenKnowledgeBase(context.Background(), spaceName, writeTx, nil, nil, false)
	if err != nil {
		t.Fatalf("OpenKnowledgeBase() error = %v", err)
	}
	if err := kb.SetConfig(context.Background(), &memory.KnowledgeBaseConfig{LastVectorized: time.Now().Unix()}); err != nil {
		t.Fatalf("SetConfig() error = %v", err)
	}
	if err := writeTx.Commit(context.Background()); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/spaces?database="+dbName, nil)
	listRR := httptest.NewRecorder()
	handleListSpaces(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list spaces status = %d, body = %s", listRR.Code, listRR.Body.String())
	}

	var listResp struct {
		Data     []string                      `json:"data"`
		ItemRBAC map[string]sop.ContextRBACMap `json:"item_rbac"`
	}
	if err := json.NewDecoder(listRR.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list response error = %v", err)
	}

	if !contains(listResp.Data, spaceName) {
		t.Fatalf("expected space %q in response data, got %#v", spaceName, listResp.Data)
	}
	entry, ok := listResp.ItemRBAC[spaceName]
	if !ok {
		t.Fatalf("expected item_rbac entry for %q, got %#v", spaceName, listResp.ItemRBAC)
	}
	if got, exists := entry[sop.UICapabilityAISelect]; !exists || got {
		t.Fatalf("expected item_rbac[%q][%q] = false, got exists=%v value=%v", spaceName, sop.UICapabilityAISelect, exists, got)
	}
}

func TestListSpacesFiltersOutNonVectorizedSpaces(t *testing.T) {
	oldConfig := config
	t.Cleanup(func() { config = oldConfig })

	tmpDir := t.TempDir()
	dbName := "testdb_vectorized_spaces"
	vectorizedSpace := "vectorized_space"
	nonVectorizedSpace := "non_vectorized_space"

	config = Config{
		Databases: []DatabaseConfig{{Name: dbName, Path: tmpDir, Mode: "standalone"}},
	}

	for _, spaceName := range []string{vectorizedSpace, nonVectorizedSpace} {
		createBody, err := json.Marshal(CreateSpaceRequest{DatabaseName: dbName, SpaceName: spaceName})
		if err != nil {
			t.Fatalf("json.Marshal(create) error = %v", err)
		}
		createReq := httptest.NewRequest(http.MethodPost, "/api/spaces/create", bytes.NewReader(createBody))
		createReq.Header.Set("Content-Type", "application/json")
		createRR := httptest.NewRecorder()
		handleCreateSpace(createRR, createReq)
		if createRR.Code != http.StatusOK {
			t.Fatalf("create space %q status = %d, body = %s", spaceName, createRR.Code, createRR.Body.String())
		}
	}

	dbOpts, err := GetDBOptions(context.Background(), dbName)
	if err != nil {
		t.Fatalf("GetDBOptions() error = %v", err)
	}
	aidb := database.NewDatabase(dbOpts)
	writeTx, err := aidb.BeginTransaction(context.Background(), sop.ForWriting)
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	defer writeTx.Rollback(context.Background())

	vectorizedKB, err := aidb.OpenKnowledgeBase(context.Background(), vectorizedSpace, writeTx, nil, nil, false)
	if err != nil {
		t.Fatalf("OpenKnowledgeBase(vectorized) error = %v", err)
	}
	if err := vectorizedKB.SetConfig(context.Background(), &memory.KnowledgeBaseConfig{LastVectorized: time.Now().Unix()}); err != nil {
		t.Fatalf("SetConfig(vectorized) error = %v", err)
	}
	if err := writeTx.Commit(context.Background()); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/spaces?database="+dbName, nil)
	listRR := httptest.NewRecorder()
	handleListSpaces(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list spaces status = %d, body = %s", listRR.Code, listRR.Body.String())
	}

	var listResp struct {
		Data             []string          `json:"data"`
		DisplayNames     map[string]string `json:"display_names"`
		VectorizedSpaces map[string]bool   `json:"vectorized_spaces"`
	}
	if err := json.NewDecoder(listRR.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list response error = %v", err)
	}

	for _, name := range []string{vectorizedSpace, nonVectorizedSpace} {
		if name == vectorizedSpace {
			if !contains(listResp.Data, name) {
				t.Fatalf("expected vectorized space %q in response data, got %#v", name, listResp.Data)
			}
			continue
		}
		if contains(listResp.Data, name) {
			t.Fatalf("expected non-vectorized space %q to be filtered from response data, got %#v", name, listResp.Data)
		}
	}

	if got := listResp.VectorizedSpaces[vectorizedSpace]; !got {
		t.Fatalf("expected vectorized_spaces[%q] = true, got %v", vectorizedSpace, got)
	}
	if got := listResp.VectorizedSpaces[nonVectorizedSpace]; got {
		t.Fatalf("expected vectorized_spaces[%q] = false, got %v", nonVectorizedSpace, got)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

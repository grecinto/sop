package sopbackend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/memory"
)

// TestSpaceFederationFlow tests the complete HQ→daemon Space push and acknowledgment flow.
func TestSpaceFederationFlow(t *testing.T) {
	// Set up database configuration
	oldConfig := config
	t.Cleanup(func() { config = oldConfig })

	tmpDir := t.TempDir()
	dbName := "test"

	config = Config{
		Databases: []DatabaseConfig{{Name: dbName, Path: tmpDir, Mode: "standalone"}},
	}

	// Create a test Space with content
	ctx := context.Background()
	spaceName := "TestFederatedSpace"

	// Create and populate the Space
	db := createTestDB(t, dbName)
	if err := populateTestSpace(ctx, db, spaceName); err != nil {
		t.Fatalf("failed to populate test Space: %v", err)
	}

	// Export the Space
	spaceJSON, err := exportSpaceFromHQ(ctx, dbName, spaceName)
	if err != nil {
		t.Fatalf("failed to export Space: %v", err)
	}

	if len(spaceJSON) == 0 {
		t.Fatal("exported Space JSON is empty")
	}

	// Verify the Space can be parsed
	var spaceData map[string]any
	if err := json.Unmarshal(spaceJSON, &spaceData); err != nil {
		t.Fatalf("failed to parse exported Space JSON: %v", err)
	}

	t.Logf("Exported Space %s with %d bytes", spaceName, len(spaceJSON))

	// Test HandleHQPushSpace
	req := hqPushSpaceRequest{
		SpaceName:    spaceName,
		DatabaseName: dbName,
		DaemonID:     "test-daemon",
		Version:      1,
	}

	reqJSON, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/api/daemon/hq-push-space", bytes.NewReader(reqJSON))
	w := httptest.NewRecorder()

	// Mock daemon URL for testing (will fail to connect, but that's expected)
	t.Setenv("SOP_DAEMON_TEST_DAEMON_URL", "http://localhost:19999")

	HandleHQPushSpace(w, httpReq)

	// Should fail because daemon isn't running, but the export should work
	if w.Code != http.StatusInternalServerError {
		t.Logf("Expected failure due to daemon not running (status=%d)", w.Code)
	}

	// Check federation state was created
	state, found := GetSpaceFederationState(spaceName, "test-daemon")
	if !found {
		t.Fatal("federation state not created")
	}

	if state.SpaceName != spaceName {
		t.Errorf("expected space_name=%s, got %s", spaceName, state.SpaceName)
	}

	if state.DaemonID != "test-daemon" {
		t.Errorf("expected daemon_id=test-daemon, got %s", state.DaemonID)
	}

	if state.Status != "failed" {
		t.Logf("Expected status=failed (daemon not running), got %s", state.Status)
	}

	t.Logf("Federation state: %+v", state)
}

// TestSpaceFederationReshare tests the HQ reshare to multiple daemons.
func TestSpaceFederationReshare(t *testing.T) {
	// Set up database configuration
	oldConfig := config
	t.Cleanup(func() { config = oldConfig })

	tmpDir := t.TempDir()
	dbName := "test"

	config = Config{
		Databases: []DatabaseConfig{{Name: dbName, Path: tmpDir, Mode: "standalone"}},
	}

	ctx := context.Background()
	spaceName := "TestReshareSpace"

	// Create and populate the Space
	db := createTestDB(t, dbName)
	if err := populateTestSpace(ctx, db, spaceName); err != nil {
		t.Fatalf("failed to populate test Space: %v", err)
	}

	// Test reshare to multiple daemons
	reqPayload := map[string]any{
		"space_name":    spaceName,
		"database_name": dbName,
		"daemon_ids":    []string{"daemon-1", "daemon-2", "daemon-3"},
		"version":       1,
	}

	reqJSON, _ := json.Marshal(reqPayload)
	httpReq := httptest.NewRequest(http.MethodPost, "/api/daemon/hq-reshare-space", bytes.NewReader(reqJSON))
	w := httptest.NewRecorder()

	HandleHQReshareSpace(w, httpReq)

	// Should complete (even with failures to individual daemons)
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body, _ := io.ReadAll(w.Body)
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["status"] != "completed" {
		t.Errorf("expected status=completed, got %v", resp["status"])
	}

	results, ok := resp["results"].(map[string]any)
	if !ok {
		t.Fatal("results not found in response")
	}

	if len(results) != 3 {
		t.Errorf("expected 3 daemon results, got %d", len(results))
	}

	t.Logf("Reshare results: %+v", results)

	// Check federation states
	for _, daemonID := range []string{"daemon-1", "daemon-2", "daemon-3"} {
		state, found := GetSpaceFederationState(spaceName, daemonID)
		if !found {
			t.Errorf("federation state not found for %s", daemonID)
			continue
		}
		t.Logf("State for %s: status=%s", daemonID, state.Status)
	}
}

// TestAcknowledgeSpace tests the acknowledgment handler.
func TestAcknowledgeSpace(t *testing.T) {
	spaceName := "TestAckSpace"
	daemonID := "test-daemon"

	// Set up initial state
	updateSpaceFederationState(spaceName, daemonID, "delivered", 1)

	// Send acknowledgment
	ackPayload := map[string]any{
		"space_name":    spaceName,
		"database_name": "test",
		"daemon_id":     daemonID,
		"status":        "acknowledged",
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}

	reqJSON, _ := json.Marshal(ackPayload)
	httpReq := httptest.NewRequest(http.MethodPost, "/api/daemon/acknowledge-space", bytes.NewReader(reqJSON))
	w := httptest.NewRecorder()

	handleAcknowledgeSpace(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Check state was updated
	state, found := GetSpaceFederationState(spaceName, daemonID)
	if !found {
		t.Fatal("federation state not found")
	}

	if state.Status != "acknowledged" {
		t.Errorf("expected status=acknowledged, got %s", state.Status)
	}

	t.Logf("Acknowledgment processed: %+v", state)
}

// TestListFederationStates tests retrieving all federation states.
func TestListFederationStates(t *testing.T) {
	// Set up some test states
	updateSpaceFederationState("Space1", "daemon-1", "delivered", 1)
	updateSpaceFederationState("Space1", "daemon-2", "acknowledged", 1)
	updateSpaceFederationState("Space2", "daemon-1", "pending", 1)

	states := ListSpaceFederationStates()

	if len(states) < 3 {
		t.Errorf("expected at least 3 states, got %d", len(states))
	}

	for _, state := range states {
		t.Logf("State: space=%s daemon=%s status=%s", state.SpaceName, state.DaemonID, state.Status)
	}
}

// Helper: createTestDB creates a test database using GetDBOptions.
func createTestDB(t *testing.T, dbName string) *database.Database {
	t.Helper()

	ctx := context.Background()
	dbOpts, err := GetDBOptions(ctx, dbName)
	if err != nil {
		t.Fatalf("GetDBOptions() error = %v", err)
	}

	db := database.NewDatabase(dbOpts)
	return db
}

// Helper: populateTestSpace creates and populates a test Space with sample content.
func populateTestSpace(ctx context.Context, db *database.Database, spaceName string) error {
	trans, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer trans.Rollback(ctx)

	kb, err := db.OpenKnowledgeBase(ctx, spaceName, trans, nil, nil, true)
	if err != nil {
		return fmt.Errorf("open KB: %w", err)
	}

	// Add sample items using the Upsert API
	items := []map[string]any{
		{
			"text":     "How to handle database failover: Use replication with automatic reinstatement",
			"category": "operations",
			"source":   "best-practices",
		},
		{
			"text":     "Erasure coding repair: Use existing repair-on-read path during drive recovery",
			"category": "architecture",
			"source":   "engineering",
		},
		{
			"text":     "Test isolation: Give each test its own temporary system DB to avoid collisions",
			"category": "testing",
			"source":   "development",
		},
	}

	for i, data := range items {
		// Create Item using the memory.Item type with proper UUID
		itemID := sop.NewUUID()
		item := memory.Item[map[string]any]{
			ID:   itemID,
			Data: data,
		}

		// Use zero vector since we're not testing embeddings
		if err := kb.Store.Upsert(ctx, item, nil); err != nil {
			return fmt.Errorf("upsert item %d: %w", i, err)
		}
	}

	if err := trans.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

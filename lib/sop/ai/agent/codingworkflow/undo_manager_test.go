package codingworkflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// MockSpaceClient implements SpaceClient for testing.
type MockSpaceClient struct {
	spaces map[string]bool
	items  map[string][]SpaceItem
}

func NewMockSpaceClient() *MockSpaceClient {
	return &MockSpaceClient{
		spaces: make(map[string]bool),
		items:  make(map[string][]SpaceItem),
	}
}

func (m *MockSpaceClient) CreateSpace(ctx context.Context, req SpaceCreateRequest) error {
	m.spaces[req.Name] = true
	m.items[req.Name] = []SpaceItem{}
	return nil
}

func (m *MockSpaceClient) AddItem(ctx context.Context, spaceID string, item SpaceItem) error {
	if !m.spaces[spaceID] {
		return fmt.Errorf("space not found: %s", spaceID)
	}
	m.items[spaceID] = append(m.items[spaceID], item)
	return nil
}

func (m *MockSpaceClient) AddItemToCategory(ctx context.Context, spaceID string, categoryPath []string, item SpaceItem) error {
	item.CategoryPath = categoryPath
	return m.AddItem(ctx, spaceID, item)
}

func (m *MockSpaceClient) SemanticSearch(ctx context.Context, req SpaceSearchRequest) ([]SpaceItem, error) {
	items, ok := m.items[req.SpaceID]
	if !ok {
		return []SpaceItem{}, nil
	}

	// Simple mock: return all items with a relevance score
	results := []SpaceItem{}
	for _, item := range items {
		item.VectorScore = 0.8 // Mock relevance score
		results = append(results, item)
	}

	// Limit results
	if len(results) > req.TopK {
		results = results[:req.TopK]
	}

	return results, nil
}

func (m *MockSpaceClient) GetItem(ctx context.Context, spaceID string, itemID string) (*SpaceItem, error) {
	items, ok := m.items[spaceID]
	if !ok {
		return nil, fmt.Errorf("space not found: %s", spaceID)
	}

	for _, item := range items {
		if item.ID == itemID {
			return &item, nil
		}
	}

	return nil, fmt.Errorf("item not found: %s", itemID)
}

func (m *MockSpaceClient) DeleteItem(ctx context.Context, spaceID string, itemID string) error {
	items, ok := m.items[spaceID]
	if !ok {
		return fmt.Errorf("space not found: %s", spaceID)
	}

	filtered := []SpaceItem{}
	for _, item := range items {
		if item.ID != itemID {
			filtered = append(filtered, item)
		}
	}

	m.items[spaceID] = filtered
	return nil
}

func (m *MockSpaceClient) EnableFederation(ctx context.Context, spaceID string, userID string) error {
	// Mock: no-op
	return nil
}

func TestUndoManagerInitialization(t *testing.T) {
	tmpDir := t.TempDir()
	spaceClient := NewMockSpaceClient()

	um, err := NewUndoManager(tmpDir, "testuser", spaceClient, nil)
	if err != nil {
		t.Fatalf("NewUndoManager failed: %v", err)
	}

	if um == nil {
		t.Fatal("UndoManager is nil")
	}

	// Verify undo directory was created
	undoDir := filepath.Join(tmpDir, ".sop", "undo", "transactions")
	if _, err := os.Stat(undoDir); os.IsNotExist(err) {
		t.Errorf("Undo directory not created: %s", undoDir)
	}

	// Verify Space was created
	if !spaceClient.spaces["testuser_workflow_undo"] {
		t.Error("Personal undo Space was not created")
	}
}

func TestTransactionLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	spaceClient := NewMockSpaceClient()
	artifactExtractor := NewCodeArtifactExtractor(nil)

	um, err := NewUndoManager(tmpDir, "testuser", spaceClient, artifactExtractor)
	if err != nil {
		t.Fatalf("NewUndoManager failed: %v", err)
	}

	ctx := context.Background()

	// Begin transaction
	txn, err := um.BeginTransaction(ctx, "Test workflow", "test_fix")
	if err != nil {
		t.Fatalf("BeginTransaction failed: %v", err)
	}

	if txn.ID == "" {
		t.Error("Transaction ID is empty")
	}
	if txn.Status != "in_progress" {
		t.Errorf("Expected status 'in_progress', got '%s'", txn.Status)
	}

	// Create a test file to backup
	testFile := "test.txt"
	testContent := []byte("original content")
	fullPath := filepath.Join(tmpDir, testFile)
	if err := os.WriteFile(fullPath, testContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Backup file
	if err := um.BackupFile(ctx, testFile); err != nil {
		t.Fatalf("BackupFile failed: %v", err)
	}

	// Modify file
	modifiedContent := []byte("modified content")
	if err := os.WriteFile(fullPath, modifiedContent, 0644); err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	// End transaction
	if err := um.EndTransaction(ctx, true, 5*time.Second, 3, 0); err != nil {
		t.Fatalf("EndTransaction failed: %v", err)
	}

	// Verify transaction was stored in Space
	items := spaceClient.items["testuser_workflow_undo"]
	if len(items) == 0 {
		t.Fatal("No items stored in Space")
	}

	// Verify transaction metadata
	item := items[0]
	if item.ID != txn.ID {
		t.Errorf("Expected item ID %s, got %s", txn.ID, item.ID)
	}

	// Verify backup file exists
	if len(txn.ModifiedFiles) == 0 {
		t.Fatal("No modified files recorded")
	}

	record := txn.ModifiedFiles[0]
	if _, err := os.Stat(record.BackupPath); os.IsNotExist(err) {
		t.Errorf("Backup file not found: %s", record.BackupPath)
	}
}

func TestSemanticSearch(t *testing.T) {
	tmpDir := t.TempDir()
	spaceClient := NewMockSpaceClient()

	um, err := NewUndoManager(tmpDir, "testuser", spaceClient, nil)
	if err != nil {
		t.Fatalf("NewUndoManager failed: %v", err)
	}

	ctx := context.Background()

	// Create a few transactions
	for i := 0; i < 3; i++ {
		_, _ = um.BeginTransaction(ctx, fmt.Sprintf("Test workflow %d", i), "test_fix")

		// Create and backup a test file
		testFile := fmt.Sprintf("test%d.txt", i)
		fullPath := filepath.Join(tmpDir, testFile)
		os.WriteFile(fullPath, []byte("content"), 0644)
		um.BackupFile(ctx, testFile)

		// Modify file
		os.WriteFile(fullPath, []byte("modified"), 0644)

		um.EndTransaction(ctx, true, time.Second, 1, 0)

		// Sleep to ensure different timestamps
		time.Sleep(10 * time.Millisecond)
	}

	// Perform semantic search
	query := UndoQuery{
		SemanticQuery:   "test workflow",
		RankBy:          RankByHybrid,
		RecencyWeight:   0.3,
		RelevanceWeight: 0.7,
		Limit:           10,
	}

	results, err := um.SemanticSearch(ctx, query)
	if err != nil {
		t.Fatalf("SemanticSearch failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("Expected results, got none")
	}

	// Note: Due to multi-path storage, we may get duplicate items
	// Just verify we got some results and they're time-ranked
	if len(results) < 3 {
		t.Errorf("Expected at least 3 results, got %d", len(results))
	}

	// Verify results are time-ranked (newest first for hybrid with recency)
	for i := 0; i < len(results)-1; i++ {
		if results[i].Timestamp.Before(results[i+1].Timestamp) {
			t.Error("Results are not properly time-ranked (newest should be first)")
		}
	}
}

func TestRestoreTransaction(t *testing.T) {
	tmpDir := t.TempDir()
	spaceClient := NewMockSpaceClient()

	um, err := NewUndoManager(tmpDir, "testuser", spaceClient, nil)
	if err != nil {
		t.Fatalf("NewUndoManager failed: %v", err)
	}

	ctx := context.Background()

	// Create original file
	testFile := "test.txt"
	originalContent := []byte("original content")
	fullPath := filepath.Join(tmpDir, testFile)
	if err := os.WriteFile(fullPath, originalContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Begin transaction and backup
	txn, _ := um.BeginTransaction(ctx, "Test modification", "test_fix")
	um.BackupFile(ctx, testFile)

	// Modify file
	modifiedContent := []byte("modified content")
	os.WriteFile(fullPath, modifiedContent, 0644)

	// End transaction
	um.EndTransaction(ctx, true, time.Second, 1, 0)

	// Verify file is modified
	content, _ := os.ReadFile(fullPath)
	if string(content) != string(modifiedContent) {
		t.Error("File was not modified as expected")
	}

	// Restore transaction
	options := RestoreOptions{
		Mode:         RestoreFull,
		VerifyHashes: true,
	}

	if err := um.RestoreTransaction(ctx, txn.ID, options); err != nil {
		t.Fatalf("RestoreTransaction failed: %v", err)
	}

	// Verify file is restored to original
	restoredContent, _ := os.ReadFile(fullPath)
	if string(restoredContent) != string(originalContent) {
		t.Errorf("File was not restored. Expected '%s', got '%s'", originalContent, restoredContent)
	}
}

func TestTransactionStats(t *testing.T) {
	tmpDir := t.TempDir()
	spaceClient := NewMockSpaceClient()

	um, err := NewUndoManager(tmpDir, "testuser", spaceClient, nil)
	if err != nil {
		t.Fatalf("NewUndoManager failed: %v", err)
	}

	ctx := context.Background()

	// Create a few transactions
	for i := 0; i < 5; i++ {
		txn, _ := um.BeginTransaction(ctx, fmt.Sprintf("Test %d", i), "test_fix")
		_ = txn // Suppress unused warning

		testFile := fmt.Sprintf("test%d.txt", i)
		fullPath := filepath.Join(tmpDir, testFile)
		os.WriteFile(fullPath, []byte("content"), 0644)
		um.BackupFile(ctx, testFile)
		os.WriteFile(fullPath, []byte("modified"), 0644)

		// Mark some as failed
		success := i%2 == 0
		um.EndTransaction(ctx, success, time.Second, 1, 0)
	}

	// Get stats
	stats, err := um.GetTransactionStats(ctx)
	if err != nil {
		t.Fatalf("GetTransactionStats failed: %v", err)
	}

	if stats.TotalTransactions != 5 {
		t.Errorf("Expected 5 transactions, got %d", stats.TotalTransactions)
	}

	// Note: Failed transactions are auto-rolled back, so they count as undone, not failed
	// Successful ones: i=0,2,4 (3 total)
	if stats.SuccessfulCount != 3 {
		t.Errorf("Expected 3 successful, got %d", stats.SuccessfulCount)
	}

	// Failed transactions are auto-rolled back and marked as undone
	if stats.UndoneCount != 2 {
		t.Errorf("Expected 2 undone (auto-rolled back), got %d", stats.UndoneCount)
	}
}

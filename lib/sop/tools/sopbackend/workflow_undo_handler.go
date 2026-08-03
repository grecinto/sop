package sopbackend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"
	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai/agent/codingworkflow"
	aidb "github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/embed"
	"github.com/sharedcode/sop/ai/memory"
	"github.com/sharedcode/sop/database"
)

// UndoRecentResponse represents the recent undo history response
type UndoRecentResponse struct {
	Success      bool                                 `json:"success"`
	Message      string                               `json:"message,omitempty"`
	Transactions []codingworkflow.WorkflowTransaction `json:"transactions,omitempty"`
	Total        int                                  `json:"total"`
	Summary      []UndoHistoryItem                    `json:"summary,omitempty"`
}

// UndoHistoryItem is a UI-friendly summary for the undo history timeline.
type UndoHistoryItem struct {
	TransactionID string                 `json:"transaction_id"`
	SessionID     string                 `json:"session_id,omitempty"`
	WorkstreamID  string                 `json:"workstream_id,omitempty"`
	GroupKey      string                 `json:"group_key,omitempty"`
	Timestamp     string                 `json:"timestamp"`
	Description   string                 `json:"description"`
	WorkflowType  string                 `json:"workflow_type"`
	Status        string                 `json:"status"`
	Summary       string                 `json:"summary"`
	Files         []UndoFileSummary      `json:"files,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// UndoFileSummary is a compact per-file diff summary for UI cards.
type UndoFileSummary struct {
	FilePath    string            `json:"file_path"`
	ChangeType  string            `json:"change_type"`
	DiffSummary string            `json:"diff_summary,omitempty"`
	DiffStats   string            `json:"diff_stats,omitempty"`
	DeltaFormat string            `json:"delta_format,omitempty"`
	Preview     string            `json:"preview,omitempty"`
	DiffPreview string            `json:"diff_preview,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// UndoRestoreRequest represents an undo restore request
type UndoRestoreRequest struct {
	TransactionID string `json:"transaction_id"`
	CreateBackup  bool   `json:"create_backup"`
	DryRun        bool   `json:"dry_run,omitempty"`
	VerifyHashes  bool   `json:"verify_hashes,omitempty"`
}

// UndoRestoreResponse represents an undo restore response
type UndoRestoreResponse struct {
	Success       bool     `json:"success"`
	Message       string   `json:"message,omitempty"`
	TransactionID string   `json:"transaction_id,omitempty"`
	FilesRestored int      `json:"files_restored,omitempty"`
	RestoredFiles []string `json:"restored_files,omitempty"`
}

// UndoSearchResponse represents an undo search response
type UndoSearchResponse struct {
	Success      bool                                 `json:"success"`
	Message      string                               `json:"message,omitempty"`
	Transactions []codingworkflow.WorkflowTransaction `json:"transactions,omitempty"`
	Total        int                                  `json:"total"`
	Query        string                               `json:"query,omitempty"`
}

// UndoStatsResponse represents undo system statistics
type UndoStatsResponse struct {
	Success bool                             `json:"success"`
	Message string                           `json:"message,omitempty"`
	Stats   *codingworkflow.TransactionStats `json:"stats,omitempty"`
}

// handleUndoRecent returns recent undo transactions
func handleUndoRecent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse limit parameter
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	// Get undo manager
	undoMgr, err := getUndoManager(r)
	if err != nil {
		json.NewEncoder(w).Encode(UndoRecentResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to get undo manager: %v", err),
		})
		return
	}

	// Query recent transactions
	ctx := context.Background()
	query := codingworkflow.UndoQuery{
		SemanticQuery: "", // Empty query returns all
		Limit:         limit,
		RankBy:        codingworkflow.RankByTimeDesc,
	}

	transactions, err := undoMgr.SemanticSearch(ctx, query)
	if err != nil {
		json.NewEncoder(w).Encode(UndoRecentResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to query transactions: %v", err),
		})
		return
	}

	summary := buildUndoHistorySummary(transactions, getUndoWorkingDir(r))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(UndoRecentResponse{
		Success:      true,
		Transactions: transactions,
		Total:        len(transactions),
		Summary:      summary,
	})
}

// handleUndoRestore restores a transaction
func handleUndoRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request
	var req UndoRestoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(UndoRestoreResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	// Extract transaction ID from URL path if not in body
	if req.TransactionID == "" {
		// Parse from /api/workflow/undo/restore/{txnId}
		pathParts := strings.Split(r.URL.Path, "/")
		if len(pathParts) >= 6 {
			req.TransactionID = pathParts[5]
		}
	}

	if req.TransactionID == "" {
		json.NewEncoder(w).Encode(UndoRestoreResponse{
			Success: false,
			Message: "Transaction ID is required",
		})
		return
	}

	// Get undo manager
	undoMgr, err := getUndoManager(r)
	if err != nil {
		json.NewEncoder(w).Encode(UndoRestoreResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to get undo manager: %v", err),
		})
		return
	}

	// Restore transaction
	ctx := context.Background()
	opts := codingworkflow.RestoreOptions{
		Mode:         codingworkflow.RestoreFull,
		DryRun:       req.DryRun,
		CreateBackup: req.CreateBackup,
		VerifyHashes: req.VerifyHashes,
	}

	err = undoMgr.RestoreTransaction(ctx, req.TransactionID, opts)
	if err != nil {
		json.NewEncoder(w).Encode(UndoRestoreResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to restore transaction: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(UndoRestoreResponse{
		Success:       true,
		Message:       "Transaction restored successfully",
		TransactionID: req.TransactionID,
		FilesRestored: 0,          // TODO: Track number of restored files
		RestoredFiles: []string{}, // TODO: Return list of restored files
	})
}

// handleUndoSearch performs semantic search on undo history
func handleUndoSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters
	searchQuery := r.URL.Query().Get("q")
	if searchQuery == "" {
		json.NewEncoder(w).Encode(UndoSearchResponse{
			Success: false,
			Message: "Query parameter 'q' is required",
		})
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	// Get undo manager
	undoMgr, err := getUndoManager(r)
	if err != nil {
		json.NewEncoder(w).Encode(UndoSearchResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to get undo manager: %v", err),
		})
		return
	}

	// Perform semantic search
	ctx := context.Background()
	query := codingworkflow.UndoQuery{
		SemanticQuery:   searchQuery,
		Limit:           limit,
		RankBy:          codingworkflow.RankByHybrid,
		RecencyWeight:   0.3,
		RelevanceWeight: 0.7,
	}

	// Parse optional filters
	if workflowType := r.URL.Query().Get("workflow_type"); workflowType != "" {
		query.WorkflowType = workflowType
	}
	if status := r.URL.Query().Get("status"); status != "" {
		query.Status = status
	}
	if dateRange := r.URL.Query().Get("date_range"); dateRange != "" {
		query.DateRange = dateRange
	}

	transactions, err := undoMgr.SemanticSearch(ctx, query)
	if err != nil {
		json.NewEncoder(w).Encode(UndoSearchResponse{
			Success: false,
			Message: fmt.Sprintf("Search failed: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(UndoSearchResponse{
		Success:      true,
		Transactions: transactions,
		Total:        len(transactions),
		Query:        searchQuery,
	})
}

// handleUndoStats returns undo system statistics
func buildUndoHistorySummary(transactions []codingworkflow.WorkflowTransaction, workingDir string) []UndoHistoryItem {
	items := make([]UndoHistoryItem, 0, len(transactions))
	for _, txn := range transactions {
		files := make([]UndoFileSummary, 0, len(txn.ModifiedFiles))
		for _, record := range txn.ModifiedFiles {
			files = append(files, UndoFileSummary{
				FilePath:    record.FilePath,
				ChangeType:  record.ChangeType,
				DiffSummary: record.DiffSummary,
				DiffStats:   record.DiffStats,
				DeltaFormat: record.DeltaFormat,
				Preview:     buildDeltaPreview(record.Delta),
				DiffPreview: buildFileDiffPreview(record, workingDir),
				Metadata:    record.Metadata,
			})
		}

		summaryText := txn.Description
		if txn.WorkflowType != "" {
			summaryText = fmt.Sprintf("%s · %s", txn.Description, txn.WorkflowType)
		}

		groupKey := txn.WorkstreamID
		if groupKey == "" {
			groupKey = txn.SessionID
		}

		items = append(items, UndoHistoryItem{
			TransactionID: txn.ID,
			SessionID:     txn.SessionID,
			WorkstreamID:  txn.WorkstreamID,
			GroupKey:      groupKey,
			Timestamp:     txn.Timestamp.Format(time.RFC3339),
			Description:   txn.Description,
			WorkflowType:  txn.WorkflowType,
			Status:        txn.Status,
			Summary:       summaryText,
			Files:         files,
			Metadata: map[string]interface{}{
				"modified_file_count": len(txn.ModifiedFiles),
				"execution_time":      txn.ExecutionTime,
				"tags":                txn.Tags,
				"session_id":          txn.SessionID,
				"workstream_id":       txn.WorkstreamID,
			},
		})
	}
	return items
}

func buildDeltaPreview(delta string) string {
	if strings.TrimSpace(delta) == "" {
		return ""
	}

	lines := strings.Split(strings.ReplaceAll(delta, "\r\n", "\n"), "\n")
	previewLines := make([]string, 0, 3)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		previewLines = append(previewLines, trimmed)
		if len(previewLines) == 3 {
			break
		}
	}

	if len(previewLines) == 0 {
		return ""
	}

	preview := strings.Join(previewLines, " | ")
	if len(preview) > 140 {
		preview = preview[:137] + "..."
	}
	return preview
}

func buildFileDiffPreview(record codingworkflow.ModifiedFileRecord, workingDir string) string {
	if workingDir == "" {
		workingDir = "."
	}

	basePath := filepath.Clean(workingDir)
	fullPath := filepath.Join(basePath, record.FilePath)

	oldText := record.Delta
	newText := ""
	if record.ChangeType == "delete" {
		newText = ""
	} else {
		content, err := os.ReadFile(fullPath)
		if err == nil {
			newText = string(content)
		} else if !os.IsNotExist(err) {
			return ""
		}
	}

	if strings.TrimSpace(oldText) == "" && strings.TrimSpace(newText) == "" {
		return ""
	}

	if strings.TrimSpace(oldText) == "" {
		oldText = ""
	}

	dmp := diffmatchpatch.New()
	patches := dmp.PatchMake(oldText, newText)
	preview := dmp.PatchToText(patches)
	if preview == "" {
		preview = fmt.Sprintf("--- %s\n+++ %s\n", record.FilePath, record.FilePath)
		if oldText != "" {
			preview += "-" + oldText + "\n"
		}
		if newText != "" {
			preview += "+" + newText + "\n"
		}
	}
	return preview
}

func getUndoWorkingDir(r *http.Request) string {
	cfg := GetConfig()
	workingDir := "."
	if cfg.SystemDB != nil && cfg.SystemDB.Path != "" {
		workingDir = cfg.SystemDB.Path
	} else if len(cfg.Databases) > 0 && cfg.Databases[0].Path != "" {
		workingDir = cfg.Databases[0].Path
	}
	return workingDir
}

func handleUndoStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get undo manager
	undoMgr, err := getUndoManager(r)
	if err != nil {
		json.NewEncoder(w).Encode(UndoStatsResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to get undo manager: %v", err),
		})
		return
	}

	// Get statistics
	ctx := context.Background()
	stats, err := undoMgr.GetTransactionStats(ctx)
	if err != nil {
		json.NewEncoder(w).Encode(UndoStatsResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to get statistics: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(UndoStatsResponse{
		Success: true,
		Stats:   stats,
	})
}

// Global cache for UndoManager instances (one per user)
var (
	undoManagerCache   = make(map[string]*codingworkflow.UndoManager)
	undoManagerCacheMu sync.RWMutex
)

// getUndoManager retrieves or creates the UndoManager for the current user
func getUndoManager(r *http.Request) (*codingworkflow.UndoManager, error) {
	authCtx := sop.GetAuthFromContext(r.Context())
	userName := authCtx.UserID
	if userName == "" {
		return nil, fmt.Errorf("user not authenticated")
	}

	undoManagerCacheMu.RLock()
	if mgr, exists := undoManagerCache[userName]; exists {
		undoManagerCacheMu.RUnlock()
		return mgr, nil
	}
	undoManagerCacheMu.RUnlock()

	undoManagerCacheMu.Lock()
	defer undoManagerCacheMu.Unlock()

	if mgr, exists := undoManagerCache[userName]; exists {
		return mgr, nil
	}

	workingDir := getUndoWorkingDir(r)

	spaceClient := newRealSpaceClient(r)
	artifactExtractor := codingworkflow.NewCodeArtifactExtractor(codingworkflow.NewGrammarRegistry())

	mgr, err := codingworkflow.NewUndoManager(workingDir, userName, spaceClient, artifactExtractor)
	if err != nil {
		return nil, fmt.Errorf("failed to create undo manager: %w", err)
	}

	undoManagerCache[userName] = mgr
	return mgr, nil
}

// realSpaceClient implements SpaceClient using the actual SOP database backend
type realSpaceClient struct {
	request *http.Request
}

func newRealSpaceClient(r *http.Request) codingworkflow.SpaceClient {
	return &realSpaceClient{request: r}
}

func (c *realSpaceClient) CreateSpace(ctx context.Context, req codingworkflow.SpaceCreateRequest) error {
	dbOpts, err := GetDBOptions(ctx, SystemDBName)
	if err != nil {
		return fmt.Errorf("get db options: %w", err)
	}
	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer trans.Rollback(ctx)
	db := aidb.NewDatabase(dbOpts)
	llm := GetConfiguredLLM(c.request)
	embedder := GetConfiguredEmbedder(c.request)
	_, err = db.OpenKnowledgeBase(ctx, req.Name, trans, llm, embedder, true)
	if err != nil {
		return fmt.Errorf("create space: %w", err)
	}
	return trans.Commit(ctx)
}

func (c *realSpaceClient) AddItem(ctx context.Context, spaceID string, item codingworkflow.SpaceItem) error {
	return c.AddItemToCategory(ctx, spaceID, item.CategoryPath, item)
}

func (c *realSpaceClient) AddItemToCategory(ctx context.Context, spaceID string, categoryPath []string, item codingworkflow.SpaceItem) error {
	dbOpts, err := GetDBOptions(ctx, SystemDBName)
	if err != nil {
		return fmt.Errorf("get db options: %w", err)
	}
	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer trans.Rollback(ctx)
	db := aidb.NewDatabase(dbOpts)
	llm := GetConfiguredLLM(c.request)
	embedder := GetConfiguredEmbedder(c.request)
	kb, err := db.OpenKnowledgeBase(ctx, spaceID, trans, llm, embedder, true)
	if err != nil {
		return fmt.Errorf("open space: %w", err)
	}
	catPathStr := strings.Join(categoryPath, "/")
	itemUUID, uuidErr := sop.ParseUUID(item.ID)
	if uuidErr != nil {
		return fmt.Errorf("invalid item ID: %w", uuidErr)
	}
	memItem := &memory.Item[map[string]any]{
		ID:   itemUUID,
		Data: item.Metadata,
	}
	if memItem.Data == nil {
		memItem.Data = make(map[string]any)
	}
	memItem.Data["title"] = item.Title
	memItem.Data["content"] = item.Content
	params := []memory.UpsertItemParam[map[string]any]{{CategoryPath: catPathStr, Item: memItem}}
	if err := kb.UpsertItems(ctx, params); err != nil {
		return fmt.Errorf("upsert item: %w", err)
	}
	return trans.Commit(ctx)
}

func (c *realSpaceClient) SemanticSearch(ctx context.Context, req codingworkflow.SpaceSearchRequest) ([]codingworkflow.SpaceItem, error) {
	dbOpts, err := GetDBOptions(ctx, SystemDBName)
	if err != nil {
		return nil, fmt.Errorf("get db options: %w", err)
	}
	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForReading)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer trans.Rollback(ctx)
	db := aidb.NewDatabase(dbOpts)
	llm := GetConfiguredLLM(c.request)
	embedder := GetConfiguredEmbedder(c.request)
	kb, err := db.OpenKnowledgeBase(ctx, req.SpaceID, trans, llm, embedder, false)
	if err != nil {
		return nil, fmt.Errorf("open space: %w", err)
	}
	queryVecs, err := embed.QueryTexts(ctx, embedder, []string{req.Query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(queryVecs) == 0 {
		return []codingworkflow.SpaceItem{}, nil
	}
	topK := req.TopK
	if topK == 0 {
		topK = 20
	}
	opts := &memory.SearchOptions[map[string]any]{Limit: topK}
	hits, err := kb.Store.Query(ctx, queryVecs[0], opts)
	if err != nil {
		return nil, fmt.Errorf("query space: %w", err)
	}
	items := make([]codingworkflow.SpaceItem, 0, len(hits))
	for _, hit := range hits {
		item := codingworkflow.SpaceItem{
			ID:          hit.ID,
			VectorScore: float64(hit.Score),
			Metadata:    hit.Payload,
		}
		if title, ok := hit.Payload["title"].(string); ok {
			item.Title = title
		}
		if content, ok := hit.Payload["content"].(string); ok {
			item.Content = content
		}
		items = append(items, item)
	}
	return items, nil
}

func (c *realSpaceClient) GetItem(ctx context.Context, spaceID string, itemID string) (*codingworkflow.SpaceItem, error) {
	dbOpts, err := GetDBOptions(ctx, SystemDBName)
	if err != nil {
		return nil, fmt.Errorf("get db options: %w", err)
	}
	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForReading)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer trans.Rollback(ctx)
	db := aidb.NewDatabase(dbOpts)
	llm := GetConfiguredLLM(c.request)
	embedder := GetConfiguredEmbedder(c.request)
	kb, err := db.OpenKnowledgeBase(ctx, spaceID, trans, llm, embedder, false)
	if err != nil {
		return nil, fmt.Errorf("open space: %w", err)
	}
	itemUUID, uuidErr := sop.ParseUUID(itemID)
	if uuidErr != nil {
		return nil, fmt.Errorf("invalid item ID: %w", uuidErr)
	}
	mem, err := kb.Store.Get(ctx, memory.ItemKey{ItemID: itemUUID})
	if err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}
	if mem == nil {
		return nil, fmt.Errorf("item not found")
	}
	item := &codingworkflow.SpaceItem{ID: mem.ID.String(), Metadata: mem.Data}
	if title, ok := mem.Data["title"].(string); ok {
		item.Title = title
	}
	if content, ok := mem.Data["content"].(string); ok {
		item.Content = content
	}
	return item, nil
}

func (c *realSpaceClient) DeleteItem(ctx context.Context, spaceID string, itemID string) error {
	dbOpts, err := GetDBOptions(ctx, SystemDBName)
	if err != nil {
		return fmt.Errorf("get db options: %w", err)
	}
	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer trans.Rollback(ctx)
	db := aidb.NewDatabase(dbOpts)
	llm := GetConfiguredLLM(c.request)
	embedder := GetConfiguredEmbedder(c.request)
	kb, err := db.OpenKnowledgeBase(ctx, spaceID, trans, llm, embedder, false)
	if err != nil {
		return fmt.Errorf("open space: %w", err)
	}
	itemUUID, uuidErr := sop.ParseUUID(itemID)
	if uuidErr != nil {
		return fmt.Errorf("invalid item ID: %w", uuidErr)
	}
	if err := kb.Store.Delete(ctx, memory.ItemKey{ItemID: itemUUID}); err != nil {
		return fmt.Errorf("delete item: %w", err)
	}
	return trans.Commit(ctx)
}

func (c *realSpaceClient) EnableFederation(ctx context.Context, spaceID string, userID string) error {
	// TODO: Implement federation enablement
	// For now, this is a no-op as federation is configured separately
	return nil
}

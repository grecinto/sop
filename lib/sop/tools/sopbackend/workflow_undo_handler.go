package sopbackend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(UndoRecentResponse{
		Success:      true,
		Transactions: transactions,
		Total:        len(transactions),
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

	cfg := GetConfig()
	workingDir := "."
	if cfg.SystemDB != nil && cfg.SystemDB.Path != "" {
		workingDir = cfg.SystemDB.Path
	} else if len(cfg.Databases) > 0 && cfg.Databases[0].Path != "" {
		workingDir = cfg.Databases[0].Path
	}

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

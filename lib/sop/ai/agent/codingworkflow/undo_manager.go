package codingworkflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sharedcode/sop/ai/memory"
)

// UndoManager manages workflow transaction lifecycle and undo operations.
// It integrates with Space for semantic search and uses the filesystem for backup storage.
type UndoManager struct {
	workingDir string
	userName   string
	spaceID    string // Per-user Space: "{userName}_workflow_undo"

	// Dependencies
	spaceClient     SpaceClient
	artifactExtract *CodeArtifactExtractor

	// Configuration
	retentionPolicy RetentionPolicy

	// State
	currentTxn *WorkflowTransaction
	mu         sync.RWMutex
}

// SpaceClient defines the interface for Space operations.
// This will be implemented by the actual Space backend.
type SpaceClient interface {
	CreateSpace(ctx context.Context, req SpaceCreateRequest) error
	AddItem(ctx context.Context, spaceID string, item SpaceItem) error
	AddItemToCategory(ctx context.Context, spaceID string, categoryPath []string, item SpaceItem) error
	SemanticSearch(ctx context.Context, req SpaceSearchRequest) ([]SpaceItem, error)
	GetItem(ctx context.Context, spaceID string, itemID string) (*SpaceItem, error)
	DeleteItem(ctx context.Context, spaceID string, itemID string) error
	EnableFederation(ctx context.Context, spaceID string, userID string) error
}

// SpaceCreateRequest represents a Space creation request.
type SpaceCreateRequest struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Type        string `json:"type"`
	Private     bool   `json:"private"`
	Owner       string `json:"owner"`
}

// SpaceItem represents an item stored in a Space.
type SpaceItem struct {
	ID                  string                     `json:"id"`
	CategoryPath        []string                   `json:"category_path"`
	Title               string                     `json:"title"`
	Content             string                     `json:"content"`
	Metadata            map[string]interface{}     `json:"metadata"`
	VectorScore         float64                    `json:"vector_score,omitempty"` // From semantic search
	IndexPopulationMode memory.IndexPopulationMode `json:"index_population_mode,omitempty"`
}

// SpaceSearchRequest represents a semantic search query.
type SpaceSearchRequest struct {
	SpaceID string                 `json:"space_id"`
	Query   string                 `json:"query"`
	Filters map[string]interface{} `json:"filters,omitempty"`
	TopK    int                    `json:"top_k"`
}

// NewUndoManager creates a new UndoManager for a user.
func NewUndoManager(workingDir string, userName string, spaceClient SpaceClient, artifactExtractor *CodeArtifactExtractor) (*UndoManager, error) {
	if workingDir == "" {
		return nil, fmt.Errorf("working directory is required")
	}
	if userName == "" {
		return nil, fmt.Errorf("user name is required")
	}

	// Normalize user name for Space ID
	normalizedUser := strings.ToLower(strings.ReplaceAll(userName, " ", "_"))
	spaceID := fmt.Sprintf("%s_workflow_undo", normalizedUser)

	um := &UndoManager{
		workingDir:      workingDir,
		userName:        userName,
		spaceID:         spaceID,
		spaceClient:     spaceClient,
		artifactExtract: artifactExtractor,
		retentionPolicy: DefaultRetentionPolicy(),
	}

	// Initialize undo directory structure
	if err := um.initializeDirectories(); err != nil {
		return nil, fmt.Errorf("initialize directories: %w", err)
	}

	// Create or ensure personal undo Space exists
	if err := um.ensureUndoSpace(context.Background()); err != nil {
		return nil, fmt.Errorf("ensure undo space: %w", err)
	}

	return um, nil
}

// initializeDirectories creates the undo directory structure.
func (um *UndoManager) initializeDirectories() error {
	undoDir := filepath.Join(um.workingDir, ".sop", "undo")

	dirs := []string{
		undoDir,
		filepath.Join(undoDir, "transactions"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}

	return nil
}

// ensureUndoSpace creates the personal undo Space if it doesn't exist.
func (um *UndoManager) ensureUndoSpace(ctx context.Context) error {
	// Try to create Space (idempotent operation)
	displayName := titleCase(um.userName) + " Workflow Undo"

	err := um.spaceClient.CreateSpace(ctx, SpaceCreateRequest{
		Name:        um.spaceID,
		DisplayName: displayName,
		Type:        "workflow_undo",
		Private:     true,
		Owner:       um.userName,
	})

	// Ignore "already exists" errors
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}

	// Enable federation for syncing across user's devices
	if err := um.spaceClient.EnableFederation(ctx, um.spaceID, um.userName); err != nil {
		// Log but don't fail - federation is optional
		// TODO: Add proper logging
	}

	return nil
}

// BeginTransaction starts a new workflow transaction.
func (um *UndoManager) BeginTransaction(ctx context.Context, description string, workflowType string) (*WorkflowTransaction, error) {
	um.mu.Lock()
	defer um.mu.Unlock()

	if um.currentTxn != nil {
		return nil, fmt.Errorf("transaction already in progress: %s", um.currentTxn.ID)
	}

	txn := &WorkflowTransaction{
		ID:            generateTransactionID(),
		Timestamp:     time.Now(),
		Description:   description,
		Status:        "in_progress",
		WorkflowType:  workflowType,
		UserName:      um.userName,
		TriggeredBy:   "user",
		ModifiedFiles: []ModifiedFileRecord{},
		Context:       make(map[string]interface{}),
	}

	// Create transaction directory
	txnDir := um.getTransactionDir(txn.ID)
	backupDir := filepath.Join(txnDir, "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return nil, fmt.Errorf("create transaction directory: %w", err)
	}

	txn.BackupPath = backupDir
	um.currentTxn = txn

	return txn, nil
}

// BackupFile creates a backup of a file before modification.
func (um *UndoManager) BackupFile(ctx context.Context, filePath string) error {
	um.mu.Lock()
	defer um.mu.Unlock()

	if um.currentTxn == nil {
		return fmt.Errorf("no transaction in progress")
	}

	// Check if already backed up
	for _, record := range um.currentTxn.ModifiedFiles {
		if record.FilePath == filePath {
			// Already backed up
			return nil
		}
	}

	// Read original content
	fullPath := filepath.Join(um.workingDir, filePath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		// File might not exist (new file case)
		if os.IsNotExist(err) {
			// Record as new file creation
			um.currentTxn.ModifiedFiles = append(um.currentTxn.ModifiedFiles, ModifiedFileRecord{
				FilePath:     filePath,
				BackupPath:   "",
				OriginalHash: "",
				Timestamp:    time.Now(),
				ChangeType:   "create",
				SizeBytes:    0,
			})
			return nil
		}
		return fmt.Errorf("read file %s: %w", filePath, err)
	}

	// Calculate hash
	hash := sha256.Sum256(content)
	originalHash := hex.EncodeToString(hash[:])

	// Capture a compact delta payload instead of a full-file backup.
	delta := string(content)
	deltaFormat := "text-full"
	if len(content) > 0 {
		deltaFormat = "text-raw"
	}

	// Get file info
	fileInfo, _ := os.Stat(fullPath)
	sizeBytes := int64(0)
	if fileInfo != nil {
		sizeBytes = fileInfo.Size()
	}

	// Record modification
	record := ModifiedFileRecord{
		FilePath:     filePath,
		BackupPath:   "",
		OriginalHash: originalHash,
		Timestamp:    time.Now(),
		ChangeType:   "modify",
		SizeBytes:    sizeBytes,
		Delta:        delta,
		DeltaFormat:  deltaFormat,
		DiffSummary:  fmt.Sprintf("tracked %s for diff replay", filePath),
		DiffStats:    "+0 -0",
		Metadata: map[string]string{
			"kind":      "text",
			"source":    "undo-manager",
			"format":    deltaFormat,
			"file_path": filePath,
		},
	}

	um.currentTxn.ModifiedFiles = append(um.currentTxn.ModifiedFiles, record)

	return nil
}

// EndTransaction completes a transaction and stores it in Space.
func (um *UndoManager) EndTransaction(ctx context.Context, success bool, executionTime time.Duration, stepCount int, retryCount int) error {
	um.mu.Lock()

	if um.currentTxn == nil {
		um.mu.Unlock()
		return fmt.Errorf("no transaction in progress")
	}

	txn := um.currentTxn
	um.currentTxn = nil
	um.mu.Unlock()

	// Update transaction metadata
	txn.ExecutionTime = executionTime.String()
	txn.StepCount = stepCount
	txn.RetryCount = retryCount

	if success {
		txn.Status = "completed"
	} else {
		txn.Status = "failed"
		// Auto-rollback failed transactions
		if err := um.restoreTransaction(ctx, txn); err != nil {
			return fmt.Errorf("auto-rollback failed transaction: %w", err)
		}
		return nil
	}

	// Calculate modified hashes and line changes
	if err := um.calculateModificationMetrics(txn); err != nil {
		return fmt.Errorf("calculate modification metrics: %w", err)
	}

	// Extract code artifacts via AST when the transaction does not already carry them.
	if len(txn.CodeArtifacts) == 0 && um.artifactExtract != nil {
		artifacts, err := um.artifactExtract.ExtractFromModifiedFiles(txn.ModifiedFiles, um.workingDir)
		if err != nil {
			// Log but don't fail - artifact extraction is best-effort.
			// Fall back to file path inference when extraction fails.
			artifacts = um.inferArtifactsFromPaths(txn.ModifiedFiles)
		}
		txn.CodeArtifacts = artifacts
	}

	if len(txn.CategoryPath) == 0 {
		if um.artifactExtract != nil && len(txn.CodeArtifacts) > 0 {
			txn.CategoryPath = um.artifactExtract.GenerateCategoryPath(txn.CodeArtifacts)
		}
		if len(txn.CategoryPath) == 0 {
			txn.CategoryPath = um.inferCategoryPath(txn.CodeArtifacts)
		}
	}
	if len(txn.CategoryPath) > 0 {
		txn.ApplicationDomain = txn.CategoryPath[0]
		if len(txn.CategoryPath) > 1 {
			txn.SubComponent = txn.CategoryPath[1]
		}
	}

	// Save transaction metadata to filesystem
	if err := um.saveTransactionMetadata(txn); err != nil {
		return fmt.Errorf("save transaction metadata: %w", err)
	}

	// Store in Space with multiple category paths
	if err := um.storeInSpace(ctx, txn); err != nil {
		return fmt.Errorf("store in space: %w", err)
	}

	return nil
}

// calculateModificationMetrics computes file hashes and line changes.
func (um *UndoManager) calculateModificationMetrics(txn *WorkflowTransaction) error {
	for i := range txn.ModifiedFiles {
		record := &txn.ModifiedFiles[i]

		// Skip if file was created (no original)
		if record.ChangeType == "create" {
			continue
		}

		fullPath := filepath.Join(um.workingDir, record.FilePath)

		// Check if file still exists (might be deleted)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				record.ChangeType = "delete"
				continue
			}
			return fmt.Errorf("read modified file %s: %w", record.FilePath, err)
		}

		// Calculate modified hash
		hash := sha256.Sum256(content)
		record.ModifiedHash = hex.EncodeToString(hash[:])

		// Estimate line changes from the captured delta when available.
		if record.OriginalHash != "" && record.Delta != "" {
			originalContent := []byte(record.Delta)
			record.LinesAdded, record.LinesRemoved = estimateLineChanges(originalContent, content)
			if record.LinesAdded > 0 || record.LinesRemoved > 0 {
				record.DiffStats = fmt.Sprintf("+%d -%d", record.LinesAdded, record.LinesRemoved)
				record.DiffSummary = fmt.Sprintf("%s changed (%s)", record.FilePath, record.DiffStats)
			} else {
				record.DiffSummary = fmt.Sprintf("%s updated", record.FilePath)
			}
			if record.Metadata == nil {
				record.Metadata = make(map[string]string)
			}
			record.Metadata["lines_added"] = fmt.Sprintf("%d", record.LinesAdded)
			record.Metadata["lines_removed"] = fmt.Sprintf("%d", record.LinesRemoved)
		}
	}

	return nil
}

// estimateLineChanges estimates lines added/removed (simple diff).
func estimateLineChanges(original, modified []byte) (added int, removed int) {
	origLines := strings.Split(string(original), "\n")
	modLines := strings.Split(string(modified), "\n")

	if len(modLines) > len(origLines) {
		added = len(modLines) - len(origLines)
	} else if len(origLines) > len(modLines) {
		removed = len(origLines) - len(modLines)
	}

	// TODO: More sophisticated diff algorithm
	return added, removed
}

// storeInSpace stores the transaction in Space using a presentation hierarchy and a
// separate semantic-index hierarchy for later retrieval/search. The UI explorer is not
// yet implemented, so this remains a backend-only structure for future UI work.
func (um *UndoManager) storeInSpace(ctx context.Context, txn *WorkflowTransaction) error {
	// Generate searchable content for vectorization
	txn.SearchableContent = um.generateSearchableContent(txn)

	repoName := um.deriveRepoName()
	presentationPath := um.derivePresentationPath(txn)
	semanticPath := um.deriveSemanticIndexPath(txn)

	itemsToStore := []SpaceItem{}

	// Transaction-level workstream record for undo history review.
	itemsToStore = append(itemsToStore, SpaceItem{
		ID:      txn.ID,
		Title:   txn.Description,
		Content: txn.SearchableContent,
		Metadata: map[string]interface{}{
			"kind":          "workstream",
			"repo_name":     repoName,
			"txn_id":        txn.ID,
			"workflow_type": txn.WorkflowType,
			"status":        txn.Status,
		},
		CategoryPath: []string{"Coding Workflows", repoName, "Transactions"},
	})

	// Presentation repo container.
	itemsToStore = append(itemsToStore, SpaceItem{
		ID:      fmt.Sprintf("%s_repo", txn.ID),
		Title:   fmt.Sprintf("Repository: %s", repoName),
		Content: fmt.Sprintf("Presentation container for workflow review data for %s.", repoName),
		Metadata: map[string]interface{}{
			"kind":      "repo",
			"repo_name": repoName,
			"txn_id":    txn.ID,
		},
		CategoryPath: []string{"Coding Workflows", repoName},
	})

	// Semantic index repo container.
	itemsToStore = append(itemsToStore, SpaceItem{
		ID:      fmt.Sprintf("%s_repo_semantic", txn.ID),
		Title:   fmt.Sprintf("Repository: %s", repoName),
		Content: fmt.Sprintf("Semantic index entry for repository %s.", repoName),
		Metadata: map[string]interface{}{
			"kind":           "repo",
			"repo_name":      repoName,
			"txn_id":         txn.ID,
			"semantic_index": true,
		},
		CategoryPath: []string{"Semantic Index", "Coding Workflows", repoName},
	})

	// Presentation code-path container.
	presentationContainerPath := append([]string{"Coding Workflows", repoName}, presentationPath[2:]...)
	itemsToStore = append(itemsToStore, SpaceItem{
		ID:      fmt.Sprintf("%s_presentation_path", txn.ID),
		Title:   fmt.Sprintf("Code Path: %s", strings.Join(presentationContainerPath[2:], " / ")),
		Content: txn.SearchableContent,
		Metadata: map[string]interface{}{
			"kind":              "presentation_path",
			"repo_name":         repoName,
			"txn_id":            txn.ID,
			"presentation_path": presentationContainerPath,
		},
		CategoryPath:        presentationContainerPath,
		IndexPopulationMode: memory.IndexPopulationModeReference,
	})

	// Semantic index code-path container.
	semanticContainerPath := append([]string{"Semantic Index", "Coding Workflows", repoName}, semanticPath[3:]...)
	itemsToStore = append(itemsToStore, SpaceItem{
		ID:      fmt.Sprintf("%s_semantic_path", txn.ID),
		Title:   fmt.Sprintf("Semantic Path: %s", strings.Join(semanticContainerPath[3:], " / ")),
		Content: txn.SearchableContent,
		Metadata: map[string]interface{}{
			"kind":           "semantic_path",
			"repo_name":      repoName,
			"txn_id":         txn.ID,
			"semantic_path":  semanticContainerPath,
			"semantic_index": true,
		},
		CategoryPath:        semanticContainerPath,
		IndexPopulationMode: memory.IndexPopulationModeFull,
	})

	// File-delta items. These are the leaf review artifacts.
	for _, file := range txn.ModifiedFiles {
		presentationFilePath := append(presentationContainerPath, file.FilePath)
		semanticFilePath := append(semanticContainerPath, file.FilePath)
		fileItem := SpaceItem{
			ID:      fmt.Sprintf("%s_file_%s", txn.ID, sanitizeSpaceSegment(file.FilePath)),
			Title:   file.FilePath,
			Content: fmt.Sprintf("File change for %s\n\n%s\n%s", file.FilePath, file.DiffSummary, file.DiffStats),
			Metadata: map[string]interface{}{
				"kind":              "file_delta",
				"repo_name":         repoName,
				"txn_id":            txn.ID,
				"file_path":         file.FilePath,
				"change_type":       file.ChangeType,
				"delta":             file.Delta,
				"delta_format":      file.DeltaFormat,
				"diff_summary":      file.DiffSummary,
				"diff_stats":        file.DiffStats,
				"metadata":          file.Metadata,
				"presentation_path": presentationFilePath,
				"semantic_path":     semanticFilePath,
				"semantic_index":    true,
			},
			CategoryPath:        semanticFilePath,
			IndexPopulationMode: memory.IndexPopulationModeFull,
		}
		itemsToStore = append(itemsToStore, fileItem)
	}

	// Store each semantic item in the Space. Additional UI exploration can be layered on
	// top of this backend structure later. Tracker: UI explorer/navigation is not yet
	// implemented and should be revisited separately.
	for _, item := range itemsToStore {
		if err := um.spaceClient.AddItemToCategory(ctx, um.spaceID, item.CategoryPath, item); err != nil {
			return fmt.Errorf("add semantic item %s to category %v: %w", item.ID, item.CategoryPath, err)
		}
	}

	return nil
}

func (um *UndoManager) deriveRepoName() string {
	if um.workingDir == "" {
		return "unknown_repo"
	}
	return sanitizeSpaceSegment(filepath.Base(um.workingDir))
}

func (um *UndoManager) derivePresentationPath(txn *WorkflowTransaction) []string {
	segments := []string{"Coding Workflows"}
	segments = append(segments, um.deriveRepoName())

	for _, artifact := range txn.CodeArtifacts {
		if artifact.Namespace != "" {
			segments = append(segments, sanitizeSpaceSegment(artifact.Namespace))
			return segments
		}
		if artifact.Package != "" {
			segments = append(segments, sanitizeSpaceSegment(artifact.Package))
			return segments
		}
	}

	segments = append(segments, "general")
	return segments
}

func (um *UndoManager) deriveSemanticIndexPath(txn *WorkflowTransaction) []string {
	segments := []string{"Semantic Index", "Coding Workflows"}
	segments = append(segments, um.deriveRepoName())

	for _, artifact := range txn.CodeArtifacts {
		if artifact.Namespace != "" {
			segments = append(segments, sanitizeSpaceSegment(artifact.Namespace))
			return segments
		}
		if artifact.Package != "" {
			segments = append(segments, sanitizeSpaceSegment(artifact.Package))
			return segments
		}
	}

	segments = append(segments, "general")
	return segments
}

func sanitizeSpaceSegment(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, string(os.PathSeparator), "_")
	value = strings.ReplaceAll(value, "/", "_")
	value = strings.ReplaceAll(value, "\\", "_")
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.Trim(value, "_-.")
	if value == "" {
		return "root"
	}
	return value
}

// generateCategoryPaths creates multiple category paths for a transaction.
func (um *UndoManager) generateCategoryPaths(txn *WorkflowTransaction) [][]string {
	paths := [][]string{
		// Path 1: Date hierarchy (for fast temporal lookup)
		{txn.Timestamp.Format("2006-01-02")},
	}

	// Path 2: Code artifact hierarchy (for semantic search)
	if len(txn.CategoryPath) > 0 {
		paths = append(paths, txn.CategoryPath)
	}

	// Path 3: Workflow type hierarchy (optional)
	if txn.WorkflowType != "" {
		paths = append(paths, []string{"By Type", txn.WorkflowType})
	}

	return paths
}

// generateSearchableContent creates rich text for vectorization.
func (um *UndoManager) generateSearchableContent(txn *WorkflowTransaction) string {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("Workflow: %s\n\n", txn.Description))
	buf.WriteString(fmt.Sprintf("Type: %s\n", txn.WorkflowType))
	buf.WriteString(fmt.Sprintf("Date: %s\n\n", txn.Timestamp.Format("2006-01-02 15:04")))

	if txn.ApplicationDomain != "" {
		buf.WriteString(fmt.Sprintf("Component: %s", txn.ApplicationDomain))
		if txn.SubComponent != "" {
			buf.WriteString(fmt.Sprintf(" → %s", txn.SubComponent))
		}
		buf.WriteString("\n\n")
	}

	buf.WriteString("Modified files:\n")
	for _, file := range txn.ModifiedFiles {
		buf.WriteString(fmt.Sprintf("- %s", file.FilePath))
		if file.LinesAdded > 0 || file.LinesRemoved > 0 {
			buf.WriteString(fmt.Sprintf(" (+%d -%d lines)", file.LinesAdded, file.LinesRemoved))
		}
		buf.WriteString("\n")
	}

	if len(txn.Tags) > 0 {
		buf.WriteString(fmt.Sprintf("\nTags: %s\n", strings.Join(txn.Tags, ", ")))
	}

	// Include code artifact details for better semantic matching
	if len(txn.CodeArtifacts) > 0 {
		buf.WriteString("\nCode Artifacts:\n")
		for _, artifact := range txn.CodeArtifacts {
			buf.WriteString(fmt.Sprintf("- Package: %s\n", artifact.Package))
			if len(artifact.Functions) > 0 {
				buf.WriteString(fmt.Sprintf("  Functions: %s\n", strings.Join(artifact.Functions, ", ")))
			}
			if len(artifact.Types) > 0 {
				buf.WriteString(fmt.Sprintf("  Types: %s\n", strings.Join(artifact.Types, ", ")))
			}
		}
	}

	return buf.String()
}

// saveTransactionMetadata saves transaction metadata to filesystem.
func (um *UndoManager) saveTransactionMetadata(txn *WorkflowTransaction) error {
	txnDir := um.getTransactionDir(txn.ID)
	metadataPath := filepath.Join(txnDir, "metadata.json")

	data, err := json.MarshalIndent(txn, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	return nil
}

// getTransactionDir returns the directory path for a transaction.
func (um *UndoManager) getTransactionDir(txnID string) string {
	return filepath.Join(um.workingDir, ".sop", "undo", "transactions", txnID)
}

// inferArtifactsFromPaths infers code artifacts from file paths (fallback).
func (um *UndoManager) inferArtifactsFromPaths(files []ModifiedFileRecord) []CodeArtifact {
	artifacts := []CodeArtifact{}

	for _, file := range files {
		parts := strings.Split(file.FilePath, "/")
		if len(parts) == 0 {
			continue
		}

		artifact := CodeArtifact{
			File: file.FilePath,
		}

		// Infer package from path
		if len(parts) > 1 {
			artifact.Package = parts[0]
		}

		artifacts = append(artifacts, artifact)
	}

	return artifacts
}

func (um *UndoManager) inferCategoryPath(artifacts []CodeArtifact) []string {
	if len(artifacts) == 0 {
		return []string{"general"}
	}

	for _, artifact := range artifacts {
		if artifact.Namespace != "" {
			return []string{sanitizeSpaceSegment(artifact.Namespace)}
		}
		if artifact.Package != "" {
			return []string{sanitizeSpaceSegment(artifact.Package)}
		}
	}

	return []string{"general"}
}

// Helper function to title case a string.
func titleCase(s string) string {
	words := strings.Fields(strings.ReplaceAll(s, "_", " "))
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
		}
	}
	return strings.Join(words, " ")
}

// generateTransactionID creates a unique transaction ID.
func generateTransactionID() string {
	timestamp := time.Now().Format("20060102_150405")
	uid := uuid.New().String()[:8]
	return fmt.Sprintf("txn_%s_%s", timestamp, uid)
}

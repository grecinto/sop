package codingworkflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// SemanticSearch performs a semantic search with time-ranked results.
func (um *UndoManager) SemanticSearch(ctx context.Context, query UndoQuery) ([]WorkflowTransaction, error) {
	// Set default ranking strategy
	if query.RankBy == "" {
		query.RankBy = RankByHybrid
	}
	if query.RecencyWeight == 0 {
		query.RecencyWeight = 0.3
	}
	if query.RelevanceWeight == 0 {
		query.RelevanceWeight = 0.7
	}
	if query.Limit == 0 {
		query.Limit = 20
	}

	// Perform semantic search in Space
	spaceReq := SpaceSearchRequest{
		SpaceID: um.spaceID,
		Query:   query.SemanticQuery,
		TopK:    query.Limit * 2, // Get more candidates for re-ranking
		Filters: make(map[string]interface{}),
	}

	// Add metadata filters
	if query.WorkflowType != "" {
		spaceReq.Filters["workflow_type"] = query.WorkflowType
	}
	if query.Status != "" {
		spaceReq.Filters["status"] = query.Status
	}
	if query.Component != "" {
		spaceReq.Filters["application_domain"] = query.Component
	}
	if query.SubComponent != "" {
		spaceReq.Filters["sub_component"] = query.SubComponent
	}

	// Execute search
	items, err := um.spaceClient.SemanticSearch(ctx, spaceReq)
	if err != nil {
		return nil, fmt.Errorf("semantic search: %w", err)
	}

	// Convert Space items to transactions
	transactions := []WorkflowTransaction{}
	for _, item := range items {
		txn, err := um.spaceItemToTransaction(item)
		if err != nil {
			continue // Skip malformed items
		}
		txn.RelevanceScore = item.VectorScore
		transactions = append(transactions, txn)
	}

	// Apply date filters
	transactions = um.filterByDate(transactions, query)

	// Apply file filters
	if query.FilePath != "" || query.FilePattern != "" {
		transactions = um.filterByFiles(transactions, query)
	}

	// Apply tag filters
	if len(query.Tags) > 0 {
		transactions = um.filterByTags(transactions, query)
	}

	// Rank results
	transactions = um.rankResults(transactions, query)

	// Apply pagination
	start := query.Offset
	end := start + query.Limit
	if start >= len(transactions) {
		return []WorkflowTransaction{}, nil
	}
	if end > len(transactions) {
		end = len(transactions)
	}

	return transactions[start:end], nil
}

// filterByDate filters transactions by date range.
func (um *UndoManager) filterByDate(transactions []WorkflowTransaction, query UndoQuery) []WorkflowTransaction {
	// Handle named date ranges
	if query.DateRange != "" {
		query.After, query.Before = parseDateRange(query.DateRange)
	}

	if query.After == nil && query.Before == nil {
		return transactions
	}

	filtered := []WorkflowTransaction{}
	for _, txn := range transactions {
		if query.After != nil && txn.Timestamp.Before(*query.After) {
			continue
		}
		if query.Before != nil && txn.Timestamp.After(*query.Before) {
			continue
		}
		filtered = append(filtered, txn)
	}

	return filtered
}

// filterByFiles filters transactions by file path.
func (um *UndoManager) filterByFiles(transactions []WorkflowTransaction, query UndoQuery) []WorkflowTransaction {
	filtered := []WorkflowTransaction{}

	for _, txn := range transactions {
		hasMatch := false
		for _, file := range txn.ModifiedFiles {
			if query.FilePath != "" && file.FilePath == query.FilePath {
				hasMatch = true
				break
			}
			// TODO: Add glob/regex pattern matching
		}
		if hasMatch {
			filtered = append(filtered, txn)
		}
	}

	return filtered
}

// filterByTags filters transactions by tags (match any).
func (um *UndoManager) filterByTags(transactions []WorkflowTransaction, query UndoQuery) []WorkflowTransaction {
	filtered := []WorkflowTransaction{}

	for _, txn := range transactions {
		hasMatch := false
		for _, queryTag := range query.Tags {
			for _, txnTag := range txn.Tags {
				if queryTag == txnTag {
					hasMatch = true
					break
				}
			}
			if hasMatch {
				break
			}
		}
		if hasMatch {
			filtered = append(filtered, txn)
		}
	}

	return filtered
}

// rankResults ranks transactions according to the query strategy.
func (um *UndoManager) rankResults(transactions []WorkflowTransaction, query UndoQuery) []WorkflowTransaction {
	now := time.Now()

	for i := range transactions {
		txn := &transactions[i]

		switch query.RankBy {
		case RankByTimeDesc:
			txn.RelevanceScore = float64(txn.Timestamp.Unix())
		case RankByTimeAsc:
			txn.RelevanceScore = -float64(txn.Timestamp.Unix())
		case RankByRelevance:
			// Score already set from Space search
		case RankByHybrid:
			// Normalize timestamp to 0-1 scale with exponential decay
			recencyScore := calculateRecencyScore(txn.Timestamp, now)
			relevanceScore := txn.RelevanceScore

			// Weighted combination
			txn.RelevanceScore = (recencyScore * query.RecencyWeight) + (relevanceScore * query.RelevanceWeight)
		}
	}

	// Sort by score descending
	sort.Slice(transactions, func(i, j int) bool {
		return transactions[i].RelevanceScore > transactions[j].RelevanceScore
	})

	return transactions
}

// calculateRecencyScore computes a recency score with exponential decay.
func calculateRecencyScore(timestamp time.Time, now time.Time) float64 {
	daysSince := now.Sub(timestamp).Hours() / 24.0

	// Exponential decay: score halves every 7 days
	decayHalfLife := 7.0
	score := math.Pow(0.5, daysSince/decayHalfLife)

	return score
}

// parseDateRange converts named date ranges to time bounds.
func parseDateRange(rangeStr string) (*time.Time, *time.Time) {
	now := time.Now()

	switch rangeStr {
	case "today":
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return &start, nil
	case "yesterday":
		start := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, now.Location())
		end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return &start, &end
	case "last_week":
		start := now.AddDate(0, 0, -7)
		return &start, nil
	case "last_month":
		start := now.AddDate(0, -1, 0)
		return &start, nil
	default:
		return nil, nil
	}
}

// RestoreTransaction restores a transaction by its ID.
func (um *UndoManager) RestoreTransaction(ctx context.Context, txnID string, options RestoreOptions) error {
	// Load transaction
	txn, err := um.loadTransactionFromFilesystem(txnID)
	if err != nil {
		return fmt.Errorf("load transaction: %w", err)
	}

	// Verify hashes if requested
	if options.VerifyHashes && !um.verifyBackupIntegrity(txn) {
		return fmt.Errorf("backup integrity check failed for transaction %s", txnID)
	}

	// Preview mode
	if options.Mode == RestorePreview || options.DryRun {
		return um.previewRestore(txn, options)
	}

	// Create backup of current state if requested
	if options.CreateBackup {
		if err := um.backupCurrentState(ctx, txn); err != nil {
			return fmt.Errorf("backup current state: %w", err)
		}
	}

	// Perform restore
	return um.restoreTransaction(ctx, txn)
}

// restoreTransaction restores files from a transaction.
func (um *UndoManager) restoreTransaction(ctx context.Context, txn *WorkflowTransaction) error {
	// Restore files in reverse order
	for i := len(txn.ModifiedFiles) - 1; i >= 0; i-- {
		record := txn.ModifiedFiles[i]

		fullPath := filepath.Join(um.workingDir, record.FilePath)

		switch record.ChangeType {
		case "create":
			// File was created, delete it
			if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("delete created file %s: %w", record.FilePath, err)
			}

		case "delete":
			// File was deleted, restore from backup
			if record.BackupPath == "" {
				return fmt.Errorf("no backup for deleted file %s", record.FilePath)
			}
			backup, err := os.ReadFile(record.BackupPath)
			if err != nil {
				return fmt.Errorf("read backup %s: %w", record.BackupPath, err)
			}

			// Ensure directory exists
			dir := filepath.Dir(fullPath)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("create directory %s: %w", dir, err)
			}

			if err := os.WriteFile(fullPath, backup, 0644); err != nil {
				return fmt.Errorf("restore deleted file %s: %w", record.FilePath, err)
			}

		case "modify":
			// File was modified, restore from backup
			if record.BackupPath == "" {
				return fmt.Errorf("no backup for modified file %s", record.FilePath)
			}
			backup, err := os.ReadFile(record.BackupPath)
			if err != nil {
				return fmt.Errorf("read backup %s: %w", record.BackupPath, err)
			}

			// Verify original hash if available
			if record.OriginalHash != "" {
				hash := sha256Hash(backup)
				if hash != record.OriginalHash {
					return fmt.Errorf("backup integrity check failed for %s", record.FilePath)
				}
			}

			if err := os.WriteFile(fullPath, backup, 0644); err != nil {
				return fmt.Errorf("restore file %s: %w", record.FilePath, err)
			}
		}
	}

	// Update transaction status
	txn.Status = "undone"
	return um.saveTransactionMetadata(txn)
}

// previewRestore shows what would change without actually restoring.
func (um *UndoManager) previewRestore(txn *WorkflowTransaction, options RestoreOptions) error {
	fmt.Printf("\nWould restore %d files from transaction: %s\n\n", len(txn.ModifiedFiles), txn.ID)
	fmt.Printf("Description: %s\n", txn.Description)
	fmt.Printf("Date: %s\n", txn.Timestamp.Format("2006-01-02 15:04"))
	fmt.Printf("Type: %s\n\n", txn.WorkflowType)

	fmt.Println("Files to restore:")
	for _, record := range txn.ModifiedFiles {
		fmt.Printf("  %s (%s)", record.FilePath, record.ChangeType)
		if record.LinesAdded > 0 || record.LinesRemoved > 0 {
			fmt.Printf(" [+%d -%d lines]", record.LinesAdded, record.LinesRemoved)
		}
		fmt.Println()
	}

	if options.ConfirmPrompt {
		fmt.Print("\nConfirm restore? (y/n): ")
		// TODO: Read user input
	}

	return nil
}

// verifyBackupIntegrity verifies backup file hashes.
func (um *UndoManager) verifyBackupIntegrity(txn *WorkflowTransaction) bool {
	for _, record := range txn.ModifiedFiles {
		if record.BackupPath == "" || record.OriginalHash == "" {
			continue
		}

		backup, err := os.ReadFile(record.BackupPath)
		if err != nil {
			return false
		}

		hash := sha256Hash(backup)
		if hash != record.OriginalHash {
			return false
		}
	}
	return true
}

// backupCurrentState creates a backup of the current state before restore.
func (um *UndoManager) backupCurrentState(ctx context.Context, txn *WorkflowTransaction) error {
	// Create a new transaction for the current state
	_, err := um.BeginTransaction(ctx, fmt.Sprintf("Backup before restore to %s", txn.ID), "backup")
	if err != nil {
		return err
	}

	// Backup all files that will be affected
	for _, record := range txn.ModifiedFiles {
		if err := um.BackupFile(ctx, record.FilePath); err != nil {
			// Continue even if some files fail
			continue
		}
	}

	// Complete backup transaction
	return um.EndTransaction(ctx, true, 0, 0, 0)
}

// loadTransactionFromFilesystem loads a transaction from filesystem metadata.
func (um *UndoManager) loadTransactionFromFilesystem(txnID string) (*WorkflowTransaction, error) {
	metadataPath := filepath.Join(um.getTransactionDir(txnID), "metadata.json")

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("read metadata: %w", err)
	}

	var txn WorkflowTransaction
	if err := json.Unmarshal(data, &txn); err != nil {
		return nil, fmt.Errorf("unmarshal metadata: %w", err)
	}

	return &txn, nil
}

// spaceItemToTransaction converts a Space item to a transaction.
func (um *UndoManager) spaceItemToTransaction(item SpaceItem) (WorkflowTransaction, error) {
	txn := WorkflowTransaction{
		ID:             item.ID,
		RelevanceScore: item.VectorScore,
	}

	// Extract metadata
	if val, ok := item.Metadata["txn_id"].(string); ok {
		txn.ID = val
	}
	if val, ok := item.Metadata["timestamp"].(float64); ok {
		txn.Timestamp = time.Unix(int64(val), 0)
	}
	if val, ok := item.Metadata["workflow_type"].(string); ok {
		txn.WorkflowType = val
	}
	if val, ok := item.Metadata["status"].(string); ok {
		txn.Status = val
	}
	if val, ok := item.Metadata["user_name"].(string); ok {
		txn.UserName = val
	}
	if val, ok := item.Metadata["application_domain"].(string); ok {
		txn.ApplicationDomain = val
	}
	if val, ok := item.Metadata["sub_component"].(string); ok {
		txn.SubComponent = val
	}

	// Extract arrays
	if val, ok := item.Metadata["tags"].([]interface{}); ok {
		txn.Tags = make([]string, 0, len(val))
		for _, v := range val {
			if s, ok := v.(string); ok {
				txn.Tags = append(txn.Tags, s)
			}
		}
	}

	// Extract description from title (remove date prefix)
	txn.Description = item.Title
	if len(item.Title) > 19 && item.Title[0] == '[' {
		// Remove "[2006-01-02 15:04] " prefix
		txn.Description = item.Title[19:]
	}

	return txn, nil
}

// sha256Hash computes SHA256 hash of data.
func sha256Hash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// GetTransactionStats returns analytics about undo history.
func (um *UndoManager) GetTransactionStats(ctx context.Context) (*TransactionStats, error) {
	stats := &TransactionStats{
		ByComponent:    make(map[string]int),
		ByWorkflowType: make(map[string]int),
	}

	// List all transactions
	txnDir := filepath.Join(um.workingDir, ".sop", "undo", "transactions")
	entries, err := os.ReadDir(txnDir)
	if err != nil {
		return nil, fmt.Errorf("read transactions directory: %w", err)
	}

	totalExecSec := 0.0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		txn, err := um.loadTransactionFromFilesystem(entry.Name())
		if err != nil {
			continue // Skip malformed transactions
		}

		stats.TotalTransactions++

		switch txn.Status {
		case "completed":
			stats.SuccessfulCount++
		case "failed":
			stats.FailedCount++
		case "undone":
			stats.UndoneCount++
		}

		// Track oldest/newest
		if stats.OldestTimestamp.IsZero() || txn.Timestamp.Before(stats.OldestTimestamp) {
			stats.OldestTimestamp = txn.Timestamp
		}
		if stats.NewestTimestamp.IsZero() || txn.Timestamp.After(stats.NewestTimestamp) {
			stats.NewestTimestamp = txn.Timestamp
		}

		// Count by component
		if txn.ApplicationDomain != "" {
			stats.ByComponent[txn.ApplicationDomain]++
		}

		// Count by workflow type
		if txn.WorkflowType != "" {
			stats.ByWorkflowType[txn.WorkflowType]++
		}

		// Calculate storage
		for _, file := range txn.ModifiedFiles {
			stats.TotalStorageMB += float64(file.SizeBytes) / (1024 * 1024)
		}

		// Parse execution time
		if txn.ExecutionTime != "" {
			if d, err := time.ParseDuration(txn.ExecutionTime); err == nil {
				totalExecSec += d.Seconds()
			}
		}
	}

	if stats.TotalTransactions > 0 {
		stats.AverageExecutionSec = totalExecSec / float64(stats.TotalTransactions)
	}

	return stats, nil
}

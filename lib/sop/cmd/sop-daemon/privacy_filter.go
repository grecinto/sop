package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sharedcode/sop/ai/privacy"
)

var (
	globalPrivacyFilter   *privacy.PrivacyFilter
	globalPrivacyFilterMu sync.RWMutex
	auditLogPath          string
)

type noopAuditLogger struct{}

func (noopAuditLogger) Log(ctx context.Context, entry *privacy.AuditLogEntry) error {
	return nil
}

// initPrivacyFilter initializes the privacy filter for the daemon.
func initPrivacyFilter(daemonDataDir string) error {
	auditLogPath = filepath.Join(daemonDataDir, "privacy_audit.jsonl")

	_, err := ensurePrivacyFilterInitialized()
	if err != nil {
		return err
	}

	settings := getPrivacySettings()
	if settings == nil {
		return fmt.Errorf("privacy settings not initialized")
	}

	logVerbose("privacy filter initialized global_share_enabled=%t policy_type=%s auto_redact_pii=%t",
		settings.GlobalShareEnabled,
		settings.PolicyType,
		settings.AutoRedactPII,
	)

	return nil
}

func ensurePrivacyFilterInitialized() (*privacy.PrivacyFilter, error) {
	if filter := getPrivacyFilter(); filter != nil {
		return filter, nil
	}

	settings := getPrivacySettings()
	if settings == nil {
		settings = privacy.NewDefaultPrivacySettings("daemon-user")
		globalPrivacySettingsMu.Lock()
		globalPrivacySettings = settings
		globalPrivacySettingsMu.Unlock()
	}

	detector := privacy.NewPIIDetector()
	var auditLog privacy.AuditLogger
	if auditLogPath != "" {
		auditLog = &FileAuditLogger{LogPath: auditLogPath}
	} else {
		auditLog = noopAuditLogger{}
	}

	filter := privacy.NewPrivacyFilter(settings, detector, auditLog)
	globalPrivacyFilterMu.Lock()
	globalPrivacyFilter = filter
	globalPrivacyFilterMu.Unlock()

	return filter, nil
}

// getPrivacyFilter returns the global privacy filter (thread-safe).
func getPrivacyFilter() *privacy.PrivacyFilter {
	globalPrivacyFilterMu.RLock()
	defer globalPrivacyFilterMu.RUnlock()
	return globalPrivacyFilter
}

// updatePrivacyFilter updates the filter with new settings.
func updatePrivacyFilter(settings *privacy.PrivacySettings) {
	filter := getPrivacyFilter()
	if filter != nil {
		filter.UpdateSettings(settings)
	}
}

// filterContentBeforeShare applies privacy filtering before sharing content to HQ.
// This is the daemon-side enforcement point for outbound content.
func filterContentBeforeShare(ctx context.Context, category string, content string, userID string) (*privacy.FilterResult, error) {
	filter, err := ensurePrivacyFilterInitialized()
	if err != nil {
		return nil, err
	}

	result, err := filter.FilterBeforeShare(ctx, category, content, userID)
	if err != nil {
		return nil, fmt.Errorf("privacy filter error: %w", err)
	}

	// Log filter decision at verbose level to avoid noisy per-request output.
	logVerbose("privacy filter result action=%s reason=%q category=%s has_pii=%t requires_approval=%t",
		result.Action,
		result.Reason,
		category,
		len(result.PIIMatches) > 0,
		result.RequiresApproval,
	)

	return result, nil
}

// filterBatchBeforeShare applies privacy filtering to multiple items.
func filterBatchBeforeShare(ctx context.Context, category string, items []string, userID string) ([]*privacy.FilterResult, error) {
	filter, err := ensurePrivacyFilterInitialized()
	if err != nil {
		return nil, err
	}

	results, err := filter.FilterBatch(ctx, category, items, userID)
	if err != nil {
		return nil, fmt.Errorf("batch privacy filter error: %w", err)
	}

	// Generate summary for logging at verbose level.
	summary := privacy.GenerateSummary(results)
	logVerbose("privacy filter batch summary total_items=%d allowed=%d blocked=%d pending=%d redacted=%d pii_detected=%d",
		summary.TotalItems,
		summary.Allowed,
		summary.Blocked,
		summary.Pending,
		summary.Redacted,
		summary.PIIDetected,
	)

	return results, nil
}

// FileAuditLogger implements AuditLogger by writing to a JSON Lines file.
type FileAuditLogger struct {
	LogPath string
	mu      sync.Mutex
}

// Log writes an audit entry to the log file.
func (l *FileAuditLogger) Log(ctx context.Context, entry *privacy.AuditLogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Open file in append mode
	f, err := os.OpenFile(l.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("failed to open audit log: %w", err)
	}
	defer f.Close()

	// Write JSON line
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal audit entry: %w", err)
	}

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write audit entry: %w", err)
	}

	return nil
}

// handleGetAuditLog returns recent audit log entries (API endpoint).
func handleGetAuditLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read audit log file
	data, err := os.ReadFile(auditLogPath)
	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]privacy.AuditLogEntry{})
			return
		}
		http.Error(w, fmt.Sprintf("Failed to read audit log: %v", err), http.StatusInternalServerError)
		return
	}

	// Parse JSONL format
	lines := string(data)
	var entries []privacy.AuditLogEntry
	for _, line := range splitLines(lines) {
		if line == "" {
			continue
		}
		var entry privacy.AuditLogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // Skip malformed lines
		}
		entries = append(entries, entry)
	}

	// Return most recent 100 entries
	if len(entries) > 100 {
		entries = entries[len(entries)-100:]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

// splitLines splits a string by newlines.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// ApprovalQueue manages items pending manual approval.
type ApprovalQueue struct {
	items []ApprovalItem
	mu    sync.RWMutex
}

// ApprovalItem represents content awaiting approval.
type ApprovalItem struct {
	ID              string             `json:"id"`
	Category        string             `json:"category"`
	OriginalContent string             `json:"original_content"`
	RedactedContent string             `json:"redacted_content,omitempty"`
	PIIMatches      []privacy.PIIMatch `json:"pii_matches,omitempty"`
	Reason          string             `json:"reason"`
	CreatedAt       time.Time          `json:"created_at"`
	UserID          string             `json:"user_id"`
}

var globalApprovalQueue = &ApprovalQueue{
	items: make([]ApprovalItem, 0),
}

// AddToApprovalQueue adds an item to the approval queue.
func (q *ApprovalQueue) AddToApprovalQueue(item ApprovalItem) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, item)
}

// GetPendingItems returns all pending approval items.
func (q *ApprovalQueue) GetPendingItems() []ApprovalItem {
	q.mu.RLock()
	defer q.mu.RUnlock()
	items := make([]ApprovalItem, len(q.items))
	copy(items, q.items)
	return items
}

// RemoveItem removes an item from the queue by ID.
func (q *ApprovalQueue) RemoveItem(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, item := range q.items {
		if item.ID == id {
			q.items = append(q.items[:i], q.items[i+1:]...)
			return true
		}
	}
	return false
}

// handleGetApprovalQueue returns pending approval items (API endpoint).
func handleGetApprovalQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	items := globalApprovalQueue.GetPendingItems()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

// handleApproveItem approves an item for sharing (API endpoint).
func handleApproveItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID      string `json:"id"`
		Content string `json:"content,omitempty"` // Optional edited content
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	// Remove from queue
	if !globalApprovalQueue.RemoveItem(req.ID) {
		http.Error(w, "Item not found", http.StatusNotFound)
		return
	}

	// TODO: Push approved content to HQ via sync queue

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "approved"})
}

// handleRejectItem rejects an item from sharing (API endpoint).
func handleRejectItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	// Remove from queue
	if !globalApprovalQueue.RemoveItem(req.ID) {
		http.Error(w, "Item not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "rejected"})
}

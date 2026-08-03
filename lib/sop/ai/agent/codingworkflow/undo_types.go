package codingworkflow

import (
	"time"
)

// WorkflowTransaction represents one undoable workflow run.
// Each transaction is an atomic unit that can be restored as a whole.
type WorkflowTransaction struct {
	// Core identity
	ID          string    `json:"id"`
	Timestamp   time.Time `json:"timestamp"`
	Description string    `json:"description"`
	Status      string    `json:"status"` // "in_progress", "completed", "failed", "undone"

	// Workflow metadata
	WorkflowType string   `json:"workflow_type"` // "test_fix", "refactoring", etc.
	Tags         []string `json:"tags,omitempty"`
	TriggeredBy  string   `json:"triggered_by"` // "user", "auto", "scheduled"
	UserName     string   `json:"user_name"`

	// Code artifact analysis (extracted via AST)
	CodeArtifacts     []CodeArtifact `json:"code_artifacts"`
	ApplicationDomain string         `json:"application_domain"` // "Authentication System"
	SubComponent      string         `json:"sub_component"`      // "Middleware"
	CategoryPath      []string       `json:"category_path"`      // For Space storage

	// File modifications
	ModifiedFiles []ModifiedFileRecord `json:"modified_files"`
	BackupPath    string               `json:"backup_path"` // Base path for backups

	// Execution context
	Context       map[string]interface{} `json:"context,omitempty"`
	SessionID     string                 `json:"session_id,omitempty"`
	WorkstreamID  string                 `json:"workstream_id,omitempty"`
	ExecutionTime string                 `json:"execution_time"`
	StepCount     int                    `json:"step_count"`
	RetryCount    int                    `json:"retry_count"`

	// Search optimization
	SearchableContent string  `json:"-"` // Generated for vectorization
	RelevanceScore    float64 `json:"-"` // Computed during search
}

// CodeArtifact represents extracted code structure from modified files.
type CodeArtifact struct {
	Namespace string   `json:"namespace"` // "github.com/myapp/auth"
	Package   string   `json:"package"`   // "auth", "middleware"
	File      string   `json:"file"`      // "auth/middleware.go"
	Functions []string `json:"functions"` // ["ValidateJWT", "extractToken"]
	Types     []string `json:"types"`     // ["JWTValidator", "Token"]
}

// ModifiedFileRecord tracks a single file modification within a transaction.
type ModifiedFileRecord struct {
	FilePath     string            `json:"file_path"`              // Relative path from working dir
	BackupPath   string            `json:"backup_path"`            // Deprecated: retained for compatibility with older records
	OriginalHash string            `json:"original_hash"`          // SHA256 of original content
	ModifiedHash string            `json:"modified_hash"`          // SHA256 of modified content
	Timestamp    time.Time         `json:"timestamp"`              // When file was modified
	LinesAdded   int               `json:"lines_added"`            // Number of lines added
	LinesRemoved int               `json:"lines_removed"`          // Number of lines removed
	ChangeType   string            `json:"change_type"`            // "create", "modify", "delete"
	SizeBytes    int64             `json:"size_bytes"`             // Original file size
	Delta        string            `json:"delta,omitempty"`        // Compact text delta payload for replaying the change
	DeltaFormat  string            `json:"delta_format,omitempty"` // e.g. "text-replace"
	DiffSummary  string            `json:"diff_summary,omitempty"` // Short human-readable summary for UI cards
	DiffStats    string            `json:"diff_stats,omitempty"`   // Compact counts like "+3 -1"
	Metadata     map[string]string `json:"metadata,omitempty"`     // UI-friendly metadata for timeline rendering
}

// UndoQuery specifies search criteria for finding transactions.
type UndoQuery struct {
	// Time filters
	After     *time.Time `json:"after,omitempty"`
	Before    *time.Time `json:"before,omitempty"`
	DateRange string     `json:"date_range,omitempty"` // "today", "yesterday", "last_week"

	// Semantic search
	SemanticQuery string `json:"semantic_query,omitempty"` // Natural language query

	// File filters
	FilePath    string `json:"file_path,omitempty"`    // Exact match or glob
	FilePattern string `json:"file_pattern,omitempty"` // Regex pattern

	// Workflow filters
	WorkflowType string   `json:"workflow_type,omitempty"`
	Tags         []string `json:"tags,omitempty"` // Match any
	Status       string   `json:"status,omitempty"`

	// Code artifact filters
	Component    string `json:"component,omitempty"`     // "Authentication System"
	SubComponent string `json:"sub_component,omitempty"` // "Middleware"

	// Ranking strategy
	RankBy          RankingStrategy `json:"rank_by"`
	RecencyWeight   float64         `json:"recency_weight"`   // 0.0-1.0, default 0.3
	RelevanceWeight float64         `json:"relevance_weight"` // 0.0-1.0, default 0.7

	// Pagination
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
}

// RankingStrategy determines how search results are ordered.
type RankingStrategy string

const (
	RankByHybrid    RankingStrategy = "hybrid"    // Combine recency + relevance (default)
	RankByTimeDesc  RankingStrategy = "time_desc" // Newest first
	RankByTimeAsc   RankingStrategy = "time_asc"  // Oldest first
	RankByRelevance RankingStrategy = "relevance" // Pure semantic similarity
)

// RestoreOptions configures how a transaction is restored.
type RestoreOptions struct {
	// Restore mode
	Mode RestoreMode `json:"mode"`

	// Selective restore
	FilesOnly    []string `json:"files_only,omitempty"`    // Only restore these files
	ExcludeFiles []string `json:"exclude_files,omitempty"` // Don't restore these

	// Safety checks
	DryRun        bool `json:"dry_run"`        // Show what would change
	CreateBackup  bool `json:"create_backup"`  // Create backup before restore (optional, undo system provides inherent redo)
	ConfirmPrompt bool `json:"confirm_prompt"` // Ask for confirmation
	VerifyHashes  bool `json:"verify_hashes"`  // Verify backup integrity
}

// RestoreMode determines what gets restored.
type RestoreMode string

const (
	RestoreFull      RestoreMode = "full"      // Restore all files in transaction
	RestoreSelective RestoreMode = "selective" // Restore only specified files
	RestorePreview   RestoreMode = "preview"   // Just show what would change
)

// RetentionPolicy defines cleanup rules for old transactions.
type RetentionPolicy struct {
	KeepDays        int      `json:"keep_days"`        // Keep history for N days (default: 30)
	MaxTransactions int      `json:"max_transactions"` // Max total transactions (default: 1000)
	MaxStorageMB    int      `json:"max_storage_mb"`   // Max storage for backups (default: 500)
	KeepMilestones  bool     `json:"keep_milestones"`  // Keep tagged transactions
	CompressOld     bool     `json:"compress_old"`     // Compress backups older than 7 days
	CleanupSchedule string   `json:"cleanup_schedule"` // "daily", "weekly"
	MilestoneTags   []string `json:"milestone_tags"`   // Tags that mark milestones
}

// DefaultRetentionPolicy returns the default retention policy.
func DefaultRetentionPolicy() RetentionPolicy {
	return RetentionPolicy{
		KeepDays:        30,
		MaxTransactions: 1000,
		MaxStorageMB:    500,
		KeepMilestones:  true,
		CompressOld:     true,
		CleanupSchedule: "daily",
		MilestoneTags:   []string{"milestone", "important", "release"},
	}
}

// TransactionStats provides analytics about undo history.
type TransactionStats struct {
	TotalTransactions   int            `json:"total_transactions"`
	SuccessfulCount     int            `json:"successful_count"`
	FailedCount         int            `json:"failed_count"`
	UndoneCount         int            `json:"undone_count"`
	TotalStorageMB      float64        `json:"total_storage_mb"`
	OldestTimestamp     time.Time      `json:"oldest_timestamp"`
	NewestTimestamp     time.Time      `json:"newest_timestamp"`
	ByComponent         map[string]int `json:"by_component"`     // Component -> count
	ByWorkflowType      map[string]int `json:"by_workflow_type"` // Type -> count
	AverageExecutionSec float64        `json:"average_execution_sec"`
}

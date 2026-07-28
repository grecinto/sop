package codingworkflow

import (
	"context"
	"fmt"
	"strings"
)

// UndoAwarePatchEngine wraps the base PatchEngine to automatically backup files.
// This is the integration point that intercepts file modifications.
type UndoAwarePatchEngine struct {
	base        PatchEngine
	undoManager *UndoManager
}

// NewUndoAwarePatchEngine creates a patch engine that tracks modifications for undo.
func NewUndoAwarePatchEngine(base PatchEngine, undoManager *UndoManager) *UndoAwarePatchEngine {
	return &UndoAwarePatchEngine{
		base:        base,
		undoManager: undoManager,
	}
}

// Apply applies a patch while automatically backing up the file.
func (u *UndoAwarePatchEngine) Apply(task Task, patch *Patch, state RunState) error {
	// Backup file BEFORE applying patch
	if u.undoManager != nil && patch != nil && patch.FilePath != "" {
		ctx := context.Background()
		if err := u.undoManager.BackupFile(ctx, patch.FilePath); err != nil {
			// Log error but continue - backup is best-effort
			// TODO: Add proper logging
			fmt.Printf("Warning: failed to backup file %s: %v\n", patch.FilePath, err)
		}
	}

	// Apply patch using base implementation
	return u.base.Apply(task, patch, state)
}

// WorkflowManagerWithUndo wraps WorkflowManager to add undo transaction management.
type WorkflowManagerWithUndo struct {
	base        *WorkflowManager
	undoManager *UndoManager
}

// NewWorkflowManagerWithUndo creates a workflow manager with undo support.
func NewWorkflowManagerWithUndo(base *WorkflowManager, undoManager *UndoManager) *WorkflowManagerWithUndo {
	return &WorkflowManagerWithUndo{
		base:        base,
		undoManager: undoManager,
	}
}

// Execute runs a workflow sequence with automatic transaction management.
func (wmu *WorkflowManagerWithUndo) Execute(ctx context.Context, steps []Step, task Task, state RunState) (WorkflowResult, error) {
	// Start undo transaction
	txn, err := wmu.undoManager.BeginTransaction(ctx, task.Description, determineWorkflowType(task))
	if err != nil {
		return WorkflowResult{}, fmt.Errorf("begin undo transaction: %w", err)
	}

	// Add transaction ID to state metadata
	if state.Metadata == nil {
		state.Metadata = make(map[string]string)
	}
	state.Metadata["undo_transaction_id"] = txn.ID

	// Execute workflow
	result, err := wmu.base.Execute(ctx, steps, task, state)

	// Complete transaction
	success := result.Success && err == nil
	endErr := wmu.undoManager.EndTransaction(
		ctx,
		success,
		result.ExecutionTime,
		len(result.CompletedSteps),
		result.RetryCount,
	)

	if endErr != nil {
		// Log error but don't override workflow error
		fmt.Printf("Warning: failed to complete undo transaction: %v\n", endErr)
	}

	return result, err
}

// determineWorkflowType infers workflow type from task description.
func determineWorkflowType(task Task) string {
	// Simple keyword matching
	// TODO: Use more sophisticated classification
	desc := task.Description

	keywords := map[string]string{
		"test":        "test_fix",
		"fix":         "test_fix",
		"debug":       "debugging",
		"refactor":    "refactoring",
		"implement":   "implementation",
		"add":         "implementation",
		"create":      "implementation",
		"security":    "security_enhancement",
		"auth":        "security_enhancement",
		"optimize":    "optimization",
		"performance": "optimization",
		"document":    "documentation",
		"doc":         "documentation",
	}

	descLower := strings.ToLower(desc)
	for keyword, wfType := range keywords {
		if strings.Contains(descLower, keyword) {
			return wfType
		}
	}

	return "general"
}

// EnableUndoForOrchestrator wraps an orchestrator's PatchEngine with undo support.
func EnableUndoForOrchestrator(orchestrator *Orchestrator, undoManager *UndoManager) {
	// Wrap the existing patch engine
	orchestrator.PatchEngine = NewUndoAwarePatchEngine(orchestrator.PatchEngine, undoManager)
}

// EnableUndoForWorkflowManager enables undo support for a workflow manager.
func EnableUndoForWorkflowManager(workflowManager *WorkflowManager, undoManager *UndoManager) *WorkflowManagerWithUndo {
	// Also wrap the orchestrator's patch engine
	if workflowManager.orchestrator != nil {
		EnableUndoForOrchestrator(workflowManager.orchestrator, undoManager)
	}

	return NewWorkflowManagerWithUndo(workflowManager, undoManager)
}

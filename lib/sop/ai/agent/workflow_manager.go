package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai/agent/capabilities"
	"github.com/sharedcode/sop/ai/memory"
)

// WorkflowManager manages KB-driven workflow orchestration.
// It retrieves workflow definitions from the KnowledgeBase and executes them
// by injecting work items into the queue.
type WorkflowManager struct {
	kb                *memory.KnowledgeBase[map[string]any]
	conditionRegistry map[string]ConditionEvaluator
	scriptLibraryPath string
	scriptExecutor    ScriptExecutor
}

// ScriptExecutor is an interface for executing optimization scripts.
// This allows for dependency injection and testing.
type ScriptExecutor interface {
	ExecuteScript(ctx context.Context, scriptPath string, input map[string]any) (map[string]any, error)
}

// Ensure WorkflowManager implements the interface needed by codingworkflow
var _ interface {
	GetWorkflow(ctx context.Context, query string) (interface{}, error)
	Execute(ctx context.Context, workflow interface{}, queue capabilities.WorkQueue, originalQuery string) error
} = (*WorkflowManager)(nil)

// NewWorkflowManager creates a new WorkflowManager with the given KnowledgeBase.
func NewWorkflowManager(kb *memory.KnowledgeBase[map[string]any]) *WorkflowManager {
	wm := &WorkflowManager{
		kb:                kb,
		conditionRegistry: make(map[string]ConditionEvaluator),
	}

	// Register built-in conditions using workqueue.TurnResult
	wm.RegisterCondition("tests_still_failing", func(result TurnResult) bool {
		return result.TestsFailed && result.Error != nil
	})

	wm.RegisterCondition("has_error_output", func(result TurnResult) bool {
		return result.Error != nil
	})

	wm.RegisterCondition("tests_passing", func(result TurnResult) bool {
		return !result.TestsFailed && result.Error == nil
	})

	return wm
}

// RegisterCondition registers a custom condition evaluator.
func (wm *WorkflowManager) RegisterCondition(name string, evaluator ConditionEvaluator) {
	wm.conditionRegistry[name] = evaluator
}

// GetWorkflow retrieves a workflow definition from the KnowledgeBase.
// It searches by category path or by semantic similarity to the query.
// Returns interface{} for compatibility with codingworkflow package interface.
func (wm *WorkflowManager) GetWorkflow(ctx context.Context, query string) (interface{}, error) {
	return wm.getWorkflowInternal(ctx, query)
}

// getWorkflowInternal is the typed internal method.
func (wm *WorkflowManager) getWorkflowInternal(ctx context.Context, query string) (*WorkflowDefinition, error) {
	if wm.kb == nil {
		return nil, fmt.Errorf("knowledge base is nil")
	}

	// Try searching by category path first (for known workflow paths)
	// Example: "Workflows/TestDebugging/Go/UnitTests"
	if strings.Contains(query, "Workflows/") {
		results, err := wm.kb.SearchByPath(ctx, []memory.PathSearchParam{
			{CategoryPath: query, SearchText: ""},
		})
		if err == nil && len(results) > 0 {
			return wm.parseWorkflowFromItem(&results[0])
		}
	}

	// Fall back to semantic search
	results, err := wm.kb.Search(ctx, []memory.SearchRequest[map[string]any]{
		{Text: query, Limit: 1},
	})
	if err != nil {
		return nil, fmt.Errorf("workflow search failed: %w", err)
	}

	if len(results) == 0 || len(results[0]) == 0 {
		return nil, fmt.Errorf("no workflow found for query: %s", query)
	}

	// Extract the Item from ai.Hit
	hit := results[0][0]

	// Parse UUID from string ID
	itemID, err := sop.ParseUUID(hit.ID)
	if err != nil {
		itemID = sop.NilUUID
	}

	item := memory.Item[map[string]any]{
		ID:         itemID,
		Data:       hit.Payload,
		DocID:      memory.DocIDs(hit.DocID),
		CategoryID: sop.NilUUID, // Not available in Hit
	}

	return wm.parseWorkflowFromItem(&item)
}

// parseWorkflowFromItem parses an Item into a WorkflowDefinition.
func (wm *WorkflowManager) parseWorkflowFromItem(item *memory.Item[map[string]any]) (*WorkflowDefinition, error) {
	var workflow WorkflowDefinition

	// Parse from Data field
	if item.Data != nil {
		data, err := json.Marshal(item.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal workflow data: %w", err)
		}
		if err := json.Unmarshal(data, &workflow); err != nil {
			return nil, fmt.Errorf("failed to parse workflow from data: %w", err)
		}
		return &workflow, nil
	}

	return nil, fmt.Errorf("item data is nil")
}

// Execute executes a workflow by injecting work items into the queue.
// Each workflow step becomes a work item with the specified actor and priority.
// Accepts interface{} for compatibility with codingworkflow package interface.
func (wm *WorkflowManager) Execute(ctx context.Context, workflowInterface interface{}, queue capabilities.WorkQueue, originalQuery string) error {
	workflow, ok := workflowInterface.(*WorkflowDefinition)
	if !ok {
		return fmt.Errorf("invalid workflow type")
	}
	return wm.executeInternal(workflow, queue, originalQuery)
}

// executeInternal is the typed internal method.
func (wm *WorkflowManager) executeInternal(workflow *WorkflowDefinition, queue capabilities.WorkQueue, originalQuery string) error {
	if workflow == nil {
		return fmt.Errorf("workflow is nil")
	}
	if queue == nil {
		return fmt.Errorf("queue is nil")
	}

	// Inject work items for each step
	for _, step := range workflow.Steps {
		query := wm.expandTemplate(step.QueryTemplate, originalQuery)

		// Build condition function if specified
		var condition func(TurnResult) bool
		if step.Condition != nil {
			if evaluator, ok := wm.conditionRegistry[step.Condition.Type]; ok {
				condition = evaluator
			}
		}

		// Create work item
		workItem := WorkItem{
			Query:     query,
			Actor:     step.Actor,
			Priority:  step.Priority,
			Condition: condition,
		}

		if err := queue.Enqueue(workItem); err != nil {
			return fmt.Errorf("failed to enqueue work item: %w", err)
		}
	}

	// Apply actor limits if specified
	// Note: We can't type assert capabilities.WorkQueue to *WorkQueue due to interface signature differences
	// Actor limits are applied at queue construction time instead
	_ = workflow.ActorLimits // Acknowledge the field exists for future use

	return nil
}

// expandTemplate replaces placeholders in a query template with actual values.
// Supported placeholders: {original_query}
func (wm *WorkflowManager) expandTemplate(template string, originalQuery string) string {
	result := strings.ReplaceAll(template, "{original_query}", originalQuery)
	return result
}

// SetScriptLibraryPath sets the directory containing workflow optimization scripts.
func (wm *WorkflowManager) SetScriptLibraryPath(path string) {
	wm.scriptLibraryPath = path
}

// SetScriptExecutor sets the script executor for optimization.
func (wm *WorkflowManager) SetScriptExecutor(executor ScriptExecutor) {
	wm.scriptExecutor = executor
}

// optimizeWithScripts attempts to execute a step using a script instead of LLM.
// Returns (result, true) if script succeeded, (nil, false) if LLM should be used.
func (wm *WorkflowManager) optimizeWithScripts(ctx context.Context, step WorkflowStep, input map[string]any) (map[string]any, bool) {
	// Check if optimization is enabled
	if step.OptimizationProfile == nil || !step.OptimizationProfile.PreferScript {
		return nil, false
	}

	// Check if script executor is available
	if wm.scriptExecutor == nil {
		return nil, false
	}

	// Check if script library path is set
	if wm.scriptLibraryPath == "" {
		return nil, false
	}

	// Try each script in the library
	for _, scriptName := range step.OptimizationProfile.ScriptLibrary {
		scriptPath := filepath.Join(wm.scriptLibraryPath, scriptName+".json")

		// Check if script file exists
		if _, err := os.Stat(scriptPath); err != nil {
			continue
		}

		// Set timeout if specified
		execCtx := ctx
		if step.OptimizationProfile.MaxScriptTime > 0 {
			var cancel context.CancelFunc
			execCtx, cancel = context.WithTimeout(ctx, time.Duration(step.OptimizationProfile.MaxScriptTime)*time.Millisecond)
			defer cancel()
		}

		// Execute script
		result, err := wm.scriptExecutor.ExecuteScript(execCtx, scriptPath, input)
		if err == nil && result != nil {
			// Script succeeded
			return result, true
		}

		// Script failed - try next one or fall back
		if !step.OptimizationProfile.FallbackToLLM {
			// No fallback allowed, return error
			return map[string]any{"error": err.Error()}, true
		}
	}

	// No script succeeded, use LLM
	return nil, false
}

// LoadScript loads a script definition from the library.
func (wm *WorkflowManager) LoadScript(scriptName string) (map[string]any, error) {
	if wm.scriptLibraryPath == "" {
		return nil, fmt.Errorf("script library path not set")
	}

	scriptPath := filepath.Join(wm.scriptLibraryPath, scriptName+".json")
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read script %s: %w", scriptName, err)
	}

	var script map[string]any
	if err := json.Unmarshal(data, &script); err != nil {
		return nil, fmt.Errorf("failed to parse script %s: %w", scriptName, err)
	}

	return script, nil
}

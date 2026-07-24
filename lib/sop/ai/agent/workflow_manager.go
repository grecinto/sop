package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai/agent/capabilities"
	"github.com/sharedcode/sop/ai/memory"
)

// WorkflowManager manages KB-driven workflow orchestration.
// It retrieves workflow definitions from the KnowledgeBase and executes them
// by injecting work items into the queue.
// Supports searching across multiple KBs: Core Playbook (always) + selected Spaces.
//
// Performance: Workflows are cached in-memory to avoid I/O contention with Fleet Learning
// write traffic in the shared core_playbook KB. Cache is refreshed periodically or on-demand.
type WorkflowManager struct {
	kb                *memory.KnowledgeBase[map[string]any]   // Primary KB (Core Playbook)
	selectedKBs       []*memory.KnowledgeBase[map[string]any] // Additional selected KBs
	conditionRegistry map[string]ConditionEvaluator
	scriptLibraryPath string
	scriptExecutor    ScriptExecutor

	// In-memory workflow cache (to avoid I/O contention with Fleet Learning writes)
	workflowCache       map[string]*WorkflowDefinition // key: workflow ID or category path
	workflowCacheMu     sync.RWMutex
	workflowCacheLoaded time.Time
	workflowCacheTTL    time.Duration // 0 = no expiration (refresh only on demand)
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
		workflowCache:     make(map[string]*WorkflowDefinition),
		workflowCacheTTL:  0, // No expiration by default (manual refresh only)
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

// SetSelectedKBs sets additional KBs to search for workflows (beyond Core Playbook).
func (wm *WorkflowManager) SetSelectedKBs(kbs []*memory.KnowledgeBase[map[string]any]) {
	wm.selectedKBs = kbs
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
// Searches across Core Playbook + selected Spaces for matching workflows.
// Uses in-memory cache to avoid I/O contention with Fleet Learning writes.
func (wm *WorkflowManager) getWorkflowInternal(ctx context.Context, query string) (*WorkflowDefinition, error) {
	if wm.kb == nil {
		return nil, fmt.Errorf("knowledge base is nil")
	}

	// Check cache first (read lock)
	wm.workflowCacheMu.RLock()
	if cached, ok := wm.workflowCache[query]; ok {
		// Check TTL if configured
		if wm.workflowCacheTTL == 0 || time.Since(wm.workflowCacheLoaded) < wm.workflowCacheTTL {
			wm.workflowCacheMu.RUnlock()
			return cached, nil
		}
	}
	wm.workflowCacheMu.RUnlock()

	// Cache miss or expired - search KB
	workflow, err := wm.searchWorkflowInKB(ctx, query)
	if err != nil {
		return nil, err
	}

	// Update cache (write lock)
	if workflow != nil {
		wm.workflowCacheMu.Lock()
		wm.workflowCache[query] = workflow
		// Also cache by workflow ID for faster lookups
		if workflow.ID != "" {
			wm.workflowCache[workflow.ID] = workflow
		}
		wm.workflowCacheMu.Unlock()
	}

	return workflow, nil
}

// searchWorkflowInKB performs the actual KB search (uncached).
func (wm *WorkflowManager) searchWorkflowInKB(ctx context.Context, query string) (*WorkflowDefinition, error) {
	// Collect all KBs to search: Core Playbook + selected Spaces
	kbsToSearch := []*memory.KnowledgeBase[map[string]any]{wm.kb}
	kbsToSearch = append(kbsToSearch, wm.selectedKBs...)

	// Try searching by category path first (for known workflow paths)
	// Example: "Workflows/TestDebugging/Go/UnitTests"
	if strings.Contains(query, "Workflows/") {
		for _, kb := range kbsToSearch {
			if kb == nil {
				continue
			}
			results, err := kb.SearchByPath(ctx, []memory.PathSearchParam{
				{CategoryPath: query, SearchText: ""},
			})
			if err == nil && len(results) > 0 {
				if workflow, err := wm.parseWorkflowFromItem(&results[0]); err == nil && workflow != nil {
					return workflow, nil
				}
			}
		}
	}

	// Fall back to semantic search across all KBs
	for _, kb := range kbsToSearch {
		if kb == nil {
			continue
		}

		results, err := kb.Search(ctx, []memory.SearchRequest[map[string]any]{
			{Text: query, Limit: 3}, // Get top 3 matches per KB
		})
		if err != nil {
			continue // Try next KB
		}

		if len(results) > 0 && len(results[0]) > 0 {
			// Check each hit for workflow data
			for _, hit := range results[0] {
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

				if workflow, err := wm.parseWorkflowFromItem(&item); err == nil && workflow != nil {
					return workflow, nil // Return first valid workflow found
				}
			}
		}

		// Try scanning this KB if semantic search didn't find workflows
		if workflow, err := wm.findWorkflowInKB(ctx, kb, query); err == nil && workflow != nil {
			return workflow, nil
		}
	}

	return nil, fmt.Errorf("no workflow found for query: %s", query)
}

func (wm *WorkflowManager) findWorkflowInKB(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], query string) (*WorkflowDefinition, error) {
	if kb == nil {
		return nil, nil
	}

	items, _, err := kb.ListItems(ctx, memory.ListItemsParam{Limit: 1000})
	if err != nil {
		return nil, nil
	}

	for _, item := range items {
		if item.Data == nil {
			continue
		}
		if workflow, err := wm.parseWorkflowFromItem(&item); err == nil && workflow != nil {
			if wm.workflowMatchesQuery(workflow, query) {
				return workflow, nil
			}
		}
	}

	return nil, nil
}

func (wm *WorkflowManager) workflowMatchesQuery(workflow *WorkflowDefinition, query string) bool {
	if workflow == nil {
		return false
	}
	queryText := strings.ToLower(strings.TrimSpace(query))
	if queryText == "" {
		return false
	}
	if strings.Contains(strings.ToLower(workflow.Name), queryText) || strings.Contains(strings.ToLower(workflow.Description), queryText) {
		return true
	}
	for _, step := range workflow.Steps {
		if strings.Contains(strings.ToLower(step.QueryTemplate), queryText) || strings.Contains(strings.ToLower(step.Instructions), queryText) {
			return true
		}
	}
	return false
}

// parseWorkflowFromItem parses an Item into a WorkflowDefinition.
func (wm *WorkflowManager) parseWorkflowFromItem(item *memory.Item[map[string]any]) (*WorkflowDefinition, error) {
	if item == nil {
		return nil, fmt.Errorf("item is nil")
	}
	if item.Data == nil {
		return nil, fmt.Errorf("item data is nil")
	}

	if workflow, err := wm.parseWorkflowFromRawData(item.Data); err != nil {
		return nil, err
	} else if workflow != nil {
		return workflow, nil
	}

	return nil, fmt.Errorf("failed to parse workflow data")
}

func (wm *WorkflowManager) parseWorkflowFromRawData(data map[string]any) (*WorkflowDefinition, error) {
	if data == nil {
		return nil, nil
	}

	if workflowTemplate, ok := data["workflow_template"]; ok {
		workflow, err := wm.buildWorkflowFromTemplate(data, workflowTemplate)
		if err != nil {
			return nil, err
		}
		if workflow != nil {
			return workflow, nil
		}
	}

	var workflow WorkflowDefinition
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal workflow data: %w", err)
	}
	if err := json.Unmarshal(dataJSON, &workflow); err != nil {
		return nil, fmt.Errorf("failed to parse workflow from data: %w", err)
	}
	return &workflow, nil
}

func (wm *WorkflowManager) buildWorkflowFromTemplate(data map[string]any, workflowTemplate any) (*WorkflowDefinition, error) {
	switch template := workflowTemplate.(type) {
	case map[string]any:
		return wm.buildWorkflowDefinitionFromTemplateMap(data, template), nil
	case string:
		var templateMap map[string]any
		if err := json.Unmarshal([]byte(template), &templateMap); err != nil {
			return nil, fmt.Errorf("failed to parse workflow template JSON: %w", err)
		}
		return wm.buildWorkflowDefinitionFromTemplateMap(data, templateMap), nil
	default:
		return nil, nil
	}
}

func (wm *WorkflowManager) buildWorkflowDefinitionFromTemplateMap(data map[string]any, templateMap map[string]any) *WorkflowDefinition {
	workflow := &WorkflowDefinition{}

	workflow.Name = firstStringValue(data, "workflow_name", "name")
	if workflow.Name == "" {
		workflow.Name = firstStringValue(templateMap, "name")
	}
	workflow.Description = firstStringValue(data, "workflow_description", "description")
	if workflow.Description == "" {
		workflow.Description = firstStringValue(templateMap, "description")
	}
	workflow.ProtocolGuidance = firstStringValue(templateMap, "protocol_guidance")
	if workflow.ProtocolGuidance == "" {
		workflow.ProtocolGuidance = firstStringValue(data, "protocol_guidance")
	}
	workflow.ID = firstStringValue(data, "workflow_id", "id")
	if workflow.ID == "" {
		workflow.ID = firstStringValue(templateMap, "workflow_id")
	}

	if steps, err := parseWorkflowSteps(templateMap["steps"]); err == nil {
		workflow.Steps = steps
	}
	if len(workflow.Steps) == 0 {
		if steps, err := parseWorkflowSteps(data["steps"]); err == nil {
			workflow.Steps = steps
		}
	}

	if workflow.Name == "" {
		workflow.Name = firstStringValue(data, "workflow_name")
	}
	if workflow.Description == "" {
		workflow.Description = firstStringValue(data, "description")
	}
	return workflow
}

func firstStringValue(source map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := source[key]; ok {
			switch v := value.(type) {
			case string:
				if strings.TrimSpace(v) != "" {
					return strings.TrimSpace(v)
				}
			}
		}
	}
	return ""
}

func parseWorkflowSteps(raw any) ([]WorkflowStep, error) {
	switch value := raw.(type) {
	case nil:
		return nil, nil
	case []any:
		steps := make([]WorkflowStep, 0, len(value))
		for idx, item := range value {
			stepMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			step := WorkflowStep{
				Step:            idx + 1,
				Title:           firstStringValue(stepMap, "title"),
				Actor:           "assistant",
				Priority:        100 - idx,
				QueryTemplate:   firstStringValue(stepMap, "query_template", "title"),
				Instructions:    firstStringValue(stepMap, "instructions"),
				RequiredContext: parseStringList(stepMap["required_context"]),
				SuccessCriteria: parseStringList(stepMap["success_criteria"]),
				IterationPolicy: firstStringValue(stepMap, "iteration_policy"),
				ToolsExpected:   parseStringList(stepMap["tools_expected"]),
			}
			if stepValue, ok := stepMap["step"]; ok {
				if parsed, err := parseIntValue(stepValue); err == nil {
					step.Step = parsed
				}
			}
			if actorValue, ok := stepMap["actor"]; ok {
				if actor, ok := actorValue.(string); ok && strings.TrimSpace(actor) != "" {
					step.Actor = strings.TrimSpace(actor)
				}
			}
			if priorityValue, ok := stepMap["priority"]; ok {
				if parsed, err := parseIntValue(priorityValue); err == nil {
					step.Priority = parsed
				}
			}
			steps = append(steps, step)
		}
		return steps, nil
	case []map[string]any:
		steps := make([]WorkflowStep, 0, len(value))
		for idx, stepMap := range value {
			step := WorkflowStep{
				Step:            idx + 1,
				Title:           firstStringValue(stepMap, "title"),
				Actor:           "assistant",
				Priority:        100 - idx,
				QueryTemplate:   firstStringValue(stepMap, "query_template", "title"),
				Instructions:    firstStringValue(stepMap, "instructions"),
				RequiredContext: parseStringList(stepMap["required_context"]),
				SuccessCriteria: parseStringList(stepMap["success_criteria"]),
				IterationPolicy: firstStringValue(stepMap, "iteration_policy"),
				ToolsExpected:   parseStringList(stepMap["tools_expected"]),
			}
			if stepValue, ok := stepMap["step"]; ok {
				if parsed, err := parseIntValue(stepValue); err == nil {
					step.Step = parsed
				}
			}
			if actorValue, ok := stepMap["actor"]; ok {
				if actor, ok := actorValue.(string); ok && strings.TrimSpace(actor) != "" {
					step.Actor = strings.TrimSpace(actor)
				}
			}
			if priorityValue, ok := stepMap["priority"]; ok {
				if parsed, err := parseIntValue(priorityValue); err == nil {
					step.Priority = parsed
				}
			}
			steps = append(steps, step)
		}
		return steps, nil
	default:
		return nil, nil
	}
}

func parseStringList(raw any) []string {
	switch value := raw.(type) {
	case nil:
		return nil
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil
		}
		parts := strings.Split(trimmed, ",")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				result = append(result, part)
			}
		}
		return result
	case []string:
		result := make([]string, 0, len(value))
		for _, item := range value {
			item = strings.TrimSpace(item)
			if item != "" {
				result = append(result, item)
			}
		}
		return result
	case []any:
		result := make([]string, 0, len(value))
		for _, item := range value {
			if str, ok := item.(string); ok {
				str = strings.TrimSpace(str)
				if str != "" {
					result = append(result, str)
				}
			}
		}
		return result
	default:
		return nil
	}
}

func parseIntValue(raw any) (int, error) {
	switch value := raw.(type) {
	case int:
		return value, nil
	case int64:
		return int(value), nil
	case float64:
		return int(value), nil
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0, fmt.Errorf("empty integer")
		}
		var parsed int
		_, err := fmt.Sscanf(trimmed, "%d", &parsed)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unsupported integer value")
	}
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

		workflowContext := WorkflowExecutionContext{
			WorkflowID:          workflow.ID,
			WorkflowName:        workflow.Name,
			WorkflowDescription: workflow.Description,
			ProtocolGuidance:    workflow.ProtocolGuidance,
			StepNumber:          step.Step,
			StepTitle:           step.Title,
			StepActor:           step.Actor,
			StepInstructions:    step.Instructions,
			RequiredContext:     append([]string(nil), step.RequiredContext...),
			SuccessCriteria:     append([]string(nil), step.SuccessCriteria...),
			IterationPolicy:     step.IterationPolicy,
			ToolsExpected:       append([]string(nil), step.ToolsExpected...),
		}

		// Create work item
		workItem := WorkItem{
			Query:     query,
			Actor:     step.Actor,
			Priority:  step.Priority,
			Condition: condition,
			Context: map[string]any{
				"workflow_step": workflowContext,
			},
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

// =====================================================================
// Workflow Cache Management
// =====================================================================

// RefreshWorkflowCache preloads all workflows from Core Playbook into memory.
// This should be called at daemon startup to avoid I/O contention with Fleet Learning writes.
// Returns count of workflows loaded and any error.
func (wm *WorkflowManager) RefreshWorkflowCache(ctx context.Context) (int, error) {
	if wm.kb == nil {
		return 0, fmt.Errorf("knowledge base is nil")
	}

	// Search for all items under Workflows/ category
	pathReq := memory.PathSearchParam{
		CategoryPath: "Workflows/",
		SearchText:   "",
	}

	items, err := wm.kb.SearchByPath(ctx, []memory.PathSearchParam{pathReq})
	if err != nil {
		// Fall back to semantic search if path search fails
		searchReq := memory.SearchRequest[map[string]any]{
			Text:  "workflow",
			Limit: 500,
		}
		batch, err2 := wm.kb.Search(ctx, []memory.SearchRequest[map[string]any]{searchReq})
		if err2 != nil {
			return 0, fmt.Errorf("failed to search workflows: %w (path search error: %v)", err2, err)
		}
		if len(batch) == 0 || len(batch[0]) == 0 {
			return 0, nil // No workflows found
		}
		// Convert hits to items (hits have Payload, items have Data)
		items = make([]memory.Item[map[string]any], len(batch[0]))
		for i, hit := range batch[0] {
			items[i] = memory.Item[map[string]any]{
				ID:            sop.UUID{}, // We don't have ID from hit
				CategoryID:    sop.UUID{},
				Data:          hit.Payload,
				EmbeddingText: hit.DocumentContent,
			}
		}
	}

	// Parse and cache workflows
	wm.workflowCacheMu.Lock()
	defer wm.workflowCacheMu.Unlock()

	// Clear existing cache
	wm.workflowCache = make(map[string]*WorkflowDefinition)
	count := 0

	for _, item := range items {
		if item.Data == nil {
			continue
		}

		// Try to parse as WorkflowDefinition
		data, err := json.Marshal(item.Data)
		if err != nil {
			continue
		}

		var workflow WorkflowDefinition
		if err := json.Unmarshal(data, &workflow); err != nil {
			continue
		}

		// Validate it's a real workflow
		if workflow.ID == "" || len(workflow.Steps) == 0 {
			continue
		}

		// Cache by ID and category path
		wm.workflowCache[workflow.ID] = &workflow
		if workflow.CategoryPath != "" {
			wm.workflowCache[workflow.CategoryPath] = &workflow
		}
		count++
	}

	wm.workflowCacheLoaded = time.Now()
	return count, nil
}

// InvalidateWorkflowCache clears the in-memory workflow cache.
// Call this after SA publishes a workflow to force reload on next access.
func (wm *WorkflowManager) InvalidateWorkflowCache() {
	wm.workflowCacheMu.Lock()
	defer wm.workflowCacheMu.Unlock()
	wm.workflowCache = make(map[string]*WorkflowDefinition)
	wm.workflowCacheLoaded = time.Time{} // Zero time = never loaded
}

// InvalidateWorkflow removes a specific workflow from cache.
// Call this when a single workflow is updated.
func (wm *WorkflowManager) InvalidateWorkflow(workflowID string) {
	wm.workflowCacheMu.Lock()
	defer wm.workflowCacheMu.Unlock()
	delete(wm.workflowCache, workflowID)
}

// SetCacheTTL configures the workflow cache time-to-live.
// Set to 0 for manual refresh only (default).
// Set to a duration (e.g., 5*time.Minute) for automatic expiration.
func (wm *WorkflowManager) SetCacheTTL(ttl time.Duration) {
	wm.workflowCacheMu.Lock()
	defer wm.workflowCacheMu.Unlock()
	wm.workflowCacheTTL = ttl
}

// GetCacheStats returns cache statistics for monitoring.
func (wm *WorkflowManager) GetCacheStats() map[string]any {
	wm.workflowCacheMu.RLock()
	defer wm.workflowCacheMu.RUnlock()

	return map[string]any{
		"workflow_count": len(wm.workflowCache),
		"loaded_at":      wm.workflowCacheLoaded.Format(time.RFC3339),
		"ttl_seconds":    wm.workflowCacheTTL.Seconds(),
		"cache_age":      time.Since(wm.workflowCacheLoaded).String(),
	}
}

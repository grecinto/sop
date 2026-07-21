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

	if len(results) > 0 && len(results[0]) > 0 {
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

		if workflow, err := wm.parseWorkflowFromItem(&item); err == nil && workflow != nil {
			return workflow, nil
		}
	}

	if workflow, err := wm.findWorkflowByScanningKB(ctx, query); err != nil {
		return nil, err
	} else if workflow != nil {
		return workflow, nil
	}

	return nil, fmt.Errorf("no workflow found for query: %s", query)
}

func (wm *WorkflowManager) findWorkflowByScanningKB(ctx context.Context, query string) (*WorkflowDefinition, error) {
	if wm.kb == nil {
		return nil, nil
	}

	items, _, err := wm.kb.ListItems(ctx, memory.ListItemsParam{Limit: 1000})
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

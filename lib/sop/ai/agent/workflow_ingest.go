package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/sharedcode/sop/ai/memory"
)

// IngestWorkflow reads a workflow JSON file and ingests it into the KnowledgeBase.
// The workflow is stored at its CategoryPath with the JSON data in the Data field.
func IngestWorkflow(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], workflowPath string) error {
	if kb == nil {
		return fmt.Errorf("knowledge base is nil")
	}

	// Read workflow JSON file
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		return fmt.Errorf("failed to read workflow file: %w", err)
	}

	// Parse workflow
	var workflow WorkflowDefinition
	if err := json.Unmarshal(data, &workflow); err != nil {
		return fmt.Errorf("failed to parse workflow JSON: %w", err)
	}

	// Validate required fields
	if workflow.ID == "" {
		return fmt.Errorf("workflow_id is required")
	}
	if workflow.CategoryPath == "" {
		return fmt.Errorf("category_path is required")
	}
	if len(workflow.Steps) == 0 {
		return fmt.Errorf("workflow must have at least one step")
	}

	// Convert workflow to map for KB storage
	var workflowData map[string]any
	if err := json.Unmarshal(data, &workflowData); err != nil {
		return fmt.Errorf("failed to convert workflow to map: %w", err)
	}

	// Create embedding text from workflow metadata
	embeddingText := fmt.Sprintf("%s\n%s\n%s",
		workflow.Name,
		workflow.Description,
		workflow.CategoryPath,
	)

	// Add trigger patterns to embedding text
	for _, pattern := range workflow.TriggerPatterns {
		embeddingText += "\n" + pattern
	}

	// Upsert into KnowledgeBase
	params := []memory.UpsertItemParam[map[string]any]{
		{
			CategoryPath: workflow.CategoryPath,
			Item: &memory.Item[map[string]any]{
				Data:          workflowData,
				EmbeddingText: embeddingText,
			},
		},
	}

	if err := kb.UpsertItems(ctx, params); err != nil {
		return fmt.Errorf("failed to upsert workflow into KB: %w", err)
	}

	return nil
}

// IngestWorkflowsFromDirectory ingests all JSON workflow files from a directory.
func IngestWorkflowsFromDirectory(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], dirPath string) error {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Only process .json files
		name := entry.Name()
		if len(name) < 5 || name[len(name)-5:] != ".json" {
			continue
		}

		workflowPath := dirPath + "/" + name
		if err := IngestWorkflow(ctx, kb, workflowPath); err != nil {
			return fmt.Errorf("failed to ingest %s: %w", name, err)
		}
		count++
	}

	if count == 0 {
		return fmt.Errorf("no workflow JSON files found in directory")
	}

	return nil
}

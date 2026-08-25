package planner

import (
	"github.com/sharedcode/sop/ai/agent/capabilities"
)

// PhaseToolAdapter provides domain-specific tools for each phase.
// Each domain implements this interface to surface appropriate tools.
type PhaseToolAdapter interface {
	GetTools(phaseID string) []capabilities.ToolDefinition
}

// CodingPhaseToolAdapter provides coding workflow tools for each phase
type CodingPhaseToolAdapter struct{}

func NewCodingPhaseToolAdapter() *CodingPhaseToolAdapter {
	return &CodingPhaseToolAdapter{}
}

func (a *CodingPhaseToolAdapter) GetTools(phaseID string) []capabilities.ToolDefinition {
	taskSchema := `{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {"type": "string", "description": "Task description or query"},
    "working_dir": {"type": "string", "description": "Use 'current' for session working directory (default if omitted). Never pass absolute paths."},
    "active_file": {"type": "string", "description": "Primary file for changes (optional, inferred if omitted)"}
  }
}`

	searchSchema := `{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {"type": "string", "description": "Task description"},
    "search": {"type": "string", "description": "Search query for patterns or code (optional)"}
  }
}`

	patchSchema := `{
  "type": "object",
  "required": ["file_path", "old_content", "new_content"],
  "properties": {
    "file_path": {"type": "string", "description": "Path to file to modify"},
    "old_content": {"type": "string", "description": "Content to replace"},
    "new_content": {"type": "string", "description": "New content"},
    "description": {"type": "string", "description": "Description of change"}
  }
}`

	switch phaseID {
	case "intake":
		return []capabilities.ToolDefinition{{
			Name:        phaseID + "_build_context",
			Description: "OPTIONAL: Builds repository context if task involves code. Call when user mentions files, repos, or code locations. Returns repository structure, files, dependencies.",
			Schema:      taskSchema,
		}}

	case "investigation":
		return []capabilities.ToolDefinition{{
			Name:        phaseID + "_build_context",
			Description: "REQUIRED FIRST STEP: Builds repository context (structure, files, dependencies). Must call before other investigation tools. Returns ContextPack with working directory, active file, repository summary.",
			Schema:      taskSchema,
		}, {
			Name:        phaseID + "_inspect_repo",
			Description: "Inspects current repository state and changed files. WHEN TO CALL: After build_context, to understand current state before analysis. Returns changed files list and diff preview.",
			Schema:      taskSchema,
		}, {
			Name:        phaseID + "_search_repo",
			Description: "Searches repository for patterns, implementations, or code. WHEN TO CALL: To find similar code, error patterns, or existing implementations before concluding something is novel. Returns matching code locations.",
			Schema:      searchSchema,
		}, {
			Name:        phaseID + "_list_changed_files",
			Description: "Lists files with uncommitted changes. WHEN TO CALL: To identify in-progress work and avoid conflicts. Returns list of changed file paths.",
			Schema:      taskSchema,
		}}

	case "planning":
		return []capabilities.ToolDefinition{{
			Name:        phaseID + "_assess_and_route",
			Description: "PLANNING-FIRST ARCHITECTURE: Assesses the user query and determines the appropriate starting phase. Always call this FIRST in planning phase to understand what needs to be done. Analyzes query semantics and creates execution strategy. Returns assessment with recommended starting phase and reasoning.",
			Schema:      taskSchema,
		}, {
			Name:        phaseID + "_create_succession_path",
			Description: "Creates a phase succession path with checkpoints. Call after assess_and_route to define the full sequence of phases needed to complete the task. Returns planned path: [investigation, planning, execution, review, delivery] with checkpoints for re-assessment.",
			Schema: `{
  "type": "object",
  "required": ["phases"],
  "properties": {
    "phases": {"type": "array", "items": {"type": "string"}, "description": "Ordered phase sequence: ['investigation', 'planning', 'execution', 'review', 'delivery']"},
    "checkpoints": {"type": "array", "items": {"type": "string"}, "description": "Checkpoint names: ['after_investigation', 'after_execution']"},
    "reasoning": {"type": "string", "description": "Why this path was chosen"},
    "confidence": {"type": "string", "description": "Confidence level: high|medium|low"}
  }
}`,
		}, {
			Name:        phaseID + "_build_context",
			Description: "REQUIRED FIRST STEP: Builds repository context for planning. Must call before planning_plan_changes. Returns ContextPack with repository structure needed for concrete plan generation.",
			Schema:      taskSchema,
		}, {
			Name:        phaseID + "_inspect_repo",
			Description: "Inspects current repository state. WHEN TO CALL: Before planning, to understand existing code structure and patterns. Returns changed files and current state.",
			Schema:      taskSchema,
		}, {
			Name:        phaseID + "_search_repo",
			Description: "Searches repository for implementation patterns. WHEN TO CALL: Before designing, to find similar implementations and follow established patterns rather than inventing new approaches. Returns matching patterns and code locations.",
			Schema:      searchSchema,
		}, {
			Name:        phaseID + "_list_changed_files",
			Description: "Lists changed files to avoid conflicts. WHEN TO CALL: Before finalizing plan, to ensure plan doesn't conflict with in-progress work. Returns list of changed files.",
			Schema:      taskSchema,
		}, {
			Name:        phaseID + "_plan_changes",
			Description: "Generates concrete execution plan from gathered context. WHEN TO CALL: After build_context, search_repo, and inspect_repo complete. This is the core planning step. Returns ordered list of implementation steps with file/function specifics. IF EMPTY: Requirements may be unclear - ask clarifying question.",
			Schema:      taskSchema,
		}}

	case "execution":
		return []capabilities.ToolDefinition{{
			Name:        phaseID + "_preview_patch",
			Description: "SAFETY REQUIRED: Previews patch before applying. WHEN TO CALL: Before every apply_patch call. Shows user what will change. Returns patch preview with old/new content comparison.",
			Schema:      patchSchema,
		}, {
			Name:        phaseID + "_apply_patch",
			Description: "Applies patch to codebase. SAFETY RULE: Must call preview_patch and get user approval BEFORE calling this. WHEN TO CALL: Only after preview shown and approved. Returns confirmation of applied changes.",
			Schema:      patchSchema,
		}, {
			Name:        phaseID + "_run_tests",
			Description: "Runs tests to validate changes. WHEN TO CALL: After each apply_patch call, to ensure changes work correctly and don't introduce regressions. Returns test results (pass/fail) and output.",
			Schema:      taskSchema,
		}}

	case "review":
		return []capabilities.ToolDefinition{{
			Name:        phaseID + "_build_context",
			Description: "Builds repository context for review. WHEN TO CALL: At review start, to understand current state after execution. Returns repository structure and current state.",
			Schema:      taskSchema,
		}, {
			Name:        phaseID + "_inspect_repo",
			Description: "Inspects repository state to see applied changes. WHEN TO CALL: To review what was changed during execution phase. Returns changed files and diff.",
			Schema:      taskSchema,
		}, {
			Name:        phaseID + "_run_tests",
			Description: "Runs comprehensive tests for validation. WHEN TO CALL: To validate all requirements are met and no regressions introduced. Returns test results and coverage.",
			Schema:      taskSchema,
		}}

	default:
		return nil
	}
}

// StoresPhaseToolAdapter provides Stores domain tools for each phase
type StoresPhaseToolAdapter struct{}

func NewStoresPhaseToolAdapter() *StoresPhaseToolAdapter {
	return &StoresPhaseToolAdapter{}
}

func (a *StoresPhaseToolAdapter) GetTools(phaseID string) []capabilities.ToolDefinition {
	storeTaskSchema := `{
  "type": "object",
  "required": ["database"],
  "properties": {
    "database": {"type": "string", "description": "Target database name"},
    "store": {"type": "string", "description": "Store name (optional if listing all)"}
  }
}`

	storeScanSchema := `{
  "type": "object",
  "required": ["database", "store"],
  "properties": {
    "database": {"type": "string", "description": "Target database name"},
    "store": {"type": "string", "description": "Store name to scan"},
    "prefix": {"type": "string", "description": "Optional key prefix filter"},
    "limit": {"type": "number", "description": "Max records to return (default 100)"}
  }
}`

	storeScriptSchema := `{
  "type": "object",
  "required": ["database", "script"],
  "properties": {
    "database": {"type": "string", "description": "Target database name"},
    "script": {"type": "string", "description": "Multi-step operation script"},
    "dry_run": {"type": "boolean", "description": "Preview without executing (default false)"}
  }
}`

	switch phaseID {
	case "investigation":
		// Read-only operations for understanding store structure and content
		return []capabilities.ToolDefinition{{
			Name:        phaseID + "_list_stores",
			Description: "Lists all stores in the database. WHEN TO CALL: First step to discover available stores before querying. Returns store names and metadata. READ-ONLY operation.",
			Schema:      storeTaskSchema,
		}, {
			Name:        phaseID + "_scan",
			Description: "Scans store records with optional filters. WHEN TO CALL: To understand data shape, content, and patterns. Returns sample records. READ-ONLY operation.",
			Schema:      storeScanSchema,
		}, {
			Name:        phaseID + "_describe",
			Description: "Gets store schema (key structure, value types). WHEN TO CALL: Before query design, to understand data structure. Returns schema information. READ-ONLY operation.",
			Schema:      storeTaskSchema,
		}}

	case "planning":
		// Query design and strategy
		return []capabilities.ToolDefinition{{
			Name:        phaseID + "_design_query",
			Description: "Designs multi-step query plan. WHEN TO CALL: After investigation, before execution. Analyzes requirements and creates execution strategy. Returns query plan with steps.",
			Schema:      storeTaskSchema,
		}}

	case "execution":
		// Write operations (Create, Update, Delete)
		return []capabilities.ToolDefinition{{
			Name:        phaseID + "_execute_script",
			Description: "Executes multi-step store operations. SAFETY RULE: Use dry_run=true first to preview changes. WHEN TO CALL: Only after planning phase and user approval. Returns execution results.",
			Schema:      storeScriptSchema,
		}}

	case "review":
		// Validation after execution
		return []capabilities.ToolDefinition{{
			Name:        phaseID + "_verify_results",
			Description: "Validates execution results against requirements. WHEN TO CALL: After execution, to ensure data integrity and requirements met. Returns validation report.",
			Schema:      storeTaskSchema,
		}}

	default:
		return nil
	}
}

// SpacesPhaseToolAdapter provides Spaces domain tools for each phase
type SpacesPhaseToolAdapter struct{}

func NewSpacesPhaseToolAdapter() *SpacesPhaseToolAdapter {
	return &SpacesPhaseToolAdapter{}
}

func (a *SpacesPhaseToolAdapter) GetTools(phaseID string) []capabilities.ToolDefinition {
	spaceTaskSchema := `{
  "type": "object",
  "required": ["space_name"],
  "properties": {
    "space_name": {"type": "string", "description": "Target space/knowledge base name"},
    "category": {"type": "string", "description": "Optional category path"}
  }
}`

	spaceMutationSchema := `{
  "type": "object",
  "required": ["space_name", "content"],
  "properties": {
    "space_name": {"type": "string", "description": "Target space name"},
    "content": {"type": "string", "description": "Content to mint/store"},
    "category": {"type": "string", "description": "Category path for organization"},
    "metadata": {"type": "object", "description": "Optional metadata"}
  }
}`

	switch phaseID {
	case "investigation":
		// Read-only operations for understanding space structure
		return []capabilities.ToolDefinition{{
			Name:        phaseID + "_read_config",
			Description: "Reads space configuration (routing, persona, tools). WHEN TO CALL: First step to understand space setup. Returns configuration object. READ-ONLY operation.",
			Schema:      spaceTaskSchema,
		}, {
			Name:        phaseID + "_list_items",
			Description: "Lists items in space by category. WHEN TO CALL: To understand existing content and structure. Returns list of items with metadata. READ-ONLY operation.",
			Schema:      spaceTaskSchema,
		}}

	case "execution":
		// Write operations
		return []capabilities.ToolDefinition{{
			Name:        phaseID + "_mint_to_space",
			Description: "Persists content to a space. WHEN TO CALL: To store generated or discovered knowledge. Manages its own write transaction. Returns confirmation.",
			Schema:      spaceMutationSchema,
		}, {
			Name:        phaseID + "_enrich_space",
			Description: "Runs enrichment pipeline on space content. WHEN TO CALL: When user explicitly requests derived knowledge refresh. Returns enrichment results.",
			Schema:      spaceTaskSchema,
		}}

	case "review":
		// Validation
		return []capabilities.ToolDefinition{{
			Name:        phaseID + "_verify_content",
			Description: "Validates space content integrity. WHEN TO CALL: After mutations, to ensure knowledge quality. Returns validation report.",
			Schema:      spaceTaskSchema,
		}}

	default:
		return nil
	}
}

// DesktopPhaseToolAdapter provides Desktop automation tools for each phase
type DesktopPhaseToolAdapter struct{}

func NewDesktopPhaseToolAdapter() *DesktopPhaseToolAdapter {
	return &DesktopPhaseToolAdapter{}
}

func (a *DesktopPhaseToolAdapter) GetTools(phaseID string) []capabilities.ToolDefinition {
	commandSchema := `{
  "type": "object",
  "required": ["command"],
  "properties": {
    "command": {"type": "string", "description": "Shell command to execute"},
    "working_dir": {"type": "string", "description": "Use 'current' for session working directory (default if omitted)"}
  }
}`

	switch phaseID {
	case "planning":
		// Script design
		return []capabilities.ToolDefinition{{
			Name:        phaseID + "_create_script",
			Description: "Creates a script for complex automation. WHEN TO CALL: For multi-step shell operations. Returns script content.",
			Schema:      commandSchema,
		}}

	case "execution":
		// Command execution
		return []capabilities.ToolDefinition{{
			Name:        phaseID + "_execute_command",
			Description: "Executes a shell command. WHEN TO CALL: For single commands or after planning. Returns command output.",
			Schema:      commandSchema,
		}}

	default:
		return nil
	}
}

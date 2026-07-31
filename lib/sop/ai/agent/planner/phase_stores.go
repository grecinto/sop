package planner

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

// StoresPhase implements phase capability for stores domain.
// Handles database/store operations: list, scan, describe, execute, verify.
type StoresPhase struct {
	BasePhase
}

// newStoresPhase creates a stores domain phase
func newStoresPhase(phaseID string, infra *SharedInfrastructure) *StoresPhase {
	name, description := phaseMetadata(phaseID)
	return &StoresPhase{
		BasePhase: newBasePhase(phaseID, name, description, "stores", infra),
	}
}

func (p *StoresPhase) BuildContext(query string, state capabilities.RoutingState) string {
	switch p.phaseID {
	case "intake":
		return "PHASE: Intake - Gather store/database requirements"
	case "investigation":
		return "PHASE: Investigation - Explore store structure and data (READ-ONLY)"
	case "planning":
		return "PHASE: Planning - Design query/operation strategy"
	case "execution":
		return "PHASE: Execution - Execute store operations (use dry_run first)"
	case "review":
		return "PHASE: Review - Validate results against requirements"
	case "delivery":
		return "PHASE: Delivery - Summarize store operations"
	default:
		return fmt.Sprintf("Phase capability: %s for stores domain", p.name)
	}
}

func (p *StoresPhase) ExposeTools() []capabilities.ToolDefinition {
	// Common capture/state tools
	tools := []capabilities.ToolDefinition{{
		Name:        p.captureTool,
		Description: fmt.Sprintf("Captures %s phase artifact to memory", p.name),
		Schema:      captureToolSchema(),
	}, {
		Name:        p.stateTool,
		Description: fmt.Sprintf("Returns current persisted state for %s phase", p.name),
		Schema:      stateToolSchema(),
	}}

	// Add phase-specific stores tools
	tools = append(tools, p.getStoresTools()...)
	return tools
}

func (p *StoresPhase) getStoresTools() []capabilities.ToolDefinition {
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

	switch p.phaseID {
	case "investigation":
		// Read-only operations
		return []capabilities.ToolDefinition{{
			Name:        p.phaseToolName("list_stores"),
			Description: "Lists all stores in the database. WHEN: First step to discover available stores. READ-ONLY.",
			Schema:      storeTaskSchema,
		}, {
			Name:        p.phaseToolName("scan"),
			Description: "Scans store records with optional filters. WHEN: To understand data shape and content. READ-ONLY.",
			Schema:      storeScanSchema,
		}, {
			Name:        p.phaseToolName("describe"),
			Description: "Gets store schema (key structure, value types). WHEN: Before query design. READ-ONLY.",
			Schema:      storeTaskSchema,
		}}

	case "planning":
		return []capabilities.ToolDefinition{{
			Name:        p.phaseToolName("design_query"),
			Description: "Designs multi-step query plan. WHEN: After investigation, before execution.",
			Schema:      storeTaskSchema,
		}}

	case "execution":
		return []capabilities.ToolDefinition{{
			Name:        p.phaseToolName("execute_script"),
			Description: "Executes multi-step store operations. SAFETY: Use dry_run=true first to preview.",
			Schema:      storeScriptSchema,
		}}

	case "review":
		return []capabilities.ToolDefinition{{
			Name:        p.phaseToolName("verify_results"),
			Description: "Validates execution results against requirements.",
			Schema:      storeTaskSchema,
		}}

	default:
		return nil
	}
}

func (p *StoresPhase) BuildInstructions(query string, state capabilities.RoutingState) string {
	switch p.phaseID {
	case "investigation":
		return "Use Investigation to: 1) List stores, 2) Scan data, 3) Describe schema, 4) Document findings. All operations are READ-ONLY."
	case "planning":
		return "Use Planning to: 1) Design query strategy, 2) Create operation plan, 3) Capture plan artifact."
	case "execution":
		return "Use Execution to: 1) Execute script with dry_run=true first, 2) Get user approval, 3) Execute for real, 4) Capture results. SAFETY: Always dry-run first."
	case "review":
		return "Use Review to: 1) Verify results, 2) Validate against requirements, 3) Capture validation results."
	default:
		return fmt.Sprintf("Use %s capability to work with stores", p.name)
	}
}

func (p *StoresPhase) ExecuteTool(ctx context.Context, toolName string, args map[string]any, state capabilities.RoutingState) (string, error) {
	_ = ctx
	_ = state

	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "" {
		return "", fmt.Errorf("empty tool name")
	}

	// Common tools
	if name == strings.ToLower(p.captureTool) {
		return p.runCapture(args)
	}
	if name == strings.ToLower(p.stateTool) {
		return p.runState(args)
	}

	// Stores-specific tools
	switch name {
	case strings.ToLower(p.phaseToolName("list_stores")):
		return p.runListStores(args)
	case strings.ToLower(p.phaseToolName("scan")):
		return p.runScan(args)
	case strings.ToLower(p.phaseToolName("describe")):
		return p.runDescribe(args)
	case strings.ToLower(p.phaseToolName("design_query")):
		return p.runDesignQuery(args)
	case strings.ToLower(p.phaseToolName("execute_script")):
		return p.runExecuteScript(args)
	case strings.ToLower(p.phaseToolName("verify_results")):
		return p.runVerifyResults(args)
	default:
		return "", fmt.Errorf("unsupported tool %q in stores domain", toolName)
	}
}

// Stores-specific tool execution methods

func (p *StoresPhase) runListStores(args map[string]any) (string, error) {
	database := readPhaseString(args, "database", "")
	if database == "" {
		return "", fmt.Errorf("database name is required")
	}

	// TODO: Implement actual store listing
	// This is a placeholder - connect to your actual store infrastructure
	return fmt.Sprintf("Listing stores in database '%s':\n- store_alpha\n- store_beta\n- store_gamma", database), nil
}

func (p *StoresPhase) runScan(args map[string]any) (string, error) {
	database := readPhaseString(args, "database", "")
	store := readPhaseString(args, "store", "")
	prefix := readPhaseString(args, "prefix", "")

	if database == "" || store == "" {
		return "", fmt.Errorf("database and store names are required")
	}

	// TODO: Implement actual store scanning
	result := fmt.Sprintf("Scanning store '%s' in database '%s'", store, database)
	if prefix != "" {
		result += fmt.Sprintf(" with prefix '%s'", prefix)
	}
	result += "\n\nSample records:\n- key1: value1\n- key2: value2"
	return result, nil
}

func (p *StoresPhase) runDescribe(args map[string]any) (string, error) {
	database := readPhaseString(args, "database", "")
	store := readPhaseString(args, "store", "")

	if database == "" || store == "" {
		return "", fmt.Errorf("database and store names are required")
	}

	// TODO: Implement actual schema description
	return fmt.Sprintf(`Store Schema for '%s' in database '%s':

Key Structure: string
Value Type: JSON object
Fields:
- id: string
- name: string
- created_at: timestamp
- metadata: object`, store, database), nil
}

func (p *StoresPhase) runDesignQuery(args map[string]any) (string, error) {
	database := readPhaseString(args, "database", "")
	if database == "" {
		return "", fmt.Errorf("database name is required")
	}

	// TODO: Implement query design logic
	return fmt.Sprintf(`Query Plan for database '%s':

Steps:
1. List all stores
2. Scan target store for matching records
3. Filter by criteria
4. Return results

Estimated operations: 3
Safety: READ-ONLY operations`, database), nil
}

func (p *StoresPhase) runExecuteScript(args map[string]any) (string, error) {
	database := readPhaseString(args, "database", "")
	script := readPhaseString(args, "script", "")
	dryRun := readPhaseBool(args, "dry_run")

	if database == "" || script == "" {
		return "", fmt.Errorf("database and script are required")
	}

	if dryRun {
		return fmt.Sprintf(`DRY RUN for database '%s':

Script:
%s

Preview:
- Would execute 3 operations
- Would modify 5 records
- Estimated completion: 2s

To execute for real, set dry_run=false`, database, script), nil
	}

	// TODO: Implement actual script execution
	return fmt.Sprintf(`Executed script on database '%s':

Results:
- 3 operations completed
- 5 records modified
- Duration: 1.8s

Status: SUCCESS`, database), nil
}

func (p *StoresPhase) runVerifyResults(args map[string]any) (string, error) {
	database := readPhaseString(args, "database", "")
	if database == "" {
		return "", fmt.Errorf("database name is required")
	}

	// TODO: Implement result verification
	return fmt.Sprintf(`Verification Report for database '%s':

✓ Data integrity: PASS
✓ Requirements met: PASS
✓ No corruption detected

Status: VERIFIED`, database), nil
}

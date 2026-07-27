package codingworkflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

// workQueueContextKey is the context key for accessing the work queue
// This must match the key used in the agent package to avoid import cycles
type workQueueContextKey string

const (
	ctxKeyWorkQueue      workQueueContextKey = "work_queue"
	ctxKeyMultiTurnQuery string              = "multi_turn_query"
)

// WorkItem represents a work item to inject into the queue
type WorkItem struct {
	Query     string
	Actor     string
	Priority  int
	Condition func(interface{}) bool
}

// getWorkQueue retrieves the work queue from context
// Uses string key directly to avoid type mismatch with agent package's workQueueContextKey
func getWorkQueue(ctx context.Context) (capabilities.WorkQueue, bool) {
	// Use the same string value as agent.ctxKeyWorkQueue = workQueueContextKey("work_queue")
	queue, ok := ctx.Value("work_queue").(capabilities.WorkQueue)
	return queue, ok
}

const (
	CapabilityID   = "coding_workflow"
	CapabilityName = "Coding Workflow"
)

// Capability adapts the coding workflow orchestrator into the agent capability plugin contract.
// This capability is code-driven and executes hardcoded workflows.
// It is separate from KB-driven workflows handled by WorkflowManager.
type Capability struct {
	Orchestrator *Orchestrator
}

func NewCapability(orchestrator *Orchestrator) *Capability {
	return &Capability{
		Orchestrator: orchestrator,
	}
}

func (c *Capability) ID() string   { return CapabilityID }
func (c *Capability) Name() string { return CapabilityName }
func (c *Capability) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{
		EnabledByDefault: false,
		Tier:             "local",
		RequiredGate:     capabilities.GateWriteAccess, // Gap 1: Requires write access
		Approvals: []capabilities.ApprovalType{
			capabilities.ApprovalFileWrite, // File modifications need approval
		},
		Permissions: []capabilities.Permission{
			{
				Resource: "filesystem",
				Actions:  []string{"read", "write", "create", "delete"},
				Gate:     capabilities.GateWriteAccess,
			},
			{
				Resource: "shell",
				Actions:  []string{"read"},
				Gate:     capabilities.GateReadAccess,
			},
		},
	}
}

func (c *Capability) CanHandle(query string, state capabilities.RoutingState) bool {
	if c == nil || c.Orchestrator == nil {
		return false
	}
	if hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint)); hint != "" {
		if hint == "coding_workflow" || hint == "codingworkflow" || hint == "repo_edit" {
			return true
		}
		if hint == "desktop_automation" || hint == "local_ops" {
			return false
		}
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return false
	}
	return strings.Contains(q, "fix") ||
		strings.Contains(q, "refactor") ||
		strings.Contains(q, "patch") ||
		strings.Contains(q, "implement") ||
		strings.Contains(q, "modify") ||
		strings.Contains(q, "search") ||
		strings.Contains(q, "edit") ||
		strings.Contains(q, "repo") ||
		strings.Contains(q, "code") ||
		strings.Contains(q, "parser") ||
		strings.Contains(q, "function") ||
		strings.Contains(q, "class") ||
		strings.Contains(q, "module") ||
		strings.Contains(q, "generate") ||
		strings.Contains(q, "json") ||
		strings.Contains(q, "script") ||
		strings.Contains(q, "lego") ||
		strings.Contains(q, "analy") ||
		strings.Contains(q, "analysis") ||
		strings.Contains(q, "sql") ||
		strings.Contains(q, "record") ||
		strings.Contains(q, "table") ||
		strings.Contains(q, "design") ||
		strings.Contains(q, "plan") ||
		strings.Contains(q, "architect") ||
		strings.Contains(q, "propose") ||
		strings.Contains(q, "document") ||
		strings.Contains(q, "readme") ||
		strings.Contains(q, "spec") ||
		strings.Contains(q, "requirement")
}

func (c *Capability) BuildContext(query string, state capabilities.RoutingState) string {
	return "Structured Context: Coding Workflow\n- Use the local workflow for code edits, structured generation, and analysis tasks.\n- Prefer a design-first approach: understand the requirement, shape the change, and make the implementation plan explicit.\n- Use TDD as a lightweight companion to the design: draft or review a focused test case when a code change is underway, without turning tests into a mandatory gate.\n- Preserve continuity, plan steps explicitly, and keep actions previewable and safe."
}

func (c *Capability) ExposeTools() []capabilities.ToolDefinition {
	return []capabilities.ToolDefinition{{Name: "coding_workflow", Description: "Runs the local workflow for repository inspection, patch preview, safe edits, artifact generation, and analysis.", Schema: "{}"}}
}

func (c *Capability) BuildInstructions(query string, state capabilities.RoutingState) string {
	return "Use the coding workflow capability for local-runtime code, generation, and analysis tasks. Start from the design intent, make the plan explicit, and use TDD as an assistive practice by drafting or reviewing focused tests while implementing. Keep actions explicit, previewable, and safe, and preserve continuation context across turns.\n\n" + CodeFormatInstructions
}

// Orchestrate implements MultiTurnCapability for queue-based multi-turn orchestration.
// This enables the coding workflow to inject work items across multiple ReAct turns.
func (c *Capability) Orchestrate(ctx context.Context, state capabilities.RoutingState) (bool, error) {
	if c == nil || c.Orchestrator == nil {
		return false, fmt.Errorf("coding workflow capability requires an orchestrator")
	}

	// Get the work queue from context
	queue, ok := getWorkQueue(ctx)
	if !ok {
		return false, fmt.Errorf("work queue not found in context")
	}

	// Get the initial query from context
	query, ok := ctx.Value("multi_turn_query").(string)
	if !ok || query == "" {
		return false, fmt.Errorf("query not found in context")
	}

	// Detect test-fix requests (Phase 1.5)
	// This takes priority over general classification
	lowerQuery := strings.ToLower(query)
	isTestFix := strings.Contains(lowerQuery, "fix") &&
		(strings.Contains(lowerQuery, "test") ||
			strings.Contains(lowerQuery, "failing") ||
			strings.Contains(lowerQuery, "failed"))

	if isTestFix {
		return c.orchestrateTestFixWorkflow(ctx, queue, query)
	}

	// Classify the task to determine workflow type
	classifier := DefaultTaskClassifier()
	classification := classifier.Classify(query, "", "")

	// Inject work items based on task type
	switch classification.TaskType {
	case TaskTypeDebugging:
		return c.orchestrateDebuggingWorkflow(ctx, queue, query)
	case TaskTypeImplementation:
		return c.orchestrateImplementationWorkflow(ctx, queue, query)
	case TaskTypeTesting:
		return c.orchestrateTestingWorkflow(ctx, queue, query)
	case TaskTypeRefactoring:
		return c.orchestrateRefactoringWorkflow(ctx, queue, query)
	case TaskTypeArchitecture:
		return c.orchestrateArchitectureWorkflow(ctx, queue, query)
	case TaskTypeDocumentation:
		return c.orchestrateDocumentationWorkflow(ctx, queue, query)
	default:
		// For unknown/other task types, use general workflow
		return c.orchestrateGeneralWorkflow(ctx, queue, query)
	}
}

// orchestrateDebuggingWorkflow injects work items for debugging tasks
func (c *Capability) orchestrateDebuggingWorkflow(ctx context.Context, queue capabilities.WorkQueue, query string) (bool, error) {
	// Debugging workflow: analyze → identify → fix → test
	EmitWorkflowPhase(ctx, 1, "Analyzing code to identify the bug")
	queue.Enqueue(map[string]interface{}{
		"query":    fmt.Sprintf("Analyze the code to identify the bug: %s", query),
		"actor":    "coding_workflow.analyzer",
		"priority": 10,
	})

	EmitWorkflowPhase(ctx, 2, "Generating patch to fix the bug")
	queue.Enqueue(map[string]interface{}{
		"query":    "Generate patch to fix the identified bug",
		"actor":    "coding_workflow.patcher",
		"priority": 10,
	})

	EmitWorkflowPhase(ctx, 3, "Applying the patch")
	queue.Enqueue(map[string]interface{}{
		"query":    "Apply the patch to fix the bug",
		"actor":    "coding_workflow.applier",
		"priority": 10,
	})

	return true, nil
}

// orchestrateTestFixWorkflow injects work items for test fixing tasks (Phase 1.5).
// This implements the 7-step test debugging protocol using queue-based orchestration.
//
// Workflow: Run tests → Read errors → Read test → Read impl → Apply fix → Verify → Iterate
//
// Step 4: Actor names are clearly defined:
//   - test_debugger.runner: Runs tests with verbose output
//   - test_debugger.analyzer: Reads errors, test source, and implementation
//   - test_debugger.fixer: Applies fixes based on analysis
//   - test_debugger.verifier: Re-runs tests to verify the fix
//   - test_debugger.repair: Iterates on fixes if tests still fail (max 3 iterations)
func (c *Capability) orchestrateTestFixWorkflow(ctx context.Context, queue capabilities.WorkQueue, query string) (bool, error) {
	// Step 1: Run tests with verbose output to capture failure
	EmitWorkflowPhase(ctx, 1, "Running tests with verbose output")
	queue.Enqueue(map[string]interface{}{
		"query":    fmt.Sprintf("Run tests with verbose output to capture the failure: %s", query),
		"actor":    "test_debugger.runner",
		"priority": 100,
	})

	// Step 2: Read error output from stored file
	EmitWorkflowPhase(ctx, 2, "Analyzing test error output")
	queue.Enqueue(map[string]interface{}{
		"query":    "Read the test error output. If the output was stored to a file, use read_tool_result or grep_tool_result to analyze it. Extract the actual error message.",
		"actor":    "test_debugger.analyzer",
		"priority": 90,
	})

	// Step 3: Read test source to understand what's being tested
	EmitWorkflowPhase(ctx, 3, "Reading test source code")
	queue.Enqueue(map[string]interface{}{
		"query":    "Read the failing test source code to understand what it's testing and what functions it calls.",
		"actor":    "test_debugger.analyzer",
		"priority": 80,
	})

	// Step 4: Read implementation code
	EmitWorkflowPhase(ctx, 4, "Analyzing implementation code")
	queue.Enqueue(map[string]interface{}{
		"query":    "Find and read the implementation code that the test is checking. Use search_repo_files or grep_repo_text if needed.",
		"actor":    "test_debugger.analyzer",
		"priority": 70,
	})

	// Step 5: Apply fix based on analysis
	EmitWorkflowPhase(ctx, 5, "Applying fix to resolve test failure")
	queue.Enqueue(map[string]interface{}{
		"query":    "Based on the error message, test code, and implementation code, identify the root cause and apply the fix. Use patch_file for focused changes.",
		"actor":    "test_debugger.fixer",
		"priority": 60,
	})

	// Step 6: Verify the fix by re-running tests
	EmitWorkflowPhase(ctx, 6, "Verifying the fix")
	queue.Enqueue(map[string]interface{}{
		"query":    "Re-run the same test to verify the fix works. Show the passing test output as proof.",
		"actor":    "test_debugger.verifier",
		"priority": 50,
	})

	// Step 7: Iterate if needed (conditional - only if tests still fail)
	// Note: EnqueueConditional is not yet implemented in the queue adapter,
	// so we enqueue unconditionally but with lower priority.
	// The repair actor will be limited to max 3 iterations via SetMaxPerActor.
	queue.Enqueue(map[string]interface{}{
		"query":    "The test still failed. Analyze the new error and iterate on the fix. Return to step 2 (read errors) with the new failure information.",
		"actor":    "test_debugger.repair",
		"priority": 40,
	})

	return true, nil
}

// orchestrateImplementationWorkflow injects work items for implementation tasks
func (c *Capability) orchestrateImplementationWorkflow(ctx context.Context, queue capabilities.WorkQueue, query string) (bool, error) {
	// Implementation workflow: design → implement → test
	EmitWorkflowPhase(ctx, 1, "Designing implementation approach")
	queue.Enqueue(map[string]interface{}{
		"query":    fmt.Sprintf("Design the implementation approach: %s", query),
		"actor":    "coding_workflow.designer",
		"priority": 10,
	})

	EmitWorkflowPhase(ctx, 2, "Generating implementation code")
	queue.Enqueue(map[string]interface{}{
		"query":    "Generate code to implement the design",
		"actor":    "coding_workflow.implementer",
		"priority": 10,
	})

	EmitWorkflowPhase(ctx, 3, "Applying implementation")
	queue.Enqueue(map[string]interface{}{
		"query":    "Apply the implementation",
		"actor":    "coding_workflow.applier",
		"priority": 10,
	})

	return true, nil
}

// orchestrateTestingWorkflow injects work items for testing tasks
func (c *Capability) orchestrateTestingWorkflow(ctx context.Context, queue capabilities.WorkQueue, query string) (bool, error) {
	// Testing workflow: run tests → analyze failures → fix
	EmitWorkflowPhase(ctx, 1, "Running tests")
	queue.Enqueue(map[string]interface{}{
		"query":    fmt.Sprintf("Run tests: %s", query),
		"actor":    "coding_workflow.tester",
		"priority": 10,
	})

	return true, nil
}

// orchestrateRefactoringWorkflow injects work items for refactoring tasks
func (c *Capability) orchestrateRefactoringWorkflow(ctx context.Context, queue capabilities.WorkQueue, query string) (bool, error) {
	// Refactoring workflow: analyze → plan → refactor → test
	EmitWorkflowPhase(ctx, 1, "Analyzing code structure for refactoring")
	queue.Enqueue(map[string]interface{}{
		"query":    fmt.Sprintf("Analyze code structure for refactoring: %s", query),
		"actor":    "coding_workflow.analyzer",
		"priority": 10,
	})

	EmitWorkflowPhase(ctx, 2, "Generating refactoring plan")
	queue.Enqueue(map[string]interface{}{
		"query":    "Generate refactoring plan",
		"actor":    "coding_workflow.planner",
		"priority": 10,
	})

	EmitWorkflowPhase(ctx, 3, "Applying refactoring changes")
	queue.Enqueue(map[string]interface{}{
		"query":    "Apply refactoring changes",
		"actor":    "coding_workflow.refactorer",
		"priority": 10,
	})

	return true, nil
}

// orchestrateGeneralWorkflow injects work items for general/unknown tasks
func (c *Capability) orchestrateGeneralWorkflow(ctx context.Context, queue capabilities.WorkQueue, query string) (bool, error) {
	// General workflow: just execute the query as-is
	// The initial user query is already in the queue, so we don't need to add anything
	EmitProgressMessage(ctx, "Processing request")
	return true, nil
}

// orchestrateArchitectureWorkflow injects work items for design/architecture tasks
// Leverages AST parsing, tree-sitter, and repo analysis infrastructure for deep code understanding
func (c *Capability) orchestrateArchitectureWorkflow(ctx context.Context, queue capabilities.WorkQueue, query string) (bool, error) {
	// Architecture workflow: analyze → design → validate → document
	// Step 1: Analyze existing codebase structure with AST and tree-sitter
	EmitWorkflowPhase(ctx, 1, "Analyzing codebase structure with AST")
	queue.Enqueue(map[string]interface{}{
		"query":    fmt.Sprintf("Analyze the current codebase structure and architecture using AST analysis: %s", query),
		"actor":    "architect.analyzer",
		"priority": 100,
	})

	// Step 2: Identify components, dependencies, and constraints
	EmitWorkflowPhase(ctx, 2, "Mapping components and dependencies")
	queue.Enqueue(map[string]interface{}{
		"query":    "Map out existing components, their dependencies, and identify architectural constraints.",
		"actor":    "architect.mapper",
		"priority": 90,
	})

	// Step 3: Propose architecture design with component breakdown
	EmitWorkflowPhase(ctx, 3, "Designing architecture proposal")
	queue.Enqueue(map[string]interface{}{
		"query":    "Design the architecture with clear component breakdown, interfaces, and data flows. Use FormatArchitectureProposal or FormatCodeProposal to show code examples.",
		"actor":    "architect.designer",
		"priority": 80,
	})

	// Step 4: Validate design against requirements and best practices
	EmitWorkflowPhase(ctx, 4, "Validating design against best practices")
	queue.Enqueue(map[string]interface{}{
		"query":    "Validate the proposed design against requirements, constraints, and architectural best practices.",
		"actor":    "architect.validator",
		"priority": 70,
	})

	// Step 5: Generate Architecture Decision Records (ADRs) or design documentation
	EmitWorkflowPhase(ctx, 5, "Documenting architecture decision")
	queue.Enqueue(map[string]interface{}{
		"query":    "Document the architecture decision with rationale, alternatives considered, and consequences.",
		"actor":    "architect.documenter",
		"priority": 60,
	})

	return true, nil
}

// orchestrateDocumentationWorkflow injects work items for documentation generation tasks
// Uses AST parsing and code analysis to generate accurate, comprehensive documentation
func (c *Capability) orchestrateDocumentationWorkflow(ctx context.Context, queue capabilities.WorkQueue, query string) (bool, error) {
	// Documentation workflow: analyze → extract → generate → review
	// Step 1: Analyze code structure with AST to understand what needs documenting
	EmitWorkflowPhase(ctx, 1, "Analyzing code structure with AST")
	queue.Enqueue(map[string]interface{}{
		"query":    fmt.Sprintf("Analyze the code structure with AST to identify functions, types, and interfaces to document: %s", query),
		"actor":    "documenter.analyzer",
		"priority": 100,
	})

	// Step 2: Extract existing documentation and identify gaps
	EmitWorkflowPhase(ctx, 2, "Extracting existing documentation and identifying gaps")
	queue.Enqueue(map[string]interface{}{
		"query":    "Extract existing comments and documentation, identify missing or incomplete documentation.",
		"actor":    "documenter.extractor",
		"priority": 90,
	})

	// Step 3: Generate documentation (README, godoc comments, API docs)
	EmitWorkflowPhase(ctx, 3, "Generating comprehensive documentation")
	queue.Enqueue(map[string]interface{}{
		"query":    "Generate comprehensive documentation including package overview, function descriptions, usage examples with code blocks, and API references. Use FormatSingleCodeBlock for examples.",
		"actor":    "documenter.writer",
		"priority": 80,
	})

	// Step 4: Review documentation for completeness and clarity
	EmitWorkflowPhase(ctx, 4, "Reviewing documentation for accuracy")
	queue.Enqueue(map[string]interface{}{
		"query":    "Review generated documentation for accuracy, completeness, and clarity. Ensure examples are correct.",
		"actor":    "documenter.reviewer",
		"priority": 70,
	})

	return true, nil
}

func (c *Capability) Run(query string, state capabilities.RoutingState) (capabilities.CapabilityResult, error) {
	if c == nil || c.Orchestrator == nil {
		return capabilities.CapabilityResult{}, fmt.Errorf("coding workflow capability requires an orchestrator")
	}
	workingDir, activeFile := inferTaskInputs(query)
	metadata := map[string]string{"query": query}
	if state.Domain != "" {
		metadata["domain"] = state.Domain
	}
	if workingDir != "" {
		metadata["working_dir"] = workingDir
	}
	if activeFile != "" {
		metadata["active_file"] = activeFile
	}
	task := Task{Description: query, ActiveFile: activeFile, State: RunState{Args: map[string]string{"query": query}, Metadata: metadata}}
	if workingDir != "" {
		task.WorkingDir = workingDir
	}
	result, err := c.Orchestrator.Execute(task)
	if err != nil {
		// Check if this is an approval requirement
		if approvalErr, ok := err.(*ApprovalRequiredError); ok {
			// Return structured approval payload that can be emitted as ACTION_REQUIRED
			return capabilities.CapabilityResult{
				Summary: approvalErr.Error(),
				Data:    approvalErr.ToActionPayload(),
			}, fmt.Errorf("<<<ACTION_REQUIRED:%s>>>", mustMarshalJSON(approvalErr.ToActionPayload()))
		}
		return capabilities.CapabilityResult{}, err
	}
	data := map[string]any{
		"message":            result.Message,
		"applied_patches":    len(result.AppliedPatches),
		"task_type":          result.TaskType,
		"plan":               result.Plan,
		"resumed_from_state": result.ResumedFromState,
		"active_file":        result.ContextPack.ActiveFile,
		"working_dir":        result.ContextPack.WorkingDir,
		// Phase 4: KB metadata for frontend display
		"kbs_used":          result.KBsConsulted,
		"rules_applied":     result.RulesApplied,
		"explanation":       result.Explanation,
		"revision_attempts": result.RevisionAttempts,
	}
	if c.Orchestrator != nil {
		data["continuity_signal"] = "task_outcome"
	}

	// Orchestrator now provides conclusive messages - pass through directly
	return capabilities.CapabilityResult{Summary: result.Message, Data: data}, nil
}

func inferTaskInputs(query string) (string, string) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return "", ""
	}
	if workingDir := inferWorkingDir(trimmed); workingDir != "" {
		return workingDir, inferActiveFile(trimmed)
	}
	return "", inferActiveFile(trimmed)
}

func inferWorkingDir(query string) string {
	patterns := []string{"in ", "under ", "from ", "for ", "at ", "repo ", "workspace "}
	for _, pattern := range patterns {
		if idx := strings.Index(strings.ToLower(query), pattern); idx >= 0 {
			candidate := strings.TrimSpace(query[idx+len(pattern):])
			if candidate == "" {
				continue
			}
			parts := strings.Fields(candidate)
			if len(parts) > 0 {
				first := strings.Trim(parts[0], "./")
				if first != "" && strings.Contains(first, "/") {
					return first
				}
			}
		}
	}
	return ""
}

func inferActiveFile(query string) string {
	for _, token := range strings.Fields(query) {
		candidate := strings.TrimSpace(token)
		if candidate == "" {
			continue
		}
		if strings.HasPrefix(candidate, "http") || strings.HasPrefix(candidate, "#") {
			continue
		}
		if strings.Contains(candidate, "/") || strings.HasSuffix(candidate, ".go") || strings.HasSuffix(candidate, ".ts") || strings.HasSuffix(candidate, ".js") || strings.HasSuffix(candidate, ".py") || strings.HasSuffix(candidate, ".md") || strings.HasSuffix(candidate, ".json") {
			return strings.Trim(candidate, ".,")
		}
	}
	return ""
}

func mustMarshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

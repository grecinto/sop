package codingworkflow

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// RunState carries explicit arguments and state for a workflow execution.
// Guard rail: always pass state explicitly through Task.State or RunState rather than
// relying on hidden context or ambient globals. This keeps the workflow inspectable,
// deterministic, and safe for future copilots to extend.
type RunState struct {
	Args     map[string]string
	Metadata map[string]string
}

// Task describes a coding task to execute in a repository.
// Guard rail: keep task inputs explicit and minimal. Prefer small, well-defined fields
// over implicit runtime behavior so tools, patches, and memory can be reasoned about
// in isolation.
type Task struct {
	WorkingDir       string
	ActiveFile       string
	Description      string
	TaskType         string // Workflow type (debugging, testing, implementation, etc.)
	Patch            *Patch
	Patches          []*Patch
	AllowDestructive bool
	State            RunState
}

// StructuredSystemContext captures domain-neutral architectural context for a planned task.
type StructuredSystemContext struct {
	Subsystem         string   `json:"subsystem,omitempty"`
	PrimaryTargets    []string `json:"primary_targets,omitempty"`
	SupportingTargets []string `json:"supporting_targets,omitempty"`
	Interfaces        []string `json:"interfaces,omitempty"`
	Contracts         []string `json:"contracts,omitempty"`
	Invariants        []string `json:"invariants,omitempty"`
	DomainConstraints []string `json:"domain_constraints,omitempty"`
}

// StructuredPlanStep captures one operational step in a structured plan.
type StructuredPlanStep struct {
	StepID             string   `json:"step_id,omitempty"`
	StepDomain         string   `json:"step_domain,omitempty"`
	Goal               string   `json:"goal,omitempty"`
	Files              []string `json:"files,omitempty"`
	EvidenceToCollect  []string `json:"evidence_to_collect,omitempty"`
	DecisionRule       string   `json:"decision_rule,omitempty"`
	AllowedChangeScope string   `json:"allowed_change_scope,omitempty"`
	ActionInstruction  string   `json:"action_instruction,omitempty"`
	Validation         []string `json:"validation,omitempty"`
	ExitCriteria       []string `json:"exit_criteria,omitempty"`
	EscalateIf         []string `json:"escalate_if,omitempty"`
	Status             string   `json:"status,omitempty"`
	Notes              []string `json:"notes,omitempty"`
	UpdatedAt          int64    `json:"updated_at,omitempty"`
}

// StructuredCompletionGate captures final completion requirements for the structured plan.
type StructuredCompletionGate struct {
	Requirements []string `json:"requirements,omitempty"`
}

// StructuredPlan is the first-class domain-neutral execution plan persisted in workstream state.
type StructuredPlan struct {
	Objective      string                   `json:"objective,omitempty"`
	WorkflowShape  string                   `json:"workflow_shape,omitempty"`
	Domain         string                   `json:"domain,omitempty"`
	SystemContext  StructuredSystemContext  `json:"system_context,omitempty"`
	Steps          []StructuredPlanStep     `json:"steps,omitempty"`
	CurrentStep    int                      `json:"current_step,omitempty"`
	CompletedSteps []string                 `json:"completed_steps,omitempty"`
	CompletionGate StructuredCompletionGate `json:"completion_gate,omitempty"`
	CreatedAt      int64                    `json:"created_at,omitempty"`
	UpdatedAt      int64                    `json:"updated_at,omitempty"`
}

// BasePlanFromStructuredPlan projects the structured plan into the legacy step list
// used by older planning and execution paths.
func BasePlanFromStructuredPlan(plan StructuredPlan) []string {
	if len(plan.Steps) == 0 {
		return nil
	}
	base := make([]string, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		text := strings.TrimSpace(step.ActionInstruction)
		if text == "" {
			text = strings.TrimSpace(step.Goal)
		}
		if text != "" {
			base = append(base, text)
		}
	}
	return base
}

// ContextPack is a compact package of repository context for the model.
type ContextPack struct {
	ActiveFile            string
	ActiveFileSnippet     string
	SymbolName            string
	SymbolSnippet         string
	Symbols               []string
	Imports               []string
	CodeStructureSummary  string
	DependencyFiles       []string
	RelatedFiles          []string
	TestFiles             []string
	ASTLeafNodes          []string
	StructuralPath        string
	WorkingDir            string
	ModuleName            string
	RepositoryFiles       []string
	ResumedFromState      bool
	RepositorySummary     string
	RepositoryDiffPreview string
	RecentCodingMemory    string
	PriorPaths            []string
	TaskType              string
	ContinuationSummary   string
	ExpertiseSummary      string
	ExpertiseSources      []string
	ExpertiseCapsule      ExpertiseCapsule // full expertise capsule for validation
	Provider              string
	ProviderSource        string
	ProviderModel         string
	VisualizationSummary  string
}

// Patch describes a targeted file edit.
type Patch struct {
	FilePath     string
	OldContent   string
	NewContent   string
	Description  string
	PatchContent string
}

// CodingMemory captures a compact, high-signal summary of a coding task outcome.
// It is intended to be reusable across STM, MRU, and LTM layers without storing raw patch blobs.
type CodingMemory struct {
	TaskID         string
	Description    string
	WorkingDir     string
	ActiveFile     string
	FilesTouched   []string
	Outcome        string
	TestStatus     string
	Lesson         string
	NextStep       string
	RepositoryHint string
	Plan           []string
	// AST/Tree-sitter structural information for semantic category paths
	StructuralPath string   // e.g., "sop-cloud/ai/agent/Execute"
	ASTLeafNodes   []string // e.g., ["Execute", "BuildContext"]
	// KB expertise tracking (Phase 1 enhancement)
	KBsUsed         []string // which knowledge bases were consulted
	RulesApplied    []string // which specific rules/patterns were applied
	ComplianceScore float64  // 0.0-1.0, how well output matched KB rules
	RevisionCount   int      // how many validation revisions were needed
}

// ContinuitySignal is a lightweight, capability-scoped signal that can later be consumed
// by a shared continuity pipeline module without coupling the workflow to that module.
type ContinuitySignal struct {
	CapabilityID string
	Kind         string
	Summary      string
	Metadata     map[string]string
}

// ExecutionResult captures the outcome of a task run.
type ExecutionResult struct {
	AppliedPatches   []Patch
	ContextPack      ContextPack
	Message          string
	TaskType         string
	Plan             []string
	ResumedFromState bool
	// Validation and KB tracking (Phase 1 enhancement)
	ValidationResult ValidationResult // validation outcome
	RevisionAttempts int              // number of revision loops
	KBsConsulted     []string         // which KBs were used for expertise
	RulesApplied     []string         // which specific rules were referenced
	// Phase 4: UX improvements
	Explanation string // human-readable explanation of KB selection and impact
}

// EvaluationCase captures a single evaluation scenario for a workflow.
type EvaluationCase struct {
	Name           string
	Task           Task
	ExpectContains string
}

// EvaluationResult captures the pass/fail outcome of an evaluation case.
type EvaluationResult struct {
	Name    string
	Passed  bool
	Message string
}

// ApprovalRequiredError is returned when a task requires user approval.
type ApprovalRequiredError struct {
	ActionType               string
	Reasoning                string
	FilePath                 string
	PatchSummary             string
	RequiresInput            bool
	WorkingDir               string
	WorkingDirCandidates     []string
	WorkingDirPromptRequired bool
}

func (e *ApprovalRequiredError) Error() string {
	return fmt.Sprintf("approval required: %s", e.Reasoning)
}

func (e *ApprovalRequiredError) ToActionPayload() map[string]any {
	payload := map[string]any{
		"action_type":       e.ActionType,
		"reasoning":         e.Reasoning,
		"requires_approval": true,
	}
	if e.FilePath != "" {
		payload["file_path"] = e.FilePath
	}
	if e.PatchSummary != "" {
		payload["statement"] = e.PatchSummary
	}
	if e.RequiresInput {
		payload["requires_input"] = true
	}
	if e.WorkingDir != "" {
		payload["working_dir"] = e.WorkingDir
	}
	if len(e.WorkingDirCandidates) > 0 {
		payload["working_dir_candidates"] = e.WorkingDirCandidates
	}
	if e.WorkingDirPromptRequired {
		payload["working_dir_prompt_required"] = true
	}
	return payload
}

// buildKBMetadataJSON creates a compact JSON string for KB metadata
// This is embedded in the response as an HTML comment for frontend parsing
func buildKBMetadataJSON(result ExecutionResult) string {
	metadata := map[string]any{}

	if len(result.KBsConsulted) > 0 {
		metadata["kbs_used"] = result.KBsConsulted
	}
	if len(result.RulesApplied) > 0 {
		metadata["rules_applied"] = result.RulesApplied
	}
	if result.Explanation != "" {
		metadata["explanation"] = result.Explanation
	}
	if result.RevisionAttempts > 0 {
		metadata["revision_attempts"] = result.RevisionAttempts
	}
	if result.TaskType != "" {
		metadata["task_type"] = result.TaskType
	}

	if len(metadata) == 0 {
		return ""
	}

	jsonBytes, err := json.Marshal(metadata)
	if err != nil {
		return ""
	}
	return string(jsonBytes)
}

// buildConclusiveContextMessage creates a clear, conclusive message for context-only execution
func buildConclusiveContextMessage(task Task, pack ContextPack, validationResult ValidationResult, revisionAttempts int) string {
	// Start with status indicator
	status := "✅ Analysis Complete"

	// Build context summary
	contextParts := []string{}
	if pack.CodeStructureSummary != "" {
		contextParts = append(contextParts, fmt.Sprintf("Structure: %s", pack.CodeStructureSummary))
	}
	if pack.ExpertiseSummary != "" {
		contextParts = append(contextParts, fmt.Sprintf("Expertise: %s", pack.ExpertiseSummary))
	}
	if pack.Provider != "" {
		contextParts = append(contextParts, fmt.Sprintf("Provider: %s", pack.Provider))
	}
	if pack.ActiveFile != "" {
		contextParts = append(contextParts, fmt.Sprintf("Active File: %s", pack.ActiveFile))
	}

	// Build task-specific conclusion
	conclusion := ""
	if task.Description != "" {
		switch pack.TaskType {
		case "planning":
			conclusion = fmt.Sprintf("\n\n**Task**: %s\n**Status**: Design context prepared\n**Next Steps**: Review the plan above and request implementation, or refine the design", task.Description)
		case "analysis":
			conclusion = fmt.Sprintf("\n\n**Task**: %s\n**Status**: Analysis context assembled\n**Next Steps**: Review the materials above and provide feedback", task.Description)
		case "generation":
			conclusion = fmt.Sprintf("\n\n**Task**: %s\n**Status**: Generation context ready\n**Next Steps**: Request the artifact generation with the context above", task.Description)
		default:
			conclusion = fmt.Sprintf("\n\n**Task**: %s\n**Status**: Context assembled for editing\n**Next Steps**: Review the files above and request specific changes", task.Description)
		}
	} else {
		conclusion = "\n\n**Status**: Context assembled\n**Next Steps**: Provide task description to proceed"
	}

	// Combine into final message
	contextSummary := ""
	if len(contextParts) > 0 {
		contextSummary = "\n\n" + strings.Join(contextParts, "\n")
	}

	return fmt.Sprintf("%s%s%s", status, contextSummary, conclusion)
}

// buildConclusivePatchMessage creates a clear, conclusive message for patch execution
func buildConclusivePatchMessage(appliedPatches []Patch, testResult TestResult, taskDescription string) string {
	numPatches := len(appliedPatches)
	if numPatches == 0 {
		return "ℹ️ No patches applied\n\n**Status**: Analysis complete\n**Next Steps**: Review context and request specific changes"
	}

	// Determine status
	status := ""
	conclusion := ""

	if testResult.Passed {
		status = "✅ **Task Complete**"
		patchWord := "patch"
		if numPatches > 1 {
			patchWord = "patches"
		}
		conclusion = fmt.Sprintf("\n\n**Applied**: %d %s\n**Tests**: Passed ✓\n**Status**: Changes verified and ready\n**Next Steps**: Review the diff above or proceed to next task", numPatches, patchWord)
	} else {
		status = "⚠️ **Task Incomplete**"
		patchWord := "patch"
		if numPatches > 1 {
			patchWord = "patches"
		}
		conclusion = fmt.Sprintf("\n\n**Applied**: %d %s\n**Tests**: Failed ✗\n**Status**: Changes applied but tests indicate issues\n**Next Steps**: Review test output, adjust changes, or investigate test failures", numPatches, patchWord)
	}

	if taskDescription != "" {
		return fmt.Sprintf("%s\n\n**Task**: %s%s", status, taskDescription, conclusion)
	}

	return fmt.Sprintf("%s%s", status, conclusion)
}

// RunEvaluationSuite executes a lightweight evaluation harness for the legacy
// workflow types.
// The controller-style orchestrator entrypoint is intentionally disabled; new
// callers should use PlannerCapability, MultiStepPlannerCapability, or the
// Ask-driven phase/workstream system instead of reviving workflow-manager
// execution.
func RunEvaluationSuite(sharedInfra *SharedInfrastructure, cases []EvaluationCase) []EvaluationResult {
	results := make([]EvaluationResult, 0, len(cases))
	for _, c := range cases {
		if sharedInfra == nil {
			results = append(results, EvaluationResult{Name: c.Name, Passed: false, Message: "SharedInfrastructure is required"})
			continue
		}
		results = append(results, EvaluationResult{
			Name:    c.Name,
			Passed:  false,
			Message: "controller-style execution is disabled: use the Planner, Multi-Step Planner, or phase/workstream system instead",
		})
	}
	return results
}

// Helper functions for path management

func persistRepositoryPathMemory(memory SessionMemory, task Task, pack ContextPack) error {
	if memory == nil {
		return nil
	}
	paths := collectRepositoryPathHints(task, pack)
	if len(paths) == 0 {
		return nil
	}
	return memory.Save("coding-workflow-paths", map[string]string{"prior_paths": strings.Join(paths, ",")})
}

func collectRepositoryPathHints(task Task, pack ContextPack) []string {
	candidates := []string{}
	appendIfPath := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		value = filepath.ToSlash(strings.TrimPrefix(value, "./"))
		if value == "." {
			return
		}
		if !contains(candidates, value) {
			candidates = append(candidates, value)
		}
	}
	appendIfPath(task.ActiveFile)
	for _, path := range pack.PriorPaths {
		appendIfPath(path)
	}
	for _, path := range pack.RelatedFiles {
		appendIfPath(path)
	}
	for _, path := range pack.DependencyFiles {
		appendIfPath(path)
	}
	for _, path := range pack.TestFiles {
		appendIfPath(path)
	}
	if len(candidates) == 0 {
		return nil
	}
	return candidates
}

func mergePathHints(existing []string, incoming []string) []string {
	merged := append([]string(nil), existing...)
	for _, candidate := range incoming {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		candidate = filepath.ToSlash(strings.TrimPrefix(candidate, "./"))
		if candidate == "." {
			continue
		}
		if !contains(merged, candidate) {
			merged = append(merged, candidate)
		}
	}
	return merged
}

func parsePriorPaths(value string) []string {
	parts := strings.Split(value, ",")
	parsed := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		parsed = append(parsed, filepath.ToSlash(strings.TrimPrefix(trimmed, "./")))
	}
	return parsed
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		if value == "" {
			continue
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

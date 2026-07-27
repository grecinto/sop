package codingworkflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/sharedcode/sop/ai/memory"
	ts "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
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
	Patch            *Patch
	Patches          []*Patch
	AllowDestructive bool
	State            RunState
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

// ContextManager builds a concise context pack for a task.
type ContextManager interface {
	BuildContext(Task, RunState) (ContextPack, error)
}

// Planner produces a structured execution plan for a task based on the assembled context.
type Planner interface {
	Plan(Task, RunState, ContextPack) ([]string, error)
}

// ExpertiseRetriever produces a short, high-signal guidance capsule for a task.
type ExpertiseRetriever interface {
	Retrieve(Task, RunState, ContextPack) (ExpertiseCapsule, error)
}

// ExpertiseCapsule carries a compact guidance summary that can be injected near the point of action.
type ExpertiseCapsule struct {
	Summary  string
	Category string
	Sources  []string
}

// RepoIndexer provides lightweight repository metadata.
type RepoIndexer interface {
	Index(Task, RunState) ([]string, error)
}

// PatchEngine applies a patch to disk.
type PatchEngine interface {
	Apply(Task, *Patch, RunState) error
}

// ChangedFileInspector reports changed files in a repository.
type ChangedFileInspector interface {
	ListChangedFiles(Task, RunState) ([]string, error)
}

// DiffPreviewer creates a preview for a proposed patch.
type DiffPreviewer interface {
	Preview(Task, *Patch, RunState) (string, error)
}

// RepoSearcher collects repository matches for a query.
type RepoSearcher interface {
	Search(Task, RunState, string) ([]string, error)
}

// RepoInspector produces a concise repo-state summary for the workflow.
type RepoInspector interface {
	Inspect(Task, RunState) (RepoInspectionSummary, error)
}

// SessionMemory persists lightweight task state across turns.
type SessionMemory interface {
	Save(string, map[string]string) error
	Load(string) (map[string]string, error)
}

// KnowledgeBaseRetriever reads grounded guidance from repository docs and knowledge files.
type KnowledgeBaseRetriever interface {
	Retrieve(Task, RunState, ContextPack) (ExpertiseCapsule, error)
}

// ProviderRouter resolves the active LLM provider and source for a workflow run.
type ProviderRouter interface {
	Resolve(Task, RunState, ContextPack) (ProviderSelection, error)
}

// ProviderSelection captures the active provider and source used for a workflow run.
type ProviderSelection struct {
	Provider string
	Source   string
	Model    string
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

// SafetyPolicy controls whether a task is allowed to mutate files.
type SafetyPolicy interface {
	Allow(Task, RunState) error
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

// TestRunner executes repository tests and returns a concise result for the workflow loop.
type TestRunner interface {
	Run(Task, RunState) (TestResult, error)
}

// TestResult captures the outcome of a test run.
type TestResult struct {
	Passed bool
	Output string
}

// Orchestrator executes a coding task through the facade-based workflow.
// Guard rail: keep the orchestration layer thin. It should coordinate explicit inputs,
// policy checks, and pluggable components without embedding hidden runtime assumptions.
// Deployment note: this workflow is intended for local-runtime use cases such as an
// end-user daemon or host-side RAG runtime. HQ/site-server deployments should not rely
// on this capability surface unless they explicitly opt into a local-execution adapter.
type Orchestrator struct {
	ContextManager         ContextManager
	RepoIndexer            RepoIndexer
	PatchEngine            PatchEngine
	ChangedFileInspector   ChangedFileInspector
	DiffPreviewer          DiffPreviewer
	RepoSearcher           RepoSearcher
	RepoInspector          RepoInspector
	Memory                 SessionMemory
	Policy                 SafetyPolicy
	TestRunner             TestRunner
	Planner                Planner
	ExpertiseRetriever     ExpertiseRetriever
	KnowledgeBaseRetriever KnowledgeBaseRetriever
	ProviderRouter         ProviderRouter
	MemorySink             func(CodingMemory)
	ContinuitySink         func(ContinuitySignal)
	GrammarRegistry        *GrammarRegistry
	OrchestrationBackend   OrchestrationBackend
	ValidationOrchestrator *ValidationOrchestrator        // Phase 1: KB expertise enforcement
	TaskClassifier         *TaskClassifier                // Phase 2: automatic KB selection
	KBEffectiveness        *memory.KBEffectivenessTracker // Phase 2: historical success tracking
	ExplanationGenerator   *ExplanationGenerator          // Phase 4: explain KB selection and impact
	MaxIterations          int
}

// Execute builds context, applies an optional patch, persists lightweight memory, and
// returns a compact next-step message suitable for a simple tool-calling loop.
// Guard rail: do not mutate files or state without an explicit policy decision and an
// explicit task payload. Prefer preview/approval flows for destructive operations.
func (o *Orchestrator) Execute(task Task) (ExecutionResult, error) {
	if o.Policy != nil {
		if err := o.Policy.Allow(task, task.State); err != nil {
			return ExecutionResult{}, err
		}
	}
	if o.ContextManager == nil {
		o.ContextManager = &DefaultContextManager{}
	}
	if o.RepoIndexer == nil {
		o.RepoIndexer = &DefaultRepoIndexer{}
	}
	if o.PatchEngine == nil {
		o.PatchEngine = &DefaultPatchEngine{}
	}
	if o.Memory == nil {
		if path := strings.TrimSpace(os.Getenv("SOP_CODING_WORKFLOW_MEMORY_PATH")); path != "" {
			o.Memory = NewFileSessionMemory(path)
		} else {
			o.Memory = NewInMemorySessionMemory()
		}
	}
	if o.ChangedFileInspector == nil {
		o.ChangedFileInspector = &GitRepoInspector{}
	}
	if o.DiffPreviewer == nil {
		o.DiffPreviewer = &DefaultDiffPreviewer{}
	}
	if o.RepoSearcher == nil {
		o.RepoSearcher = &FileSearchAdapter{}
	}
	if o.RepoInspector == nil {
		o.RepoInspector = &GitRepoInspector{}
	}
	if o.TestRunner == nil {
		o.TestRunner = &DefaultTestRunner{}
	}
	if o.Planner == nil {
		o.Planner = &DefaultPlanner{}
	}
	if o.ExpertiseRetriever == nil {
		o.ExpertiseRetriever = &DefaultExpertiseRetriever{}
	}
	if o.KnowledgeBaseRetriever == nil {
		o.KnowledgeBaseRetriever = &DefaultKnowledgeBaseRetriever{}
	}
	if o.ProviderRouter == nil {
		o.ProviderRouter = &DefaultProviderRouter{}
	}
	if o.ValidationOrchestrator == nil {
		// Initialize with default configuration: strict mode enabled for KB enforcement
		validator := NewValidator(DefaultValidatorConfig())
		citationConfig := DefaultCitationConfig()
		o.ValidationOrchestrator = NewValidationOrchestrator(validator, citationConfig)
	}
	if o.TaskClassifier == nil {
		// Phase 2: Initialize task classifier for automatic KB selection
		o.TaskClassifier = DefaultTaskClassifier()
	}
	if o.ExplanationGenerator == nil {
		// Phase 4: Initialize explanation generator for user-facing KB explanations
		o.ExplanationGenerator = DefaultExplanationGenerator()
	}
	// Note: KBEffectiveness is optional and should be provided by the orchestrator creator
	// if historical tracking is desired (requires persistence path)
	if o.GrammarRegistry == nil {
		o.GrammarRegistry = NewGrammarRegistry()
	}
	if o.OrchestrationBackend == nil {
		backend, err := NewOrchestrationBackend(os.Getenv("SOP_CODING_ORCHESTRATION_BACKEND"), nil)
		if err == nil && backend != nil {
			o.OrchestrationBackend = backend
		}
	}

	pack, err := o.ContextManager.BuildContext(task, task.State)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("build context: %w", err)
	}
	if o.Memory != nil {
		if memoryValues, err := o.Memory.Load("coding-memory"); err == nil {
			if recent, ok := memoryValues["coding_memory"]; ok {
				pack.RecentCodingMemory = recent
			}
		}
		if memoryValues, err := o.Memory.Load("coding-workflow-paths"); err == nil {
			if priorPaths, ok := memoryValues["prior_paths"]; ok {
				pack.PriorPaths = mergePathHints(pack.PriorPaths, parsePriorPaths(priorPaths))
			}
		}
	}
	if len(pack.PriorPaths) > 0 {
		for _, path := range pack.PriorPaths {
			if path != "" && !contains(pack.RelatedFiles, path) {
				pack.RelatedFiles = append(pack.RelatedFiles, path)
			}
		}
	}
	if _, err := o.RepoIndexer.Index(task, task.State); err != nil {
		return ExecutionResult{}, fmt.Errorf("index repository: %w", err)
	}
	if plan, err := o.Planner.Plan(task, task.State, pack); err == nil && len(plan) > 0 {
		pack.RepositorySummary = strings.Join(plan, " | ")
		if pack.RepositoryDiffPreview == "" {
			pack.RepositoryDiffPreview = pack.RepositorySummary
		}
	}
	if pack.RepositorySummary != "" {
		pack.RepositorySummary = fmt.Sprintf("%s; target=%s", pack.RepositorySummary, strings.TrimSpace(task.ActiveFile))
	}

	// Phase 2: Classify task for intelligent KB selection while preserving the workflow-level task type
	var taskClassification TaskClassification
	if o.TaskClassifier != nil {
		taskClassification = o.TaskClassifier.Classify(task.Description, task.ActiveFile, task.WorkingDir)
		if pack.TaskType == "" {
			pack.TaskType = string(taskClassification.TaskType)
		}

		// Log classification for observability
		if o.ContinuitySink != nil {
			kbNames := []string{}
			for _, kb := range taskClassification.KBs {
				kbNames = append(kbNames, fmt.Sprintf("%s(%.0f%%)", kb.Name, kb.Confidence*100))
			}
			o.ContinuitySink(ContinuitySignal{
				CapabilityID: CapabilityID,
				Kind:         "task_classification",
				Summary:      fmt.Sprintf("Task classified as %s (%.0f%% confidence), recommended KBs: %s", taskClassification.TaskType, taskClassification.Confidence*100, strings.Join(kbNames, ", ")),
				Metadata: map[string]string{
					"task_type":  string(taskClassification.TaskType),
					"confidence": fmt.Sprintf("%.2f", taskClassification.Confidence),
					"kb_count":   fmt.Sprintf("%d", len(taskClassification.KBs)),
				},
			})
		}
	}

	if expertise, err := o.ExpertiseRetriever.Retrieve(task, task.State, pack); err == nil {
		if strings.TrimSpace(expertise.Summary) != "" {
			pack.ExpertiseSummary = expertise.Summary
			pack.ExpertiseSources = expertise.Sources
			pack.ExpertiseCapsule = expertise // store full capsule for validation
		}
	} else {
		_ = err
	}
	if kb, err := o.KnowledgeBaseRetriever.Retrieve(task, task.State, pack); err == nil {
		if strings.TrimSpace(kb.Summary) != "" {
			if strings.TrimSpace(pack.ExpertiseSummary) != "" {
				pack.ExpertiseSummary = strings.TrimSpace(pack.ExpertiseSummary) + " | " + strings.TrimSpace(kb.Summary)
			} else {
				pack.ExpertiseSummary = kb.Summary
			}
			pack.ExpertiseSources = append(pack.ExpertiseSources, kb.Sources...)
		}
	} else {
		_ = err
	}
	if provider, err := o.ProviderRouter.Resolve(task, task.State, pack); err == nil {
		pack.Provider = provider.Provider
		pack.ProviderSource = provider.Source
		pack.ProviderModel = provider.Model
	} else {
		_ = err
	}
	if memorySummary, ok := o.Memory.(*FileSessionMemory); ok {
		if summary, err := memorySummary.BuildVisualizationSummary(); err == nil {
			pack.VisualizationSummary = summary
		}
	}
	if changed, err := o.ChangedFileInspector.ListChangedFiles(task, task.State); err == nil {
		pack.RelatedFiles = append(pack.RelatedFiles, changed...)
	} else {
		_ = err
	}
	if inspection, err := o.RepoInspector.Inspect(task, task.State); err == nil {
		if inspection.DiffPreview != "" {
			pack.RepositorySummary = inspection.DiffPreview
			pack.RepositoryDiffPreview = inspection.DiffPreview
		}
	} else {
		_ = err
	}
	if searchHits, err := o.RepoSearcher.Search(task, task.State, task.ActiveFile); err == nil {
		for _, hit := range searchHits {
			if hit != "" && !contains(pack.RelatedFiles, hit) {
				pack.RelatedFiles = append(pack.RelatedFiles, hit)
			}
		}
	} else {
		_ = err
	}
	patches := []*Patch{}
	if task.Patch != nil {
		patches = append(patches, task.Patch)
	}
	if len(task.Patches) > 0 {
		patches = append(patches, task.Patches...)
	}

	// Phase 1: Validate and enrich patches with KB expertise enforcement
	var validationResult ValidationResult
	var revisionAttempts int
	if len(patches) > 0 && o.ValidationOrchestrator != nil && pack.ExpertiseCapsule.Summary != "" {
		// Validation is enabled and we have expertise to validate against
		finalPatches, vResult, attempts, err := o.ValidationOrchestrator.ValidateAndEnrich(
			patches,
			pack.ExpertiseCapsule,
			task.Description,
			nil, // no revision callback in orchestrator (revisions handled externally)
		)
		if err == nil {
			patches = finalPatches
			validationResult = vResult
			revisionAttempts = attempts
			// Observability: log validation outcome
			if o.ContinuitySink != nil {
				status := "passed"
				if !vResult.Passed {
					status = "failed"
				}
				o.ContinuitySink(ContinuitySignal{
					CapabilityID: CapabilityID,
					Kind:         "validation_outcome",
					Summary:      fmt.Sprintf("KB validation %s: %.0f%% compliance, %d violations, %d revisions", status, vResult.ComplianceScore*100, len(vResult.Violations), attempts),
					Metadata: map[string]string{
						"status":            status,
						"compliance_score":  fmt.Sprintf("%.2f", vResult.ComplianceScore),
						"violations":        fmt.Sprintf("%d", len(vResult.Violations)),
						"revision_attempts": fmt.Sprintf("%d", attempts),
						"rules_checked":     fmt.Sprintf("%d", len(vResult.RulesChecked)),
					},
				})
			}
		}
		// Note: validation errors are logged but don't block execution
		// This ensures the system degrades gracefully if validation fails
	}

	if len(patches) > 0 {
		if _, err := o.ensureTestCoverage(task, pack); err != nil {
			return ExecutionResult{}, fmt.Errorf("prepare tests: %w", err)
		}
		maxIterations := o.MaxIterations
		if maxIterations <= 0 {
			maxIterations = len(patches)
		}
		applied := make([]Patch, 0, len(patches))
		lastTestResult := TestResult{}
		filesTouched := make([]string, 0, len(patches))
		for i := 0; i < maxIterations && i < len(patches); i++ {
			patch := patches[i]
			if preview, err := o.DiffPreviewer.Preview(task, patch, task.State); err == nil {
				_ = preview
			} else {
				_ = err
			}
			if err := o.PatchEngine.Apply(task, patch, task.State); err != nil {
				return ExecutionResult{}, fmt.Errorf("apply patch: %w", err)
			}
			applied = append(applied, *patch)
			if patch.FilePath != "" {
				filesTouched = append(filesTouched, patch.FilePath)
			}
			memoryValues := map[string]string{
				"description":   task.Description,
				"active_file":   task.ActiveFile,
				"applied_patch": patch.Description,
			}
			for key, value := range task.State.Metadata {
				memoryValues[key] = value
			}
			if err := o.Memory.Save("latest-task", memoryValues); err != nil {
				return ExecutionResult{}, fmt.Errorf("persist memory: %w", err)
			}
			testResult, err := o.TestRunner.Run(task, task.State)
			if err != nil {
				return ExecutionResult{}, fmt.Errorf("run tests: %w", err)
			}
			lastTestResult = testResult
			if !testResult.Passed && i+1 < maxIterations {
				continue
			}
		}
		// Build conclusive message for patch execution
		message := buildConclusivePatchMessage(applied, lastTestResult, task.Description)
		codingMemory := CodingMemory{
			Description:    task.Description,
			WorkingDir:     task.WorkingDir,
			ActiveFile:     task.ActiveFile,
			FilesTouched:   filesTouched,
			Outcome:        message,
			TestStatus:     "passed",
			RepositoryHint: pack.RepositorySummary,
			StructuralPath: pack.StructuralPath,
			ASTLeafNodes:   pack.ASTLeafNodes,
		}
		if !lastTestResult.Passed {
			codingMemory.TestStatus = "failed"
		}
		if codingMemory.RepositoryHint == "" {
			codingMemory.RepositoryHint = pack.RepositoryDiffPreview
		}
		if o.MemorySink != nil {
			o.MemorySink(codingMemory)
		}
		if o.ContinuitySink != nil {
			summary := codingMemory.Outcome
			if strings.TrimSpace(codingMemory.Description) != "" {
				summary = fmt.Sprintf("%s: %s", codingMemory.Description, codingMemory.Outcome)
			}
			o.ContinuitySink(ContinuitySignal{
				CapabilityID: CapabilityID,
				Kind:         "task_outcome",
				Summary:      summary,
				Metadata: map[string]string{
					"description": codingMemory.Description,
					"files":       strings.Join(codingMemory.FilesTouched, ","),
					"status":      codingMemory.TestStatus,
				},
			})
		}

		// Phase 2: Record outcome to KB effectiveness tracker
		if o.KBEffectiveness != nil && len(codingMemory.KBsUsed) > 0 {
			// Determine success based on test status and compliance score
			success := codingMemory.TestStatus == "passed" && codingMemory.ComplianceScore >= 0.7
			taskType := pack.TaskType
			if taskType == "" {
				taskType = "implementation" // Default if not classified
			}

			// Record effectiveness for each KB used
			for _, kbName := range codingMemory.KBsUsed {
				o.KBEffectiveness.RecordOutcome(taskType, kbName, success, codingMemory.ComplianceScore)
			}
		}

		// Persist coding memory with KB tracking fields
		memoryStr := fmt.Sprintf("description=%s; outcome=%s; files=%s; status=%s", codingMemory.Description, codingMemory.Outcome, strings.Join(codingMemory.FilesTouched, ","), codingMemory.TestStatus)
		if len(codingMemory.KBsUsed) > 0 {
			memoryStr += fmt.Sprintf("; kbs=%s", strings.Join(codingMemory.KBsUsed, ","))
		}
		if len(codingMemory.RulesApplied) > 0 {
			memoryStr += fmt.Sprintf("; rules=%s", strings.Join(codingMemory.RulesApplied, ","))
		}
		if codingMemory.ComplianceScore > 0 {
			memoryStr += fmt.Sprintf("; compliance=%.0f%%", codingMemory.ComplianceScore*100)
		}
		if codingMemory.RevisionCount > 0 {
			memoryStr += fmt.Sprintf("; revisions=%d", codingMemory.RevisionCount)
		}
		memoryValues := map[string]string{
			"coding_memory": memoryStr,
		}
		if err := o.Memory.Save("coding-memory", memoryValues); err != nil {
			return ExecutionResult{}, fmt.Errorf("persist coding memory: %w", err)
		}
		if err := persistRepositoryPathMemory(o.Memory, task, pack); err != nil {
			return ExecutionResult{}, fmt.Errorf("persist repository path memory: %w", err)
		}
		return ExecutionResult{AppliedPatches: applied, ContextPack: pack, Message: message, TaskType: pack.TaskType, Plan: []string{}, ResumedFromState: pack.ResumedFromState}, nil
	}
	memoryValues := map[string]string{"description": task.Description, "active_file": task.ActiveFile}
	for key, value := range task.State.Metadata {
		memoryValues[key] = value
	}
	if err := o.Memory.Save("latest-task", memoryValues); err != nil {
		return ExecutionResult{}, fmt.Errorf("persist memory: %w", err)
	}
	// Build conclusive message for non-patch execution
	message := buildConclusiveContextMessage(task, pack, validationResult, revisionAttempts)
	if o.OrchestrationBackend != nil {
		message = fmt.Sprintf("%s; orchestration: %s", message, o.OrchestrationBackend.Describe(task, pack))
	}
	if pack.RecentCodingMemory != "" {
		message = fmt.Sprintf("%s; recent coding memory: %s", message, pack.RecentCodingMemory)
	}
	plan := []string{}
	if generatedPlan, err := o.Planner.Plan(task, task.State, pack); err == nil {
		plan = generatedPlan
	}
	if err := persistRepositoryPathMemory(o.Memory, task, pack); err != nil {
		return ExecutionResult{}, fmt.Errorf("persist repository path memory: %w", err)
	}

	// Phase 4: Generate human-readable explanation of KB selection and impact
	explanation := ""
	if o.ExplanationGenerator != nil && taskClassification.TaskType != "" {
		kbsUsed := ExtractKBNamesFromExpertise(pack.ExpertiseCapsule)
		rulesApplied := validationResult.RulesChecked
		complianceScore := validationResult.ComplianceScore

		explanation = o.ExplanationGenerator.GenerateFullExplanation(
			taskClassification,
			kbsUsed,
			rulesApplied,
			validationResult,
			revisionAttempts,
			complianceScore,
		)
	}

	result := ExecutionResult{
		ContextPack:      pack,
		Message:          message,
		TaskType:         pack.TaskType,
		Plan:             plan,
		ResumedFromState: pack.ResumedFromState,
		ValidationResult: validationResult,
		RevisionAttempts: revisionAttempts,
		KBsConsulted:     ExtractKBNamesFromExpertise(pack.ExpertiseCapsule),
		RulesApplied:     validationResult.RulesChecked,
		Explanation:      explanation,
	}

	// Phase 4: Append KB metadata as HTML comment for frontend parsing
	// This enables feedback buttons and explanation display in the chat UI
	if len(result.KBsConsulted) > 0 || result.Explanation != "" {
		metadataJSON := buildKBMetadataJSON(result)
		if metadataJSON != "" {
			result.Message = result.Message + "\n\n<!-- KB_METADATA: " + metadataJSON + " -->"
		}
	}

	return result, nil
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

// DefaultExpertiseRetriever produces concise, high-signal guidance for coding, QA, and review-oriented tasks.
type DefaultExpertiseRetriever struct{}

func (d *DefaultExpertiseRetriever) Retrieve(task Task, state RunState, pack ContextPack) (ExpertiseCapsule, error) {
	_ = state
	description := strings.ToLower(strings.TrimSpace(task.Description))
	category := "coding"
	summaryParts := []string{}

	switch {
	case strings.Contains(description, "review") || strings.Contains(description, "qa") || strings.Contains(description, "test"):
		category = "qa"
		summaryParts = append(summaryParts, "Review the change for correctness, regressions, and edge cases.")
		summaryParts = append(summaryParts, "Prefer focused tests or checks that prove the behavior you intend to preserve.")
	case strings.Contains(description, "implement") || strings.Contains(description, "fix") || strings.Contains(description, "patch") || strings.Contains(description, "refactor") || pack.TaskType == "code":
		summaryParts = append(summaryParts, "Best practice: keep the edit focused, preserve interfaces, and validate behavior with targeted tests.")
	default:
		summaryParts = append(summaryParts, "Best practice: make the change explicit, keep the scope tight, and validate assumptions before concluding.")
	}
	if pack.ActiveFile != "" {
		summaryParts = append(summaryParts, fmt.Sprintf("For %s, inspect the affected path and confirm related coverage before finalizing.", pack.ActiveFile))
	}
	if pack.TaskType == "planning" {
		summaryParts = append(summaryParts, "When moving from design to implementation, capture the acceptance criteria and the likely test seams.")
	}

	sources := []string{"local:default_expertise"}
	if category == "qa" {
		sources = append(sources, "local:qa_guidance")
	} else {
		sources = append(sources, "local:coding_guidance")
	}

	return ExpertiseCapsule{Summary: strings.Join(summaryParts, " "), Category: category, Sources: sources}, nil
}

// DefaultContextManager reads the active file and nearby test files.
type DefaultContextManager struct{}

func (d *DefaultContextManager) BuildContext(task Task, state RunState) (ContextPack, error) {
	workingDir := task.WorkingDir
	if workingDir == "" {
		workingDir = "."
	}
	resolvedActiveFile := task.ActiveFile
	resumedFromState := false
	if state.Metadata != nil {
		if prevActiveFile, ok := state.Metadata["active_file"]; ok && prevActiveFile != "" {
			resolvedActiveFile = prevActiveFile
			resumedFromState = true
		}
		if continuationSummary, ok := state.Metadata["continuation_summary"]; ok && continuationSummary != "" {
			resumedFromState = true
		}
	}
	taskType := classifyTaskType(task, state, resolvedActiveFile)
	var content string
	if resolvedActiveFile != "" {
		activePath := filepath.Join(workingDir, resolvedActiveFile)
		rawContent, readErr := os.ReadFile(activePath)
		if readErr != nil {
			return ContextPack{}, fmt.Errorf("read active file %s: %w", activePath, readErr)
		}
		content = string(rawContent)
	} else {
		content = ""
	}
	related := []string{}
	dependencyFiles := []string{}
	testFiles := []string{}
	symbolName := ""
	symbolSnippet := content
	symbols := collectSymbols(content)
	imports := collectImports(content)
	astLeafNodes, structuralPath := collectASTLeafNodes(content, workingDir, resolvedActiveFile)
	if len(astLeafNodes) > 0 {
		for _, leaf := range astLeafNodes {
			if leaf != "" && !contains(symbols, leaf) {
				symbols = append(symbols, leaf)
			}
		}
		if symbolName == "" && len(astLeafNodes) > 0 {
			symbolName = astLeafNodes[0]
		}
	}
	codeStructureSummary := summarizeCodeStructure(imports, symbols)
	if len(astLeafNodes) > 0 {
		codeStructureSummary = fmt.Sprintf("%s; ast=%s", codeStructureSummary, strings.Join(astLeafNodes, ","))
	}
	if resolvedActiveFile != "" {
		testName := strings.TrimSuffix(resolvedActiveFile, filepath.Ext(resolvedActiveFile)) + "_test.go"
		if testPath := filepath.Join(workingDir, testName); fileExists(testPath) {
			testFiles = append(testFiles, testName)
		}
		if fileExists(filepath.Join(workingDir, "main_test.go")) {
			testFiles = append(testFiles, "main_test.go")
		}
		for _, match := range globTestFiles(workingDir) {
			testFiles = append(testFiles, match)
		}
		related = append(related, resolvedActiveFile)
	}
	if task.Description != "" {
		symbolName = inferSymbolName(task.Description)
	}
	if symbolName == "" {
		symbolName = inferSymbolName(content)
	}
	if symbolName != "" {
		symbolSnippet = extractSymbolSnippet(content, symbolName)
	}
	if len(symbols) == 0 && symbolName != "" {
		symbols = []string{symbolName}
	}
	for _, path := range collectRelatedFiles(workingDir, resolvedActiveFile) {
		if path != "" && !contains(related, path) {
			related = append(related, path)
		}
	}
	for _, path := range collectDependencyFiles(workingDir, resolvedActiveFile, content, symbols) {
		if path != "" && !contains(dependencyFiles, path) {
			dependencyFiles = append(dependencyFiles, path)
		}
	}
	moduleName := inferModuleName(workingDir)
	repoFiles := collectRepositoryFiles(workingDir)
	repositorySummary := ""
	if moduleName != "" {
		repositorySummary = fmt.Sprintf("module=%s", moduleName)
	}
	if len(repoFiles) > 0 {
		if repositorySummary != "" {
			repositorySummary += "; "
		}
		repositorySummary += fmt.Sprintf("files=%s", strings.Join(repoFiles, ","))
	}
	continuationSummary := ""
	if state.Metadata != nil {
		if summary, ok := state.Metadata["continuation_summary"]; ok && summary != "" {
			continuationSummary = summary
		}
	}
	return ContextPack{ActiveFile: resolvedActiveFile, ActiveFileSnippet: content, SymbolName: symbolName, SymbolSnippet: symbolSnippet, Symbols: symbols, Imports: imports, CodeStructureSummary: codeStructureSummary, DependencyFiles: dependencyFiles, RelatedFiles: related, TestFiles: testFiles, ASTLeafNodes: astLeafNodes, StructuralPath: structuralPath, WorkingDir: workingDir, ModuleName: moduleName, RepositoryFiles: repoFiles, RepositorySummary: repositorySummary, ResumedFromState: resumedFromState, TaskType: taskType, ContinuationSummary: continuationSummary}, nil
}

func classifyTaskType(task Task, state RunState, resolvedActiveFile string) string {
	description := strings.ToLower(strings.TrimSpace(task.Description))
	if mode := taskMode(state); mode != "" {
		if mode == "planning" {
			if strings.Contains(description, "implement") || strings.Contains(description, "build") || strings.Contains(description, "patch") || strings.Contains(description, "edit") || strings.Contains(description, "fix") || strings.Contains(description, "refactor") || strings.Contains(description, "change") || strings.Contains(description, "write code") || strings.Contains(description, "add") {
				return "code"
			}
			return "planning"
		}
		if mode == "implement" || mode == "code" || mode == "execution" {
			return "code"
		}
	}
	if strings.Contains(description, "generate") || strings.Contains(description, "json") || strings.Contains(description, "script") || strings.Contains(description, "lego") {
		return "generation"
	}
	if strings.Contains(description, "design") || strings.Contains(description, "brainstorm") || strings.Contains(description, "architecture") || strings.Contains(description, "proposal") || strings.Contains(description, "options") || strings.Contains(description, "tradeoff") {
		return "planning"
	}
	if strings.Contains(description, "discuss") && (strings.Contains(description, "design") || strings.Contains(description, "plan") || strings.Contains(description, "approach") || strings.Contains(description, "tradeoff") || strings.Contains(description, "architecture")) {
		return "planning"
	}
	if strings.Contains(description, "plan") && (strings.Contains(description, "implementation") || strings.Contains(description, "change") || strings.Contains(description, "design") || strings.Contains(description, "feature") || strings.Contains(description, "system")) {
		return "planning"
	}
	if strings.Contains(description, "implement") || strings.Contains(description, "build") || strings.Contains(description, "patch") || strings.Contains(description, "edit") || strings.Contains(description, "fix") || strings.Contains(description, "refactor") || strings.Contains(description, "change") || strings.Contains(description, "write code") || strings.Contains(description, "add") {
		return "code"
	}
	if strings.Contains(description, "analy") || strings.Contains(description, "inspect") || strings.Contains(description, "review") || strings.Contains(description, "summar") || strings.Contains(description, "diagnos") || strings.Contains(description, "propose") || strings.Contains(description, "explain") {
		return "analysis"
	}
	if resolvedActiveFile == "" {
		return "analysis"
	}
	if state.Metadata != nil {
		if continuationSummary, ok := state.Metadata["continuation_summary"]; ok && strings.TrimSpace(continuationSummary) != "" {
			return "analysis"
		}
	}
	return "code"
}

func taskMode(state RunState) string {
	if state.Metadata != nil {
		if mode, ok := state.Metadata["mode"]; ok && strings.TrimSpace(mode) != "" {
			return strings.ToLower(strings.TrimSpace(mode))
		}
		if mode, ok := state.Metadata["conversation_mode"]; ok && strings.TrimSpace(mode) != "" {
			return strings.ToLower(strings.TrimSpace(mode))
		}
	}
	if state.Args != nil {
		if mode, ok := state.Args["mode"]; ok && strings.TrimSpace(mode) != "" {
			return strings.ToLower(strings.TrimSpace(mode))
		}
		if mode, ok := state.Args["conversation_mode"]; ok && strings.TrimSpace(mode) != "" {
			return strings.ToLower(strings.TrimSpace(mode))
		}
	}
	return ""
}

func (o *Orchestrator) ensureTestCoverage(task Task, pack ContextPack) (bool, error) {
	if task.ActiveFile == "" {
		return false, nil
	}
	if task.Patch == nil && len(task.Patches) == 0 {
		return false, nil
	}
	workingDir := task.WorkingDir
	if workingDir == "" {
		workingDir = "."
	}
	activePath := filepath.Join(workingDir, task.ActiveFile)
	if !fileExists(activePath) {
		return false, nil
	}
	baseName := strings.TrimSuffix(filepath.Base(task.ActiveFile), filepath.Ext(task.ActiveFile))
	testFileName := baseName + "_test.go"
	if baseName == "main" {
		testFileName = "main_test.go"
	}
	testPath := filepath.Join(workingDir, testFileName)
	if fileExists(testPath) {
		return false, nil
	}
	packageName := "main"
	if pkg := inferPackageNameFromContent(pack.ActiveFileSnippet); pkg != "" {
		packageName = pkg
	}
	symbolName := "Example"
	if len(pack.Symbols) > 0 {
		symbolName = pack.Symbols[0]
	} else if baseName != "" {
		symbolName = capitalize(baseName)
	}
	expectedValue := ""
	for _, patch := range append([]*Patch{task.Patch}, task.Patches...) {
		if patch == nil {
			continue
		}
		if value := extractQuotedLiteral(patch.NewContent); value != "" {
			expectedValue = value
			break
		}
	}
	if expectedValue == "" {
		expectedValue = "value"
	}
	content := fmt.Sprintf("package %s\n\nimport \"testing\"\n\nfunc Test%s(t *testing.T) {\n\tif got := %s(); got != %q {\n\t\tt.Fatalf(\"%s() = %%q, want %%q\", got, %q)\n\t}\n}\n", packageName, symbolName, symbolName, expectedValue, symbolName, expectedValue)
	if err := os.WriteFile(testPath, []byte(content), 0o644); err != nil {
		return false, fmt.Errorf("write test file %s: %w", testPath, err)
	}
	return true, nil
}

func inferPackageNameFromContent(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "package ") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}
	return ""
}

func extractQuotedLiteral(input string) string {
	trimmed := strings.TrimSpace(input)
	if len(trimmed) >= 2 && strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"") {
		return strings.Trim(trimmed, "\"")
	}
	return ""
}

func capitalize(input string) string {
	if input == "" {
		return ""
	}
	if len(input) == 1 {
		return strings.ToUpper(input)
	}
	return strings.ToUpper(input[:1]) + input[1:]
}

// DefaultPlanner creates a concise execution plan for an edit task.
type DefaultPlanner struct{}

func (d *DefaultPlanner) Plan(task Task, state RunState, pack ContextPack) ([]string, error) {
	_ = state
	plan := []string{}
	if strings.TrimSpace(pack.ExpertiseSummary) != "" {
		plan = append(plan, "gather grounding from the selected Space and expertise guidance")
	}
	plan = append(plan, "inspect relevant files")
	if task.Description != "" {
		plan = append(plan, fmt.Sprintf("reason about %q", task.Description))
	}
	if pack.TaskType == "planning" {
		plan = append(plan, "clarify goals and constraints")
		plan = append(plan, "discuss architecture and tradeoffs")
		plan = append(plan, "prepare an implementation-ready plan")
		return plan, nil
	}
	if pack.TaskType == "analysis" {
		plan = append(plan, "derive a solution")
		plan = append(plan, "validate the proposed approach")
		if pack.ContinuationSummary != "" {
			plan = append(plan, fmt.Sprintf("resume from prior context: %s", pack.ContinuationSummary))
		}
		return plan, nil
	}
	if pack.TaskType == "generation" {
		plan = append(plan, "compose the requested artifact")
		plan = append(plan, "validate the structure")
		if pack.RepositorySummary != "" {
			plan = append(plan, fmt.Sprintf("check the target schema: %s", pack.RepositorySummary))
		}
		return plan, nil
	}
	if task.ActiveFile != "" {
		plan = append(plan, fmt.Sprintf("edit active file %s", task.ActiveFile))
		plan = append(plan, "decompose the task into the smallest implementation steps")
		plan = append(plan, "define the expected behavior")
		plan = append(plan, "draft or review a focused test case")
	}
	if len(pack.PriorPaths) > 0 {
		plan = append(plan, "reuse prior repository paths")
		plan = append(plan, fmt.Sprintf("prior repository paths: %s", strings.Join(pack.PriorPaths, ",")))
	}
	if len(pack.Symbols) > 0 {
		plan = append(plan, fmt.Sprintf("focus on symbols %s", strings.Join(pack.Symbols, ",")))
	}
	if len(pack.Imports) > 0 {
		plan = append(plan, fmt.Sprintf("inspect imports %s", strings.Join(pack.Imports, ",")))
	}
	if pack.CodeStructureSummary != "" {
		plan = append(plan, fmt.Sprintf("use code structure summary: %s", pack.CodeStructureSummary))
	}
	if len(pack.DependencyFiles) > 0 {
		plan = append(plan, fmt.Sprintf("inspect dependency file(s): %s", strings.Join(pack.DependencyFiles, ",")))
	}
	if len(pack.RelatedFiles) > 0 {
		plan = append(plan, fmt.Sprintf("edit %d related file(s)", len(pack.RelatedFiles)))
		if len(pack.RelatedFiles) > 1 {
			plan = append(plan, "inspect cross-file dependencies")
			plan = append(plan, "decompose the change across the relevant files")
		}
	}
	if len(pack.TestFiles) > 0 {
		plan = append(plan, fmt.Sprintf("review related tests: %s", strings.Join(pack.TestFiles, ",")))
	}
	if pack.ModuleName != "" {
		plan = append(plan, fmt.Sprintf("work inside module %s", pack.ModuleName))
	}
	plan = append(plan, "validate the change against the repository context")
	plan = append(plan, "run targeted tests and inspect failures")
	return plan, nil
}

// DefaultRepoIndexer returns a lightweight list of repository files.
type DefaultRepoIndexer struct{}

func (d *DefaultRepoIndexer) Index(task Task, state RunState) ([]string, error) {
	_ = state
	workingDir := task.WorkingDir
	if workingDir == "" {
		workingDir = "."
	}
	entries, err := os.ReadDir(workingDir)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, entry.Name())
		}
	}
	return files, nil
}

// DefaultPatchEngine applies simple text replacement patches.
type DefaultPatchEngine struct{}

func (d *DefaultPatchEngine) Apply(task Task, patch *Patch, state RunState) error {
	_ = state
	if patch == nil {
		return fmt.Errorf("patch is required")
	}
	if patch.PatchContent != "" {
		if patch.FilePath == "" {
			return fmt.Errorf("patch file path is required")
		}
		patchFile, err := os.CreateTemp("", "codingworkflow-patch-*.patch")
		if err != nil {
			return fmt.Errorf("create patch temp file: %w", err)
		}
		if _, err := patchFile.WriteString(patch.PatchContent); err != nil {
			_ = patchFile.Close()
			_ = os.Remove(patchFile.Name())
			return fmt.Errorf("write patch temp file: %w", err)
		}
		if err := patchFile.Close(); err != nil {
			_ = os.Remove(patchFile.Name())
			return fmt.Errorf("close patch temp file: %w", err)
		}
		defer os.Remove(patchFile.Name())

		workingDir := task.WorkingDir
		if workingDir == "" {
			workingDir = "."
		}
		cmd := exec.Command("git", "apply", patchFile.Name())
		cmd.Dir = workingDir
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("apply git patch: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	}

	if patch.FilePath == "" {
		return fmt.Errorf("patch is required")
	}
	fullPath := filepath.Join(task.WorkingDir, patch.FilePath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Errorf("read target file: %w", err)
	}
	if patch.OldContent != "" && !strings.Contains(string(content), patch.OldContent) {
		return fmt.Errorf("target content %q not found", patch.OldContent)
	}
	updated := string(content)
	if patch.OldContent != "" {
		updated = strings.Replace(updated, patch.OldContent, patch.NewContent, 1)
	} else {
		updated = patch.NewContent
	}
	if err := os.WriteFile(fullPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write target file: %w", err)
	}
	return nil
}

// DefaultTestRunner runs package tests from the task working directory and reports pass/fail.
type DefaultTestRunner struct{}

func (d *DefaultTestRunner) Run(task Task, state RunState) (TestResult, error) {
	_ = state
	workingDir := task.WorkingDir
	if workingDir == "" {
		workingDir = "."
	}
	args := []string{"test"}
	if fileExists(filepath.Join(workingDir, "go.mod")) {
		selectedTests := d.selectRelevantTestFiles(workingDir, task.ActiveFile)
		if len(selectedTests) > 0 {
			args = append(args, fmt.Sprintf("./..."), "-run", "Test")
		} else {
			args = append(args, "./...")
		}
	} else {
		args = append(args, "./...")
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = workingDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return TestResult{Passed: false, Output: string(output)}, nil
	}
	return TestResult{Passed: true, Output: string(output)}, nil
}

func (d *DefaultTestRunner) selectRelevantTestFiles(workingDir, activeFile string) []string {
	if activeFile == "" {
		return nil
	}
	var files []string
	base := strings.TrimSuffix(filepath.Base(activeFile), filepath.Ext(activeFile))
	for _, candidate := range []string{base + "_test.go", "main_test.go"} {
		if candidate != "" && fileExists(filepath.Join(workingDir, candidate)) && !contains(files, candidate) {
			files = append(files, candidate)
		}
	}
	entries, err := os.ReadDir(workingDir)
	if err != nil {
		return files
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, "_test.go") && !contains(files, name) {
			if strings.Contains(name, base) || strings.HasPrefix(name, "main") || strings.HasPrefix(name, "helper") {
				files = append(files, name)
			}
		}
	}
	if len(files) == 0 {
		if fileExists(filepath.Join(workingDir, "main_test.go")) {
			files = []string{"main_test.go"}
		}
	}
	return files
}

// DefaultSafetyPolicy allows read-only tasks but blocks destructive actions unless explicitly enabled.
type DefaultSafetyPolicy struct{}

func (d *DefaultSafetyPolicy) Allow(task Task, state RunState) error {
	if task.Patch == nil && len(task.Patches) == 0 {
		return nil
	}
	if task.AllowDestructive {
		return nil
	}
	if state.Metadata != nil {
		if state.Metadata["approval"] == "granted" || state.Metadata["approved"] == "true" {
			return nil
		}
	}
	if state.Args != nil {
		if state.Args["approval"] == "granted" || state.Args["approved"] == "true" {
			return nil
		}
	}

	// Build approval requirement
	filePath := ""
	patchSummary := ""
	if task.Patch != nil {
		filePath = task.Patch.FilePath
		patchSummary = fmt.Sprintf("Apply patch to %s", task.Patch.FilePath)
	} else if len(task.Patches) > 0 {
		filePath = task.Patches[0].FilePath
		patchSummary = fmt.Sprintf("Apply %d patches", len(task.Patches))
	}

	workingDirCandidates := []string{}
	if task.WorkingDir != "" {
		workingDirCandidates = discoverWorkingDirCandidates(task.WorkingDir)
	} else {
		workingDirCandidates = discoverWorkingDirCandidates(".")
	}
	promptRequired := task.WorkingDir == "" && len(workingDirCandidates) > 1
	return &ApprovalRequiredError{
		ActionType:               "coding_workflow_patch",
		Reasoning:                "This coding task will modify files in your repository. Please review and approve the changes.",
		FilePath:                 filePath,
		PatchSummary:             patchSummary,
		WorkingDir:               task.WorkingDir,
		WorkingDirCandidates:     workingDirCandidates,
		WorkingDirPromptRequired: promptRequired,
	}
}

func discoverWorkingDirCandidates(startDir string) []string {
	if strings.TrimSpace(startDir) == "" {
		startDir = "."
	}
	seen := map[string]bool{}
	candidates := []string{}
	addCandidate := func(dir string) {
		trimmed := strings.TrimSpace(dir)
		if trimmed == "" {
			return
		}
		cleaned := filepath.Clean(trimmed)
		if cleaned == "." {
			if cwd, err := os.Getwd(); err == nil {
				cleaned = filepath.Clean(cwd)
			}
		}
		if cleaned == "" || cleaned == string(filepath.Separator) {
			return
		}
		if seen[cleaned] {
			return
		}
		if info, err := os.Stat(filepath.Join(cleaned, ".git")); err == nil && info.IsDir() {
			seen[cleaned] = true
			candidates = append(candidates, cleaned)
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		addCandidate(cwd)
	}
	addCandidate(startDir)
	for _, seed := range []string{startDir, "."} {
		current := seed
		if strings.TrimSpace(current) == "" {
			current = "."
		}
		if current == "." {
			if cwd, err := os.Getwd(); err == nil {
				current = cwd
			}
		}
		for {
			addCandidate(current)
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			if entries, err := os.ReadDir(parent); err == nil {
				for _, entry := range entries {
					if !entry.IsDir() || entry.Name() == ".git" {
						continue
					}
					addCandidate(filepath.Join(parent, entry.Name()))
				}
			}
			current = parent
		}
	}
	return candidates
}

// InMemorySessionMemory stores values in memory for the current process.
type InMemorySessionMemory struct {
	data map[string]map[string]string
}

func NewInMemorySessionMemory() *InMemorySessionMemory {
	return &InMemorySessionMemory{data: map[string]map[string]string{}}
}

func (m *InMemorySessionMemory) Save(key string, values map[string]string) error {
	if m.data == nil {
		m.data = map[string]map[string]string{}
	}
	m.data[key] = values
	return nil
}

func (m *InMemorySessionMemory) Load(key string) (map[string]string, error) {
	if m.data == nil {
		return nil, fmt.Errorf("memory not found")
	}
	values, exists := m.data[key]
	if !exists {
		return nil, fmt.Errorf("memory not found")
	}
	return values, nil
}

// FileSessionMemory persists workflow state to a local JSON file for local-host workflows.
type FileSessionMemory struct {
	path string
}

func NewFileSessionMemory(path string) *FileSessionMemory {
	return &FileSessionMemory{path: path}
}

func (m *FileSessionMemory) Save(key string, values map[string]string) error {
	if m.path == "" {
		return fmt.Errorf("memory path is required")
	}
	payload := map[string]map[string]string{}
	if existing, err := m.loadAll(); err == nil {
		payload = existing
	} else if !os.IsNotExist(err) {
		return err
	}
	payload[key] = values
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil && !os.IsExist(err) {
		return err
	}
	return os.WriteFile(m.path, data, 0o644)
}

func (m *FileSessionMemory) Load(key string) (map[string]string, error) {
	payload, err := m.loadAll()
	if err != nil {
		return nil, err
	}
	values, exists := payload[key]
	if !exists {
		return nil, fmt.Errorf("memory not found")
	}
	return values, nil
}

func (m *FileSessionMemory) BuildVisualizationSummary() (string, error) {
	payload, err := m.loadAll()
	if err != nil {
		return "", err
	}
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := []string{"local workflow history"}
	for _, key := range keys {
		values := payload[key]
		if len(values) == 0 {
			continue
		}
		entries := make([]string, 0, len(values))
		for k, v := range values {
			entries = append(entries, fmt.Sprintf("%s=%s", k, v))
		}
		sort.Strings(entries)
		parts = append(parts, fmt.Sprintf("%s:%s", key, strings.Join(entries, ";")))
	}
	if len(parts) == 1 {
		return "local workflow history: no entries yet", nil
	}
	return strings.Join(parts, " | "), nil
}

func (m *FileSessionMemory) loadAll() (map[string]map[string]string, error) {
	if m.path == "" {
		return nil, fmt.Errorf("memory path is required")
	}
	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]map[string]string{}, nil
		}
		return nil, err
	}
	payload := map[string]map[string]string{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// DefaultKnowledgeBaseRetriever reads repository docs and knowledge files to create grounded guidance.
type DefaultKnowledgeBaseRetriever struct{}

func (d *DefaultKnowledgeBaseRetriever) Retrieve(task Task, state RunState, pack ContextPack) (ExpertiseCapsule, error) {
	_ = state
	_ = pack
	workingDir := task.WorkingDir
	if workingDir == "" {
		workingDir = "."
	}
	queryTerms := []string{}
	for _, part := range strings.Fields(strings.ToLower(strings.TrimSpace(task.Description))) {
		if part == "" {
			continue
		}
		queryTerms = append(queryTerms, strings.Trim(part, "\"'.,:;()[]{}"))
	}
	if task.ActiveFile != "" {
		queryTerms = append(queryTerms, strings.TrimSuffix(strings.ToLower(filepath.Base(task.ActiveFile)), filepath.Ext(task.ActiveFile)))
	}
	seen := map[string]bool{}
	var terms []string
	for _, term := range queryTerms {
		clean := normalizeKnowledgeBaseSearchTerm(term)
		if clean != "" && !seen[clean] {
			seen[clean] = true
			terms = append(terms, term)
		}
	}
	matches := []string{}
	sources := []string{}
	matchedLabels := []string{}
	_ = filepath.WalkDir(workingDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".txt" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if len(terms) == 0 {
			return nil
		}
		matchedTerms := documentMatchesKnowledgeBaseTerms(string(content), terms)
		if len(matchedTerms) == 0 {
			return nil
		}
		relPath, err := filepath.Rel(workingDir, path)
		if err != nil {
			relPath = path
		}
		relPath = filepath.ToSlash(relPath)
		matches = append(matches, strings.TrimSpace(string(content)))
		sources = append(sources, relPath)
		matchedLabels = append(matchedLabels, matchedTerms...)
		return nil
	})
	if len(matches) == 0 {
		return ExpertiseCapsule{Summary: "No knowledge-base guidance was found for this task.", Category: "coding", Sources: []string{"local:knowledge_base"}}, nil
	}
	labelSummary := strings.Join(uniqueStrings(matchedLabels), ", ")
	if labelSummary == "" {
		labelSummary = "task terms"
	}
	summary := fmt.Sprintf("Knowledge-base guidance available from %d source(s) matching %s: %s", len(matches), labelSummary, strings.Join(sources, ", "))
	return ExpertiseCapsule{Summary: summary, Category: "coding", Sources: sources}, nil
}

func normalizeKnowledgeBaseSearchTerm(term string) string {
	term = strings.ToLower(strings.TrimSpace(term))
	term = strings.Trim(term, "\"'.,:;()[]{}")
	var builder strings.Builder
	for _, r := range term {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			builder.WriteRune(r)
		}
	}
	term = builder.String()
	if term == "" {
		return ""
	}
	for _, suffix := range []string{"ing", "ed", "er", "ers", "es", "s", "tion", "tions", "ment", "ments", "y"} {
		if strings.HasSuffix(term, suffix) && len(term) > len(suffix)+1 {
			term = strings.TrimSuffix(term, suffix)
			break
		}
	}
	if strings.HasSuffix(term, "e") && len(term) > 2 {
		term = strings.TrimSuffix(term, "e")
	}
	return term
}

func documentMatchesKnowledgeBaseTerms(content string, terms []string) []string {
	if len(terms) == 0 {
		return nil
	}
	var matched []string
	for _, term := range terms {
		if term == "" {
			continue
		}
		if contentMatchesKnowledgeBaseTerm(content, term) {
			matched = append(matched, term)
		}
	}
	return uniqueStrings(matched)
}

func contentMatchesKnowledgeBaseTerm(content string, term string) bool {
	contentTerms := strings.FieldsFunc(normalizeKnowledgeBaseSearchText(content), func(r rune) bool { return unicode.IsSpace(r) })
	for _, contentTerm := range contentTerms {
		if contentTerm == "" {
			continue
		}
		if contentTerm == term || strings.HasPrefix(contentTerm, term) || strings.HasPrefix(term, contentTerm) || normalizeKnowledgeBaseSearchTerm(contentTerm) == term || normalizeKnowledgeBaseSearchTerm(contentTerm) == normalizeKnowledgeBaseSearchTerm(term) {
			return true
		}
	}
	return false
}

func normalizeKnowledgeBaseSearchText(text string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			builder.WriteRune(r)
		} else {
			builder.WriteRune(' ')
		}
	}
	return strings.TrimSpace(builder.String())
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

// DefaultProviderRouter resolves a provider using environment-backed BYO API key settings.
type DefaultProviderRouter struct{}

func (d *DefaultProviderRouter) Resolve(task Task, state RunState, pack ContextPack) (ProviderSelection, error) {
	_ = task
	_ = state
	_ = pack
	if provider, ok := os.LookupEnv("OPENAI_API_KEY"); ok && provider != "" {
		return ProviderSelection{Provider: "openai", Source: "env", Model: "gpt-4o-mini"}, nil
	}
	if provider, ok := os.LookupEnv("ANTHROPIC_API_KEY"); ok && provider != "" {
		return ProviderSelection{Provider: "anthropic", Source: "env", Model: "claude-3-5-sonnet-latest"}, nil
	}
	if provider, ok := os.LookupEnv("GOOGLE_API_KEY"); ok && provider != "" {
		return ProviderSelection{Provider: "google", Source: "env", Model: "gemini-2.0-flash"}, nil
	}
	return ProviderSelection{Provider: "openai", Source: "default", Model: "gpt-4o-mini"}, nil
}

// RunEvaluationSuite executes a lightweight evaluation harness for the workflow.
func RunEvaluationSuite(orchestrator *Orchestrator, cases []EvaluationCase) []EvaluationResult {
	results := make([]EvaluationResult, 0, len(cases))
	for _, c := range cases {
		if orchestrator == nil {
			results = append(results, EvaluationResult{Name: c.Name, Passed: false, Message: "orchestrator is required"})
			continue
		}
		result, err := orchestrator.Execute(c.Task)
		if err != nil {
			results = append(results, EvaluationResult{Name: c.Name, Passed: false, Message: err.Error()})
			continue
		}
		message := strings.ToLower(strings.TrimSpace(result.Message))
		passed := true
		if c.ExpectContains != "" && !strings.Contains(message, strings.ToLower(strings.TrimSpace(c.ExpectContains))) {
			passed = false
		}
		results = append(results, EvaluationResult{Name: c.Name, Passed: passed, Message: result.Message})
	}
	return results
}

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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func globTestFiles(workingDir string) []string {
	matches, err := filepath.Glob(filepath.Join(workingDir, "*_test.go"))
	if err != nil {
		return nil
	}
	return matches
}

func inferSymbolName(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}
	for _, token := range []string{"update ", "fix ", "refactor ", "add ", "rename ", "modify ", "inspect "} {
		trimmed = strings.TrimPrefix(trimmed, token)
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return ""
	}
	candidate := strings.TrimSpace(fields[0])
	candidate = strings.Trim(candidate, "\"'.,:;()[]{}")
	if candidate == "" {
		return ""
	}
	if strings.Contains(candidate, " ") {
		return ""
	}
	return candidate
}

func extensionToLanguage(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	default:
		return ""
	}
}

func collectASTLeafNodes(content, workingDir, activeFile string) ([]string, string) {
	if strings.TrimSpace(content) == "" {
		return nil, ""
	}

	// V1: Use enhanced tree-sitter parser with multi-language support
	ext := filepath.Ext(activeFile)
	lang := extensionToLanguage(ext)
	if lang == "" {
		lang = "go"
	}

	tsParser := NewTreeSitterParser(lang)
	leaves, err := tsParser.ParseSymbols(content)
	if err != nil || len(leaves) == 0 {
		// Fallback to legacy parsing
		leaves = collectSymbolsLegacy(content)
	}

	if len(leaves) == 0 {
		return nil, ""
	}

	// Keep the original structural path logic
	parser := ts.NewParser()
	parser.SetLanguage(golang.GetLanguage())
	tree, treeErr := parser.ParseCtx(context.Background(), nil, []byte(content))
	if treeErr != nil || tree == nil {
		return leaves, ""
	}
	root := tree.RootNode()
	if root == nil {
		return leaves, ""
	}

	var walk func(node *ts.Node)
	walk = func(node *ts.Node) {
		if node == nil {
			return
		}
		if node.Type() == "function_declaration" || node.Type() == "method_declaration" {
			if name := extractASTLeafName(node, []byte(content)); name != "" && !contains(leaves, name) {
				leaves = append(leaves, name)
			}
		}
		for i := 0; i < int(node.NamedChildCount()); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(root)
	if len(leaves) == 0 {
		return nil, ""
	}
	structuralPath := ""
	if activeFile != "" {
		relPath := filepath.ToSlash(activeFile)
		if relDir := filepath.Dir(relPath); relDir != "." && relDir != "/" {
			structuralPath = strings.Join([]string{filepath.Base(workingDir), relDir, leaves[0]}, "/")
		} else {
			structuralPath = strings.Join([]string{filepath.Base(workingDir), leaves[0]}, "/")
		}
	}
	if structuralPath == "" {
		structuralPath = strings.Join([]string{filepath.Base(workingDir), leaves[0]}, "/")
	}
	return leaves, structuralPath
}

func extractASTLeafName(node *ts.Node, source []byte) string {
	if node == nil {
		return ""
	}
	for _, fieldName := range []string{"name"} {
		if child := node.ChildByFieldName(fieldName); child != nil {
			name := strings.TrimSpace(child.Content(source))
			if name != "" {
				return name
			}
		}
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		if child.Type() == "identifier" || child.Type() == "field_identifier" {
			name := strings.TrimSpace(child.Content(source))
			if name != "" {
				return name
			}
		}
	}
	return ""
}

func collectSymbolsLegacy(content string) []string {
	return collectSymbols(content)
}

func collectSymbols(content string) []string {
	var symbols []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "func ") || strings.HasPrefix(trimmed, "type ") || strings.HasPrefix(trimmed, "var ") || strings.HasPrefix(trimmed, "const ") {
			parts := strings.Fields(trimmed)
			if len(parts) > 1 {
				name := strings.TrimSuffix(parts[1], "{")
				name = strings.TrimSuffix(name, "(")
				name = strings.TrimSuffix(name, " struct")
				name = strings.TrimSuffix(name, " interface")
				if strings.HasPrefix(trimmed, "func ") {
					if idx := strings.Index(name, "("); idx >= 0 {
						name = name[:idx]
					}
				}
				if name != "" && !contains(symbols, name) {
					symbols = append(symbols, name)
				}
			}
		}
	}
	return symbols
}

func collectImports(content string) []string {
	var imports []string
	inBlock := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "import ") {
			if strings.Contains(trimmed, "(") {
				inBlock = true
				continue
			}
			if strings.HasPrefix(trimmed, "import \"") && strings.HasSuffix(trimmed, "\"") {
				imports = append(imports, strings.Trim(trimmed[len("import "):], "\""))
			}
			continue
		}
		if inBlock {
			if trimmed == ")" {
				inBlock = false
				continue
			}
			if strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"") {
				imports = append(imports, strings.Trim(trimmed, "\""))
			}
		}
	}
	return imports
}

func summarizeCodeStructure(imports, symbols []string) string {
	parts := []string{"code structure"}
	if len(imports) > 0 {
		parts = append(parts, fmt.Sprintf("imports=%s", strings.Join(imports, ",")))
	}
	if len(symbols) > 0 {
		parts = append(parts, fmt.Sprintf("symbols=%s", strings.Join(symbols, ",")))
	}
	if len(parts) == 1 {
		return "code structure: no obvious imports or symbols"
	}
	return strings.Join(parts, "; ")
}

func extractSymbolSnippet(content, symbolName string) string {
	if content == "" || symbolName == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	startIdx := -1
	for idx, line := range lines {
		if strings.Contains(line, symbolName) {
			startIdx = idx
			break
		}
	}
	if startIdx == -1 {
		return content
	}

	startLine := startIdx
	if startIdx > 0 {
		startLine = startIdx - 1
	}
	endLine := len(lines)
	braceDepth := 0
	foundBlock := false
	for idx := startIdx; idx < len(lines); idx++ {
		line := lines[idx]
		for _, ch := range line {
			if ch == '{' {
				braceDepth++
			} else if ch == '}' {
				braceDepth--
				if braceDepth == 0 && idx > startIdx {
					endLine = idx + 1
					foundBlock = true
					break
				}
			}
		}
		if foundBlock {
			break
		}
	}
	if !foundBlock {
		endLine = startIdx + 3
		if endLine > len(lines) {
			endLine = len(lines)
		}
	}
	if endLine <= startLine {
		endLine = startLine + 1
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	if startLine < 0 {
		startLine = 0
	}
	snippet := strings.Join(lines[startLine:endLine], "\n")
	if strings.TrimSpace(snippet) == "" {
		return content
	}
	return snippet
}

func collectRelatedFiles(workingDir, activeFile string) []string {
	var files []string
	err := filepath.WalkDir(workingDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(workingDir, path)
		if err != nil {
			return err
		}
		if relPath == "." || relPath == activeFile || filepath.ToSlash(relPath) == filepath.ToSlash(activeFile) {
			return nil
		}
		ext := filepath.Ext(relPath)
		if ext == ".go" || ext == ".md" || ext == ".txt" {
			files = append(files, filepath.ToSlash(relPath))
		}
		return nil
	})
	if err != nil {
		return nil
	}
	return files
}

func collectDependencyFiles(workingDir, activeFile, content string, symbols []string) []string {
	if activeFile == "" {
		return nil
	}
	var deps []string
	normalizedContent := strings.ToLower(content)
	err := filepath.WalkDir(workingDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(workingDir, path)
		if err != nil {
			return err
		}
		if relPath == "." || relPath == activeFile || filepath.ToSlash(relPath) == filepath.ToSlash(activeFile) {
			return nil
		}
		if filepath.Ext(relPath) != ".go" {
			return nil
		}
		candidateContent, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		candidateText := string(candidateContent)
		candidateLower := strings.ToLower(candidateText)
		for _, symbol := range symbols {
			if strings.Contains(candidateLower, strings.ToLower(symbol)) {
				deps = append(deps, filepath.ToSlash(relPath))
				return nil
			}
		}
		for _, token := range strings.Fields(normalizedContent) {
			if strings.HasPrefix(token, "helper") || strings.HasPrefix(token, "config") || strings.HasPrefix(token, "service") {
				if strings.Contains(candidateLower, token) {
					deps = append(deps, filepath.ToSlash(relPath))
					return nil
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil
	}
	return deps
}

func collectRepositoryFiles(workingDir string) []string {
	var files []string
	err := filepath.WalkDir(workingDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(workingDir, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(relPath))
		return nil
	})
	if err != nil {
		return nil
	}
	return files
}

func inferModuleName(workingDir string) string {
	goModPath := filepath.Join(workingDir, "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}
	return ""
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

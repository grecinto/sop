package codingworkflow

import (
	"fmt"
	"os"
	"os/exec"
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
	DependencyFiles       []string
	RelatedFiles          []string
	TestFiles             []string
	WorkingDir            string
	ModuleName            string
	RepositoryFiles       []string
	ResumedFromState      bool
	RepositorySummary     string
	RepositoryDiffPreview string
	RecentCodingMemory    string
	TaskType              string
	ContinuationSummary   string
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
}

// ContextManager builds a concise context pack for a task.
type ContextManager interface {
	BuildContext(Task, RunState) (ContextPack, error)
}

// Planner produces a structured execution plan for a task based on the assembled context.
type Planner interface {
	Plan(Task, RunState, ContextPack) ([]string, error)
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

// SafetyPolicy controls whether a task is allowed to mutate files.
type SafetyPolicy interface {
	Allow(Task, RunState) error
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
	ContextManager       ContextManager
	RepoIndexer          RepoIndexer
	PatchEngine          PatchEngine
	ChangedFileInspector ChangedFileInspector
	DiffPreviewer        DiffPreviewer
	RepoSearcher         RepoSearcher
	RepoInspector        RepoInspector
	Memory               SessionMemory
	Policy               SafetyPolicy
	TestRunner           TestRunner
	Planner              Planner
	MemorySink           func(CodingMemory)
	ContinuitySink       func(ContinuitySignal)
	MaxIterations        int
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
		o.Memory = NewInMemorySessionMemory()
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
		message := "patches applied; next step: inspect the diff"
		if i := len(applied); i > 0 {
			if lastTestResult.Passed {
				message = fmt.Sprintf("applied %d patch(es); tests passed; next step: inspect the diff", i)
			} else {
				message = fmt.Sprintf("applied %d patch(es); tests failed; next step: inspect the test output", i)
			}
		}
		codingMemory := CodingMemory{
			Description:    task.Description,
			WorkingDir:     task.WorkingDir,
			ActiveFile:     task.ActiveFile,
			FilesTouched:   filesTouched,
			Outcome:        message,
			TestStatus:     "passed",
			RepositoryHint: pack.RepositorySummary,
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
		memoryValues := map[string]string{
			"coding_memory": fmt.Sprintf("description=%s; outcome=%s; files=%s; status=%s", codingMemory.Description, codingMemory.Outcome, strings.Join(codingMemory.FilesTouched, ","), codingMemory.TestStatus),
		}
		if err := o.Memory.Save("coding-memory", memoryValues); err != nil {
			return ExecutionResult{}, fmt.Errorf("persist coding memory: %w", err)
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
	message := "context assembled"
	if task.Description != "" {
		switch pack.TaskType {
		case "planning":
			message = fmt.Sprintf("design discussion prepared for %q; next step: refine the plan or request implementation", task.Description)
		case "analysis":
			message = fmt.Sprintf("context assembled for %q; next step: inspect or analyze the relevant materials", task.Description)
		case "generation":
			message = fmt.Sprintf("context assembled for %q; next step: draft the requested artifact", task.Description)
		default:
			message = fmt.Sprintf("context assembled for %q; next step: inspect or edit the relevant files", task.Description)
		}
	}
	if pack.RecentCodingMemory != "" {
		message = fmt.Sprintf("%s; recent coding memory: %s", message, pack.RecentCodingMemory)
	}
	plan := []string{}
	if generatedPlan, err := o.Planner.Plan(task, task.State, pack); err == nil {
		plan = generatedPlan
	}
	return ExecutionResult{ContextPack: pack, Message: message, TaskType: pack.TaskType, Plan: plan, ResumedFromState: pack.ResumedFromState}, nil
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
	return ContextPack{ActiveFile: resolvedActiveFile, ActiveFileSnippet: content, SymbolName: symbolName, SymbolSnippet: symbolSnippet, Symbols: symbols, DependencyFiles: dependencyFiles, RelatedFiles: related, TestFiles: testFiles, WorkingDir: workingDir, ModuleName: moduleName, RepositoryFiles: repoFiles, RepositorySummary: repositorySummary, ResumedFromState: resumedFromState, TaskType: taskType, ContinuationSummary: continuationSummary}, nil
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
	plan := []string{"inspect relevant files"}
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
		plan = append(plan, "define the expected behavior")
		plan = append(plan, "draft or review a focused test case")
	}
	if len(pack.Symbols) > 0 {
		plan = append(plan, fmt.Sprintf("focus on symbols %s", strings.Join(pack.Symbols, ",")))
	}
	if len(pack.DependencyFiles) > 0 {
		plan = append(plan, fmt.Sprintf("inspect dependency file(s): %s", strings.Join(pack.DependencyFiles, ",")))
	}
	if len(pack.RelatedFiles) > 0 {
		plan = append(plan, fmt.Sprintf("edit %d related file(s)", len(pack.RelatedFiles)))
		if len(pack.RelatedFiles) > 1 {
			plan = append(plan, "inspect cross-file dependencies")
		}
	}
	if len(pack.TestFiles) > 0 {
		plan = append(plan, fmt.Sprintf("review related tests: %s", strings.Join(pack.TestFiles, ",")))
	}
	if pack.ModuleName != "" {
		plan = append(plan, fmt.Sprintf("work inside module %s", pack.ModuleName))
	}
	plan = append(plan, "run tests")
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
	return fmt.Errorf("patch execution requires explicit approval")
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
	entries, err := os.ReadDir(workingDir)
	if err != nil {
		return nil
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == activeFile {
			continue
		}
		if filepath.Ext(entry.Name()) == ".go" || filepath.Ext(entry.Name()) == ".md" || filepath.Ext(entry.Name()) == ".txt" {
			files = append(files, entry.Name())
		}
	}
	return files
}

func collectDependencyFiles(workingDir, activeFile, content string, symbols []string) []string {
	entries, err := os.ReadDir(workingDir)
	if err != nil {
		return nil
	}
	if activeFile == "" {
		return nil
	}
	var deps []string
	normalizedContent := strings.ToLower(content)
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == activeFile {
			continue
		}
		if filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		candidateContent, err := os.ReadFile(filepath.Join(workingDir, entry.Name()))
		if err != nil {
			continue
		}
		candidateText := string(candidateContent)
		candidateLower := strings.ToLower(candidateText)
		for _, symbol := range symbols {
			if strings.Contains(candidateLower, strings.ToLower(symbol)) {
				deps = append(deps, entry.Name())
				break
			}
		}
		if len(deps) > 0 && contains(deps, entry.Name()) {
			continue
		}
		for _, token := range strings.Fields(normalizedContent) {
			if strings.HasPrefix(token, "helper") || strings.HasPrefix(token, "config") || strings.HasPrefix(token, "service") {
				if strings.Contains(candidateLower, token) {
					deps = append(deps, entry.Name())
					break
				}
			}
		}
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

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

	ts "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

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

// inferTaskInputs extracts workingDir and activeFile from path-like tokens in the query
func inferTaskInputs(query string) (workingDir, activeFile string) {
	tokens := strings.Fields(query)
	for _, token := range tokens {
		// Look for path-like patterns (contains / or .)
		if strings.Contains(token, "/") || (strings.Contains(token, ".") && !strings.HasPrefix(token, ".")) {
			// Clean up common suffixes/prefixes
			cleaned := strings.TrimRight(token, ".,;:!?")

			// If it looks like a file path
			if strings.Contains(cleaned, "/") {
				activeFile = cleaned
				// Extract directory from the file path
				if idx := strings.LastIndex(cleaned, "/"); idx > 0 {
					workingDir = cleaned[:idx]
				}
				break
			}
		}
	}

	// If we have a file but no directory, try to infer it
	if activeFile != "" && workingDir == "" {
		workingDir = "."
	}

	return workingDir, activeFile
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

// ProjectType represents the detected project language/framework
type ProjectType string

const (
	ProjectTypeGo      ProjectType = "go"
	ProjectTypePython  ProjectType = "python"
	ProjectTypeJava    ProjectType = "java"
	ProjectTypeRust    ProjectType = "rust"
	ProjectTypeCSharp  ProjectType = "csharp"
	ProjectTypeNode    ProjectType = "node"
	ProjectTypeUnknown ProjectType = "unknown"
)

// DefaultTestRunner runs package tests from the task working directory and reports pass/fail.
// Supports multiple languages: Go, Python, Java, Rust, C#, Node.js
type DefaultTestRunner struct{}

func (d *DefaultTestRunner) Run(task Task, state RunState) (TestResult, error) {
	_ = state
	workingDir := task.WorkingDir
	if workingDir == "" {
		workingDir = "."
	}

	// Detect project type
	projectType := d.detectProjectType(workingDir)

	// Dispatch to language-specific test runner
	switch projectType {
	case ProjectTypeGo:
		return d.runGoTests(workingDir, task.ActiveFile)
	case ProjectTypePython:
		return d.runPythonTests(workingDir, task.ActiveFile)
	case ProjectTypeJava:
		return d.runJavaTests(workingDir)
	case ProjectTypeRust:
		return d.runRustTests(workingDir)
	case ProjectTypeCSharp:
		return d.runCSharpTests(workingDir)
	case ProjectTypeNode:
		return d.runNodeTests(workingDir)
	default:
		return TestResult{
			Passed: false,
			Output: fmt.Sprintf("Unknown project type in %s. Unable to determine test command.", workingDir),
		}, nil
	}
}

// detectProjectType identifies the project language based on marker files
func (d *DefaultTestRunner) detectProjectType(workingDir string) ProjectType {
	// Go: go.mod
	if fileExists(filepath.Join(workingDir, "go.mod")) {
		return ProjectTypeGo
	}

	// Python: requirements.txt, setup.py, pyproject.toml, pytest.ini
	pythonMarkers := []string{"requirements.txt", "setup.py", "pyproject.toml", "pytest.ini", "setup.cfg"}
	for _, marker := range pythonMarkers {
		if fileExists(filepath.Join(workingDir, marker)) {
			return ProjectTypePython
		}
	}

	// Java: pom.xml (Maven) or build.gradle (Gradle)
	if fileExists(filepath.Join(workingDir, "pom.xml")) || fileExists(filepath.Join(workingDir, "build.gradle")) {
		return ProjectTypeJava
	}

	// Rust: Cargo.toml
	if fileExists(filepath.Join(workingDir, "Cargo.toml")) {
		return ProjectTypeRust
	}

	// C#: *.csproj or *.sln
	entries, err := os.ReadDir(workingDir)
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasSuffix(name, ".csproj") || strings.HasSuffix(name, ".sln") {
				return ProjectTypeCSharp
			}
		}
	}

	// Node.js: package.json
	if fileExists(filepath.Join(workingDir, "package.json")) {
		return ProjectTypeNode
	}

	return ProjectTypeUnknown
}

// runGoTests executes Go tests
func (d *DefaultTestRunner) runGoTests(workingDir, activeFile string) (TestResult, error) {
	args := []string{"test"}
	selectedTests := d.selectRelevantGoTestFiles(workingDir, activeFile)
	if len(selectedTests) > 0 {
		args = append(args, "./...", "-run", "Test")
	} else {
		args = append(args, "./...")
	}

	cmd := exec.Command("go", args...)
	cmd.Dir = workingDir
	output, err := cmd.CombinedOutput()

	passed := err == nil
	return TestResult{Passed: passed, Output: string(output)}, nil
}

// runPythonTests executes Python tests (pytest or unittest)
func (d *DefaultTestRunner) runPythonTests(workingDir, activeFile string) (TestResult, error) {
	var cmd *exec.Cmd

	// Try pytest first (more common in modern Python projects)
	if d.commandExists("pytest") {
		args := []string{"-v"}
		if activeFile != "" && strings.HasSuffix(activeFile, "_test.py") {
			args = append(args, activeFile)
		}
		cmd = exec.Command("pytest", args...)
	} else {
		// Fallback to unittest
		cmd = exec.Command("python", "-m", "unittest", "discover", "-v")
	}

	cmd.Dir = workingDir
	output, err := cmd.CombinedOutput()

	// pytest returns 0 for success, non-zero for failures
	passed := err == nil
	return TestResult{Passed: passed, Output: string(output)}, nil
}

// runJavaTests executes Java tests (Maven or Gradle)
func (d *DefaultTestRunner) runJavaTests(workingDir string) (TestResult, error) {
	var cmd *exec.Cmd

	// Check if Maven or Gradle
	if fileExists(filepath.Join(workingDir, "pom.xml")) {
		cmd = exec.Command("mvn", "test")
	} else if fileExists(filepath.Join(workingDir, "build.gradle")) {
		cmd = exec.Command("gradle", "test")
	} else {
		return TestResult{
			Passed: false,
			Output: "No Maven (pom.xml) or Gradle (build.gradle) configuration found",
		}, nil
	}

	cmd.Dir = workingDir
	output, err := cmd.CombinedOutput()

	// Check output for success indicators
	outputStr := string(output)
	passed := err == nil && (strings.Contains(outputStr, "BUILD SUCCESS") || strings.Contains(outputStr, "BUILD SUCCESSFUL"))

	return TestResult{Passed: passed, Output: outputStr}, nil
}

// runRustTests executes Rust tests
func (d *DefaultTestRunner) runRustTests(workingDir string) (TestResult, error) {
	cmd := exec.Command("cargo", "test")
	cmd.Dir = workingDir
	output, err := cmd.CombinedOutput()

	// cargo test returns 0 for success
	passed := err == nil
	return TestResult{Passed: passed, Output: string(output)}, nil
}

// runCSharpTests executes C# tests
func (d *DefaultTestRunner) runCSharpTests(workingDir string) (TestResult, error) {
	cmd := exec.Command("dotnet", "test")
	cmd.Dir = workingDir
	output, err := cmd.CombinedOutput()

	// Check output for success indicators
	outputStr := string(output)
	passed := err == nil && !strings.Contains(outputStr, "Failed!")

	return TestResult{Passed: passed, Output: outputStr}, nil
}

// runNodeTests executes Node.js tests (via npm test or yarn test)
func (d *DefaultTestRunner) runNodeTests(workingDir string) (TestResult, error) {
	var cmd *exec.Cmd

	// Try npm first, then yarn
	if d.commandExists("npm") {
		cmd = exec.Command("npm", "test")
	} else if d.commandExists("yarn") {
		cmd = exec.Command("yarn", "test")
	} else {
		return TestResult{
			Passed: false,
			Output: "Neither npm nor yarn found. Cannot run Node.js tests.",
		}, nil
	}

	cmd.Dir = workingDir
	output, err := cmd.CombinedOutput()

	// npm/yarn test returns 0 for success
	passed := err == nil
	return TestResult{Passed: passed, Output: string(output)}, nil
}

// selectRelevantGoTestFiles selects relevant Go test files based on active file
func (d *DefaultTestRunner) selectRelevantGoTestFiles(workingDir, activeFile string) []string {
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

// commandExists checks if a command is available in PATH
func (d *DefaultTestRunner) commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// globTestFiles returns test files for the detected project type
func globTestFiles(workingDir string) []string {
	var patterns []string

	// Detect project type
	projectType := detectProjectTypeFromDir(workingDir)

	switch projectType {
	case ProjectTypeGo:
		patterns = []string{"*_test.go"}
	case ProjectTypePython:
		patterns = []string{"test_*.py", "*_test.py"}
	case ProjectTypeJava:
		patterns = []string{"*Test.java", "*Tests.java"}
	case ProjectTypeRust:
		patterns = []string{"*.rs"} // Rust tests are in same files with #[test]
	case ProjectTypeNode:
		patterns = []string{"*.test.js", "*.spec.js", "*.test.ts", "*.spec.ts"}
	default:
		// Try all patterns if type unknown
		patterns = []string{"*_test.go", "test_*.py", "*Test.java", "*.test.js", "*.spec.ts"}
	}

	var allMatches []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(workingDir, pattern))
		if err == nil {
			allMatches = append(allMatches, matches...)
		}
	}
	return allMatches
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
	if alias := resolveTreeSitterLanguageAliasFromExtension(ext); alias != "" {
		return alias
	}

	switch ext {
	case ".cs":
		return "csharp"
	default:
		return ""
	}
}

// detectProjectTypeFromDir identifies project type from directory (helper for context gathering)
func detectProjectTypeFromDir(workingDir string) ProjectType {
	// Go: go.mod
	if fileExists(filepath.Join(workingDir, "go.mod")) {
		return ProjectTypeGo
	}

	// Python: requirements.txt, setup.py, pyproject.toml
	pythonMarkers := []string{"requirements.txt", "setup.py", "pyproject.toml", "pytest.ini"}
	for _, marker := range pythonMarkers {
		if fileExists(filepath.Join(workingDir, marker)) {
			return ProjectTypePython
		}
	}

	// Java: pom.xml or build.gradle
	if fileExists(filepath.Join(workingDir, "pom.xml")) || fileExists(filepath.Join(workingDir, "build.gradle")) {
		return ProjectTypeJava
	}

	// Rust: Cargo.toml
	if fileExists(filepath.Join(workingDir, "Cargo.toml")) {
		return ProjectTypeRust
	}

	// C#: *.csproj or *.sln
	entries, err := os.ReadDir(workingDir)
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasSuffix(name, ".csproj") || strings.HasSuffix(name, ".sln") {
				return ProjectTypeCSharp
			}
		}
	}

	// Node.js: package.json
	if fileExists(filepath.Join(workingDir, "package.json")) {
		return ProjectTypeNode
	}

	return ProjectTypeUnknown
}

func collectASTLeafNodes(content, workingDir, activeFile string) ([]string, string) {
	if strings.TrimSpace(content) == "" {
		return nil, ""
	}

	// V1: Use enhanced tree-sitter parser with multi-language support
	ext := filepath.Ext(activeFile)
	lang := extensionToLanguage(ext)

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

// collectSymbols extracts symbols from content (multi-language)
func collectSymbols(content string) []string {
	var symbols []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)

		// Go: func, type, var, const
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
			continue
		}

		// Python: def, class (must come before Java check)
		if strings.HasPrefix(trimmed, "def ") {
			parts := strings.Fields(trimmed)
			if len(parts) > 1 {
				name := parts[1]
				if idx := strings.IndexAny(name, "(:"); idx > 0 {
					name = name[:idx]
				}
				if name != "" && !contains(symbols, name) {
					symbols = append(symbols, name)
				}
			}
			continue
		}
		if strings.HasPrefix(trimmed, "class ") && strings.Contains(trimmed, ":") {
			// Python class (has colon)
			parts := strings.Fields(trimmed)
			if len(parts) > 1 {
				name := parts[1]
				if idx := strings.IndexAny(name, "(:"); idx > 0 {
					name = name[:idx]
				}
				if name != "" && !contains(symbols, name) {
					symbols = append(symbols, name)
				}
			}
			continue
		}

		// Java/C#: public/private/protected class/interface/method
		if strings.Contains(trimmed, "class ") || strings.Contains(trimmed, "interface ") || strings.Contains(trimmed, "enum ") {
			for _, keyword := range []string{"class ", "interface ", "enum "} {
				if idx := strings.Index(trimmed, keyword); idx >= 0 {
					after := trimmed[idx+len(keyword):]
					parts := strings.Fields(after)
					if len(parts) > 0 {
						name := strings.TrimSuffix(parts[0], "{")
						if name != "" && !contains(symbols, name) {
							symbols = append(symbols, name)
						}
					}
					break
				}
			}
			continue
		}

		// Rust: fn, struct, impl, trait
		if strings.HasPrefix(trimmed, "fn ") || strings.HasPrefix(trimmed, "struct ") || strings.HasPrefix(trimmed, "trait ") {
			parts := strings.Fields(trimmed)
			if len(parts) > 1 {
				name := parts[1]
				if idx := strings.IndexAny(name, "(<{"); idx > 0 {
					name = name[:idx]
				}
				if name != "" && !contains(symbols, name) {
					symbols = append(symbols, name)
				}
			}
			continue
		}

		// JavaScript/TypeScript: function, const, class, export
		if strings.HasPrefix(trimmed, "function ") || strings.HasPrefix(trimmed, "const ") ||
			strings.HasPrefix(trimmed, "export function ") || strings.HasPrefix(trimmed, "export const ") ||
			strings.HasPrefix(trimmed, "export class ") {
			for _, prefix := range []string{"export function ", "export const ", "export class ", "function ", "const ", "class "} {
				if strings.HasPrefix(trimmed, prefix) {
					after := strings.TrimPrefix(trimmed, prefix)
					parts := strings.FieldsFunc(after, func(r rune) bool {
						return r == '(' || r == '=' || r == '{' || r == ' '
					})
					if len(parts) > 0 && parts[0] != "" && !contains(symbols, parts[0]) {
						symbols = append(symbols, parts[0])
					}
					break
				}
			}
			continue
		}
	}
	return symbols
}

// collectImports extracts imports from content (multi-language)
func collectImports(content string) []string {
	var imports []string
	inBlock := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Go: import "..." or import ( ... )
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

		// Python: import x or from x import y
		if strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "from ") {
			if strings.HasPrefix(trimmed, "from ") {
				parts := strings.Fields(trimmed)
				if len(parts) >= 2 {
					imports = append(imports, parts[1])
				}
			} else {
				after := strings.TrimPrefix(trimmed, "import ")
				parts := strings.Split(after, ",")
				for _, part := range parts {
					modName := strings.TrimSpace(strings.Split(part, " as ")[0])
					if modName != "" {
						imports = append(imports, modName)
					}
				}
			}
			continue
		}

		// Java/C#: import x.y.z;
		if strings.HasPrefix(trimmed, "import ") && strings.HasSuffix(trimmed, ";") {
			importPath := strings.TrimSuffix(strings.TrimPrefix(trimmed, "import "), ";")
			importPath = strings.TrimSpace(importPath)
			if importPath != "" {
				imports = append(imports, importPath)
			}
			continue
		}

		// Rust: use x::y;
		if strings.HasPrefix(trimmed, "use ") && strings.HasSuffix(trimmed, ";") {
			usePath := strings.TrimSuffix(strings.TrimPrefix(trimmed, "use "), ";")
			usePath = strings.TrimSpace(usePath)
			if usePath != "" {
				imports = append(imports, usePath)
			}
			continue
		}

		// JavaScript/TypeScript: import { x } from 'y' or require('x')
		if strings.HasPrefix(trimmed, "import ") {
			if idx := strings.Index(trimmed, "from "); idx > 0 {
				after := strings.TrimSpace(trimmed[idx+5:])
				after = strings.Trim(after, "';\"")
				if after != "" {
					imports = append(imports, after)
				}
			}
		}
		if strings.Contains(trimmed, "require(") {
			if start := strings.Index(trimmed, "require("); start >= 0 {
				after := trimmed[start+8:]
				if end := strings.IndexAny(after, ")')"); end > 0 {
					modName := strings.Trim(after[:end], "'\"")
					if modName != "" {
						imports = append(imports, modName)
					}
				}
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

// collectRelatedFiles finds related source files (multi-language)
func collectRelatedFiles(workingDir, activeFile string) []string {
	var files []string
	sourceExts := map[string]bool{
		".go": true, ".py": true, ".java": true, ".rs": true,
		".js": true, ".ts": true, ".jsx": true, ".tsx": true,
		".c": true, ".cpp": true, ".h": true, ".cs": true,
		".md": true, ".txt": true, ".json": true, ".yaml": true,
	}
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
		if sourceExts[ext] {
			files = append(files, filepath.ToSlash(relPath))
		}
		return nil
	})
	if err != nil {
		return nil
	}
	return files
}

// collectDependencyFiles finds files containing symbols (multi-language)
func collectDependencyFiles(workingDir, activeFile, content string, symbols []string) []string {
	if activeFile == "" {
		return nil
	}
	var deps []string
	normalizedContent := strings.ToLower(content)
	sourceExts := map[string]bool{
		".go": true, ".py": true, ".java": true, ".rs": true,
		".js": true, ".ts": true, ".jsx": true, ".tsx": true, ".cs": true,
	}
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
		// Check if it's a source file
		if !sourceExts[filepath.Ext(relPath)] {
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

// inferModuleName extracts module/package name for detected project type
func inferModuleName(workingDir string) string {
	// Go: go.mod
	if data, err := os.ReadFile(filepath.Join(workingDir, "go.mod")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "module ") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					return parts[1]
				}
			}
		}
	}

	// Python: setup.py or pyproject.toml
	if data, err := os.ReadFile(filepath.Join(workingDir, "setup.py")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "name=") {
				if start := strings.Index(line, "name="); start >= 0 {
					after := line[start+5:]
					after = strings.Trim(strings.TrimSpace(after), "'\"")
					if idx := strings.IndexAny(after, "',\""); idx > 0 {
						return after[:idx]
					}
				}
			}
		}
	}

	// Java: pom.xml
	if data, err := os.ReadFile(filepath.Join(workingDir, "pom.xml")); err == nil {
		content := string(data)
		if start := strings.Index(content, "<artifactId>"); start >= 0 {
			start += 12
			if end := strings.Index(content[start:], "</artifactId>"); end > 0 {
				return strings.TrimSpace(content[start : start+end])
			}
		}
	}

	// Rust: Cargo.toml
	if data, err := os.ReadFile(filepath.Join(workingDir, "Cargo.toml")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "name = ") {
				after := strings.TrimPrefix(strings.TrimSpace(line), "name = ")
				after = strings.Trim(after, "'\"")
				return after
			}
		}
	}

	// Node.js: package.json
	if data, err := os.ReadFile(filepath.Join(workingDir, "package.json")); err == nil {
		var pkg map[string]interface{}
		if err := json.Unmarshal(data, &pkg); err == nil {
			if name, ok := pkg["name"].(string); ok {
				return name
			}
		}
	}

	return ""
}

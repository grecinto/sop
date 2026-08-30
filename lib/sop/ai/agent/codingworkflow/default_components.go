package codingworkflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/sharedcode/sop/ai"
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
		// Normalize active file path - strip working directory components if duplicated
		// Example: workingDir="/a/b/lib/sop", file="lib/sop/tools/file.go" → "tools/file.go"
		normalizedFile := resolvedActiveFile
		var activePath string

		if filepath.IsAbs(normalizedFile) {
			// File is already absolute - use it as-is, don't join with workingDir
			activePath = normalizedFile
		} else if filepath.IsAbs(workingDir) {
			// File is relative - check if it duplicates working directory components
			workDirSlash := filepath.ToSlash(workingDir)
			fileSlash := filepath.ToSlash(normalizedFile)

			// Extract last N components of working directory and check if file starts with them
			parts := strings.Split(strings.Trim(workDirSlash, "/"), "/")
			for n := len(parts); n >= 1; n-- {
				suffix := strings.Join(parts[len(parts)-n:], "/")
				prefix := suffix + "/"
				if strings.HasPrefix(fileSlash, prefix) {
					// Strip the redundant prefix
					normalizedFile = strings.TrimPrefix(fileSlash, prefix)
					break
				}
			}
			activePath = filepath.Join(workingDir, normalizedFile)
		} else {
			// Both relative - just join them
			activePath = filepath.Join(workingDir, normalizedFile)
		}
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
	if state.Metadata != nil {
		if continuationSummary, ok := state.Metadata["continuation_summary"]; ok && strings.TrimSpace(continuationSummary) != "" {
			return "analysis"
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

// LLMPlanner asks the configured model to produce a structured plan and falls back
// to DefaultPlanner if generation or parsing fails.
type LLMPlanner struct {
	LLMAccessor func(context.Context) (any, error)
	Fallback    Planner
}

func NewLLMPlanner(llmAccessor func(context.Context) (any, error), fallback Planner) *LLMPlanner {
	if fallback == nil {
		fallback = &DefaultPlanner{}
	}
	return &LLMPlanner{LLMAccessor: llmAccessor, Fallback: fallback}
}

func (p *LLMPlanner) Plan(task Task, state RunState, pack ContextPack) (StructuredPlan, error) {
	if p == nil || p.LLMAccessor == nil {
		return p.planWithFallback(task, state, pack)
	}
	llm, err := p.LLMAccessor(context.Background())
	if err != nil {
		return p.planWithFallback(task, state, pack)
	}
	gen, ok := llm.(ai.Generator)
	if !ok || gen == nil {
		return p.planWithFallback(task, state, pack)
	}

	prompt, err := buildStructuredPlannerPrompt(task, state, pack)
	if err != nil {
		return p.planWithFallback(task, state, pack)
	}
	resp, err := gen.Generate(context.Background(), prompt, ai.GenOptions{
		Temperature:    0.1,
		MaxTokens:      2200,
		ThinkingLevel:  "low",
		ResponseSchema: structuredPlannerResponseSchema(),
	})
	if err != nil {
		return p.planWithFallback(task, state, pack)
	}

	plan, ok := parseStructuredPlannerResponse(resp.Text)
	if !ok {
		return p.planWithFallback(task, state, pack)
	}
	return normalizeStructuredPlan(plan, task, state, pack), nil
}

func (p *LLMPlanner) planWithFallback(task Task, state RunState, pack ContextPack) (StructuredPlan, error) {
	if p != nil && p.Fallback != nil {
		return p.Fallback.Plan(task, state, pack)
	}
	return (&DefaultPlanner{}).Plan(task, state, pack)
}

func (d *DefaultPlanner) Plan(task Task, state RunState, pack ContextPack) (StructuredPlan, error) {
	_ = state
	plan := []string{}
	workflowShape := "implementation"
	domain := "coding_workflow"
	if strings.TrimSpace(pack.ExpertiseSummary) != "" {
		plan = append(plan, "gather grounding from the selected Space and expertise guidance")
	}
	plan = append(plan, "inspect relevant files")
	if task.Description != "" {
		plan = append(plan, fmt.Sprintf("reason about %q", task.Description))
	}
	if pack.TaskType == "planning" {
		workflowShape = "design_implementation"
		plan = append(plan, "clarify goals and constraints")
		plan = append(plan, "discuss architecture and tradeoffs")
		plan = append(plan, "prepare an implementation-ready plan")
		return buildStructuredPlanFromBase(task, state, pack, domain, workflowShape, plan), nil
	}
	if pack.TaskType == "analysis" {
		workflowShape = "analysis"
		plan = append(plan, "derive a solution")
		plan = append(plan, "validate the proposed approach")
		if pack.ContinuationSummary != "" {
			plan = append(plan, fmt.Sprintf("resume from prior context: %s", pack.ContinuationSummary))
		}
		return buildStructuredPlanFromBase(task, state, pack, domain, workflowShape, plan), nil
	}
	if pack.TaskType == "generation" {
		workflowShape = "generation"
		plan = append(plan, "compose the requested artifact")
		plan = append(plan, "validate the structure")
		if pack.RepositorySummary != "" {
			plan = append(plan, fmt.Sprintf("check the target schema: %s", pack.RepositorySummary))
		}
		return buildStructuredPlanFromBase(task, state, pack, domain, workflowShape, plan), nil
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
	return buildStructuredPlanFromBase(task, state, pack, domain, workflowShape, plan), nil
}

func buildStructuredPlanFromBase(task Task, state RunState, pack ContextPack, domain, workflowShape string, base []string) StructuredPlan {
	steps := make([]StructuredPlanStep, 0, len(base))
	for idx, item := range base {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		files := make([]string, 0, 4)
		for _, file := range pack.RelatedFiles {
			if file != "" && !contains(files, file) {
				files = append(files, file)
			}
		}
		for _, file := range pack.DependencyFiles {
			if file != "" && !contains(files, file) {
				files = append(files, file)
			}
		}
		if len(files) == 0 && task.ActiveFile != "" {
			files = append(files, task.ActiveFile)
		}
		steps = append(steps, StructuredPlanStep{
			StepID:            fmt.Sprintf("step_%d", idx+1),
			StepDomain:        domain,
			Goal:              item,
			Files:             append([]string(nil), files...),
			ActionInstruction: item,
			Validation:        defaultValidationForStructuredStep(idx, len(base), pack),
			ExitCriteria:      defaultExitCriteriaForStructuredStep(idx, len(base), pack),
			Status:            "pending",
		})
	}
	objective := strings.TrimSpace(task.Description)
	if objective == "" {
		objective = "Complete the requested task"
	}
	return StructuredPlan{
		Objective:      objective,
		WorkflowShape:  workflowShape,
		Domain:         domain,
		SystemContext:  defaultStructuredSystemContext(task, pack),
		Steps:          steps,
		CurrentStep:    0,
		CompletedSteps: []string{},
		CompletionGate: StructuredCompletionGate{Requirements: defaultCompletionGateRequirements(task, state, pack)},
	}
}

func defaultStructuredSystemContext(task Task, pack ContextPack) StructuredSystemContext {
	primary := []string{}
	if task.ActiveFile != "" {
		primary = append(primary, task.ActiveFile)
	}
	for _, file := range pack.TestFiles {
		if file != "" && !contains(primary, file) {
			primary = append(primary, file)
		}
	}
	supporting := []string{}
	for _, file := range pack.DependencyFiles {
		if file != "" && !contains(supporting, file) && !contains(primary, file) {
			supporting = append(supporting, file)
		}
	}
	interfaces := append([]string(nil), pack.Symbols...)
	constraints := []string{}
	if pack.RepositorySummary != "" {
		constraints = append(constraints, "Repository context: "+pack.RepositorySummary)
	}
	if pack.CodeStructureSummary != "" {
		constraints = append(constraints, "Structure summary: "+pack.CodeStructureSummary)
	}
	return StructuredSystemContext{
		Subsystem:         inferSubsystemFromPlanContext(task, pack),
		PrimaryTargets:    primary,
		SupportingTargets: supporting,
		Interfaces:        interfaces,
		Contracts:         defaultContractsForStructuredPlan(task, pack),
		Invariants:        defaultInvariantsForStructuredPlan(task, pack),
		DomainConstraints: constraints,
	}
}

func inferSubsystemFromPlanContext(task Task, pack ContextPack) string {
	if task.ActiveFile != "" {
		return strings.TrimSpace(task.ActiveFile)
	}
	if pack.ModuleName != "" {
		return pack.ModuleName
	}
	return "coding workflow"
}

func defaultContractsForStructuredPlan(task Task, pack ContextPack) []string {
	contracts := []string{"Keep the change aligned with the intended behavior of the active task."}
	if len(pack.TestFiles) > 0 {
		contracts = append(contracts, "Tests and implementation should reflect the same intended contract.")
	}
	return contracts
}

func defaultInvariantsForStructuredPlan(task Task, pack ContextPack) []string {
	invariants := []string{"Keep the change scoped to the evidence gathered for the task."}
	if task.ActiveFile != "" {
		invariants = append(invariants, "Prefer the active file and its nearby dependencies before widening scope.")
	}
	return invariants
}

func defaultValidationForStructuredStep(idx, total int, pack ContextPack) []string {
	validation := []string{"review the resulting diff"}
	if len(pack.TestFiles) > 0 {
		validation = append(validation, fmt.Sprintf("run or inspect relevant tests: %s", strings.Join(pack.TestFiles, ", ")))
	}
	if idx == total-1 {
		validation = append(validation, "confirm the change satisfies the original request")
	}
	return validation
}

func defaultFilesForStructuredStep(task Task, pack ContextPack) []string {
	files := make([]string, 0, len(pack.RelatedFiles)+len(pack.DependencyFiles)+1)
	if task.ActiveFile != "" {
		files = append(files, task.ActiveFile)
	}
	for _, file := range pack.RelatedFiles {
		if file != "" && !contains(files, file) {
			files = append(files, file)
		}
	}
	for _, file := range pack.DependencyFiles {
		if file != "" && !contains(files, file) {
			files = append(files, file)
		}
	}
	return files
}

func defaultEvidenceForStructuredStep(idx, total int, task Task, pack ContextPack) []string {
	evidence := []string{"local behavior or state evidence from the current context"}
	if idx == 0 {
		evidence = []string{"target scope", "current behavior or current state", "nearest supporting context"}
	}
	if len(pack.TestFiles) > 0 {
		evidence = append(evidence, fmt.Sprintf("validation evidence from %s", strings.Join(pack.TestFiles, ", ")))
	}
	lower := strings.ToLower(strings.TrimSpace(task.Description))
	if strings.Contains(lower, "store") || strings.Contains(lower, "database") {
		evidence = append(evidence, "schema and result-shape evidence")
	}
	if strings.Contains(lower, "space") || strings.Contains(lower, "knowledge") {
		evidence = append(evidence, "retrieval or knowledge-state evidence")
	}
	if idx == total-1 {
		evidence = append(evidence, "final validation outcome")
	}
	return evidence
}

func defaultDecisionRuleForStructuredStep(idx, total int, task Task, pack ContextPack) string {
	lower := strings.ToLower(strings.TrimSpace(task.Description))
	if idx == 0 {
		if len(pack.TestFiles) > 0 {
			return "Decide whether the issue is in the implementation, the expectation, or missing context before changing anything."
		}
		return "Decide the narrowest correct next action from the observed context before mutating state."
	}
	if strings.Contains(lower, "store") || strings.Contains(lower, "database") {
		return "Prefer the smallest store operation or query change that matches observed schema and expected result contract."
	}
	if strings.Contains(lower, "space") || strings.Contains(lower, "knowledge") {
		return "Prefer the narrowest retrieval or knowledge mutation scope that preserves the intended knowledge contract."
	}
	if idx == total-1 {
		return "Only conclude success when validation aligns with the original objective and no unresolved blocker remains."
	}
	return "Prefer the smallest change or operation that satisfies the current step without broadening scope."
}

func defaultAllowedScopeForStructuredStep(idx, total int, task Task, pack ContextPack) string {
	if idx == 0 {
		return "analysis only"
	}
	if idx == total-1 {
		return "validation and summary only"
	}
	lower := strings.ToLower(strings.TrimSpace(task.Description))
	if strings.Contains(lower, "store") || strings.Contains(lower, "database") {
		return "narrow store operation or query change only"
	}
	if strings.Contains(lower, "space") || strings.Contains(lower, "knowledge") {
		return "narrow retrieval or knowledge mutation only"
	}
	if len(pack.TestFiles) > 0 {
		return "localized code and/or expectation change only"
	}
	return "localized change only"
}

func defaultNotesForStructuredStep(idx, total int, task Task, pack ContextPack) []string {
	notes := []string{}
	if idx == 0 && strings.TrimSpace(pack.CodeStructureSummary) != "" {
		notes = append(notes, "structure: "+pack.CodeStructureSummary)
	}
	if strings.TrimSpace(pack.RepositorySummary) != "" {
		notes = append(notes, "context: "+pack.RepositorySummary)
	}
	if idx == total-1 {
		notes = append(notes, "summarize what changed and what remains uncertain")
	}
	if strings.TrimSpace(task.Description) != "" {
		notes = append(notes, "task: "+strings.TrimSpace(task.Description))
	}
	return notes
}

func defaultExitCriteriaForStructuredStep(idx, total int, pack ContextPack) []string {
	criteria := []string{"step result is captured in the plan context"}
	if len(pack.TestFiles) > 0 {
		criteria = append(criteria, "relevant tests pass or are explicitly accounted for")
	}
	if idx == total-1 {
		criteria = append(criteria, "execution can hand off cleanly to final validation")
	}
	return criteria
}

func defaultCompletionGateRequirements(task Task, state RunState, pack ContextPack) []string {
	domain := inferPlanDomain(task, state, pack)
	lowerDomain := strings.TrimSpace(strings.ToLower(domain))
	requirements := []string{"Current objective is satisfied or the blocking condition is explicit."}

	switch lowerDomain {
	case "stores":
		requirements = append(requirements,
			"The store result shape matches the store contract and any mutation resolves the observed mismatch.",
			"Read-only vs mutating behavior is checked against the relevant store schema and result-shape validation contract.")
	case "spaces":
		requirements = append(requirements,
			"The retrieval or knowledge-state outcome matches the intended space semantics after the mutation.",
			"Any content change is scoped to the target space and validated for semantic integrity.")
	case "desktop":
		requirements = append(requirements,
			"The command output or filesystem state matches the expected execution result without widening scope.",
			"Destructive or mutating commands are explicitly validated and constrained to the intended target.")
	default:
		if len(pack.TestFiles) > 0 {
			requirements = append(requirements, "The most relevant validation step has been rerun or the blocker is recorded.")
		}
	}

	requirements = append(requirements, "The final summary states what changed and what remains uncertain.")
	return requirements
}

func buildStructuredPlannerPrompt(task Task, state RunState, pack ContextPack) (string, error) {
	domain := inferPlanDomain(task, state, pack)
	planningContext := ""
	if defaultSharedPlanningContextResolver != nil {
		planningContext = defaultSharedPlanningContextResolver(queryTextForPlanningPrompt(task, pack), domain)
	}
	domainGuidance := domainSpecificPlannerGuidance(domain)
	planQualityRules := []string{
		"- Every step must explain what to inspect or change, why it matters, and how it will be validated.",
		"- Prefer structural context over vague choreography: targets, contracts, invariants, decision rules, and validation must be explicit.",
		"- Keep the plan narrow and operational; avoid broad speculative rewrites.",
		"- If the task is a repair, include a decision rule that distinguishes stale expectations from broken implementation or incorrect operations.",
		"- If the task is a design or feature change, make slice boundaries and acceptance checks explicit before broad execution.",
	}
	examples := plannerPromptExamples(domain)
	payload := map[string]any{
		"task":               task,
		"state":              state,
		"pack":               pack,
		"domain":             domain,
		"capability_context": planningContext,
		"planner_guidance":   domainGuidance,
	}
	bytes, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`You are a domain-agnostic execution planner for an AI agent.

Generate a strict JSON structured plan that can be persisted in workstream state and executed step-by-step.

Requirements:
- Use the provided context only.
- Produce a plan that is narrow, grounded, and operational.
- Include structural context: subsystem, primary targets, supporting targets, interfaces, contracts, invariants, and domain constraints when available.
- For each step include: step_id, step_domain, goal, files, evidence_to_collect, decision_rule, allowed_change_scope, action_instruction, validation, exit_criteria, escalate_if, status, notes.
- Default each step status to "pending".
- Use concise, actionable language.
- Do not wrap the JSON in markdown.

Quality bar:
%s

Domain-specific planning guidance:
%s

Examples:
%s

Context:
%s`, strings.Join(planQualityRules, "\n"), domainGuidance, examples, string(bytes)), nil
}

func domainSpecificPlannerGuidance(domain string) string {
	switch strings.TrimSpace(strings.ToLower(domain)) {
	case "stores":
		return strings.Join([]string{
			"- For Stores, identify target databases or stores, schema assumptions, relation boundaries, and whether the plan is read-only vs mutating.",
			"- Good plans mention store schema and result-shape validation and the specific store operation or script boundary.",
		}, "\n")
	case "spaces":
		return strings.Join([]string{
			"- For Spaces, identify target spaces, category scope, retrieval inputs, mutation boundaries, and semantic integrity expectations.",
			"- Good plans mention the expected retrieval or knowledge-state outcome after each mutation step.",
		}, "\n")
	case "desktop":
		return strings.Join([]string{
			"- For Desktop, identify working directory, command scope, filesystem boundaries, and command validation steps.",
			"- Good plans distinguish bounded inspection commands from commands that mutate local state.",
		}, "\n")
	default:
		return strings.Join([]string{
			"- For Coding, identify subsystem, primary files, supporting files, interfaces, tests, and behavior contracts.",
			"- Good plans distinguish code defects, stale tests, and broader architectural mismatch before editing.",
		}, "\n")
	}
}

func plannerPromptExamples(domain string) string {
	switch strings.TrimSpace(strings.ToLower(domain)) {
	case "stores":
		return strings.Join([]string{
			"- Example step goal: Inspect the target store schema and read-only vs mutating boundaries before touching query logic.",
			"- Example decision rule: If the result shape differs from the schema expectation, verify the store path, scope, and read-only vs mutating contract before changing the script.",
			"- Example validation: run the read-only scan and verify the expected records, mutation count, and store schema and result-shape validation.",
		}, "\n")
	case "spaces":
		return strings.Join([]string{
			"- Example step goal: Inspect the target space scope, current knowledge surface, and any retrieval/mutation boundaries.",
			"- Example decision rule: If retrieval is wrong, distinguish stale expectation from stale knowledge before mutating content.",
			"- Example validation: confirm the intended retrieval or knowledge-state outcome and semantic integrity after the change.",
		}, "\n")
	case "desktop":
		return strings.Join([]string{
			"- Example step goal: Inspect the repository or workspace state with a bounded command and explicit shell or filesystem scope.",
			"- Example decision rule: If the command can mutate state, require the narrowest scope, a safe target, and explicit validation path before execution.",
			"- Example validation: verify command output, git state, filesystem changes, and the final execution result against the original goal.",
		}, "\n")
	default:
		return strings.Join([]string{
			"- Example step goal: Compare the failing behavior to the nearest implementation path and the relevant test contract.",
			"- Example decision rule: Distinguish stale expectations from broken implementation before editing code or tests.",
			"- Example validation: rerun the narrowest relevant test or check before widening scope and confirm the fix with a regression signal.",
		}, "\n")
	}
}

var defaultSharedPlanningContextResolver func(query string, domain string) string

// SetDefaultPlanningContextResolver lets the host inject capability-provided planning context
// into the shared planner prompt without changing the Planner contract.
func SetDefaultPlanningContextResolver(resolver func(query string, domain string) string) {
	defaultSharedPlanningContextResolver = resolver
}

func queryTextForPlanningPrompt(task Task, pack ContextPack) string {
	if strings.TrimSpace(task.Description) != "" {
		return strings.TrimSpace(task.Description)
	}
	if strings.TrimSpace(pack.ContinuationSummary) != "" {
		return strings.TrimSpace(pack.ContinuationSummary)
	}
	return ""
}

func structuredPlannerResponseSchema() map[string]any {
	stepSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"step_id":              map[string]any{"type": "string"},
			"step_domain":          map[string]any{"type": "string"},
			"goal":                 map[string]any{"type": "string"},
			"files":                map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"evidence_to_collect":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"decision_rule":        map[string]any{"type": "string"},
			"allowed_change_scope": map[string]any{"type": "string"},
			"action_instruction":   map[string]any{"type": "string"},
			"validation":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"exit_criteria":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"escalate_if":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"status":               map[string]any{"type": "string"},
			"notes":                map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{"goal", "action_instruction"},
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"objective":      map[string]any{"type": "string"},
			"workflow_shape": map[string]any{"type": "string"},
			"domain":         map[string]any{"type": "string"},
			"system_context": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"subsystem":          map[string]any{"type": "string"},
					"primary_targets":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"supporting_targets": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"interfaces":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"contracts":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"invariants":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"domain_constraints": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
			},
			"steps": map[string]any{"type": "array", "items": stepSchema},
			"completion_gate": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"requirements": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
			},
		},
		"required": []string{"objective", "workflow_shape", "steps"},
	}
}

func parseStructuredPlannerResponse(text string) (StructuredPlan, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return StructuredPlan{}, false
	}
	var plan StructuredPlan
	if err := json.Unmarshal([]byte(trimmed), &plan); err == nil {
		return plan, true
	}
	if cleaned := extractJSONFromMarkdownLocal(trimmed); cleaned != "" {
		if err := json.Unmarshal([]byte(cleaned), &plan); err == nil {
			return plan, true
		}
	}
	return StructuredPlan{}, false
}

func extractJSONFromMarkdownLocal(text string) string {
	start := strings.Index(text, "```")
	if start == -1 {
		return ""
	}
	remainder := text[start+3:]
	if strings.HasPrefix(strings.ToLower(remainder), "json") {
		remainder = remainder[4:]
	}
	end := strings.Index(remainder, "```")
	if end == -1 {
		return ""
	}
	return strings.TrimSpace(remainder[:end])
}

func normalizeStructuredPlan(plan StructuredPlan, task Task, state RunState, pack ContextPack) StructuredPlan {
	if strings.TrimSpace(plan.Objective) == "" {
		plan.Objective = strings.TrimSpace(task.Description)
	}
	if strings.TrimSpace(plan.Objective) == "" {
		plan.Objective = "Complete the requested task"
	}
	if strings.TrimSpace(plan.Domain) == "" {
		plan.Domain = inferPlanDomain(task, state, pack)
	}
	if strings.TrimSpace(plan.WorkflowShape) == "" {
		plan.WorkflowShape = defaultWorkflowShape(task, pack)
	}
	if len(plan.SystemContext.PrimaryTargets) == 0 && len(plan.SystemContext.SupportingTargets) == 0 {
		plan.SystemContext = defaultStructuredSystemContext(task, pack)
	}
	if len(plan.CompletionGate.Requirements) == 0 {
		plan.CompletionGate.Requirements = defaultCompletionGateRequirements(task, state, pack)
	}
	if len(plan.Steps) == 0 {
		plan = buildStructuredPlanFromBase(task, state, pack, plan.Domain, plan.WorkflowShape, []string{"inspect relevant context", "define the next safe action", "validate the result"})
		return plan
	}
	for idx := range plan.Steps {
		if strings.TrimSpace(plan.Steps[idx].StepID) == "" {
			plan.Steps[idx].StepID = fmt.Sprintf("step_%d", idx+1)
		}
		if strings.TrimSpace(plan.Steps[idx].StepDomain) == "" {
			plan.Steps[idx].StepDomain = plan.Domain
		}
		if strings.TrimSpace(plan.Steps[idx].Status) == "" {
			plan.Steps[idx].Status = "pending"
		}
		if len(plan.Steps[idx].Files) == 0 {
			plan.Steps[idx].Files = defaultFilesForStructuredStep(task, pack)
		}
		if len(plan.Steps[idx].EvidenceToCollect) == 0 {
			plan.Steps[idx].EvidenceToCollect = defaultEvidenceForStructuredStep(idx, len(plan.Steps), task, pack)
		}
		if strings.TrimSpace(plan.Steps[idx].DecisionRule) == "" {
			plan.Steps[idx].DecisionRule = defaultDecisionRuleForStructuredStep(idx, len(plan.Steps), task, pack)
		}
		if strings.TrimSpace(plan.Steps[idx].AllowedChangeScope) == "" {
			plan.Steps[idx].AllowedChangeScope = defaultAllowedScopeForStructuredStep(idx, len(plan.Steps), task, pack)
		}
		if len(plan.Steps[idx].Validation) == 0 {
			plan.Steps[idx].Validation = defaultValidationForStructuredStep(idx, len(plan.Steps), pack)
		}
		if len(plan.Steps[idx].ExitCriteria) == 0 {
			plan.Steps[idx].ExitCriteria = defaultExitCriteriaForStructuredStep(idx, len(plan.Steps), pack)
		}
		if len(plan.Steps[idx].Notes) == 0 {
			plan.Steps[idx].Notes = defaultNotesForStructuredStep(idx, len(plan.Steps), task, pack)
		}
	}
	if plan.CurrentStep < 0 || plan.CurrentStep >= len(plan.Steps) {
		plan.CurrentStep = 0
	}
	return plan
}

func inferPlanDomain(task Task, state RunState, pack ContextPack) string {
	if state.Metadata != nil {
		if domain := strings.TrimSpace(state.Metadata["domain"]); domain != "" {
			return domain
		}
	}
	if state.Args != nil {
		if domain := strings.TrimSpace(state.Args["domain"]); domain != "" {
			return domain
		}
	}
	return "coding_workflow"
}

func defaultWorkflowShape(task Task, pack ContextPack) string {
	if pack.TaskType == "planning" {
		return "design_implementation"
	}
	if pack.TaskType == "analysis" {
		return "analysis"
	}
	if pack.TaskType == "generation" {
		return "generation"
	}
	return "implementation"
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
type DefaultTestRunner struct {
	infra *SharedInfrastructure // For accessing LocalCommandExecutor to route through PTY
}

func (d *DefaultTestRunner) Run(ctx context.Context, task Task, state RunState) (TestResult, error) {
	workingDir := task.WorkingDir
	if workingDir == "" {
		workingDir = "."
	}

	// Log for debugging
	log.Printf("[DefaultTestRunner] Run called with workingDir=%q", workingDir)

	// Detect project type
	projectType := d.detectProjectType(workingDir)

	// Log detection result
	log.Printf("[DefaultTestRunner] Detected project type: %v for workingDir=%q", projectType, workingDir)

	// Dispatch to language-specific test runner
	switch projectType {
	case ProjectTypeGo:
		return d.runGoTests(ctx, task, workingDir, state)
	case ProjectTypePython:
		return d.runPythonTests(ctx, workingDir, task.ActiveFile, state)
	case ProjectTypeJava:
		return d.runJavaTests(ctx, workingDir, state)
	case ProjectTypeRust:
		return d.runRustTests(ctx, workingDir, state)
	case ProjectTypeCSharp:
		return d.runCSharpTests(ctx, workingDir, state)
	case ProjectTypeNode:
		return d.runNodeTests(ctx, workingDir, state)
	default:
		return TestResult{
			Passed: false,
			Output: fmt.Sprintf("Unknown project type in %s. Unable to determine test command.", workingDir),
		}, nil
	}
}

// detectProjectType identifies the project language based on marker files
func (d *DefaultTestRunner) detectProjectType(workingDir string) ProjectType {
	// Log marker file checks
	log.Printf("[detectProjectType] Checking workingDir=%q", workingDir)

	// Go: go.mod
	goModPath := filepath.Join(workingDir, "go.mod")
	if fileExists(goModPath) {
		log.Printf("[detectProjectType] Found go.mod at %q - returning ProjectTypeGo", goModPath)
		return ProjectTypeGo
	}
	log.Printf("[detectProjectType] go.mod not found at %q", goModPath)

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

// runGoTests executes Go tests with query-aware focusing
func (d *DefaultTestRunner) runGoTests(ctx context.Context, task Task, workingDir string, state RunState) (TestResult, error) {
	args := []string{"test"}

	// Parse query for test name patterns and package paths
	testPattern := extractGoTestPattern(task.Description)
	packagePath := extractGoPackagePath(task.Description)

	if testPattern != "" {
		// Focused test run: go test -run TestName ./package
		if packagePath != "" {
			args = append(args, "-run", testPattern, "./"+packagePath)
			log.Printf("[runGoTests] Focused test: -run %s ./%s", testPattern, packagePath)
		} else {
			args = append(args, "-run", testPattern, "./...")
			log.Printf("[runGoTests] Focused test: -run %s ./...", testPattern)
		}
	} else if packagePath != "" {
		// Package-scoped: go test ./package
		args = append(args, "./"+packagePath)
		log.Printf("[runGoTests] Package-scoped: ./%s", packagePath)
	} else {
		// Broad pass: go test ./...
		args = append(args, "./...")
		log.Printf("[runGoTests] Broad pass: ./...")
	}

	// Route through LocalCommandExecutor to use PTY for stdout/stderr capture
	cmdStr := "go " + strings.Join(args, " ")
	output, passed := d.executeCommandThroughPTY(ctx, cmdStr, workingDir, state)
	return TestResult{Passed: passed, Output: output}, nil
}

// extractGoTestPattern extracts test name from query like "TestAuthenticateUser_ValidCredentials"
func extractGoTestPattern(query string) string {
	// Match Go test names (TestXxx, Test_xxx)
	re := regexp.MustCompile(`\b(Test[A-Z_][A-Za-z0-9_]*)\b`)
	if matches := re.FindStringSubmatch(query); len(matches) > 1 {
		log.Printf("[extractGoTestPattern] Found test pattern: %s", matches[1])
		return matches[1]
	}
	log.Printf("[extractGoTestPattern] No test pattern found in query: %s", query)
	return ""
}

// extractGoPackagePath extracts package path from query with explicit markers
// Only matches when package path is explicitly indicated, not from prose like "failing packages/tests"
func extractGoPackagePath(query string) string {
	// Match explicit package indicators:
	// - "in package X", "test package X", "package X"
	// - "go test X" or "go test ./X"
	// - Paths starting with ./ or containing multiple segments clearly indicating a path
	patterns := []string{
		`(?:in|test|package)\s+([a-z][a-z0-9/_-]+(?:/[a-z][a-z0-9_-]+)+)`, // "in package tools/sopbackend"
		`go\s+test\s+(?:\./)?([a-z][a-z0-9/_-]+(?:/[a-z][a-z0-9_-]+)+)`,   // "go test tools/sopbackend"
		`\./([a-z][a-z0-9/_-]+(?:/[a-z][a-z0-9_-]+)+)`,                    // "./tools/sopbackend"
		`\b([a-z]+/[a-z]+/[a-z0-9_-]+)\b`,                                 // "cmd/daemon/main" (3+ segments)
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(query); len(matches) > 1 {
			pkg := strings.TrimPrefix(matches[1], "./")
			log.Printf("[extractGoPackagePath] Found package path: %s (pattern: %s)", pkg, pattern)
			return pkg
		}
	}
	log.Printf("[extractGoPackagePath] No explicit package path found in query: %s", query)
	return ""
}

// runPythonTests executes Python tests (pytest or unittest)
func (d *DefaultTestRunner) runPythonTests(ctx context.Context, workingDir, activeFile string, state RunState) (TestResult, error) {
	var cmdStr string

	// Try pytest first (more common in modern Python projects)
	if d.commandExists("pytest") {
		args := "-v"
		if activeFile != "" && strings.HasSuffix(activeFile, "_test.py") {
			args += " " + activeFile
		}
		cmdStr = "pytest " + args
	} else {
		// Fallback to unittest
		cmdStr = "python -m unittest discover -v"
	}

	// Route through LocalCommandExecutor to use PTY
	output, passed := d.executeCommandThroughPTY(ctx, cmdStr, workingDir, state)
	return TestResult{Passed: passed, Output: output}, nil
}

// runJavaTests executes Java tests (Maven or Gradle)
func (d *DefaultTestRunner) runJavaTests(ctx context.Context, workingDir string, state RunState) (TestResult, error) {
	var cmdStr string

	// Check if Maven or Gradle
	if fileExists(filepath.Join(workingDir, "pom.xml")) {
		cmdStr = "mvn test"
	} else if fileExists(filepath.Join(workingDir, "build.gradle")) {
		cmdStr = "gradle test"
	} else {
		return TestResult{
			Passed: false,
			Output: "No Maven (pom.xml) or Gradle (build.gradle) configuration found",
		}, nil
	}

	// Route through LocalCommandExecutor to use PTY
	output, _ := d.executeCommandThroughPTY(ctx, cmdStr, workingDir, state)

	// Check output for success indicators
	passed := strings.Contains(output, "BUILD SUCCESS") || strings.Contains(output, "BUILD SUCCESSFUL")
	return TestResult{Passed: passed, Output: output}, nil
}

// runRustTests executes Rust tests
func (d *DefaultTestRunner) runRustTests(ctx context.Context, workingDir string, state RunState) (TestResult, error) {
	// Route through LocalCommandExecutor to use PTY
	output, passed := d.executeCommandThroughPTY(ctx, "cargo test", workingDir, state)
	return TestResult{Passed: passed, Output: output}, nil
}

// runCSharpTests executes C# tests
func (d *DefaultTestRunner) runCSharpTests(ctx context.Context, workingDir string, state RunState) (TestResult, error) {
	// Route through LocalCommandExecutor to use PTY
	output, _ := d.executeCommandThroughPTY(ctx, "dotnet test", workingDir, state)

	// Check output for success indicators
	passed := !strings.Contains(output, "Failed!")
	return TestResult{Passed: passed, Output: output}, nil
}

// runNodeTests executes Node.js tests (npm or yarn)
func (d *DefaultTestRunner) runNodeTests(ctx context.Context, workingDir string, state RunState) (TestResult, error) {
	var cmdStr string

	// Check if npm or yarn
	if fileExists(filepath.Join(workingDir, "package-lock.json")) {
		cmdStr = "npm test"
	} else if fileExists(filepath.Join(workingDir, "yarn.lock")) {
		cmdStr = "yarn test"
	} else {
		return TestResult{
			Passed: false,
			Output: "No package-lock.json or yarn.lock found",
		}, nil
	}

	// Route through LocalCommandExecutor to use PTY
	output, passed := d.executeCommandThroughPTY(ctx, cmdStr, workingDir, state)
	return TestResult{Passed: passed, Output: output}, nil
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

// executeCommandThroughPTY routes command execution through LocalCommandExecutor
// which uses the daemon's PTY system for stdout/stderr capture and Terminal UI display
// CRITICAL: NEVER create context.Background() here - it strips ai.CtxKeyEventStreamer
// which breaks emitTerminalCommand() display in chat history.
func (d *DefaultTestRunner) executeCommandThroughPTY(ctx context.Context, command, workingDir string, state RunState) (output string, passed bool) {
	if d.infra == nil || d.infra.LocalCommandExecutor == nil {
		// Fallback to direct exec if infrastructure not available
		return d.executeCommandDirect(command, workingDir)
	}

	// IMPORTANT: Use the provided context - it contains the event streamer for chat display.
	// Creating context.Background() here would lose the streamer and break command visibility.
	args := map[string]any{
		"command":     command,
		"working_dir": workingDir,
	}

	// Extract SessionID from RunState.Metadata if available
	if state.Metadata != nil {
		if sessionID, ok := state.Metadata["terminal_session_id"]; ok && sessionID != "" {
			args["session_id"] = sessionID
		}
	}

	result, err := d.infra.LocalCommandExecutor(ctx, args)
	if err != nil {
		return err.Error(), false
	}

	// LocalCommandExecutor returns JSON {"stdout":"...", "stderr":"...", "exit_code":0}
	// Parse to extract actual output
	var response struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
	}
	if jsonErr := json.Unmarshal([]byte(result), &response); jsonErr == nil {
		combined := response.Stdout
		if response.Stderr != "" {
			if combined != "" {
				combined += "\n"
			}
			combined += response.Stderr
		}
		return combined, response.ExitCode == 0
	}

	// If not JSON, return raw result
	return result, !strings.Contains(strings.ToLower(result), "error")
}

// executeCommandDirect is fallback for when PTY routing is unavailable
func (d *DefaultTestRunner) executeCommandDirect(command, workingDir string) (output string, passed bool) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = workingDir
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
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
		workingDirCandidates = []string{task.WorkingDir}
	} else {
		if cwd, err := os.Getwd(); err == nil {
			workingDirCandidates = []string{cwd}
		}
	}
	promptRequired := false // No longer prompt for working directory - use session context
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

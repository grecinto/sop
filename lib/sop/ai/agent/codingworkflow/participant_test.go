package codingworkflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultContextManagerBuildsContext(t *testing.T) {
	tmpDir := t.TempDir()
	activePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(activePath, []byte("package main\n\nfunc Hello() string {\n\treturn \"hi\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write active file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "main_test.go"), []byte("package main\n\nimport \"testing\"\n\nfunc TestHello(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	manager := &DefaultContextManager{}
	pack, err := manager.BuildContext(Task{WorkingDir: tmpDir, ActiveFile: "main.go"}, RunState{})
	if err != nil {
		t.Fatalf("BuildContext returned error: %v", err)
	}
	if pack.ActiveFile == "" {
		t.Fatalf("expected active file to be populated")
	}
	if !strings.Contains(pack.ActiveFileSnippet, "func Hello") {
		t.Fatalf("expected snippet to contain function declaration, got %q", pack.ActiveFileSnippet)
	}
	if len(pack.TestFiles) < 1 {
		t.Fatalf("expected at least one related test file, got %d", len(pack.TestFiles))
	}
}

func TestDefaultContextManagerIncludesModuleMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/demo\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	manager := &DefaultContextManager{}
	pack, err := manager.BuildContext(Task{WorkingDir: tmpDir, ActiveFile: "main.go"}, RunState{})
	if err != nil {
		t.Fatalf("BuildContext returned error: %v", err)
	}
	if pack.ModuleName != "example.com/demo" {
		t.Fatalf("expected module name example.com/demo, got %q", pack.ModuleName)
	}
	if !strings.Contains(pack.RepositorySummary, "example.com/demo") {
		t.Fatalf("expected repository summary to include module name, got %q", pack.RepositorySummary)
	}
	if !contains(pack.RepositoryFiles, "go.mod") {
		t.Fatalf("expected repository files to include go.mod, got %v", pack.RepositoryFiles)
	}
}

func TestDefaultContextManagerExtractsImportsAndCodeStructure(t *testing.T) {
	tmpDir := t.TempDir()
	activePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(activePath, []byte("package main\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n)\n\n\ntype Config struct {\n\tName string\n}\n\nfunc BuildConfig() Config {\n\treturn Config{Name: \"demo\"}\n}\n"), 0o644); err != nil {
		t.Fatalf("write active file: %v", err)
	}

	manager := &DefaultContextManager{}
	pack, err := manager.BuildContext(Task{WorkingDir: tmpDir, ActiveFile: "main.go"}, RunState{})
	if err != nil {
		t.Fatalf("BuildContext returned error: %v", err)
	}
	if !contains(pack.Imports, "fmt") {
		t.Fatalf("expected imports to include fmt, got %v", pack.Imports)
	}
	if !contains(pack.Imports, "strings") {
		t.Fatalf("expected imports to include strings, got %v", pack.Imports)
	}
	if !contains(pack.Symbols, "Config") {
		t.Fatalf("expected symbols to include Config, got %v", pack.Symbols)
	}
	if !contains(pack.Symbols, "BuildConfig") {
		t.Fatalf("expected symbols to include BuildConfig, got %v", pack.Symbols)
	}
	if !strings.Contains(pack.CodeStructureSummary, "imports") {
		t.Fatalf("expected code structure summary to mention imports, got %q", pack.CodeStructureSummary)
	}
}

func TestDefaultContextManagerFindsRelatedFilesInNestedRepositoryPaths(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n\nfunc BuildConfig() string { return \"demo\" }\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "pkg", "internal"), 0o755); err != nil {
		t.Fatalf("mkdir nested dirs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "pkg", "internal", "helper.go"), []byte("package internal\n\nfunc BuildConfig() string { return \"helper\" }\n"), 0o644); err != nil {
		t.Fatalf("write helper.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "pkg", "internal", "other.go"), []byte("package internal\n\nfunc Unrelated() string { return \"nope\" }\n"), 0o644); err != nil {
		t.Fatalf("write other.go: %v", err)
	}

	manager := &DefaultContextManager{}
	pack, err := manager.BuildContext(Task{WorkingDir: tmpDir, ActiveFile: "main.go"}, RunState{})
	if err != nil {
		t.Fatalf("BuildContext returned error: %v", err)
	}
	if !contains(pack.DependencyFiles, filepath.ToSlash(filepath.Join("pkg", "internal", "helper.go"))) {
		t.Fatalf("expected nested helper file to be surfaced as dependency context, got %v", pack.DependencyFiles)
	}
}

func TestDefaultPlannerBuildsExecutionPlan(t *testing.T) {
	planner := &DefaultPlanner{}
	pack := ContextPack{ActiveFile: "main.go", RelatedFiles: []string{"main_test.go"}, TestFiles: []string{"main_test.go"}}
	plan, err := planner.Plan(Task{WorkingDir: ".", ActiveFile: "main.go", Description: "fix parser"}, RunState{}, pack)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(plan) == 0 {
		t.Fatalf("expected a non-empty execution plan")
	}
	joined := strings.Join(plan, " ")
	if !strings.Contains(joined, "inspect") {
		t.Fatalf("expected plan to include inspection step, got %q", joined)
	}
	if !strings.Contains(joined, "tests") {
		t.Fatalf("expected plan to include testing step, got %q", joined)
	}
}

func TestDefaultPlannerAddsDecompositionAndValidationStepsForComplexCodeTasks(t *testing.T) {
	planner := &DefaultPlanner{}
	pack := ContextPack{TaskType: "code", ActiveFile: "parser.go", RelatedFiles: []string{"parser.go", "parser_test.go"}, TestFiles: []string{"parser_test.go"}}
	plan, err := planner.Plan(Task{WorkingDir: ".", ActiveFile: "parser.go", Description: "implement parser and add coverage across parser and serializer"}, RunState{}, pack)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	joined := strings.Join(plan, " ")
	for _, want := range []string{"decompose", "validate", "tests"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected plan to include %q, got %q", want, joined)
		}
	}
}

func TestDefaultPlannerAddsGroundingStepWhenExpertiseGuidanceExists(t *testing.T) {
	planner := &DefaultPlanner{}
	pack := ContextPack{ActiveFile: "main.go", ExpertiseSummary: "Actionable generation guidance: research the selected Space first"}
	plan, err := planner.Plan(Task{WorkingDir: ".", ActiveFile: "main.go", Description: "implement parser"}, RunState{}, pack)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	joined := strings.Join(plan, " ")
	if !strings.Contains(joined, "gather grounding") {
		t.Fatalf("expected plan to include a grounding step, got %q", joined)
	}
}

func TestDefaultContextManagerBuildsGeneralContextWithoutActiveFile(t *testing.T) {
	tmpDir := t.TempDir()
	manager := &DefaultContextManager{}
	pack, err := manager.BuildContext(Task{WorkingDir: tmpDir, Description: "analyze store inventory and propose a SQL-based cleanup plan"}, RunState{Metadata: map[string]string{"continuation_summary": "continue from prior store analysis"}})
	if err != nil {
		t.Fatalf("BuildContext returned error: %v", err)
	}
	if pack.ActiveFile != "" {
		t.Fatalf("expected no active file for general analysis context, got %q", pack.ActiveFile)
	}
	if pack.TaskType != "analysis" {
		t.Fatalf("expected analysis task type, got %q", pack.TaskType)
	}
	if !strings.Contains(pack.ContinuationSummary, "prior store analysis") {
		t.Fatalf("expected continuation summary to be carried over, got %q", pack.ContinuationSummary)
	}
}

func TestDefaultPlannerBuildsGeneralSolutionDiscoveryPlan(t *testing.T) {
	planner := &DefaultPlanner{}
	pack := ContextPack{TaskType: "analysis", ContinuationSummary: "continue from prior store analysis"}
	plan, err := planner.Plan(Task{WorkingDir: ".", Description: "analyze store inventory and propose a SQL-based cleanup plan"}, RunState{}, pack)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	joined := strings.Join(plan, " ")
	if !strings.Contains(joined, "derive a solution") {
		t.Fatalf("expected general analysis plan to include solution discovery, got %q", joined)
	}
	if !strings.Contains(joined, "validate the proposed approach") {
		t.Fatalf("expected general analysis plan to include validation guidance, got %q", joined)
	}
}

func TestDefaultContextManagerClassifiesGenerationTasks(t *testing.T) {
	manager := &DefaultContextManager{}
	pack, err := manager.BuildContext(Task{WorkingDir: ".", Description: "generate a lego-style JSON script for a store workflow"}, RunState{})
	if err != nil {
		t.Fatalf("BuildContext returned error: %v", err)
	}
	if pack.TaskType != "generation" {
		t.Fatalf("expected generation task type, got %q", pack.TaskType)
	}
}

func TestDefaultContextManagerClassifiesPlanningTasks(t *testing.T) {
	manager := &DefaultContextManager{}
	pack, err := manager.BuildContext(Task{WorkingDir: ".", Description: "help me design a parser and discuss tradeoffs"}, RunState{})
	if err != nil {
		t.Fatalf("BuildContext returned error: %v", err)
	}
	if pack.TaskType != "planning" {
		t.Fatalf("expected planning task type, got %q", pack.TaskType)
	}
}

func TestDefaultContextManagerClassifiesImplementationAfterPlanningIntent(t *testing.T) {
	manager := &DefaultContextManager{}
	pack, err := manager.BuildContext(Task{WorkingDir: ".", Description: "implement the parser design now"}, RunState{Metadata: map[string]string{"mode": "planning"}})
	if err != nil {
		t.Fatalf("BuildContext returned error: %v", err)
	}
	if pack.TaskType != "code" {
		t.Fatalf("expected implementation handoff to resolve to code mode, got %q", pack.TaskType)
	}
}

func TestDefaultExpertiseRetrieverBuildsFocusedGuidanceForCodeTasks(t *testing.T) {
	retriever := &DefaultExpertiseRetriever{}
	capsule, err := retriever.Retrieve(Task{WorkingDir: ".", Description: "implement a parser and add coverage"}, RunState{}, ContextPack{TaskType: "code", ActiveFile: "parser.go"})
	if err != nil {
		t.Fatalf("Retrieve returned error: %v", err)
	}
	if capsule.Summary == "" {
		t.Fatalf("expected a non-empty expertise summary")
	}
	lower := strings.ToLower(capsule.Summary)
	if !strings.Contains(lower, "tests") && !strings.Contains(lower, "best practice") {
		t.Fatalf("expected guidance to mention testing or best-practice advice, got %q", capsule.Summary)
	}
}

func TestDefaultPlannerBuildsGenerationPlan(t *testing.T) {
	planner := &DefaultPlanner{}
	pack := ContextPack{TaskType: "generation", RepositorySummary: "schema=store"}
	plan, err := planner.Plan(Task{WorkingDir: ".", Description: "generate a lego-style JSON script for a store workflow"}, RunState{}, pack)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	joined := strings.Join(plan, " ")
	if !strings.Contains(joined, "compose the requested artifact") {
		t.Fatalf("expected generation plan to include composition guidance, got %q", joined)
	}
	if !strings.Contains(joined, "validate the structure") {
		t.Fatalf("expected generation plan to include structure validation, got %q", joined)
	}
}

func TestDefaultContextManagerIncludesNestedRepositoryFiles(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "pkg", "internal"), 0o755); err != nil {
		t.Fatalf("mkdir nested dirs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "pkg", "internal", "helper.go"), []byte("package internal\n"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	manager := &DefaultContextManager{}
	pack, err := manager.BuildContext(Task{WorkingDir: tmpDir, ActiveFile: "main.go"}, RunState{})
	if err != nil {
		t.Fatalf("BuildContext returned error: %v", err)
	}
	if !contains(pack.RepositoryFiles, filepath.ToSlash(filepath.Join("pkg", "internal", "helper.go"))) {
		t.Fatalf("expected nested repository file in context, got %v", pack.RepositoryFiles)
	}
}

func TestDefaultKnowledgeBaseRetrieverFindsRelevantDocs(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "docs", "parser-guide.md"), []byte("# Parser guide\n\nUse the parser carefully when editing syntax trees."), 0o644); err != nil {
		t.Fatalf("write guide: %v", err)
	}

	retriever := &DefaultKnowledgeBaseRetriever{}
	capsule, err := retriever.Retrieve(Task{WorkingDir: tmpDir, ActiveFile: "main.go", Description: "fix parser logic"}, RunState{}, ContextPack{})
	if err != nil {
		t.Fatalf("Retrieve returned error: %v", err)
	}
	if capsule.Summary == "" {
		t.Fatalf("expected a non-empty knowledge base summary")
	}
	if !strings.Contains(strings.ToLower(capsule.Summary), "parser") {
		t.Fatalf("expected summary to mention parser, got %q", capsule.Summary)
	}
	if len(capsule.Sources) == 0 {
		t.Fatalf("expected at least one knowledge source")
	}
}

func TestDefaultKnowledgeBaseRetrieverMatchesMorphologicalVariants(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "docs", "syntax-notes.md"), []byte("# Syntax notes\n\nThis guide explains how a parser should behave when handling tokens."), 0o644); err != nil {
		t.Fatalf("write guide: %v", err)
	}

	retriever := &DefaultKnowledgeBaseRetriever{}
	capsule, err := retriever.Retrieve(Task{WorkingDir: tmpDir, ActiveFile: "main.go", Description: "fix parsing behavior"}, RunState{}, ContextPack{})
	if err != nil {
		t.Fatalf("Retrieve returned error: %v", err)
	}
	if len(capsule.Sources) == 0 {
		t.Fatalf("expected to retrieve a matching doc for parsing behavior, got %#v", capsule)
	}
	if !strings.Contains(strings.ToLower(capsule.Summary), "parsing") && !strings.Contains(strings.ToLower(capsule.Summary), "parser") {
		t.Fatalf("expected summary to mention the matching parsing/parser guidance, got %q", capsule.Summary)
	}
}

func TestFileSessionMemoryPersistsToDiskAndBuildsSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.json")
	memory := NewFileSessionMemory(path)
	if err := memory.Save("latest-task", map[string]string{"description": "fix parser"}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	loaded, err := memory.Load("latest-task")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded["description"] != "fix parser" {
		t.Fatalf("expected persisted description, got %q", loaded["description"])
	}
	summary, err := memory.BuildVisualizationSummary()
	if err != nil {
		t.Fatalf("BuildVisualizationSummary returned error: %v", err)
	}
	if !strings.Contains(summary, "fix parser") {
		t.Fatalf("expected visualization summary to include the persisted task, got %q", summary)
	}
}

func TestDefaultProviderRouterResolvesBYOProvider(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	router := &DefaultProviderRouter{}
	selection, err := router.Resolve(Task{}, RunState{}, ContextPack{})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if selection.Provider != "openai" {
		t.Fatalf("expected openai provider, got %q", selection.Provider)
	}
	if selection.Source != "env" {
		t.Fatalf("expected env-backed source, got %q", selection.Source)
	}
}

func TestRunEvaluationSuiteReportsDisabledControllerPath(t *testing.T) {
	orchestrator := &Orchestrator{}
	results := RunEvaluationSuite(orchestrator, []EvaluationCase{{Name: "context-only", Task: Task{Description: "inspect hello"}, ExpectContains: "context assembled"}})
	if len(results) != 1 {
		t.Fatalf("expected one evaluation result, got %d", len(results))
	}
	if results[0].Passed {
		t.Fatal("expected disabled controller-path evaluation to report failure")
	}
	if !strings.Contains(results[0].Message, "controller-style execution is disabled") {
		t.Fatalf("expected disabled-path guidance in message, got %q", results[0].Message)
	}
}

func TestDefaultTestRunnerSelectsTargetedTests(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/demo\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "main_test.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main_test.go: %v", err)
	}

	runner := &DefaultTestRunner{}
	result, err := runner.Run(Task{WorkingDir: tmpDir, ActiveFile: "main.go"}, RunState{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Output == "" {
		t.Fatalf("expected test output to be populated")
	}
}

func TestDefaultTestRunnerSelectsRelatedTestFiles(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "helper.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write helper.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "main_test.go"), []byte("package main\n\nfunc TestMain(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatalf("write main_test.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "helper_test.go"), []byte("package main\n\nfunc TestHelper(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatalf("write helper_test.go: %v", err)
	}

	runner := &DefaultTestRunner{}
	files := runner.selectRelevantGoTestFiles(tmpDir, "main.go")
	if !contains(files, "main_test.go") {
		t.Fatalf("expected main_test.go to be selected, got %v", files)
	}
	if !contains(files, "helper_test.go") {
		t.Fatalf("expected helper_test.go to be selected, got %v", files)
	}
}

func TestDefaultPlannerBuildsMultiFilePlan(t *testing.T) {
	planner := &DefaultPlanner{}
	pack := ContextPack{ActiveFile: "main.go", RelatedFiles: []string{"main.go", "helper.go", "main_test.go"}, TestFiles: []string{"main_test.go"}}
	plan, err := planner.Plan(Task{WorkingDir: ".", ActiveFile: "main.go", Description: "refactor parser"}, RunState{}, pack)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	joined := strings.Join(plan, " ")
	if !strings.Contains(joined, "3 related file") {
		t.Fatalf("expected plan to mention related files, got %q", joined)
	}
}

func TestDefaultPlannerBuildsConcreteEditChecklist(t *testing.T) {
	planner := &DefaultPlanner{}
	pack := ContextPack{ActiveFile: "main.go", Symbols: []string{"Hello"}, DependencyFiles: []string{"helper.go"}, RelatedFiles: []string{"helper.go"}, TestFiles: []string{"main_test.go"}}
	plan, err := planner.Plan(Task{WorkingDir: ".", ActiveFile: "main.go", Description: "update Hello"}, RunState{}, pack)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	joined := strings.Join(plan, " ")
	if !strings.Contains(joined, "edit active file main.go") {
		t.Fatalf("expected checklist to include active file edit, got %q", joined)
	}
	if !strings.Contains(joined, "inspect dependency file(s): helper.go") {
		t.Fatalf("expected checklist to include dependency file guidance, got %q", joined)
	}
}

func TestDefaultContextManagerCollectsActiveFileSymbols(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n\nfunc Hello() string {\n\treturn \"hi\"\n}\n\ntype Config struct{}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	manager := &DefaultContextManager{}
	pack, err := manager.BuildContext(Task{WorkingDir: tmpDir, ActiveFile: "main.go", Description: "update Hello"}, RunState{})
	if err != nil {
		t.Fatalf("BuildContext returned error: %v", err)
	}
	if !contains(pack.Symbols, "Hello") {
		t.Fatalf("expected symbols to include Hello, got %v", pack.Symbols)
	}
	if !contains(pack.Symbols, "Config") {
		t.Fatalf("expected symbols to include Config, got %v", pack.Symbols)
	}
}

func TestDefaultContextManagerResumesActiveFileFromState(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	manager := &DefaultContextManager{}
	pack, err := manager.BuildContext(Task{WorkingDir: tmpDir, Description: "continue previous work"}, RunState{Metadata: map[string]string{"active_file": "main.go"}})
	if err != nil {
		t.Fatalf("BuildContext returned error: %v", err)
	}
	if pack.ActiveFile != "main.go" {
		t.Fatalf("expected active file to be resumed, got %q", pack.ActiveFile)
	}
	if !pack.ResumedFromState {
		t.Fatal("expected context to indicate it resumed from prior state")
	}
}

func TestDefaultContextManagerCollectsDependencyFiles(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n\nfunc Hello() string {\n\treturn Helper()\n}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "helper.go"), []byte("package main\n\nfunc Helper() string {\n\treturn \"noop\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write helper.go: %v", err)
	}

	manager := &DefaultContextManager{}
	pack, err := manager.BuildContext(Task{WorkingDir: tmpDir, ActiveFile: "main.go"}, RunState{})
	if err != nil {
		t.Fatalf("BuildContext returned error: %v", err)
	}
	if !contains(pack.DependencyFiles, "helper.go") {
		t.Fatalf("expected dependency files to include helper.go, got %v", pack.DependencyFiles)
	}
}

func TestDefaultPatchEngineAppliesReplacement(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	engine := &DefaultPatchEngine{}
	err := engine.Apply(Task{WorkingDir: tmpDir}, &Patch{FilePath: "sample.txt", OldContent: "hello", NewContent: "goodbye"}, RunState{})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file after patch: %v", err)
	}
	if !strings.Contains(string(content), "goodbye world") {
		t.Fatalf("expected patched content, got %q", string(content))
	}
}

func TestDefaultContextManagerBuildsSymbolAwareContext(t *testing.T) {
	tmpDir := t.TempDir()
	activePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(activePath, []byte("package main\n\nfunc Hello() string {\n\treturn \"hi\"\n}\n\nfunc Helper() string {\n\treturn \"noop\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write active file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "helper.go"), []byte("package main\n\nfunc Helper() string {\n\treturn \"noop\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write helper file: %v", err)
	}

	manager := &DefaultContextManager{}
	pack, err := manager.BuildContext(Task{WorkingDir: tmpDir, ActiveFile: "main.go", Description: "update Hello"}, RunState{})
	if err != nil {
		t.Fatalf("BuildContext returned error: %v", err)
	}
	if pack.SymbolName != "Hello" {
		t.Fatalf("expected symbol Hello, got %q", pack.SymbolName)
	}
	if !strings.Contains(pack.SymbolSnippet, "func Hello") {
		t.Fatalf("expected symbol snippet to contain func Hello, got %q", pack.SymbolSnippet)
	}
	if !contains(pack.RelatedFiles, "helper.go") {
		t.Fatalf("expected related files to include helper.go, got %v", pack.RelatedFiles)
	}
}

func TestDefaultContextManagerUsesASTLeafsForCodeAwareContext(t *testing.T) {
	tmpDir := t.TempDir()
	activePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(activePath, []byte("package main\n\nfunc Greeting() string {\n\treturn \"hi\"\n}\n\nfunc Helper() string {\n\treturn Greeting()\n}\n"), 0o644); err != nil {
		t.Fatalf("write active file: %v", err)
	}

	manager := &DefaultContextManager{}
	pack, err := manager.BuildContext(Task{WorkingDir: tmpDir, ActiveFile: "main.go", Description: "update Greeting"}, RunState{})
	if err != nil {
		t.Fatalf("BuildContext returned error: %v", err)
	}
	if !contains(pack.ASTLeafNodes, "Greeting") {
		t.Fatalf("expected AST leaf nodes to include Greeting, got %v", pack.ASTLeafNodes)
	}
	if !strings.Contains(pack.CodeStructureSummary, "ast") {
		t.Fatalf("expected code structure summary to mention AST context, got %q", pack.CodeStructureSummary)
	}
	if pack.StructuralPath == "" {
		t.Fatalf("expected a structural path to be generated")
	}
	if !strings.Contains(pack.StructuralPath, "Greeting") {
		t.Fatalf("expected structural path to include the resolved leaf, got %q", pack.StructuralPath)
	}
}

func TestDefaultSafetyPolicyAllowsExplicitApproval(t *testing.T) {
	policy := &DefaultSafetyPolicy{}
	err := policy.Allow(Task{Patch: &Patch{FilePath: "sample.txt"}}, RunState{Metadata: map[string]string{"approval": "granted"}})
	if err != nil {
		t.Fatalf("expected approval metadata to authorize patching, got %v", err)
	}
}

func TestDefaultContextManagerBuildsStructuralSnippet(t *testing.T) {
	tmpDir := t.TempDir()
	activePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(activePath, []byte("package main\n\nfunc Hello() string {\n\treturn \"hi\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write active file: %v", err)
	}

	manager := &DefaultContextManager{}
	pack, err := manager.BuildContext(Task{WorkingDir: tmpDir, ActiveFile: "main.go", Description: "update Hello"}, RunState{})
	if err != nil {
		t.Fatalf("BuildContext returned error: %v", err)
	}
	if !strings.Contains(pack.SymbolSnippet, "return \"hi\"") {
		t.Fatalf("expected structural snippet to include function body, got %q", pack.SymbolSnippet)
	}
}

func TestDefaultPatchEngineAppliesGitStylePatchContent(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if out, err := exec.Command("git", "init", tmpDir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, string(out))
	}

	engine := &DefaultPatchEngine{}
	err := engine.Apply(Task{WorkingDir: tmpDir}, &Patch{FilePath: "sample.txt", PatchContent: "diff --git a/sample.txt b/sample.txt\n--- a/sample.txt\n+++ b/sample.txt\n@@ -1 +1 @@\n-hello\n+goodbye\n"}, RunState{})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file after patch: %v", err)
	}
	if string(content) != "goodbye\n" {
		t.Fatalf("expected patched content, got %q", string(content))
	}
}

type stubChangedFileInspector struct {
	files []string
}

func (s *stubChangedFileInspector) ListChangedFiles(Task, RunState) ([]string, error) {
	return s.files, nil
}

type stubRepoInspector struct {
	summary RepoInspectionSummary
}

func (s *stubRepoInspector) Inspect(Task, RunState) (RepoInspectionSummary, error) {
	return s.summary, nil
}

type countingTestRunner struct {
	runs int
}

func (c *countingTestRunner) Run(task Task, state RunState) (TestResult, error) {
	c.runs++
	return TestResult{Passed: true, Output: "ok"}, nil
}

// mockExpertiseRetriever returns a fixed expertise capsule for testing
type mockExpertiseRetriever struct {
	capsule ExpertiseCapsule
}

func (m *mockExpertiseRetriever) Retrieve(Task, RunState, ContextPack) (ExpertiseCapsule, error) {
	return m.capsule, nil
}

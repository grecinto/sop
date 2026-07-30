package codingworkflow

import (
	"context"
	"fmt"

	ts "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

const (
	CapabilityID   = "coding_workflow"
	CapabilityName = "Coding Workflow"
)

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

// STMReader provides read access to ShortTermMemory for phase succession
type STMReader interface {
	GetCurrentThread() WorkStreamReader
	CreatePhasePlan(phases []string, checkpoints map[string]bool, reasoning, confidence string) error
}

// WorkStreamReader provides read access to ConversationThread for phase succession
type WorkStreamReader interface {
	GetStreamContext() WorkStreamContextReader
}

// WorkStreamContextReader provides read/write access to WorkStreamContext
type WorkStreamContextReader interface {
	GetPhasePlan() PhasePlanReader
	SetPhasePlan(PhasePlanReader)
}

// PhasePlanReader provides read/write access to PhasePlan
type PhasePlanReader interface {
	GetPlannedPhases() []string
	GetCurrentIndex() int
	SetCurrentIndex(int)
	GetCheckpoints() map[string]bool
	AdvancePhase() (nextPhase string, hasNext bool)
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

// SharedInfrastructure bundles reusable coding workflow tools for use by WorkflowManager Steps.
// This enables Steps to access AST analysis, tree-sitter parsing, patch application,
// test execution, and other repository operations without duplicating infrastructure.
//
// Design: Each Step receives SharedInfrastructure via context during execution.
// Steps call methods on this struct to perform operations like:
//   - AST analysis (analyze error messages, extract symbols)
//   - Tree-sitter parsing (parse files, query syntax trees)
//   - Patch operations (apply patches, preview diffs)
//   - Test execution (run tests, parse results)
//   - Repository operations (search, index, inspect)
//
// Guard rail: SharedInfrastructure wraps the existing workflow tools without
// modifying their behavior. This allows WorkflowManager to reuse proven infrastructure
// while maintaining backward compatibility with the older codingworkflow entry points.
type SharedInfrastructure struct {
	// Core infrastructure from Orchestrator
	ContextManager       ContextManager
	RepoIndexer          RepoIndexer
	PatchEngine          PatchEngine
	ChangedFileInspector ChangedFileInspector
	DiffPreviewer        DiffPreviewer
	RepoSearcher         RepoSearcher
	RepoInspector        RepoInspector
	TestRunner           TestRunner
	Planner              Planner
	ExpertiseRetriever   ExpertiseRetriever
	KBRetriever          KnowledgeBaseRetriever
	ProviderRouter       ProviderRouter
	GrammarRegistry      *GrammarRegistry
	Memory               SessionMemory
	Policy               SafetyPolicy

	// Validation infrastructure (Phase 1 enhancement)
	ValidationOrchestrator *ValidationOrchestrator

	// Memory sink for LTM integration
	MemorySink func(CodingMemory)

	// STMAccessor provides access to the agent's ShortTermMemory for phase succession
	// This allows phase capabilities to read/update PhasePlan in WorkStreamContext
	STMAccessor func() STMReader
}

// NewDefaultSharedInfrastructure creates a SharedInfrastructure with default runtime components.
func NewDefaultSharedInfrastructure() *SharedInfrastructure {
	return (&SharedInfrastructure{}).ensureDefaults()
}

func (s *SharedInfrastructure) ensureDefaults() *SharedInfrastructure {
	if s == nil {
		return NewDefaultSharedInfrastructure()
	}
	if s.Memory == nil {
		s.Memory = NewInMemorySessionMemory()
	}
	if s.ContextManager == nil {
		s.ContextManager = &DefaultContextManager{}
	}
	if s.RepoIndexer == nil {
		s.RepoIndexer = &DefaultRepoIndexer{}
	}
	if s.ChangedFileInspector == nil {
		s.ChangedFileInspector = &GitRepoInspector{}
	}
	if s.DiffPreviewer == nil {
		s.DiffPreviewer = &DefaultDiffPreviewer{}
	}
	if s.RepoSearcher == nil {
		s.RepoSearcher = &FileSearchAdapter{}
	}
	if s.RepoInspector == nil {
		s.RepoInspector = &GitRepoInspector{}
	}
	if s.TestRunner == nil {
		s.TestRunner = &DefaultTestRunner{}
	}
	if s.Planner == nil {
		s.Planner = &DefaultPlanner{}
	}
	if s.ExpertiseRetriever == nil {
		s.ExpertiseRetriever = &DefaultExpertiseRetriever{}
	}
	if s.KBRetriever == nil {
		s.KBRetriever = &DefaultKnowledgeBaseRetriever{}
	}
	if s.ProviderRouter == nil {
		s.ProviderRouter = &DefaultProviderRouter{}
	}
	if s.Policy == nil {
		s.Policy = &DefaultSafetyPolicy{}
	}
	if s.GrammarRegistry == nil {
		s.GrammarRegistry = NewGrammarRegistry()
	}
	return s
}

// ASTOperations provides high-level AST analysis operations for Steps.
type ASTOperations struct {
	grammar *GrammarRegistry
}

// NewASTOperations creates AST operations helper.
func (s *SharedInfrastructure) NewASTOperations() *ASTOperations {
	return &ASTOperations{grammar: s.GrammarRegistry}
}

// ParseFile parses a file using tree-sitter and returns the AST root node.
// Returns the parser and root node for further querying.
func (a *ASTOperations) ParseFile(filePath string, content []byte) (*ts.Parser, *ts.Node, error) {
	if a.grammar == nil {
		return nil, nil, fmt.Errorf("grammar registry not initialized")
	}

	// For tree-sitter, we need the actual language object
	// For now, default to Go (can be extended with language detection from file extension)
	lang := golang.GetLanguage()
	parser := ts.NewParser()
	parser.SetLanguage(lang)

	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return nil, nil, fmt.Errorf("parse failed: %w", err)
	}

	if tree == nil {
		return nil, nil, fmt.Errorf("parse returned nil tree")
	}

	root := tree.RootNode()
	if root == nil {
		return nil, nil, fmt.Errorf("root node is nil")
	}

	return parser, root, nil
}

// ExtractFunctions extracts function names from a parsed AST.
// Works for Go files (can be extended for other languages).
func (a *ASTOperations) ExtractFunctions(root *ts.Node, content []byte) []string {
	if root == nil {
		return nil
	}

	var functions []string
	cursor := ts.NewTreeCursor(root)
	defer cursor.Close()

	// Walk tree looking for function declarations
	var walk func()
	walk = func() {
		node := cursor.CurrentNode()
		if node.Type() == "function_declaration" || node.Type() == "method_declaration" {
			// Find the name node
			for i := uint32(0); i < node.ChildCount(); i++ {
				child := node.Child(int(i))
				if child.Type() == "identifier" {
					name := content[child.StartByte():child.EndByte()]
					functions = append(functions, string(name))
					break
				}
			}
		}

		if cursor.GoToFirstChild() {
			walk()
			cursor.GoToParent()
		}

		for cursor.GoToNextSibling() {
			walk()
		}
	}

	walk()
	return functions
}

// ExtractImports extracts import statements from a parsed AST.
// Works for Go files (can be extended for other languages).
func (a *ASTOperations) ExtractImports(root *ts.Node, content []byte) []string {
	if root == nil {
		return nil
	}

	var imports []string
	cursor := ts.NewTreeCursor(root)
	defer cursor.Close()

	// Walk tree looking for import declarations
	var walk func()
	walk = func() {
		node := cursor.CurrentNode()
		if node.Type() == "import_declaration" || node.Type() == "import_spec" {
			// Extract the import path
			for i := uint32(0); i < node.ChildCount(); i++ {
				child := node.Child(int(i))
				if child.Type() == "interpreted_string_literal" {
					path := content[child.StartByte():child.EndByte()]
					imports = append(imports, string(path))
				}
			}
		}

		if cursor.GoToFirstChild() {
			walk()
			cursor.GoToParent()
		}

		for cursor.GoToNextSibling() {
			walk()
		}
	}

	walk()
	return imports
}

// FindSymbolAtPosition finds the symbol (function, type, variable) at a given line/column.
// Returns the symbol name and its type (e.g., "function_declaration").
func (a *ASTOperations) FindSymbolAtPosition(root *ts.Node, content []byte, line, column uint32) (string, string, error) {
	if root == nil {
		return "", "", fmt.Errorf("root node is nil")
	}

	// Find node at position
	point := ts.Point{Row: line, Column: column}
	cursor := ts.NewTreeCursor(root)
	defer cursor.Close()

	var targetNode *ts.Node
	var walk func() bool
	walk = func() bool {
		node := cursor.CurrentNode()
		startPoint := node.StartPoint()
		endPoint := node.EndPoint()

		// Check if position is within this node
		if (startPoint.Row < point.Row || (startPoint.Row == point.Row && startPoint.Column <= point.Column)) &&
			(endPoint.Row > point.Row || (endPoint.Row == point.Row && endPoint.Column >= point.Column)) {

			// Found containing node
			targetNode = node

			// Try to go deeper
			if cursor.GoToFirstChild() {
				if walk() {
					return true
				}
				cursor.GoToParent()
			}

			return true
		}

		// Try siblings
		for cursor.GoToNextSibling() {
			if walk() {
				return true
			}
		}

		return false
	}

	walk()

	if targetNode == nil {
		return "", "", fmt.Errorf("no symbol found at position")
	}

	// Find the most specific named node
	for targetNode != nil {
		nodeType := targetNode.Type()
		if nodeType == "function_declaration" || nodeType == "method_declaration" ||
			nodeType == "type_declaration" || nodeType == "var_declaration" {
			// Extract name
			for i := uint32(0); i < targetNode.ChildCount(); i++ {
				child := targetNode.Child(int(i))
				if child.Type() == "identifier" || child.Type() == "type_identifier" {
					name := content[child.StartByte():child.EndByte()]
					return string(name), nodeType, nil
				}
			}
		}
		targetNode = targetNode.Parent()
	}

	return "", "", fmt.Errorf("no symbol found")
}

// ContextPackOperations provides helper methods for building and manipulating ContextPacks.
type ContextPackOperations struct {
	infra *SharedInfrastructure
}

// NewContextPackOperations creates context pack helper.
func (s *SharedInfrastructure) NewContextPackOperations() *ContextPackOperations {
	return &ContextPackOperations{infra: s}
}

// BuildContextForTask builds a ContextPack for a given task.
func (c *ContextPackOperations) BuildContextForTask(task Task, state RunState) (ContextPack, error) {
	if c.infra.ContextManager == nil {
		return ContextPack{}, fmt.Errorf("context manager not initialized")
	}
	return c.infra.ContextManager.BuildContext(task, state)
}

// EnrichWithExpertise adds expertise capsule to a context pack.
func (c *ContextPackOperations) EnrichWithExpertise(task Task, state RunState, pack *ContextPack) error {
	if c.infra.ExpertiseRetriever == nil {
		return fmt.Errorf("expertise retriever not initialized")
	}

	capsule, err := c.infra.ExpertiseRetriever.Retrieve(task, state, *pack)
	if err != nil {
		return err
	}

	pack.ExpertiseCapsule = capsule
	pack.ExpertiseSummary = capsule.Summary
	pack.ExpertiseSources = capsule.Sources
	return nil
}

// PatchOperations provides helper methods for patch generation and application.
type PatchOperations struct {
	infra *SharedInfrastructure
}

// NewPatchOperations creates patch operations helper.
func (s *SharedInfrastructure) NewPatchOperations() *PatchOperations {
	return &PatchOperations{infra: s}
}

// ApplyPatch applies a patch to disk with safety checks.
func (p *PatchOperations) ApplyPatch(task Task, patch *Patch, state RunState) error {
	// Check safety policy first
	if p.infra.Policy != nil {
		if err := p.infra.Policy.Allow(task, state); err != nil {
			return err
		}
	}

	if p.infra.PatchEngine == nil {
		return fmt.Errorf("patch engine not initialized")
	}

	return p.infra.PatchEngine.Apply(task, patch, state)
}

// PreviewPatch generates a preview of what a patch would do.
func (p *PatchOperations) PreviewPatch(task Task, patch *Patch, state RunState) (string, error) {
	if p.infra.DiffPreviewer == nil {
		return "", fmt.Errorf("diff previewer not initialized")
	}
	return p.infra.DiffPreviewer.Preview(task, patch, state)
}

// TestOperations provides helper methods for test execution and result parsing.
type TestOperations struct {
	infra *SharedInfrastructure
}

// NewTestOperations creates test operations helper.
func (s *SharedInfrastructure) NewTestOperations() *TestOperations {
	return &TestOperations{infra: s}
}

// RunTests executes tests for a task and returns results.
func (t *TestOperations) RunTests(task Task, state RunState) (TestResult, error) {
	if t.infra.TestRunner == nil {
		return TestResult{}, fmt.Errorf("test runner not initialized")
	}
	return t.infra.TestRunner.Run(task, state)
}

// RepoOperations provides helper methods for repository operations.
type RepoOperations struct {
	infra *SharedInfrastructure
}

// NewRepoOperations creates repository operations helper.
func (s *SharedInfrastructure) NewRepoOperations() *RepoOperations {
	return &RepoOperations{infra: s}
}

// SearchRepo searches the repository for a query.
func (r *RepoOperations) SearchRepo(task Task, state RunState, query string) ([]string, error) {
	if r.infra.RepoSearcher == nil {
		return nil, fmt.Errorf("repo searcher not initialized")
	}
	return r.infra.RepoSearcher.Search(task, state, query)
}

// IndexRepo indexes the repository and returns file list.
func (r *RepoOperations) IndexRepo(task Task, state RunState) ([]string, error) {
	if r.infra.RepoIndexer == nil {
		return nil, fmt.Errorf("repo indexer not initialized")
	}
	return r.infra.RepoIndexer.Index(task, state)
}

// InspectRepo produces a repository summary.
func (r *RepoOperations) InspectRepo(task Task, state RunState) (RepoInspectionSummary, error) {
	if r.infra.RepoInspector == nil {
		return RepoInspectionSummary{}, fmt.Errorf("repo inspector not initialized")
	}
	return r.infra.RepoInspector.Inspect(task, state)
}

// ListChangedFiles returns list of changed files in the repository.
func (r *RepoOperations) ListChangedFiles(task Task, state RunState) ([]string, error) {
	if r.infra.ChangedFileInspector == nil {
		return nil, fmt.Errorf("changed file inspector not initialized")
	}
	return r.infra.ChangedFileInspector.ListChangedFiles(task, state)
}

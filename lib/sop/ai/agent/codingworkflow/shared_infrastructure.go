package codingworkflow

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	ts "github.com/smacker/go-tree-sitter"
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
	Plan(Task, RunState, ContextPack) (StructuredPlan, error)
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
	StoreMultiStepPlan(planData any) error // Stores a multi-step plan (handles JSON marshaling)
}

// WorkStreamReader provides read access to ConversationThread for phase succession
type WorkStreamReader interface {
	GetStreamContext() WorkStreamContextReader
}

// WorkStreamContextReader provides read/write access to WorkStreamContext
type WorkStreamContextReader interface {
	GetPhasePlan() PhasePlanReader
	SetPhasePlan(PhasePlanReader)
	GetStructuredPlan() StructuredPlanReader
	SetStructuredPlan(StructuredPlanReader)
	GetMultiStepPlan() MultiStepPlanReader
	SetMultiStepPlan(MultiStepPlanReader)
	GetPhaseArtifacts() map[string]map[string]any
}

// StructuredPlanReader provides read/write access to a structured execution plan.
type StructuredPlanReader interface {
	GetObjective() string
	GetWorkflowShape() string
	GetDomain() string
	GetSystemContext() StructuredSystemContext
	GetSteps() []StructuredPlanStep
	SetSteps([]StructuredPlanStep)
	GetCurrentStep() int
	SetCurrentStep(int)
	GetCompletedSteps() []string
	SetCompletedSteps([]string)
	GetCompletionGate() StructuredCompletionGate
}

// PhasePlanReader provides read/write access to PhasePlan
type PhasePlanReader interface {
	GetPlannedPhases() []string
	GetCurrentIndex() int
	SetCurrentIndex(int)
	GetCheckpoints() map[string]bool
	AdvancePhase() (nextPhase string, hasNext bool)
}

// MultiStepPlanReader provides read/write access to MultiStepPlan
type MultiStepPlanReader interface {
	GetSteps() []StepReader
	GetCurrentStepIndex() int
	SetCurrentStepIndex(int)
	GetCompletedSteps() []string
	SetCompletedSteps([]string)
	GetTotalSteps() int
}

// StepReader provides read access to a single step in a multi-step plan
type StepReader interface {
	GetStepID() string
	GetStepNumber() int
	GetGoal() string
	GetDomain() string
	GetStatus() string
	SetStatus(string)
	GetArtifacts() map[string]any
	SetArtifacts(map[string]any)
	SetCompletedAt(int64)
	GetPhasePlan() PhasePlanReader // Access to step's phase plan
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
// CRITICAL: Must accept context.Context to preserve event streamer for chat display.
// NEVER create context.Background() - it strips the event streamer needed for emitTerminalCommand().
type TestRunner interface {
	Run(ctx context.Context, task Task, state RunState) (TestResult, error)
}

// TestResult captures the outcome of a test run.
type TestResult struct {
	Passed bool
	Output string
}

// SharedInfrastructure bundles reusable coding workflow tools for the active
// planner, phase, and workstream system, while also remaining compatible with
// deprecated workflow-manager code.
// This enables orchestrated execution paths to access AST analysis, tree-sitter
// parsing, patch application, test execution, and other repository operations
// without duplicating infrastructure.
//
// Design: active phase/planner code and legacy Step implementations call methods
// on this struct to perform operations like:
//   - AST analysis (analyze error messages, extract symbols)
//   - Tree-sitter parsing (parse files, query syntax trees)
//   - Patch operations (apply patches, preview diffs)
//   - Test execution (run tests, parse results)
//   - Repository operations (search, index, inspect)
//
// Guard rail: SharedInfrastructure is not a signal to extend deprecated
// WorkflowManager/Step orchestration. New features should plug into Planner,
// Multi-Step Planner, or phase/workstream execution and only rely on this type
// as a reusable tool bundle.
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

	// GetSessionQueue provides access to the agent's work queue for enqueueing next steps
	// This allows capabilities to route work to other capabilities (Participant Pattern)
	GetSessionQueue func() WorkQueueAccessor

	// Adaptive classification support - enables switching between heuristic/LLM classification
	// When nil, defaults to heuristic (fast keyword matching)
	// Set to enable LLM-based or adaptive classification strategies
	PhaseClassificationStrategy any // Type: planner.PhaseClassificationStrategy (avoiding import cycle)

	// Classification history tracks user corrections and failures to enable adaptive classification
	// Allows system to learn from user feedback and escalate to LLM when heuristics fail
	ClassificationHistory any // Type: *planner.ClassificationHistory (avoiding import cycle)

	// Phase plan mutator enables dynamic phase plan modification during conversation
	// Allows LLM to add/remove/reorder phases based on user feedback or discoveries
	PhasePlanMutator any // Type: planner.PhasePlanMutator (avoiding import cycle)

	// LTMAccessor provides access to Long-Term Memory for team patterns
	// This allows phase capabilities to search team-specific coding conventions
	// Returns any to avoid import cycle, caller should type assert to *memory.KnowledgeBase[map[string]any]
	LTMAccessor func(ctx context.Context) (any, error)

	// DatabaseAccessor provides access to the database for operations like vectorization
	// Returns any to avoid import cycle, caller should type assert to *database.Database
	DatabaseAccessor func(ctx context.Context) (any, error)

	// LTMName returns the name of the Long-Term Memory knowledge base
	LTMName func() string

	// LLMAccessor provides access to the LLM generator for operations like vectorization
	// Returns any to avoid import cycle, caller should type assert to ai.Generator
	LLMAccessor func(ctx context.Context) (any, error)

	// PlanningContextResolver lets domain capabilities contribute planning context
	// while the planner remains domain agnostic in schema and lifecycle.
	PlanningContextResolver func(query string, domain string) string

	// EmbedderAccessor provides access to the embeddings provider for vectorization
	// Returns any to avoid import cycle, caller should type assert to ai.Embeddings
	EmbedderAccessor func(ctx context.Context) (any, error)

	// LocalCommandExecutor reuses the agent's bounded local-command path for
	// desktop/shell phase tools so they preserve known-command wrapping and
	// large-output handling instead of bypassing it with ad hoc execution.
	LocalCommandExecutor func(ctx context.Context, args map[string]any) (string, error)
}

// WorkQueueAccessor provides a generic interface for enqueueing work items
type WorkQueueAccessor interface {
	Enqueue(item any) error
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
		s.TestRunner = &DefaultTestRunner{infra: s}
	}
	if s.Planner == nil {
		if s.LLMAccessor != nil {
			s.Planner = NewLLMPlanner(s.LLMAccessor, &DefaultPlanner{})
		} else {
			s.Planner = &DefaultPlanner{}
		}
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

	lang, alias := resolveTreeSitterLanguageForPath(filePath)
	if lang == nil {
		return nil, nil, fmt.Errorf("unsupported file type for AST parsing: %s", filepath.Ext(filePath))
	}

	parser := ts.NewParser()
	parser.SetLanguage(lang)

	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return nil, nil, fmt.Errorf("parse failed: %w", err)
	}

	if tree == nil {
		return nil, nil, fmt.Errorf("parse returned nil tree for language %q", alias)
	}

	root := tree.RootNode()
	if root == nil {
		return nil, nil, fmt.Errorf("root node is nil")
	}

	return parser, root, nil
}

// ExtractFunctions extracts function names from a parsed AST.
// Supports Go, Python, JavaScript/TypeScript, Java, and Rust AST shapes.
func (a *ASTOperations) ExtractFunctions(root *ts.Node, content []byte) []string {
	if root == nil {
		return nil
	}

	functionNodeTypes := map[string]bool{
		"function_declaration":    true,
		"method_declaration":      true,
		"function_definition":     true,
		"constructor_declaration": true,
		"function_item":           true,
		"method_definition":       true,
	}

	functions := make([]string, 0)
	seen := make(map[string]struct{})

	walkAST(root, func(node *ts.Node) {
		if node == nil || !functionNodeTypes[node.Type()] {
			return
		}

		name := extractNodeName(node, content)
		if name == "" {
			return
		}

		if _, exists := seen[name]; exists {
			return
		}

		seen[name] = struct{}{}
		functions = append(functions, name)
	})

	return functions
}

// ExtractImports extracts import statements from a parsed AST.
// Supports Go, Python, JavaScript/TypeScript, Java, and Rust AST shapes.
func (a *ASTOperations) ExtractImports(root *ts.Node, content []byte) []string {
	if root == nil {
		return nil
	}

	importNodeTypes := map[string]bool{
		"import_spec":           true,
		"import_declaration":    true,
		"import_statement":      true,
		"import_from_statement": true,
		"use_declaration":       true,
		"using_directive":       true,
	}

	imports := make([]string, 0)
	seen := make(map[string]struct{})

	walkAST(root, func(node *ts.Node) {
		if node == nil || !importNodeTypes[node.Type()] {
			return
		}

		value := extractImportValue(node, content)
		if value == "" {
			return
		}

		if _, exists := seen[value]; exists {
			return
		}

		seen[value] = struct{}{}
		imports = append(imports, value)
	})

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
			nodeType == "function_definition" || nodeType == "constructor_declaration" ||
			nodeType == "function_item" || nodeType == "method_definition" ||
			nodeType == "type_declaration" || nodeType == "var_declaration" ||
			nodeType == "type_spec" || nodeType == "class_definition" ||
			nodeType == "class_declaration" || nodeType == "interface_declaration" ||
			nodeType == "enum_declaration" || nodeType == "struct_item" ||
			nodeType == "trait_item" {
			// Extract name
			for i := uint32(0); i < targetNode.ChildCount(); i++ {
				child := targetNode.Child(int(i))
				if child.Type() == "identifier" || child.Type() == "type_identifier" ||
					child.Type() == "field_identifier" || child.Type() == "property_identifier" {
					name := content[child.StartByte():child.EndByte()]
					return string(name), nodeType, nil
				}
			}
		}
		targetNode = targetNode.Parent()
	}

	return "", "", fmt.Errorf("no symbol found")
}

func walkAST(node *ts.Node, visit func(*ts.Node)) {
	if node == nil || visit == nil {
		return
	}

	visit(node)
	for i := 0; i < int(node.NamedChildCount()); i++ {
		walkAST(node.NamedChild(i), visit)
	}
}

func extractNodeName(node *ts.Node, content []byte) string {
	if node == nil {
		return ""
	}

	for _, fieldName := range []string{"name", "identifier"} {
		if child := node.ChildByFieldName(fieldName); child != nil {
			if name := normalizeNodeText(child.Content(content)); name != "" {
				return name
			}
		}
	}

	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "identifier", "field_identifier", "property_identifier", "type_identifier":
			if name := normalizeNodeText(child.Content(content)); name != "" {
				return name
			}
		}
	}

	return ""
}

func extractImportValue(node *ts.Node, content []byte) string {
	if node == nil {
		return ""
	}

	stringNodeTypes := map[string]bool{
		"interpreted_string_literal": true,
		"raw_string_literal":         true,
		"string":                     true,
		"string_literal":             true,
	}

	if literal := findFirstNodeContent(node, content, stringNodeTypes); literal != "" {
		return strings.Trim(literal, "\"'`")
	}

	text := normalizeNodeText(node.Content(content))
	if text == "" {
		return ""
	}

	return text
}

func findFirstNodeContent(node *ts.Node, content []byte, types map[string]bool) string {
	if node == nil {
		return ""
	}

	if types[node.Type()] {
		return normalizeNodeText(node.Content(content))
	}

	for i := 0; i < int(node.NamedChildCount()); i++ {
		if value := findFirstNodeContent(node.NamedChild(i), content, types); value != "" {
			return value
		}
	}

	return ""
}

func normalizeNodeText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
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
func (t *TestOperations) RunTests(ctx context.Context, task Task, state RunState) (TestResult, error) {
	if t.infra.TestRunner == nil {
		return TestResult{}, fmt.Errorf("test runner not initialized")
	}
	return t.infra.TestRunner.Run(ctx, task, state)
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

func (r *RepoOperations) SearchRepoWithContext(ctx context.Context, task Task, state RunState, query string) ([]string, error) {
	if r.infra.RepoSearcher == nil {
		return nil, fmt.Errorf("repo searcher not initialized")
	}
	if searcher, ok := r.infra.RepoSearcher.(interface {
		SearchWithContext(context.Context, Task, RunState, string) ([]string, error)
	}); ok {
		return searcher.SearchWithContext(ctx, task, state, query)
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

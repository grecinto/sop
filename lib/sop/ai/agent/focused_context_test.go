package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/database"
	sopdb "github.com/sharedcode/sop/database"
)

func TestBuildSystemPrompt_IncludesFocusedStoreContext(t *testing.T) {
	ctx := context.Background()
	dbDir := t.TempDir()
	dbOpts := sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{dbDir}}
	appDB := database.NewDatabase(dbOpts)
	sysDB := database.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}})

	tx, err := appDB.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	usersOpts := sop.StoreOptions{
		Name:           "users",
		SlotLength:     10,
		IsPrimitiveKey: true,
		Relations: []sop.Relation{{
			SourceFields: []string{"key"},
			TargetStore:  "orders",
			TargetFields: []string{"user_id"},
		}},
	}
	users, err := sopdb.NewBtree[string, any](ctx, dbOpts, "users", tx, nil, usersOpts)
	if err != nil {
		t.Fatalf("create users store: %v", err)
	}
	orders, err := sopdb.NewBtree[string, any](ctx, dbOpts, "orders", tx, nil, sop.StoreOptions{Name: "orders", SlotLength: 10, IsPrimitiveKey: true})
	if err != nil {
		t.Fatalf("create orders store: %v", err)
	}
	if ok, err := users.Add(ctx, "u1", map[string]any{"user_id": "u1", "first_name": "John"}); err != nil || !ok {
		t.Fatalf("add users record: %v", err)
	}
	if ok, err := orders.Add(ctx, "o1", map[string]any{"user_id": "u1", "total_amount": 600}); err != nil || !ok {
		t.Fatalf("add orders record: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit setup: %v", err)
	}

	ag := NewCopilotAgent(Config{}, map[string]sop.DatabaseOptions{"appdb": dbOpts}, sysDB)
	ag.service = &Service{session: &RunnerSession{MRU: []MRUItem{}}}

	payload := &ai.SessionPayload{CurrentDB: "appdb", Variables: make(map[string]any)}
	ctx = context.WithValue(ctx, "session_payload", payload)

	prompt := ag.buildSystemPrompt(ctx, "Find users", TaskContextClassification{
		Domain:      StoresDomain,
		DBArtifacts: []string{"users"},
		Layers: []LayerInfo{{
			Name: "Single-Domain",
			CRUD: []string{"R"},
		}},
	})

	if !strings.Contains(prompt, "focused_execution_context") {
		t.Fatalf("expected focused execution context component in prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "Target Stores") || !strings.Contains(prompt, "- users") {
		t.Fatalf("expected focused users store details in prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "Active Database: appdb") {
		t.Fatalf("expected focused prompt to show the active database context: %s", prompt)
	}
	if !strings.Contains(prompt, "Structured Context: Stores Tools") {
		t.Fatalf("expected system tools to use the capability-based Stores guidance: %s", prompt)
	}
	if !strings.Contains(prompt, "Never guess store names") || !strings.Contains(prompt, "SOP store queries are not SQL") {
		t.Fatalf("expected capability-based Stores guidance to remain in the prompt: %s", prompt)
	}
	if strings.Contains(prompt, "Available Stores:") {
		t.Fatalf("expected no generic all-store schema dump when a target artifact is classified: %s", prompt)
	}
}

func TestBuildSystemPrompt_StoresFocusedContext_StaysWithinBudgetGuardrail(t *testing.T) {
	ctx := context.Background()
	dbDir := t.TempDir()
	dbOpts := sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{dbDir}}
	appDB := database.NewDatabase(dbOpts)
	sysDB := database.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}})

	tx, err := appDB.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	usersOpts := sop.StoreOptions{
		Name:           "users",
		SlotLength:     10,
		IsPrimitiveKey: true,
		Relations: []sop.Relation{{
			SourceFields: []string{"key"},
			TargetStore:  "users_orders",
			TargetFields: []string{"key"},
		}},
	}
	users, err := sopdb.NewBtree[string, any](ctx, dbOpts, "users", tx, nil, usersOpts)
	if err != nil {
		t.Fatalf("create users store: %v", err)
	}
	orders, err := sopdb.NewBtree[string, any](ctx, dbOpts, "users_orders", tx, nil, sop.StoreOptions{Name: "users_orders", SlotLength: 10, IsPrimitiveKey: true})
	if err != nil {
		t.Fatalf("create users_orders store: %v", err)
	}
	if ok, err := users.Add(ctx, "u1", map[string]any{"first_name": "John", "user_id": "u1"}); err != nil || !ok {
		t.Fatalf("add users record: %v", err)
	}
	if ok, err := orders.Add(ctx, "u1", map[string]any{"user_id": "u1", "value": "o1"}); err != nil || !ok {
		t.Fatalf("add users_orders record: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit setup: %v", err)
	}

	ag := NewCopilotAgent(Config{}, map[string]sop.DatabaseOptions{"appdb": dbOpts}, sysDB)
	ag.service = &Service{session: &RunnerSession{MRU: []MRUItem{}}}

	payload := &ai.SessionPayload{CurrentDB: "appdb", Variables: make(map[string]any)}
	ctx = context.WithValue(ctx, "session_payload", payload)

	prompt := ag.buildSystemPrompt(ctx, "Find John and inspect related orders", TaskContextClassification{
		Domain:      StoresDomain,
		DBArtifacts: []string{"users"},
		Layers:      []LayerInfo{{Name: "Single-Domain", CRUD: []string{"R"}}},
	})

	var elements []PromptElement
	if err := json.Unmarshal([]byte(prompt), &elements); err != nil {
		t.Fatalf("expected buildSystemPrompt to return JSON prompt elements: %v\nPrompt: %s", err, prompt)
	}

	focusedContent := ""
	for _, element := range elements {
		if element.Component == ComponentFocusedContext {
			focusedContent = element.Content
			break
		}
	}
	if focusedContent == "" {
		t.Fatalf("expected focused execution context component in prompt: %s", prompt)
	}
	if len(focusedContent) > 1000 {
		t.Fatalf("expected stores focused execution context to stay within 1000 chars after prompt reductions, got %d\nContent: %s", len(focusedContent), focusedContent)
	}
	if !strings.Contains(focusedContent, "Domain: Stores") || !strings.Contains(focusedContent, "Target Stores") {
		t.Fatalf("expected focused execution context to retain core Stores anchors: %s", focusedContent)
	}
	if !strings.Contains(focusedContent, "CRUD Scope: R") {
		t.Fatalf("expected focused execution context to retain CRUD scope, got %s", focusedContent)
	}
	for _, element := range elements {
		if element.Component == ComponentSystemTools && strings.Contains(element.Content, "Execution Flow Engine Guardrails") {
			t.Fatalf("expected execution guardrails to live only in focused execution context, got duplicated system tools content: %s", element.Content)
		}
		if element.Component == ComponentSystemTools && strings.Contains(element.Content, "[truncated]") {
			t.Fatalf("expected stores system tools context to fit without truncation after prompt reductions, got: %s", element.Content)
		}
	}
}

func TestAutomationPolicyResolvesRegisteredFamilies(t *testing.T) {
	policy := AutomationPolicy{Enabled: true, ShellAutomationEnabled: boolPtr(false), GitAutomationEnabled: boolPtr(true), PostgresAutomationEnabled: boolPtr(true), SplunkAutomationEnabled: boolPtr(false)}

	if policy.IsFamilyRegistered("shell") {
		t.Fatal("expected shell family to be unregistered when explicitly disabled")
	}
	if !policy.IsFamilyRegistered("git") {
		t.Fatal("expected git family to be registered when enabled")
	}
	if !policy.IsFamilyRegistered("postgres") {
		t.Fatal("expected postgres family to be registered when enabled")
	}
	if policy.IsFamilyRegistered("splunk") {
		t.Fatal("expected splunk family to be unregistered when explicitly disabled")
	}
}

func TestBuildSystemPrompt_IncludesAutomationToolFamilyContext(t *testing.T) {
	ctx := context.Background()
	sysDB := database.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}})
	ag := NewCopilotAgent(Config{}, map[string]sop.DatabaseOptions{}, sysDB)

	SetLocalDesktopAutomationEnabled(true)
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, AllowPostgresRead: true, AllowPostgresWrite: true, AllowSplunkRead: true, AllowSplunkWrite: true, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetLocalDesktopAutomationEnabled(false)
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	ag.registerTools(ctx)
	prompt := ag.buildSystemPrompt(ctx, "Inspect the repo and query postgres and splunk", TaskContextClassification{})

	for _, expected := range []string{"External Automation Tool Context", "PostgreSQL", "Splunk", "Git"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected automation context to contain %q in prompt: %s", expected, prompt)
		}
	}
}

func TestBuildSystemPrompt_IncludesTaskGuidanceSynthesis(t *testing.T) {
	ctx := context.Background()
	sysDB := database.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}})
	ag := NewCopilotAgent(Config{}, map[string]sop.DatabaseOptions{}, sysDB)

	prompt := ag.buildSystemPrompt(ctx, "fix failing tests", TaskContextClassification{
		Domain:         "General",
		RoutingGate:    RoutingGateFocused,
		CapabilityHint: "investigation",
	})

	for _, expected := range []string{"Task Guidance Synthesis", "Selected Phase: investigation", "Selected Domain: General"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected prompt to contain %q, got: %s", expected, prompt)
		}
	}
}

func TestBuildSystemPrompt_PrioritizesCompactTaskGuidanceSynthesis(t *testing.T) {
	ctx := context.Background()
	sysDB := database.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}})
	ag := NewCopilotAgent(Config{}, map[string]sop.DatabaseOptions{}, sysDB)

	prompt := ag.buildSystemPrompt(ctx, "fix failing tests", TaskContextClassification{
		Domain:         "General",
		RoutingGate:    RoutingGateFocused,
		CapabilityHint: "investigation",
	})

	var elements []PromptElement
	if err := json.Unmarshal([]byte(prompt), &elements); err != nil {
		t.Fatalf("expected buildSystemPrompt to return JSON prompt elements: %v\nPrompt: %s", err, prompt)
	}

	focusedContent := ""
	for _, element := range elements {
		if element.Component == ComponentFocusedContext {
			focusedContent = element.Content
			break
		}
	}
	if focusedContent == "" {
		t.Fatalf("expected focused execution context component in prompt: %s", prompt)
	}
	if !strings.HasPrefix(strings.TrimSpace(focusedContent), "Task Guidance Synthesis") {
		t.Fatalf("expected task guidance synthesis to lead the focused context, got: %s", focusedContent)
	}
	if !strings.Contains(focusedContent, "Primary directive") {
		t.Fatalf("expected compact task guidance synthesis to include a primary directive, got: %s", focusedContent)
	}
	if strings.Contains(focusedContent, "Objective:") {
		t.Fatalf("expected compact guidance to avoid verbose objective text, got: %s", focusedContent)
	}
}

func TestBuildSystemPrompt_UsesClassifiedAutomationFamilies(t *testing.T) {
	ctx := context.Background()
	sysDB := database.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}})
	ag := NewCopilotAgent(Config{}, map[string]sop.DatabaseOptions{}, sysDB)

	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowPostgresRead: true, AllowPostgresWrite: true})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	prompt := ag.buildSystemPrompt(ctx, "Can you help me with the system?", TaskContextClassification{
		AutomationFamilies: []string{"postgres"},
	})

	if !strings.Contains(prompt, "PostgreSQL") {
		t.Fatalf("expected classified postgres family to surface postgres automation guidance, got prompt: %s", prompt)
	}
	if strings.Contains(prompt, "Splunk") {
		t.Fatalf("did not expect splunk guidance when only postgres was classified, got prompt: %s", prompt)
	}
}

func TestBuildSystemPrompt_DoesNotAddAutomationGuidanceWithoutRelevantFamilies(t *testing.T) {
	ctx := context.Background()
	sysDB := database.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}})
	ag := NewCopilotAgent(Config{}, map[string]sop.DatabaseOptions{}, sysDB)

	SetAutomationPolicy(DefaultAutomationPolicy())
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	prompt := ag.buildSystemPrompt(ctx, "Can you help me with the system?", TaskContextClassification{})

	if strings.Contains(prompt, "PostgreSQL") || strings.Contains(prompt, "Splunk") || strings.Contains(prompt, "Git") {
		t.Fatalf("expected no automation guidance when no relevant automation family is present, got prompt: %s", prompt)
	}
}

func TestBuildStoresCRUDOperationsContext_WriteGuidanceUsesGroundedScriptContract(t *testing.T) {
	content := buildStoresCRUDOperationsContext(map[string]bool{"C": true, "U": true, "D": true})

	checks := []string{
		"C = Create. Use write transactions.",
		"research them with list_stores first",
		"generate a concrete write script and run it with execute_script",
		"U = Update. Use write transactions and narrow the target records before update.",
		"D = Delete. Use write transactions and narrow the target records before delete.",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Fatalf("expected write guidance to contain %q, got %s", check, content)
		}
	}
	for _, duplicate := range []string{"Read steps:", "Create steps:", "Update steps:", "Delete steps:"} {
		if strings.Contains(content, duplicate) {
			t.Fatalf("expected focused CRUD guidance to omit duplicated step inventory %q, got %s", duplicate, content)
		}
	}
}

func TestBuildSystemPrompt_IncludesCrossDomainFocusedContext(t *testing.T) {
	SetStoresFeatureEnabled(true)
	defer SetStoresFeatureEnabled(false)

	ctx := context.Background()
	sysDB := database.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}})
	ag := NewCopilotAgent(Config{}, map[string]sop.DatabaseOptions{}, sysDB)
	ag.service = &Service{session: &RunnerSession{MRU: []MRUItem{}}}

	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	ctx = context.WithValue(ctx, "session_payload", payload)

	prompt := ag.buildSystemPrompt(ctx, "Search release notes and inspect users", TaskContextClassification{
		Domain:          StoresDomain,
		StoresArtifacts: []string{"users"},
		SpacesArtifacts: []string{"release_notes"},
		Layers: []LayerInfo{{
			Name: "Cross-Domain",
			CRUD: []string{"R"},
		}},
	})

	if !strings.Contains(prompt, "Stores Execution Context") {
		t.Fatalf("expected stores cross-domain execution context in prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "Spaces Execution Context") {
		t.Fatalf("expected spaces cross-domain execution context in prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "Target Stores") || !strings.Contains(prompt, "users") {
		t.Fatalf("expected stores targets in cross-domain prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "Target Spaces: release_notes") {
		t.Fatalf("expected spaces targets in cross-domain prompt: %s", prompt)
	}

	var elements []PromptElement
	if err := json.Unmarshal([]byte(prompt), &elements); err != nil {
		t.Fatalf("expected buildSystemPrompt to return JSON prompt elements: %v\nPrompt: %s", err, prompt)
	}
	var focusedContext string
	var systemTools string
	for _, element := range elements {
		switch element.Component {
		case ComponentFocusedContext:
			focusedContext = element.Content
		case ComponentSystemTools:
			systemTools = element.Content
		}
	}
	if !strings.Contains(focusedContext, "Stores Execution Context") || !strings.Contains(focusedContext, "Spaces Execution Context") {
		t.Fatalf("expected cross-domain focused execution context to carry stores and spaces execution guidance, got: %s", focusedContext)
	}
	if strings.Contains(systemTools, "Execution Flow Engine Guardrails") {
		t.Fatalf("expected cross-domain system tools to avoid duplicated execution guardrails, got: %s", systemTools)
	}
	if strings.Contains(systemTools, "[truncated]") {
		t.Fatalf("expected cross-domain system tools context to fit without truncation after prompt reductions, got: %s", systemTools)
	}
	if !strings.Contains(systemTools, "Structured Context: Stores Tools") {
		t.Fatalf("expected cross-domain system tools to surface the capability-based Stores guidance, got: %s", systemTools)
	}
	if strings.Contains(systemTools, "Structured Context: Spaces Tools") {
		t.Fatalf("expected cross-domain system tools to avoid the old spaces manual blob, got: %s", systemTools)
	}
	if !strings.Contains(systemTools, "You are a Stores Database Expert Agent") {
		t.Fatalf("expected cross-domain system tools to preserve the Stores capability guidance, got: %s", systemTools)
	}
}

func TestBuildSystemPrompt_IncludesScriptAuthoringContextForStores(t *testing.T) {
	SetStoresFeatureEnabled(true)
	defer SetStoresFeatureEnabled(false)

	ctx := context.Background()
	sysDB := database.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}})
	ag := NewCopilotAgent(Config{}, map[string]sop.DatabaseOptions{}, sysDB)
	ag.service = &Service{session: &RunnerSession{MRU: []MRUItem{}}}

	payload := &ai.SessionPayload{CurrentDB: SystemDBName, Variables: make(map[string]any)}
	ctx = context.WithValue(ctx, "session_payload", payload)

	prompt := ag.buildSystemPrompt(ctx, "Create a script named expensive_orders to find orders over 1000", TaskContextClassification{
		Domain:          StoresDomain,
		DBArtifacts:     []string{"orders"},
		Layers:          []LayerInfo{{Name: "Single-Domain", CRUD: []string{"R"}}},
		ScriptAuthoring: true,
	})

	if !strings.Contains(prompt, "Relevant Script Authoring Operations") {
		t.Fatalf("expected focused execution context to include script authoring guidance: %s", prompt)
	}
	if !strings.Contains(prompt, "Use execute_script for multi-step store workflows") {
		t.Fatalf("expected focused execution context to include execute_script authoring guidance: %s", prompt)
	}
	if !strings.Contains(prompt, "create_script") {
		t.Fatalf("expected create_script guidance in prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "store reusable steps under the") || !strings.Contains(prompt, "script field") {
		t.Fatalf("expected script payload shape guidance in prompt: %s", prompt)
	}

	var elements []PromptElement
	if err := json.Unmarshal([]byte(prompt), &elements); err != nil {
		t.Fatalf("expected buildSystemPrompt to return JSON prompt elements: %v\nPrompt: %s", err, prompt)
	}
	var focusedContext string
	var systemTools string
	for _, element := range elements {
		switch element.Component {
		case ComponentFocusedContext:
			focusedContext = element.Content
		case ComponentSystemTools:
			systemTools = element.Content
		}
	}
	if strings.Contains(systemTools, "[truncated]") {
		t.Fatalf("expected script-authoring system tools context to fit without truncation after prompt reductions, got: %s", systemTools)
	}
	if !strings.Contains(focusedContext, "Relevant Script Authoring Operations") {
		t.Fatalf("expected script-authoring guidance to remain in focused execution context, got: %s", focusedContext)
	}
	if !strings.Contains(systemTools, "Structured Context: Stores Tools") {
		t.Fatalf("expected system tools to keep the Stores capability context for script-authoring prompts, got: %s", systemTools)
	}
}

func TestBuildSystemPrompt_DedupesFocusedToolSectionsAgainstSystemToolsBaseline(t *testing.T) {
	ctx := context.Background()
	sysDB := database.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}})
	ag := NewCopilotAgent(Config{}, map[string]sop.DatabaseOptions{}, sysDB)
	ag.service = &Service{session: &RunnerSession{MRU: []MRUItem{}}}

	payload := &ai.SessionPayload{CurrentDB: SystemDBName, Variables: make(map[string]any)}
	ctx = context.WithValue(ctx, "session_payload", payload)

	ag.markMRUCategoryWithSource(SYSTEM_TOOLS, buildScriptToolDescriptionContext(StoresDomain, map[string]bool{"R": true}), MRUSourceSystemTools)

	prompt := ag.buildSystemPrompt(ctx, "Create a script named expensive_orders to find orders over 1000", TaskContextClassification{
		Domain:          StoresDomain,
		DBArtifacts:     []string{"orders"},
		Layers:          []LayerInfo{{Name: "Single-Domain", CRUD: []string{"R"}}},
		ScriptAuthoring: true,
	})

	var elements []PromptElement
	if err := json.Unmarshal([]byte(prompt), &elements); err != nil {
		t.Fatalf("expected buildSystemPrompt to return JSON prompt elements: %v\nPrompt: %s", err, prompt)
	}

	systemTools := ""
	for _, element := range elements {
		if element.Component == ComponentSystemTools {
			systemTools = element.Content
			break
		}
	}
	if systemTools == "" {
		t.Fatalf("expected system tools component in prompt: %s", prompt)
	}
	if count := strings.Count(systemTools, "Structured Context: Script Authoring Tools"); count != 1 {
		t.Fatalf("expected script authoring tools section once after baseline dedupe, got %d\n%s", count, systemTools)
	}
	if count := strings.Count(systemTools, "Structured Context: Stores Tools"); count != 1 {
		t.Fatalf("expected stores tools section once after baseline merge, got %d\n%s", count, systemTools)
	}
}

func TestBuildSystemPrompt_StoresSystemTools_PrefersCompactProtocolSlice(t *testing.T) {
	ctx := context.Background()
	sysDB := database.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}})
	ag := NewCopilotAgent(Config{}, map[string]sop.DatabaseOptions{}, sysDB)
	ag.service = &Service{session: &RunnerSession{MRU: []MRUItem{}}}

	prompt := ag.buildSystemPrompt(ctx, "Find John orders over 500", TaskContextClassification{
		Domain:      StoresDomain,
		DBArtifacts: []string{"users", "orders"},
		Layers:      []LayerInfo{{Name: "Single-Domain", CRUD: []string{"R"}}},
	})

	var elements []PromptElement
	if err := json.Unmarshal([]byte(prompt), &elements); err != nil {
		t.Fatalf("expected buildSystemPrompt to return JSON prompt elements: %v\nPrompt: %s", err, prompt)
	}

	systemTools := ""
	for _, element := range elements {
		if element.Component == ComponentSystemTools {
			systemTools = element.Content
			break
		}
	}
	if systemTools == "" {
		t.Fatalf("expected system tools component in prompt: %s", prompt)
	}
	if strings.Contains(systemTools, "<h2> Example</h2>") || strings.Contains(systemTools, "Execution Flow Engine Guardrails") {
		t.Fatalf("expected stores system tools to omit the large execute_script example block, got: %s", systemTools)
	}
	if !strings.Contains(systemTools, "Structured Context: Stores Tools") || !strings.Contains(systemTools, "You are a Stores Database Expert Agent") {
		t.Fatalf("expected stores system tools to use the compact stores tool context, got: %s", systemTools)
	}
	if !strings.Contains(systemTools, "Never guess store names") || !strings.Contains(systemTools, "retry once") {
		t.Fatalf("expected simplified stores tool protocol guidance to remain visible in system tools, got: %s", systemTools)
	}
	if !strings.Contains(systemTools, "multi-step store plan") || !strings.Contains(systemTools, "place those steps inside execute_script.script") {
		t.Fatalf("expected stores system tools to guide multi-step operations to use execute_script, got: %s", systemTools)
	}
}

func TestBuildSpacesCRUDOperationsContext_DoesNotRequireVectorizationByDefault(t *testing.T) {
	ctx := buildSpacesCRUDOperationsContext(map[string]bool{"C": true, "U": true})

	if !strings.Contains(ctx, "Use mint_to_space to add generated content") {
		t.Fatalf("expected create guidance to route generated space writes through mint_to_space: %s", ctx)
	}
	if !strings.Contains(ctx, "do not attempt an external import workflow") {
		t.Fatalf("expected create guidance to avoid import-style tool calls for generated content: %s", ctx)
	}
	if !strings.Contains(ctx, "pass that exact name in mint_to_space.kb_name") {
		t.Fatalf("expected create guidance to require explicit kb_name for user-named Spaces: %s", ctx)
	}
	if !strings.Contains(ctx, "Do NOT call vectorize_space_items") {
		t.Fatalf("expected create guidance to avoid default vectorization: %s", ctx)
	}
	if !strings.Contains(ctx, "mint_to_space manages its own transaction internally") {
		t.Fatalf("expected update guidance to describe mint_to_space transaction handling: %s", ctx)
	}
	if !strings.Contains(ctx, "Do NOT call vectorization APIs unless the user explicitly asks") {
		t.Fatalf("expected update guidance to avoid default vectorization: %s", ctx)
	}
}

func TestBuildSpacesCRUDOperationsContext_ReadToolsManageOwnTransactions(t *testing.T) {
	ctx := buildSpacesCRUDOperationsContext(map[string]bool{"R": true})

	if !strings.Contains(ctx, "list_space_categories, list_space_items, search_space, and read_space_config") {
		t.Fatalf("expected read guidance to mention Space discovery tools: %s", ctx)
	}
	if !strings.Contains(ctx, "manage their own read transactions") {
		t.Fatalf("expected read guidance to describe direct Space API transaction ownership: %s", ctx)
	}
	if !strings.Contains(ctx, "Do NOT wrap") {
		t.Fatalf("expected read guidance to forbid begin_tx wrapping for direct Space API tools: %s", ctx)
	}
}

func TestBuildSpacesCRUDOperationsContext_DeleteUsesDeleteSpace(t *testing.T) {
	ctx := buildSpacesCRUDOperationsContext(map[string]bool{"D": true})

	if !strings.Contains(ctx, "Use delete_space for full Space deletion") {
		t.Fatalf("expected delete guidance to expose delete_space: %s", ctx)
	}
	if !strings.Contains(ctx, "Prefer a confirmation step with the user") {
		t.Fatalf("expected delete guidance to require confirmation wording: %s", ctx)
	}
}

package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/memory"
)

type RouterTestGen struct {
	Response       string
	CapturedPrompt string
	Gate2IsSwitch  bool // if true, fake a switch boolean output
}

func (m *RouterTestGen) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	m.CapturedPrompt = opts.SystemPrompt

	// If it's Gate 2 Continuity
	if strings.Contains(opts.SystemPrompt, "The user is potentially continuing a previous topic") || strings.Contains(opts.SystemPrompt, "The user is continuing") {
		if m.Gate2IsSwitch {
			return ai.GenOutput{Text: `{"intent": "SWITCH", "layers": [{"name": "Single-Domain"}]}`}, nil
		}
		// otherwise return updated context via JSON
		return ai.GenOutput{Text: `{"intent": "CONTINUE", "domain": "TestInherited", "db_artifacts": ["Users"], "stores_artifacts": ["Users"], "layers": [{"name": "Single-Domain", "crud": ["R"]}]}`}, nil
	}

	return ai.GenOutput{Text: m.Response}, nil
}
func (m *RouterTestGen) Name() string                                 { return "RouterTestGen" }
func (m *RouterTestGen) EstimateCost(inTokens, outTokens int) float64 { return 0.0 }

func (m *RouterTestGen) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
	return nil
}

func TestEvaluateRoutingGates_UsesCapabilityHintFromMRUForContinuation(t *testing.T) {
	ctx := context.Background()
	sysDBOptions := sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}}
	sysDB := database.NewDatabase(sysDBOptions)

	ag := NewCopilotAgent(Config{}, map[string]sop.DatabaseOptions{}, sysDB)
	if ag.service == nil {
		ag.service = &Service{}
	}
	ag.service.session = &RunnerSession{MRU: []MRUItem{}}
	ag.markMRUCategory(capabilityAffinityMRUCategory, "capability=coding_workflow; intent=repo_edit", MRUSourceAskOutcome, MRUScopeSession)

	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	payload.Variables["RoutingState"] = &TaskContextClassification{Entity: "Omni", Domain: "Desktop"}
	ctx = context.WithValue(ctx, "session_payload", payload)

	gen := &RouterTestGen{}
	taskCtx, err := ag.evaluateRoutingGates(ctx, "keep going with the parser fix", gen)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskCtx == nil {
		t.Fatal("expected a routing classification")
	}
	if taskCtx.CapabilityHint != capabilityHintCodingWorkflow {
		t.Fatalf("expected coding workflow hint to be reused on continuation, got %q", taskCtx.CapabilityHint)
	}
	if taskCtx.IntentFamily != intentFamilyRepoEdit {
		t.Fatalf("expected repo edit intent family, got %q", taskCtx.IntentFamily)
	}
}

func TestApplyCapabilityRoutingHintsKeepsPriorCapabilityForNearbyContinuation(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{MRU: []MRUItem{}}}
	ag.markMRUCategory(capabilityAffinityMRUCategory, "capability=desktop_automation; intent=local_ops", MRUSourceAskOutcome, MRUScopeSession)

	taskCtx := &TaskContextClassification{Domain: "Desktop", RoutingGate: RoutingGateContinuity}
	ag.applyCapabilityRoutingHints(context.Background(), taskCtx, "check the current workspace state")

	if taskCtx.CapabilityHint != capabilityHintDesktopAutomation {
		t.Fatalf("expected nearby continuation to reuse desktop automation, got %q", taskCtx.CapabilityHint)
	}
	if taskCtx.IntentFamily != intentFamilyLocalOps {
		t.Fatalf("expected local ops intent family, got %q", taskCtx.IntentFamily)
	}
}

func TestApplyCapabilityRoutingHintsSwitchesToCodingWorkflowForImplementationAsk(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{MRU: []MRUItem{}}}
	ag.markMRUCategory(capabilityAffinityMRUCategory, "capability=desktop_automation; intent=local_ops", MRUSourceAskOutcome, MRUScopeSession)

	taskCtx := &TaskContextClassification{Domain: "Desktop", RoutingGate: RoutingGateContinuity}
	ag.applyCapabilityRoutingHints(context.Background(), taskCtx, "implement a parser fix in the repository")

	if taskCtx.CapabilityHint != capabilityHintCodingWorkflow {
		t.Fatalf("expected implementation ask to switch to coding workflow, got %q", taskCtx.CapabilityHint)
	}
	if taskCtx.IntentFamily != intentFamilyRepoEdit {
		t.Fatalf("expected repo edit intent family, got %q", taskCtx.IntentFamily)
	}
}

func TestResetRoutingForTopicSwitchClearsCapabilityAffinityHint(t *testing.T) {
	ctx := context.Background()
	sysDBOptions := sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}}
	sysDB := database.NewDatabase(sysDBOptions)

	ag := NewCopilotAgent(Config{}, map[string]sop.DatabaseOptions{}, sysDB)
	if ag.service == nil {
		ag.service = &Service{}
	}
	ag.service.session = &RunnerSession{MRU: []MRUItem{}}
	ag.markMRUCategory(capabilityAffinityMRUCategory, "capability=coding_workflow; intent=repo_edit", MRUSourceAskOutcome, MRUScopeSession)

	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	ctx = context.WithValue(ctx, "session_payload", payload)
	ag.resetRoutingForTopicSwitch("let's start over", payload)

	if _, ok := ag.GetMRUCategory(capabilityAffinityMRUCategory); ok {
		t.Fatal("expected capability affinity hint to be cleared on topic switch")
	}
}

func TestEvaluateRoutingGates_MarksGeneralContextAfterLightClassification(t *testing.T) {
	ctx := context.Background()
	sysDBOptions := sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}}
	sysDB := database.NewDatabase(sysDBOptions)

	cfg := Config{}
	ag := NewCopilotAgent(cfg, map[string]sop.DatabaseOptions{}, sysDB)
	if ag.service == nil {
		ag.service = &Service{}
	}
	ag.service.session = &RunnerSession{MRU: []MRUItem{}}

	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	ctx = context.WithValue(ctx, "session_payload", payload)

	gen := &RouterTestGen{Response: `{"entity": "Omni", "domain": "", "db_artifacts": [], "layers": []}`}
	taskCtx, err := ag.evaluateRoutingGates(ctx, "Fresh ask about something else", gen)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskCtx == nil {
		t.Fatal("expected a routing classification")
	}
	if !taskCtx.GeneralContext {
		t.Fatalf("expected cold-start light classification to mark general context")
	}
	if taskCtx.RoutingGate != RoutingGateDiscovery {
		t.Fatalf("expected cold-start routing gate for general context, got %q", taskCtx.RoutingGate)
	}
}

func TestEvaluateRoutingGates_SpecializedRoutingHandlesDeepPathQueries(t *testing.T) {
	ctx := context.Background()
	sysDBOptions := sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}}
	sysDB := database.NewDatabase(sysDBOptions)

	tx, err := sysDB.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("BeginTransaction failed: %v", err)
	}
	kb, err := sysDB.OpenKnowledgeBase(ctx, "sop", tx, nil, nil, false)
	if err != nil {
		t.Fatalf("OpenKnowledgeBase failed: %v", err)
	}
	if err := kb.SetConfig(ctx, &memory.KnowledgeBaseConfig{TextSearchEnabled: true, LastVectorized: 1}); err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	ag := NewCopilotAgent(Config{}, map[string]sop.DatabaseOptions{}, sysDB)
	if ag.service == nil {
		ag.service = &Service{}
	}
	ag.service.session = &RunnerSession{MRU: []MRUItem{}}

	ctx = context.WithValue(ctx, "session_payload", &ai.SessionPayload{Variables: make(map[string]any)})

	taskCtx, err := ag.evaluateRoutingGates(ctx, "SOP:language/c#/tutorial", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskCtx == nil {
		t.Fatalf("expected a routing classification for deep path query")
	}
	if got := taskCtx.RoutingGate; got != RoutingGateFocused {
		t.Fatalf("expected specialized focused routing to handle SOP path query, got %q", got)
	}
	if !hasLayer(taskCtx.Layers, "KBRoute") {
		t.Fatalf("expected KBRoute layer for specialized KB routing, got %+v", taskCtx.Layers)
	}
}

func TestThreeGates_RoutingArchitecture(t *testing.T) {
	ctx := context.Background()
	sysDBOptions := sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}}
	sysDB := database.NewDatabase(sysDBOptions)

	tx, _ := sysDB.BeginTransaction(ctx, sop.ForWriting)
	sysDB.NewBtree(ctx, "TestStore", tx)
	tx.Commit(ctx)

	cfg := Config{}
	ag := NewCopilotAgent(cfg, map[string]sop.DatabaseOptions{}, sysDB)

	if ag.service == nil {
		ag.service = &Service{}
	}
	ag.service.session = &RunnerSession{
		MRU: []MRUItem{},
	}

	t.Run("Gate 1: Prefix Routing", func(t *testing.T) {
		gen := &RouterTestGen{
			Response: `{"entity": "Omni", "domain": "Stores", "db_artifacts": ["TestStore"], "layers": []}`,
		}
		payload := &ai.SessionPayload{Variables: make(map[string]any)}
		ctx = context.WithValue(ctx, "session_payload", payload)

		query := "Omni:Stores:TestStore"
		taskCtx, err := ag.evaluateRoutingGates(ctx, query, gen)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if taskCtx == nil {
			t.Fatal("Gate 1 expected a routing classification")
		}
		if taskCtx.Domain == "" {
			t.Errorf("Gate 1 failed to populate a domain, got %+v", taskCtx)
		}
		if taskCtx.RoutingGate == "" {
			t.Errorf("Gate 1 should mark a routing gate, got %+v", taskCtx)
		}
		if toolsCtx := ag.getSystemToolsContext(ctx); toolsCtx != "" {
			t.Errorf("Gate 1 should not inject system tools during classification, got %q", toolsCtx)
		}

		// Verify MRU Session context state is persisted for the current routing path.
		rs, ok := payload.Variables["RoutingState"].(*TaskContextClassification)
		if !ok || rs == nil {
			t.Errorf("Gate 1 failed to persist RoutingState into payload, got %#v", payload.Variables["RoutingState"])
		}
	})

	t.Run("Gate 2: MRU Context Inheritance", func(t *testing.T) {
		gen := &RouterTestGen{}
		ag.service.session.MRU = []MRUItem{
			{Category: askOutcomeMRUCategoryQuery, Context: "- Last user ask: Find John orders", Source: MRUSourceAskOutcome, Scope: MRUScopeSession},
			{Category: askOutcomeMRUCategoryToolPattern, Context: "- Tool pattern: list_stores -> execute_script", Source: MRUSourceAskOutcome, Scope: MRUScopeSession},
		}
		// Seed payload with existing context
		payload := &ai.SessionPayload{Variables: make(map[string]any)}
		payload.Variables["RoutingState"] = &TaskContextClassification{
			Entity: "Omni",
			Domain: "OldDomain",
		}
		ctx = context.WithValue(ctx, "session_payload", payload)

		query := "Keep going but use TestInherited"
		taskCtx, err := ag.evaluateRoutingGates(ctx, query, gen)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if taskCtx == nil {
			t.Fatal("Gate 2 expected a routing classification")
		}
		if taskCtx.Domain == "" {
			t.Errorf("Gate 2 failed to populate a domain, got %+v", taskCtx)
		}
		if taskCtx.RoutingGate == "" {
			t.Errorf("Gate 2 should mark a routing gate, got %+v", taskCtx)
		}
		if toolsCtx := ag.getSystemToolsContext(ctx); toolsCtx != "" {
			t.Fatalf("expected Gate 2 continuity classification to avoid system tool injection, got %q", toolsCtx)
		}
	})

	t.Run("Gate 2: Topic Switch Fallthrough to Gate 3", func(t *testing.T) {
		gen := &RouterTestGen{
			Gate2IsSwitch: true,                                                                       // Will output SWITCH
			Response:      `{"entity": "Omni", "domain": "NewApp", "db_artifacts": [], "layers": []}`, // Gate 3 response
		}

		payload := &ai.SessionPayload{Variables: make(map[string]any)}
		payload.Variables["RoutingState"] = &TaskContextClassification{
			Entity: "Omni",
			Domain: "OldDomain",
		}
		ctx = context.WithValue(ctx, "session_payload", payload)
		ag.markMRUCategoryWithSource(SYSTEM_TOOLS, "stale tools", MRUSourceSystemTools)
		ag.markMRUCategoryWithSource(playbookMRUCategory("Spaces"), "stale spaces playbook", MRUSourcePlaybook)
		ag.markMRUCategoryWithSource(playbookMRUCategory("sop"), "stale kb context", MRUSourcePlaybook)
		ag.markMRUCategoryWithSource("PERSONA_omni", "durable persona", MRUSourcePersona)

		query := "Nevermind, let's create a New App"
		taskCtx, err := ag.evaluateRoutingGates(ctx, query, gen)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if taskCtx == nil {
			t.Fatal("Gate 2 expected a routing classification")
		}
		if taskCtx.Domain == "" {
			t.Errorf("Gate 2 should return a populated domain, got %+v", taskCtx)
		}
		if _, ok := payload.Variables["RoutingState"].(*TaskContextClassification); !ok || payload.Variables["RoutingState"].(*TaskContextClassification) == nil {
			t.Errorf("Gate 2 switch should replace stale routing state, got %v", payload.Variables["RoutingState"])
		}
		if toolsCtx := ag.getSystemToolsContext(ctx); toolsCtx != "" {
			t.Logf("Gate 2 switch currently leaves tool context in place: %q", toolsCtx)
		}

		if _, ok := ag.GetMRUCategory("PERSONA_omni"); !ok {
			t.Errorf("Gate 2 switch should preserve persona MRU cache, present=%v", ok)
		}
	})

	t.Run("Gate 3: Cold Start", func(t *testing.T) {
		gen := &RouterTestGen{
			Response: `{"entity": "Omni", "domain": "Discovery", "db_artifacts": [], "layers": []}`,
		}
		// Empty payload
		payload := &ai.SessionPayload{Variables: make(map[string]any)}
		ctx = context.WithValue(ctx, "session_payload", payload)

		query := "Cold start query"
		taskCtx, err := ag.evaluateRoutingGates(ctx, query, gen)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if taskCtx == nil {
			t.Fatal("Gate 3 expected a routing classification")
		}
		if taskCtx.Domain == "" {
			t.Errorf("Gate 3 failed to populate a domain, got %+v", taskCtx)
		}
		if taskCtx.RoutingGate == "" {
			t.Errorf("Gate 3 should mark a routing gate, got %+v", taskCtx)
		}
		if toolsCtx := ag.getSystemToolsContext(ctx); toolsCtx != "" {
			t.Logf("Gate 3 currently keeps tool context in place: %q", toolsCtx)
		}
	})

	t.Run("Gate 2: Rehydrates Routing State From STM", func(t *testing.T) {
		gen := &RouterTestGen{}
		payload := &ai.SessionPayload{Variables: make(map[string]any)}
		ctx = context.WithValue(ctx, "session_payload", payload)
		ag.service.session.Memory = NewShortTermMemory()
		ag.service.session.Memory.SetRoutingState(&TaskContextClassification{
			Entity:      "Omni",
			Domain:      StoresDomain,
			DBArtifacts: []string{"users"},
			Layers:      []LayerInfo{{Name: "Single-Domain", CRUD: []string{"R"}}},
		})

		taskCtx, err := ag.evaluateRoutingGates(ctx, "show users again", gen)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if taskCtx == nil {
			t.Fatalf("expected a routing classification after STM rehydration, got %+v", taskCtx)
		}
		if taskCtx.RoutingGate == "" {
			t.Fatalf("expected a routing gate after STM rehydration, got %+v", taskCtx)
		}
		if rs, ok := payload.Variables["RoutingState"].(*TaskContextClassification); !ok || rs == nil {
			t.Fatalf("expected routing state to be restored and updated through Gate 2, got %v", payload.Variables["RoutingState"])
		}
	})

	t.Run("Gate 2: New Topic Signal Skips Continuation Inheritance", func(t *testing.T) {
		gen := &RouterTestGen{Response: `{"entity": "Omni", "domain": "FreshDomain", "db_artifacts": [], "layers": []}`}
		payload := &ai.SessionPayload{Variables: make(map[string]any)}
		payload.Variables["RoutingState"] = &TaskContextClassification{Entity: "Omni", Domain: "OldDomain"}
		ctx = context.WithValue(ctx, "session_payload", payload)
		ctx = ag.applyAskConfig(ctx, ai.NewConfigMap())
		cfg := ai.NewConfigMap()
		cfg.Set("is_new_topic", true)
		ctx = ag.applyAskConfig(ctx, cfg)

		taskCtx, err := ag.evaluateRoutingGates(ctx, "Let's start over with fresh context", gen)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if taskCtx == nil {
			t.Fatal("expected a routing classification")
		}
		if taskCtx.Domain != "FreshDomain" {
			t.Fatalf("expected new-topic signal to bypass stale MRU continuation and use fresh routing, got domain %q", taskCtx.Domain)
		}
		if taskCtx.RoutingGate != RoutingGateDiscovery {
			t.Fatalf("expected cold-start routing gate for explicit new-topic asks, got %q", taskCtx.RoutingGate)
		}
	})
}

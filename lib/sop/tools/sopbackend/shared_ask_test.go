package sopbackend

import (
	"context"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
)

type stubSharedAskAgent struct {
	queries []string
}

func (a *stubSharedAskAgent) Open(ctx context.Context) error  { return nil }
func (a *stubSharedAskAgent) Close(ctx context.Context) error { return nil }
func (a *stubSharedAskAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}
func (a *stubSharedAskAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	a.queries = append(a.queries, query)
	return "shared-list-tools", nil
}
func (a *stubSharedAskAgent) Execute(ctx context.Context, toolName string, args map[string]any) (string, error) {
	return "", nil
}
func (a *stubSharedAskAgent) ListTools(ctx context.Context) ([]ai.ToolDefinition, error) {
	return nil, nil
}

func TestAskSharedAgentRoutesListToolsThroughSharedBackendAgent(t *testing.T) {
	prevLoadedAgents := loadedAgents
	prevSessions := activeSessions
	stub := &stubSharedAskAgent{}
	loadedAgents = map[string]ai.Agent[map[string]any]{"copilot": stub}
	activeSessions = NewSessionManager(10)
	defer func() {
		loadedAgents = prevLoadedAgents
		activeSessions = prevSessions
	}()

	out, err := AskSharedAgent(context.Background(), "/list_tools", nil)
	if err != nil {
		t.Fatalf("AskSharedAgent() error = %v", err)
	}
	if out != "shared-list-tools" {
		t.Fatalf("AskSharedAgent() = %q, want %q", out, "shared-list-tools")
	}
	if len(stub.queries) != 1 || stub.queries[0] != "/list_tools" {
		t.Fatalf("AskSharedAgent() forwarded %v, want [/list_tools]", stub.queries)
	}
}

type stubParallelSharedAskAgent struct {
	stubSharedAskAgent
	parallelQueries []string
}

func (a *stubParallelSharedAskAgent) AskParallel(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	a.parallelQueries = append(a.parallelQueries, query)
	return "shared-parallel-result", nil
}

func TestAskSharedAgentUsesAskParallelForParallelizableQueries(t *testing.T) {
	prevLoadedAgents := loadedAgents
	prevSessions := activeSessions
	stub := &stubParallelSharedAskAgent{}
	loadedAgents = map[string]ai.Agent[map[string]any]{"copilot": stub}
	activeSessions = NewSessionManager(10)
	defer func() {
		loadedAgents = prevLoadedAgents
		activeSessions = prevSessions
	}()

	out, err := AskSharedAgent(context.Background(), "run tests and check formatting", nil)
	if err != nil {
		t.Fatalf("AskSharedAgent() error = %v", err)
	}
	if out != "shared-parallel-result" {
		t.Fatalf("AskSharedAgent() = %q, want %q", out, "shared-parallel-result")
	}
	if len(stub.parallelQueries) != 1 || stub.parallelQueries[0] != "run tests and check formatting" {
		t.Fatalf("AskSharedAgent() parallel forwarded %v, want [run tests and check formatting]", stub.parallelQueries)
	}
}

func TestAskSharedAgentSkipsUserLookupWhenAuthDisabled(t *testing.T) {
	prevLoadedAgents := loadedAgents
	prevSessions := activeSessions
	prevDisableAuth := config.DisableAuth
	config.DisableAuth = true
	defer func() {
		loadedAgents = prevLoadedAgents
		activeSessions = prevSessions
		config.DisableAuth = prevDisableAuth
	}()

	stub := &stubSharedAskAgent{}
	loadedAgents = map[string]ai.Agent[map[string]any]{"copilot": stub}
	activeSessions = NewSessionManager(10)

	ctx := sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "ghost-user"})
	out, err := AskSharedAgent(ctx, "/c ls", nil)
	if err != nil {
		t.Fatalf("AskSharedAgent() error = %v", err)
	}
	if out != "shared-list-tools" {
		t.Fatalf("AskSharedAgent() = %q, want %q", out, "shared-list-tools")
	}
	if len(stub.queries) != 1 || stub.queries[0] != "/c ls" {
		t.Fatalf("AskSharedAgent() forwarded %v, want [/c ls]", stub.queries)
	}
}

type cloneTrackingSharedAskAgent struct {
	queries         []string
	cloneCount      *int
	blueprintCalls  *int
	instanceLabel   string
	childAskCounter *int
}

func (a *cloneTrackingSharedAskAgent) Open(ctx context.Context) error  { return nil }
func (a *cloneTrackingSharedAskAgent) Close(ctx context.Context) error { return nil }
func (a *cloneTrackingSharedAskAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}
func (a *cloneTrackingSharedAskAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	if a.instanceLabel == "blueprint" && a.blueprintCalls != nil {
		*a.blueprintCalls++
	}
	if a.childAskCounter != nil {
		*a.childAskCounter++
	}
	a.queries = append(a.queries, query)
	return a.instanceLabel, nil
}
func (a *cloneTrackingSharedAskAgent) Execute(ctx context.Context, toolName string, args map[string]any) (string, error) {
	return "", nil
}
func (a *cloneTrackingSharedAskAgent) ListTools(ctx context.Context) ([]ai.ToolDefinition, error) {
	return nil, nil
}
func (a *cloneTrackingSharedAskAgent) Clone() ai.Agent[map[string]any] {
	if a.cloneCount != nil {
		*a.cloneCount++
	}
	return &cloneTrackingSharedAskAgent{
		cloneCount:      a.cloneCount,
		blueprintCalls:  a.blueprintCalls,
		instanceLabel:   "cloned",
		childAskCounter: a.childAskCounter,
	}
}

func TestAskSharedAgentUsesSessionCloneInsteadOfBlueprint(t *testing.T) {
	prevLoadedAgents := loadedAgents
	prevSessions := activeSessions
	defer func() {
		loadedAgents = prevLoadedAgents
		activeSessions = prevSessions
	}()

	cloneCount := 0
	blueprintCalls := 0
	childAskCount := 0
	activeSessions = NewSessionManager(10)
	loadedAgents = map[string]ai.Agent[map[string]any]{
		"copilot": &cloneTrackingSharedAskAgent{
			cloneCount:      &cloneCount,
			blueprintCalls:  &blueprintCalls,
			instanceLabel:   "blueprint",
			childAskCounter: &childAskCount,
		},
	}

	cfg := ai.NewConfigMap()
	cfg.Set("payload", &ai.SessionPayload{SessionID: "session-1", UserID: "alice"})

	first, err := AskSharedAgent(context.Background(), "first", cfg)
	if err != nil {
		t.Fatalf("first AskSharedAgent() error = %v", err)
	}
	second, err := AskSharedAgent(context.Background(), "second", cfg)
	if err != nil {
		t.Fatalf("second AskSharedAgent() error = %v", err)
	}

	if first != "cloned" || second != "cloned" {
		t.Fatalf("AskSharedAgent() returned %q and %q, want cloned instances", first, second)
	}
	if cloneCount != 1 {
		t.Fatalf("clone count = %d, want 1 for session reuse", cloneCount)
	}
	if blueprintCalls != 0 {
		t.Fatalf("blueprint Ask calls = %d, want 0", blueprintCalls)
	}
	if childAskCount != 2 {
		t.Fatalf("cloned agent Ask count = %d, want 2", childAskCount)
	}
	if _, ok := activeSessions.Get("session-1"); !ok {
		t.Fatal("expected active session to be stored for session-1")
	}
	if _, ok := activeSessions.Get("user:alice"); ok {
		t.Fatal("did not expect fallback user session key when payload session_id is present")
	}
}

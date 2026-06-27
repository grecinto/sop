package sopbackend

import (
	"context"
	"testing"

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
	stub := &stubSharedAskAgent{}
	loadedAgents = map[string]ai.Agent[map[string]any]{"copilot": stub}
	defer func() { loadedAgents = prevLoadedAgents }()

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

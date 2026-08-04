package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
)

type askWithRequestQueueStubAgent struct {
	openCalls  int
	closeCalls int
	lastQuery  string
	lastCfg    *ai.ConfigMap
	response   string
}

func (a *askWithRequestQueueStubAgent) Open(ctx context.Context) error {
	a.openCalls++
	return nil
}

func (a *askWithRequestQueueStubAgent) Close(ctx context.Context) error {
	a.closeCalls++
	return nil
}

func (a *askWithRequestQueueStubAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}

func (a *askWithRequestQueueStubAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	a.lastQuery = query
	a.lastCfg = cfg
	return a.response, nil
}

// TestAskWithRequest_BasicFlow tests the new explicit parameter API
func TestAskWithRequest_BasicFlow(t *testing.T) {
	// This is a minimal smoke test to verify the new API compiles and runs
	// A real test would need proper setup with domain, database, generator, etc.

	// Skip for now as it requires full infrastructure setup
	t.Skip("Smoke test for new API - requires full infrastructure")

	// Example of how the new API would be used:
	req := AskRequest{
		Query: "test query",
		Session: &ai.SessionPayload{
			CurrentDB: "test_db",
		},
	}

	// This demonstrates the explicit parameter pattern
	_ = req
}

// TestAskRequest_AllFieldsInitialized demonstrates all available fields
func TestAskRequest_AllFieldsInitialized(t *testing.T) {
	ctx := context.Background()

	req := AskRequest{
		Query:          "Find all users",
		Session:        &ai.SessionPayload{CurrentDB: "main"},
		Executor:       nil, // Will be auto-initialized
		Generator:      nil, // Will use service default
		Database:       nil, // Will be derived from session
		Writer:         nil,
		EventStreamer:  nil,
		ProgressSink:   nil,
		ScriptRecorder: nil, // Will use service default
		DefaultFormat:  "json",
		Options:        ai.NewConfigMap(),
	}

	// Verify struct is properly initialized
	if req.Query != "Find all users" {
		t.Errorf("Expected query 'Find all users', got %q", req.Query)
	}

	if req.Session == nil {
		t.Error("Session should not be nil")
	}

	if req.Session.CurrentDB != "main" {
		t.Errorf("Expected CurrentDB 'main', got %q", req.Session.CurrentDB)
	}

	// Use ctx to avoid unused variable warning
	_ = ctx
}

// TestAskResponse_Structure verifies the response structure
func TestAskResponse_Structure(t *testing.T) {
	resp := AskResponse{
		FinalText:      "User: John Doe",
		UpdatedSession: &ai.SessionPayload{CurrentDB: "main"},
		CarryoverState: &ai.CarryoverState{
			Mode:     ai.CarryoverModeCompact,
			Provider: "gemini",
		},
		ToolCalls: []ai.ToolCall{
			{Name: "execute_script", Args: map[string]any{"script": []any{}}},
		},
		OutcomeFacts:   []string{"Found 1 user"},
		OutcomeRecipes: []ai.LearnedRecipe{},
	}

	// Verify response structure
	if resp.FinalText != "User: John Doe" {
		t.Errorf("Expected FinalText 'User: John Doe', got %q", resp.FinalText)
	}

	if resp.UpdatedSession == nil {
		t.Error("UpdatedSession should not be nil")
	}

	if resp.CarryoverState == nil {
		t.Error("CarryoverState should not be nil")
	}

	if len(resp.ToolCalls) != 1 {
		t.Errorf("Expected 1 tool call, got %d", len(resp.ToolCalls))
	}

	if len(resp.OutcomeFacts) != 1 {
		t.Errorf("Expected 1 outcome fact, got %d", len(resp.OutcomeFacts))
	}
}

func TestServiceAskWithRequest_UsesCopilotRegistryAgent(t *testing.T) {
	stub := &askWithRequestQueueStubAgent{response: "queued-result"}
	svc := &Service{
		registry: map[string]ai.Agent[map[string]any]{"copilot": stub},
		session:  NewRunnerSession(),
	}

	resp, err := svc.AskWithRequest(context.Background(), AskRequest{
		Query:         "complete the queued workflow",
		Session:       &ai.SessionPayload{SessionID: "sess-1"},
		ThinkingLevel: "high",
		DefaultFormat: "json",
		ProviderOverride: &ProviderDetails{
			Provider: "anthropic",
		},
	})
	if err != nil {
		t.Fatalf("AskWithRequest() error = %v", err)
	}
	if resp.FinalText != "queued-result" {
		t.Fatalf("AskWithRequest() = %q, want %q", resp.FinalText, "queued-result")
	}
	if stub.openCalls != 1 || stub.closeCalls != 1 {
		t.Fatalf("lifecycle calls = open:%d close:%d, want 1/1", stub.openCalls, stub.closeCalls)
	}
	if stub.lastQuery != "complete the queued workflow" {
		t.Fatalf("lastQuery = %q, want %q", stub.lastQuery, "complete the queued workflow")
	}
	if stub.lastCfg == nil {
		t.Fatal("expected config map to be forwarded")
	}
	if v, ok := stub.lastCfg.Get("thinking_level"); !ok || v != "high" {
		t.Fatalf("thinking_level = %#v, want %q", v, "high")
	}
	if v, ok := stub.lastCfg.Get("default_format"); !ok || v != "json" {
		t.Fatalf("default_format = %#v, want %q", v, "json")
	}
	if v, ok := stub.lastCfg.Get("provider_details"); !ok || v == nil {
		t.Fatalf("provider_details missing: %#v", v)
	}
	if resp.UpdatedSession == nil || resp.UpdatedSession.SessionID != "sess-1" {
		t.Fatalf("updated session = %#v, want session_id sess-1", resp.UpdatedSession)
	}
}

func TestServiceAskWithRequest_RejectsSOPGeneratedTraceID(t *testing.T) {
	svc := &Service{session: NewRunnerSession()}
	_, err := svc.AskWithRequest(context.Background(), AskRequest{
		Query:   "loop back",
		TraceID: ai.NewParticipantTraceID(),
	})
	if err == nil {
		t.Fatal("expected recursion guard error")
	}
	if !strings.Contains(err.Error(), "trace_id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

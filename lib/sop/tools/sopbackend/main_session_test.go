package sopbackend

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent"
)

// mockAgent for testing cloning and session handling
type mockAgent struct {
	id string
}

func (m *mockAgent) Open(ctx context.Context) error {
	return nil
}
func (m *mockAgent) Close(ctx context.Context) error {
	return nil
}
func (m *mockAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}
func (m *mockAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	return "Mock Response from " + m.id, nil
}

func (m *mockAgent) Clone() ai.Agent[map[string]any] {
	return &mockAgent{id: "cloned"}
}

func TestSessionIDHandling(t *testing.T) {
	// Setup
	activeSessions = NewSessionManager(100)

	loadedAgents = map[string]ai.Agent[map[string]any]{
		"default": &mockAgent{id: "blueprint"},
	}

	// Request 1: No SessionID -> Expect a new Session ID to be generated
	reqBody := `{"message": "Hello", "database": "", "store": "", "agent": "default"}`
	req := httptest.NewRequest("POST", "/ai/chat", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handleAIChat(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("Expected status OK, got %v", res.Status)
	}

	body := w.Body.String()
	if strings.Contains(body, `"type":"session_id"`) || strings.Contains(body, `"event":"session_id"`) {
		t.Fatalf("expected session_id to remain internal to the backend stream, got: %s", body)
	}

	activeSessions.mu.Lock()
	if len(activeSessions.lookup) != 1 {
		activeSessions.mu.Unlock()
		t.Fatalf("expected exactly 1 active session, got %d", len(activeSessions.lookup))
	}
	var sessionID string
	for id := range activeSessions.lookup {
		sessionID = id
		break
	}
	activeSessions.mu.Unlock()

	if sessionID == "" {
		t.Fatal("Failed to resolve generated session_id from session manager")
	}

	// Verify the session agent was stored

	sessionAgent, exists := activeSessions.Get(sessionID)

	if !exists {
		t.Fatal("Expected agent to be stored in activeSessions map, but it was missing")
	}

	// Verify it was cloned
	if sa, ok := sessionAgent.(*mockAgent); !ok || sa.id != "cloned" {
		t.Fatalf("Expected agent to be 'cloned', got %v", sessionAgent)
	}

	// Request 2: Use the existing session ID
	reqBody2 := `{"message": "Hello again", "database": "", "store": "", "agent": "default", "session_id": "` + sessionID + `"}`
	req2 := httptest.NewRequest("POST", "/ai/chat", bytes.NewBufferString(reqBody2))
	req2.Header.Set("Content-Type", "application/json")

	w2 := httptest.NewRecorder()
	handleAIChat(w2, req2)

	// Verify the map still contains exactly 1 item

	activeSessions.mu.Lock()
	count := len(activeSessions.lookup)
	activeSessions.mu.Unlock()

	if count != 1 {
		t.Fatalf("Expected strictly 1 session, but activeSessions grew to %d", count)
	}
}

func TestSessionIDConcurrency(t *testing.T) {
	// Setup
	activeSessions = NewSessionManager(100)

	loadedAgents = map[string]ai.Agent[map[string]any]{
		"default": &mockAgent{id: "blueprint"},
	}

	var wg sync.WaitGroup
	numRequests := 50

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reqBody := `{"message": "Hello", "database": "", "store": "", "agent": "default"}`
			req := httptest.NewRequest("POST", "/ai/chat", bytes.NewBufferString(reqBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handleAIChat(w, req)

			if w.Result().StatusCode != http.StatusOK {
				t.Errorf("Unexpected status code!")
			}
		}()
	}

	wg.Wait()

	activeSessions.mu.Lock()
	count := len(activeSessions.lookup)
	activeSessions.mu.Unlock()

	if count != numRequests {
		t.Fatalf("Expected %d active sessions after concurrent requests, got %d", numRequests, count)
	}
}

// payloadMockAgent for testing SessionPayload extraction
type payloadMockAgent struct {
	id          string
	capturedCfg *ai.ConfigMap
}

func (m *payloadMockAgent) Open(ctx context.Context) error  { return nil }
func (m *payloadMockAgent) Close(ctx context.Context) error { return nil }
func (m *payloadMockAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}
func (m *payloadMockAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	m.capturedCfg = cfg
	return "Payload Mock Response", nil
}
func (m *payloadMockAgent) Clone() ai.Agent[map[string]any] {
	return &payloadMockAgent{id: "cloned"}
}

func TestSessionPayloadPropagation(t *testing.T) {
	// Setup
	activeSessions = NewSessionManager(100)
	mock := &payloadMockAgent{id: "blueprint"}
	loadedAgents = map[string]ai.Agent[map[string]any]{
		"payload_tester": mock,
	}

	// Test passing SelectedKBs and Domain
	reqBody := `{"message": "Hello", "domain": "Finance", "selected_kbs": [{"name": "sys:SOP"}, {"name": "curr:Finance"}], "agent": "payload_tester"}`
	req := httptest.NewRequest("POST", "/ai/chat", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handleAIChat(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("Expected status OK, got %v", res.Status)
	}

	body := w.Body.String()
	if strings.Contains(body, `"type":"session_id"`) || strings.Contains(body, `"event":"session_id"`) {
		t.Fatalf("expected session_id to remain internal to the backend stream, got: %s", body)
	}

	activeSessions.mu.Lock()
	if len(activeSessions.lookup) != 1 {
		activeSessions.mu.Unlock()
		t.Fatalf("expected exactly 1 active session, got %d", len(activeSessions.lookup))
	}
	var sessionID string
	for id := range activeSessions.lookup {
		sessionID = id
		break
	}
	activeSessions.mu.Unlock()

	if sessionID == "" {
		t.Fatal("Failed to resolve session_id from session manager")
	}

	agentRaw, exists := activeSessions.Get(sessionID)
	if !exists {
		t.Fatal("Agent not found in session manager")
	}

	pmAgent, ok := agentRaw.(*payloadMockAgent)
	if !ok {
		t.Fatal("Agent is not a payloadMockAgent")
	}

	// Check if captured config has the SessionPayload
	var payload *ai.SessionPayload
	if pmAgent.capturedCfg != nil {
		if p, ok := pmAgent.capturedCfg.Get("payload"); ok {
			if sp, ok := p.(*ai.SessionPayload); ok {
				payload = sp
			}
		}
	}

	if payload == nil {
		t.Fatal("Expected SessionPayload to be populated in options")
	}
	if payload.ActiveDomain != "Finance" {
		t.Errorf("Expected ActiveDomain 'Finance', got '%s'", payload.ActiveDomain)
	}
	if len(payload.SelectedKBs) != 2 {
		t.Fatalf("Expected 2 SelectedKBs, got %d", len(payload.SelectedKBs))
	}
	if payload.SelectedKBs[0].Name != "sys:SOP" || payload.SelectedKBs[1].Name != "curr:Finance" {
		t.Errorf("SelectedKBs mismatch: %v", payload.SelectedKBs)
	}
}

type eventStreamingMockAgent struct{}

func (m *eventStreamingMockAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	if streamer, ok := ctx.Value(ai.CtxKeyEventStreamer).(func(string, any)); ok && streamer != nil {
		streamer("tool_call", map[string]any{
			"tool": "mint_to_space",
			"args": map[string]any{"kb_name": "Tasks2"},
		})
		streamer("tool_result", map[string]any{
			"tool":   "mint_to_space",
			"args":   map[string]any{"kb_name": "Tasks2"},
			"result": "ok",
		})
	}
	return "Plain final answer.", nil
}

func (m *eventStreamingMockAgent) Open(ctx context.Context) error  { return nil }
func (m *eventStreamingMockAgent) Close(ctx context.Context) error { return nil }
func (m *eventStreamingMockAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}
func (m *eventStreamingMockAgent) Clone() ai.Agent[map[string]any] { return &eventStreamingMockAgent{} }

type streamerCaptureAgent struct {
	capturedCtx context.Context
}

func (m *streamerCaptureAgent) Open(ctx context.Context) error  { return nil }
func (m *streamerCaptureAgent) Close(ctx context.Context) error { return nil }
func (m *streamerCaptureAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}
func (m *streamerCaptureAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	m.capturedCtx = ctx
	return "ok", nil
}
func (m *streamerCaptureAgent) Clone() ai.Agent[map[string]any] {
	return m
}

func TestHandleAIChat_InjectsJSONStreamerIntoExecutionContext(t *testing.T) {
	activeSessions = NewSessionManager(100)
	mock := &streamerCaptureAgent{}
	loadedAgents = map[string]ai.Agent[map[string]any]{
		"streamer_tester": mock,
	}

	req := httptest.NewRequest("POST", "/ai/chat", bytes.NewBufferString(`{"message":"ping","agent":"streamer_tester"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleAIChat(w, req)

	if mock.capturedCtx == nil {
		t.Fatal("expected Ask to receive a context")
	}
	if _, ok := mock.capturedCtx.Value(agent.CtxKeyJSONStreamer).(*agent.JSONStreamer); !ok {
		t.Fatal("expected Ask context to contain a JSON streamer for record delivery")
	}
}

func TestSetupStream_EmitsVisibleContentForToolResults(t *testing.T) {
	w := httptest.NewRecorder()
	sendEvent, _ := SetupStream(w)

	sendEvent(ai.ReasoningEventToolResult, map[string]any{
		"tool":   "lookup_user",
		"result": "John is in the users store.",
	})

	body := w.Body.String()
	if !strings.Contains(body, `"type":"content"`) {
		t.Fatalf("expected content event for tool_result, got: %s", body)
	}
	if !strings.Contains(body, "Result from lookup_user") {
		t.Fatalf("expected human-readable tool result content, got: %s", body)
	}
}

func TestSetupStream_HandlesListStoresResultAsContent(t *testing.T) {
	w := httptest.NewRecorder()
	sendEvent, _ := SetupStream(w)

	sendEvent(ai.ReasoningEventToolResult, map[string]any{
		"tool":   "list_stores",
		"result": `[{"name":"alpha"}]`,
	})

	body := w.Body.String()
	if !strings.Contains(body, `"type":"content"`) {
		t.Fatalf("expected content event for list_stores result, got: %s", body)
	}
	if !strings.Contains(body, "list_stores") {
		t.Fatalf("expected list_stores result content in stream, got: %s", body)
	}
}

func TestSetupStream_SuppressesReducerNoticeContent(t *testing.T) {
	w := httptest.NewRecorder()
	sendEvent, _ := SetupStream(w)

	sendEvent(ai.ReasoningEventToolResult, map[string]any{
		"tool":   "select",
		"result": `[{"id":1,"_result_reducer_notice":"Results were 10 and were cutoff after first 4 rows. The UI already displayed these sample rows; do not repeat them in your answer."}]`,
	})

	body := w.Body.String()
	if strings.Contains(body, `"type":"content"`) {
		t.Fatalf("expected reducer-sample payload to suppress content rendering, got: %s", body)
	}
}

func TestSetupStream_MarksStructuredToolResultsAsStreamedContent(t *testing.T) {
	w := httptest.NewRecorder()
	sendEvent, hasStreamedContent := SetupStream(w)

	sendEvent(ai.ReasoningEventToolResult, map[string]any{
		"tool":   "execute_script",
		"result": `[{"name":"alpha"}]`,
	})

	if !hasStreamedContent() {
		t.Fatal("expected structured execute_script result to count as streamed UI output")
	}
	if strings.Contains(w.Body.String(), `"type":"record"`) {
		t.Fatal("expected execute_script tool_result to avoid re-emitting duplicate record events, got: " + w.Body.String())
	}
}

func TestSetupStream_MarksRecordEventsAsStreamedContent(t *testing.T) {
	w := httptest.NewRecorder()
	sendEvent, hasStreamedContent := SetupStream(w)

	sendEvent("record", map[string]any{"id": 1})

	if !hasStreamedContent() {
		t.Fatal("expected record events to count as streamed UI output")
	}
}

func TestSetupStream_MarksContentEventsAsStreamedContent(t *testing.T) {
	w := httptest.NewRecorder()
	sendEvent, hasStreamedContent := SetupStream(w)

	sendEvent("content", "list_tools inventory")

	if !hasStreamedContent() {
		t.Fatal("expected content events to count as streamed UI output")
	}
}

func TestSetupStream_SanitizesToolErrorMessagesBeforeRendering(t *testing.T) {
	w := httptest.NewRecorder()
	sendEvent, _ := SetupStream(w)

	sendEvent(ai.ReasoningEventToolError, map[string]any{
		"tool":  "list_directory",
		"error": "tool 'list_directory' not found in any registered agent for request 123e4567-e89b-12d3-a456-426614174000",
	})

	body := w.Body.String()
	if strings.Contains(body, "123e4567-e89b-12d3-a456-426614174000") {
		t.Fatalf("expected sanitized tool error payload, got: %s", body)
	}
	if !strings.Contains(body, "could not find the requested tool") {
		t.Fatalf("expected friendly tool error message in stream, got: %s", body)
	}
}

func TestSetupStream_UnescapesToolResultText(t *testing.T) {
	if got := renderVisibleText(`Link table: UserID -\u003e OrderID`); got != "Link table: UserID -> OrderID" {
		t.Fatalf("expected real arrow rendering for escaped unicode text, got: %q", got)
	}
}

func TestHandleAIChat_StreamsStructuredToolEvents(t *testing.T) {
	activeSessions = NewSessionManager(100)
	loadedAgents = map[string]ai.Agent[map[string]any]{
		"event_tester": &eventStreamingMockAgent{},
	}

	reqBody := `{"message": "Create tasks", "database": "", "agent": "event_tester"}`
	req := httptest.NewRequest("POST", "/ai/chat", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handleAIChat(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("Expected status OK, got %v", res.Status)
	}

	body := w.Body.String()
	// SetupStream now converts tool_call events to user-friendly content
	if !strings.Contains(body, "🔧 **Calling**") {
		t.Fatalf("expected formatted tool call in NDJSON stream, got: %s", body)
	}
	if !strings.Contains(body, `"type":"tool_result"`) {
		t.Fatalf("expected tool_result event in NDJSON stream, got: %s", body)
	}
	if !strings.Contains(body, `"kb_name":"Tasks2"`) {
		t.Fatalf("expected structured tool args in NDJSON stream, got: %s", body)
	}
}

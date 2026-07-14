package sopbackend

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/memory"
)

func TestKBRefinementHandlersSupportTaskBasedFallbacks(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "kb_effectiveness.json")
	tracker, err := memory.NewKBEffectivenessTracker(storePath, false)
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	defer tracker.Close()

	tracker.RecordOutcome("debugging", "error-handling-space", true, 0.93)
	tracker.RecordOutcome("debugging", "testing-space", true, 0.88)

	previousTracker := kbEffectivenessTracker
	kbEffectivenessTracker = tracker
	defer func() {
		kbEffectivenessTracker = previousTracker
	}()

	feedbackReq := httptest.NewRequest(http.MethodPost, "/api/ai/feedback", bytes.NewBufferString(`{"message_id":"msg-1","kb_name":"error-handling-space","task_type":"debugging","feedback":"positive","user_id":"u1","session_id":"s1"}`))
	feedbackRec := httptest.NewRecorder()
	HandleKBFeedback(feedbackRec, feedbackReq)
	if feedbackRec.Code != http.StatusOK {
		t.Fatalf("feedback handler returned %d: %s", feedbackRec.Code, feedbackRec.Body.String())
	}

	alternativesReq := httptest.NewRequest(http.MethodPost, "/api/ai/kb/alternatives", bytes.NewBufferString(`{"task":"debugging","current_kb":"error-handling-space","excluded_kbs":["error-handling-space"]}`))
	alternativesRec := httptest.NewRecorder()
	HandleKBAlternatives(alternativesRec, alternativesReq)
	if alternativesRec.Code != http.StatusOK {
		t.Fatalf("alternatives handler returned %d: %s", alternativesRec.Code, alternativesRec.Body.String())
	}

	var altResp KBAlternativesResponse
	if err := json.Unmarshal(alternativesRec.Body.Bytes(), &altResp); err != nil {
		t.Fatalf("decode alternatives response: %v", err)
	}
	if !altResp.Success {
		t.Fatalf("expected success response, got %+v", altResp)
	}
	if len(altResp.Alternatives) == 0 {
		t.Fatalf("expected alternative KB suggestions, got %#v", altResp)
	}
	if altResp.Alternatives[0].Name != "testing-space" {
		t.Fatalf("expected testing-space as first alternative, got %+v", altResp.Alternatives[0])
	}

	revalidateReq := httptest.NewRequest(http.MethodPost, "/api/ai/chat/revalidate", bytes.NewBufferString(`{"message_id":"msg-2","task":"debugging","code":"func readConfig(path string) error {\n    f, err := os.Open(path)\n    if err != nil {\n        return err\n    }\n    defer f.Close()\n    return nil\n}","kb_name":"error-handling-space","strict_mode":true}`))
	revalidateRec := httptest.NewRecorder()
	HandleKBRevalidate(revalidateRec, revalidateReq)
	if revalidateRec.Code != http.StatusOK {
		t.Fatalf("revalidate handler returned %d: %s", revalidateRec.Code, revalidateRec.Body.String())
	}

	var revalidateResp KBRevalidateResponse
	if err := json.Unmarshal(revalidateRec.Body.Bytes(), &revalidateResp); err != nil {
		t.Fatalf("decode revalidate response: %v", err)
	}
	if !revalidateResp.Success {
		t.Fatalf("expected revalidate success, got %+v", revalidateResp)
	}
	if !revalidateResp.ValidationPassed {
		t.Fatalf("expected validation to pass for well-formed code, got %+v", revalidateResp)
	}
}

func TestKBRefinementHandlersRejectInvalidRequests(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/ai/chat/revalidate", bytes.NewBufferString(`{"code":"func noop() {}"}`))
	rec := httptest.NewRecorder()
	HandleKBRevalidate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d for invalid request, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}

	altReq := httptest.NewRequest(http.MethodPost, "/api/ai/kb/alternatives", bytes.NewBufferString(`{"current_kb":"missing"}`))
	altRec := httptest.NewRecorder()
	HandleKBAlternatives(altRec, altReq)
	if altRec.Code != http.StatusOK {
		t.Fatalf("expected success with empty alternatives, got %d: %s", altRec.Code, altRec.Body.String())
	}
}

func TestKBChatTemplateIncludesRefinementControls(t *testing.T) {
	path := filepath.Join("templates", "scripts_kb_chat.html")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read chat template: %v", err)
	}
	content := string(data)
	for _, needle := range []string{
		"kb-refinement-toggle",
		"applyStricterValidation",
		"tryDifferentSpace",
		"/api/ai/chat/revalidate",
		"/api/ai/kb/alternatives",
	} {
		if !strings.Contains(content, needle) {
			t.Fatalf("expected %q in %s", needle, path)
		}
	}
}

func BenchmarkHandleKBAlternatives(b *testing.B) {
	tempDir := b.TempDir()
	storePath := filepath.Join(tempDir, "kb_effectiveness.json")
	tracker, err := memory.NewKBEffectivenessTracker(storePath, false)
	if err != nil {
		b.Fatalf("create tracker: %v", err)
	}
	defer tracker.Close()

	tracker.RecordOutcome("debugging", "error-handling-space", true, 0.93)
	tracker.RecordOutcome("debugging", "testing-space", true, 0.88)

	previousTracker := kbEffectivenessTracker
	kbEffectivenessTracker = tracker
	defer func() {
		kbEffectivenessTracker = previousTracker
	}()

	payload := []byte(`{"task":"debugging","current_kb":"error-handling-space","excluded_kbs":["error-handling-space"]}`)
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/ai/kb/alternatives", bytes.NewBuffer(payload))
		rec := httptest.NewRecorder()
		HandleKBAlternatives(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
		}
	}
}

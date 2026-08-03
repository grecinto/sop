package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
)

type stubAskParticipant struct {
	lastRequest AskRequest
	response    AskResponse
	err         error
}

func (s *stubAskParticipant) AskWithRequest(ctx context.Context, req AskRequest) (AskResponse, error) {
	s.lastRequest = req
	return s.response, s.err
}

func TestParticipantHandlerDelegatesRequest(t *testing.T) {
	participant := &stubAskParticipant{
		response: AskResponse{
			FinalText:      "delegated-result",
			UpdatedSession: &ai.SessionPayload{SessionID: "s-123", UserID: "user-1"},
			OutcomeFacts:   []string{"fact-a"},
		},
	}
	handler := NewParticipantHandler(participant)

	body := `{"query":"fix the parser","session":{"session_id":"s-123","user_id":"user-1"},"thinking_level":"high","default_format":"json","is_new_topic":true,"options":{"forced_db_name":"workspace"}}`
	req := httptest.NewRequest(http.MethodPost, "/participant/ask", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if participant.lastRequest.Query != "fix the parser" {
		t.Fatalf("query = %q, want %q", participant.lastRequest.Query, "fix the parser")
	}
	if participant.lastRequest.Session == nil || participant.lastRequest.Session.SessionID != "s-123" {
		t.Fatalf("session = %#v, want session_id s-123", participant.lastRequest.Session)
	}
	if participant.lastRequest.ThinkingLevel != "high" {
		t.Fatalf("thinking_level = %q, want %q", participant.lastRequest.ThinkingLevel, "high")
	}
	if participant.lastRequest.DefaultFormat != "json" {
		t.Fatalf("default_format = %q, want %q", participant.lastRequest.DefaultFormat, "json")
	}
	if participant.lastRequest.Options == nil {
		t.Fatal("options were not forwarded")
	}
	if forcedDB, ok := participant.lastRequest.Options.Get("forced_db_name"); !ok || forcedDB != "workspace" {
		t.Fatalf("forced_db_name = %#v, want %q", forcedDB, "workspace")
	}
	if isNewTopic, ok := participant.lastRequest.Options.Get("is_new_topic"); !ok || isNewTopic != true {
		t.Fatalf("is_new_topic = %#v, want true", isNewTopic)
	}

	var resp ParticipantResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.FinalText != "delegated-result" {
		t.Fatalf("final_text = %q, want %q", resp.FinalText, "delegated-result")
	}
	if resp.UpdatedSession == nil || resp.UpdatedSession.UserID != "user-1" {
		t.Fatalf("updated_session = %#v, want user-1", resp.UpdatedSession)
	}
	if len(resp.OutcomeFacts) != 1 || resp.OutcomeFacts[0] != "fact-a" {
		t.Fatalf("outcome_facts = %#v, want [fact-a]", resp.OutcomeFacts)
	}
}

func TestParticipantClientPostsRequest(t *testing.T) {
	client := &ParticipantClient{
		URL: "https://participant.example.test/ask",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			if got := r.Header.Get("X-Controller"); got != "outer-agent" {
				t.Fatalf("X-Controller = %q, want %q", got, "outer-agent")
			}

			var req ParticipantRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if req.Query != "inspect repo state" {
				t.Fatalf("query = %q, want %q", req.Query, "inspect repo state")
			}
			respBody, err := json.Marshal(ParticipantResponse{
				FinalText:      "participant-complete",
				UpdatedSession: &ParticipantSession{SessionID: "remote-session"},
			})
			if err != nil {
				t.Fatalf("marshal response: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(string(respBody))),
			}, nil
		})},
		Headers: http.Header{"X-Controller": []string{"outer-agent"}},
	}

	resp, err := client.Ask(context.Background(), ParticipantRequest{
		Query:   "inspect repo state",
		Session: &ParticipantSession{SessionID: "remote-session"},
	})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if resp.FinalText != "participant-complete" {
		t.Fatalf("final_text = %q, want %q", resp.FinalText, "participant-complete")
	}
	if resp.UpdatedSession == nil || resp.UpdatedSession.SessionID != "remote-session" {
		t.Fatalf("updated_session = %#v, want session remote-session", resp.UpdatedSession)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

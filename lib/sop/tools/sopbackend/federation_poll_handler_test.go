package sopbackend

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestHandleFederationPollReturnsCurrentCursor(t *testing.T) {
	// Reset global cursor for test
	globalFederationCursor = nil
	globalFederationCursorOnce = sync.Once{}

	cursor := GetGlobalFederationCursor()
	currentHash, _, _ := cursor.GetCurrentCursor()

	reqBody := `{"daemon_id":"test-daemon","last_applied_cursor":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/federation/poll", strings.NewReader(reqBody))
	rr := httptest.NewRecorder()

	HandleFederationPoll(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp FederationPollResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.CurrentCursor != currentHash {
		t.Errorf("expected cursor %s, got %s", currentHash, resp.CurrentCursor)
	}

	if !resp.HasDelta {
		t.Error("expected delta for empty last_applied_cursor")
	}
}

func TestHandleFederationPollDetectsNoDelta(t *testing.T) {
	// Reset global cursor for test
	globalFederationCursor = nil
	globalFederationCursorOnce = sync.Once{}

	cursor := GetGlobalFederationCursor()
	currentHash, _, _ := cursor.GetCurrentCursor()

	reqBody := `{"daemon_id":"test-daemon","last_applied_cursor":"` + currentHash + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/federation/poll", strings.NewReader(reqBody))
	rr := httptest.NewRecorder()

	HandleFederationPoll(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp FederationPollResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.HasDelta {
		t.Error("expected no delta when cursor matches")
	}

	if resp.StreamEndpoint != "" {
		t.Error("expected no stream endpoint when no delta")
	}
}

func TestHandleFederationPollDetectsDeltaAfterIncrement(t *testing.T) {
	// Reset global cursor for test
	globalFederationCursor = nil
	globalFederationCursorOnce = sync.Once{}

	cursor := GetGlobalFederationCursor()
	oldHash, _, _ := cursor.GetCurrentCursor()

	// Simulate a change
	cursor.IncrementVersion()

	reqBody := `{"daemon_id":"test-daemon","last_applied_cursor":"` + oldHash + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/federation/poll", strings.NewReader(reqBody))
	rr := httptest.NewRecorder()

	HandleFederationPoll(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp FederationPollResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.HasDelta {
		t.Error("expected delta after cursor increment")
	}

	if resp.StreamEndpoint == "" {
		t.Error("expected stream endpoint when delta exists")
	}

	if resp.CurrentCursor == oldHash {
		t.Error("expected different cursor after increment")
	}
}

func TestHandleFederationPollRejectsNonPost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/daemon/federation/poll", nil)
	rr := httptest.NewRecorder()

	HandleFederationPoll(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rr.Code)
	}
}

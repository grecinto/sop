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

func TestHandleFederationPollRespectsRateLimiting(t *testing.T) {
	// Reset global state
	globalFederationCursor = nil
	globalFederationCursorOnce = sync.Once{}
	hqRateLimiterMu.Lock()
	hqRateLimiter = nil
	hqRateLimiterMu.Unlock()

	// Set very low rate limits for testing
	t.Setenv("SOP_HQ_PER_DAEMON_RATE", "6")  // 6 req/min = 0.1 req/sec
	t.Setenv("SOP_HQ_PER_DAEMON_BURST", "2") // burst of 2

	cursor := GetGlobalFederationCursor()
	cursor.IncrementVersion()

	daemonID := "rate-test-daemon"

	// First request should succeed (uses burst token)
	reqBody1 := `{"daemon_id":"` + daemonID + `","last_applied_cursor":""}`
	req1 := httptest.NewRequest(http.MethodPost, "/api/daemon/federation/poll", strings.NewReader(reqBody1))
	rr1 := httptest.NewRecorder()
	HandleFederationPoll(rr1, req1)

	if rr1.Code != http.StatusOK {
		t.Fatalf("first request: expected status 200, got %d", rr1.Code)
	}

	var resp1 FederationPollResponse
	if err := json.NewDecoder(rr1.Body).Decode(&resp1); err != nil {
		t.Fatalf("failed to decode first response: %v", err)
	}

	if throttle, ok := resp1.Throttle["allowed"].(bool); !ok || !throttle {
		t.Errorf("first request should be allowed by throttle")
	}

	// Second request should succeed (uses second burst token)
	reqBody2 := `{"daemon_id":"` + daemonID + `","last_applied_cursor":""}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/daemon/federation/poll", strings.NewReader(reqBody2))
	rr2 := httptest.NewRecorder()
	HandleFederationPoll(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("second request: expected status 200, got %d", rr2.Code)
	}

	// Third request should be rate limited (burst exhausted)
	reqBody3 := `{"daemon_id":"` + daemonID + `","last_applied_cursor":""}`
	req3 := httptest.NewRequest(http.MethodPost, "/api/daemon/federation/poll", strings.NewReader(reqBody3))
	rr3 := httptest.NewRecorder()
	HandleFederationPoll(rr3, req3)

	if rr3.Code != http.StatusOK {
		t.Fatalf("third request: expected status 200, got %d", rr3.Code)
	}

	var resp3 FederationPollResponse
	if err := json.NewDecoder(rr3.Body).Decode(&resp3); err != nil {
		t.Fatalf("failed to decode third response: %v", err)
	}

	if throttle, ok := resp3.Throttle["allowed"].(bool); !ok || throttle {
		t.Errorf("third request should be blocked by throttle, got allowed=%v", throttle)
	}

	if reason, ok := resp3.Throttle["reason"].(string); !ok || reason == "" {
		t.Errorf("expected throttle reason, got %v", reason)
	}

	// JSON numbers decode as float64, not int
	if interval, ok := resp3.Throttle["sync_interval_seconds"].(float64); !ok || interval <= 0 {
		t.Errorf("expected positive sync_interval_seconds when rate limited, got %v (type %T)", resp3.Throttle["sync_interval_seconds"], resp3.Throttle["sync_interval_seconds"])
	}
}

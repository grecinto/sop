package sopbackend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
)

func TestHandleHQSyncAcceptsBatchWithArtifacts(t *testing.T) {
	// Setup config with root user
	config = Config{UsersStore: NewUserStore()}
	rootUser := &UserRecord{
		Username:     "root",
		Role:         sop.RoleSystemAdmin,
		PasswordHash: SecuredString("$2a$10$test"),
		Status:       "active",
	}
	if err := config.ensureUserStore().Save(context.Background(), rootUser); err != nil {
		t.Fatalf("save root user: %v", err)
	}

	batch := map[string]any{
		"kind":       "shared_memory_sync",
		"session_id": "test-session",
		"user_id":    "daemon",
		"daemon_id":  "test-daemon",
		"timestamp":  "2026-07-05T12:00:00Z",
		"shared_memory": map[string]any{
			"enabled":      true,
			"scope":        "shared-memory",
			"mode":         "periodic",
			"last_sync_at": "2026-07-05T12:00:00Z",
			"artifacts": []map[string]any{
				{
					"text":     "Test pattern for authentication retry",
					"category": "shared_memory",
					"metadata": map[string]any{
						"source": "daemon_ask",
					},
				},
			},
		},
	}

	body, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal batch: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/hq-sync", strings.NewReader(string(body)))
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{
		UserID: "daemon",
		Roles:  []string{sop.RoleUser},
	}))
	rr := httptest.NewRecorder()

	HandleHQSync(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var response map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response["status"] != "accepted" {
		t.Fatalf("expected status 'accepted', got %v", response["status"])
	}

	if response["daemon_id"] != "test-daemon" {
		t.Fatalf("expected daemon_id 'test-daemon', got %v", response["daemon_id"])
	}
}

func TestHandleHQSyncRejectsNonPost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/daemon/hq-sync", nil)
	rr := httptest.NewRecorder()

	HandleHQSync(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}
}

func TestHandleHQSyncRejectsInvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/hq-sync", strings.NewReader("invalid json"))
	rr := httptest.NewRecorder()

	HandleHQSync(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestHandleHQSyncAcceptsEmptyArtifacts(t *testing.T) {
	// Setup config with root user
	config = Config{UsersStore: NewUserStore()}
	rootUser := &UserRecord{
		Username:     "root",
		Role:         sop.RoleSystemAdmin,
		PasswordHash: SecuredString("$2a$10$test"),
		Status:       "active",
	}
	if err := config.ensureUserStore().Save(context.Background(), rootUser); err != nil {
		t.Fatalf("save root user: %v", err)
	}

	batch := map[string]any{
		"kind":      "shared_memory_sync",
		"daemon_id": "test-daemon",
		"timestamp": "2026-07-05T12:00:00Z",
		"shared_memory": map[string]any{
			"enabled": true,
			"scope":   "shared-memory",
		},
	}

	body, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal batch: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/hq-sync", strings.NewReader(string(body)))
	rr := httptest.NewRecorder()

	HandleHQSync(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestHandleHQRetrieveAcceptsQuery(t *testing.T) {
	// Reset rate limiter for fresh test
	hqRateLimiterMu.Lock()
	hqRateLimiter = nil
	hqRateLimiterMu.Unlock()

	// Setup config with root user
	config = Config{UsersStore: NewUserStore()}
	rootUser := &UserRecord{
		Username:     "root",
		Role:         sop.RoleSystemAdmin,
		PasswordHash: SecuredString("$2a$10$test"),
		Status:       "active",
	}
	if err := config.ensureUserStore().Save(context.Background(), rootUser); err != nil {
		t.Fatalf("save root user: %v", err)
	}

	reqData := map[string]any{
		"daemon_id": "test-daemon",
		"query":     "test query",
		"limit":     5,
	}

	body, err := json.Marshal(reqData)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/hq-retrieve", strings.NewReader(string(body)))
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{
		UserID: "daemon",
		Roles:  []string{sop.RoleUser},
	}))
	rr := httptest.NewRecorder()

	HandleHQRetrieve(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var response hqRetrieveResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Artifacts == nil {
		t.Fatalf("expected artifacts array, got nil")
	}
}

func TestHandleHQRetrieveRejectsNonPost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/daemon/hq-retrieve", nil)
	rr := httptest.NewRecorder()

	HandleHQRetrieve(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}
}

func TestHandleHQRetrieveAcceptsEmptyQuery(t *testing.T) {
	// Reset rate limiter for fresh test
	hqRateLimiterMu.Lock()
	hqRateLimiter = nil
	hqRateLimiterMu.Unlock()

	// Setup config with root user
	config = Config{UsersStore: NewUserStore()}
	rootUser := &UserRecord{
		Username:     "root",
		Role:         sop.RoleSystemAdmin,
		PasswordHash: SecuredString("$2a$10$test"),
		Status:       "active",
	}
	if err := config.ensureUserStore().Save(context.Background(), rootUser); err != nil {
		t.Fatalf("save root user: %v", err)
	}

	reqData := map[string]any{
		"daemon_id": "test-daemon",
		"query":     "",
		"limit":     5,
	}

	body, err := json.Marshal(reqData)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/hq-retrieve", strings.NewReader(string(body)))
	rr := httptest.NewRecorder()

	HandleHQRetrieve(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var response hqRetrieveResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(response.Artifacts) != 0 {
		t.Fatalf("expected empty artifacts for empty query, got %d", len(response.Artifacts))
	}
}

func TestHandleHQSyncRateLimitsExcessRequests(t *testing.T) {
	t.Setenv("SOP_HQ_RATE_LIMITING_ENABLED", "true")
	t.Setenv("SOP_HQ_PER_DAEMON_RATE", "60")
	t.Setenv("SOP_HQ_PER_DAEMON_BURST", "1")
	t.Setenv("SOP_HQ_GLOBAL_RATE", "1000")
	t.Setenv("SOP_HQ_GLOBAL_BURST", "1")

	hqRateLimiterMu.Lock()
	hqRateLimiter = nil
	hqRateLimiterMu.Unlock()

	batch := map[string]any{
		"kind":      "shared_memory_sync",
		"daemon_id": "rate-limit-daemon",
		"shared_memory": map[string]any{
			"enabled":   true,
			"artifacts": []map[string]any{},
		},
	}

	body, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal batch: %v", err)
	}

	firstReq := httptest.NewRequest(http.MethodPost, "/api/daemon/hq-sync", strings.NewReader(string(body)))
	firstRR := httptest.NewRecorder()
	HandleHQSync(firstRR, firstReq)
	if firstRR.Code != http.StatusOK {
		t.Fatalf("expected first sync to be accepted, got %d with body: %s", firstRR.Code, firstRR.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/daemon/hq-sync", strings.NewReader(string(body)))
	secondRR := httptest.NewRecorder()
	HandleHQSync(secondRR, secondReq)
	if secondRR.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second sync to be rate limited, got %d with body: %s", secondRR.Code, secondRR.Body.String())
	}
	if got := secondRR.Header().Get("Retry-After"); got == "" {
		t.Fatalf("expected Retry-After header on rate limited response")
	}
	if got := secondRR.Header().Get("X-RateLimit-Reason"); got == "" {
		t.Fatalf("expected X-RateLimit-Reason header on rate limited response")
	}
}

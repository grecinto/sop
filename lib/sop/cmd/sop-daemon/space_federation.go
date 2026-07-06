package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	log "log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent"
	aidb "github.com/sharedcode/sop/ai/database"
)

var (
	federatedSpaces   = make(map[string]FederatedSpaceInfo) // key: spaceName
	federatedSpacesMu sync.RWMutex
)

// FederatedSpaceInfo tracks federated Spaces received from HQ.
type FederatedSpaceInfo struct {
	SpaceName    string    `json:"space_name"`
	DatabaseName string    `json:"database_name"`
	ReceivedAt   time.Time `json:"received_at"`
	Version      int       `json:"version"`
	ItemCount    int       `json:"item_count"`
	Source       string    `json:"source"` // "hq" or "local"
}

// handleReceiveSpaceFromHQ receives a Space from HQ and imports it locally.
func handleReceiveSpaceFromHQ(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req struct {
		SpaceName    string `json:"space_name"`
		DatabaseName string `json:"database_name"`
		SpaceData    string `json:"space_data"`
		Timestamp    string `json:"timestamp"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		log.Warn("daemon receive space: invalid JSON payload", "error", err)
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if req.SpaceName == "" || req.DatabaseName == "" || req.SpaceData == "" {
		http.Error(w, "space_name, database_name, and space_data are required", http.StatusBadRequest)
		return
	}

	log.Info("daemon receive space: received Space from HQ",
		"space_name", req.SpaceName,
		"database_name", req.DatabaseName,
	)

	// Import the Space into local database
	if err := importSpaceFromHQ(context.Background(), req.DatabaseName, req.SpaceName, []byte(req.SpaceData)); err != nil {
		log.Error("daemon receive space: failed to import Space",
			"error", err,
			"space_name", req.SpaceName,
		)
		http.Error(w, "Failed to import Space", http.StatusInternalServerError)
		return
	}

	// Track the federated Space
	federatedSpacesMu.Lock()
	federatedSpaces[req.SpaceName] = FederatedSpaceInfo{
		SpaceName:    req.SpaceName,
		DatabaseName: req.DatabaseName,
		ReceivedAt:   time.Now().UTC(),
		Source:       "hq",
	}
	federatedSpacesMu.Unlock()

	log.Info("daemon receive space: successfully imported",
		"space_name", req.SpaceName,
		"database_name", req.DatabaseName,
	)

	// Send acknowledgment to HQ
	go acknowledgeSpaceReceipt(req.SpaceName, req.DatabaseName)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"status":    "imported",
		"message":   fmt.Sprintf("Space %s imported successfully", req.SpaceName),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// importSpaceFromHQ imports a Space received from HQ into the local database.
func importSpaceFromHQ(ctx context.Context, dbName, spaceName string, spaceData []byte) error {
	db, err := sharedMemoryDatabase()
	if err != nil {
		return fmt.Errorf("get database: %w", err)
	}

	trans, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer trans.Rollback(ctx)

	kb, err := db.OpenKnowledgeBase(ctx, spaceName, trans, nil, nil, true)
	if err != nil {
		return fmt.Errorf("open knowledge base: %w", err)
	}

	reader := bytes.NewReader(spaceData)
	if err := kb.ImportJSON(ctx, reader, ""); err != nil {
		return fmt.Errorf("import JSON: %w", err)
	}

	if err := trans.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// acknowledgeSpaceReceipt sends an acknowledgment to HQ that a Space was received.
func acknowledgeSpaceReceipt(spaceName, dbName string) {
	hqBaseURL := strings.TrimSpace(os.Getenv("SOP_HQ_BASE_URL"))
	if hqBaseURL == "" {
		log.Debug("daemon acknowledge: no HQ URL configured, skipping acknowledgment")
		return
	}

	endpoint := strings.TrimRight(hqBaseURL, "/") + "/api/daemon/acknowledge-space"

	payload := map[string]any{
		"space_name":    spaceName,
		"database_name": dbName,
		"daemon_id":     "local-daemon",
		"status":        "acknowledged",
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		log.Warn("daemon acknowledge: failed to marshal payload", "error", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(jsonPayload))
	if err != nil {
		log.Warn("daemon acknowledge: failed to create request", "error", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Warn("daemon acknowledge: failed to send", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Warn("daemon acknowledge: HQ returned error", "status", resp.StatusCode)
		return
	}

	log.Info("daemon acknowledge: sent acknowledgment to HQ",
		"space_name", spaceName,
	)
}

// retrieveFederatedSpaceContext retrieves context from federated Spaces during research phase.
func retrieveFederatedSpaceContext(ctx context.Context, query string, limit int) (string, error) {
	federatedSpacesMu.RLock()
	spaceNames := make([]string, 0, len(federatedSpaces))
	for name := range federatedSpaces {
		spaceNames = append(spaceNames, name)
	}
	federatedSpacesMu.RUnlock()

	if len(spaceNames) == 0 {
		return "", nil
	}

	db, err := sharedMemoryDatabase()
	if err != nil {
		return "", fmt.Errorf("get database: %w", err)
	}

	var results []string

	for _, spaceName := range spaceNames {
		hits, err := retrieveFromSpace(ctx, db, spaceName, query, limit)
		if err != nil {
			log.Warn("daemon federated retrieval: failed to query Space",
				"error", err,
				"space_name", spaceName,
			)
			continue
		}

		if len(hits) > 0 {
			for _, hit := range hits {
				if text, ok := hit.Payload["text"].(string); ok && strings.TrimSpace(text) != "" {
					results = append(results, fmt.Sprintf("[%s] %s", spaceName, text))
				}
			}
		}
	}

	if len(results) == 0 {
		return "", nil
	}

	return fmt.Sprintf("Federated Space Context:\n%s", strings.Join(results, "\n")), nil
}

// retrieveFromSpace retrieves relevant content from a specific Space.
func retrieveFromSpace(ctx context.Context, db *aidb.Database, spaceName, query string, limit int) ([]ai.Hit[map[string]any], error) {
	trans, err := db.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer trans.Rollback(ctx)

	// Use the shared memory retrieval helper
	hits, err := agent.RetrieveSharedMemory(ctx, db, nil, nil, query, spaceName, limit)
	if err != nil {
		return nil, fmt.Errorf("retrieve: %w", err)
	}

	return hits, nil
}

// GetFederatedSpaces returns the list of federated Spaces available locally.
func GetFederatedSpaces() []FederatedSpaceInfo {
	federatedSpacesMu.RLock()
	defer federatedSpacesMu.RUnlock()

	spaces := make([]FederatedSpaceInfo, 0, len(federatedSpaces))
	for _, space := range federatedSpaces {
		spaces = append(spaces, space)
	}
	return spaces
}

// pushSpaceContentToHQ pushes daemon-generated content derived from a federated Space back to HQ.
func pushSpaceContentToHQ(ctx context.Context, spaceName, content, category string) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	hqBaseURL := strings.TrimSpace(os.Getenv("SOP_HQ_BASE_URL"))
	if hqBaseURL == "" {
		log.Debug("daemon push content: no HQ URL configured")
		return nil
	}

	endpoint := strings.TrimRight(hqBaseURL, "/") + "/api/daemon/hq-sync"

	artifact := struct {
		Text     string         `json:"text"`
		Category string         `json:"category"`
		Metadata map[string]any `json:"metadata"`
	}{
		Text:     content,
		Category: category,
		Metadata: map[string]any{
			"source_space": spaceName,
			"daemon_id":    "local-daemon",
			"derived":      true,
			"promoted_at":  time.Now().UTC().Format(time.RFC3339),
		},
	}

	batch := map[string]any{
		"kind":       "shared_memory_sync",
		"session_id": "daemon-space-content",
		"user_id":    "daemon",
		"daemon_id":  "local-daemon",
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"shared_memory": map[string]any{
			"enabled":      true,
			"scope":        "space-derived",
			"mode":         "on-demand",
			"last_sync_at": time.Now().UTC().Format(time.RFC3339),
			"artifacts":    []any{artifact},
		},
	}

	jsonPayload, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonPayload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HQ returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Info("daemon push content: sent Space-derived content to HQ",
		"space_name", spaceName,
		"category", category,
	)

	return nil
}

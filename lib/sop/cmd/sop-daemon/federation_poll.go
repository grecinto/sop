package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// FederationPollClient manages periodic polling to HQ for federation changes.
type FederationPollClient struct {
	hqBaseURL         string
	daemonID          string
	lastAppliedCursor string
	pollInterval      time.Duration
}

// NewFederationPollClient creates a new poll client.
func NewFederationPollClient(hqBaseURL, daemonID string) *FederationPollClient {
	return &FederationPollClient{
		hqBaseURL:         strings.TrimRight(hqBaseURL, "/"),
		daemonID:          daemonID,
		lastAppliedCursor: "",
		pollInterval:      getEnvDuration("SOP_FEDERATION_POLL_INTERVAL", 1*time.Hour),
	}
}

// federationPollRequest represents the poll request payload.
type federationPollRequest struct {
	DaemonID          string `json:"daemon_id"`
	LastAppliedCursor string `json:"last_applied_cursor"`
}

// federationPollResponse represents the poll response from HQ.
type federationPollResponse struct {
	CurrentCursor  string    `json:"current_cursor"`
	HasDelta       bool      `json:"has_delta"`
	DeltaCount     int       `json:"delta_count,omitempty"`
	StreamEndpoint string    `json:"stream_endpoint,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
}

// Poll checks HQ for federation changes since the last cursor.
func (c *FederationPollClient) Poll(ctx context.Context) (*federationPollResponse, error) {
	if !getEnvBool("SOP_FEDERATION_POLL_ENABLED", true) {
		return nil, fmt.Errorf("federation polling disabled")
	}

	pollURL := c.hqBaseURL + "/api/daemon/federation/poll"

	req := federationPollRequest{
		DaemonID:          c.daemonID,
		LastAppliedCursor: c.lastAppliedCursor,
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal poll request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, pollURL, strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("create poll request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	// Add authentication if available
	if secret := os.Getenv("SOP_HQ_SIGNING_SECRET"); secret != "" {
		httpReq.Header.Set("Authorization", "Bearer "+secret)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send poll request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("poll failed with status %d: %s", resp.StatusCode, string(body))
	}

	var pollResp federationPollResponse
	if err := json.NewDecoder(resp.Body).Decode(&pollResp); err != nil {
		return nil, fmt.Errorf("decode poll response: %w", err)
	}

	log.Printf("Federation poll: cursor=%s, has_delta=%v, delta_count=%d",
		pollResp.CurrentCursor, pollResp.HasDelta, pollResp.DeltaCount)

	return &pollResp, nil
}

// UpdateCursor updates the last applied cursor after successful sync.
func (c *FederationPollClient) UpdateCursor(cursor string) {
	c.lastAppliedCursor = cursor
}

// GetPollInterval returns the configured poll interval.
func (c *FederationPollClient) GetPollInterval() time.Duration {
	return c.pollInterval
}

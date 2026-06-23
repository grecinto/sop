package topology

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type RemoteAskRequest struct {
	Query string `json:"query"`
}

type RemoteAskResponse struct {
	Answer string `json:"answer"`
}

func AskRemote(ctx context.Context, endpoint string, query string) (RemoteAskResponse, error) {
	payload := RemoteAskRequest{Query: query}
	body, err := json.Marshal(payload)
	if err != nil {
		return RemoteAskResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBuffer(body))
	if err != nil {
		return RemoteAskResponse{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return RemoteAskResponse{}, fmt.Errorf("remote ask request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return RemoteAskResponse{}, fmt.Errorf("remote ask returned status %d", resp.StatusCode)
	}

	var out RemoteAskResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return RemoteAskResponse{}, fmt.Errorf("decode response: %w", err)
	}
	return out, nil
}

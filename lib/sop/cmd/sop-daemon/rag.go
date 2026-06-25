package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent"
)

const defaultDaemonConfigFileName = "config.json"

func ensureConfigPath(explicitPath string) (string, bool, error) {
	if strings.TrimSpace(explicitPath) != "" {
		resolved := resolveConfigPath(explicitPath)
		return resolved, false, nil
	}

	candidates := []string{}
	if v := os.Getenv("XDG_CONFIG_HOME"); strings.TrimSpace(v) != "" {
		candidates = append(candidates, filepath.Join(v, "sop", defaultDaemonConfigFileName))
	} else {
		if home, err := os.UserHomeDir(); err == nil {
			// TODO: add explicit macOS and Windows app-data/config directory handling here.
			candidates = append(candidates, filepath.Join(home, ".config", "sop", defaultDaemonConfigFileName))
			candidates = append(candidates, filepath.Join(home, "sop", defaultDaemonConfigFileName))
		}
		candidates = append(candidates, filepath.Join(cwd(), defaultDaemonConfigFileName))
		candidates = append(candidates, filepath.Join(cwd(), "sop", defaultDaemonConfigFileName))
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate, false, nil
		}
	}

	defaultPath := ""
	if v := os.Getenv("XDG_CONFIG_HOME"); strings.TrimSpace(v) != "" {
		defaultPath = filepath.Join(v, "sop", defaultDaemonConfigFileName)
	} else if home, err := os.UserHomeDir(); err == nil {
		// TODO: use platform-appropriate config directories on macOS and Windows.
		defaultPath = filepath.Join(home, ".config", "sop", defaultDaemonConfigFileName)
	} else {
		defaultPath = filepath.Join(cwd(), defaultDaemonConfigFileName)
	}

	if err := os.MkdirAll(filepath.Dir(defaultPath), 0o755); err != nil {
		return "", false, fmt.Errorf("create config directory: %w", err)
	}

	// Create default database path relative to config
	dbPath := filepath.Join(filepath.Dir(defaultPath), "sop_data")
	
	bootstrap := fmt.Sprintf(`{
  "port": 9090,
  "databases": [
    {
      "name": "local",
      "path": "%s",
      "mode": "standalone"
    }
  ],
  "pageSize": 40,
  "production_mode": true
}
`, dbPath)
	if err := os.WriteFile(defaultPath, []byte(bootstrap), 0o644); err != nil {
		return "", false, fmt.Errorf("write default config: %w", err)
	}
	
	log.Printf("Created default daemon config at %s with standalone database (no SOP KB)", defaultPath)
	return defaultPath, true, nil
}

type AskRequest struct {
	Query string `json:"query"`
	Auth  struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	} `json:"auth"`
	HQ struct {
		BaseURL   string `json:"base_url"`
		UserID    string `json:"user_id"`
		SessionID string `json:"session_id"`
	} `json:"hq"`
}

type AskResponse struct {
	Answer string `json:"answer"`
	Error  string `json:"error,omitempty"`
}

type localRAGService struct {
	service ai.Agent[map[string]any]
}

func newLocalRAGService(configPath string) (*localRAGService, error) {
	if configPath == "" {
		return nil, fmt.Errorf("config path is required")
	}

	if svc, err := autoProvisionSharedCopilotService(context.Background()); err == nil {
		return &localRAGService{service: svc}, nil
	} else {
		log.Printf("falling back to local runtime config for daemon agent bootstrap: %v", err)
	}

	cfg, err := agent.LoadConfigFromFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	if cfg.StoragePath != "" {
		if absPath, err := filepath.Abs(cfg.StoragePath); err == nil {
			cfg.StoragePath = absPath
		}
	}
	svc, err := agent.NewFromConfig(context.Background(), *cfg, agent.Dependencies{})
	if err != nil {
		return nil, fmt.Errorf("build agent: %w", err)
	}
	return &localRAGService{service: svc}, nil
}

func autoProvisionSharedCopilotService(ctx context.Context) (ai.Agent[map[string]any], error) {
	candidates := []string{
		"ai/data/copilot_pipeline.json",
		filepath.Join(cwd(), "ai/data/copilot_pipeline.json"),
		filepath.Join(cwd(), "..", "ai/data/copilot_pipeline.json"),
		filepath.Join(cwd(), "..", "..", "ai/data/copilot_pipeline.json"),
		filepath.Join("/Volumes/T7/sop-cloud/lib/sop", "ai/data/copilot_pipeline.json"),
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		cfg, err := agent.LoadConfigFromFile(candidate)
		if err != nil {
			return nil, fmt.Errorf("load shared pipeline config %s: %w", candidate, err)
		}
		svc, err := agent.NewFromConfig(ctx, *cfg, agent.Dependencies{})
		if err != nil {
			return nil, fmt.Errorf("build shared pipeline agent %s: %w", candidate, err)
		}
		return svc, nil
	}

	return nil, fmt.Errorf("shared copilot pipeline config not found")
}

func (s *localRAGService) handleAsk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req AskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, AskResponse{Error: "Invalid JSON body: " + err.Error()})
		return
	}
	if req.Query == "" {
		respondJSON(w, http.StatusBadRequest, AskResponse{Error: "Query cannot be empty"})
		return
	}
	if s == nil || s.service == nil {
		respondJSON(w, http.StatusServiceUnavailable, AskResponse{Error: "local RAG service is not initialized"})
		return
	}

	ctx := r.Context()
	payload := &ai.SessionPayload{
		CurrentUserQuery: req.Query,
		UserID:           strings.TrimSpace(req.HQ.UserID),
		SessionID:        strings.TrimSpace(req.HQ.SessionID),
	}
	if payload.SessionID == "" {
		payload.SessionID = payload.UserID
	}
	if payload.SessionID == "" {
		payload.SessionID = "local-daemon"
	}
	cfg := ai.NewConfigMap()
	cfg.Set("payload", payload)
	cfg.Set("access_token", strings.TrimSpace(req.Auth.AccessToken))
	cfg.Set("refresh_token", strings.TrimSpace(req.Auth.RefreshToken))
	cfg.Set("hq_base_url", strings.TrimSpace(req.HQ.BaseURL))
	cfg.Set("hq_user_id", strings.TrimSpace(req.HQ.UserID))
	cfg.Set("hq_session_id", strings.TrimSpace(req.HQ.SessionID))
	ctx = context.WithValue(ctx, "session_payload", payload)

	log.Printf("daemon ask request received: query=%q session=%q", req.Query, payload.SessionID)
	answer, err := s.service.Ask(ctx, req.Query, cfg)
	if err != nil {
		log.Printf("daemon ask request failed: query=%q error=%v", req.Query, err)
		respondJSON(w, http.StatusInternalServerError, AskResponse{Error: err.Error()})
		return
	}
	log.Printf("daemon ask request completed: query=%q answer_len=%d", req.Query, len(answer))
	respondJSON(w, http.StatusOK, AskResponse{Answer: answer})
}

func resolveConfigPath(path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Clean(filepath.Join(cwd(), path))
}

func cwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

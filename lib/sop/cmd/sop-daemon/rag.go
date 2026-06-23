package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent"
)

type AskRequest struct {
	Query string `json:"query"`
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
	answer, err := s.service.Ask(context.Background(), req.Query, nil)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, AskResponse{Error: err.Error()})
		return
	}
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

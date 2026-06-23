package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
)

func TestInitializeRequest_UsesAuthenticatedUserID(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/ai/chat", strings.NewReader(`{"message":"hi","user_id":"client-random"}`))
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "alice"}))

	chatReq, err := initializeRequest(nil, req)
	if err != nil {
		t.Fatalf("initializeRequest failed: %v", err)
	}
	if got := chatReq.UserID; got != "alice" {
		t.Fatalf("expected authenticated user ID to override request payload, got %q", got)
	}
}

func TestConstructPayload_PreservesRawUserMessage(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	config = Config{Databases: []DatabaseConfig{{Name: "demo", Path: t.TempDir()}}}

	req := &aiChatRequest{
		Message:  "show me orders",
		Database: "demo",
	}

	ctx, payload, _, fullMessage := constructPayload(context.Background(), httptest.NewRecorder(), req, llmSettings{}, func(string, any) {}, nil)
	if ctx == nil || payload == nil {
		t.Fatal("expected constructPayload to return context and payload")
	}
	if got := strings.TrimSpace(fullMessage); got != req.Message {
		t.Fatalf("expected raw user message to stay untouched, got %q want %q", got, req.Message)
	}
}

func TestLoadAgentFindsCopilotConfigFromToolsWorkingDirectory(t *testing.T) {
	oldLoadedAgents := loadedAgents
	loadedAgents = make(map[string]ai.Agent[map[string]any])
	defer func() { loadedAgents = oldLoadedAgents }()

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	toolsDir := filepath.Join(oldWd, "..")
	if err := os.Chdir(toolsDir); err != nil {
		t.Fatalf("Chdir(%q) error = %v", toolsDir, err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	if err := loadAgent(context.Background(), "copilot", "ai/data/copilot_pipeline.json"); err != nil {
		t.Fatalf("loadAgent() error = %v", err)
	}
	if _, ok := loadedAgents["copilot"]; !ok {
		t.Fatal("expected copilot agent to be registered")
	}
}

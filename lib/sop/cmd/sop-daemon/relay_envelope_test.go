package main

import (
	"strings"
	"testing"
)

func TestNormalizeHQBaseURLTrimsAndPortsLegacyAndNewFields(t *testing.T) {
	req := UIHandoffRequest{
		Query: "  hello  ",
		Auth: struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		}{
			AccessToken:  " token ",
			RefreshToken: " refresh ",
		},
		HQ: struct {
			BaseURL   string `json:"base_url"`
			Port      string `json:"port,omitempty"`
			UserID    string `json:"user_id"`
			SessionID string `json:"session_id"`
		}{
			BaseURL:   "https://hq.example",
			Port:      "8443",
			UserID:    "user-42",
			SessionID: "sess-17",
		},
		LLMProvider:   "anthropic",
		LLMAPIKey:     "api-key",
		LLMModel:      "claude-3.7",
		LLMURL:        "https://api.anthropic.com",
		ThinkingLevel: "medium",
	}

	if got := normalizeHQBaseURL(req.HQ.BaseURL, req.HQ.Port); got != "https://hq.example:8443" {
		t.Fatalf("expected normalized target URL, got %q", got)
	}
	if got := strings.TrimSpace(req.Query); got != "hello" {
		t.Fatalf("expected query to be trimmed, got %q", got)
	}
	if got := strings.TrimSpace(req.Auth.AccessToken); got != "token" {
		t.Fatalf("expected access token to be trimmed, got %q", got)
	}
	if got := strings.TrimSpace(req.LLMProvider); got != "anthropic" {
		t.Fatalf("expected provider name to be preserved, got %q", got)
	}
	if got := strings.TrimSpace(req.LLMModel); got != "claude-3.7" {
		t.Fatalf("expected provider model to be preserved, got %q", got)
	}
}

func TestNormalizeHQBaseURLHandlesNewTargetShape(t *testing.T) {
	req := UIHandoffRequest{
		Query: "",
		HQ: struct {
			BaseURL   string `json:"base_url"`
			Port      string `json:"port,omitempty"`
			UserID    string `json:"user_id"`
			SessionID string `json:"session_id"`
		}{
			BaseURL:   " http://127.0.0.1 ",
			Port:      "3000",
			UserID:    "u-1",
			SessionID: "s-1",
		},
		LLMProvider:   "openai",
		LLMAPIKey:     "secret",
		LLMModel:      "gpt-4o",
		LLMURL:        "https://api.openai.com",
		ThinkingLevel: "high",
	}

	if got := normalizeHQBaseURL(req.HQ.BaseURL, req.HQ.Port); got != "http://127.0.0.1:3000" {
		t.Fatalf("expected target URL from new payload shape, got %q", got)
	}
	if got := strings.TrimSpace(req.LLMProvider); got != "openai" {
		t.Fatalf("expected provider name from new payload shape, got %q", got)
	}
	if got := strings.TrimSpace(req.ThinkingLevel); got != "high" {
		t.Fatalf("expected provider thought from new payload shape, got %q", got)
	}
}

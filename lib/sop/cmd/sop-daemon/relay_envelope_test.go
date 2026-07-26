package main

import "testing"

func TestComposeRelayEnvelopeUsesLegacyAndNewFields(t *testing.T) {
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
		InputParts:    []string{"first", "second"},
	}

	envelope := req.composeRelayEnvelope()

	if envelope.Query != "hello" {
		t.Fatalf("expected query to be trimmed, got %q", envelope.Query)
	}
	if envelope.Auth.AccessToken != "token" {
		t.Fatalf("expected access token to be trimmed, got %q", envelope.Auth.AccessToken)
	}
	if envelope.Target.BaseURL != "https://hq.example:8443" {
		t.Fatalf("expected normalized target URL, got %q", envelope.Target.BaseURL)
	}
	if envelope.Provider.Name != "anthropic" {
		t.Fatalf("expected provider name to be preserved, got %q", envelope.Provider.Name)
	}
	if envelope.Provider.Model != "claude-3.7" {
		t.Fatalf("expected provider model to be preserved, got %q", envelope.Provider.Model)
	}
	if len(envelope.InputParts) != 2 || envelope.InputParts[0] != "first" || envelope.InputParts[1] != "second" {
		t.Fatalf("expected input parts to be preserved, got %#v", envelope.InputParts)
	}
}

func TestComposeRelayEnvelopeUsesNewTargetAndProviderShape(t *testing.T) {
	req := UIHandoffRequest{
		Query: "",
		Target: struct {
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
		Provider: struct {
			Name    string `json:"name,omitempty"`
			APIKey  string `json:"api_key,omitempty"`
			Model   string `json:"model,omitempty"`
			URL     string `json:"url,omitempty"`
			Thought string `json:"thought,omitempty"`
		}{
			Name:    "openai",
			APIKey:  "secret",
			Model:   "gpt-4o",
			URL:     "https://api.openai.com",
			Thought: "high",
		},
	}

	envelope := req.composeRelayEnvelope()

	if envelope.Query != "connect" {
		t.Fatalf("expected empty query to default to connect, got %q", envelope.Query)
	}
	if envelope.Target.BaseURL != "http://127.0.0.1:3000" {
		t.Fatalf("expected target URL from new payload shape, got %q", envelope.Target.BaseURL)
	}
	if envelope.Provider.Name != "openai" {
		t.Fatalf("expected provider name from new payload shape, got %q", envelope.Provider.Name)
	}
	if envelope.Provider.Thought != "high" {
		t.Fatalf("expected provider thought from new payload shape, got %q", envelope.Provider.Thought)
	}
}

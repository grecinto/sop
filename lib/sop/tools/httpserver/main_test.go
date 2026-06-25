package main

import (
	"os"
	"strings"
	"testing"
)

func TestFloaterUsesSharedAppShell(t *testing.T) {
	scriptContent, err := os.ReadFile("templates/scripts_part05.html")
	if err != nil {
		t.Fatalf("failed to read chat script template: %v", err)
	}
	if !strings.Contains(string(scriptContent), "/app") {
		t.Fatalf("expected floater to open the shared app shell")
	}
	if !strings.Contains(string(scriptContent), "chatFloater") {
		t.Fatalf("expected floater to open the shared app shell in floater mode")
	}
}

func TestFloaterScriptHandlesInputStateAndMainWindowHide(t *testing.T) {
	scriptContent, err := os.ReadFile("templates/scripts_part05.html")
	if err != nil {
		t.Fatalf("failed to read chat script template: %v", err)
	}
	content := string(scriptContent)
	if !strings.Contains(content, "function handleChatInput") {
		t.Fatalf("expected floater script to manage input-driven send-button state")
	}
	if strings.Contains(content, "setDashboardVisibility(false)") {
		t.Fatalf("expected floater mode to leave the main dashboard launcher behavior intact")
	}
}

func TestFloaterIsLocalRuntimeOnly(t *testing.T) {
	modalContent, err := os.ReadFile("templates/modals.html")
	if err != nil {
		t.Fatalf("failed to read modals template: %v", err)
	}
	if !strings.Contains(string(modalContent), `id="chat-floater-btn"`) {
		t.Fatalf("expected floater button in modal template")
	}

	scriptContent, err := os.ReadFile("templates/scripts_part05.html")
	if err != nil {
		t.Fatalf("failed to read chat script template: %v", err)
	}
	if !strings.Contains(string(scriptContent), "Floater is only available in local runtime") {
		t.Fatalf("expected local-runtime-only floater guard in chat script template")
	}
	if !strings.Contains(string(scriptContent), "chatFloater") {
		t.Fatalf("expected floater to open the dedicated chat page")
	}
}

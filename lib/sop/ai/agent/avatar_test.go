package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/memory"
)

func TestBuildAvatarPrompt(t *testing.T) {
	agent := NewCopilotAgent(Config{}, nil, nil)
	agent.registerTools(context.Background()) // Populate registry with default tools
	ctx := context.WithValue(context.Background(), SessionPayloadKey, &ai.SessionPayload{CurrentDB: "system", Variables: map[string]any{}})

	avatarName := "test_expert"
	taskContext := "Analyze the database schema"
	customPersona := "You are a database testing Avatar."
	allowedTools := []string{"list_databases"}

	prompt := agent.buildAvatarPrompt(ctx, avatarName, taskContext, customPersona, allowedTools)

	// Asserts persona injection
	if !strings.Contains(prompt, "You are a database testing Avatar.") {
		t.Errorf("Expected custom persona injection, but got: %s", prompt)
	}

	// Asserts Task Context injection
	if !strings.Contains(prompt, "Task Context from Omni Supervisor:\nAnalyze the database schema") {
		t.Errorf("Expected task context injection, but got: %s", prompt)
	}

	// Asserts Tool injection
	if !strings.Contains(prompt, "ONLY use these allowed tools:") {
		t.Errorf("Expected allowed tools section, but got: %s", prompt)
	}
	if !strings.Contains(prompt, "- list_databases") {
		t.Errorf("Expected list_databases tool definition, but got: %s", prompt)
	}

	// Make sure it doesn't contain forbidden tools
	if strings.Contains(prompt, "- execute_shell_command") {
		t.Errorf("Avatar prompt should not contain unapproved tool execution instructions")
	}
}

func TestBuildAvatarPrompt_DefaultPersona(t *testing.T) {
	agent := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.WithValue(context.Background(), SessionPayloadKey, &ai.SessionPayload{CurrentDB: "system", Variables: map[string]any{}})

	avatarName := "blank_expert"
	taskContext := "Do something blank"

	prompt := agent.buildAvatarPrompt(ctx, avatarName, taskContext, "", nil)

	if !strings.Contains(prompt, "You are the blank_expert Avatar. Your task is strictly limited to your domain.") {
		t.Errorf("Expected default persona fallback, but got: %s", prompt)
	}

	// Should not have the tool section if no allowed tools are configured
	if strings.Contains(prompt, "ONLY use these allowed tools:") {
		t.Errorf("Expected no tool instruction for empty tools array, but got: %s", prompt)
	}
}

func TestBuildAvatarPrompt_IncludesSelectedSpaceExpertInstructions(t *testing.T) {
	sysDB := database.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}})
	ctx := context.Background()
	tx, err := sysDB.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	kb, err := sysDB.OpenKnowledgeBase(ctx, "medical", tx, nil, nil, false)
	if err != nil {
		t.Fatalf("open kb: %v", err)
	}
	if err := kb.SetConfig(ctx, &memory.KnowledgeBaseConfig{SystemPrompt: "You are the Medical Expert.", Description: "Medical diagnosis and triage guidance for patient-facing support."}); err != nil {
		t.Fatalf("set kb config: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	agent := NewCopilotAgent(Config{}, nil, sysDB)
	promptCtx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{
		CurrentDB: SystemDBName,
		SelectedKBs: []ai.ArtifactReference{{
			Name:         "medical",
			Type:         ai.ArtifactTypeSpace,
			DatabaseName: SystemDBName,
		}},
	})

	prompt := agent.buildAvatarPrompt(promptCtx, "medical", "Explain the diagnosis guidance", "", nil)

	if !strings.Contains(prompt, "Selected Space Expert Context") {
		t.Fatalf("expected selected-space guidance in avatar prompt, got: %s", prompt)
	}
	if !strings.Contains(prompt, "You are the Medical Expert.") {
		t.Fatalf("expected selected-space system prompt in avatar prompt, got: %s", prompt)
	}
	if !strings.Contains(prompt, "medical") {
		t.Fatalf("expected medical Space name in avatar prompt, got: %s", prompt)
	}
}

func TestBuildAvatarPrompt_IncludesStructuredSelectedSpaceExpertBrief(t *testing.T) {
	sysDB := database.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}})
	ctx := context.Background()
	tx, err := sysDB.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	kb, err := sysDB.OpenKnowledgeBase(ctx, "medical", tx, nil, nil, false)
	if err != nil {
		t.Fatalf("open kb: %v", err)
	}
	if err := kb.SetConfig(ctx, &memory.KnowledgeBaseConfig{SystemPrompt: "You are the Medical Expert.", Description: "Medical diagnosis and triage guidance for patient-facing support."}); err != nil {
		t.Fatalf("set kb config: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	agent := NewCopilotAgent(Config{}, nil, sysDB)
	promptCtx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{
		CurrentDB: SystemDBName,
		SelectedKBs: []ai.ArtifactReference{{
			Name:         "medical",
			Type:         ai.ArtifactTypeSpace,
			DatabaseName: SystemDBName,
		}},
	})

	prompt := agent.buildAvatarPrompt(promptCtx, "medical", "Explain the diagnosis guidance", "", nil)

	if !strings.Contains(prompt, "Structured Expert Brief") {
		t.Fatalf("expected structured expert brief in avatar prompt, got: %s", prompt)
	}
	if !strings.Contains(prompt, "Purpose:") {
		t.Fatalf("expected purpose section in expert brief, got: %s", prompt)
	}
	if !strings.Contains(prompt, "Common Constraints:") {
		t.Fatalf("expected common constraints section in expert brief, got: %s", prompt)
	}
}

func TestBuildAvatarPrompt_UsesRoutingGateContinuityForCompactExpertContext(t *testing.T) {
	sysDB := database.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}})
	ctx := context.Background()
	tx, err := sysDB.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	kb, err := sysDB.OpenKnowledgeBase(ctx, "medical", tx, nil, nil, false)
	if err != nil {
		t.Fatalf("open kb: %v", err)
	}
	if err := kb.SetConfig(ctx, &memory.KnowledgeBaseConfig{SystemPrompt: "You are the Medical Expert.", Description: "Medical diagnosis and triage guidance for patient-facing support."}); err != nil {
		t.Fatalf("set kb config: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	agent := NewCopilotAgent(Config{}, nil, sysDB)
	promptCtx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{
		CurrentDB: SystemDBName,
		SelectedKBs: []ai.ArtifactReference{{
			Name:         "medical",
			Type:         ai.ArtifactTypeSpace,
			DatabaseName: SystemDBName,
		}},
	})

	prompt := agent.buildAvatarPromptWithTaskContext(promptCtx, "medical", "Explain the diagnosis guidance further", "", nil, TaskContextClassification{RoutingGate: RoutingGateContinuity})
	if strings.Contains(prompt, "SystemPrompt: You are the Medical Expert.") {
		t.Fatalf("expected continuity routing to keep selected-space system prompt compact, got: %s", prompt)
	}
	if !strings.Contains(prompt, "Continuing with the selected knowledge base/Space \"medical\" or LTM memory") {
		t.Fatalf("expected continuity routing to preserve compact expert framing, got: %s", prompt)
	}
}

func TestBuildAvatarPrompt_DoesNotRepeatSelectedSpaceSystemPromptOnContinuation(t *testing.T) {
	sysDB := database.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}})
	ctx := context.Background()
	tx, err := sysDB.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	kb, err := sysDB.OpenKnowledgeBase(ctx, "medical", tx, nil, nil, false)
	if err != nil {
		t.Fatalf("open kb: %v", err)
	}
	if err := kb.SetConfig(ctx, &memory.KnowledgeBaseConfig{SystemPrompt: "You are the Medical Expert.", Description: "Medical diagnosis and triage guidance for patient-facing support."}); err != nil {
		t.Fatalf("set kb config: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	agent := NewCopilotAgent(Config{}, nil, sysDB)
	agent.service = &Service{session: NewRunnerSession(), EnableShortTermMemory: false}
	promptCtx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{
		CurrentDB: SystemDBName,
		SelectedKBs: []ai.ArtifactReference{{
			Name:         "medical",
			Type:         ai.ArtifactTypeSpace,
			DatabaseName: SystemDBName,
		}},
	})

	firstPrompt := agent.buildAvatarPrompt(promptCtx, "medical", "Explain the diagnosis guidance", "", nil)
	if !strings.Contains(firstPrompt, "SystemPrompt: You are the Medical Expert.") {
		t.Fatalf("expected initial avatar prompt to include selected-space system prompt, got: %s", firstPrompt)
	}

	secondPrompt := agent.buildAvatarPrompt(promptCtx, "medical", "Explain the diagnosis guidance further", "", nil)
	if strings.Contains(secondPrompt, "SystemPrompt: You are the Medical Expert.") {
		t.Fatalf("expected continuation avatar prompt to avoid repeating the selected-space system prompt, got: %s", secondPrompt)
	}
	if !strings.Contains(secondPrompt, "Continuing with the selected knowledge base/Space \"medical\" or LTM memory") {
		t.Fatalf("expected continuation avatar prompt to retain compact expert framing, got: %s", secondPrompt)
	}
}

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

func TestBuildSelectedSpaceResearchContextPartsUsesPriorSuccesses(t *testing.T) {
	state := &ResearchWorkflowState{
		SuccessfulCategoryPaths: []string{"Engineering/Code"},
		SuccessfulItemPaths:     []string{"Engineering/Code/Parsing"},
	}

	parts := buildSelectedSpaceResearchContextParts("fix parser bugs", state)
	if len(parts) == 0 {
		t.Fatalf("expected research context parts, got none")
	}

	joined := strings.Join(parts, "\n")
	if !strings.Contains(joined, "prior successful category paths") {
		t.Fatalf("expected prior category paths in research context, got %q", joined)
	}
	if !strings.Contains(joined, "Engineering/Code/Parsing") {
		t.Fatalf("expected prior item paths in research context, got %q", joined)
	}
	if !strings.Contains(joined, "fix parser bugs") {
		t.Fatalf("expected current ask to be included in research context, got %q", joined)
	}
}

func TestSelectedSpaceHasEvidenceRequiresRealGroundingSignals(t *testing.T) {
	if selectedSpaceHasEvidence("", "", "") {
		t.Fatal("expected no evidence when all selected-space signals are empty")
	}
	if !selectedSpaceHasEvidence("SystemPrompt: legal persona", "", "") {
		t.Fatal("expected selected-space config to count as evidence")
	}
	if !selectedSpaceHasEvidence("", "Structured Expert Brief:\nPurpose: legal review", "") {
		t.Fatal("expected selected-space brief to count as evidence")
	}
	if !selectedSpaceHasEvidence("", "", "Digest highlights:\n- relevant clause") {
		t.Fatal("expected selected-space digest to count as evidence")
	}
}

func TestBuildSystemPromptMentionsLTMOrSelectedKBAsKnowledgeSources(t *testing.T) {
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
	if err := kb.SetConfig(ctx, &memory.KnowledgeBaseConfig{SystemPrompt: "You are the Medical Expert."}); err != nil {
		t.Fatalf("set kb config: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	ag := NewCopilotAgent(Config{}, nil, sysDB)
	promptCtx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{
		CurrentDB: SystemDBName,
		SelectedKBs: []ai.ArtifactReference{{
			Name:         "medical",
			Type:         ai.ArtifactTypeSpace,
			DatabaseName: SystemDBName,
		}},
	})

	prompt := ag.buildSystemPrompt(promptCtx, "Explain the diagnosis guidance", TaskContextClassification{})
	if !strings.Contains(prompt, "selected knowledge base/Space") || !strings.Contains(prompt, "LTM memory") {
		t.Fatalf("expected prompt to describe the knowledge source as either LTM or a selected knowledge base/Space, got: %s", prompt)
	}
}

func TestSelectedSpaceConfigContextHidesCategorizerPolicySummary(t *testing.T) {
	sysDB := database.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}})
	ctx := context.Background()
	tx, err := sysDB.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	kb, err := sysDB.OpenKnowledgeBase(ctx, "policy_space", tx, nil, nil, false)
	if err != nil {
		t.Fatalf("open kb: %v", err)
	}
	if err := kb.SetConfig(ctx, &memory.KnowledgeBaseConfig{
		Description: "Policy space",
		CategorizerConfig: &memory.CategorizerConfig{
			Type: "rule",
			Default: map[string]any{
				"categories": []any{map[string]any{"path": "Coding Best Practices/Go", "keywords": []any{"go", "context"}}},
			},
			Fallback: "uncategorized",
		},
	}); err != nil {
		t.Fatalf("set kb config: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	ag := NewCopilotAgent(Config{}, nil, sysDB)
	contextText := ag.selectedSpaceConfigContext(ctx, ai.ArtifactReference{Name: "policy_space", DatabaseName: SystemDBName})
	if contextText == "" {
		t.Fatal("expected selected-space config context")
	}
	if strings.Contains(contextText, "Categorizer") || strings.Contains(contextText, "rule") || strings.Contains(contextText, "Coding Best Practices/Go") {
		t.Fatalf("expected selected-space config to hide categorizer policy details, got: %s", contextText)
	}
}

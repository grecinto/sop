package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/teampatterns"
	"github.com/sharedcode/sop/ai/memory"
)

// StoreContinueTeamPractice persists a Continue-originated team practice in the Team Patterns LTM tree.
func (a *CopilotAgent) StoreContinueTeamPractice(ctx context.Context, practice string) (*teampatterns.TeamPattern, error) {
	if a == nil {
		return nil, fmt.Errorf("agent is required")
	}
	if a.systemDB == nil {
		return nil, fmt.Errorf("systemDB not available")
	}
	content := strings.TrimSpace(practice)
	if content == "" {
		return nil, fmt.Errorf("team practice content is required")
	}
	if a.Memory == nil {
		a.Memory = memory.NewMemoryUnit(ai.AgentIDOmni)
	}
	a.Memory.BindSession(ctx)

	pattern := &teampatterns.TeamPattern{
		PatternName: "Memory",
		PatternType: "continue",
		Convention:  content,
		Reasoning:   "Captured from a Continue team practice prompt.",
		WhenToUse:   "When preserving a team practice or best practice from Continue.",
		ScopeType:   "team",
		ScopeID:     strings.TrimSpace(a.Memory.AgentID),
		Status:      "active",
		Tags:        []string{"continue", "team-practice"},
	}

	var embedder ai.Embeddings
	if a.service != nil && a.service.Domain() != nil {
		embedder = a.service.Domain().Embedder()
	}

	tx, err := a.systemDB.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		return nil, fmt.Errorf("begin team practice transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	ltm, err := a.Memory.OpenLongTermMemory(ctx, a.systemDB, tx, a.brain, embedder)
	if err != nil {
		return nil, fmt.Errorf("open team patterns ltm: %w", err)
	}
	if err := teampatterns.StoreTeamPattern(ctx, ltm, pattern); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit team practice transaction: %w", err)
	}

	if embedder != nil {
		_ = teampatterns.VectorizeTeamPatterns(ctx, a.systemDB, a.Memory.LongTermMemoryName(), a.brain, embedder)
	}

	return pattern, nil
}

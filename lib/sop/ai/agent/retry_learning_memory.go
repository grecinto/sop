package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/database"
)

const retryLearningStoreName = "retry_learnings"

func retryLearningScopeKey(provider, model, domain, repo string) string {
	parts := []string{
		strings.TrimSpace(provider),
		strings.TrimSpace(model),
		strings.TrimSpace(domain),
		strings.TrimSpace(repo),
	}
	for i := range parts {
		if parts[i] == "" {
			parts[i] = "_"
		}
	}
	return strings.Join(parts, "::")
}

func loadPersistentRetryLearnings(ctx context.Context, systemDB *database.Database, payload *ai.SessionPayload, provider, model string) ([]ai.RetryLearning, error) {
	if systemDB == nil || payload == nil || strings.TrimSpace(payload.UserID) == "" || strings.TrimSpace(provider) == "" {
		return nil, nil
	}
	tx, err := systemDB.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		return nil, fmt.Errorf("begin retry learning read tx: %w", err)
	}
	defer tx.Rollback(ctx)
	store, err := systemDB.OpenModelStore(ctx, retryLearningStoreName, tx)
	if err != nil {
		return nil, fmt.Errorf("open retry learning store: %w", err)
	}
	category := preferenceCategory(payload)
	keys := []string{
		retryLearningScopeKey(provider, model, payload.ActiveDomain, payload.ActiveRepo),
		retryLearningScopeKey(provider, model, "", ""),
	}
	merged := make([]ai.RetryLearning, 0)
	seen := make(map[string]struct{})
	for _, key := range keys {
		var stored []ai.RetryLearning
		if err := store.Load(ctx, category, key, &stored); err != nil {
			if strings.Contains(err.Error(), "not found") {
				continue
			}
			return nil, fmt.Errorf("load retry learnings: %w", err)
		}
		for _, learning := range stored {
			fingerprint := learning.Provider + "|" + learning.Model + "|" + learning.DomainScope + "|" + learning.RepoScope + "|" + learning.ToolName + "|" + learning.ErrorPattern + "|" + string(learning.PreferredStrategy)
			if _, ok := seen[fingerprint]; ok {
				continue
			}
			seen[fingerprint] = struct{}{}
			merged = append(merged, learning)
		}
	}
	return merged, nil
}

func persistRetryLearnings(ctx context.Context, systemDB *database.Database, payload *ai.SessionPayload, provider, model string, learnings []ai.RetryLearning) error {
	if systemDB == nil || payload == nil || strings.TrimSpace(payload.UserID) == "" || strings.TrimSpace(provider) == "" || len(learnings) == 0 {
		return nil
	}
	eligible := make([]ai.RetryLearning, 0, len(learnings))
	for _, learning := range learnings {
		if learning.SuccessCount < 2 || learning.SuccessCount <= learning.FailureCount {
			continue
		}
		clone := learning
		clone.Provider = provider
		clone.Model = model
		clone.DomainScope = strings.TrimSpace(payload.ActiveDomain)
		clone.RepoScope = strings.TrimSpace(payload.ActiveRepo)
		eligible = append(eligible, clone)
	}
	if len(eligible) == 0 {
		return nil
	}
	tx, err := systemDB.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		return fmt.Errorf("begin retry learning write tx: %w", err)
	}
	defer tx.Rollback(ctx)
	store, err := systemDB.OpenModelStore(ctx, retryLearningStoreName, tx)
	if err != nil {
		return fmt.Errorf("open retry learning store: %w", err)
	}
	category := preferenceCategory(payload)
	keys := []string{
		retryLearningScopeKey(provider, model, payload.ActiveDomain, payload.ActiveRepo),
		retryLearningScopeKey(provider, model, "", ""),
	}
	for _, key := range keys {
		if err := store.Save(ctx, category, key, eligible); err != nil {
			return fmt.Errorf("save retry learnings: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func hydrateRetryLearningsIntoCarryover(ctx context.Context, systemDB *database.Database, payload *ai.SessionPayload, gen ai.Generator, carryover *ai.CarryoverState) *ai.CarryoverState {
	if gen == nil {
		return carryover
	}
	provider := strings.TrimSpace(gen.Name())
	model := ""
	if capability, ok := carryoverCapability(gen); ok {
		model = strings.TrimSpace(capability.Model)
	}
	learnings, err := loadPersistentRetryLearnings(ctx, systemDB, payload, provider, model)
	if err != nil || len(learnings) == 0 {
		return carryover
	}
	if carryover == nil {
		carryover = &ai.CarryoverState{}
	}
	if len(carryover.RetryLearnings) == 0 {
		carryover.RetryLearnings = append([]ai.RetryLearning(nil), learnings...)
	}
	return carryover
}

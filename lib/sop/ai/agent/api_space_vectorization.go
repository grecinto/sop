package agent

import (
	"context"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/memory"
)

const categoryRefreshBatchSize = 10

func (a *CopilotAgent) refreshCategoryVectorsForMutation(
	ctx context.Context,
	db *database.Database,
	kbName string,
	categoryIDs []sop.UUID,
	embedder ai.Embeddings,
) error {
	if a == nil || db == nil || kbName == "" || embedder == nil {
		return nil
	}

	seen := make(map[sop.UUID]struct{}, len(categoryIDs))
	deduped := make([]sop.UUID, 0, len(categoryIDs))
	for _, id := range categoryIDs {
		if id.IsNil() {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		deduped = append(deduped, id)
	}
	if len(deduped) == 0 {
		return nil
	}

	return db.VectorizeCategories(ctx, kbName, a.brain, embedder, categoryRefreshBatchSize, deduped)
}

func (a *CopilotAgent) resolveCategoryIDsForItemMutation(
	ctx context.Context,
	kb *memory.KnowledgeBase[map[string]any],
	params []memory.UpsertItemParam[map[string]any],
) []sop.UUID {
	if a == nil || kb == nil || len(params) == 0 {
		return nil
	}

	seen := make(map[sop.UUID]struct{}, len(params))
	categoryIDs := make([]sop.UUID, 0, len(params))
	for _, param := range params {
		catID := param.CategoryID
		if catID.IsNil() && param.CategoryPath != "" {
			catsByPath, err := kb.Store.CategoriesByPath(ctx)
			if err == nil {
				if found, _ := catsByPath.Find(ctx, param.CategoryPath, false); found {
					catID, _ = catsByPath.GetCurrentValue(ctx)
				}
			}
		}
		if catID.IsNil() {
			continue
		}
		if _, ok := seen[catID]; ok {
			continue
		}
		seen[catID] = struct{}{}
		categoryIDs = append(categoryIDs, catID)
	}
	return categoryIDs
}

func collectCategoryIDsFromCategoryParams(params []memory.UpsertCategoryParam) []sop.UUID {
	if len(params) == 0 {
		return nil
	}
	seen := make(map[sop.UUID]struct{}, len(params))
	categoryIDs := make([]sop.UUID, 0, len(params))
	for _, param := range params {
		if param.Category == nil {
			continue
		}
		catID := param.Category.ID
		if catID.IsNil() {
			continue
		}
		if _, ok := seen[catID]; ok {
			continue
		}
		seen[catID] = struct{}{}
		categoryIDs = append(categoryIDs, catID)
	}
	return categoryIDs
}

package memory

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/embed"
)

// KnowledgeBase provides a clean, unified API for developers.
// It orchestrates both the storage tables and the LLM memory management.
type KnowledgeBase[T any] struct {
	Store   MemoryStore[T]
	Manager *MemoryManager[T]
	// MaxMathCategoryDistance specifies the max Euclidean distance to cluster centroids
	// to avoid calling the LLM for category categorization. Set to 0.0 or less to disable
	// and always rely on "pristine" LLM categorization.
	MaxMathCategoryDistance float32

	configCache *KnowledgeBaseConfig
}

// Returns this KnowledgeBase's name.
func (kb *KnowledgeBase[T]) Name() string {
	return kb.Store.Name()
}

func isNilGenericValue[T any](v T) bool {
	value := reflect.ValueOf(v)
	if !value.IsValid() {
		return true
	}
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func uniqueCategories(cats []*Category) []*Category {
	seen := make(map[sop.UUID]struct{}, len(cats))
	unique := make([]*Category, 0, len(cats))
	for _, cat := range cats {
		if cat == nil || cat.ID.IsNil() {
			continue
		}
		if _, ok := seen[cat.ID]; ok {
			continue
		}
		seen[cat.ID] = struct{}{}
		unique = append(unique, cat)
	}
	return unique
}

// findCategoryByPath tries lexical lookup first, then semantic fallback via CategoryText embedding.
func (kb *KnowledgeBase[T]) findCategoryByPath(ctx context.Context, path string) ([]*Category, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}

	// Try lexical lookup
	catsByPath, err := kb.Store.CategoriesByPath(ctx)
	if err != nil {
		return nil, err
	}
	found, err := catsByPath.Find(ctx, path, false)
	if err != nil {
		return nil, err
	}
	if found {
		catID, err := catsByPath.GetCurrentValue(ctx)
		if err != nil {
			return nil, err
		}
		cats, err := kb.Store.Categories(ctx)
		if err != nil {
			return nil, err
		}
		foundCat, err := cats.Find(ctx, catID, false)
		if err != nil {
			return nil, err
		}
		if foundCat {
			cat, err := cats.GetCurrentValue(ctx)
			if err == nil && cat != nil && !cat.ID.IsNil() {
				return []*Category{cat}, nil
			}
		}
	}

	// Semantic fallback via CategoryByDistance.
	// If the KB has not been vectorized yet, skip the semantic path instead of failing the lookup.
	if kb.Manager != nil && kb.Manager.embedder != nil {
		partsVecs, err := embedCategoryPath(ctx, path, kb.Manager.embedder)
		if err != nil {
			return nil, err
		}
		cats, err := kb.Store.SemanticCategoryByPath(ctx, partsVecs)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "domain reference vector is not set") {
				return nil, nil
			}
			return nil, err
		}
		return cats, nil
	}

	return nil, nil
}

func NormalizeCategoryToken(text string) string {
	text = strings.TrimSpace(text)
	text = strings.ToLower(text)
	text = strings.NewReplacer(
		"c#", "csharp",
		"c++", "cpp",
		".net", "dotnet",
		"&", " and ",
		"＆", " and ",
		"/", " ",
		"／", " ",
		"\\", " ",
		">", " ",
		"|", " ",
		"-", " ",
		"_", " ",
		":", " ",
		"：", " ",
		";", " ",
		"；", " ",
		"(", " ",
		")", " ",
		"（", " ",
		"）", " ",
		"[", " ",
		"]", " ",
		"【", " ",
		"】", " ",
		"{", " ",
		"}", " ",
		"+", " ",
		"=", " ",
		"@", " ",
		"#", " ",
		"%", " ",
		"$", " ",
		"!", " ",
		"?", " ",
		".", " ",
		",", " ",
		"'", " ",
		"\"", " ",
		"，", " ",
		"。", " ",
		"！", " ",
		"？", " ",
	).Replace(text)
	text = strings.Map(func(r rune) rune {
		switch {
		case r >= 'Ａ' && r <= 'Ｚ':
			return r - 'Ａ' + 'A'
		case r >= 'ａ' && r <= 'ｚ':
			return r - 'ａ' + 'a'
		case r >= '０' && r <= '９':
			return r - '０' + '0'
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r):
			return r
		default:
			return ' '
		}
	}, text)
	text = strings.Join(strings.Fields(text), " ")
	return text
}

func normalize(text string) string {
	return NormalizeCategoryToken(text)
}

func embedCategoryPath(ctx context.Context, catPath string, embedder ai.Embeddings) ([][]float32, error) {
	return CategoryPathVectors(ctx, catPath, embedder)
}

func CategoryPathVectors(ctx context.Context, catPath string, embedder ai.Embeddings) ([][]float32, error) {
	parts := strings.Split(catPath, "/")
	if len(parts) == 0 || (len(parts) == 1 && strings.TrimSpace(parts[0]) == "") {
		parts = strings.Split(catPath, "\\")
	}
	if len(parts) == 0 || (len(parts) == 1 && strings.TrimSpace(parts[0]) == "") {
		return nil, nil
	}

	cleanParts := make([]string, 0, len(parts))
	for _, part := range parts {
		cleanPart := normalize(part)
		if cleanPart != "" {
			cleanParts = append(cleanParts, cleanPart)
		}
	}
	if len(cleanParts) == 0 {
		return nil, nil
	}

	vecs, err := embed.CategoryTexts(ctx, embedder, cleanParts)
	if err != nil {
		return nil, err
	}
	return vecs, nil
}

// findCategoriesByTextSearch extracts categories from text search results.
func (kb *KnowledgeBase[T]) findCategoriesByTextSearch(ctx context.Context, text string, limit int) ([]*Category, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}

	// Delegate to store to extract categories from text index
	if s, ok := kb.Store.(interface {
		GetCategoriesFromTextSearch(context.Context, string) ([]*Category, error)
	}); ok {
		return s.GetCategoriesFromTextSearch(ctx, text)
	}

	return nil, nil
}

func (kb *KnowledgeBase[T]) searchTextWithinCategory(ctx context.Context, category *Category, text string, limit int) ([]ai.Hit[T], error) {
	if kb == nil || kb.Store == nil || category == nil || category.ID.IsNil() || strings.TrimSpace(text) == "" {
		return nil, nil
	}

	s, ok := kb.Store.(*store[T])
	if !ok || s == nil || s.textIndex == nil {
		return nil, nil
	}

	searchResults, err := s.textIndex.Search(ctx, text)
	if err != nil {
		return nil, err
	}

	hits := make([]ai.Hit[T], 0, len(searchResults))
	for _, res := range searchResults {
		if limit > 0 && len(hits) >= limit {
			break
		}
		parts := strings.Split(res.DocID, ",")
		if len(parts) != 2 {
			continue
		}
		catID, err := sop.ParseUUID(parts[0])
		if err != nil || catID.Compare(category.ID) != 0 {
			continue
		}
		itemID, err := sop.ParseUUID(parts[1])
		if err != nil {
			continue
		}

		itemKey := ItemKey{CategoryID: category.ID, ItemID: itemID}
		foundItem, err := s.items.Find(ctx, itemKey, false)
		if err != nil || !foundItem {
			continue
		}
		item, err := s.items.GetCurrentValue(ctx)
		if err != nil || item.ID.IsNil() {
			continue
		}

		hits = append(hits, ai.Hit[T]{
			ID:      item.ID.String(),
			DocID:   item.DocID,
			Score:   float32(res.Score),
			Payload: item.Data,
		})
	}
	return hits, nil
}

func hydrateSearchHitsWithDocumentContent[T any](ctx context.Context, kb *KnowledgeBase[T], hits []ai.Hit[T]) {
	if kb == nil || kb.Store == nil {
		return
	}
	for i := range hits {
		hits[i].DocumentContent = ResolveHitText(ctx, kb, hits[i].Payload, hits[i].DocID)
		hits[i].SourceLabel = ResolveHitSourceLabel(ctx, kb, hits[i].DocID)
	}
}

func parentCategoryPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) <= 1 {
		return ""
	}
	parentParts := parts[:len(parts)-1]
	return strings.Join(parentParts, "/")
}

func mergeSearchHits[T any](existing, incoming []ai.Hit[T]) []ai.Hit[T] {
	merged := make([]ai.Hit[T], 0, len(existing)+len(incoming))
	merged = append(merged, existing...)
	seen := make(map[string]int, len(merged))
	for i, hit := range merged {
		seen[searchHitIdentity(hit)] = i
	}

	for _, hit := range incoming {
		key := searchHitIdentity(hit)
		if idx, ok := seen[key]; ok {
			current := &merged[idx]
			if hit.Score > current.Score {
				current.Score = hit.Score
			} else {
				current.Score += 0.01
			}
			if strings.TrimSpace(current.ID) == "" && strings.TrimSpace(hit.ID) != "" {
				current.ID = hit.ID
			}
			if len(current.DocID) == 0 && len(hit.DocID) > 0 {
				current.DocID = append([]string(nil), hit.DocID...)
			}
			if current.DocumentContent == "" && hit.DocumentContent != "" {
				current.DocumentContent = hit.DocumentContent
			}
			if current.SourceLabel == "" && hit.SourceLabel != "" {
				current.SourceLabel = hit.SourceLabel
			}
			continue
		}
		seen[key] = len(merged)
		merged = append(merged, hit)
	}
	return merged
}

func searchHitIdentity[T any](hit ai.Hit[T]) string {
	if strings.TrimSpace(hit.ID) != "" {
		return "id:" + hit.ID
	}
	if len(hit.DocID) > 0 {
		return "docid:" + strings.Join(hit.DocID, "|")
	}
	return "payload:" + fmt.Sprintf("%T:%v", hit.Payload, hit.Payload)
}

func searchHitEvidence[T any](hit *ai.Hit[T], queryText string, pathHints []string) (lexical int, pathMatches int) {
	if hit == nil {
		return 0, 0
	}
	payloadText := payloadTextForHit(hit)
	if strings.TrimSpace(queryText) != "" {
		queryTokens := tokenizeSearchText(queryText)
		payloadTokens := tokenizeSearchText(payloadText)
		seen := make(map[string]struct{}, len(queryTokens))
		for _, q := range queryTokens {
			seen[q] = struct{}{}
		}
		for _, p := range payloadTokens {
			if _, ok := seen[p]; ok {
				lexical++
			}
		}
		for _, path := range pathHints {
			if strings.TrimSpace(path) == "" {
				continue
			}
			pathTokens := tokenizeSearchText(path)
			for _, token := range pathTokens {
				if _, ok := seen[token]; ok {
					pathMatches++
					break
				}
			}
		}
	}
	if pathMatches == 0 {
		for _, path := range pathHints {
			if strings.TrimSpace(path) == "" {
				continue
			}
			if strings.Contains(strings.ToLower(payloadText), strings.ToLower(path)) {
				pathMatches++
			}
		}
	}
	return lexical, pathMatches
}

func boostSearchHitScore[T any](hit ai.Hit[T], queryText string, pathHints []string) ai.Hit[T] {
	lexical, pathMatches := searchHitEvidence(&hit, queryText, pathHints)
	if lexical > 0 || pathMatches > 0 {
		boost := float32(lexical)*0.12 + float32(pathMatches)*0.08
		hit.Score = hit.Score - boost
	}
	return hit
}

func categorySpecificityBoost(category *Category) float32 {
	if category == nil {
		return 0
	}
	depth := 1
	if strings.TrimSpace(category.Path) != "" {
		depth += strings.Count(category.Path, "/")
	}
	if len(category.ParentIDs) > 0 {
		depth += len(category.ParentIDs)
	}
	return float32(depth) * 0.01
}

func applyCategorySpecificityBoost[T any](hits []ai.Hit[T], category *Category) {
	boost := categorySpecificityBoost(category)
	if boost <= 0 || len(hits) == 0 {
		return
	}
	for i := range hits {
		hits[i].Score = hits[i].Score - boost
	}
}

func applyCategoryPositionPenalty[T any](hits []ai.Hit[T], candidateIndex int) {
	if len(hits) == 0 || candidateIndex <= 0 {
		return
	}
	penalty := float32(candidateIndex) * 0.01
	for i := range hits {
		hits[i].Score = hits[i].Score - penalty
	}
}

func tokenizeSearchText(text string) []string {
	trimmed := normalize(text)
	if trimmed == "" {
		return nil
	}
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return nil
	}
	return parts
}

func payloadTextForHit[T any](hit *ai.Hit[T]) string {
	if hit == nil {
		return ""
	}
	if payload, ok := any(hit.Payload).(string); ok {
		return payload
	}
	if payload, ok := any(hit.Payload).(map[string]any); ok {
		if text, ok := payload["text"].(string); ok {
			return text
		}
	}
	return fmt.Sprintf("%v", hit.Payload)
}

func searchOptionsSummary[T any](opts *SearchOptions[T]) []any {
	if opts == nil {
		return []any{"limit", 0, "category_path", "", "has_filter", false}
	}
	return []any{"limit", opts.Limit, "category_path", opts.CategoryPath, "has_filter", opts.Filter != nil}
}

func containsCategory(cats []*Category, target *Category) bool {
	if target == nil {
		return false
	}
	for _, cat := range cats {
		if cat == nil {
			continue
		}
		if cat.ID == target.ID {
			return true
		}
	}
	return false
}

func appendParentCategories[T any](ctx context.Context, cats []*Category, seen map[sop.UUID]struct{}, out *[]*Category, store *store[T]) {
	if store == nil {
		return
	}

	var walk func([]*Category, int)
	walk = func(current []*Category, depth int) {
		if len(current) == 0 || depth >= 4 {
			return
		}
		var next []*Category
		for _, cat := range current {
			if cat == nil || cat.ID.IsNil() {
				continue
			}
			for _, parent := range cat.ParentIDs {
				if parent.ParentID.IsNil() {
					continue
				}
				if _, ok := seen[parent.ParentID]; ok {
					continue
				}
				seen[parent.ParentID] = struct{}{}
				found, err := store.categories.Find(ctx, parent.ParentID, false)
				if err != nil || !found {
					continue
				}
				parentCat, err := store.categories.GetCurrentValue(ctx)
				if err != nil || parentCat == nil || parentCat.ID.IsNil() {
					continue
				}
				*out = append(*out, parentCat)
				next = append(next, parentCat)
			}
		}
		walk(next, depth+1)
	}

	walk(cats, 0)
}

func uniqueCategoryPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

// Search provides one reusable entry point for single or batch retrieval.
// Precedence:
// 1. If CategoryPath specified: try CategoryByPath, fallback to CategoryByDistance
// 2. If no category yet and Text present: use CategoryText embedding to get resolved in CategoryByDistance + TextSearch categories. (BOTH)
// 3. If no category found: short circuit and return empty
// 4. Use found categories to do vector search
// 5. Return matching items
func (kb *KnowledgeBase[T]) Search(ctx context.Context, requests []SearchRequest[T]) ([][]ai.Hit[T], error) {
	if len(requests) == 0 {
		return nil, nil
	}

	results := make([][]ai.Hit[T], 0, len(requests))
	for _, req := range requests {
		var candidates []*Category

		// Step 1: If CategoryPath(s) specified, try CategoryByPath then CategoryByDistance
		categoryPaths := make([]string, 0, 1+len(req.CategoryPaths))
		if strings.TrimSpace(req.CategoryPath) != "" {
			categoryPaths = append(categoryPaths, req.CategoryPath)
		}
		for _, path := range req.CategoryPaths {
			if strings.TrimSpace(path) != "" {
				categoryPaths = append(categoryPaths, path)
			}
		}
		for _, path := range uniqueCategoryPaths(categoryPaths) {
			cats, err := kb.findCategoryByPath(ctx, path)
			if err != nil {
				return nil, err
			}
			if len(cats) > 0 {
				candidates = append(candidates, cats...)
			}
		}

		// Step 2: If no category yet and Text present, use CategoryText embedding to get resolved in CategoryByDistance + TextSearch categories. (BOTH)
		if len(candidates) == 0 && strings.TrimSpace(req.Text) != "" {
			if kb.Manager != nil && kb.Manager.retrievalRouter != nil {
				if catName, err := kb.Manager.Categorize(req.Text, CategorizeOptions{Mode: CategorizerModeRetrieval}); err == nil && strings.TrimSpace(catName) != "" {
					if cat, err := kb.Manager.EnsureCategory(ctx, catName); err == nil && cat != sop.NilUUID {
						cats, err := kb.Store.Categories(ctx)
						if err == nil {
							if found, _ := cats.Find(ctx, cat, false); found {
								if current, err := cats.GetCurrentValue(ctx); err == nil && current != nil {
									candidates = append(candidates, current)
								}
							}
						}
					}
				}
			}
			if kb.Manager != nil {
				if confCat, ok := kb.Manager.categorizer.(ConfidenceCategorizer); ok {
					if res, err := confCat.CategorizeWithConfidence(req.Text, CategorizeOptions{Mode: CategorizerModeRetrieval}); err == nil && strings.TrimSpace(res.Category) != "" {
						if res.Confidence >= 0.7 {
							if cat, err := kb.Manager.EnsureCategory(ctx, res.Category); err == nil && cat != sop.NilUUID {
								cats, err := kb.Store.Categories(ctx)
								if err == nil {
									if found, _ := cats.Find(ctx, cat, false); found {
										if current, err := cats.GetCurrentValue(ctx); err == nil && current != nil {
											candidates = append(candidates, current)
										}
									}
								}
							}
						} else if parent := parentCategoryPath(res.Category); parent != "" {
							if cat, err := kb.Manager.EnsureCategory(ctx, parent); err == nil && cat != sop.NilUUID {
								cats, err := kb.Store.Categories(ctx)
								if err == nil {
									if found, _ := cats.Find(ctx, cat, false); found {
										if current, err := cats.GetCurrentValue(ctx); err == nil && current != nil {
											candidates = append(candidates, current)
										}
									}
								}
							}
						}
					}
				} else if catName, err := kb.Manager.Categorize(req.Text, CategorizeOptions{Mode: CategorizerModeRetrieval}); err == nil && strings.TrimSpace(catName) != "" {
					if cat, err := kb.Manager.EnsureCategory(ctx, catName); err == nil && cat != sop.NilUUID {
						cats, err := kb.Store.Categories(ctx)
						if err == nil {
							if found, _ := cats.Find(ctx, cat, false); found {
								if current, err := cats.GetCurrentValue(ctx); err == nil && current != nil {
									candidates = append(candidates, current)
								}
							}
						}
					}
				}
			}

			// Use CategoryText embedding to get resolved in CategoryByDistance
			if kb.Manager != nil && kb.Manager.embedder != nil {
				vecs, err := embed.CategoryTexts(ctx, kb.Manager.embedder, []string{normalize(req.Text)})
				if err == nil && len(vecs) > 0 {
					cat, _, err := kb.Manager.FindClosestCategory(ctx, vecs[0])
					if err == nil && cat != nil {
						candidates = append(candidates, cat)
					}
				}
			}

			// ALSO get categories from TextSearch (BOTH paths, not fallback)
			textCats, err := kb.findCategoriesByTextSearch(ctx, req.Text, req.Limit)
			if err == nil && len(textCats) > 0 {
				candidates = append(candidates, textCats...)
			}
		}

		// Step 3: Short circuit if no category found
		if len(candidates) == 0 {
			continue
		}

		// Step 4: Use found categories to do vector search
		queryVector := req.Vector
		if len(queryVector) == 0 && strings.TrimSpace(req.Text) != "" && kb.Manager != nil && kb.Manager.embedder != nil {
			vecs, err := embed.QueryTexts(ctx, kb.Manager.embedder, []string{normalize(req.Text)})
			if err == nil && len(vecs) > 0 {
				queryVector = vecs[0]
			}
		}

		var hits []ai.Hit[T]

		candidates = uniqueCategories(candidates)
		pathHints := make([]string, 0)
		if kb.Manager != nil && kb.Manager.retrievalRouter != nil && len(candidates) > 0 {
			pathNames := make([]string, 0, len(candidates))
			for _, cat := range candidates {
				if cat != nil {
					if strings.TrimSpace(cat.Path) != "" {
						pathNames = append(pathNames, cat.Path)
					} else if strings.TrimSpace(cat.Name) != "" {
						pathNames = append(pathNames, cat.Name)
					}
				}
			}
			if len(pathNames) > 0 {
				orderedPaths := kb.Manager.retrievalRouter.RankedPaths(pathNames)
				if len(orderedPaths) > 0 {
					ordered := make([]*Category, 0, len(candidates))
					for _, path := range orderedPaths {
						for _, cat := range candidates {
							if cat == nil {
								continue
							}
							if strings.TrimSpace(cat.Path) == path || strings.TrimSpace(cat.Name) == path {
								ordered = append(ordered, cat)
								break
							}
						}
					}
					for _, cat := range candidates {
						if cat != nil && !containsCategory(ordered, cat) {
							ordered = append(ordered, cat)
						}
					}
					candidates = ordered
				}
			}
			pathHints = append(pathHints, pathNames...)
		}
		for catIndex, cat := range candidates {
			catOpts := &SearchOptions[T]{
				Limit:          req.Limit,
				CategoryVector: cat.CenterVector,
				Filter:         req.Filter,
			}
			catHits, err := kb.Store.QueryItems(ctx, queryVector, cat, catOpts)
			if err != nil {
				return nil, err
			}

			if len(catHits) == 0 {
				if strings.TrimSpace(req.Text) != "" {
					catTextHits, err := kb.searchTextWithinCategory(ctx, cat, req.Text, req.Limit)
					if err != nil {
						return nil, err
					}
					hits = mergeSearchHits(hits, catTextHits)
				}
				continue
			}

			// Boost items from better-matching categories by incorporating category distance
			if len(queryVector) > 0 && len(cat.CenterVector) > 0 {
				categoryDist := Distance(queryVector, cat.CenterVector, true)
				// Adjust scores: smaller category distance = better match = higher score
				// Category distance penalty scaled to keep item scores dominant
				for i := range catHits {
					catHits[i].Score = catHits[i].Score - (categoryDist * 0.1)
				}
			}
			applyCategorySpecificityBoost(catHits, cat)
			applyCategoryPositionPenalty(catHits, catIndex)
			hits = mergeSearchHits(hits, catHits)

			if strings.TrimSpace(req.Text) != "" {
				catTextHits, err := kb.searchTextWithinCategory(ctx, cat, req.Text, req.Limit)
				if err != nil {
					return nil, err
				}
				applyCategorySpecificityBoost(catTextHits, cat)
				hits = mergeSearchHits(hits, catTextHits)
			}
		}

		if len(hits) == 0 && len(candidates) > 0 && strings.TrimSpace(req.Text) != "" {
			seenParents := make(map[sop.UUID]struct{}, len(candidates))
			for _, cat := range candidates {
				if cat != nil && !cat.ID.IsNil() {
					seenParents[cat.ID] = struct{}{}
				}
			}
			var parentCandidates []*Category
			if store, ok := kb.Store.(*store[T]); ok {
				appendParentCategories(ctx, candidates, seenParents, &parentCandidates, store)
			}
			for _, parent := range parentCandidates {
				if parent == nil || parent.ID.IsNil() || containsCategory(candidates, parent) {
					continue
				}
				catHits, err := kb.Store.QueryItems(ctx, queryVector, parent, &SearchOptions[T]{Limit: req.Limit, CategoryVector: parent.CenterVector, Filter: req.Filter})
				if err != nil {
					return nil, err
				}
				applyCategorySpecificityBoost(catHits, parent)
				applyCategoryPositionPenalty(catHits, len(candidates))
				hits = mergeSearchHits(hits, catHits)
				if len(catHits) == 0 {
					catTextHits, err := kb.searchTextWithinCategory(ctx, parent, req.Text, req.Limit)
					if err != nil {
						return nil, err
					}
					applyCategorySpecificityBoost(catTextHits, parent)
					applyCategoryPositionPenalty(catTextHits, len(candidates))
					hits = mergeSearchHits(hits, catTextHits)
				}
			}
		}

		if len(hits) == 0 && len(candidates) > 0 {
			fallbackVector := queryVector
			if len(fallbackVector) == 0 && strings.TrimSpace(req.Text) != "" && kb.Manager != nil && kb.Manager.embedder != nil {
				vecs, err := embed.QueryTexts(ctx, kb.Manager.embedder, []string{normalize(req.Text)})
				if err == nil && len(vecs) > 0 {
					fallbackVector = vecs[0]
				}
			}
			if len(fallbackVector) > 0 {
				fallbackCat, _, err := kb.Store.FindClosestCategory(ctx, fallbackVector)
				if err == nil && fallbackCat != nil && !containsCategory(candidates, fallbackCat) {
					catHits, err := kb.Store.QueryItems(ctx, fallbackVector, fallbackCat, &SearchOptions[T]{Limit: req.Limit, CategoryVector: fallbackCat.CenterVector, Filter: req.Filter})
					if err != nil {
						return nil, err
					}
					applyCategorySpecificityBoost(catHits, fallbackCat)
					applyCategoryPositionPenalty(catHits, len(candidates)+1)
					hits = mergeSearchHits(hits, catHits)
					if len(catHits) == 0 && strings.TrimSpace(req.Text) != "" {
						catTextHits, err := kb.searchTextWithinCategory(ctx, fallbackCat, req.Text, req.Limit)
						if err != nil {
							return nil, err
						}
						applyCategorySpecificityBoost(catTextHits, fallbackCat)
						applyCategoryPositionPenalty(catTextHits, len(candidates)+1)
						hits = mergeSearchHits(hits, catTextHits)
					}
				}
			}
		}

		if req.IncludeDocumentContent {
			hydrateSearchHitsWithDocumentContent(ctx, kb, hits)
		}
		if kb.Manager != nil && kb.Manager.retrievalRouter != nil && len(candidates) > 0 {
			for _, cat := range candidates {
				if cat == nil {
					continue
				}
				if strings.TrimSpace(cat.Path) != "" {
					kb.Manager.retrievalRouter.NotePath(cat.Path)
				} else if strings.TrimSpace(cat.Name) != "" {
					kb.Manager.retrievalRouter.NotePath(cat.Name)
				}
			}
		}

		// Step 5: Sort by evidence-first ranking: lexical overlap and path agreement outrank weak vector-only matches.
		if len(hits) > 1 {
			for i := range hits {
				hits[i] = boostSearchHitScore(hits[i], req.Text, pathHints)
			}
			sort.Slice(hits, func(i, j int) bool {
				lexI, pathI := searchHitEvidence(&hits[i], req.Text, pathHints)
				lexJ, pathJ := searchHitEvidence(&hits[j], req.Text, pathHints)
				if lexI != lexJ {
					return lexI > lexJ
				}
				if pathI != pathJ {
					return pathI > pathJ
				}
				if hits[i].Score == hits[j].Score {
					return hits[i].ID < hits[j].ID
				}
				return hits[i].Score < hits[j].Score
			})
		}

		// Return matching items
		if len(hits) > 0 {
			results = append(results, hits)
		}
	}
	return results, nil
}

// Thought represents the individual entity of data in a batch categorization execution.
type Thought[T any] struct {
	Summaries     []string
	CategoryPath  string
	CategoryPaths []string
	DocID         DocIDs
	Data          T
	Vectors       [][]float32
	Positions     []VectorKey
	VectorHash    string
}

// IngestThoughts securely categorizes and stores an array of thoughts, optimizing latency
// by clustering queries and sending a batch request to the LLM generator.
func (kb *KnowledgeBase[T]) IngestThoughts(ctx context.Context, thoughts []Thought[T], persona string) error {
	if len(thoughts) == 0 {
		return nil
	}

	// 0. Resolve missing summaries via LLM LLM Enrichment
	for i, thought := range thoughts {
		if len(thought.Summaries) > 0 {
			continue
		}

		dataStr := ""
		if str, ok := any(thought.Data).(string); ok {
			dataStr = str
		} else {
			b, _ := json.Marshal(thought.Data)
			dataStr = string(b)
		}

		gen, err := kb.Manager.GenerateSummaries(ctx, dataStr)
		if err == nil && len(gen) > 0 {
			thoughts[i].Summaries = gen
		} else {
			thoughts[i].Summaries = []string{dataStr}
		}
	}

	var uncategorizedIdx []int
	var uncategorizedTexts []string
	resolvedCategoryPaths := make([][]string, len(thoughts))

	// 2. Evaluate if we need to categorize
	for i, thought := range thoughts {
		explicitPaths := make([]string, 0, 1+len(thought.CategoryPaths))
		if strings.TrimSpace(thought.CategoryPath) != "" {
			explicitPaths = append(explicitPaths, thought.CategoryPath)
		}
		for _, path := range thought.CategoryPaths {
			if strings.TrimSpace(path) != "" {
				explicitPaths = append(explicitPaths, path)
			}
		}
		explicitPaths = uniqueCategoryPaths(explicitPaths)
		if len(explicitPaths) > 0 {
			resolvedCategoryPaths[i] = explicitPaths
			thoughts[i].CategoryPath = explicitPaths[0]
			thoughts[i].CategoryPaths = append([]string(nil), explicitPaths[1:]...)
			continue // Already formally categorized
		}

		categorizedByMath := false
		if kb.MaxMathCategoryDistance > 0 && len(thought.Vectors) > 0 {
			closest, dist, err := kb.Manager.FindClosestCategory(ctx, thought.Vectors[0])
			if err == nil && closest != nil && dist <= kb.MaxMathCategoryDistance {
				resolvedCategoryPaths[i] = []string{closest.Path}
				thoughts[i].CategoryPath = closest.Path
				categorizedByMath = true
			}
			if !categorizedByMath && kb.Manager != nil {
				if catName, err := kb.Manager.Categorize(strings.Join(thought.Summaries, " "), CategorizeOptions{Mode: CategorizerModeStorage, PersonaContext: persona}); err == nil && strings.TrimSpace(catName) != "" {
					resolvedCategoryPaths[i] = []string{catName}
					thoughts[i].CategoryPath = catName
					categorizedByMath = true
				}
			}
		}

		if !categorizedByMath {
			uncategorizedIdx = append(uncategorizedIdx, i)
			// Aggregate summaries for LLM to deduce category
			agg := ""
			for _, s := range thought.Summaries {
				agg += s + " "
			}
			uncategorizedTexts = append(uncategorizedTexts, strings.TrimSpace(agg))
		}
	}

	// 3. Batch Fallback to categorizer-based classification
	if len(uncategorizedTexts) > 0 {
		var generatedPaths [][]string
		if multiCatManager, ok := any(kb.Manager).(interface {
			CategorizeManyCandidates(texts []string, opts CategorizeOptions) ([][]CategorizerCandidate, error)
		}); ok {
			candidatesByText, err := multiCatManager.CategorizeManyCandidates(uncategorizedTexts, CategorizeOptions{Mode: CategorizerModeStorage, PersonaContext: persona})
			if err != nil {
				return err
			}
			generatedPaths = make([][]string, len(candidatesByText))
			for idx, candidates := range candidatesByText {
				paths := make([]string, 0, len(candidates))
				for _, candidate := range candidates {
					if strings.TrimSpace(candidate.Category) == "" {
						continue
					}
					if containsString(paths, candidate.Category) {
						continue
					}
					paths = append(paths, candidate.Category)
				}
				if len(paths) == 0 {
					if catName, err := kb.Manager.Categorize(uncategorizedTexts[idx], CategorizeOptions{Mode: CategorizerModeStorage, PersonaContext: persona}); err == nil && strings.TrimSpace(catName) != "" {
						paths = []string{catName}
					}
				}
				generatedPaths[idx] = uniqueCategoryPaths(paths)
			}
		} else {
			generated, err := kb.Manager.CategorizeMany(uncategorizedTexts, CategorizeOptions{Mode: CategorizerModeStorage, PersonaContext: persona})
			if err != nil {
				return err
			}
			generatedPaths = make([][]string, len(generated))
			for idx, category := range generated {
				if strings.TrimSpace(category) != "" {
					generatedPaths[idx] = []string{category}
				}
			}
		}

		// Re-align the generated outputs with the original array
		for idx, origIdx := range uncategorizedIdx {
			if len(generatedPaths[idx]) == 0 {
				continue
			}
			resolvedCategoryPaths[origIdx] = generatedPaths[idx]
			thoughts[origIdx].CategoryPath = generatedPaths[idx][0]
			thoughts[origIdx].CategoryPaths = append([]string(nil), generatedPaths[idx][1:]...)
		}
	}

	// 4. Ensure Categories and Store
	catCache := make(map[string]*Category)

	// Batch collect all summaries for classification embedding
	type thoughtWithIndex struct {
		thought Thought[T]
		index   int
	}
	var needsClassificationVecs []thoughtWithIndex
	var allClassificationTexts []string

	for i, thought := range thoughts {
		if len(thought.Summaries) > 0 {
			needsClassificationVecs = append(needsClassificationVecs, thoughtWithIndex{thought, i})
			allClassificationTexts = append(allClassificationTexts, thought.Summaries...)
		}
	}

	// Batch embed all classification vectors at once
	var allClassificationVecs [][]float32
	if len(allClassificationTexts) > 0 && kb.Manager != nil && kb.Manager.embedder != nil {
		vecs, err := embed.CategoryTexts(ctx, kb.Manager.embedder, allClassificationTexts)
		if err == nil {
			allClassificationVecs = vecs
		}
	}

	// Map classification vectors back to thoughts
	thoughtClassificationVecs := make(map[int][][]float32)
	vecIdx := 0
	for _, twi := range needsClassificationVecs {
		summaryCount := len(twi.thought.Summaries)
		if vecIdx+summaryCount <= len(allClassificationVecs) {
			thoughtClassificationVecs[twi.index] = allClassificationVecs[vecIdx : vecIdx+summaryCount]
			vecIdx += summaryCount
		}
	}

	for i, thought := range thoughts {
		chosenPaths := resolvedCategoryPaths[i]
		if len(chosenPaths) == 0 {
			chosenPaths = []string{thought.CategoryPath}
		}
		chosenPaths = uniqueCategoryPaths(chosenPaths)
		if len(chosenPaths) == 0 {
			chosenPaths = []string{"uncategorized"}
		}

		item := Item[T]{
			ID:         sop.NewUUID(),
			DocID:      thought.DocID,
			Summaries:  thought.Summaries,
			Data:       thought.Data,
			Positions:  thought.Positions,
			VectorHash: thought.VectorHash,
		}

		// Use pre-batched classification vectors
		classificationVecs := thoughtClassificationVecs[i]

		for _, catPath := range chosenPaths {
			cat, exists := catCache[catPath]
			if !exists {
				catID, err := kb.Manager.EnsureCategory(ctx, catPath)
				if err != nil {
					return err
				}
				// Retrieve the full category object
				cats, err := kb.Store.Categories(ctx)
				if err != nil {
					return err
				}
				found, err := cats.Find(ctx, catID, false)
				if err != nil {
					return err
				}
				if !found {
					return fmt.Errorf("category not found after ensuring: %v", catID)
				}
				cat, err = cats.GetCurrentValue(ctx)
				if err != nil {
					return err
				}

				// Initialize CenterVector if empty - must use CategoryTexts (classification space)
				if len(cat.CenterVector) == 0 && kb.Manager != nil && kb.Manager.embedder != nil {
					catVecs, err := embed.CategoryTexts(ctx, kb.Manager.embedder, []string{normalize(cat.Name)})
					if err == nil && len(catVecs) > 0 && len(catVecs[0]) > 0 {
						cat.CenterVector = catVecs[0]
						// Update the category in the store
						cats.UpdateCurrentItem(ctx, cat.ID, cat)
					}
				}
				catCache[catPath] = cat
			}

			err := kb.Store.UpsertByCategoryID(ctx, cat.ID, cat.CenterVector, item, thought.Vectors, classificationVecs)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// IngestThought securely categorizes and stores a thought.
// If category is omitted (""), the LLM dynamically categorizes the text, unless it is close
// enough to an existing category centroid and MaxMathCategoryDistance > 0.
func (kb *KnowledgeBase[T]) IngestThought(
	ctx context.Context,
	text string,
	category string,
	persona string,
	vector []float32,
	data T,
) error {
	var vectors [][]float32
	if vector != nil {
		vectors = [][]float32{vector}
	}
	return kb.IngestThoughts(ctx, []Thought[T]{{Summaries: []string{text}, CategoryPath: category, Data: data, Vectors: vectors}}, persona)
}

// TriggerSleepCycle forces the LLM to scan, reflect, and re-organize dense categories.
func (kb *KnowledgeBase[T]) TriggerSleepCycle(ctx context.Context) error {
	if err := kb.Manager.SleepCycle(ctx); err != nil {
		return err
	}
	return nil
}

func encodeConfigValue[T any](config *KnowledgeBaseConfig) (T, error) {
	var zero T
	if config == nil {
		return zero, nil
	}

	configBytes, err := json.Marshal(config)
	if err != nil {
		return zero, err
	}

	valueType := reflect.TypeOf(zero)
	if valueType == nil {
		return zero, nil
	}
	if valueType.Kind() == reflect.String {
		value := reflect.ValueOf(string(configBytes)).Convert(valueType)
		return value.Interface().(T), nil
	}

	var v T
	if err := json.Unmarshal(configBytes, &v); err != nil {
		return zero, err
	}
	return v, nil
}

func decodeConfigValue[T any](data T) (*KnowledgeBaseConfig, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	trimmed := bytes.TrimSpace(b)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var stored string
		if err := json.Unmarshal(trimmed, &stored); err == nil {
			trimmed = bytes.TrimSpace([]byte(stored))
		}
	}

	var cfg KnowledgeBaseConfig
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, nil
	}
	if err := json.Unmarshal(trimmed, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// GetConfig retrieves the metadata configuration for this KnowledgeBase.
func (kb *KnowledgeBase[T]) GetConfig(ctx context.Context) (*KnowledgeBaseConfig, error) {
	if kb.configCache != nil {
		return kb.configCache, nil
	}

	itemsBtree, err := kb.Store.Items(ctx)
	if err != nil {
		return nil, err
	}

	nilKey := ItemKey{CategoryID: sop.NilUUID, ItemID: sop.NilUUID}
	found, err := itemsBtree.Find(ctx, nilKey, false)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil // No config found
	}

	item, err := itemsBtree.GetCurrentValue(ctx)
	if err != nil {
		return nil, err
	}

	cfg, err := decodeConfigValue(item.Data)
	if err != nil {
		return nil, err
	}

	kb.configCache = cfg
	return cfg, nil
}

// SetConfig saves the metadata configuration for this KnowledgeBase.
func (kb *KnowledgeBase[T]) SetConfig(ctx context.Context, config *KnowledgeBaseConfig) error {
	itemsBtree, err := kb.Store.Items(ctx)
	if err != nil {
		return err
	}

	nilKey := ItemKey{CategoryID: sop.NilUUID, ItemID: sop.NilUUID}

	storedData, err := encodeConfigValue[T](config)
	if err != nil {
		return err
	}

	configItem := Item[T]{
		ID:         sop.NilUUID,
		CategoryID: sop.NilUUID,
		Data:       storedData,
	}

	_, err = itemsBtree.Upsert(ctx, nilKey, configItem)
	if err == nil {
		kb.configCache = config
		if kb.Manager != nil {
			kb.Manager.SetKnowledgeBase(kb)
			if config != nil && config.CategorizerConfig != nil {
				if configureErr := kb.Manager.ConfigureFromKnowledgeBaseConfig(config); configureErr != nil {
					return configureErr
				}
			}
		}
	}
	return err
}

// Initialize ensures the knowledge base has an embedder attached based on its persisted config.
// It uses the configured embedder name and dimension when available, falling back to a simple embedder.
func (kb *KnowledgeBase[T]) Initialize(ctx context.Context) error {
	if kb == nil || kb.Manager == nil {
		return nil
	}
	if kb.Manager.embedder != nil {
		return nil
	}

	cfg, err := kb.GetConfig(ctx)
	if err != nil {
		return err
	}
	if cfg == nil {
		return nil
	}

	dim := cfg.EmbedderDimension
	name := strings.TrimSpace(cfg.Embedder)
	if name == "" {
		name = kb.Store.Name()
	}
	if name == "" {
		name = "simple"
	}

	embedder, err := embed.NewFromName(name, dim)
	if err != nil {
		return err
	}
	if embedder != nil {
		kb.Manager.embedder = embedder
	}
	return nil
}

// ComputeVectorHash computes a predictable string hash representing the text content.
// We optionally include dimensions, but explicitly exclude embedderName so that compatible models (same dims) don't trigger re-vectorization unless content actually changes.
func ComputeVectorHash(embedderDim int, texts ...string) string {
	hasher := sha256.New()

	dimStr := "dim:" + strconv.Itoa(embedderDim) + "::"
	hasher.Write([]byte(dimStr))

	for _, t := range texts {
		hasher.Write([]byte(t + "::"))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

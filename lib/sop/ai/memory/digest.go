package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/embed"
)

type KBDigestRequest struct {
	Queries            []string
	PerQueryLimit      int
	MaxResults         int
	MinScore           float32
	UseClosestCategory bool
	KeywordFallback    bool
}

type KBDigestHit struct {
	ID         string
	DocID      []string
	Score      float32
	Category   string
	Text       string
	Query      string
	SearchType string
}

func DigestKnowledgeBase(ctx context.Context, kb *KnowledgeBase[map[string]any], embedder ai.Embeddings, req KBDigestRequest) ([]KBDigestHit, error) {
	if kb == nil {
		return nil, nil
	}

	queries := normalizeDigestQueries(req.Queries)
	if len(queries) == 0 {
		return nil, nil
	}

	perQueryLimit := req.PerQueryLimit
	if perQueryLimit <= 0 {
		perQueryLimit = 5
	}
	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = perQueryLimit
	}
	minScore := req.MinScore
	if minScore <= 0 {
		minScore = 0.6
	}

	merged := make(map[string]KBDigestHit, len(queries)*perQueryLimit)
	for _, query := range queries {
		categoryFilter := ""
		if embedder != nil {
			vecs, err := embed.CategoryTexts(ctx, embedder, []string{normalize(query)})
			if err == nil && len(vecs) > 0 {
				if req.UseClosestCategory {
					closestCat, _, err := kb.Manager.FindClosestCategory(ctx, vecs[0])
					if err == nil && closestCat != nil {
						categoryFilter = closestCat.Name
					}
				}

				semanticOpts := []*SearchOptions[map[string]any]{{Limit: perQueryLimit}}
				if categoryFilter != "" {
					semanticOpts = append([]*SearchOptions[map[string]any]{{
						Limit:        perQueryLimit,
						CategoryPath: categoryFilter,
					}}, semanticOpts...)
				}

				for _, opts := range semanticOpts {
					vecs, err = embed.QueryTexts(ctx, embedder, []string{normalize(query)})
					if err != nil {
						return nil, err
					}
					batch, err := kb.Search(ctx, []SearchRequest[map[string]any]{{
						Vector:       vecs[0],
						Limit:        opts.Limit,
						CategoryPath: opts.CategoryPath,
					}})
					if err != nil {
						return nil, err
					}
					if len(batch) == 0 {
						continue
					}
					for _, hit := range batch[0] {
						relevance := digestSemanticRelevance(hit.Score)
						if relevance < minScore {
							continue
						}
						mergeDigestHit(merged, KBDigestHit{
							ID:         hit.ID,
							DocID:      hit.DocID,
							Score:      relevance,
							Category:   extractDigestCategory(hit.Payload),
							Text:       ResolveHitText(ctx, kb, hit.Payload, hit.DocID),
							Query:      query,
							SearchType: "semantic",
						})
					}
				}
			}
		}

		if req.KeywordFallback {
			batch, err := kb.Search(ctx, []SearchRequest[map[string]any]{{
				Text:  query,
				Limit: perQueryLimit,
			}})
			if err != nil {
				return nil, err
			}
			if len(batch) == 0 {
				continue
			}
			for _, hit := range batch[0] {
				mergeDigestHit(merged, KBDigestHit{
					ID:         hit.ID,
					DocID:      hit.DocID,
					Score:      hit.Score,
					Category:   extractDigestCategory(hit.Payload),
					Text:       ResolveHitText(ctx, kb, hit.Payload, hit.DocID),
					Query:      query,
					SearchType: "keyword",
				})
			}
		}
	}

	results := make([]KBDigestHit, 0, len(merged))
	for _, hit := range merged {
		if strings.TrimSpace(hit.Text) == "" {
			continue
		}
		results = append(results, hit)
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].Category != results[j].Category {
			return results[i].Category < results[j].Category
		}
		return results[i].Text < results[j].Text
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results, nil
}

func normalizeDigestQueries(queries []string) []string {
	if len(queries) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(queries))
	normalized := make([]string, 0, len(queries))
	for _, query := range queries {
		query = strings.TrimSpace(query)
		if query == "" {
			continue
		}
		key := strings.ToLower(query)
		if seen[key] {
			continue
		}
		seen[key] = true
		normalized = append(normalized, query)
	}
	return normalized
}

func mergeDigestHit(merged map[string]KBDigestHit, hit KBDigestHit) {
	text := strings.TrimSpace(hit.Text)
	if text == "" {
		return
	}
	key := strings.Join(hit.DocID, "|")
	if key == "" {
		key = hit.Category + "|" + text
	}
	if existing, ok := merged[key]; !ok || hit.Score > existing.Score {
		hit.Text = text
		merged[key] = hit
	}
}

func digestSemanticRelevance(distance float32) float32 {
	if distance < 0 {
		distance = 0
	}
	return 1 / (1 + distance)
}

func ResolveHitText[T any](ctx context.Context, kb *KnowledgeBase[T], payload any, docIDs []string) string {
	if kb == nil || kb.Store == nil {
		return extractDigestText(payload)
	}
	for _, docID := range docIDs {
		trimmed := strings.TrimSpace(docID)
		if trimmed == "" {
			continue
		}
		parsedID, err := sop.ParseUUID(trimmed)
		if err != nil {
			continue
		}
		doc, err := kb.Store.GetDocument(ctx, parsedID)
		if err != nil || doc == nil {
			continue
		}
		if text := strings.TrimSpace(doc.Content); text != "" {
			return normalizeStructuredHitText(text)
		}
		if text := strings.TrimSpace(doc.PayloadRef); text != "" {
			return normalizeStructuredHitText(text)
		}
	}
	return extractDigestText(payload)
}

func ResolveHitSourceLabel[T any](ctx context.Context, kb *KnowledgeBase[T], docIDs []string) string {
	if len(docIDs) == 0 {
		return ""
	}
	for _, docID := range docIDs {
		trimmed := strings.TrimSpace(docID)
		if trimmed == "" {
			continue
		}
		if kb == nil || kb.Store == nil {
			return trimmed
		}
		parsedID, err := sop.ParseUUID(trimmed)
		if err != nil {
			return trimmed
		}
		doc, err := kb.Store.GetDocument(ctx, parsedID)
		if err != nil || doc == nil {
			return trimmed
		}
		for _, candidate := range []string{doc.Title, doc.Source, doc.URL, doc.PayloadRef} {
			label := strings.TrimSpace(candidate)
			if label != "" {
				return label
			}
		}
		return trimmed
	}
	return ""
}

func normalizeStructuredHitText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var payload any
		if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
			return formatStructuredHitText(payload)
		}
	}
	return trimmed
}

func formatStructuredHitText(payload any) string {
	switch v := payload.(type) {
	case map[string]any:
		parts := make([]string, 0, len(v))
		for _, key := range []string{"description", "summary", "outcome", "lesson", "next_step", "title", "content"} {
			if value, ok := v[key]; ok {
				text := strings.TrimSpace(fmt.Sprint(value))
				if text != "" {
					parts = append(parts, fmt.Sprintf("%s: %s", strings.ToTitle(strings.ReplaceAll(key, "_", " ")), text))
				}
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " | ")
		}
	case []any:
		items := make([]string, 0, len(v))
		for _, item := range v {
			if rendered := formatStructuredHitText(item); rendered != "" {
				items = append(items, rendered)
			}
		}
		if len(items) > 0 {
			return strings.Join(items, " | ")
		}
	}
	return fmt.Sprintf("%v", payload)
}

func extractDigestText(payload any) string {
	if payload == nil {
		return ""
	}
	switch v := payload.(type) {
	case map[string]any:
		for _, field := range []string{"_raw_content", "content", "description", "text", "page_content"} {
			if text, ok := v[field].(string); ok && strings.TrimSpace(text) != "" {
				return normalizeStructuredHitText(text)
			}
		}
	case string:
		return normalizeStructuredHitText(v)
	}
	return fmt.Sprintf("%v", payload)
}

func extractDigestCategory(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	for _, field := range []string{"category", "category_path", "path"} {
		if text, ok := payload[field].(string); ok {
			return text
		}
	}
	return ""
}

package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	aidb "github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/embed"
	"github.com/sharedcode/sop/ai/memory"
)

const defaultSharedMemoryKBName = "shared_memory"

// PromoteSharedMemoryArtifact stores a generalized artifact into the shared-memory knowledge base.
// It is intentionally additive: it preserves local memory behavior while making a compact,
// privacy-safe pattern available for future retrieval.
func PromoteSharedMemoryArtifact(
	ctx context.Context,
	db *aidb.Database,
	llm ai.Generator,
	embedder ai.Embeddings,
	text, kbName, category, scopeType, scopeID string,
	payload map[string]any,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil {
		return fmt.Errorf("shared memory database is required")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("shared memory text is required")
	}
	if strings.TrimSpace(kbName) == "" {
		kbName = defaultSharedMemoryKBName
	}
	if strings.TrimSpace(category) == "" {
		category = "shared_memory"
	}
	if strings.TrimSpace(scopeType) == "" {
		scopeType = "scope"
	}
	if strings.TrimSpace(scopeID) == "" {
		scopeID = "shared"
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload = copySharedMemoryPayload(payload)
	payload["text"] = text
	payload["category"] = category
	payload["scope_type"] = scopeType
	payload["scope_id"] = scopeID
	payload["promoted_at"] = time.Now().UTC().Format(time.RFC3339)

	tx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		return fmt.Errorf("begin shared-memory tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var vectors [][]float32
	if embedder != nil {
		vecs, err := embed.DocumentTexts(ctx, embedder, []string{text})
		if err == nil && len(vecs) > 0 && len(vecs[0]) > 0 {
			vectors = [][]float32{vecs[0]}
		}
	}

	kb, err := db.OpenKnowledgeBase(ctx, kbName, tx, llm, embedder, false, true)
	if err != nil {
		return fmt.Errorf("open shared-memory knowledge base %q: %w", kbName, err)
	}

	if err := kb.IngestThought(ctx, text, category, "system", nil, payload); err != nil {
		return fmt.Errorf("ingest shared-memory thought: %w", err)
	}
	if len(vectors) > 0 {
		if err := kb.IngestThought(ctx, text, category, "system", vectors[0], payload); err != nil {
			return fmt.Errorf("ingest shared-memory thought with vector: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit shared-memory tx: %w", err)
	}
	return nil
}

// RetrieveSharedMemory returns the most relevant shared artifacts for a query.
func RetrieveSharedMemory(
	ctx context.Context,
	db *aidb.Database,
	llm ai.Generator,
	embedder ai.Embeddings,
	query, kbName string,
	limit int,
) ([]ai.Hit[map[string]any], error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil {
		return nil, fmt.Errorf("shared memory database is required")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if strings.TrimSpace(kbName) == "" {
		kbName = defaultSharedMemoryKBName
	}
	if limit <= 0 {
		limit = 5
	}

	tx, err := db.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		return nil, fmt.Errorf("begin shared-memory read tx: %w", err)
	}
	defer tx.Rollback(ctx)

	kb, err := db.OpenKnowledgeBase(ctx, kbName, tx, llm, embedder, false, true)
	if err != nil {
		return nil, fmt.Errorf("open shared-memory knowledge base %q: %w", kbName, err)
	}

	batch, err := kb.Search(ctx, []memory.SearchRequest[map[string]any]{{Text: query, Limit: limit}})
	if err != nil {
		return nil, fmt.Errorf("search shared-memory knowledge base: %w", err)
	}
	if len(batch) > 0 && len(batch[0]) > 0 {
		return batch[0], nil
	}
	return fallbackRetrieveSharedMemory(ctx, kb, query, limit)
}

func fallbackRetrieveSharedMemory(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], query string, limit int) ([]ai.Hit[map[string]any], error) {
	if kb == nil || kb.Store == nil {
		return nil, nil
	}
	items, err := kb.Store.Items(ctx)
	if err != nil {
		return nil, fmt.Errorf("read shared-memory items: %w", err)
	}
	if items == nil {
		return nil, nil
	}
	ok, err := items.First(ctx)
	if err != nil {
		return nil, fmt.Errorf("seek shared-memory items: %w", err)
	}

	var hits []ai.Hit[map[string]any]
	queryTokens := tokenizeSharedMemoryQuery(query)
	for ok {
		item, err := items.GetCurrentValue(ctx)
		if err != nil {
			return nil, fmt.Errorf("read shared-memory item: %w", err)
		}
		text := extractSharedMemoryText(item.Data)
		if text == "" && len(item.Summaries) > 0 {
			text = strings.Join(item.Summaries, " ")
		}
		if text != "" && sharedMemoryQueryMatches(queryTokens, text) {
			hits = append(hits, ai.Hit[map[string]any]{
				ID:      item.ID.String(),
				DocID:   item.DocID,
				Score:   1.0,
				Payload: sharedMemoryPayloadFromItem(item),
			})
			if len(hits) >= limit {
				break
			}
		}
		nextOK, err := items.Next(ctx)
		if err != nil {
			return nil, fmt.Errorf("advance shared-memory items: %w", err)
		}
		ok = nextOK
	}
	return hits, nil
}

func tokenizeSharedMemoryQuery(query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	parts := strings.FieldsFunc(query, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ',' || r == '.' || r == '-' || r == '_'
	})
	var tokens []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len(part) >= 2 {
			tokens = append(tokens, part)
		}
	}
	return tokens
}

func sharedMemoryQueryMatches(tokens []string, text string) bool {
	if len(tokens) == 0 {
		return strings.TrimSpace(text) != ""
	}
	needle := strings.ToLower(strings.TrimSpace(text))
	if needle == "" {
		return false
	}
	matches := 0
	for _, token := range tokens {
		if strings.Contains(needle, token) {
			matches++
		}
	}
	return matches > 0
}

func extractSharedMemoryText(data any) string {
	if data == nil {
		return ""
	}
	if payload, ok := data.(map[string]any); ok {
		for _, key := range []string{"text", "description", "content", "page_content", "_raw_content"} {
			if value, ok := payload[key].(string); ok {
				value = strings.TrimSpace(value)
				if value != "" {
					return value
				}
			}
		}
	}
	return strings.TrimSpace(fmt.Sprintf("%v", data))
}

func sharedMemoryPayloadFromItem(item memory.Item[map[string]any]) map[string]any {
	if item.Data == nil {
		return map[string]any{}
	}
	payload := item.Data
	out := copySharedMemoryPayload(payload)
	if _, ok := out["text"]; !ok {
		out["text"] = extractSharedMemoryText(payload)
	}
	return out
}

func copySharedMemoryPayload(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(payload)+4)
	for k, v := range payload {
		out[k] = v
	}
	return out
}

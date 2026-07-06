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

// PromoteKnowledgeArtifact stores a compact artifact into a named knowledge-base entry.
// When an artifact reference is present, the content becomes selectable as a research source
// through the existing selected-space/knowledge-base flow.
func PromoteKnowledgeArtifact(
	ctx context.Context,
	db *aidb.Database,
	llm ai.Generator,
	embedder ai.Embeddings,
	text string,
	artifactRef ai.ArtifactReference,
	kbName, category, scopeType, scopeID string,
	payload map[string]any,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil {
		return fmt.Errorf("shared memory database is required")
	}
	if strings.TrimSpace(kbName) == "" {
		kbName = strings.TrimSpace(artifactRef.Name)
	}
	if strings.TrimSpace(kbName) == "" {
		kbName = defaultSharedMemoryKBName
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload = copySharedMemoryPayload(payload)
	payload["knowledge_artifact"] = true
	payload["artifact_name"] = strings.TrimSpace(artifactRef.Name)
	payload["artifact_type"] = string(artifactRef.Type)
	payload["artifact_db_name"] = strings.TrimSpace(artifactRef.DatabaseName)
	payload["artifact_ref"] = artifactReferencePayload(artifactRef)
	return PromoteSharedMemoryArtifact(ctx, db, llm, embedder, text, kbName, category, scopeType, scopeID, payload)
}

// ExtractArtifactReference parses an artifact reference from a generic payload value.
func ExtractArtifactReference(value any) ai.ArtifactReference {
	switch ref := value.(type) {
	case ai.ArtifactReference:
		return ref
	case *ai.ArtifactReference:
		if ref != nil {
			return *ref
		}
	}

	if payload, ok := value.(map[string]any); ok {
		if nested, ok := payload["artifact_ref"]; ok {
			return ExtractArtifactReference(nested)
		}
		return extractArtifactReferenceFromMap(payload)
	}

	if payload, ok := value.(map[string]string); ok {
		return extractArtifactReferenceFromMapString(payload)
	}
	return ai.ArtifactReference{}
}

func extractArtifactReferenceFromMap(payload map[string]any) ai.ArtifactReference {
	if len(payload) == 0 {
		return ai.ArtifactReference{}
	}
	ref := ai.ArtifactReference{}
	if name, ok := payload["name"].(string); ok {
		ref.Name = strings.TrimSpace(name)
	}
	if ref.Name == "" {
		if name, ok := payload["artifact_name"].(string); ok {
			ref.Name = strings.TrimSpace(name)
		}
	}
	if dbName, ok := payload["db_name"].(string); ok {
		ref.DatabaseName = strings.TrimSpace(dbName)
	}
	if ref.DatabaseName == "" {
		if dbName, ok := payload["artifact_db_name"].(string); ok {
			ref.DatabaseName = strings.TrimSpace(dbName)
		}
	}
	if artifactType, ok := payload["type"].(string); ok {
		ref.Type = ai.ArtifactType(strings.TrimSpace(artifactType))
	}
	if ref.Type == "" {
		if artifactType, ok := payload["artifact_type"].(string); ok {
			ref.Type = ai.ArtifactType(strings.TrimSpace(artifactType))
		}
	}
	return ref
}

func extractArtifactReferenceFromMapString(payload map[string]string) ai.ArtifactReference {
	if len(payload) == 0 {
		return ai.ArtifactReference{}
	}
	ref := ai.ArtifactReference{}
	if name := strings.TrimSpace(payload["name"]); name != "" {
		ref.Name = name
	} else if name := strings.TrimSpace(payload["artifact_name"]); name != "" {
		ref.Name = name
	}
	if dbName := strings.TrimSpace(payload["db_name"]); dbName != "" {
		ref.DatabaseName = dbName
	} else if dbName := strings.TrimSpace(payload["artifact_db_name"]); dbName != "" {
		ref.DatabaseName = dbName
	}
	if artifactType := strings.TrimSpace(payload["type"]); artifactType != "" {
		ref.Type = ai.ArtifactType(artifactType)
	} else if artifactType := strings.TrimSpace(payload["artifact_type"]); artifactType != "" {
		ref.Type = ai.ArtifactType(artifactType)
	}
	return ref
}

func artifactReferencePayload(ref ai.ArtifactReference) map[string]any {
	if ref.Name == "" && ref.DatabaseName == "" && ref.Type == "" {
		return nil
	}
	payload := map[string]any{
		"name":    strings.TrimSpace(ref.Name),
		"type":    string(ref.Type),
		"db_name": strings.TrimSpace(ref.DatabaseName),
	}
	return payload
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

	fallbackTx, err := db.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		return nil, fmt.Errorf("begin fallback shared-memory read tx: %w", err)
	}
	defer fallbackTx.Rollback(ctx)

	fallbackKB, err := db.OpenKnowledgeBase(ctx, kbName, fallbackTx, llm, embedder, false, true)
	if err != nil {
		return nil, fmt.Errorf("open fallback shared-memory knowledge base %q: %w", kbName, err)
	}
	return fallbackRetrieveSharedMemory(ctx, fallbackKB, query, limit)
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

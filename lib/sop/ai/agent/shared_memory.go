package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// FederationTarget specifies where a federated artifact should be stored.
type FederationTarget string

const (
	// FederationTargetSharedMemory stores patterns, heuristics, and preferences
	FederationTargetSharedMemory FederationTarget = "shared_memory"
	// FederationTargetCorePlaybook stores proven workflows and procedures
	FederationTargetCorePlaybook FederationTarget = "core_playbook"
	// FederationTargetCustomKB stores domain-specific knowledge in a named KB
	FederationTargetCustomKB FederationTarget = "custom_kb"
)

// FederatedArtifactV2 represents a versioned, routable federated artifact.
// V2 adds: targets, versioning, deduplication, and conflict resolution.
type FederatedArtifactV2 struct {
	// V1 compatibility fields
	Text     string         `json:"text"`
	Category string         `json:"category"`
	Metadata map[string]any `json:"metadata,omitempty"`

	// V2 routing and typing
	Target      FederationTarget `json:"target"`       // Where to store (shared_memory, core_playbook, custom_kb)
	ContentType string           `json:"content_type"` // "pattern", "workflow", "knowledge"

	// V2 versioning and deduplication
	Version       int      `json:"version"`                  // Monotonic version number
	ContentHash   string   `json:"content_hash"`             // SHA-256 of normalized content
	ParentVersion int      `json:"parent_version,omitempty"` // Previous version (0 if initial)
	Supersedes    []string `json:"supersedes,omitempty"`     // Artifact IDs this replaces

	// V2 provenance and recurrence
	OriginDaemonID  string `json:"origin_daemon_id"` // First daemon to promote
	LastModifiedBy  string `json:"last_modified_by"` // Latest daemon to modify
	RecurrenceCount int    `json:"recurrence_count"` // How many daemons learned this
	CreatedAt       string `json:"created_at"`       // RFC3339
	UpdatedAt       string `json:"updated_at"`       // RFC3339

	// Workflow-specific fields (when ContentType="workflow")
	WorkflowDefinition *WorkflowDefinition `json:"workflow_definition,omitempty"`
	WorkflowID         string              `json:"workflow_id,omitempty"`
	ExecutionStats     map[string]any      `json:"execution_stats,omitempty"`
}

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

// =====================================================================
// Federation V2: Workflow-Specific Functions
// =====================================================================

// computeContentHash generates a SHA-256 hash of the normalized content.
// Used for deduplication and version tracking.
func computeContentHash(content string) string {
	normalized := strings.TrimSpace(strings.ToLower(content))
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:])
}

// computeWorkflowHash generates a deterministic hash for a workflow definition.
func computeWorkflowHash(workflow *WorkflowDefinition) string {
	if workflow == nil {
		return ""
	}
	// Hash the critical parts: ID, version, steps
	data, err := json.Marshal(map[string]any{
		"id":          workflow.ID,
		"version":     workflow.Version,
		"steps":       workflow.Steps,
		"description": strings.TrimSpace(workflow.Description),
	})
	if err != nil {
		return computeContentHash(workflow.Description)
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// PromoteWorkflowArtifactV2 promotes a successful workflow execution to federation.
// This is V2-specific for workflow federation to Core Playbook.
func PromoteWorkflowArtifactV2(
	ctx context.Context,
	db *aidb.Database,
	llm ai.Generator,
	embedder ai.Embeddings,
	workflow *WorkflowDefinition,
	daemonID string,
	executionStats map[string]any,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil {
		return fmt.Errorf("database is required for workflow promotion")
	}
	if workflow == nil {
		return fmt.Errorf("workflow definition is required")
	}
	if strings.TrimSpace(daemonID) == "" {
		return fmt.Errorf("daemon ID is required")
	}

	// Build V2 artifact
	now := time.Now().UTC().Format(time.RFC3339)
	artifact := FederatedArtifactV2{
		Text:               workflow.Description,
		Category:           "workflow_pattern",
		Target:             FederationTargetCorePlaybook,
		ContentType:        "workflow",
		Version:            1, // Will be incremented by HQ if duplicate exists
		ContentHash:        computeWorkflowHash(workflow),
		OriginDaemonID:     daemonID,
		LastModifiedBy:     daemonID,
		RecurrenceCount:    1,
		CreatedAt:          now,
		UpdatedAt:          now,
		WorkflowDefinition: workflow,
		WorkflowID:         workflow.ID,
		ExecutionStats:     executionStats,
		Metadata: map[string]any{
			"workflow_name":      workflow.Name,
			"workflow_category":  workflow.CategoryPath,
			"workflow_version":   workflow.Version,
			"trigger_patterns":   workflow.TriggerPatterns,
			"step_count":         len(workflow.Steps),
			"federation_version": "v2",
			"promoted_at":        now,
		},
	}

	// Store in Core Playbook KB (local promotion)
	// This makes it available immediately to this daemon
	if err := storeWorkflowArtifactLocally(ctx, db, llm, embedder, &artifact); err != nil {
		return fmt.Errorf("failed to store workflow locally: %w", err)
	}

	return nil
}

// storeWorkflowArtifactLocally stores a workflow artifact in the local Core Playbook KB.
func storeWorkflowArtifactLocally(
	ctx context.Context,
	db *aidb.Database,
	llm ai.Generator,
	embedder ai.Embeddings,
	artifact *FederatedArtifactV2,
) error {
	if artifact == nil || artifact.WorkflowDefinition == nil {
		return fmt.Errorf("invalid workflow artifact")
	}

	kbName := "core_playbook"
	if artifact.Target == FederationTargetCustomKB {
		if customKB, ok := artifact.Metadata["target_kb"].(string); ok && customKB != "" {
			kbName = customKB
		}
	}

	tx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	kb, err := db.OpenKnowledgeBase(ctx, kbName, tx, llm, embedder, false, true)
	if err != nil {
		return fmt.Errorf("open knowledge base %q: %w", kbName, err)
	}

	// Prepare payload with full V2 metadata
	payload := map[string]any{
		"federation_version":  "v2",
		"target":              string(artifact.Target),
		"content_type":        artifact.ContentType,
		"version":             artifact.Version,
		"content_hash":        artifact.ContentHash,
		"origin_daemon_id":    artifact.OriginDaemonID,
		"last_modified_by":    artifact.LastModifiedBy,
		"recurrence_count":    artifact.RecurrenceCount,
		"created_at":          artifact.CreatedAt,
		"updated_at":          artifact.UpdatedAt,
		"workflow_id":         artifact.WorkflowID,
		"workflow_definition": artifact.WorkflowDefinition,
		"execution_stats":     artifact.ExecutionStats,
	}

	// Merge artifact metadata
	for k, v := range artifact.Metadata {
		if _, exists := payload[k]; !exists {
			payload[k] = v
		}
	}

	// Generate embedding for semantic search
	var vectors [][]float32
	if embedder != nil {
		searchText := fmt.Sprintf("%s. %s", artifact.WorkflowDefinition.Name, artifact.Text)
		vecs, err := embed.DocumentTexts(ctx, embedder, []string{searchText})
		if err == nil && len(vecs) > 0 && len(vecs[0]) > 0 {
			vectors = [][]float32{vecs[0]}
		}
	}

	// Store in KB
	categoryPath := artifact.WorkflowDefinition.CategoryPath
	if categoryPath == "" {
		categoryPath = "Workflows/Federated"
	}

	if err := kb.IngestThought(ctx, artifact.Text, categoryPath, "system", nil, payload); err != nil {
		return fmt.Errorf("ingest workflow thought: %w", err)
	}
	if len(vectors) > 0 {
		if err := kb.IngestThought(ctx, artifact.Text, categoryPath, "system", vectors[0], payload); err != nil {
			return fmt.Errorf("ingest workflow thought with vector: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// ParseFederatedArtifactV2 extracts a V2 artifact from raw metadata.
// Handles both V1 (for backward compatibility) and V2 formats.
func ParseFederatedArtifactV2(metadata map[string]any) (*FederatedArtifactV2, error) {
	if metadata == nil {
		return nil, fmt.Errorf("metadata is nil")
	}

	// Check for V2 marker
	if version, ok := metadata["federation_version"].(string); ok && version == "v2" {
		// Parse as V2
		artifact := &FederatedArtifactV2{}

		if text, ok := metadata["text"].(string); ok {
			artifact.Text = text
		}
		if category, ok := metadata["category"].(string); ok {
			artifact.Category = category
		}
		if target, ok := metadata["target"].(string); ok {
			artifact.Target = FederationTarget(target)
		}
		if contentType, ok := metadata["content_type"].(string); ok {
			artifact.ContentType = contentType
		}
		if version, ok := metadata["version"].(int); ok {
			artifact.Version = version
		} else if versionFloat, ok := metadata["version"].(float64); ok {
			artifact.Version = int(versionFloat)
		}
		if hash, ok := metadata["content_hash"].(string); ok {
			artifact.ContentHash = hash
		}
		if parentVer, ok := metadata["parent_version"].(int); ok {
			artifact.ParentVersion = parentVer
		} else if parentVerFloat, ok := metadata["parent_version"].(float64); ok {
			artifact.ParentVersion = int(parentVerFloat)
		}
		if originID, ok := metadata["origin_daemon_id"].(string); ok {
			artifact.OriginDaemonID = originID
		}
		if lastModBy, ok := metadata["last_modified_by"].(string); ok {
			artifact.LastModifiedBy = lastModBy
		}
		if recurrence, ok := metadata["recurrence_count"].(int); ok {
			artifact.RecurrenceCount = recurrence
		} else if recurrenceFloat, ok := metadata["recurrence_count"].(float64); ok {
			artifact.RecurrenceCount = int(recurrenceFloat)
		}
		if createdAt, ok := metadata["created_at"].(string); ok {
			artifact.CreatedAt = createdAt
		}
		if updatedAt, ok := metadata["updated_at"].(string); ok {
			artifact.UpdatedAt = updatedAt
		}
		if workflowID, ok := metadata["workflow_id"].(string); ok {
			artifact.WorkflowID = workflowID
		}

		// Parse workflow definition
		if workflowData, ok := metadata["workflow_definition"]; ok {
			if workflowMap, ok := workflowData.(map[string]any); ok {
				workflowJSON, err := json.Marshal(workflowMap)
				if err == nil {
					var workflow WorkflowDefinition
					if err := json.Unmarshal(workflowJSON, &workflow); err == nil {
						artifact.WorkflowDefinition = &workflow
					}
				}
			} else if workflow, ok := workflowData.(*WorkflowDefinition); ok {
				artifact.WorkflowDefinition = workflow
			}
		}

		if execStats, ok := metadata["execution_stats"].(map[string]any); ok {
			artifact.ExecutionStats = execStats
		}

		artifact.Metadata = metadata
		return artifact, nil
	}

	// Fall back to V1 format (for backward compatibility)
	return nil, fmt.Errorf("V1 artifact format not supported in workflow federation")
}

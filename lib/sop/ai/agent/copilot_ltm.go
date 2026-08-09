package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
	"github.com/sharedcode/sop/ai/memory"
)

func buildLTMMemoryCategoryPath(category, capability, scopeType, scopeID string) string {
	category = strings.TrimSpace(category)
	if category == "" {
		category = "General"
	}
	scopeType = strings.TrimSpace(scopeType)
	scopeID = strings.TrimSpace(scopeID)
	parts := []string{strings.Title(strings.ToLower(category))}
	if scopeType != "" && scopeID != "" {
		parts = append(parts, strings.Title(strings.ToLower(scopeType)))
		parts = append(parts, scopeID)
	} else if capability != "" {
		parts = append(parts, strings.Title(strings.ToLower(strings.ReplaceAll(capability, "_", " "))))
	}
	return strings.Join(parts, "/")
}

type codingMemoryLTMIndexEntry struct {
	Kind         string
	CategoryPath string
	Payload      map[string]any
}

func shouldVectorizeLTMIndexEntry(entry codingMemoryLTMIndexEntry) bool {
	return strings.EqualFold(strings.TrimSpace(entry.Kind), "semantic")
}

func buildCodingMemoryLTMPayloadAndIndexes(mem cw.CodingMemory) (map[string]any, []codingMemoryLTMIndexEntry) {
	docID := sop.NewUUID()
	scopeType := "repo"
	scopeID := strings.TrimSpace(mem.RepositoryHint)
	if scopeID == "" {
		scopeID = "default"
	}
	canonicalPayload := map[string]any{
		"document_id":      docID.String(),
		"payload_type":     "coding_workflow_memory",
		"description":      strings.TrimSpace(mem.Description),
		"active_file":      strings.TrimSpace(mem.ActiveFile),
		"files_touched":    mem.FilesTouched,
		"outcome":          strings.TrimSpace(mem.Outcome),
		"test_status":      strings.TrimSpace(mem.TestStatus),
		"lesson":           strings.TrimSpace(mem.Lesson),
		"next_step":        strings.TrimSpace(mem.NextStep),
		"repository_hint":  strings.TrimSpace(mem.RepositoryHint),
		"structural_path":  strings.TrimSpace(mem.StructuralPath),
		"ast_leaf_nodes":   mem.ASTLeafNodes,
		"status":           "active",
		"lifecycle_status": "active",
		"scope_type":       scopeType,
		"scope_id":         scopeID,
		"recorded_at":      time.Now().UnixMilli(),
	}
	pathIndexPath := buildCodingMemoryPathIndexCategoryPath(mem)
	codeStructureIndexPath := buildCodingMemoryCodeStructureCategoryPath(mem)
	semanticIndexPath := buildCodingMemorySemanticCategoryPath(mem)
	indexPayload := map[string]any{
		"document_id":          docID.String(),
		"payload_type":         "coding_workflow_memory",
		"canonical_payload":    canonicalPayload,
		"path_index":           pathIndexPath,
		"code_structure_index": codeStructureIndexPath,
		"semantic_index":       semanticIndexPath,
	}
	entries := []codingMemoryLTMIndexEntry{
		{Kind: "path", CategoryPath: pathIndexPath, Payload: indexPayload},
		{Kind: "code_structure", CategoryPath: codeStructureIndexPath, Payload: indexPayload},
		{Kind: "semantic", CategoryPath: semanticIndexPath, Payload: indexPayload},
	}
	return indexPayload, entries
}

func buildCodingMemoryLTMThoughts(mem cw.CodingMemory, documentID string, entries []codingMemoryLTMIndexEntry) []memory.Thought[map[string]any] {
	thoughts := make([]memory.Thought[map[string]any], 0, len(entries))
	for _, entry := range entries {
		text := fmt.Sprintf("Coding workflow outcome: %s", strings.TrimSpace(mem.Description))
		if mem.Outcome != "" {
			text = fmt.Sprintf("%s\nOutcome: %s", text, mem.Outcome)
		}
		if len(mem.FilesTouched) > 0 {
			text = fmt.Sprintf("%s\nFiles: %s", text, strings.Join(mem.FilesTouched, ", "))
		}
		if mem.TestStatus != "" {
			text = fmt.Sprintf("%s\nTests: %s", text, mem.TestStatus)
		}
		mode := memory.IndexPopulationModeFull
		if strings.EqualFold(strings.TrimSpace(entry.Kind), "path") || strings.EqualFold(strings.TrimSpace(entry.Kind), "code_structure") {
			mode = memory.IndexPopulationModeReference
		}
		thoughts = append(thoughts, memory.Thought[map[string]any]{
			Summaries:           []string{text},
			CategoryPath:        entry.CategoryPath,
			DocID:               memory.DocIDs{documentID},
			IndexPopulationMode: mode,
			Data: map[string]any{
				"document_id":          documentID,
				"index_kind":           entry.Kind,
				"canonical_payload":    entry.Payload["canonical_payload"],
				"path_index":           entry.Payload["path_index"],
				"code_structure_index": entry.Payload["code_structure_index"],
				"semantic_index":       entry.Payload["semantic_index"],
			},
		})
	}
	return thoughts
}

func buildCodingMemoryPathIndexCategoryPath(mem cw.CodingMemory) string {
	parts := []string{"Coding", "Workflow"}
	repoHint := strings.TrimSpace(mem.RepositoryHint)
	if repoHint != "" {
		parts = append(parts, "Repo", repoHint)
	} else {
		parts = append(parts, "Repo", "Default")
	}
	if activeFile := strings.TrimSpace(mem.ActiveFile); activeFile != "" {
		parts = append(parts, "File", filepath.Base(activeFile))
	}
	return strings.Join(parts, "/")
}

// buildCodingMemoryCodeStructureCategoryPath builds hierarchical code-tree category paths
// for O(1) categorical lookup by package/type/method.
func buildCodingMemoryCodeStructureCategoryPath(mem cw.CodingMemory) string {
	// Use AST/Tree-sitter structural path if available
	// Builds code-tree paths like: "Coding/Go/Package/ai.agent.codingworkflow/Type/Orchestrator/Method/Execute"
	if structuralPath := strings.TrimSpace(mem.StructuralPath); structuralPath != "" {
		return buildCodeTreeCategoryPath(mem.ActiveFile, structuralPath, mem.ASTLeafNodes)
	}
	// No structural path available
	return ""
}

// buildCodingMemorySemanticCategoryPath builds NESTED SEMANTIC category paths
// for vector search within semantic "pigeon holes".
//
// Key: Use the SAME FORMULA during storage and retrieval so queries land in correct buckets.
// This solves the scaling problem - can't do flat vector search over millions of snippets,
// but CAN if pre-organized into semantic buckets.
//
// Spaces semantic navigation handles fuzzy matching between vocabulary variants:
//
//	"error-handling" ≈ "exception-handling" ≈ "fault-tolerance"
//	"http-networking" ≈ "http" ≈ "rest-api"
//	Just like: "Technology/Silicon Valley" ≈ "Tech/Palo Alto"
//
// Examples:
//   - "Coding/Semantic/error-handling/retry-logic"
//   - "Coding/Semantic/data-persistence/database/sql-query"
//   - "Coding/Semantic/http-networking/middleware/authentication"
func buildCodingMemorySemanticCategoryPath(mem cw.CodingMemory) string {
	parts := []string{"Coding", "Semantic"}
	seed := []string{}
	if desc := strings.TrimSpace(mem.Description); desc != "" {
		seed = append(seed, desc)
	}
	if outcome := strings.TrimSpace(mem.Outcome); outcome != "" {
		seed = append(seed, outcome)
	}
	if lesson := strings.TrimSpace(mem.Lesson); lesson != "" {
		seed = append(seed, lesson)
	}
	if nextStep := strings.TrimSpace(mem.NextStep); nextStep != "" {
		seed = append(seed, nextStep)
	}
	if activeFile := strings.TrimSpace(mem.ActiveFile); activeFile != "" {
		seed = append(seed, filepath.Base(activeFile))
	}
	for _, candidate := range seed {
		normalized := strings.ToLower(strings.TrimSpace(candidate))
		normalized = strings.NewReplacer(" ", "-", "/", "-", "_", "-", ".", "-").Replace(normalized)
		normalized = strings.Trim(normalized, "-")
		if normalized == "" {
			continue
		}
		parts = append(parts, normalized)
		if len(parts) >= 6 {
			break
		}
	}
	// Extract semantic concepts from code content
	// Use deterministic keyword extraction (same formula for storage & retrieval)
	concepts := extractSemanticConcepts(mem)

	// Build nested semantic category path (max 5 levels deep for pigeon holes)
	for i, concept := range concepts {
		if i >= 3 { // Limit depth: Coding/Semantic/{c1}/{c2}/{c3}
			break
		}
		parts = append(parts, concept)
	}

	// Ensure at least one semantic concept
	if len(parts) == 2 {
		parts = append(parts, "general")
	}

	return strings.Join(parts, "/")
}

// extractSemanticConcepts extracts semantic concept keywords from coding memory
// using a DETERMINISTIC formula (same for storage and retrieval).
//
// This is the key to the "pigeon hole" approach:
// 1. Storage: extract concepts from code → build semantic path → store in bucket
// 2. Retrieval: extract concepts from query → build semantic path → search in bucket
// 3. Match: same formula means query lands in correct semantic bucket
func extractSemanticConcepts(mem cw.CodingMemory) []string {
	text := strings.Join([]string{
		mem.Description,
		mem.Outcome,
		mem.Lesson,
		mem.NextStep,
	}, " ")
	concepts := extractSemanticConceptsFromText(text)
	for _, node := range mem.ASTLeafNodes {
		nodeLower := strings.ToLower(node)
		if strings.Contains(nodeLower, "handler") {
			if !containsString(concepts, "http") {
				concepts = append(concepts, "http")
			}
		} else if strings.Contains(nodeLower, "parser") {
			if !containsString(concepts, "parsing") {
				concepts = append(concepts, "parsing")
			}
		} else if strings.Contains(nodeLower, "validator") {
			if !containsString(concepts, "validation") {
				concepts = append(concepts, "validation")
			}
		}
	}
	return concepts
}

func extractSemanticConceptsFromText(text string) []string {
	var concepts []string
	seenConcepts := make(map[string]bool)

	// Semantic keyword patterns (deterministic extraction)
	// Note: Spaces semantic navigation handles fuzzy matching, so:
	//   "error-handling" ≈ "exception-handling" ≈ "fault-tolerance"
	//   "retry-logic" ≈ "retry-pattern" ≈ "backoff-strategy"
	// We just need consistent conceptual vocabulary - Spaces bridges variants.
	semanticPatterns := []struct {
		concept  string
		keywords []string
	}{
		{concept: "parsing", keywords: []string{"parse", "parser", "decode", "unmarshal", "deserialize", "lexer", "syntax"}},
		{concept: "retry-logic", keywords: []string{"retry", "backoff", "exponential", "circuit-breaker", "resilience"}},
		{concept: "error-handling", keywords: []string{"error", "exception", "failure", "fault", "crash", "panic"}},
		{concept: "data-persistence", keywords: []string{"database", "query", "sql", "orm", "repository", "storage", "persist"}},
		{concept: "http-networking", keywords: []string{"http", "rest", "api", "endpoint", "request", "response", "client", "server"}},
		{concept: "authentication", keywords: []string{"auth", "login", "token", "jwt", "session", "credential", "oauth", "sso"}},
		{concept: "validation", keywords: []string{"validate", "validator", "check", "verify", "sanitize", "constraint"}},
		{concept: "observability", keywords: []string{"log", "logger", "trace", "debug", "monitor", "metric", "telemetry"}},
		{concept: "testing", keywords: []string{"test", "mock", "assert", "fixture", "suite", "unit", "integration"}},
		{concept: "caching", keywords: []string{"cache", "redis", "memcache", "ttl", "memoize", "invalidate"}},
		{concept: "concurrency", keywords: []string{"goroutine", "thread", "async", "parallel", "mutex", "lock", "race", "sync"}},
		{concept: "configuration", keywords: []string{"config", "settings", "env", "environment", "flag", "parameter"}},
		{concept: "middleware", keywords: []string{"middleware", "interceptor", "filter", "handler", "pipeline"}},
		{concept: "serialization", keywords: []string{"json", "xml", "yaml", "proto", "marshal", "encode", "format"}},
		{concept: "security", keywords: []string{"security", "encrypt", "decrypt", "hash", "sign", "certificate", "tls"}},
		{concept: "routing", keywords: []string{"route", "router", "path", "url", "match", "dispatch"}},
		{concept: "transformation", keywords: []string{"transform", "convert", "map", "reduce", "filter", "process"}},
		{concept: "scheduling", keywords: []string{"schedule", "cron", "timer", "interval", "periodic", "job"}},
	}

	textLower := strings.ToLower(text)

	// Match semantic patterns
	for _, pattern := range semanticPatterns {
		for _, keyword := range pattern.keywords {
			if strings.Contains(textLower, keyword) {
				if !seenConcepts[pattern.concept] {
					concepts = append(concepts, pattern.concept)
					seenConcepts[pattern.concept] = true
					break
				}
			}
		}
	}

	return concepts
}

func buildSemanticResearchPaths(query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	concepts := extractSemanticConceptsFromText(query)
	if len(concepts) == 0 {
		return nil
	}
	parts := []string{"Coding", "Semantic"}
	for _, concept := range concepts {
		if len(parts) >= 5 {
			break
		}
		parts = append(parts, concept)
	}
	if len(parts) <= 2 {
		return nil
	}
	return []string{strings.Join(parts, "/")}
}

// buildCodeTreeCategoryPath constructs hierarchical code-tree category paths
// from tree-sitter/AST structural information for O(1) categorical lookup.
//
// Examples:
//   - "Coding/Go/Package/ai.agent.codingworkflow/Type/Orchestrator/Method/Execute"
//   - "Coding/Python/Package/retriever/Class/VectorRetriever/Method/search"
//   - "Coding/TypeScript/Package/utils/Namespace/format/Function/formatDate"
func buildCodeTreeCategoryPath(activeFile, structuralPath string, astLeafNodes []string) string {
	// Detect language from file extension
	ext := strings.ToLower(filepath.Ext(activeFile))
	language := "Generic"
	symbolType := "Function"

	switch ext {
	case ".go":
		language = "Go"
		// In Go: could be Function or Method depending on receiver
		// For simplicity, use Type if multiple AST nodes (suggesting struct + methods)
		if len(astLeafNodes) > 1 {
			symbolType = "Type"
		}
	case ".py":
		language = "Python"
		symbolType = "Class"
	case ".ts", ".tsx":
		language = "TypeScript"
		symbolType = "Namespace"
	case ".js", ".jsx":
		language = "JavaScript"
		symbolType = "Namespace"
	case ".java":
		language = "Java"
		symbolType = "Class"
	case ".cs":
		language = "CSharp"
		symbolType = "Namespace"
	case ".rb":
		language = "Ruby"
		symbolType = "Class"
	case ".rs":
		language = "Rust"
		symbolType = "Module"
	}

	// Parse structural path: "sop-cloud/ai/agent/Execute"
	// Extract: package/module path and symbol name
	pathParts := strings.Split(structuralPath, "/")
	if len(pathParts) < 2 {
		// Fallback: use simple path
		return fmt.Sprintf("Coding/%s/%s/%s", language, symbolType, structuralPath)
	}

	// Skip repo name (first part), extract package path and symbol
	var packagePath string
	var symbolName string

	if len(pathParts) == 2 {
		// "repo/Symbol"
		symbolName = pathParts[1]
		packagePath = "main"
	} else {
		// "repo/path/to/package/Symbol"
		symbolName = pathParts[len(pathParts)-1]
		packageParts := pathParts[1 : len(pathParts)-1]
		packagePath = strings.Join(packageParts, ".")
	}

	// Build hierarchical code-tree path
	parts := []string{"Coding", language, "Package", packagePath}

	// Add symbol type and name
	if len(astLeafNodes) > 1 {
		// Multiple AST nodes suggest Type with Methods
		parts = append(parts, symbolType, astLeafNodes[0])
		if len(astLeafNodes) > 1 {
			parts = append(parts, "Method", astLeafNodes[1])
		}
	} else if len(astLeafNodes) == 1 {
		// Single AST node: use as function/method
		parts = append(parts, symbolType, astLeafNodes[0])
	} else {
		// No AST nodes: use extracted symbol from path
		parts = append(parts, symbolType, symbolName)
	}

	return strings.Join(parts, "/")
}

func buildLTMMemoryMetadata(category, capability, scopeType, scopeID string) map[string]any {
	category = strings.TrimSpace(category)
	capability = strings.TrimSpace(capability)
	scopeType = strings.TrimSpace(scopeType)
	scopeID = strings.TrimSpace(scopeID)
	if category == "" {
		category = "general"
	}
	if capability == "" {
		capability = "general"
	}
	if scopeType == "" {
		scopeType = "global"
	}
	if scopeID == "" {
		scopeID = "global"
	}
	now := time.Now().UnixMilli()
	return map[string]any{
		"category":                    category,
		"capability":                  capability,
		"scope_type":                  scopeType,
		"scope_id":                    scopeID,
		"provenance":                  "agent",
		"status":                      "active",
		"lifecycle_status":            "active",
		"lifecycle_access_count":      1,
		"lifecycle_last_used_at":      now,
		"lifecycle_last_evaluated_at": now,
		"updated_at":                  now,
	}
}

func isRepoScopedLTMPayload(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	scopeType := strings.TrimSpace(fmt.Sprint(payload["scope_type"]))
	if scopeType == "" {
		scopeType = strings.TrimSpace(fmt.Sprint(payload["scopeType"]))
	}
	if scopeType != "" && !strings.EqualFold(scopeType, "repo") {
		return false
	}
	if scopeType == "repo" {
		return true
	}
	repoHint := strings.TrimSpace(fmt.Sprint(payload["repository_hint"]))
	if repoHint == "" {
		repoHint = strings.TrimSpace(fmt.Sprint(payload["scope_id"]))
	}
	if repoHint == "" {
		repoHint = strings.TrimSpace(fmt.Sprint(payload["scopeID"]))
	}
	return repoHint != ""
}

func touchLTMLifecycleForRepoAccess(payload map[string]any, now time.Time) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	if !isRepoScopedLTMPayload(payload) {
		return payload
	}
	if now.IsZero() {
		now = time.Now()
	}
	accessCount := 1
	if existingCount, ok := payload["lifecycle_access_count"].(int); ok {
		accessCount = existingCount + 1
	} else if existingCount, ok := payload["lifecycle_access_count"].(int64); ok {
		accessCount = int(existingCount) + 1
	} else if existingCount, ok := payload["lifecycle_access_count"].(float64); ok {
		accessCount = int(existingCount) + 1
	}
	lastUsedAt := now.UnixMilli()
	payload["lifecycle_access_count"] = accessCount
	payload["lifecycle_last_used_at"] = lastUsedAt
	payload["lifecycle_last_evaluated_at"] = lastUsedAt
	payload["updated_at"] = lastUsedAt
	payload["status"] = "active"
	payload["lifecycle_status"] = "active"
	payload["scope_valid"] = true
	delete(payload, "archived_at")
	delete(payload, "archive_reason")
	return payload
}

func touchRepoScopedLTMLifecycleState(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], scopeID string, now time.Time) error {
	if ctx == nil || kb == nil || strings.TrimSpace(scopeID) == "" {
		return nil
	}
	itemsTree, err := kb.Store.Items(ctx)
	if err != nil {
		return err
	}
	scopeID = strings.TrimSpace(scopeID)
	ok, err := itemsTree.First(ctx)
	if err != nil {
		return err
	}
	for ok {
		currentKey := itemsTree.GetCurrentKey()
		item, err := itemsTree.GetCurrentValue(ctx)
		if err != nil {
			return err
		}
		payload, okPayload := any(item.Data).(map[string]any)
		if !okPayload || payload == nil {
			payload = map[string]any{}
		}
		if isRepoScopedLTMPayload(payload) {
			payloadScopeID := strings.TrimSpace(fmt.Sprint(payload["scope_id"]))
			if payloadScopeID == "" {
				payloadScopeID = strings.TrimSpace(fmt.Sprint(payload["scopeID"]))
			}
			if payloadScopeID == "" {
				payloadScopeID = strings.TrimSpace(fmt.Sprint(payload["repository_hint"]))
			}
			if strings.EqualFold(payloadScopeID, scopeID) {
				payload = touchLTMLifecycleForRepoAccess(payload, now)
				item.Data = payload
				if _, err := itemsTree.UpdateCurrentItem(ctx, currentKey.Key, item); err != nil {
					return err
				}
			}
		}
		ok, err = itemsTree.Next(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func updateLTMMemoryLifecycleState(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], itemID string, relevanceScore int) error {
	if ctx == nil || kb == nil || strings.TrimSpace(itemID) == "" {
		return nil
	}

	parsedID, err := sop.ParseUUID(itemID)
	if err != nil {
		return nil
	}

	itemsTree, err := kb.Store.Items(ctx)
	if err != nil {
		return err
	}

	ok, err := itemsTree.First(ctx)
	if err != nil {
		return err
	}
	for ok {
		currentKey := itemsTree.GetCurrentKey()
		item, err := itemsTree.GetCurrentValue(ctx)
		if err != nil {
			return err
		}
		if item.ID == parsedID {
			payload, ok := any(item.Data).(map[string]any)
			if !ok {
				payload = map[string]any{}
			}
			if payload == nil {
				payload = map[string]any{}
			}
			now := time.Now()
			payload = touchLTMLifecycleForRepoAccess(payload, now)
			if !isRepoScopedLTMPayload(payload) {
				accessCount := 1
				if existingCount, ok := payload["lifecycle_access_count"].(int); ok {
					accessCount = existingCount + 1
					payload["lifecycle_access_count"] = accessCount
				} else if existingCount, ok := payload["lifecycle_access_count"].(int64); ok {
					accessCount = int(existingCount) + 1
					payload["lifecycle_access_count"] = accessCount
				} else if existingCount, ok := payload["lifecycle_access_count"].(float64); ok {
					accessCount = int(existingCount) + 1
					payload["lifecycle_access_count"] = accessCount
				} else {
					payload["lifecycle_access_count"] = accessCount
				}
				lastUsedAt := now.UnixMilli()
				if existingLastUsedAt, ok := payload["lifecycle_last_used_at"].(int64); ok {
					lastUsedAt = existingLastUsedAt
				} else if existingLastUsedAt, ok := payload["lifecycle_last_used_at"].(int); ok {
					lastUsedAt = int64(existingLastUsedAt)
				} else if existingLastUsedAt, ok := payload["lifecycle_last_used_at"].(float64); ok {
					lastUsedAt = int64(existingLastUsedAt)
				}
				recordedAt := now.UnixMilli()
				if existingRecordedAt, ok := payload["recorded_at"].(int64); ok {
					recordedAt = existingRecordedAt
				} else if existingRecordedAt, ok := payload["recorded_at"].(int); ok {
					recordedAt = int64(existingRecordedAt)
				} else if existingRecordedAt, ok := payload["recorded_at"].(float64); ok {
					recordedAt = int64(existingRecordedAt)
				}
				lastUsedDays := 0
				if now.UnixMilli() > lastUsedAt {
					lastUsedDays = int((now.UnixMilli() - lastUsedAt) / int64(24*time.Hour/time.Millisecond))
				}
				ageDays := 0
				if now.UnixMilli() > recordedAt {
					ageDays = int((now.UnixMilli() - recordedAt) / int64(24*time.Hour/time.Millisecond))
				}
				scopeValid := true
				if existingScopeValid, ok := payload["scope_valid"].(bool); ok {
					scopeValid = existingScopeValid
				}
				status := evaluateLTMLifecycleStatus(lastUsedDays, ageDays, accessCount, relevanceScore, scopeValid)
				payload["lifecycle_last_used_at"] = now.UnixMilli()
				payload["lifecycle_last_evaluated_at"] = now.UnixMilli()
				payload["updated_at"] = now.UnixMilli()
				payload["status"] = status.State
				payload["lifecycle_status"] = status.State
			}
			item.Data = payload
			_, err = itemsTree.UpdateCurrentItem(ctx, currentKey.Key, item)
			return err
		}
		ok, err = itemsTree.Next(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

type ltmRetrievalTarget struct {
	Category   string
	Capability string
	ScopeType  string
	ScopeID    string
}

type ltmLifecycleRetentionDecision struct {
	Action     string
	Reason     string
	Confidence int
}

type ltmLifecycleSignal struct {
	Action     string
	ScopeType  string
	ScopeID    string
	Reason     string
	Confidence int
}

type ltmLifecycleStatus struct {
	State        string
	Reason       string
	LastUsedDays int
	AgeDays      int
	Score        int
}

func applyLTMLifecycleSignal(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], signal ltmLifecycleSignal) error {
	if ctx == nil || kb == nil || signal.Action == "" {
		return nil
	}
	itemsTree, err := kb.Store.Items(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	ok, err := itemsTree.First(ctx)
	if err != nil {
		return err
	}
	for ok {
		currentKey := itemsTree.GetCurrentKey()
		item, err := itemsTree.GetCurrentValue(ctx)
		if err != nil {
			return err
		}
		payload, okPayload := any(item.Data).(map[string]any)
		if !okPayload || payload == nil {
			payload = map[string]any{}
		}
		if !ltmLifecycleSignalMatchesPayload(payload, signal) {
			ok, err = itemsTree.Next(ctx)
			if err != nil {
				return err
			}
			continue
		}
		switch strings.ToLower(signal.Action) {
		case "retire":
			payload["lifecycle_status"] = "archived"
			payload["status"] = "archived"
			payload["archived_at"] = now
			payload["archive_reason"] = strings.TrimSpace(signal.Reason)
			if signal.Reason == "" {
				payload["archive_reason"] = "explicit_user_cue"
			}
			payload["lifecycle_last_evaluated_at"] = now
			payload["updated_at"] = now
		case "reactivate":
			payload["lifecycle_status"] = "active"
			payload["status"] = "active"
			payload["scope_valid"] = true
			delete(payload, "archived_at")
			delete(payload, "archive_reason")
			payload["lifecycle_last_used_at"] = now
			payload["lifecycle_last_evaluated_at"] = now
			payload["updated_at"] = now
			if existingCount, ok := payload["lifecycle_access_count"].(int); ok {
				payload["lifecycle_access_count"] = existingCount + 1
			} else if existingCount, ok := payload["lifecycle_access_count"].(int64); ok {
				payload["lifecycle_access_count"] = int(existingCount) + 1
			} else if existingCount, ok := payload["lifecycle_access_count"].(float64); ok {
				payload["lifecycle_access_count"] = int(existingCount) + 1
			} else {
				payload["lifecycle_access_count"] = 1
			}
		}
		item.Data = payload
		if _, err := itemsTree.UpdateCurrentItem(ctx, currentKey.Key, item); err != nil {
			return err
		}
		ok, err = itemsTree.Next(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func ltmLifecycleSignalMatchesPayload(payload map[string]any, signal ltmLifecycleSignal) bool {
	if payload == nil || len(payload) == 0 {
		return signal.ScopeType == "" || signal.ScopeType == "global"
	}
	if signal.ScopeType == "" || signal.ScopeType == "global" {
		return true
	}
	scopeType := strings.TrimSpace(fmt.Sprint(payload["scope_type"]))
	if scopeType == "" {
		scopeType = strings.TrimSpace(fmt.Sprint(payload["scopeType"]))
	}
	scopeID := strings.TrimSpace(fmt.Sprint(payload["scope_id"]))
	if scopeID == "" {
		scopeID = strings.TrimSpace(fmt.Sprint(payload["scopeID"]))
	}
	if scopeType == "" && scopeID == "" {
		scopeType = strings.TrimSpace(fmt.Sprint(payload["repository_hint"]))
		scopeID = strings.TrimSpace(fmt.Sprint(payload["repository_hint"]))
	}
	if scopeType == "" {
		return false
	}
	if signal.ScopeType != "" && !strings.EqualFold(scopeType, signal.ScopeType) {
		return false
	}
	if signal.ScopeID != "" && signal.ScopeID != "global" {
		if scopeID == "" {
			return false
		}
		return strings.EqualFold(scopeID, signal.ScopeID)
	}
	return true
}

func evaluateLTMLifecycleStatus(lastUsedDays, ageDays, accessCount, relevanceScore int, scopeValid bool) ltmLifecycleStatus {
	if lastUsedDays <= 14 && ageDays <= 30 && accessCount >= 2 && relevanceScore >= 10 {
		return ltmLifecycleStatus{State: "active", Reason: "recent_and_relevant", LastUsedDays: lastUsedDays, AgeDays: ageDays, Score: 20}
	}
	if lastUsedDays <= 60 && accessCount >= 1 {
		return ltmLifecycleStatus{State: "warm", Reason: "recently_seen", LastUsedDays: lastUsedDays, AgeDays: ageDays, Score: 45}
	}
	if !scopeValid {
		return ltmLifecycleStatus{State: "stale", Reason: "invalid_scope", LastUsedDays: lastUsedDays, AgeDays: ageDays, Score: 100}
	}
	return ltmLifecycleStatus{State: "stale", Reason: "cold_or_unused", LastUsedDays: lastUsedDays, AgeDays: ageDays, Score: 80}
}

func decideLTMLifecycleRetention(payload map[string]any, now time.Time) ltmLifecycleRetentionDecision {
	if len(payload) == 0 || now.IsZero() {
		return ltmLifecycleRetentionDecision{}
	}
	status := strings.TrimSpace(fmt.Sprint(payload["lifecycle_status"]))
	if status == "" {
		status = strings.TrimSpace(fmt.Sprint(payload["status"]))
	}
	if status == "" || strings.EqualFold(status, "active") || strings.EqualFold(status, "warm") {
		return ltmLifecycleRetentionDecision{}
	}
	if strings.EqualFold(status, "archived") || strings.EqualFold(status, "deleted") {
		return ltmLifecycleRetentionDecision{}
	}

	lastUsedAt := int64(0)
	if v, ok := payload["lifecycle_last_used_at"].(int64); ok {
		lastUsedAt = v
	} else if v, ok := payload["lifecycle_last_used_at"].(int); ok {
		lastUsedAt = int64(v)
	} else if v, ok := payload["lifecycle_last_used_at"].(float64); ok {
		lastUsedAt = int64(v)
	}
	recordedAt := int64(0)
	if v, ok := payload["recorded_at"].(int64); ok {
		recordedAt = v
	} else if v, ok := payload["recorded_at"].(int); ok {
		recordedAt = int64(v)
	} else if v, ok := payload["recorded_at"].(float64); ok {
		recordedAt = int64(v)
	}
	if lastUsedAt == 0 {
		lastUsedAt = recordedAt
	}
	if recordedAt == 0 {
		recordedAt = lastUsedAt
	}
	accessCount := 1
	if v, ok := payload["lifecycle_access_count"].(int); ok {
		accessCount = v
	} else if v, ok := payload["lifecycle_access_count"].(int64); ok {
		accessCount = int(v)
	} else if v, ok := payload["lifecycle_access_count"].(float64); ok {
		accessCount = int(v)
	}
	scopeValid := true
	if v, ok := payload["scope_valid"].(bool); ok {
		scopeValid = v
	}

	lastUsedDays := 0
	if lastUsedAt > 0 && now.UnixMilli() > lastUsedAt {
		lastUsedDays = int((now.UnixMilli() - lastUsedAt) / int64(24*time.Hour/time.Millisecond))
	}
	ageDays := 0
	if recordedAt > 0 && now.UnixMilli() > recordedAt {
		ageDays = int((now.UnixMilli() - recordedAt) / int64(24*time.Hour/time.Millisecond))
	}

	if !scopeValid {
		return ltmLifecycleRetentionDecision{Action: "archive", Reason: "invalid_scope", Confidence: 90}
	}
	if strings.EqualFold(status, "stale") && (lastUsedDays > 90 || ageDays > 180 || (accessCount <= 1 && ageDays > 90)) {
		return ltmLifecycleRetentionDecision{Action: "archive", Reason: "cold_or_unused", Confidence: 80}
	}
	if lastUsedDays > 180 && accessCount <= 1 {
		return ltmLifecycleRetentionDecision{Action: "archive", Reason: "unused_for_long_time", Confidence: 75}
	}
	return ltmLifecycleRetentionDecision{}
}

func (a *CopilotAgent) applyLTMLifecycleRetention(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], now time.Time) (int, error) {
	if ctx == nil || kb == nil || a == nil || now.IsZero() {
		return 0, nil
	}
	itemsTree, err := kb.Store.Items(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	ok, err := itemsTree.First(ctx)
	if err != nil {
		return 0, err
	}
	for ok {
		currentKey := itemsTree.GetCurrentKey()
		item, err := itemsTree.GetCurrentValue(ctx)
		if err != nil {
			return 0, err
		}
		payload, ok := any(item.Data).(map[string]any)
		if !ok {
			payload = map[string]any{}
		}
		decision := decideLTMLifecycleRetention(payload, now)
		if decision.Action == "archive" {
			payload["lifecycle_status"] = "archived"
			payload["status"] = "archived"
			payload["lifecycle_last_evaluated_at"] = now.UnixMilli()
			payload["updated_at"] = now.UnixMilli()
			payload["archived_at"] = now.UnixMilli()
			payload["archive_reason"] = decision.Reason
			item.Data = payload
			if _, err := itemsTree.UpdateCurrentItem(ctx, currentKey.Key, item); err != nil {
				return 0, err
			}
			count++
		}
		ok, err = itemsTree.Next(ctx)
		if err != nil {
			return 0, err
		}
	}
	return count, nil
}

func decideLTMLifecycleCompaction(payload map[string]any, now time.Time) bool {
	if payload == nil || len(payload) == 0 || now.IsZero() {
		return false
	}
	status := strings.TrimSpace(fmt.Sprint(payload["lifecycle_status"]))
	if status == "" {
		status = strings.TrimSpace(fmt.Sprint(payload["status"]))
	}
	if !strings.EqualFold(status, "archived") && !strings.EqualFold(status, "deleted") {
		return false
	}
	archivedAt := int64(0)
	if v, ok := payload["archived_at"].(int64); ok {
		archivedAt = v
	} else if v, ok := payload["archived_at"].(int); ok {
		archivedAt = int64(v)
	} else if v, ok := payload["archived_at"].(float64); ok {
		archivedAt = int64(v)
	}
	if archivedAt == 0 {
		return false
	}
	return now.UnixMilli()-archivedAt >= int64(7*24*time.Hour/time.Millisecond)
}

func (a *CopilotAgent) applyLTMLifecycleCompaction(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], now time.Time) (int, error) {
	if ctx == nil || kb == nil || a == nil || now.IsZero() {
		return 0, nil
	}
	itemsTree, err := kb.Store.Items(ctx)
	if err != nil {
		return 0, err
	}
	var pruneKeys []memory.ItemKey
	ok, err := itemsTree.First(ctx)
	if err != nil {
		return 0, err
	}
	for ok {
		currentKey := itemsTree.GetCurrentKey()
		item, err := itemsTree.GetCurrentValue(ctx)
		if err != nil {
			return 0, err
		}
		payload, okPayload := any(item.Data).(map[string]any)
		if !okPayload {
			payload = map[string]any{}
		}
		if decideLTMLifecycleCompaction(payload, now) {
			pruneKeys = append(pruneKeys, memory.ItemKey{CategoryID: currentKey.Key.CategoryID, ItemID: currentKey.Key.ItemID})
		}
		ok, err = itemsTree.Next(ctx)
		if err != nil {
			return 0, err
		}
	}
	if len(pruneKeys) == 0 {
		return 0, nil
	}
	if err := kb.DeleteItems(ctx, pruneKeys); err != nil {
		return 0, err
	}
	return len(pruneKeys), nil
}

func detectLTMLifecycleSignal(query string) ltmLifecycleSignal {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return ltmLifecycleSignal{}
	}
	for _, pattern := range []struct {
		prefix string
		action string
		scope  string
	}{
		{prefix: "done with repo ", action: "retire", scope: "repo"},
		{prefix: "done with space ", action: "retire", scope: "space"},
		{prefix: "finished with repo ", action: "retire", scope: "repo"},
		{prefix: "finished with space ", action: "retire", scope: "space"},
		{prefix: "continue with repo ", action: "reactivate", scope: "repo"},
		{prefix: "continue with space ", action: "reactivate", scope: "space"},
		{prefix: "resume repo ", action: "reactivate", scope: "repo"},
		{prefix: "resume space ", action: "reactivate", scope: "space"},
		{prefix: "archive this context", action: "archive", scope: "global"},
		{prefix: "forget this", action: "archive", scope: "global"},
	} {
		if strings.HasPrefix(q, pattern.prefix) {
			value := strings.TrimSpace(strings.TrimPrefix(q, pattern.prefix))
			value = strings.Trim(value, " .,:;!?\"")
			if value == "" {
				value = "global"
			}
			return ltmLifecycleSignal{Action: pattern.action, ScopeType: pattern.scope, ScopeID: value, Reason: "explicit_user_cue", Confidence: 90}
		}
	}
	return ltmLifecycleSignal{}
}

func filterLTMRetrievalTargets(targets []ltmRetrievalTarget, signal ltmLifecycleSignal) []ltmRetrievalTarget {
	if signal.Action == "" {
		return targets
	}
	if signal.ScopeType == "" || signal.ScopeID == "" {
		return targets
	}
	filtered := make([]ltmRetrievalTarget, 0, len(targets))
	for _, target := range targets {
		if target.ScopeType == signal.ScopeType && target.ScopeID == signal.ScopeID {
			continue
		}
		filtered = append(filtered, target)
	}
	return filtered
}

func buildLTMRetrievalTargets(query string, payload *ai.SessionPayload, repoHint string) []ltmRetrievalTarget {
	var targets []ltmRetrievalTarget
	query = strings.TrimSpace(query)
	if payload != nil && len(payload.SelectedKBs) > 0 {
		for _, kb := range payload.SelectedKBs {
			name := strings.TrimSpace(kb.Name)
			if name == "" {
				continue
			}
			if strings.EqualFold(string(kb.Type), string(ai.ArtifactTypeSpace)) || strings.EqualFold(string(kb.Type), "space") {
				targets = append(targets, ltmRetrievalTarget{Category: "research", Capability: "research_workflow", ScopeType: "space", ScopeID: name})
			}
		}
	}

	repoHint = strings.TrimSpace(repoHint)
	if repoHint != "" {
		targets = append(targets, ltmRetrievalTarget{Category: "coding", Capability: "coding_workflow", ScopeType: "repo", ScopeID: repoHint})
	}
	if len(targets) == 0 {
		if query != "" {
			targets = append(targets, ltmRetrievalTarget{Category: "general", Capability: "general", ScopeType: "global", ScopeID: "global"})
		}
	}
	return targets
}

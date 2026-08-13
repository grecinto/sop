package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
	"github.com/sharedcode/sop/ai/memory"
)

const (
	ltmRepoStaleArchiveDays          = 90
	ltmRepoStalePruneDays            = 180
	ltmArchivedCompactionDays        = 30
	ltmPressureCompactionDays        = 14
	ltmGenericArchiveDays            = 120
	ltmGenericHardPruneDays          = 730
	ltmArchivedSummaryMinAgeDays     = 180
	ltmColdUnusedLastUsedDays        = 120
	ltmColdUnusedAgeDays             = 270
	ltmPressureForecastThreshold     = 0.7
	ltmPressureCleanupThreshold      = 0.85
	ltmPressureCriticalThreshold     = 0.95
	ltmRepoNearThresholdPressure     = 0.7
	ltmRepoHighPressureThreshold     = 0.85
	ltmRepoPressureArchiveAgeDays    = 60
	ltmRepoPressureArchiveMinAgeDays = 30
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
	now := time.Now().UnixMilli()
	canonicalPayload := map[string]any{
		"document_id":                 docID.String(),
		"payload_type":                "coding_workflow_memory",
		"description":                 strings.TrimSpace(mem.Description),
		"active_file":                 strings.TrimSpace(mem.ActiveFile),
		"files_touched":               mem.FilesTouched,
		"outcome":                     strings.TrimSpace(mem.Outcome),
		"test_status":                 strings.TrimSpace(mem.TestStatus),
		"lesson":                      strings.TrimSpace(mem.Lesson),
		"next_step":                   strings.TrimSpace(mem.NextStep),
		"repository_hint":             strings.TrimSpace(mem.RepositoryHint),
		"structural_path":             strings.TrimSpace(mem.StructuralPath),
		"ast_leaf_nodes":              mem.ASTLeafNodes,
		"status":                      "active",
		"lifecycle_status":            "active",
		"scope_type":                  scopeType,
		"scope_id":                    scopeID,
		"lifecycle_access_count":      1,
		"lifecycle_last_used_at":      now,
		"lifecycle_last_evaluated_at": now,
		"updated_at":                  now,
		"recorded_at":                 now,
	}
	pathIndexPath := buildCodingMemoryPathIndexCategoryPath(mem)
	codeStructureIndexPath := buildCodingMemoryCodeStructureCategoryPath(mem)
	semanticIndexPath := buildCodingMemorySemanticCategoryPath(mem)
	indexPayload := map[string]any{
		"document_id":                 docID.String(),
		"payload_type":                "coding_workflow_memory",
		"canonical_payload":           canonicalPayload,
		"path_index":                  pathIndexPath,
		"code_structure_index":        codeStructureIndexPath,
		"semantic_index":              semanticIndexPath,
		"status":                      "active",
		"lifecycle_status":            "active",
		"scope_type":                  scopeType,
		"scope_id":                    scopeID,
		"repository_hint":             strings.TrimSpace(mem.RepositoryHint),
		"lifecycle_access_count":      1,
		"lifecycle_last_used_at":      now,
		"lifecycle_last_evaluated_at": now,
		"updated_at":                  now,
		"recorded_at":                 now,
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
		stored := map[string]any{
			"document_id":                 documentID,
			"index_kind":                  entry.Kind,
			"canonical_payload":           entry.Payload["canonical_payload"],
			"path_index":                  entry.Payload["path_index"],
			"code_structure_index":        entry.Payload["code_structure_index"],
			"semantic_index":              entry.Payload["semantic_index"],
			"status":                      entry.Payload["status"],
			"lifecycle_status":            entry.Payload["lifecycle_status"],
			"scope_type":                  entry.Payload["scope_type"],
			"scope_id":                    entry.Payload["scope_id"],
			"repository_hint":             entry.Payload["repository_hint"],
			"lifecycle_access_count":      entry.Payload["lifecycle_access_count"],
			"lifecycle_last_used_at":      entry.Payload["lifecycle_last_used_at"],
			"lifecycle_last_evaluated_at": entry.Payload["lifecycle_last_evaluated_at"],
			"updated_at":                  entry.Payload["updated_at"],
			"recorded_at":                 entry.Payload["recorded_at"],
		}
		thoughts = append(thoughts, memory.Thought[map[string]any]{
			Summaries:           []string{text},
			CategoryPath:        entry.CategoryPath,
			DocID:               memory.DocIDs{documentID},
			IndexPopulationMode: mode,
			Data:                stored,
		})
	}
	return thoughts
}

// Coding workflow artifacts intentionally live under a distinct root category from
// general LTM memory. The underlying LTM substrate is shared, but the taxonomy is
// separated so repo/code intelligence recall stays in the Coding namespace while
// conversational memory stays in the Memory namespace.
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
func buildSemanticCategoryPathFromConcepts(concepts []string) string {
	parts := []string{"Coding", "Semantic"}
	for _, concept := range concepts {
		concept = strings.TrimSpace(concept)
		if concept == "" {
			continue
		}
		concept = strings.ToLower(concept)
		concept = strings.NewReplacer(" ", "-", "/", "-", "_", "-", ".", "-").Replace(concept)
		concept = strings.Trim(concept, "-")
		if concept == "" {
			continue
		}
		parts = append(parts, concept)
		if len(parts) >= 5 {
			break
		}
	}
	if len(parts) <= 2 {
		parts = append(parts, "general")
	}
	return strings.Join(parts, "/")
}

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
	concepts := extractSemanticConcepts(mem)
	if len(concepts) > 0 {
		for _, concept := range concepts {
			parts = append(parts, strings.ToLower(strings.TrimSpace(concept)))
			if len(parts) >= 5 {
				break
			}
		}
	}
	if len(parts) <= 2 {
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
	path := buildSemanticCategoryPathFromConcepts(concepts)
	if path == "" || path == "Coding/Semantic/general" {
		return nil
	}
	return []string{path}
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

type StorageLifecyclePolicy struct {
	Kind               string
	ScopeType          string
	ScopeID            string
	StoragePath        string
	ForecastThreshold  float64
	CleanupThreshold   float64
	Cooldown           time.Duration
	ArchiveAfterDays   int
	HardPruneAfterDays int
}

func NewStorageLifecyclePolicy(kind, scopeType, scopeID, storagePath string) StorageLifecyclePolicy {
	policy := StorageLifecyclePolicy{
		Kind:               strings.TrimSpace(kind),
		ScopeType:          strings.TrimSpace(scopeType),
		ScopeID:            strings.TrimSpace(scopeID),
		StoragePath:        strings.TrimSpace(storagePath),
		ForecastThreshold:  ltmPressureForecastThreshold,
		CleanupThreshold:   ltmPressureCleanupThreshold,
		Cooldown:           12 * time.Hour,
		ArchiveAfterDays:   ltmRepoStaleArchiveDays,
		HardPruneAfterDays: ltmGenericHardPruneDays,
	}
	if policy.Kind == "" {
		policy.Kind = "generic"
	}
	if policy.ScopeType == "" {
		policy.ScopeType = "repo"
	}
	return policy
}

func (p StorageLifecyclePolicy) BuildPressureSummary(now time.Time) map[string]any {
	pressure := estimateLTMStoragePressure(p.StoragePath)
	threshold := p.CleanupThreshold
	if threshold <= 0 {
		threshold = ltmPressureCleanupThreshold
	}
	forecastThreshold := p.ForecastThreshold
	if forecastThreshold <= 0 {
		forecastThreshold = ltmPressureForecastThreshold
	}
	state := "normal"
	switch {
	case pressure >= ltmPressureCriticalThreshold:
		state = "critical"
	case pressure >= threshold:
		state = "high"
	case pressure >= forecastThreshold:
		state = "moderate"
	}
	if now.IsZero() {
		now = time.Now()
	}
	return map[string]any{
		"kind":               p.Kind,
		"scope_type":         p.ScopeType,
		"scope_id":           p.ScopeID,
		"timestamp":          now.UnixMilli(),
		"usage_ratio":        pressure,
		"forecast_threshold": forecastThreshold,
		"threshold":          threshold,
		"pressure_state":     state,
		"cleanup_triggered":  pressure >= threshold,
		"forecast_triggered": pressure >= forecastThreshold,
	}
}

func (p StorageLifecyclePolicy) DecideAction(summary map[string]any) map[string]any {
	if summary == nil {
		return nil
	}
	pressure, _ := summary["usage_ratio"].(float64)
	state := strings.TrimSpace(fmt.Sprint(summary["pressure_state"]))
	action := "monitor"
	switch {
	case state == "critical":
		action = "archive_and_compact"
	case state == "high":
		action = "compact"
	case pressure >= p.ForecastThreshold:
		action = "monitor_and_review"
	}
	summary["action"] = action
	summary["triggered"] = summary["forecast_triggered"] == true || summary["cleanup_triggered"] == true
	return summary
}

func (p StorageLifecyclePolicy) TouchAccess(payload map[string]any, now time.Time) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	if now.IsZero() {
		now = time.Now()
	}
	if strings.TrimSpace(fmt.Sprint(payload["scope_type"])) == "" {
		payload["scope_type"] = p.ScopeType
	}
	if p.ScopeID != "" {
		payload["scope_id"] = p.ScopeID
	}
	if strings.TrimSpace(fmt.Sprint(payload["scope_id"])) == "" {
		payload["scope_id"] = p.ScopeID
	}
	count := 1
	if existingCount, ok := payload["lifecycle_access_count"].(int); ok {
		count = existingCount + 1
	} else if existingCount, ok := payload["lifecycle_access_count"].(int64); ok {
		count = int(existingCount) + 1
	} else if existingCount, ok := payload["lifecycle_access_count"].(float64); ok {
		count = int(existingCount) + 1
	}
	payload["lifecycle_access_count"] = count
	payload["lifecycle_last_used_at"] = now.UnixMilli()
	payload["lifecycle_last_evaluated_at"] = now.UnixMilli()
	payload["updated_at"] = now.UnixMilli()
	payload["status"] = "active"
	payload["lifecycle_status"] = "active"
	payload["scope_valid"] = true
	delete(payload, "archived_at")
	delete(payload, "archive_reason")
	return payload
}

func (p StorageLifecyclePolicy) EvaluateStatus(lastUsedDays, ageDays, accessCount, relevanceScore int, scopeValid bool) ltmLifecycleStatus {
	if lastUsedDays <= 14 && ageDays <= 30 && accessCount >= 2 && relevanceScore >= 10 {
		return ltmLifecycleStatus{State: "active", Reason: "recent_and_relevant", LastUsedDays: lastUsedDays, AgeDays: ageDays, Score: 20}
	}
	if lastUsedDays <= 90 && accessCount >= 1 {
		return ltmLifecycleStatus{State: "warm", Reason: "recently_seen", LastUsedDays: lastUsedDays, AgeDays: ageDays, Score: 45}
	}
	if !scopeValid {
		return ltmLifecycleStatus{State: "stale", Reason: "invalid_scope", LastUsedDays: lastUsedDays, AgeDays: ageDays, Score: 100}
	}
	return ltmLifecycleStatus{State: "stale", Reason: "cold_or_unused", LastUsedDays: lastUsedDays, AgeDays: ageDays, Score: 80}
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
	if lastUsedDays <= 90 && accessCount >= 1 {
		return ltmLifecycleStatus{State: "warm", Reason: "recently_seen", LastUsedDays: lastUsedDays, AgeDays: ageDays, Score: 45}
	}
	if !scopeValid {
		return ltmLifecycleStatus{State: "stale", Reason: "invalid_scope", LastUsedDays: lastUsedDays, AgeDays: ageDays, Score: 100}
	}
	return ltmLifecycleStatus{State: "stale", Reason: "cold_or_unused", LastUsedDays: lastUsedDays, AgeDays: ageDays, Score: 80}
}

func buildLTMPressureSummary(storagePath string, now time.Time) map[string]any {
	pressure := estimateLTMStoragePressure(storagePath)
	threshold := ltmPressureCleanupThreshold
	state := "normal"
	switch {
	case pressure >= ltmPressureCriticalThreshold:
		state = "critical"
	case pressure >= ltmPressureCleanupThreshold:
		state = "high"
	case pressure >= ltmPressureForecastThreshold:
		state = "moderate"
	}
	if now.IsZero() {
		now = time.Now()
	}
	return map[string]any{
		"timestamp":          now.UnixMilli(),
		"usage_ratio":        pressure,
		"threshold":          threshold,
		"pressure_state":     state,
		"cleanup_triggered":  pressure >= threshold,
		"forecast_triggered": pressure >= ltmPressureForecastThreshold,
	}
}

func decideLTMPressureAction(storagePath string, now time.Time) map[string]any {
	summary := buildLTMPressureSummary(storagePath, now)
	pressure, _ := summary["usage_ratio"].(float64)
	state := strings.TrimSpace(fmt.Sprint(summary["pressure_state"]))
	action := "monitor"
	switch {
	case state == "critical":
		action = "archive_and_compact"
	case state == "high":
		action = "compact"
	case pressure >= ltmPressureForecastThreshold:
		action = "monitor_and_review"
	}
	summary["action"] = action
	summary["triggered"] = summary["forecast_triggered"] == true || summary["cleanup_triggered"] == true
	return summary
}

func buildLTMFreeUpGuidance() []string {
	return []string{
		"Open Data Manager → Long-Term Memory and archive stale repo/category memory.",
		"Use the AI chat memory cleanup flow to retire older repo or semantic entries before the next pressure spike.",
	}
}

func (a *CopilotAgent) notifyLTMPressureUser(summary map[string]any, archivedCount, prunedCount int) {
	if a == nil {
		return
	}
	if summary == nil {
		return
	}
	if a.LTMPressureNotifier == nil {
		a.LTMPressureNotifier = defaultLTMPressureNotifier
	}
	if a.LTMPressureNotifier == nil {
		return
	}
	if a.ltmPressureAlertCooldown <= 0 {
		a.ltmPressureAlertCooldown = 12 * time.Hour
	}
	pressure, _ := summary["usage_ratio"].(float64)
	threshold, _ := summary["threshold"].(float64)
	state := strings.TrimSpace(fmt.Sprint(summary["pressure_state"]))
	if state == "" {
		state = "monitor"
	}
	if threshold <= 0 {
		threshold = 0.85
	}
	severity := "info"
	if state == "critical" {
		severity = "error"
	} else if state == "high" {
		severity = "warning"
	}
	forecastTriggered := summary["forecast_triggered"] == true || summary["triggered"] == true
	cleanupTriggered := summary["cleanup_triggered"] == true || summary["triggered"] == true
	triggered := forecastTriggered || cleanupTriggered
	if !triggered {
		return
	}
	title := "Memory forecast"
	message := fmt.Sprintf("Local memory is approaching its limit at %.2f%% usage. The recommended cleanup threshold is %.2f%%, so a maintenance pass is a good idea soon.", pressure*100, threshold*100)
	if cleanupTriggered {
		title = "Memory cleanup triggered"
		message = fmt.Sprintf("Local memory has reached %.2f%% usage and the daemon has started cleanup. It auto-archived %d entries and pruned %d stale records.", pressure*100, archivedCount, prunedCount)
		if archivedCount == 0 && prunedCount == 0 {
			message = fmt.Sprintf("Local memory has reached %.2f%% usage and the daemon is now cleaning up stale entries to protect local disk space.", pressure*100)
		}
	}
	freeUpTargets := buildLTMFreeUpGuidance()
	message = fmt.Sprintf("%s Two cleanup paths are available: %s %s", message, freeUpTargets[0], freeUpTargets[1])
	now := time.Now()
	a.ltmPressureAlertMu.Lock()
	defer a.ltmPressureAlertMu.Unlock()
	retriggered := !a.lastLTMPressureAlert.IsZero() && now.Sub(a.lastLTMPressureAlert) < a.ltmPressureAlertCooldown
	if retriggered {
		severity = "warning"
		if cleanupTriggered {
			message = fmt.Sprintf("Storage pressure re-triggered after a recent cleanup: %s", message)
		} else {
			message = fmt.Sprintf("Memory pressure forecast re-triggered; a cleanup window is still likely: %s", message)
		}
	}
	notification := LTMPressureNotification{
		Timestamp:     now,
		Severity:      severity,
		Title:         title,
		Message:       message,
		UsageRatio:    pressure,
		PressureState: state,
		Threshold:     threshold,
		Action:        fmt.Sprint(summary["action"]),
		Retriiggered:  retriggered,
		PrunedCount:   prunedCount,
		ArchivedCount: archivedCount,
		FreeUpTargets: freeUpTargets,
		FreeUpContext: "storage_pressure",
	}
	a.lastLTMPressureAlert = now
	a.LTMPressureNotifier(notification)
}

func summarizeLongTailArchivedMemory(payload map[string]any) string {
	if payload == nil || len(payload) == 0 {
		return ""
	}
	parts := make([]string, 0, 4)
	for _, key := range []string{"query", "summary", "outcome", "lesson", "next_step", "description"} {
		if v, ok := payload[key]; ok {
			text := strings.TrimSpace(fmt.Sprint(v))
			if text != "" && len(text) <= 220 {
				parts = append(parts, text)
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	summary := strings.Join(parts, " | ")
	if len(summary) > 220 {
		summary = summary[:217] + "..."
	}
	return summary
}

func shouldSummarizeArchivedMemory(payload map[string]any, now time.Time) bool {
	if payload == nil || len(payload) == 0 || now.IsZero() {
		return false
	}
	status := strings.TrimSpace(fmt.Sprint(payload["lifecycle_status"]))
	if status == "" {
		status = strings.TrimSpace(fmt.Sprint(payload["status"]))
	}
	if !strings.EqualFold(status, "archived") {
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
	if now.UnixMilli()-archivedAt < int64(ltmArchivedSummaryMinAgeDays*24*time.Hour/time.Millisecond) {
		return false
	}
	accessCount := 1
	if v, ok := payload["lifecycle_access_count"].(int); ok {
		accessCount = v
	} else if v, ok := payload["lifecycle_access_count"].(int64); ok {
		accessCount = int(v)
	} else if v, ok := payload["lifecycle_access_count"].(float64); ok {
		accessCount = int(v)
	}
	return accessCount <= 1
}

func shouldArchiveRepoScopedMemoryForPressure(payload map[string]any, now time.Time, storagePressure float64) bool {
	if payload == nil || len(payload) == 0 || now.IsZero() {
		return false
	}
	if !isRepoScopedLTMPayload(payload) {
		return false
	}
	if storagePressure < 0.7 {
		return false
	}
	status := strings.TrimSpace(fmt.Sprint(payload["lifecycle_status"]))
	if status == "" {
		status = strings.TrimSpace(fmt.Sprint(payload["status"]))
	}
	if status == "" || strings.EqualFold(status, "active") || strings.EqualFold(status, "warm") {
		return false
	}
	if strings.EqualFold(status, "archived") || strings.EqualFold(status, "deleted") {
		return false
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
	if storagePressure >= ltmRepoHighPressureThreshold && strings.EqualFold(status, "stale") && (accessCount <= 1 || ageDaysFromTimestamp(now, recordedAt) >= ltmRepoPressureArchiveMinAgeDays) {
		return true
	}
	lastUsedDays := 0
	if lastUsedAt > 0 && now.UnixMilli() > lastUsedAt {
		lastUsedDays = int((now.UnixMilli() - lastUsedAt) / int64(24*time.Hour/time.Millisecond))
	}
	ageDays := 0
	if recordedAt > 0 && now.UnixMilli() > recordedAt {
		ageDays = int((now.UnixMilli() - recordedAt) / int64(24*time.Hour/time.Millisecond))
	}
	return strings.EqualFold(status, "stale") && storagePressure >= ltmRepoNearThresholdPressure && (lastUsedDays >= ltmRepoStaleArchiveDays || ageDays >= ltmRepoStaleArchiveDays+ltmRepoPressureArchiveMinAgeDays || (accessCount <= 1 && ageDays >= ltmRepoPressureArchiveAgeDays))
}

func shouldPruneRepoScopedMemoryByAge(payload map[string]any, now time.Time) bool {
	if payload == nil || len(payload) == 0 || now.IsZero() {
		return false
	}
	if !isRepoScopedLTMPayload(payload) {
		return false
	}
	status := strings.TrimSpace(fmt.Sprint(payload["lifecycle_status"]))
	if status == "" {
		status = strings.TrimSpace(fmt.Sprint(payload["status"]))
	}
	if !strings.EqualFold(status, "archived") {
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
	return ageDaysFromTimestamp(now, archivedAt) >= ltmRepoStalePruneDays
}

func ageDaysFromTimestamp(now time.Time, ts int64) int {
	if now.IsZero() || ts <= 0 {
		return 0
	}
	if now.UnixMilli() <= ts {
		return 0
	}
	return int((now.UnixMilli() - ts) / int64(24*time.Hour/time.Millisecond))
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
	if strings.EqualFold(status, "stale") && (lastUsedDays > ltmColdUnusedLastUsedDays || ageDays > ltmColdUnusedAgeDays || (accessCount <= 1 && ageDays > ltmColdUnusedLastUsedDays)) {
		return ltmLifecycleRetentionDecision{Action: "archive", Reason: "cold_or_unused", Confidence: 80}
	}
	if lastUsedDays > ltmColdUnusedAgeDays && accessCount <= 1 {
		return ltmLifecycleRetentionDecision{Action: "archive", Reason: "unused_for_long_time", Confidence: 75}
	}
	return ltmLifecycleRetentionDecision{}
}

func archiveStaleRepoScopedMemoryForPressure(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], now time.Time, storagePressure float64) (int, error) {
	if ctx == nil || kb == nil || now.IsZero() {
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
		payload, okPayload := any(item.Data).(map[string]any)
		if !okPayload || payload == nil {
			payload = map[string]any{}
		}
		if shouldArchiveRepoScopedMemoryForPressure(payload, now, storagePressure) {
			payload["lifecycle_status"] = "archived"
			payload["status"] = "archived"
			payload["archived_at"] = now.UnixMilli()
			payload["archive_reason"] = "storage_pressure_stale_repo"
			payload["lifecycle_last_evaluated_at"] = now.UnixMilli()
			payload["updated_at"] = now.UnixMilli()
			if summary := summarizeLongTailArchivedMemory(payload); summary != "" {
				payload["archived_summary"] = summary
			}
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
			if summary := summarizeLongTailArchivedMemory(payload); summary != "" {
				payload["archived_summary"] = summary
			}
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
	return decideLTMLifecycleCompactionWithStoragePressure(payload, now, 0)
}

func estimateLTMStoragePressure(storagePath string) float64 {
	if strings.TrimSpace(storagePath) == "" {
		return 0
	}
	if _, err := os.Stat(storagePath); err != nil {
		return 0
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(storagePath, &stat); err != nil {
		return 0
	}
	total := uint64(stat.Blocks) * uint64(stat.Bsize)
	if total == 0 {
		return 0
	}
	available := uint64(stat.Bavail) * uint64(stat.Bsize)
	used := total - available
	if used > total {
		return 1.0
	}
	return float64(used) / float64(total)
}

func decideLTMLifecycleCompactionWithStoragePressure(payload map[string]any, now time.Time, storagePressure float64) bool {
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
	if shouldSummarizeArchivedMemory(payload, now) {
		payload["archived_summary"] = summarizeLongTailArchivedMemory(payload)
	}

	if storagePressure > 0 {
		if storagePressure < 0 {
			storagePressure = 0
		} else if storagePressure > 1 {
			storagePressure = 1
		}
		if storagePressure >= ltmPressureCleanupThreshold {
			return now.UnixMilli()-archivedAt >= int64(ltmPressureCompactionDays*24*time.Hour/time.Millisecond)
		}
	}
	if now.UnixMilli()-archivedAt >= int64(ltmGenericHardPruneDays*24*time.Hour/time.Millisecond) {
		return true
	}
	return now.UnixMilli()-archivedAt >= int64(ltmArchivedCompactionDays*24*time.Hour/time.Millisecond)
}

func (a *CopilotAgent) applyLTMLifecycleCompaction(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], now time.Time) (int, error) {
	return a.applyLTMLifecycleCompactionWithStoragePressure(ctx, kb, now, 0)
}

func (a *CopilotAgent) applyLTMLifecycleCompactionWithStoragePressure(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], now time.Time, storagePressure float64) (int, error) {
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
		if decideLTMLifecycleCompactionWithStoragePressure(payload, now, storagePressure) || shouldPruneRepoScopedMemoryByAge(payload, now) {
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

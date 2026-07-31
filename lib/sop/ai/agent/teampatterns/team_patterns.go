package teampatterns

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai/memory"

	log "log/slog"
)

// TeamPattern captures a team-specific coding convention or pattern.
// Unlike generic best practices (which LLMs already know from training),
// these patterns encode team-specific decisions, helpers, and anti-patterns.
//
// Example: "Use auth.ValidateRequestToken() NOT jwt.Parse()" - team-specific helper
//
// Team Patterns are stored in LTM under "Team Patterns/{category}/{pattern_name}"
// and are semantically searchable during code generation.
type TeamPattern struct {
	// Identity
	DocumentID  string `json:"document_id"`  // UUID for this pattern
	PatternName string `json:"pattern_name"` // "JWT Validation for HTTP Handlers"
	PatternType string `json:"pattern_type"` // "authentication" | "error_handling" | "database" | "testing" | "api_design" | "background_jobs"

	// Pattern Content
	Convention  string   `json:"convention"`   // "Use auth.ValidateRequestToken(r)"
	AntiPattern string   `json:"anti_pattern"` // "DO NOT use jwt.Parse() directly"
	Reasoning   string   `json:"reasoning"`    // "Our helper includes tenant validation + audit logging"
	Example     string   `json:"example"`      // Full working code example
	References  []string `json:"references"`   // ["lib/auth/jwt.go:45", "examples/api/handlers.go:23"]

	// Context
	WhenToUse       string   `json:"when_to_use"`      // "All authenticated HTTP endpoints"
	RelatedPatterns []string `json:"related_patterns"` // ["Error Response Format", "Auth Middleware"]
	Tags            []string `json:"tags"`             // ["security", "http", "authentication"]

	// Scope Metadata
	ScopeType string `json:"scope_type"` // "team" | "project" | "repo"
	ScopeID   string `json:"scope_id"`   // Team/project/repo identifier

	// Lifecycle
	Status     string `json:"lifecycle_status"` // "active" | "deprecated" | "archived"
	CreatedAt  int64  `json:"created_at"`       // Unix milliseconds
	UpdatedAt  int64  `json:"updated_at"`       // Unix milliseconds
	LastUsedAt int64  `json:"last_used_at"`     // Unix milliseconds
	UsageCount int    `json:"usage_count"`      // Number of times pattern was used

	// Vector Embedding (for deduplication)
	EmbeddingHash string `json:"embedding_hash"` // Hash of embedder + content
}

// ToMap converts TeamPattern to map[string]any for LTM storage.
// This matches the pattern used by CodingMemory in copilot.go.
func (tp *TeamPattern) ToMap() map[string]any {
	return map[string]any{
		"document_id":      tp.DocumentID,
		"payload_type":     "team_pattern",
		"pattern_name":     tp.PatternName,
		"pattern_type":     tp.PatternType,
		"convention":       tp.Convention,
		"anti_pattern":     tp.AntiPattern,
		"reasoning":        tp.Reasoning,
		"example":          tp.Example,
		"references":       tp.References,
		"when_to_use":      tp.WhenToUse,
		"related_patterns": tp.RelatedPatterns,
		"tags":             tp.Tags,
		"scope_type":       tp.ScopeType,
		"scope_id":         tp.ScopeID,
		"lifecycle_status": tp.Status,
		"created_at":       tp.CreatedAt,
		"updated_at":       tp.UpdatedAt,
		"last_used_at":     tp.LastUsedAt,
		"usage_count":      tp.UsageCount,
		"embedding_hash":   tp.EmbeddingHash,
	}
}

// FromMap converts map[string]any from LTM storage to TeamPattern.
func FromMap(data map[string]any) *TeamPattern {
	tp := &TeamPattern{}

	if v, ok := data["document_id"].(string); ok {
		tp.DocumentID = v
	}
	if v, ok := data["pattern_name"].(string); ok {
		tp.PatternName = v
	}
	if v, ok := data["pattern_type"].(string); ok {
		tp.PatternType = v
	}
	if v, ok := data["convention"].(string); ok {
		tp.Convention = v
	}
	if v, ok := data["anti_pattern"].(string); ok {
		tp.AntiPattern = v
	}
	if v, ok := data["reasoning"].(string); ok {
		tp.Reasoning = v
	}
	if v, ok := data["example"].(string); ok {
		tp.Example = v
	}
	if v, ok := data["references"].([]any); ok {
		tp.References = make([]string, len(v))
		for i, ref := range v {
			if s, ok := ref.(string); ok {
				tp.References[i] = s
			}
		}
	} else if v, ok := data["references"].([]string); ok {
		tp.References = v
	}
	if v, ok := data["when_to_use"].(string); ok {
		tp.WhenToUse = v
	}
	if v, ok := data["related_patterns"].([]any); ok {
		tp.RelatedPatterns = make([]string, len(v))
		for i, rel := range v {
			if s, ok := rel.(string); ok {
				tp.RelatedPatterns[i] = s
			}
		}
	} else if v, ok := data["related_patterns"].([]string); ok {
		tp.RelatedPatterns = v
	}
	if v, ok := data["tags"].([]any); ok {
		tp.Tags = make([]string, len(v))
		for i, tag := range v {
			if s, ok := tag.(string); ok {
				tp.Tags[i] = s
			}
		}
	} else if v, ok := data["tags"].([]string); ok {
		tp.Tags = v
	}
	if v, ok := data["scope_type"].(string); ok {
		tp.ScopeType = v
	}
	if v, ok := data["scope_id"].(string); ok {
		tp.ScopeID = v
	}
	if v, ok := data["lifecycle_status"].(string); ok {
		tp.Status = v
	}
	if v, ok := data["created_at"].(float64); ok {
		tp.CreatedAt = int64(v)
	}
	if v, ok := data["updated_at"].(float64); ok {
		tp.UpdatedAt = int64(v)
	}
	if v, ok := data["last_used_at"].(float64); ok {
		tp.LastUsedAt = int64(v)
	}
	if v, ok := data["usage_count"].(float64); ok {
		tp.UsageCount = int(v)
	} else if v, ok := data["usage_count"].(int); ok {
		tp.UsageCount = v
	}
	if v, ok := data["embedding_hash"].(string); ok {
		tp.EmbeddingHash = v
	}

	return tp
}

// BuildCategoryPath constructs the hierarchical category path for this pattern.
// Pattern: "Team Patterns/{PatternType}/{PatternName}"
// Examples:
//   - "Team Patterns/Authentication/JWT Validation"
//   - "Team Patterns/Error Handling/Error Wrapper Pattern"
//   - "Team Patterns/Database/Query Builder Usage"
func (tp *TeamPattern) BuildCategoryPath() string {
	parts := []string{"Team Patterns"}

	// Add pattern type as first level
	if tp.PatternType != "" {
		parts = append(parts, strings.Title(strings.ReplaceAll(tp.PatternType, "_", " ")))
	}

	// Pattern name becomes the leaf category
	if tp.PatternName != "" {
		parts = append(parts, tp.PatternName)
	}

	return strings.Join(parts, "/")
}

// BuildSummaryText constructs the text used for semantic embedding.
// This is what gets vectorized for semantic search.
func (tp *TeamPattern) BuildSummaryText() string {
	var parts []string

	if tp.PatternName != "" {
		parts = append(parts, fmt.Sprintf("Pattern: %s", tp.PatternName))
	}
	if tp.PatternType != "" {
		parts = append(parts, fmt.Sprintf("Type: %s", tp.PatternType))
	}
	if tp.Convention != "" {
		parts = append(parts, fmt.Sprintf("Convention: %s", tp.Convention))
	}
	if tp.AntiPattern != "" {
		parts = append(parts, fmt.Sprintf("Anti-pattern: %s", tp.AntiPattern))
	}
	if tp.WhenToUse != "" {
		parts = append(parts, fmt.Sprintf("Use when: %s", tp.WhenToUse))
	}
	if len(tp.Tags) > 0 {
		parts = append(parts, fmt.Sprintf("Tags: %s", strings.Join(tp.Tags, ", ")))
	}

	return strings.Join(parts, "\n")
}

// StoreTeamPattern saves a team pattern to Long-Term Memory.
// The pattern is stored in the category path "Team Patterns/{type}/{name}".
//
// IMPORTANT: This function only STORES the pattern. The categories are NOT yet vectorized.
// After storing patterns, call VectorizeTeamPatterns() to compute semantic vectors for search.
// IndexPopulationModeFull ensures the category tree is built, but vectors are computed lazily.
func StoreTeamPattern(ctx context.Context, ltm *memory.KnowledgeBase[map[string]any], pattern *TeamPattern) error {
	// Generate document ID if not set
	if pattern.DocumentID == "" {
		pattern.DocumentID = sop.NewUUID().String()
	}

	// Set timestamps
	now := time.Now().UnixMilli()
	if pattern.CreatedAt == 0 {
		pattern.CreatedAt = now
	}
	pattern.UpdatedAt = now

	// Set default status if not set
	if pattern.Status == "" {
		pattern.Status = "active"
	}

	// Build category path
	categoryPath := pattern.BuildCategoryPath()

	// Build summary text for embedding
	summaryText := pattern.BuildSummaryText()

	// Create thought for LTM storage
	thought := memory.Thought[map[string]any]{
		CategoryPath:        categoryPath,
		Summaries:           []string{summaryText},
		DocID:               memory.DocIDs{pattern.DocumentID},
		Data:                pattern.ToMap(),
		IndexPopulationMode: memory.IndexPopulationModeFull,
	}

	// Ingest into LTM (batch of 1)
	if err := ltm.IngestThoughts(ctx, []memory.Thought[map[string]any]{thought}, "system"); err != nil {
		return fmt.Errorf("failed to store team pattern: %w", err)
	}

	return nil
}

// SearchTeamPatterns searches for team patterns in Long-Term Memory.
// It performs semantic search within the "Team Patterns" category.
//
// Args:
//   - ctx: Context
//   - ltm: Long-Term Memory knowledge base
//   - query: Semantic search query (e.g., "authentication HTTP handlers")
//   - patternType: Optional filter by pattern type (e.g., "authentication")
//   - limit: Maximum number of results
//
// Returns matching patterns ordered by relevance.
func SearchTeamPatterns(ctx context.Context, ltm *memory.KnowledgeBase[map[string]any], query string, patternType string, limit int) ([]*TeamPattern, error) {
	// Build category path for search scope
	categoryPath := "Team Patterns"
	if patternType != "" {
		categoryPath = categoryPath + "/" + strings.Title(strings.ReplaceAll(patternType, "_", " "))
	}

	// Perform semantic search
	searchResults, err := ltm.Search(ctx, []memory.SearchRequest[map[string]any]{
		{
			Text:          query,
			CategoryPaths: []string{categoryPath},
			Limit:         limit,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("team pattern search failed: %w", err)
	}

	// Convert results to TeamPattern structs
	// Search returns [][]ai.Hit[T] - we get first result set
	patterns := make([]*TeamPattern, 0)
	if len(searchResults) > 0 {
		for _, hit := range searchResults[0] {
			pattern := FromMap(hit.Payload)
			patterns = append(patterns, pattern)
		}
	}

	return patterns, nil
}

// UpdatePatternUsage increments the usage count and updates last_used_at timestamp.
// This should be called whenever a pattern is successfully used in code generation.
func UpdatePatternUsage(ctx context.Context, ltm *memory.KnowledgeBase[map[string]any], documentID string) error {
	// Retrieve the pattern by searching with document ID
	searchResults, err := ltm.Search(ctx, []memory.SearchRequest[map[string]any]{
		{
			Text:          documentID,
			CategoryPaths: []string{"Team Patterns"},
			Limit:         1,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to find pattern: %w", err)
	}

	if len(searchResults) == 0 || len(searchResults[0]) == 0 {
		return fmt.Errorf("pattern not found: %s", documentID)
	}

	// Update usage metadata
	pattern := FromMap(searchResults[0][0].Payload)
	pattern.UsageCount++
	pattern.LastUsedAt = time.Now().UnixMilli()
	pattern.UpdatedAt = pattern.LastUsedAt

	// Store updated pattern
	return StoreTeamPattern(ctx, ltm, pattern)
}

// HasTeamPatterns checks if any team patterns exist in LTM.
// This is used for conditional context injection - only mention team patterns
// in the system prompt if the user has actually defined some.
func HasTeamPatterns(ctx context.Context, ltm *memory.KnowledgeBase[map[string]any]) (bool, error) {
	// Search for any items under "Team Patterns" category
	searchResults, err := ltm.Search(ctx, []memory.SearchRequest[map[string]any]{
		{
			Text:          "",
			CategoryPaths: []string{"Team Patterns"},
			Limit:         1,
		},
	})
	if err != nil {
		// If category doesn't exist, return false (not an error)
		if strings.Contains(err.Error(), "category not found") || strings.Contains(err.Error(), "no categories") {
			return false, nil
		}
		return false, fmt.Errorf("failed to check team patterns: %w", err)
	}

	return len(searchResults) > 0 && len(searchResults[0]) > 0, nil
}

// ListAllPatterns retrieves all team patterns, optionally filtered by status.
// Useful for management UI and analytics.
func ListAllPatterns(ctx context.Context, ltm *memory.KnowledgeBase[map[string]any], status string, limit int) ([]*TeamPattern, error) {
	// Search all team patterns
	searchResults, err := ltm.Search(ctx, []memory.SearchRequest[map[string]any]{
		{
			Text:          "",
			CategoryPaths: []string{"Team Patterns"},
			Limit:         limit,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list team patterns: %w", err)
	}

	// Convert and filter by status if specified
	patterns := make([]*TeamPattern, 0)
	if len(searchResults) > 0 {
		for _, hit := range searchResults[0] {
			pattern := FromMap(hit.Payload)

			// Filter by status if specified
			if status != "" && pattern.Status != status {
				continue
			}

			patterns = append(patterns, pattern)
		}
	}

	return patterns, nil
}

// DeletePattern removes a team pattern from LTM.
// TODO: Implement deletion - requires CategoryID + ItemID which aren't exposed in Hit
func DeletePattern(ctx context.Context, ltm *memory.KnowledgeBase[map[string]any], documentID string) error {
	return fmt.Errorf("DeletePattern not yet implemented")
}

// VectorizeTeamPatterns computes semantic vectors for all Team Patterns categories.
// This enables fast semantic search across patterns.
//
// WHEN TO CALL:
// - After storing one or more patterns via StoreTeamPattern()
// - When patterns have been imported from external sources
// - Periodically to ensure new categories are vectorized
//
// COST: This operation calls the embedder for each category under "Team Patterns".
// FormatPatternForDisplay formats a team pattern for LLM consumption.
// This is used by the investigation_search_team_patterns tool.
func FormatPatternForDisplay(pattern *TeamPattern, index int) string {
	var output strings.Builder

	output.WriteString(fmt.Sprintf("## Pattern %d: %s\n\n", index, pattern.PatternName))

	if pattern.Convention != "" {
		output.WriteString(fmt.Sprintf("**Convention**: %s\n\n", pattern.Convention))
	}

	if pattern.AntiPattern != "" {
		output.WriteString(fmt.Sprintf("**Anti-Pattern**: %s\n\n", pattern.AntiPattern))
	}

	if pattern.Reasoning != "" {
		output.WriteString(fmt.Sprintf("**Reasoning**: %s\n\n", pattern.Reasoning))
	}

	if pattern.Example != "" {
		output.WriteString("**Example**:\n```\n")
		output.WriteString(pattern.Example)
		output.WriteString("\n```\n\n")
	}

	if pattern.WhenToUse != "" {
		output.WriteString(fmt.Sprintf("**When to Use**: %s\n\n", pattern.WhenToUse))
	}

	if len(pattern.References) > 0 {
		output.WriteString(fmt.Sprintf("**References**: %s\n\n", strings.Join(pattern.References, ", ")))
	}

	if len(pattern.RelatedPatterns) > 0 {
		output.WriteString(fmt.Sprintf("**Related Patterns**: %s\n\n", strings.Join(pattern.RelatedPatterns, ", ")))
	}

	if len(pattern.Tags) > 0 {
		output.WriteString(fmt.Sprintf("**Tags**: %s\n\n", strings.Join(pattern.Tags, ", ")))
	}

	output.WriteString("---\n\n")

	return output.String()
}

// VectorizeTeamPatterns triggers category-specific vectorization for ONLY the "Team Patterns" category tree.
// This is more efficient than vectorizing the entire LTM space.
//
// The vectorization process:
// 1. Finds the "Team Patterns" category ID by path lookup
// 2. Calls database.VectorizeCategories() with ONLY that category ID
// 3. Computes embeddings for all pattern summaries and subcategory center vectors
// 4. Rebuilds the semantic distance index for Team Patterns only
//
// WHEN TO CALL:
// - After adding/updating multiple patterns (batch operation)
// - Before the first semantic search if patterns were added without vectorization
// - When embedder model changes and re-vectorization is needed
//
// Performance: ~1-2 seconds for 20 patterns (vs ~45s for full space vectorization)
//
// NOTE: This function requires:
// - db: *database.Database instance (concrete type, not interface)
// - kbName: LTM knowledge base name (e.g., "agent123_ltm")
// - llm: ai.Generator for any LLM-based processing
// - embedder: ai.Embeddings for computing vectors
func VectorizeTeamPatterns(ctx context.Context, db any, kbName string, llm any, embedder any) error {
	if db == nil || kbName == "" || embedder == nil {
		return fmt.Errorf("missing required parameters for vectorization")
	}

	// Type assert to concrete types (avoiding import cycle)
	// Caller must pass *database.Database, ai.Generator, ai.Embeddings
	// We use 'any' in signature to avoid circular dependency

	// Find "Team Patterns" category ID
	categoryID, err := findTeamPatternsCategoryID(ctx, db, kbName)
	if err != nil {
		return fmt.Errorf("failed to find Team Patterns category: %w", err)
	}
	if categoryID.IsNil() {
		return fmt.Errorf("Team Patterns category not found in LTM")
	}

	// Call database.VectorizeCategories with ONLY Team Patterns category
	// This vectorizes the "Team Patterns" subtree only, not the entire LTM
	type vectorizer interface {
		VectorizeCategories(ctx context.Context, kbName string, llm any, embedder any, batchSize int, categoryIDs []sop.UUID) error
	}

	vectorDB, ok := db.(vectorizer)
	if !ok {
		return fmt.Errorf("database does not support VectorizeCategories")
	}

	if err := vectorDB.VectorizeCategories(ctx, kbName, llm, embedder, 50, []sop.UUID{categoryID}); err != nil {
		return fmt.Errorf("failed to vectorize team patterns: %w", err)
	}

	log.Info("Team patterns vectorization complete", "kb", kbName, "categoryID", categoryID)
	return nil
}

// findTeamPatternsCategoryID finds the UUID of the "Team Patterns" root category.
func findTeamPatternsCategoryID(ctx context.Context, db any, kbName string) (sop.UUID, error) {
	type transactionStarter interface {
		BeginTransaction(ctx context.Context, mode sop.TransactionMode, maxTime ...time.Duration) (sop.Transaction, error)
		OpenKnowledgeBase(ctx context.Context, name string, tx sop.Transaction, llm any, embedder any, documentMode bool, enableTextSearch ...bool) (*memory.KnowledgeBase[map[string]any], error)
	}

	txDB, ok := db.(transactionStarter)
	if !ok {
		return sop.NilUUID, fmt.Errorf("database does not support transactions")
	}

	tx, err := txDB.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		return sop.NilUUID, err
	}
	defer tx.Rollback(ctx)

	kb, err := txDB.OpenKnowledgeBase(ctx, kbName, tx, nil, nil, false)
	if err != nil {
		return sop.NilUUID, err
	}

	catsByPath, err := kb.Store.CategoriesByPath(ctx)
	if err != nil {
		return sop.NilUUID, err
	}

	found, err := catsByPath.Find(ctx, "Team Patterns", false)
	if err != nil || !found {
		return sop.NilUUID, fmt.Errorf("Team Patterns category not found")
	}

	return catsByPath.GetCurrentValue(ctx)
}

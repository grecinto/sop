package agent

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
)

// ResearchSourceCandidate represents a potential research source with its relevance score.
type ResearchSourceCandidate struct {
	Reference ai.ArtifactReference
	Score     float64
	Reason    string
}

// ResolveResearchSources infers appropriate knowledge sources from task context.
// Priority order:
// 1. Explicit user selection (SelectedKBs) - highest priority, always preserved
// 2. Auto-inferred from language/file context
// 3. Synced knowledge artifacts with matching metadata
// 4. LTM fallback (implicit, no explicit source needed)
func (a *CopilotAgent) ResolveResearchSources(ctx context.Context, query string, taskCtx TaskContextClassification) []ai.ArtifactReference {
	payload := ai.GetSessionPayload(ctx)
	if payload == nil {
		return nil
	}

	// Priority 1: Explicit user selection always takes precedence
	if len(payload.SelectedKBs) > 0 {
		return payload.SelectedKBs
	}

	// Priority 2-3: Auto-detect from context
	candidates := a.collectResearchSourceCandidates(ctx, query, taskCtx, payload)
	if len(candidates) == 0 {
		return nil
	}

	// Score and rank candidates
	scored := a.scoreResearchSourceCandidates(candidates, query, taskCtx, payload)
	if len(scored) == 0 {
		return nil
	}

	// Return top candidates (up to 2 to avoid overwhelming the prompt)
	maxSources := 2
	if len(scored) < maxSources {
		maxSources = len(scored)
	}

	resolved := make([]ai.ArtifactReference, 0, maxSources)
	for i := 0; i < maxSources; i++ {
		if scored[i].Score > 0.3 { // Minimum confidence threshold
			resolved = append(resolved, scored[i].Reference)
		}
	}

	return resolved
}

// collectResearchSourceCandidates gathers potential sources from various signals.
func (a *CopilotAgent) collectResearchSourceCandidates(ctx context.Context, query string, taskCtx TaskContextClassification, payload *ai.SessionPayload) []ResearchSourceCandidate {
	candidates := make([]ResearchSourceCandidate, 0)

	// Detect language from active repo/files
	detectedLanguage := a.detectLanguageFromContext(ctx, query, payload)
	if detectedLanguage != "" {
		// Look for language-specific knowledge bases
		langCandidates := a.findLanguageSpecificSources(ctx, detectedLanguage)
		candidates = append(candidates, langCandidates...)
	}

	// Check for synced knowledge artifacts with matching metadata
	syncedCandidates := a.findSyncedArtifactSources(ctx, query, taskCtx, detectedLanguage)
	candidates = append(candidates, syncedCandidates...)

	// Check for domain-specific sources from task classification
	if taskCtx.Domain != "" {
		domainCandidates := a.findDomainSpecificSources(ctx, taskCtx.Domain)
		candidates = append(candidates, domainCandidates...)
	}

	return candidates
}

// detectLanguageFromContext infers the programming language from context signals.
func (a *CopilotAgent) detectLanguageFromContext(ctx context.Context, query string, payload *ai.SessionPayload) string {
	if payload == nil {
		return ""
	}

	// Check active repo for language indicators
	if payload.ActiveRepo != "" {
		// Common patterns: "user/repo-name" or "/path/to/repo"
		repoLower := strings.ToLower(payload.ActiveRepo)

		// Check for language-specific patterns in repo name/path
		languagePatterns := map[string][]string{
			"Go":         {"go-", "-go", "golang", "/go/", "_go", "go_"},
			"Python":     {"python-", "-python", "python", "/py/", ".py", "django", "flask", "_python"},
			"TypeScript": {"typescript-", "-typescript", "typescript", "/ts/", "react", "next", "vue", "_typescript"},
			"JavaScript": {"javascript-", "-javascript", "javascript", "/js/", "node", "_javascript", "_js"},
			"Java":       {"java-", "-java", "/java/", "spring", "maven", "gradle", "_java"},
			"Rust":       {"rust-", "-rust", "rust", "/rs/", "cargo", "_rust"},
			"Ruby":       {"ruby-", "-ruby", "ruby", "/rb/", "rails", "_ruby"},
			"CSharp":     {"csharp-", "-csharp", "csharp", "dotnet", ".net", "/cs/", "_csharp"},
		}

		for lang, patterns := range languagePatterns {
			for _, pattern := range patterns {
				if strings.Contains(repoLower, pattern) {
					return lang
				}
			}
		}
	}

	// Fallback: check query for language mentions
	queryLower := strings.ToLower(query)
	languageKeywords := map[string][]string{
		"Go":         {"go", "golang", "goroutine"},
		"Python":     {"python", "py", "django", "flask", "pandas"},
		"TypeScript": {"typescript", "ts", "react", "next.js"},
		"JavaScript": {"javascript", "js", "node", "npm"},
		"Java":       {"java", "spring", "maven"},
		"Rust":       {"rust", "cargo"},
		"Ruby":       {"ruby", "rails"},
		"CSharp":     {"c#", "csharp", ".net", "dotnet"},
	}

	for lang, keywords := range languageKeywords {
		for _, keyword := range keywords {
			if strings.Contains(queryLower, keyword) {
				return lang
			}
		}
	}

	return ""
}

// findLanguageSpecificSources searches for knowledge bases matching the detected language.
func (a *CopilotAgent) findLanguageSpecificSources(ctx context.Context, language string) []ResearchSourceCandidate {
	if a.systemDB == nil {
		return nil
	}

	candidates := make([]ResearchSourceCandidate, 0)

	// Common naming patterns for language-specific knowledge bases
	possibleNames := []string{
		strings.ToLower(language) + "-best-practices",
		strings.ToLower(language) + "-patterns",
		strings.ToLower(language) + "-kb",
		language + "_KB",
		language,
	}

	tx, err := a.systemDB.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		return candidates
	}
	defer tx.Rollback(ctx)

	// Check which knowledge bases actually exist
	stores, err := tx.GetStores(ctx)
	if err != nil {
		return candidates
	}

	storeSet := make(map[string]bool)
	for _, store := range stores {
		// Check for both Vector Store pattern (/sys_config) and Knowledge Base pattern (/categories)
		if strings.HasSuffix(store, "/sys_config") {
			name := strings.TrimSuffix(store, "/sys_config")
			storeSet[strings.ToLower(name)] = true
		} else if strings.HasSuffix(store, "/categories") {
			name := strings.TrimSuffix(store, "/categories")
			storeSet[strings.ToLower(name)] = true
		}
	}

	for _, name := range possibleNames {
		if storeSet[strings.ToLower(name)] {
			candidates = append(candidates, ResearchSourceCandidate{
				Reference: ai.ArtifactReference{
					Name:         name,
					Type:         ai.ArtifactTypeSpace,
					DatabaseName: "system",
				},
				Score:  0.8, // High score for language match
				Reason: "language-specific knowledge base",
			})
		}
	}

	return candidates
}

// findSyncedArtifactSources searches for synced knowledge artifacts with matching metadata.
func (a *CopilotAgent) findSyncedArtifactSources(ctx context.Context, query string, taskCtx TaskContextClassification, language string) []ResearchSourceCandidate {
	// TODO: When synced artifacts have rich metadata (tags, language, domain),
	// query the shared_memory or artifact-specific KBs for matching entries.
	// For now, return empty to focus on language-based detection.
	return nil
}

// findDomainSpecificSources searches for sources matching the task domain.
func (a *CopilotAgent) findDomainSpecificSources(ctx context.Context, domain string) []ResearchSourceCandidate {
	// For now, domain-specific sources are handled by language detection.
	// Future enhancement: map domains like "Stores", "Spaces" to domain-specific KBs.
	return nil
}

// scoreResearchSourceCandidates ranks candidates by relevance.
func (a *CopilotAgent) scoreResearchSourceCandidates(candidates []ResearchSourceCandidate, query string, taskCtx TaskContextClassification, payload *ai.SessionPayload) []ResearchSourceCandidate {
	if len(candidates) == 0 {
		return candidates
	}

	// Boost scores based on additional context signals
	queryLower := strings.ToLower(query)
	for i := range candidates {
		refNameLower := strings.ToLower(candidates[i].Reference.Name)

		// Boost if KB name appears in query
		if strings.Contains(queryLower, refNameLower) {
			candidates[i].Score += 0.2
		}

		// Boost if it's a recent successful source (check session memory)
		// TODO: Track successful source usage in session memory
	}

	// Sort by score (descending)
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].Score > candidates[i].Score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	return candidates
}

// detectFileLanguage infers language from file extension.
func detectFileLanguage(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	extensionMap := map[string]string{
		".go":   "Go",
		".py":   "Python",
		".ts":   "TypeScript",
		".tsx":  "TypeScript",
		".js":   "JavaScript",
		".jsx":  "JavaScript",
		".java": "Java",
		".rs":   "Rust",
		".rb":   "Ruby",
		".cs":   "CSharp",
	}
	if lang, ok := extensionMap[ext]; ok {
		return lang
	}
	return ""
}

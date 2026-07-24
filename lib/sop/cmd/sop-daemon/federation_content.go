package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/sharedcode/sop/ai/agent"
)

// FederatedContentCategory defines the types of knowledge worth sharing across the fleet.
type FederatedContentCategory string

const (
	// CategoryWorkflowPattern captures proven sequences that solve common problems.
	CategoryWorkflowPattern FederatedContentCategory = "Fleet Learning/workflow_pattern"

	// CategoryAntiPattern captures mistakes to avoid that appeared across nodes.
	CategoryAntiPattern FederatedContentCategory = "Fleet Learning/anti_pattern"

	// CategoryRepairStrategy captures successful fixes for auth, config, or runtime issues.
	CategoryRepairStrategy FederatedContentCategory = "Fleet Learning/repair_strategy"

	// CategoryToolPreference captures which tool sequences work best for which tasks.
	CategoryToolPreference FederatedContentCategory = "Fleet Learning/tool_preference"

	// CategorySafetyHeuristic captures when to verify, confirm, or slow down.
	CategorySafetyHeuristic FederatedContentCategory = "Fleet Learning/safety_heuristic"

	// CategoryVerifiedPlaybook captures multi-step sequences with proven success.
	CategoryVerifiedPlaybook FederatedContentCategory = "Fleet Learning/verified_playbook"

	// CategoryContextGathering captures effective strategies for understanding codebases.
	CategoryContextGathering FederatedContentCategory = "Fleet Learning/context_gathering"

	// CategoryErrorResolution captures patterns for diagnosing and fixing common errors.
	CategoryErrorResolution FederatedContentCategory = "Fleet Learning/error_resolution"

	// CategoryPerformanceOptimization captures patterns for improving performance.
	CategoryPerformanceOptimization FederatedContentCategory = "Fleet Learning/performance_optimization"

	// CategorySecurityPattern captures security best practices and vulnerability prevention.
	CategorySecurityPattern FederatedContentCategory = "Fleet Learning/security_pattern"

	// CategoryTestingStrategy captures effective testing approaches and patterns.
	CategoryTestingStrategy FederatedContentCategory = "Fleet Learning/testing_strategy"

	// CategoryCodeQuality captures maintainability and readability patterns.
	CategoryCodeQuality FederatedContentCategory = "Fleet Learning/code_quality"

	// CategoryAPIDesign captures API design conventions and best practices.
	CategoryAPIDesign FederatedContentCategory = "Fleet Learning/api_design"
)

// PromotionConfig defines thresholds for what gets promoted to HQ.
type PromotionConfig struct {
	MinConfidence  float64 // Minimum confidence score (0.0-1.0)
	MinRecurrence  int     // Minimum times pattern should appear before promotion
	MaxArtifactLen int     // Maximum text length for an artifact
}

var defaultPromotionConfig = PromotionConfig{
	MinConfidence:  0.7,
	MinRecurrence:  1, // V1: promote immediately, filter at HQ later
	MaxArtifactLen: 2000,
}

// FederatedArtifact represents a piece of knowledge worth sharing across daemons.
type FederatedArtifact struct {
	Category    FederatedContentCategory
	Summary     string
	Pattern     string
	Context     string
	Confidence  float64
	Metadata    map[string]any
	ShouldShare bool
}

// PromoteFederatedArtifact stores a structured artifact in the shared memory system.
func PromoteFederatedArtifact(ctx context.Context, artifact FederatedArtifact) error {
	ctx = normalizeContext(ctx)

	if !artifact.ShouldShare {
		return nil
	}

	if artifact.Confidence < defaultPromotionConfig.MinConfidence {
		return nil // Below confidence threshold
	}

	db, err := sharedMemoryDatabase(ctx)
	if err != nil {
		return err
	}

	// Build compact, privacy-safe text representation
	text := buildArtifactText(artifact)
	if len(text) > defaultPromotionConfig.MaxArtifactLen {
		text = text[:defaultPromotionConfig.MaxArtifactLen]
	}

	if strings.TrimSpace(text) == "" {
		return nil
	}

	// Enrich metadata
	metadata := artifact.Metadata
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata["category"] = string(artifact.Category)
	metadata["confidence"] = artifact.Confidence
	metadata["promoted_at"] = time.Now().UTC().Format(time.RFC3339)
	metadata["version"] = "v1"

	// Store in local shared memory
	err = agent.PromoteSharedMemoryArtifact(
		ctx,
		db,
		nil, // LLM not required
		nil, // Embedder handled by PromoteSharedMemoryArtifact
		text,
		"shared_memory",
		string(artifact.Category),
		"daemon",
		"local-daemon",
		metadata,
	)
	if err != nil {
		return err
	}

	// Also enqueue for sync to HQ
	syncQueue := GetGlobalSyncQueue()
	if syncQueue != nil {
		if err := syncQueue.Enqueue(text, string(artifact.Category), metadata); err != nil {
			log.Printf("Failed to enqueue artifact for HQ sync: %v", err)
			// Don't fail promotion if enqueue fails
		}
	}

	return nil
}

// buildArtifactText creates a compact text representation of the artifact.
func buildArtifactText(artifact FederatedArtifact) string {
	var parts []string

	if artifact.Summary != "" {
		parts = append(parts, fmt.Sprintf("Summary: %s", artifact.Summary))
	}

	if artifact.Pattern != "" {
		parts = append(parts, fmt.Sprintf("Pattern: %s", artifact.Pattern))
	}

	if artifact.Context != "" {
		parts = append(parts, fmt.Sprintf("Context: %s", artifact.Context))
	}

	return strings.Join(parts, "\n")
}

// selectRelevantCategories maps a user query to the most relevant federated categories.
func selectRelevantCategories(query string) []FederatedContentCategory {
	queryLower := strings.ToLower(strings.TrimSpace(query))
	if queryLower == "" {
		return nil
	}

	switch {
	case strings.Contains(queryLower, "security"), strings.Contains(queryLower, "auth"), strings.Contains(queryLower, "secret"), strings.Contains(queryLower, "token"), strings.Contains(queryLower, "permission"), strings.Contains(queryLower, "vulner"):
		return []FederatedContentCategory{CategorySecurityPattern, CategorySafetyHeuristic}
	case strings.Contains(queryLower, "slow"), strings.Contains(queryLower, "performance"), strings.Contains(queryLower, "latency"), strings.Contains(queryLower, "cache"), strings.Contains(queryLower, "optimiz"):
		return []FederatedContentCategory{CategoryPerformanceOptimization, CategoryToolPreference}
	case strings.Contains(queryLower, "test"), strings.Contains(queryLower, "coverage"):
		return []FederatedContentCategory{CategoryTestingStrategy, CategoryWorkflowPattern}
	case strings.Contains(queryLower, "error"), strings.Contains(queryLower, "fix"), strings.Contains(queryLower, "bug"), strings.Contains(queryLower, "failing"):
		return []FederatedContentCategory{CategoryErrorResolution, CategoryRepairStrategy, CategoryAntiPattern}
	case strings.Contains(queryLower, "refactor"), strings.Contains(queryLower, "improve"), strings.Contains(queryLower, "clean"):
		return []FederatedContentCategory{CategoryWorkflowPattern, CategoryCodeQuality, CategorySafetyHeuristic}
	case strings.Contains(queryLower, "api"), strings.Contains(queryLower, "endpoint"), strings.Contains(queryLower, "route"), strings.Contains(queryLower, "rest"):
		return []FederatedContentCategory{CategoryAPIDesign, CategoryWorkflowPattern}
	case strings.Contains(queryLower, "implement"), strings.Contains(queryLower, "create"), strings.Contains(queryLower, "add"), strings.Contains(queryLower, "feature"):
		return []FederatedContentCategory{CategoryWorkflowPattern, CategoryContextGathering}
	default:
		return []FederatedContentCategory{CategoryWorkflowPattern}
	}
}

// categorizeFederationArtifact classifies a successful interaction into a federated category.
func categorizeFederationArtifact(query, answer string) (FederatedContentCategory, float64, bool) {
	queryLower := strings.ToLower(strings.TrimSpace(query))
	answerLower := strings.ToLower(strings.TrimSpace(answer))
	if queryLower == "" || answerLower == "" {
		return CategoryWorkflowPattern, 0.65, false
	}

	switch {
	case strings.Contains(queryLower, "error") || strings.Contains(queryLower, "fix") || strings.Contains(queryLower, "bug"):
		if strings.Contains(answerLower, "fixed") || strings.Contains(answerLower, "resolved") || strings.Contains(answerLower, "corrected") {
			return CategoryErrorResolution, 0.85, true
		}
	case strings.Contains(queryLower, "implement") || strings.Contains(queryLower, "create") || strings.Contains(queryLower, "add feature"):
		if len(answer) > 200 {
			return CategoryWorkflowPattern, 0.75, true
		}
	case strings.Contains(queryLower, "test"):
		return CategoryTestingStrategy, 0.80, true
	case strings.Contains(queryLower, "refactor") || strings.Contains(queryLower, "improve"):
		return CategoryWorkflowPattern, 0.78, true
	case strings.Contains(answerLower, "error") || strings.Contains(answerLower, "failed") || strings.Contains(answerLower, "cannot"):
		return CategoryAntiPattern, 0.70, true
	}

	if len(query) > 20 && len(answer) > 50 {
		return CategoryWorkflowPattern, 0.65, true
	}

	return CategoryWorkflowPattern, 0.65, false
}

// shouldPromoteFederationArtifact decides whether a candidate artifact is valuable enough to share.
func shouldPromoteFederationArtifact(query, answer string, category FederatedContentCategory, confidence float64) bool {
	if strings.TrimSpace(query) == "" || strings.TrimSpace(answer) == "" {
		return false
	}
	if confidence < defaultPromotionConfig.MinConfidence {
		return false
	}
	if len(answer) < 20 {
		return false
	}
	if len(query) < 4 && len(answer) < 50 {
		return false
	}
	if strings.Contains(strings.ToLower(answer), "i don't know") || strings.Contains(strings.ToLower(answer), "not sure") {
		return false
	}
	_ = category
	return true
}

// SeedInitialFederatedContent creates high-value initial patterns for the fleet.
// This should be called on daemon startup to ensure baseline intelligence.
func SeedInitialFederatedContent(ctx context.Context) error {
	seeds := []FederatedArtifact{
		// Workflow Patterns
		{
			Category:    CategoryWorkflowPattern,
			Summary:     "Read before edit pattern",
			Pattern:     "Before editing files, read current content to understand context and avoid introducing conflicts",
			Context:     "Applies to all file modifications",
			Confidence:  0.95,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "high",
			},
		},
		{
			Category:    CategoryWorkflowPattern,
			Summary:     "Test after change pattern",
			Pattern:     "After code changes, run relevant tests to verify no regressions introduced",
			Context:     "Applies to code modifications in projects with test suites",
			Confidence:  0.95,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "high",
			},
		},
		{
			Category:    CategoryWorkflowPattern,
			Summary:     "Search before create pattern",
			Pattern:     "Before creating new files or functions, search codebase to avoid duplicating existing implementations",
			Context:     "Applies to feature additions and refactoring",
			Confidence:  0.90,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "medium",
			},
		},

		// Safety Heuristics
		{
			Category:    CategorySafetyHeuristic,
			Summary:     "Confirm destructive operations",
			Pattern:     "Request confirmation before deleting files, dropping databases, or performing irreversible operations",
			Context:     "Critical for data safety",
			Confidence:  0.98,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "critical",
			},
		},
		{
			Category:    CategorySafetyHeuristic,
			Summary:     "Validate before large-scale changes",
			Pattern:     "For changes affecting multiple files, validate approach on one file first before applying to all",
			Context:     "Prevents propagating errors across codebase",
			Confidence:  0.92,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "high",
			},
		},

		// Repair Strategies
		{
			Category:    CategoryRepairStrategy,
			Summary:     "Module import resolution",
			Pattern:     "When encountering import errors, check for missing dependencies in package.json/go.mod/requirements.txt and install them",
			Context:     "Common across Node.js, Go, Python projects",
			Confidence:  0.90,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":     "seed",
				"priority":   "high",
				"applies_to": []string{"nodejs", "go", "python"},
			},
		},
		{
			Category:    CategoryRepairStrategy,
			Summary:     "Build error diagnosis sequence",
			Pattern:     "On build failures: 1) Read full error message, 2) Identify file and line, 3) Read surrounding context, 4) Apply targeted fix, 5) Verify with rebuild",
			Context:     "Systematic approach to build failures",
			Confidence:  0.88,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "high",
			},
		},

		// Context Gathering
		{
			Category:    CategoryContextGathering,
			Summary:     "Progressive context expansion",
			Pattern:     "Start with targeted file reads, expand to related files only when needed. Avoid reading entire codebase upfront.",
			Context:     "Efficient context gathering for large projects",
			Confidence:  0.87,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "medium",
			},
		},
		{
			Category:    CategoryContextGathering,
			Summary:     "Use semantic search for unfamiliar codebases",
			Pattern:     "When working with unfamiliar code, use semantic search to find relevant implementations before reading files",
			Context:     "Accelerates understanding of new projects",
			Confidence:  0.85,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "medium",
			},
		},

		// Anti-Patterns
		{
			Category:    CategoryAntiPattern,
			Summary:     "Avoid blind file overwrites",
			Pattern:     "AVOID: Rewriting entire files without reading current content first. CAUSES: Loss of recent changes and merge conflicts.",
			Context:     "Common mistake leading to data loss",
			Confidence:  0.95,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "critical",
			},
		},
		{
			Category:    CategoryAntiPattern,
			Summary:     "Avoid assuming test framework without verification",
			Pattern:     "AVOID: Running tests without checking project's test framework (Jest vs Mocha, pytest vs unittest, etc). CAUSES: Incorrect test commands and false negatives.",
			Context:     "Must verify test setup before running tests",
			Confidence:  0.88,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "high",
			},
		},

		// Tool Preferences
		{
			Category:    CategoryToolPreference,
			Summary:     "Prefer semantic_search for concept lookup",
			Pattern:     "Use semantic_search instead of grep when looking for concepts, patterns, or implementations. Use grep for exact identifiers.",
			Context:     "Semantic search finds relevant code even with different naming",
			Confidence:  0.85,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "medium",
			},
		},
		{
			Category:    CategoryToolPreference,
			Summary:     "Use multi_replace for batch edits",
			Pattern:     "When making multiple independent edits, use multi_replace_string_in_file instead of sequential replace_string_in_file calls",
			Context:     "More efficient and reduces API overhead",
			Confidence:  0.90,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "medium",
			},
		},

		// Error Resolution
		{
			Category:    CategoryErrorResolution,
			Summary:     "Type mismatch resolution pattern",
			Pattern:     "For type errors: 1) Identify expected vs actual types, 2) Check for implicit conversions available, 3) Add explicit cast or conversion if safe, 4) Verify both sides of assignment",
			Context:     "Common in statically-typed languages",
			Confidence:  0.86,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":     "seed",
				"priority":   "medium",
				"applies_to": []string{"go", "typescript", "rust", "java"},
			},
		},
		{
			Category:    CategoryErrorResolution,
			Summary:     "Undefined reference resolution",
			Pattern:     "For undefined symbols: 1) Check if import is missing, 2) Verify symbol is exported from source module, 3) Check for typos in identifier, 4) Verify symbol exists in dependencies",
			Context:     "Common across all languages",
			Confidence:  0.88,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "medium",
			},
		},
		{
			Category:    CategoryErrorResolution,
			Summary:     "Null pointer / nil dereference diagnosis",
			Pattern:     "For nil/null errors: 1) Trace variable initialization, 2) Check for early returns without initialization, 3) Add nil checks before usage, 4) Consider using optional/result types",
			Context:     "Critical runtime error across all languages",
			Confidence:  0.90,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "high",
			},
		},
		{
			Category:    CategoryErrorResolution,
			Summary:     "Async/Promise error handling",
			Pattern:     "For async errors: 1) Ensure all promises have .catch() or try/catch in async/await, 2) Check for unhandled promise rejections, 3) Verify error propagation through promise chains",
			Context:     "Critical for JavaScript/TypeScript/Node.js applications",
			Confidence:  0.87,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":     "seed",
				"priority":   "high",
				"applies_to": []string{"javascript", "typescript", "nodejs"},
			},
		},
		{
			Category:    CategoryErrorResolution,
			Summary:     "Race condition detection",
			Pattern:     "For intermittent failures: 1) Look for shared state accessed from multiple goroutines/threads, 2) Add proper locking or use channels/atomic operations, 3) Check for closure variable capture in loops",
			Context:     "Critical for concurrent code",
			Confidence:  0.85,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "high",
			},
		},

		// More Workflow Patterns (Framework-specific)
		{
			Category:    CategoryWorkflowPattern,
			Summary:     "React component modification pattern",
			Pattern:     "When editing React components: 1) Read component file, 2) Check for hooks dependencies, 3) Verify prop types, 4) Update tests if present, 5) Check for memo/callback optimizations",
			Context:     "React-specific safe modification sequence",
			Confidence:  0.88,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":     "seed",
				"priority":   "high",
				"applies_to": []string{"react", "javascript", "typescript"},
			},
		},
		{
			Category:    CategoryWorkflowPattern,
			Summary:     "API endpoint modification pattern",
			Pattern:     "When modifying API endpoints: 1) Read route handler, 2) Check authentication/authorization, 3) Validate input schemas, 4) Update OpenAPI/Swagger if present, 5) Test with sample requests",
			Context:     "Safe API modification sequence",
			Confidence:  0.90,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "high",
			},
		},
		{
			Category:    CategoryWorkflowPattern,
			Summary:     "Database migration pattern",
			Pattern:     "For schema changes: 1) Create migration file, 2) Write both up and down migrations, 3) Test on development database first, 4) Verify data integrity, 5) Plan rollback strategy",
			Context:     "Critical for safe database changes",
			Confidence:  0.92,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "critical",
			},
		},
		{
			Category:    CategoryWorkflowPattern,
			Summary:     "Git workflow for feature development",
			Pattern:     "Feature development: 1) Create feature branch from main, 2) Make incremental commits with clear messages, 3) Rebase before merge to keep history clean, 4) Squash related commits if needed",
			Context:     "Standard Git workflow for team collaboration",
			Confidence:  0.85,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "medium",
			},
		},
		{
			Category:    CategoryWorkflowPattern,
			Summary:     "Debugging systematic approach",
			Pattern:     "For bugs: 1) Reproduce reliably, 2) Isolate minimal test case, 3) Binary search through code/commits, 4) Add logging at boundaries, 5) Verify fix doesn't break other cases",
			Context:     "Systematic debugging methodology",
			Confidence:  0.89,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "high",
			},
		},
		{
			Category:    CategoryWorkflowPattern,
			Summary:     "Refactoring safe sequence",
			Pattern:     "For refactoring: 1) Ensure tests exist and pass, 2) Make small incremental changes, 3) Run tests after each change, 4) Use IDE refactoring tools when available, 5) Review diff carefully",
			Context:     "Safe refactoring without breaking functionality",
			Confidence:  0.91,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "high",
			},
		},

		// Performance Optimization Patterns
		{
			Category:    CategoryPerformanceOptimization,
			Summary:     "Caching strategy pattern",
			Pattern:     "For repeated expensive operations: 1) Identify cacheable operations, 2) Choose appropriate cache scope (request/session/global), 3) Implement cache invalidation strategy, 4) Monitor cache hit rates",
			Context:     "Performance optimization through caching",
			Confidence:  0.87,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "medium",
				"type":     "performance",
			},
		},
		{
			Category:    CategoryToolPreference,
			Summary:     "Batch operations pattern",
			Pattern:     "For multiple similar operations: Group into batches to reduce overhead (database queries, API calls, file I/O). Use bulk operations when available.",
			Context:     "Reduces I/O and network overhead",
			Confidence:  0.88,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "medium",
				"type":     "performance",
			},
		},
		{
			Category:    CategoryPerformanceOptimization,
			Summary:     "Lazy loading pattern",
			Pattern:     "For large datasets or expensive initialization: Load data on-demand rather than upfront. Use lazy evaluation for computed values.",
			Context:     "Improves startup time and memory usage",
			Confidence:  0.86,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "medium",
				"type":     "performance",
			},
		},

		// Security Patterns
		{
			Category:    CategorySecurityPattern,
			Summary:     "Input validation pattern",
			Pattern:     "For user input: 1) Validate all input against expected format, 2) Sanitize before processing, 3) Use parameterized queries for database, 4) Escape output in templates",
			Context:     "Prevents injection attacks (SQL, XSS, command injection)",
			Confidence:  0.95,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "critical",
				"type":     "security",
			},
		},
		{
			Category:    CategorySecurityPattern,
			Summary:     "Authentication check pattern",
			Pattern:     "For protected endpoints: 1) Verify authentication token/session first, 2) Check authorization for specific resource, 3) Log access attempts, 4) Use secure session management",
			Context:     "Essential for securing APIs and routes",
			Confidence:  0.96,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "critical",
				"type":     "security",
			},
		},
		{
			Category:    CategorySecurityPattern,
			Summary:     "Secrets management pattern",
			Pattern:     "For sensitive data: 1) Never hardcode credentials, 2) Use environment variables or secrets manager, 3) Rotate secrets periodically, 4) Audit secret access",
			Context:     "Prevents credential leakage",
			Confidence:  0.97,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "critical",
				"type":     "security",
			},
		},
		{
			Category:    CategoryAntiPattern,
			Summary:     "Avoid trusting client-side validation only",
			Pattern:     "AVOID: Relying solely on client-side validation for security. CAUSES: Attackers can bypass client checks. SOLUTION: Always validate server-side.",
			Context:     "Critical security vulnerability",
			Confidence:  0.94,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "critical",
				"type":     "security",
			},
		},

		// Testing Patterns
		{
			Category:    CategoryTestingStrategy,
			Summary:     "Unit test isolation pattern",
			Pattern:     "For unit tests: 1) Test one behavior per test, 2) Mock external dependencies, 3) Use setup/teardown for state management, 4) Make tests deterministic and fast",
			Context:     "Ensures reliable and maintainable tests",
			Confidence:  0.89,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "high",
				"type":     "testing",
			},
		},
		{
			Category:    CategoryTestingStrategy,
			Summary:     "Test-driven development pattern",
			Pattern:     "For new features: 1) Write failing test first, 2) Implement minimal code to pass, 3) Refactor while keeping tests green, 4) Repeat cycle",
			Context:     "TDD approach for better design and coverage",
			Confidence:  0.85,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "medium",
				"type":     "testing",
			},
		},
		{
			Category:    CategoryTestingStrategy,
			Summary:     "Coverage-guided testing",
			Pattern:     "After adding tests: 1) Run with coverage report, 2) Identify uncovered branches, 3) Add tests for critical uncovered paths, 4) Aim for 80%+ on core logic",
			Context:     "Ensures comprehensive test coverage",
			Confidence:  0.86,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "medium",
				"type":     "testing",
			},
		},

		// Code Quality Patterns
		{
			Category:    CategoryCodeQuality,
			Summary:     "Function extraction pattern",
			Pattern:     "For complex functions: 1) Identify cohesive blocks, 2) Extract to named functions with clear purpose, 3) Keep functions under 50 lines, 4) Use descriptive names",
			Context:     "Improves readability and maintainability",
			Confidence:  0.87,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "medium",
				"type":     "code_quality",
			},
		},
		{
			Category:    CategoryCodeQuality,
			Summary:     "Error handling consistency",
			Pattern:     "For error handling: 1) Use consistent error types across module, 2) Include context in error messages, 3) Don't swallow errors silently, 4) Log errors at appropriate level",
			Context:     "Makes debugging easier",
			Confidence:  0.90,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "high",
				"type":     "code_quality",
			},
		},
		{
			Category:    CategoryCodeQuality,
			Summary:     "Avoid deeply nested conditionals",
			Pattern:     "AVOID: Nesting conditionals more than 3 levels deep. CAUSES: Hard to read and test. SOLUTION: Use early returns, extract functions, or use guard clauses.",
			Context:     "Maintains code readability",
			Confidence:  0.85,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "medium",
				"type":     "code_quality",
			},
		},

		// API Design Patterns
		{
			Category:    CategoryAPIDesign,
			Summary:     "REST API design pattern",
			Pattern:     "For REST endpoints: 1) Use nouns for resources, verbs for actions (GET/POST/PUT/DELETE), 2) Use proper status codes, 3) Version APIs (/v1/), 4) Return consistent error format",
			Context:     "Standard REST API conventions",
			Confidence:  0.89,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "high",
				"type":     "api_design",
			},
		},
		{
			Category:    CategoryAPIDesign,
			Summary:     "API error response pattern",
			Pattern:     "For API errors: Return JSON with {\"error\": {\"code\": \"ERR_CODE\", \"message\": \"Human readable\", \"details\": {...}}}. Use 4xx for client errors, 5xx for server errors.",
			Context:     "Consistent error handling for API clients",
			Confidence:  0.91,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "high",
				"type":     "api_design",
			},
		},

		// More Repair Strategies
		{
			Category:    CategoryRepairStrategy,
			Summary:     "Port conflict resolution",
			Pattern:     "For 'port already in use' errors: 1) Find process using port (lsof -i :PORT or netstat), 2) Kill process or use different port, 3) Check for zombie processes",
			Context:     "Common development environment issue",
			Confidence:  0.88,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "medium",
			},
		},
		{
			Category:    CategoryRepairStrategy,
			Summary:     "CORS error resolution",
			Pattern:     "For CORS errors: 1) Add appropriate CORS headers on server (Access-Control-Allow-Origin), 2) Handle preflight OPTIONS requests, 3) Include credentials if needed (Access-Control-Allow-Credentials)",
			Context:     "Common web development issue",
			Confidence:  0.87,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":     "seed",
				"priority":   "medium",
				"applies_to": []string{"web", "api"},
			},
		},
		{
			Category:    CategoryRepairStrategy,
			Summary:     "Memory leak investigation",
			Pattern:     "For memory leaks: 1) Use profiler to identify growing objects, 2) Check for unclosed resources (files, connections, listeners), 3) Look for global state accumulation, 4) Verify cleanup in teardown",
			Context:     "Critical for long-running applications",
			Confidence:  0.84,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "high",
			},
		},

		// More Context Gathering
		{
			Category:    CategoryContextGathering,
			Summary:     "Architecture understanding pattern",
			Pattern:     "To understand new codebase: 1) Find README/docs first, 2) Identify entry points (main, index), 3) Map key directories, 4) Read core abstractions, 5) Trace one request end-to-end",
			Context:     "Systematic approach to understanding architecture",
			Confidence:  0.86,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "high",
			},
		},
		{
			Category:    CategoryContextGathering,
			Summary:     "Dependency analysis pattern",
			Pattern:     "To understand dependencies: 1) Read package.json/go.mod/requirements.txt, 2) Identify core vs development dependencies, 3) Check for version constraints, 4) Review dependency tree depth",
			Context:     "Essential for dependency management",
			Confidence:  0.84,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "medium",
			},
		},

		// More Tool Preferences
		{
			Category:    CategoryToolPreference,
			Summary:     "Use file_search for navigation",
			Pattern:     "When looking for specific files by name or pattern, use file_search with glob patterns. More efficient than listing directories.",
			Context:     "Efficient file discovery",
			Confidence:  0.83,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "low",
			},
		},
		{
			Category:    CategoryToolPreference,
			Summary:     "Read large files in chunks",
			Pattern:     "For large files (>1000 lines), read targeted sections using start/end line parameters instead of reading entire file",
			Context:     "Reduces token usage and improves performance",
			Confidence:  0.85,
			ShouldShare: true,
			Metadata: map[string]any{
				"source":   "seed",
				"priority": "medium",
			},
		},
	}

	var errors []error
	for _, seed := range seeds {
		if err := PromoteFederatedArtifact(ctx, seed); err != nil {
			errors = append(errors, fmt.Errorf("seed %s: %w", seed.Summary, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("some seeds failed: %v", errors)
	}

	return nil
}

// DetectWorkflowPattern analyzes a successful task completion to extract workflow patterns.
func DetectWorkflowPattern(ctx context.Context, query string, answer string, toolsUsed []string) error {
	if strings.TrimSpace(query) == "" || strings.TrimSpace(answer) == "" {
		return nil
	}

	// Detect common workflow patterns from tool sequences
	artifact := analyzeToolSequence(query, toolsUsed)
	if artifact.ShouldShare {
		return PromoteFederatedArtifact(ctx, artifact)
	}

	return nil
}

// analyzeToolSequence detects patterns in tool usage sequences.
func analyzeToolSequence(query string, toolsUsed []string) FederatedArtifact {
	queryLower := strings.ToLower(query)

	// Pattern: Read before edit
	if containsAny(toolsUsed, []string{"read_file", "grep_search", "semantic_search"}) &&
		containsAny(toolsUsed, []string{"replace_string_in_file", "multi_replace_string_in_file", "create_file"}) {
		return FederatedArtifact{
			Category:    CategoryWorkflowPattern,
			Summary:     "Context gathering before modification",
			Pattern:     "Gathered context through search/read before making file modifications",
			Context:     fmt.Sprintf("Query: %s", truncate(query, 100)),
			Confidence:  0.85,
			ShouldShare: true,
			Metadata: map[string]any{
				"tools_used": toolsUsed,
				"source":     "detected",
			},
		}
	}

	// Pattern: Test after change
	if containsAny(toolsUsed, []string{"replace_string_in_file", "multi_replace_string_in_file"}) &&
		containsAny(toolsUsed, []string{"run_in_terminal", "runTests"}) {
		return FederatedArtifact{
			Category:    CategoryWorkflowPattern,
			Summary:     "Verification after code changes",
			Pattern:     "Ran tests or validation commands after code modifications",
			Context:     fmt.Sprintf("Query: %s", truncate(query, 100)),
			Confidence:  0.88,
			ShouldShare: true,
			Metadata: map[string]any{
				"tools_used": toolsUsed,
				"source":     "detected",
			},
		}
	}

	// Pattern: Error diagnosis sequence
	if (strings.Contains(queryLower, "error") || strings.Contains(queryLower, "fix") || strings.Contains(queryLower, "bug")) &&
		containsAny(toolsUsed, []string{"read_file", "get_errors"}) {
		return FederatedArtifact{
			Category:    CategoryErrorResolution,
			Summary:     "Systematic error diagnosis",
			Pattern:     "Read error details and file context before attempting fix",
			Context:     fmt.Sprintf("Query: %s", truncate(query, 100)),
			Confidence:  0.80,
			ShouldShare: true,
			Metadata: map[string]any{
				"tools_used": toolsUsed,
				"source":     "detected",
			},
		}
	}

	return FederatedArtifact{ShouldShare: false}
}

// containsAny checks if any of the target strings are in the slice.
func containsAny(slice []string, targets []string) bool {
	for _, item := range slice {
		for _, target := range targets {
			if item == target {
				return true
			}
		}
	}
	return false
}

// truncate shortens a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// RetrieveFederatedContext retrieves relevant federated knowledge for a query.
func RetrieveFederatedContext(ctx context.Context, query string, categories []FederatedContentCategory, limit int) (string, error) {
	ctx = normalizeContext(ctx)

	if strings.TrimSpace(query) == "" {
		return "", nil
	}

	db, err := sharedMemoryDatabase(ctx)
	if err != nil {
		return "", err
	}

	// If categories specified, build targeted query
	searchQuery := query
	if len(categories) > 0 {
		catNames := make([]string, len(categories))
		for i, cat := range categories {
			catNames[i] = string(cat)
		}
		searchQuery = fmt.Sprintf("%s %s", query, strings.Join(catNames, " "))
	}

	if limit <= 0 {
		limit = 5
	}

	hits, err := agent.RetrieveSharedMemory(ctx, db, nil, nil, searchQuery, "shared_memory", limit)
	if err != nil {
		return "", err
	}

	if len(hits) == 0 {
		return "", nil
	}

	var parts []string
	for i, hit := range hits {
		if txt, ok := hit.Payload["text"].(string); ok && strings.TrimSpace(txt) != "" {
			category := "general"
			if cat, ok := hit.Payload["category"].(string); ok {
				category = cat
			}
			parts = append(parts, fmt.Sprintf("[%s] %s", category, strings.TrimSpace(txt)))
		}
		if i >= limit-1 {
			break
		}
	}

	if len(parts) == 0 {
		return "", nil
	}

	return strings.Join(parts, "\n\n"), nil
}

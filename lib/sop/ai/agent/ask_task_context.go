package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	log "log/slog"

	"github.com/sharedcode/sop/ai"
)

// AskTaskContext represents enhanced task classification for the Ask pipeline.
// This is returned by ClassifyAskTaskContext and contains all information needed
// for LLM-as-Orchestrator pattern with Phase Factory integration.
type AskTaskContext struct {
	// Domains: One or more domains this query spans
	// Examples: ["stores"], ["spaces"], ["stores", "spaces"], ["coding_workflow"]
	Domains []string `json:"domains"`

	// Phase: The current phase for this query
	// Values: "intake", "investigation", "planning", "execution", "review", "delivery"
	// Single-step queries default to appropriate phase (usually "investigation" or "execution")
	Phase string `json:"phase"`

	// PhasePlan: Complete sequence of phases for this task
	// Single-phase: ["investigation"]
	// Multi-phase: ["investigation", "planning", "execution", "review"]
	// The Ask pipeline processes phases sequentially via work queue
	PhasePlan []string `json:"phase_plan"`

	// Intent: High-level intent category
	// Values: "read_data", "mutate_data", "generate_code", "analyze", "plan"
	Intent string `json:"intent"`

	// CRUD: Operation type flags for tool filtering (Phase 3 optimization)
	// Values: ["C"], ["R"], ["C", "U"], ["R", "U", "D"], etc.
	// Used to filter which tools are exposed to LLM based on operation intent
	CRUD []string `json:"crud"`

	// Confidence: LLM's confidence in this classification
	// Values: "high", "medium", "low"
	Confidence string `json:"confidence"`

	// Reasoning: LLM's explanation for why it chose these domains/phases
	Reasoning string `json:"reasoning"`

	// OriginalQuery: The unmodified user query
	OriginalQuery string `json:"original_query"`
}

// ClassifyAskTaskContext performs enhanced LLM-based classification that returns:
// 1. Domain(s) - Can be multiple (e.g., ["stores", "spaces"])
// 2. Phase - Current phase (e.g., "investigation")
// 3. PhasePlan - Full sequence (e.g., ["investigation", "execution"])
// 4. Intent - High-level intent (e.g., "read_data")
// 5. Confidence - LLM's confidence level
//
// This replaces the heuristic-based routing in V1. The LLM analyzes the query
// and makes ALL routing decisions based on query semantics, not keywords.
//
// User Specification Flow:
// 1. LLM classifies domain(s) and phase from query context
// 2. Returns single-phase or multi-phase plan based on complexity
// 3. Phase Factory uses this to create domain-specific phase proxies
// 4. Each proxy exposes domain+phase-specific tools
// 5. LLM sees ALL tools and decides which to call
func (a *CopilotAgent) ClassifyAskTaskContext(ctx context.Context, query string, gen ai.Generator) (*AskTaskContext, error) {
	if gen == nil {
		return nil, fmt.Errorf("generator is required for V2 classification")
	}

	// Get available artifacts for domain hints
	storesList, spacesList, err := a.GetSampleArtifacts(ctx)
	if err != nil {
		log.Warn("Failed to get sample artifacts for classification", "error", err)
		storesList = []string{}
		spacesList = []string{}
	}

	// Build classification prompt
	prompt := a.buildAskClassificationPrompt(query, storesList, spacesList)

	// Call LLM for classification
	resp, err := gen.Generate(ctx, prompt, ai.GenOptions{
		Temperature: 0.3, // Lower temperature for more consistent classification
		MaxTokens:   500,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM classification failed: %w", err)
	}

	// Parse JSON response
	var result AskTaskContext
	if err := json.Unmarshal([]byte(resp.Text), &result); err != nil {
		// Try to extract JSON from markdown code block
		if cleaned := extractJSONFromMarkdown(resp.Text); cleaned != "" {
			if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
				log.Warn("Failed to parse classification result", "response", resp.Text, "error", err)
				return a.buildFallbackClassification(query), nil
			}
		} else {
			log.Warn("Failed to parse classification result", "response", resp.Text, "error", err)
			return a.buildFallbackClassification(query), nil
		}
	}

	// Validate and normalize
	result.OriginalQuery = query
	result = a.normalizeAskTaskContext(result)

	log.Info("AskTaskContext Classification",
		"domains", result.Domains,
		"phase", result.Phase,
		"phase_plan", result.PhasePlan,
		"intent", result.Intent,
		"confidence", result.Confidence,
		"reasoning", result.Reasoning)

	return &result, nil
}

// buildAskClassificationPrompt constructs the LLM prompt for enhanced classification.
// This prompt instructs the LLM to identify:
// - Domain(s): Which domain(s) this query involves
// - Phase: What phase this query represents
// - PhasePlan: Full sequence of phases needed
// - Intent: High-level intent category
func (a *CopilotAgent) buildAskClassificationPrompt(query string, stores, spaces []string) string {
	storesHint := ""
	if len(stores) > 0 {
		storesHint = fmt.Sprintf("\nAvailable Stores: %s", strings.Join(stores[:min(5, len(stores))], ", "))
	}

	spacesHint := ""
	if len(spaces) > 0 {
		spacesHint = fmt.Sprintf("\nAvailable Spaces: %s", strings.Join(spaces[:min(5, len(spaces))], ", "))
	}

	return fmt.Sprintf(`You are a task classifier for an AI assistant. Analyze the user query and classify it into domains, phases, and intent.

**Available Domains:**
- **stores**: Database/store operations (list, scan, query, insert, update, delete)
- **spaces**: Knowledge base operations (search, read, update, vectorize)
- **coding_workflow**: Code editing, refactoring, bug fixes, feature implementation
- **desktop**: Local file operations, shell commands, git operations

**Available Phases:**
- **intake**: Initial requirements gathering, clarification
- **investigation**: Read-only exploration, discovery, analysis
- **planning**: Design, strategy, approach planning
- **execution**: Write operations, changes, implementations
- **review**: Validation, testing, verification
- **delivery**: Summary, documentation, cleanup

**User Query:**
%s
%s%s

**Your Task:**
Analyze the query and return a JSON object with this structure:

{
  "domains": ["domain1", "domain2"],  // One or more domains (can be multiple)
  "phase": "phase_name",              // Current phase for this query
  "phase_plan": ["phase1", "phase2"], // Full sequence of phases (can be single phase)
  "intent": "intent_category",        // High-level intent
  "crud": ["C", "R", "U", "D"],       // CRUD operations (one or more: C=Create, R=Read, U=Update, D=Delete)
  "confidence": "high|medium|low",    // Your confidence level
  "reasoning": "explanation"          // Why you chose these domains/phases
}

**Classification Guidelines:**

1. **Multi-Domain Detection**: If query involves multiple domains, include ALL relevant ones
   - "Query stores and search spaces" → ["stores", "spaces"]
   - "List stores" → ["stores"]

2. **Phase Selection**: Choose based on what the query is asking to DO
   - Read-only operations → "investigation"
   - Write operations → "execution"
   - Planning/design questions → "planning"
   - Validation/testing → "review"
   - Simple direct commands → "execution" (single phase)

3. **PhasePlan Thickness**: Vary plan complexity based on task complexity
   - Simple queries → Single phase: ["investigation"]
   - Medium queries → Two phases: ["investigation", "execution"]
   - Complex queries → Full SDLC: ["intake", "investigation", "planning", "execution", "review"]
   - Direct commands (e.g., /cmd ls) → ["execution"]

4. **Intent Categories:**
   - "read_data": Queries, searches, listings, descriptions
   - "mutate_data": Inserts, updates, deletes, modifications
   - "generate_code": Code generation, refactoring, implementation
   - "analyze": Analysis, investigation, understanding
   - "plan": Planning, design, strategy

5. **CRUD Classification (Phase 3 Optimization):**
   - "C" (Create): Insert, add, create new records/files/resources
   - "R" (Read): Query, list, get, search, scan, describe, read
   - "U" (Update): Modify, edit, update, patch existing records/files
   - "D" (Delete): Remove, delete, drop records/files/resources
   - Include ALL operations the query might need (e.g., ["R", "U"] for "update after checking")

**Examples:**

Query: "List stores in production database"
{
  "domains": ["stores"],
  "phase": "investigation",
  "phase_plan": ["investigation"],
  "intent": "read_data",
  "crud": ["R"],
  "confidence": "high",
  "reasoning": "Simple read-only store listing operation"
}

Query: "Query stores and search spaces for 'authentication'"
{
  "domains": ["stores", "spaces"],
  "phase": "investigation",
  "phase_plan": ["investigation"],
  "intent": "read_data",
  "crud": ["R"],
  "confidence": "high",
  "reasoning": "Read-only operations across two domains"
}

Query: "Refactor authentication to use OAuth2"
{
  "domains": ["coding_workflow"],
  "phase": "intake",
  "phase_plan": ["intake", "investigation", "planning", "execution", "review"],
  "intent": "generate_code",
  "crud": ["R", "U"],
  "confidence": "high",
  "reasoning": "Complex code refactoring requires full SDLC, needs reading and updating code"
}

Query: "Migrate user data from stores to spaces"
{
  "domains": ["stores", "spaces"],
  "phase": "investigation",
  "phase_plan": ["investigation", "planning", "execution", "review"],
  "intent": "mutate_data",
  "crud": ["R", "C", "D"],
  "confidence": "high",
  "reasoning": "Data migration across domains requires reading from stores, creating in spaces, potentially deleting from stores"
}

Query: "/cmd ls -la"
{
  "domains": ["desktop"],
  "phase": "execution",
  "phase_plan": ["execution"],
  "intent": "read_data",
  "crud": ["R"],
  "confidence": "high",
  "reasoning": "Direct shell command execution, read-only"
}

Query: "Create a new store called 'users'"
{
  "domains": ["stores"],
  "phase": "execution",
  "phase_plan": ["execution"],
  "intent": "mutate_data",
  "crud": ["C"],
  "confidence": "high",
  "reasoning": "Direct create operation"
}

Query: "Delete old logs from stores"
{
  "domains": ["stores"],
  "phase": "execution",
  "phase_plan": ["investigation", "execution"],
  "intent": "mutate_data",
  "crud": ["R", "D"],
  "confidence": "high",
  "reasoning": "Need to read/find logs first, then delete them"
}

Return ONLY the JSON object, no other text.`, query, storesHint, spacesHint)
}

// normalizeAskTaskContext validates and normalizes the LLM classification result.
func (a *CopilotAgent) normalizeAskTaskContext(result AskTaskContext) AskTaskContext {
	// Ensure at least one domain
	if len(result.Domains) == 0 {
		result.Domains = []string{"general"}
	}

	// Normalize domain names
	for i, domain := range result.Domains {
		result.Domains[i] = normalizeDomainName(domain)
	}

	// Ensure phase is set
	if result.Phase == "" {
		result.Phase = "investigation" // Default to investigation
	}

	// Normalize phase name
	result.Phase = normalizePhase(result.Phase)

	// Ensure phase_plan is set
	if len(result.PhasePlan) == 0 {
		result.PhasePlan = []string{result.Phase}
	}

	// Normalize phase names in plan
	for i, phase := range result.PhasePlan {
		result.PhasePlan[i] = normalizePhase(phase)
	}

	// Ensure current phase is in plan
	if !contains(result.PhasePlan, result.Phase) {
		result.PhasePlan = append([]string{result.Phase}, result.PhasePlan...)
	}

	// Default intent
	if result.Intent == "" {
		result.Intent = "general"
	}

	// Default confidence
	if result.Confidence == "" {
		result.Confidence = "medium"
	}

	return result
}

// buildFallbackClassification creates a safe default classification when LLM fails.
func (a *CopilotAgent) buildFallbackClassification(query string) *AskTaskContext {
	return &AskTaskContext{
		Domains:       []string{"general"},
		Phase:         "investigation",
		PhasePlan:     []string{"investigation"},
		Intent:        "general",
		Confidence:    "low",
		Reasoning:     "Fallback classification due to LLM parse failure",
		OriginalQuery: query,
	}
}

// normalizeDomainName normalizes domain names to standard values.
func normalizeDomainName(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	switch domain {
	case "store", "database", "db":
		return "stores"
	case "space", "kb", "knowledge":
		return "spaces"
	case "code", "coding", "repo", "repository":
		return "coding_workflow"
	case "shell", "terminal", "local", "file", "git":
		return "desktop"
	default:
		return domain
	}
}

// normalizePhase normalizes phase names to standard values.
func normalizePhase(phase string) string {
	phase = strings.ToLower(strings.TrimSpace(phase))
	validPhases := []string{"intake", "investigation", "planning", "execution", "review", "delivery"}
	for _, valid := range validPhases {
		if phase == valid {
			return phase
		}
	}
	// Default to investigation if invalid
	return "investigation"
}

// extractJSONFromMarkdown extracts JSON from markdown code blocks.
func extractJSONFromMarkdown(text string) string {
	// Try ```json ... ```
	if start := strings.Index(text, "```json"); start != -1 {
		start += 7
		if end := strings.Index(text[start:], "```"); end != -1 {
			return strings.TrimSpace(text[start : start+end])
		}
	}

	// Try ``` ... ```
	if start := strings.Index(text, "```"); start != -1 {
		start += 3
		if end := strings.Index(text[start:], "```"); end != -1 {
			return strings.TrimSpace(text[start : start+end])
		}
	}

	// Try to find JSON object boundaries
	if start := strings.Index(text, "{"); start != -1 {
		if end := strings.LastIndex(text, "}"); end != -1 && end > start {
			return strings.TrimSpace(text[start : end+1])
		}
	}

	return ""
}

// contains checks if a slice contains a string.
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

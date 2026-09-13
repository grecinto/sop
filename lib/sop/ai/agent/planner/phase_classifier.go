package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
)

// PhaseClassificationStrategy defines how a phase determines relevance
type PhaseClassificationStrategy interface {
	// Classify determines if this phase is relevant for the query
	Classify(ctx context.Context, phase *BasePhase, query string, state capabilities.RoutingState, history *ClassificationHistory) capabilities.ClassificationResult

	// Name returns strategy name for logging
	Name() string

	// CanEscalate returns true if this strategy can escalate to a more expensive one
	CanEscalate() bool
}

// ClassificationHistory tracks user corrections and failures to enable adaptive classification
type ClassificationHistory struct {
	// UserCorrections tracks when user explicitly corrected phase routing
	// Key: "query_hash:suggested_phase" → "corrected_phase"
	UserCorrections map[string]string

	// FailedHeuristics tracks queries where heuristic classification failed
	// Key: "query_hash" → failure count
	FailedHeuristics map[string]int

	// SuccessfulLLM tracks queries where LLM classification succeeded after heuristic failure
	SuccessfulLLM map[string]bool
}

// NewClassificationHistory creates a new history tracker
func NewClassificationHistory() *ClassificationHistory {
	return &ClassificationHistory{
		UserCorrections:  make(map[string]string),
		FailedHeuristics: make(map[string]int),
		SuccessfulLLM:    make(map[string]bool),
	}
}

// RecordCorrection records when user corrects phase routing
func (h *ClassificationHistory) RecordCorrection(query, suggestedPhase, correctedPhase string) {
	key := fmt.Sprintf("%s:%s", hashQuery(query), suggestedPhase)
	h.UserCorrections[key] = correctedPhase
}

// RecordHeuristicFailure increments failure count for a query
func (h *ClassificationHistory) RecordHeuristicFailure(query string) {
	key := hashQuery(query)
	h.FailedHeuristics[key]++
}

// RecordLLMSuccess marks a query as successfully classified by LLM
func (h *ClassificationHistory) RecordLLMSuccess(query string) {
	key := hashQuery(query)
	h.SuccessfulLLM[key] = true
}

// ShouldEscalateToLLM returns true if heuristics have failed for this query type
func (h *ClassificationHistory) ShouldEscalateToLLM(query string) bool {
	key := hashQuery(query)
	// Escalate if: failed 2+ times OR had successful LLM before
	return h.FailedHeuristics[key] >= 2 || h.SuccessfulLLM[key]
}

func hashQuery(query string) string {
	// Simple hash - just normalize and take first 50 chars
	normalized := strings.ToLower(strings.TrimSpace(query))
	if len(normalized) > 50 {
		return normalized[:50]
	}
	return normalized
}

// HeuristicClassificationStrategy uses keyword and pattern matching (fast, cheap)
type HeuristicClassificationStrategy struct{}

func NewHeuristicStrategy() *HeuristicClassificationStrategy {
	return &HeuristicClassificationStrategy{}
}

func (s *HeuristicClassificationStrategy) Name() string {
	return "heuristic"
}

func (s *HeuristicClassificationStrategy) CanEscalate() bool {
	return true
}

func (s *HeuristicClassificationStrategy) Classify(ctx context.Context, phase *BasePhase, query string, state capabilities.RoutingState, history *ClassificationHistory) capabilities.ClassificationResult {
	_ = ctx

	// Domain matching: if this phase has a specific domain, only match that domain
	if phase.domain != "" && state.Domain != "" && state.Domain != phase.domain {
		return capabilities.ClassificationResult{}
	}

	// Explicit capability hint takes highest priority
	if hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint)); hint != "" {
		if strings.Contains(hint, phase.phaseID) || strings.Contains(hint, strings.ReplaceAll(phase.name, " ", "_")) {
			return capabilities.ClassificationResult{
				Relevant: true,
				Score:    15,
				Mode:     capabilities.CapabilityModePrimary,
				Metadata: map[string]any{"strategy": "heuristic", "method": "explicit_hint"},
			}
		}
	}

	lower := strings.ToLower(strings.TrimSpace(query))
	if lower == "" {
		return capabilities.ClassificationResult{}
	}

	// Check user corrections first - if user previously corrected this, use that
	if history != nil {
		correctedPhase := history.UserCorrections[fmt.Sprintf("%s:%s", hashQuery(query), phase.phaseID)]
		if correctedPhase == phase.phaseID {
			return capabilities.ClassificationResult{
				Relevant: true,
				Score:    16, // Highest priority - user taught us
				Mode:     capabilities.CapabilityModePrimary,
				Metadata: map[string]any{"strategy": "heuristic", "method": "user_correction"},
			}
		}
	}

	// Detect the appropriate starting phase based on task semantics
	startingPhase := detectStartingPhase(lower)
	if startingPhase == phase.phaseID {
		return capabilities.ClassificationResult{
			Relevant: true,
			Score:    14,
			Mode:     capabilities.CapabilityModePrimary,
			Metadata: map[string]any{"strategy": "heuristic", "method": "phase_detection"},
		}
	}

	// Check for phase-specific keywords (supporting role)
	for _, keyword := range phaseKeywords(phase.phaseID) {
		if strings.Contains(lower, keyword) {
			return capabilities.ClassificationResult{
				Relevant: true,
				Score:    12,
				Mode:     capabilities.CapabilityModeSupporting,
				Metadata: map[string]any{"strategy": "heuristic", "method": "keyword_match"},
			}
		}
	}

	return capabilities.ClassificationResult{}
}

// LLMClassificationStrategy uses LLM for complex/ambiguous queries (slower, more accurate)
type LLMClassificationStrategy struct {
	generator LLMGenerator
}

// LLMGenerator interface for LLM calls
type LLMGenerator interface {
	Generate(ctx context.Context, prompt string) (string, error)
	Name() string
}

// aiGeneratorAdapter adapts ai.Generator to LLMGenerator interface
type aiGeneratorAdapter struct {
	gen ai.Generator
}

func (a *aiGeneratorAdapter) Generate(ctx context.Context, prompt string) (string, error) {
	output, err := a.gen.Generate(ctx, prompt, ai.GenOptions{})
	if err != nil {
		return "", err
	}
	return output.Text, nil
}

func (a *aiGeneratorAdapter) Name() string {
	return a.gen.Name()
}

// WrapGenerator wraps an ai.Generator to implement LLMGenerator
func WrapGenerator(gen ai.Generator) LLMGenerator {
	if gen == nil {
		return nil
	}
	return &aiGeneratorAdapter{gen: gen}
}

func NewLLMStrategy(generator LLMGenerator) *LLMClassificationStrategy {
	return &LLMClassificationStrategy{generator: generator}
}

func (s *LLMClassificationStrategy) Name() string {
	return "llm"
}

func (s *LLMClassificationStrategy) CanEscalate() bool {
	return false // LLM is the highest fidelity
}

func (s *LLMClassificationStrategy) Classify(ctx context.Context, phase *BasePhase, query string, state capabilities.RoutingState, history *ClassificationHistory) capabilities.ClassificationResult {
	if s.generator == nil {
		return capabilities.ClassificationResult{}
	}

	// Build prompt with phase context
	prompt := fmt.Sprintf(`Analyze if this query is relevant for the '%s' phase in a %s workflow.

Phase: %s (%s)
Description: %s
Query: %s

Consider:
1. Does the query's intent match this phase's purpose?
2. What tools/capabilities does this phase provide that the query needs?
3. Should this phase be PRIMARY (main driver) or SUPPORTING (helps another phase)?

Return JSON only:
{
  "relevant": true/false,
  "score": 0-20,
  "mode": "primary" or "supporting" or "none",
  "confidence": 0.0-1.0,
  "reasoning": "brief explanation"
}`,
		phase.name,
		phase.domain,
		phase.phaseID,
		phase.name,
		phase.description,
		query)

	output, err := s.generator.Generate(ctx, prompt)
	if err != nil {
		return capabilities.ClassificationResult{
			Metadata: map[string]any{"strategy": "llm", "error": err.Error()},
		}
	}

	var response struct {
		Relevant   bool    `json:"relevant"`
		Score      int     `json:"score"`
		Mode       string  `json:"mode"`
		Confidence float64 `json:"confidence"`
		Reasoning  string  `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(output), &response); err != nil {
		return capabilities.ClassificationResult{
			Metadata: map[string]any{"strategy": "llm", "parse_error": err.Error()},
		}
	}

	// Map mode string to enum
	var mode capabilities.CapabilityMode
	switch strings.ToLower(response.Mode) {
	case "primary":
		mode = capabilities.CapabilityModePrimary
	case "supporting":
		mode = capabilities.CapabilityModeSupporting
	default:
		mode = capabilities.CapabilityModeNone
	}

	// Record success if relevant
	if response.Relevant && history != nil {
		history.RecordLLMSuccess(query)
	}

	return capabilities.ClassificationResult{
		Relevant: response.Relevant,
		Score:    response.Score,
		Mode:     mode,
		Metadata: map[string]any{
			"strategy":   "llm",
			"confidence": response.Confidence,
			"reasoning":  response.Reasoning,
			"model":      s.generator.Name(),
		},
	}
}

// AdaptiveClassificationStrategy automatically escalates from heuristic to LLM based on history
type AdaptiveClassificationStrategy struct {
	heuristic *HeuristicClassificationStrategy
	llm       *LLMClassificationStrategy
	history   *ClassificationHistory
}

func NewAdaptiveStrategy(generator LLMGenerator, history *ClassificationHistory) *AdaptiveClassificationStrategy {
	if history == nil {
		history = NewClassificationHistory()
	}
	return &AdaptiveClassificationStrategy{
		heuristic: NewHeuristicStrategy(),
		llm:       NewLLMStrategy(generator),
		history:   history,
	}
}

func (s *AdaptiveClassificationStrategy) Name() string {
	return "adaptive"
}

func (s *AdaptiveClassificationStrategy) CanEscalate() bool {
	return false // Already handles escalation internally
}

func shouldPreferSemanticRouting(query string, state capabilities.RoutingState) bool {
	if strings.TrimSpace(query) == "" {
		return false
	}
	if strings.TrimSpace(state.CapabilityHint) != "" {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(query))

	repoSignals := []string{"repo", "repository", "codebase", "project", "package", "module", "workspace", "file", "parser", "api", "service", "lib", "source"}
	actionSignals := []string{"fix", "repair", "debug", "broken", "broke", "failing", "fail", "regression", "trace", "diagnose", "investigate"}
	ambiguousSignals := []string{"this", "it", "issue", "problem", "error", "errors", "broken", "broke"}

	if containsAnySignal(lower, repoSignals) && containsAnySignal(lower, actionSignals) {
		return true
	}
	if containsAnySignal(lower, ambiguousSignals) && containsAnySignal(lower, actionSignals) {
		return true
	}
	if strings.Contains(lower, "fix failing tests") || strings.Contains(lower, "failing tests") || strings.Contains(lower, "tests are failing") {
		return true
	}
	return false
}

func containsAnySignal(lower string, signals []string) bool {
	for _, signal := range signals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}

func (s *AdaptiveClassificationStrategy) Classify(ctx context.Context, phase *BasePhase, query string, state capabilities.RoutingState, history *ClassificationHistory) capabilities.ClassificationResult {
	// Use provided history or fall back to instance history
	if history == nil {
		history = s.history
	}

	// Prefer semantic intent for ambiguous or repo-centric repair queries.
	// Heuristics remain the cheap front door, but semantic routing decides for the
	// cases where the user is describing a task by intent rather than literal string match.
	if shouldPreferSemanticRouting(query, state) && s.llm != nil && s.llm.generator != nil {
		result := s.llm.Classify(ctx, phase, query, state, history)
		if result.Relevant {
			if result.Metadata == nil {
				result.Metadata = make(map[string]any)
			}
			result.Metadata["semantic_first"] = true
			return result
		}
	}

	// Check if we should skip heuristics and go straight to LLM
	if history.ShouldEscalateToLLM(query) {
		result := s.llm.Classify(ctx, phase, query, state, history)
		if result.Metadata == nil {
			result.Metadata = make(map[string]any)
		}
		result.Metadata["escalated"] = true
		return result
	}

	// Try heuristic first, but only as a cheap front door.
	result := s.heuristic.Classify(ctx, phase, query, state, history)

	// If heuristic found nothing or low confidence, try LLM
	if !result.Relevant || result.Score < 10 {
		if s.llm != nil && s.llm.generator != nil {
			llmResult := s.llm.Classify(ctx, phase, query, state, history)
			if llmResult.Relevant && llmResult.Score > result.Score {
				if llmResult.Metadata == nil {
					llmResult.Metadata = make(map[string]any)
				}
				llmResult.Metadata["fallback_from"] = "heuristic"
				return llmResult
			}
		}
	}

	return result
}

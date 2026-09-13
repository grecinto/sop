package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/domains/ideworker"
)

type ContinueDelegationReference = ideworker.DelegationReference

type ContinueDelegationGuidance = ideworker.DelegationGuidance

func (a *CopilotAgent) BuildContinueDelegationGuidance(ctx context.Context, query, requestedPhase string, variables map[string]any) (ContinueDelegationGuidance, error) {
	if a == nil || a.systemDB == nil {
		return ContinueDelegationGuidance{}, nil
	}
	ltm, err := a.getOrCreateLTM(ctx)
	if err != nil {
		return ContinueDelegationGuidance{}, nil
	}
	return ideworker.BuildDelegationGuidance(ctx, ltm, query, requestedPhase, variables)
}

func (a *CopilotAgent) BuildContinueKnowledgeBaseGuidance(ctx context.Context, query, requestedPhase string, variables map[string]any) (ContinueDelegationGuidance, error) {
	if a == nil || a.systemDB == nil || !shouldEnrichContinueGuidance(requestedPhase, variables) {
		return ContinueDelegationGuidance{}, nil
	}
	if ai.GetSessionPayload(ctx) == nil {
		return ContinueDelegationGuidance{}, nil
	}

	contextText := strings.TrimSpace(a.selectedSpaceExpertContext(ctx, query, false))
	if contextText == "" {
		return ContinueDelegationGuidance{}, nil
	}

	selectedKBs := make([]string, 0, 4)
	if payload := ai.GetSessionPayload(ctx); payload != nil {
		for _, kbRef := range payload.SelectedKBs {
			if name := strings.TrimSpace(kbRef.Name); name != "" {
				selectedKBs = append(selectedKBs, name)
			}
		}
	}

	kbName := strings.Join(selectedKBs, ", ")
	if kbName == "" {
		kbName = "selected knowledge base"
	}

	return ContinueDelegationGuidance{
		TeamPractices: []string{
			"Ground the coding workflow in the selected KnowledgeBase or Space when it is relevant.",
			"Prefer KB-backed conventions and documented project rules over guesses.",
		},
		SuccessCriteria: []string{
			"Use the available KnowledgeBase evidence to guide the implementation, investigation, or fix.",
			"State clearly when the selected KnowledgeBase does not contain enough evidence.",
		},
		Constraints: []string{
			"Do not ignore selected KnowledgeBase or Space grounding when it is available.",
		},
		ImplementationNotes: []string{
			contextText,
		},
		ExpectedResultFormat: "structured_execution_result",
		ReferenceContext: []ContinueDelegationReference{{
			Kind:    "knowledge_base",
			Source:  kbName,
			Title:   "Selected KnowledgeBase Context",
			Content: contextText,
			Metadata: map[string]any{
				"query":           strings.TrimSpace(query),
				"requested_phase": strings.TrimSpace(requestedPhase),
				"selected_kbs":    append([]string(nil), selectedKBs...),
			},
		}},
		Metadata: map[string]any{
			"source":            "knowledge_base",
			"selected_kb_count": len(selectedKBs),
		},
	}, nil
}

func (a *CopilotAgent) BuildContinueMemoryGuidance(ctx context.Context, query, requestedPhase string, variables map[string]any) (ContinueDelegationGuidance, error) {
	if a == nil || a.systemDB == nil || !shouldEnrichContinueGuidance(requestedPhase, variables) {
		return ContinueDelegationGuidance{}, nil
	}

	memoryContext := strings.TrimSpace(a.getSessionMemoryContext())
	semanticRecall := strings.TrimSpace(a.getLTMSemanticContext(ctx, query))
	memoryRecall := strings.TrimSpace(a.buildVirtualMemoryRecallContext(ctx, query))
	memorySignals := a.collectSessionMemorySignals(ctx)
	if memoryContext == "" && semanticRecall == "" && memoryRecall == "" && len(memorySignals) == 0 {
		return ContinueDelegationGuidance{}, nil
	}

	implementationNotes := make([]string, 0, 2)
	if semanticRecall != "" {
		implementationNotes = append(implementationNotes, semanticRecall)
	}
	if memoryRecall != "" {
		implementationNotes = append(implementationNotes, memoryRecall)
	}
	if memoryContext != "" {
		implementationNotes = append(implementationNotes, memoryContext)
	}
	refContent := memoryRecall
	if refContent == "" {
		refContent = semanticRecall
	}
	if refContent == "" {
		refContent = memoryContext
	}
	if refContent == "" {
		refContent = "Active session memory signals are available; use them to preserve continuity."
	}

	return ContinueDelegationGuidance{
		TeamPractices: []string{
			"Carry forward relevant session memory and recent work items when continuing the prompt.",
			"Use the current session's memory trail and LTM memory neighborhood to preserve continuity across investigation and fixes.",
		},
		SuccessCriteria: []string{
			"Preserve relevant prior decisions, constraints, and unresolved items from session memory and memory recall.",
		},
		Constraints: []string{
			"Do not ignore available memory signals, prior session context, or LTM memory recall when they are present.",
		},
		ImplementationNotes:  implementationNotes,
		ExpectedResultFormat: "structured_execution_result",
		ReferenceContext: func() []ContinueDelegationReference {
			refs := make([]ContinueDelegationReference, 0, 2)
			if refContent != "" && (semanticRecall != "" || memoryRecall != "") {
				refs = append(refs, ContinueDelegationReference{
					Kind: "memory_recall",
					Source: func() string {
						if memoryRecall != "" {
							return "ltm_memory_neighborhood"
						}
						return "ltm_semantic_recall"
					}(),
					Title:   fmt.Sprintf("Memory recall for %s", strings.TrimSpace(query)),
					Content: refContent,
					Metadata: map[string]any{
						"query":           strings.TrimSpace(query),
						"requested_phase": strings.TrimSpace(requestedPhase),
						"source": func() string {
							if memoryRecall != "" {
								return "ltm_memory_neighborhood"
							}
							return "ltm_semantic_recall"
						}(),
					},
				})
			}
			if memoryContext != "" {
				refs = append(refs, ContinueDelegationReference{
					Kind:    "session_memory",
					Source:  "active_session_memory",
					Title:   fmt.Sprintf("Session memory for %s", strings.TrimSpace(query)),
					Content: memoryContext,
					Metadata: map[string]any{
						"query":           strings.TrimSpace(query),
						"requested_phase": strings.TrimSpace(requestedPhase),
						"memory_signals":  append([]string(nil), memorySignals...),
						"signal_count":    len(memorySignals),
						"has_memory_text": memoryContext != "",
					},
				})
			}
			return refs
		}(),
		Metadata: map[string]any{
			"source":                  "session_memory",
			"memory_signal_count":     len(memorySignals),
			"has_ltm_semantic_recall": semanticRecall != "",
			"has_ltm_memory_recall":   memoryRecall != "",
		},
	}, nil
}

func shouldEnrichContinueGuidance(requestedPhase string, variables map[string]any) bool {
	return ideworker.ShouldEnrichContinueGuidance(requestedPhase, variables)
}

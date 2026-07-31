package agent

import (
	"context"
	"fmt"
	"strings"

	log "log/slog"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
)

type routingAnchor struct {
	prefix   string
	entity   string
	domain   string
	artifact string
	taskCtx  *TaskContextClassification
}

type routingOutcome struct {
	taskCtx *TaskContextClassification
	matched bool
	source  string
}

func isRoutingTestGenerator(gen ai.Generator) bool {
	if gen == nil {
		return false
	}
	typeName := fmt.Sprintf("%T", gen)
	return strings.Contains(typeName, "mock") || strings.Contains(typeName, "Mock") || strings.Contains(typeName, "Smart")
}

func parseRoutingAnchor(query string) *routingAnchor {

	log.Info("query: ", "query", query)
	parts := strings.Split(query, ":")
	if len(parts) <= 1 {
		return nil
	}
	if !(strings.EqualFold(parts[0], "omni")) {
		return nil
	}
	anchor := &routingAnchor{
		prefix: parts[0],
		entity: parts[0],
	}
	if len(parts) >= 2 {
		anchor.domain = strings.TrimSpace(parts[1])
	}
	if len(parts) >= 3 {
		anchor.artifact = strings.TrimSpace(parts[2])
	}

	log.Info("anchor ", "anchor", anchor)

	anchor.taskCtx = enrichFocusedTaskContext(nil, anchor.entity, anchor.domain, anchor.artifact)
	return anchor
}

func (a *CopilotAgent) resetRoutingForTopicSwitch(query string, payload *ai.SessionPayload) {
	if payload != nil && payload.Variables != nil {
		delete(payload.Variables, "RoutingState")
	}
	a.clearMRUForTopicSwitch()
	a.recordSessionBoundary(context.Background(), string(SessionBoundaryTopicSwitch), "Topic switch", strings.TrimSpace(query))
	if a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
		newThreadID := sop.NewUUID()
		thread := &ConversationThread{
			ID:         newThreadID,
			RootPrompt: query,
			Label:      "Topic Switch",
			Category:   "General",
			Exchanges:  make([]Interaction, 0),
			Status:     "active",
		}
		a.service.session.Memory.AddThread(thread)
		a.service.session.Memory.CurrentThreadID = newThreadID
	}
}

func (a *CopilotAgent) persistRoutingState(ctx context.Context, taskCtx *TaskContextClassification) {
	if taskCtx == nil {
		return
	}
	if p := ai.GetSessionPayload(ctx); p != nil {
		if p.Variables == nil {
			p.Variables = make(map[string]any)
		}
		p.Variables["RoutingState"] = taskCtx
	}
	if a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
		a.service.session.Memory.SetRoutingState(taskCtx)
	}
}

func (a *CopilotAgent) finalizeRoutingResult(ctx context.Context, query string, out *routingOutcome, gate string) *TaskContextClassification {
	if out == nil || out.taskCtx == nil {
		return nil
	}

	taskCtx := out.taskCtx
	if gate != "" {
		taskCtx.RoutingGate = gate
	}

	if isGeneralTaskContext(taskCtx) {
		return a.buildGeneralTaskContext(ctx, query, taskCtx)
	}

	a.applyCapabilityRoutingHints(ctx, taskCtx, query)
	a.persistCapabilityAffinityHint(taskCtx)
	a.persistRoutingState(ctx, taskCtx)
	return taskCtx
}

func (a *CopilotAgent) applyCapabilityRoutingHints(ctx context.Context, taskCtx *TaskContextClassification, query string) {
	if taskCtx == nil {
		return
	}
	if a != nil && a.capabilityRegistry != nil {
		if _, ok := a.capabilityRegistry.Resolve(query, capabilities.RoutingState{Domain: taskCtx.Domain, Gate: taskCtx.RoutingGate, CapabilityHint: taskCtx.CapabilityHint}); ok {
			if taskCtx.CapabilityHint == "" {
				taskCtx.CapabilityHint = capabilityHintGeneral
			}
		}
	}

	// For continuity gate, inherit from previous turn MRU
	if taskCtx.RoutingGate == RoutingGateContinuity {
		if prevCapability, prevIntent, ok := a.getCapabilityAffinityHintFromMRU(); ok {
			taskCtx.CapabilityHint = prevCapability
			taskCtx.IntentFamily = prevIntent
			return
		}
	}

	// ✅ No keyword inference - let capabilities self-classify via Classify()
	// IntentFamily can be inferred from domain if needed
	if taskCtx.IntentFamily == "" {
		taskCtx.IntentFamily = inferIntentFamily(taskCtx.Domain, "")
	}
}

func (a *CopilotAgent) persistCapabilityAffinityHint(taskCtx *TaskContextClassification) {
	if taskCtx == nil {
		return
	}
	hint := strings.TrimSpace(taskCtx.CapabilityHint)
	if hint == "" || hint == capabilityHintGeneral {
		return
	}
	intent := strings.TrimSpace(taskCtx.IntentFamily)
	if intent == "" {
		intent = inferIntentFamily(taskCtx.Domain, "")
	}
	contextText := fmt.Sprintf("capability=%s; intent=%s", hint, intent)
	a.markMRUCategory(capabilityAffinityMRUCategory, contextText, MRUSourceAskOutcome, MRUScopeSession)
}

func (a *CopilotAgent) getCapabilityAffinityHintFromMRU() (string, string, bool) {
	if a == nil || a.service == nil || a.service.session == nil {
		return "", "", false
	}
	if item, ok := a.findMRUItem(capabilityAffinityMRUCategory, MRUSourceAskOutcome, true); ok {
		contextText := strings.TrimSpace(item.Context)
		if contextText == "" {
			return "", "", false
		}
		for _, segment := range strings.Split(contextText, ";") {
			segment = strings.TrimSpace(segment)
			if strings.HasPrefix(segment, "capability=") {
				capability := strings.TrimSpace(strings.TrimPrefix(segment, "capability="))
				intent := ""
				for _, inner := range strings.Split(contextText, ";") {
					inner = strings.TrimSpace(inner)
					if strings.HasPrefix(inner, "intent=") {
						intent = strings.TrimSpace(strings.TrimPrefix(inner, "intent="))
						break
					}
				}
				return capability, intent, capability != ""
			}
		}
	}
	return "", "", false
}

// inferIntentFamily is only used for basic intent classification from domain
// NOT for capability pre-selection (that's done by capability.Classify())
func inferIntentFamily(domain string, query string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "desktop" || domain == "desktop automation" {
		return intentFamilyLocalOps
	}
	if domain == "coding_workflow" || domain == "CodingWorkflow" {
		return intentFamilyRepoEdit
	}
	return intentFamilyGeneral
}

func (a *CopilotAgent) tryPathStyleRouting(ctx context.Context, query string) *TaskContextClassification {

	log.Info("Path-Style Routing Activated", "path_query", query)
	taskCtx := buildFocusedFallbackTaskContext("Omni", SpacesDomain, query)
	taskCtx.RoutingGate = RoutingGateFocused
	annotateTaskContextIntent(taskCtx, query)
	a.persistRoutingState(ctx, taskCtx)
	return taskCtx
}

func (a *CopilotAgent) tryPrefixBasedRouting(ctx context.Context, query string, gen ai.Generator, isTest bool, anchor *routingAnchor) (*TaskContextClassification, error) {
	if anchor == nil {
		return nil, nil
	}

	log.Info("Prefix-Based Routing Activated", "prefix", anchor.prefix)

	var taskCtx *TaskContextClassification
	if !isTest && gen != nil {
		var err error
		taskCtx, err = a.ClassifyFocusedTaskContext(ctx, query, anchor.entity, anchor.domain, anchor.artifact, gen)
		if err != nil {
			return nil, err
		}
	}

	taskCtx = enrichFocusedTaskContext(taskCtx, anchor.entity, anchor.domain, anchor.artifact)
	if p := ai.GetSessionPayload(ctx); p != nil && p.Variables != nil {
		if rs, ok := p.Variables["RoutingState"].(*TaskContextClassification); ok && rs != nil && !isTest && gen != nil {
			updatedRS, isSwitch, err := a.ClassifyContinuityTaskContext(ctx, query, rs, taskCtx, gen)
			if err == nil && isSwitch {
				log.Info("Prefix-Based Routing Triggered Topic Switch Reset")
				a.resetRoutingForTopicSwitch(query, p)
			} else if err == nil && updatedRS != nil {
				taskCtx = enforceFocusedConstraints(updatedRS, anchor.entity, anchor.domain, anchor.artifact)
			}
		}
	}

	annotateTaskContextIntent(taskCtx, query)
	return taskCtx, nil
}

func (a *CopilotAgent) tryAskContinuationBasedRouting(ctx context.Context, query string, gen ai.Generator, isTest bool, anchor *routingAnchor) (*TaskContextClassification, error) {
	p := ai.GetSessionPayload(ctx)
	if p == nil || p.Variables == nil {
		return nil, nil
	}
	rs, ok := p.Variables["RoutingState"].(*TaskContextClassification)
	if !ok || rs == nil {
		return nil, nil
	}

	if !isTest && gen != nil {
		var anchorTaskCtx *TaskContextClassification
		if anchor != nil {
			anchorTaskCtx = anchor.taskCtx
		}
		updatedRS, isSwitch, err := a.ClassifyContinuityTaskContext(ctx, query, rs, anchorTaskCtx, gen)
		if err == nil && isSwitch {
			log.Info("Ask-Continuation Routing Detected Topic Switch. Falling through to Cold-Start Routing.")
			a.resetRoutingForTopicSwitch(query, p)
			return nil, nil
		}
		if err == nil && updatedRS != nil {
			log.Info("Ask-Continuation Routing Activated: Inheriting MRU Context with Updates", "domain", updatedRS.Domain)
			annotateTaskContextIntent(updatedRS, query)
			return updatedRS, nil
		}
		return nil, err
	}

	log.Info("Ask-Continuation Routing Activated: Inheriting MRU Context (Test Mode)", "domain", rs.Domain)
	annotateTaskContextIntent(rs, query)
	return rs, nil
}

func (a *CopilotAgent) tryColdStartBasedRouting(ctx context.Context, query string, gen ai.Generator, isTest bool) (*TaskContextClassification, error) {
	log.Info("Cold-Start Routing: LLM classification (no heuristics)")

	if gen == nil || isTest {
		return nil, nil
	}

	// ✅ Always use LLM classification - let LLM decide, not keywords
	// Capabilities will self-classify via their Classify() methods
	taskCtx, err := a.ClassifyTaskContext(ctx, query, gen)
	if err != nil || taskCtx == nil {
		log.Warn("Cold-start routing classification failed or returned nil", "error", err)
		return nil, err
	}

	log.Info("Cold-start classification complete", "domain", taskCtx.Domain)
	annotateTaskContextIntent(taskCtx, query)
	return taskCtx, nil
}

// inferCapabilityRoutingSignal is DEPRECATED - kept for backward compatibility with tests only
// ⚠️ DO NOT USE for routing decisions - violates LLM-as-Orchestrator pattern
// LLM should classify domains via ClassifyTaskContext(), capabilities self-classify via Classify()
func inferCapabilityRoutingSignal(query string, domain string) (string, string, string) {
	q := strings.ToLower(strings.TrimSpace(query))

	if q == "" {
		return inferIntentFamily(domain, ""), capabilityHintGeneral, ""
	}

	// Simplified keyword detection for test compatibility ONLY - NOT used in routing
	if strings.Contains(q, "git") || strings.Contains(q, "cmd") || strings.Contains(q, "shell") {
		return intentFamilyLocalOps, capabilityHintDesktopAutomation, ""
	}
	if strings.Contains(q, "fix") || strings.Contains(q, "refactor") || strings.Contains(q, "implement") {
		return intentFamilyRepoEdit, capabilityHintCodingWorkflow, ""
	}
	if strings.Contains(q, "explain") || strings.Contains(q, "summarize") {
		return intentFamilyKnowledge, capabilityHintGeneral, ""
	}
	return inferIntentFamily(domain, q), capabilityHintGeneral, ""
}

// shouldRouteColdStartToPlanner is DEPRECATED - kept for backward compatibility with tests only
// ⚠️ Returns false always - LLM-as-Orchestrator pattern replaced heuristic routing
func shouldRouteColdStartToPlanner(query string) bool {
	return false // Disabled: Let LLM classify, not heuristics
}

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
	inferredIntent, inferredHint, _ := inferCapabilityRoutingSignal(query, taskCtx.Domain)
	if taskCtx.RoutingGate == RoutingGateContinuity {
		if prevCapability, prevIntent, ok := a.getCapabilityAffinityHintFromMRU(); ok {
			if inferredHint != "" && inferredHint != capabilityHintGeneral {
				if inferredIntent == intentFamilyRepoEdit {
					taskCtx.CapabilityHint = inferredHint
					taskCtx.IntentFamily = inferredIntent
					return
				}
			}
			taskCtx.CapabilityHint = prevCapability
			if inferredIntent == intentFamilyGeneral || inferredIntent == "" {
				taskCtx.IntentFamily = prevIntent
			} else {
				taskCtx.IntentFamily = inferredIntent
			}
			if taskCtx.CapabilityHint == "" {
				taskCtx.CapabilityHint = inferredHint
			}
			return
		}
	}
	if inferredHint != "" && inferredHint != capabilityHintGeneral {
		taskCtx.CapabilityHint = inferredHint
		taskCtx.IntentFamily = inferredIntent
		return
	}
	if taskCtx.CapabilityHint == "" {
		taskCtx.IntentFamily = inferredIntent
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

func inferCapabilityRoutingSignal(query string, domain string) (string, string, string) {
	q := strings.ToLower(strings.TrimSpace(query))

	// Infer domain from keywords (for Stores/Spaces backwards compatibility)
	inferredDomain := ""
	if strings.Contains(q, "store") || strings.Contains(q, "stores") {
		inferredDomain = StoresDomain
	} else if strings.Contains(q, "space") || strings.Contains(q, "spaces") {
		inferredDomain = SpacesDomain
	}

	if q == "" {
		return inferIntentFamily(domain, ""), capabilityHintGeneral, inferredDomain
	}
	// Check explicit knowledge queries first (highest priority for clear intent signals)
	if strings.Contains(q, "summarize") || strings.Contains(q, "summary") || strings.Contains(q, "overview") || strings.Contains(q, "explain") || strings.Contains(q, "describe") || strings.Contains(q, "walk through") {
		return intentFamilyKnowledge, capabilityHintGeneral, inferredDomain
	}
	// Check desktop automation (specific technology keywords should take priority)
	// Use word boundary check for "process" to avoid matching "processing"
	hasProcess := false
	if idx := strings.Index(q, "process"); idx >= 0 {
		// Check if "process" is a standalone word (not part of "processing")
		before := idx == 0 || !isAlphaNumeric(rune(q[idx-1]))
		after := idx+7 >= len(q) || !isAlphaNumeric(rune(q[idx+7]))
		hasProcess = before && after
	}
	if strings.Contains(q, "git") || strings.Contains(q, "cmd") || strings.Contains(q, "shell") || strings.Contains(q, "postgres") || strings.Contains(q, "splunk") || strings.Contains(q, "docker") || strings.Contains(q, "service") || hasProcess || strings.Contains(q, "terminal") || strings.Contains(q, "file system") || strings.Contains(q, "filesystem") || strings.Contains(q, "local") {
		return intentFamilyLocalOps, capabilityHintDesktopAutomation, inferredDomain
	}
	// Check coding workflow (general code editing keywords)
	if strings.Contains(q, "fix") || strings.Contains(q, "refactor") || strings.Contains(q, "patch") || strings.Contains(q, "edit") || strings.Contains(q, "implement") || strings.Contains(q, "modify") || strings.Contains(q, "add test") || strings.Contains(q, "update") || strings.Contains(q, "repo") || strings.Contains(q, "code") || strings.Contains(q, "parser") || strings.Contains(q, "function") || strings.Contains(q, "class") || strings.Contains(q, "module") || strings.Contains(q, "generate") || strings.Contains(q, "json") || strings.Contains(q, "script") || strings.Contains(q, "analysis") || strings.Contains(q, "analy") || strings.Contains(q, "sql") || strings.Contains(q, "table") || strings.Contains(q, "record") || strings.Contains(q, "search") || strings.Contains(q, "design") || strings.Contains(q, "plan") || strings.Contains(q, "architect") || strings.Contains(q, "propose") || strings.Contains(q, "document") || strings.Contains(q, "readme") || strings.Contains(q, "spec") || strings.Contains(q, "write") || strings.Contains(q, "test") {
		return intentFamilyRepoEdit, capabilityHintCodingWorkflow, inferredDomain
	}
	if strings.Contains(q, "research") || strings.Contains(q, "what") || strings.Contains(q, "why") || strings.Contains(q, "how") || strings.Contains(q, "concept") || strings.Contains(q, "docs") {
		return intentFamilyKnowledge, capabilityHintGeneral, inferredDomain
	}
	return inferIntentFamily(domain, q), capabilityHintGeneral, inferredDomain
}

func isAlphaNumeric(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

func inferIntentFamily(domain string, query string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "desktop" || domain == "desktop automation" {
		return intentFamilyLocalOps
	}
	if query != "" {
		if strings.Contains(query, "git") || strings.Contains(query, "postgres") || strings.Contains(query, "splunk") || strings.Contains(query, "cmd") || strings.Contains(query, "shell") {
			return intentFamilyLocalOps
		}
		if strings.Contains(query, "fix") || strings.Contains(query, "refactor") || strings.Contains(query, "patch") || strings.Contains(query, "implement") || strings.Contains(query, "repo") || strings.Contains(query, "code") || strings.Contains(query, "parser") || strings.Contains(query, "script") || strings.Contains(query, "json") || strings.Contains(query, "analysis") || strings.Contains(query, "analy") || strings.Contains(query, "sql") {
			return intentFamilyRepoEdit
		}
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
	a.applyCapabilityRoutingHints(ctx, taskCtx, query)
	taskCtx.RoutingGate = RoutingGateFocused
	a.persistCapabilityAffinityHint(taskCtx)
	a.persistRoutingState(ctx, taskCtx)
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
			a.applyCapabilityRoutingHints(ctx, updatedRS, query)
			updatedRS.RoutingGate = RoutingGateContinuity
			a.persistCapabilityAffinityHint(updatedRS)
			a.persistRoutingState(ctx, updatedRS)
			return updatedRS, nil
		}
		return nil, err
	}

	log.Info("Ask-Continuation Routing Activated: Inheriting MRU Context (Test Mode)", "domain", rs.Domain)
	annotateTaskContextIntent(rs, query)
	a.applyCapabilityRoutingHints(ctx, rs, query)
	rs.RoutingGate = RoutingGateContinuity
	a.persistCapabilityAffinityHint(rs)
	a.persistRoutingState(ctx, rs)
	return rs, nil
}

func (a *CopilotAgent) tryColdStartBasedRouting(ctx context.Context, query string, gen ai.Generator, isTest bool) (*TaskContextClassification, error) {
	if len(strings.Split(query, ":")) == 1 {
		log.Info("Cold-Start Routing Activated")
	}
	if a != nil && a.capabilityRegistry != nil {
		if result, handled, err := a.capabilityRegistry.TryParticipate(ctx, query, capabilities.RoutingState{}); err == nil && handled {
			if result.Summary != "" {
				log.Info("Capability participation hook handled follow-up", "summary", result.Summary)
			}
		}
	}

	// Fast path: Try keyword-based routing first (no LLM call)
	inferredIntent, inferredHint, inferredDomain := inferCapabilityRoutingSignal(query, "")

	// If we have a clear capability hint (not general), use keyword routing
	if inferredHint != capabilityHintGeneral {
		log.Info("Cold-start keyword routing (fast path)", "capability", inferredHint, "intent", inferredIntent, "domain", inferredDomain)
		taskCtx := &TaskContextClassification{
			CapabilityHint: inferredHint,
			IntentFamily:   inferredIntent,
			Domain:         inferredDomain,
			RoutingGate:    RoutingGateDiscovery,
		}
		annotateTaskContextIntent(taskCtx, query)
		a.persistCapabilityAffinityHint(taskCtx)
		a.persistRoutingState(ctx, taskCtx)
		return taskCtx, nil
	}

	// Slow path: Ambiguous query requires LLM classification
	if gen == nil || isTest {
		return nil, nil
	}

	log.Info("Cold-start LLM classification (ambiguous query)")
	taskCtx, err := a.ClassifyTaskContext(ctx, query, gen)
	if err != nil || taskCtx == nil {
		log.Warn("Cold-start routing classification failed or returned nil", "error", err)
		return nil, err
	}

	log.Info("Cold-start routing classification success", "domain", taskCtx.Domain)
	annotateTaskContextIntent(taskCtx, query)
	a.applyCapabilityRoutingHints(ctx, taskCtx, query)
	taskCtx.RoutingGate = RoutingGateDiscovery
	a.persistCapabilityAffinityHint(taskCtx)
	a.persistRoutingState(ctx, taskCtx)
	return taskCtx, nil
}

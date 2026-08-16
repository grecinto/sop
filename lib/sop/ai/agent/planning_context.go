package agent

import (
	"strings"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

func (a *CopilotAgent) resolvePlanningContextForDomain(query string, domain string) string {
	if a == nil || a.capabilityRegistry == nil {
		return ""
	}
	routingState := capabilities.RoutingState{Domain: strings.TrimSpace(domain), CapabilityHint: "planning"}
	classified := a.capabilityRegistry.Classify(query, routingState)
	parts := make([]string, 0, len(classified))
	seen := map[string]bool{}
	for _, entry := range classified {
		if entry.Capability == nil {
			continue
		}
		provider, ok := entry.Capability.(capabilities.PlanningContextProvider)
		if !ok {
			continue
		}
		if seen[entry.Capability.ID()] {
			continue
		}
		seen[entry.Capability.ID()] = true
		text := strings.TrimSpace(provider.BuildPlanningContext(query, routingState))
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

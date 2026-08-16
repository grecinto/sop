package capabilities

import (
	"context"
	"fmt"
	log "log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/sharedcode/sop/ai"
)

// RoutingState is the minimal routing context shared between the core Ask pipeline and capabilities.
type RoutingState struct {
	Domain         string
	Gate           string
	CapabilityHint string
	CRUDFlags      map[string]bool // C/R/U/D flags from classification (Phase 3 optimization)
}

// Entitlement describes whether a capability is available under the current plan.
type Entitlement struct {
	// Basic flags
	Premium          bool
	EnabledByDefault bool
	Tier             string

	// Progressive authorization gates (Gap 1: Gate 0-4)
	RequiredGate AuthorizationGate

	// Approval requirements (Gap 1)
	Approvals []ApprovalType

	// Fine-grained permissions (Gap 1)
	Permissions []Permission
}

// ToolDefinition is a lightweight tool contract that capabilities can contribute.
type ToolDefinition struct {
	Name        string
	Description string
	Schema      string
	Tags        []string // CRUD tags: C, R, U, D for tool filtering (Phase 3 optimization)
}

// GuidanceRule is a single reusable instruction unit for a capability.
type GuidanceRule struct {
	ID          string
	Category    string
	Priority    string
	Description string
	AppliesTo   []string
	When        string
}

// GuidanceIntensity describes how much domain-specific steering a capability wants to contribute.
type GuidanceIntensity string

const (
	GuidanceIntensityMinimal GuidanceIntensity = "minimal"
	GuidanceIntensityLight   GuidanceIntensity = "light"
	GuidanceIntensityDeep    GuidanceIntensity = "deep"
)

// GuidanceProfile is a structured, composable guidance contract that capabilities can expose.
type GuidanceProfile struct {
	Intensity         GuidanceIntensity
	Role              string
	Principles        []string
	PhaseRules        []GuidanceRule
	SituationRules    []GuidanceRule
	ToolSelection     []GuidanceRule
	VerificationRules []GuidanceRule
	Guardrails        []GuidanceRule
}

// NewGuidanceProfile creates a progressive guidance profile with a sensible default intensity.
func NewGuidanceProfile(intensity GuidanceIntensity, role string) GuidanceProfile {
	if intensity == "" {
		intensity = GuidanceIntensityMinimal
	}
	return GuidanceProfile{Intensity: intensity, Role: role}
}

// IsEmpty reports whether a guidance profile contributes any meaningful steering.
func (p GuidanceProfile) IsEmpty() bool {
	return p.Intensity == "" && p.Role == "" && len(p.Principles) == 0 && len(p.PhaseRules) == 0 && len(p.SituationRules) == 0 && len(p.ToolSelection) == 0 && len(p.VerificationRules) == 0 && len(p.Guardrails) == 0
}

// MergeGuidanceProfiles combines two profiles, preserving the most informative values.
func MergeGuidanceProfiles(profiles ...GuidanceProfile) GuidanceProfile {
	merged := GuidanceProfile{}
	for _, profile := range profiles {
		if profile.Intensity != "" {
			merged.Intensity = profile.Intensity
		}
		if merged.Role == "" {
			merged.Role = profile.Role
		}
		merged.Principles = append(merged.Principles, profile.Principles...)
		merged.PhaseRules = append(merged.PhaseRules, profile.PhaseRules...)
		merged.SituationRules = append(merged.SituationRules, profile.SituationRules...)
		merged.ToolSelection = append(merged.ToolSelection, profile.ToolSelection...)
		merged.VerificationRules = append(merged.VerificationRules, profile.VerificationRules...)
		merged.Guardrails = append(merged.Guardrails, profile.Guardrails...)
	}
	if merged.Intensity == "" {
		merged.Intensity = GuidanceIntensityMinimal
	}
	return merged
}

// ApplyDefaults merges a child profile with a base/default profile so the child inherits the baseline
// and only overrides the fields it explicitly provides.
func (p GuidanceProfile) ApplyDefaults(base GuidanceProfile) GuidanceProfile {
	if p.Intensity == "" {
		p.Intensity = base.Intensity
	}
	if p.Role == "" {
		p.Role = base.Role
	}
	if len(p.Principles) == 0 {
		p.Principles = append([]string{}, base.Principles...)
	} else if len(base.Principles) > 0 {
		p.Principles = append(append([]string{}, base.Principles...), p.Principles...)
	}
	if len(p.PhaseRules) == 0 {
		p.PhaseRules = append([]GuidanceRule{}, base.PhaseRules...)
	} else if len(base.PhaseRules) > 0 {
		p.PhaseRules = append(append([]GuidanceRule{}, base.PhaseRules...), p.PhaseRules...)
	}
	if len(p.SituationRules) == 0 {
		p.SituationRules = append([]GuidanceRule{}, base.SituationRules...)
	} else if len(base.SituationRules) > 0 {
		p.SituationRules = append(append([]GuidanceRule{}, base.SituationRules...), p.SituationRules...)
	}
	if len(p.ToolSelection) == 0 {
		p.ToolSelection = append([]GuidanceRule{}, base.ToolSelection...)
	} else if len(base.ToolSelection) > 0 {
		p.ToolSelection = append(append([]GuidanceRule{}, base.ToolSelection...), p.ToolSelection...)
	}
	if len(p.VerificationRules) == 0 {
		p.VerificationRules = append([]GuidanceRule{}, base.VerificationRules...)
	} else if len(base.VerificationRules) > 0 {
		p.VerificationRules = append(append([]GuidanceRule{}, base.VerificationRules...), p.VerificationRules...)
	}
	if len(p.Guardrails) == 0 {
		p.Guardrails = append([]GuidanceRule{}, base.Guardrails...)
	} else if len(base.Guardrails) > 0 {
		p.Guardrails = append(append([]GuidanceRule{}, base.Guardrails...), p.Guardrails...)
	}
	if p.Intensity == "" {
		p.Intensity = GuidanceIntensityMinimal
	}
	return p
}

// StructuredGuidanceProvider allows a capability to contribute structured guidance.
type StructuredGuidanceProvider interface {
	BuildGuidanceProfile(query string, state RoutingState) GuidanceProfile
}

// PlanningContextProvider allows a capability to contribute domain-specific
// planning context while the shared planner schema remains domain agnostic.
type PlanningContextProvider interface {
	BuildPlanningContext(query string, state RoutingState) string
}

// StepCompletionProvider allows a capability to decide whether a structured plan
// step should advance based on domain-specific semantics.
type StepCompletionProvider interface {
	EvaluateStepCompletion(query string, state RoutingState, actionText string, validation []string, toolCalls []ai.ToolCall, outcomeFacts []string) bool
}

// ToolExecutor can handle capability-specific tool invocations from the Ask loop.
type ToolExecutor interface {
	Capability
	ExecuteTool(ctx context.Context, toolName string, args map[string]any, state RoutingState) (string, error)
}

// CapabilityMode describes how a capability should participate in a request.
type CapabilityMode int

const (
	CapabilityModeNone CapabilityMode = iota
	CapabilityModePrimary
	CapabilityModeSupporting
	CapabilityModeFallback
)

// ClassificationResult captures the relevance of a capability for the current Ask.
type ClassificationResult struct {
	Relevant bool
	Score    int
	Mode     CapabilityMode
	Domains  []string
	Hints    []string
	Metadata map[string]any
}

// ClassifiedCapability is a capability plus the classification outcome from that capability.
type ClassifiedCapability struct {
	Capability Capability
	Result     ClassificationResult
}

// CapabilityResult captures the output produced by a capability run.
type CapabilityResult struct {
	Summary string
	Data    map[string]any
}

// Handoff captures a follow-up prompt that Ask can enqueue when a capability wants to hand the conversation to another phase or step.
type Handoff struct {
	Direction string
	Target    string
	Prompt    string
}

// WorkflowPlanner extends capabilities with a post-execution hook for planning
// finalization.
//
// Deprecated: this interface exists for legacy planner/workflow integration and
// historical compatibility. New orchestration work should prefer
// PlannerCapability, MultiStepPlannerCapability, and the Ask-driven
// phase/workstream system rather than converting plans into WorkflowManager
// execution.
type WorkflowPlanner interface {
	Capability
	OnFinish(ctx context.Context, result CapabilityResult) error
}

// PostTurnObserver is an opt-in hook for supporting capabilities that need to
// observe the final result of an Ask turn without becoming the selected primary
// capability.
//
// Use this sparingly for passive observers such as workstream/session state
// updates. Primary capability post-turn behavior should remain in OnAfter.
type PostTurnObserver interface {
	ObserveAfterTurn(ctx context.Context, query string, state RoutingState, result CapabilityResult) error
}

// Capability is the plug-in contract for Ask domains.
//
// Capabilities participate in the Ask pipeline by supplying context, tools, and
// instructions. Execution is delegated to the LLM/tool loop rather than being
// driven from a capability-level Run hook.
//
// Lifecycle Events:
//   - OnBefore: Called before ReAct loop execution. Use for setup, validation, or state preparation.
//     If not needed, implement as: func (c *Cap) OnBefore(...) error { return nil }
//   - OnAfter: Called after ReAct loop completion. Use for cleanup, persistence, handoff logic,
//     phase/workstream updates, legacy workflow planning finalization, or any other post-execution tasks.
//     This is the single standardized hook for all post-turn capability logic.
//     If not needed, implement as: func (c *Cap) OnAfter(...) error { return nil }
type Capability interface {
	ID() string
	Name() string
	Entitlement() Entitlement
	Classify(query string, state RoutingState) ClassificationResult
	BuildContext(query string, state RoutingState) string
	ExposeTools() []ToolDefinition
	BuildInstructions(query string, state RoutingState) string
	Handoff(ctx context.Context, query string, state RoutingState) *Handoff

	// Lifecycle event handlers
	OnBefore(ctx context.Context, query string, state RoutingState) error
	OnAfter(ctx context.Context, query string, state RoutingState, result CapabilityResult) error
}

// RenderCapabilityGuidance renders a capability's structured guidance profile for the Ask prompt.
func RenderCapabilityGuidance(cap Capability, query string, state RoutingState) string {
	if cap == nil {
		return ""
	}
	provider, ok := cap.(StructuredGuidanceProvider)
	if !ok {
		return ""
	}
	profile := provider.BuildGuidanceProfile(query, state)
	return RenderGuidanceProfile(profile)
}

// RenderGuidanceProfile converts a structured guidance profile into prompt text.
func RenderGuidanceProfile(profile GuidanceProfile) string {
	if profile.IsEmpty() {
		return ""
	}

	var lines []string
	if profile.Intensity != "" && profile.Intensity != GuidanceIntensityMinimal {
		lines = append(lines, "GUIDANCE INTENSITY")
		lines = append(lines, string(profile.Intensity))
	}
	if profile.Role != "" {
		lines = append(lines, "ROLE")
		lines = append(lines, profile.Role)
	}
	if len(profile.Principles) > 0 {
		lines = append(lines, "PRINCIPLES")
		lines = append(lines, fmt.Sprintf("- %s", strings.Join(profile.Principles, "\n- ")))
	}
	for _, section := range []struct {
		name  string
		items []GuidanceRule
	}{
		{name: "PHASE RULES", items: profile.PhaseRules},
		{name: "SITUATION RULES", items: profile.SituationRules},
		{name: "TOOL SELECTION", items: profile.ToolSelection},
		{name: "VERIFICATION RULES", items: profile.VerificationRules},
		{name: "GUARDRAILS", items: profile.Guardrails},
	} {
		if len(section.items) == 0 {
			continue
		}
		lines = append(lines, section.name)
		for _, rule := range section.items {
			line := fmt.Sprintf("- %s", rule.Description)
			if rule.When != "" {
				line += fmt.Sprintf(" (when: %s)", rule.When)
			}
			if len(rule.AppliesTo) > 0 {
				line += fmt.Sprintf(" [applies to: %s]", strings.Join(rule.AppliesTo, ", "))
			}
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// PrefixHandler extends Capability to enable direct command-prefix routing.
//
// Capabilities implementing this interface can register command prefixes (e.g., "/c", "/cmd", "/shell")
// that trigger immediate execution bypassing the normal ReAct reasoning loop. This is useful for
// domain-specific commands that require low-latency, deterministic execution.
//
// Architecture:
//
//	User Query: "/cmd ls -la"
//	     ↓
//	Agent.handleDirectToolInvocation()
//	     ↓
//	Registry.TryPrefixRouting()  → Longest-prefix-first matching
//	     ↓
//	PrefixHandler.HandlePrefix(ctx, "/cmd ls -la", "ls -la")
//	     ↓
//	Immediate execution (bypassing ReAct engine)
//
// Example implementation:
//
//	func (d *DesktopCapability) GetHandledPrefixes() []string {
//	    return []string{"/c", "/cmd", "/shell", "/exec"}
//	}
//
//	func (d *DesktopCapability) HandlePrefix(ctx context.Context, query, normalized string) (string, error) {
//	    // Execute command directly via desktop automation
//	    return d.commandExecutor(ctx, normalized)
//	}
//
// Prefix Requirements:
// - Must start with "/" (slash prefixes only)
// - Must be followed by separator (space, tab, colon, newline) or end of string
// - Empty prefixes are rejected during registration
// - Conflicts are logged (last-registered-wins)
//
// Best Practices:
// - Use short, memorable prefixes ("/c" vs "/command")
// - Register multiple aliases for the same handler if helpful ("/cmd", "/shell", "/exec")
// - Return descriptive errors for invalid input
// - Validate normalized command before execution
// - Inject dependencies (like command executors) via SetXxx methods, not constructor
type PrefixHandler interface {
	Capability

	// GetHandledPrefixes returns the list of command prefixes this capability handles.
	// Prefixes must start with "/" and should be short and memorable.
	// Multiple prefixes can route to the same handler (e.g., ["/c", "/cmd", "/shell"]).
	GetHandledPrefixes() []string

	// HandlePrefix executes the command associated with the detected prefix.
	// Parameters:
	//   ctx: request context for cancellation and tracing
	//   query: original user query (e.g., "/cmd ls -la")
	//   normalizedCommand: command after prefix removal and whitespace trimming (e.g., "ls -la")
	// Returns:
	//   result: execution output or summary
	//   error: execution error or validation failure
	HandlePrefix(ctx context.Context, query string, normalizedCommand string) (string, error)
}

// Registry stores capabilities and resolves them based on routing state.
type Registry struct {
	mu             sync.RWMutex
	capabilities   []Capability
	prefixHandlers map[string]PrefixHandler // Maps prefix to handler
	metrics        *MetricsCollector        // Gap 4: Metrics and telemetry
}

// NewRegistry creates a registry and optionally registers initial capabilities.
func NewRegistry(caps ...Capability) *Registry {
	reg := &Registry{
		metrics:        NewMetricsCollector(), // Initialize metrics collector
		prefixHandlers: make(map[string]PrefixHandler),
	}
	for _, cap := range caps {
		reg.Register(cap)
	}
	return reg
}

// Register adds a capability to the registry if it is not already present.
func (r *Registry) Register(cap Capability) {
	if cap == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.capabilities {
		if existing.ID() == cap.ID() {
			return
		}
	}
	r.capabilities = append(r.capabilities, cap)

	// Register prefix handler if capability implements PrefixHandler
	if prefixHandler, ok := cap.(PrefixHandler); ok {
		for _, prefix := range prefixHandler.GetHandledPrefixes() {
			if prefix == "" {
				log.Warn("Capability attempted to register empty prefix", "capability", cap.ID())
				continue
			}
			if existing, exists := r.prefixHandlers[prefix]; exists {
				log.Warn("Prefix conflict detected: multiple capabilities registered for same prefix",
					"prefix", prefix,
					"existing_capability", existing.ID(),
					"new_capability", cap.ID(),
					"action", "overwriting")
			}
			r.prefixHandlers[prefix] = prefixHandler
		}
	}
}

// RegisterAtFront inserts a capability ahead of existing registrations without duplicating it.
func (r *Registry) RegisterAtFront(cap Capability) {
	if cap == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.capabilities {
		if existing.ID() == cap.ID() {
			return
		}
	}
	r.capabilities = append([]Capability{cap}, r.capabilities...)

	// Register prefix handler if capability implements PrefixHandler
	if prefixHandler, ok := cap.(PrefixHandler); ok {
		for _, prefix := range prefixHandler.GetHandledPrefixes() {
			if prefix == "" {
				log.Warn("Capability attempted to register empty prefix", "capability", cap.ID())
				continue
			}
			if existing, exists := r.prefixHandlers[prefix]; exists {
				log.Warn("Prefix conflict detected: multiple capabilities registered for same prefix",
					"prefix", prefix,
					"existing_capability", existing.ID(),
					"new_capability", cap.ID(),
					"action", "overwriting")
			}
			r.prefixHandlers[prefix] = prefixHandler
		}
	}
}

// Resolve returns the first capability that can handle the request.
func (r *Registry) Resolve(query string, state RoutingState) (Capability, bool) {
	classified := r.Classify(query, state)
	if len(classified) == 0 {
		return nil, false
	}
	for _, entry := range classified {
		if entry.Result.Mode == CapabilityModePrimary || entry.Result.Mode == CapabilityModeSupporting {
			cap := entry.Capability
			if cap != nil {
				r.metrics.RecordSelection(cap.ID())
				return cap, true
			}
		}
	}
	return nil, false
}

// Classify evaluates every registered capability and returns the ranked set of matches.
func (r *Registry) Classify(query string, state RoutingState) []ClassifiedCapability {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	var matches []ClassifiedCapability
	for _, cap := range r.capabilities {
		if cap == nil {
			continue
		}
		result := cap.Classify(query, state)
		if result.Relevant {
			matches = append(matches, ClassifiedCapability{Capability: cap, Result: result})
		}
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Result.Mode != matches[j].Result.Mode {
			if matches[i].Result.Mode == CapabilityModePrimary {
				return true
			}
			if matches[j].Result.Mode == CapabilityModePrimary {
				return false
			}
			if matches[i].Result.Mode == CapabilityModeSupporting {
				return true
			}
			if matches[j].Result.Mode == CapabilityModeSupporting {
				return false
			}
		}
		if matches[i].Result.Score != matches[j].Result.Score {
			return matches[i].Result.Score > matches[j].Result.Score
		}
		return matches[i].Capability.ID() < matches[j].Capability.ID()
	})
	return matches
}

// Compose returns all enabled capabilities that can handle the request, sorted by precedence.
func (r *Registry) Compose(query string, state RoutingState, entitlements map[string]bool) []Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var matched []Capability
	for _, cap := range r.capabilities {
		result := cap.Classify(query, state)
		if !result.Relevant {
			continue
		}
		if !enabledByDefaultForEntitlements(cap, entitlements) {
			continue
		}
		matched = append(matched, cap)
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].Entitlement().Premium != matched[j].Entitlement().Premium {
			return !matched[i].Entitlement().Premium
		}
		return matched[i].ID() < matched[j].ID()
	})
	return matched
}

// ListAll returns all registered capabilities.
func (r *Registry) ListAll() []Capability {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Return a copy to avoid external mutation
	result := make([]Capability, len(r.capabilities))
	copy(result, r.capabilities)
	return result
}

func enabledByDefaultForEntitlements(cap Capability, entitlements map[string]bool) bool {
	if cap == nil {
		return false
	}
	ent := cap.Entitlement()
	if entitlements == nil {
		return ent.EnabledByDefault || !ent.Premium
	}
	if ent.Premium {
		if enabled, ok := entitlements[cap.ID()]; ok {
			return enabled
		}
		return false
	}
	return ent.EnabledByDefault || !ent.Premium
}

// TryPrefixRouting checks if any registered capability can handle the query via prefix routing.
//
// This method performs longest-prefix-first matching to handle overlapping prefixes correctly.
// For example, if both "/c" and "/cmd" are registered, "/cmd ls" will match "/cmd", not "/c".
//
// Prefix Matching Rules:
// - Exact match: "/cmd" matches prefix "/cmd" (returns error for empty command)
// - Space separator: "/cmd ls" matches prefix "/cmd" with command "ls"
// - Colon separator: "/cmd:ls" matches prefix "/cmd" with command "ls"
// - Tab/newline separators: supported for whitespace normalization
// - No boundary: "/cmdline" does NOT match prefix "/cmd" (prevents false positives)
//
// Returns:
//
//	handled: true if a matching prefix was found (even if handler returns error)
//	result: execution output from the handler (empty string on error)
//	error: validation error (empty command) or handler execution error
//
// Example Usage:
//
//	handled, result, err := registry.TryPrefixRouting(ctx, "/cmd ls -la")
//	if handled {
//	    if err != nil {
//	        return "", fmt.Errorf("command execution failed: %w", err)
//	    }
//	    return result, nil  // Direct result, bypass ReAct loop
//	}
//	// No prefix match, continue to normal routing...
func (r *Registry) TryPrefixRouting(ctx context.Context, query string) (bool, string, error) {
	if r == nil {
		return false, "", nil
	}

	if query == "" {
		return false, "", nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Collect all matching prefixes and their handlers
	type prefixMatch struct {
		prefix  string
		handler PrefixHandler
	}
	var matches []prefixMatch

	for prefix, handler := range r.prefixHandlers {
		// Check for exact prefix match or prefix with separator
		if query == prefix {
			// Exact match with no command - handle to return error
			matches = append(matches, prefixMatch{prefix, handler})
		} else if len(query) > len(prefix) && query[:len(prefix)] == prefix {
			// Check if next character is a valid separator (space, tab, colon)
			nextChar := query[len(prefix)]
			if isSeparator(nextChar) {
				matches = append(matches, prefixMatch{prefix, handler})
			}
		}
	}

	if len(matches) == 0 {
		return false, "", nil
	}

	// Sort matches by prefix length (longest first) to handle overlapping prefixes like /c and /cmd
	sort.SliceStable(matches, func(i, j int) bool {
		return len(matches[i].prefix) > len(matches[j].prefix)
	})

	// Use the longest matching prefix
	match := matches[0]
	command := query[len(match.prefix):]
	if len(command) > 0 && isSeparator(command[0]) {
		command = command[1:] // Skip separator
	}
	command = trimSpace(command)

	if command == "" {
		log.Debug("Empty command after prefix", "prefix", match.prefix, "query", query)
		return true, "", fmt.Errorf("empty command after prefix %q: usage example: %s ls", match.prefix, match.prefix)
	}

	// Found a match, delegate to handler
	log.Debug("Routing to capability via prefix", "prefix", match.prefix, "capability", match.handler.ID(), "command", command)
	result, err := match.handler.HandlePrefix(ctx, query, command)
	if err != nil {
		log.Debug("Capability prefix handler returned error", "prefix", match.prefix, "capability", match.handler.ID(), "error", err)
	}
	return true, result, err
}

// GetMetrics returns metrics for a specific capability (Gap 4: Metrics and Telemetry)
func (r *Registry) GetMetrics(capabilityID string) CapabilityMetrics {
	if r == nil || r.metrics == nil {
		return CapabilityMetrics{}
	}
	return r.metrics.GetMetrics(capabilityID)
}

// GetAllMetrics returns metrics for all capabilities (Gap 4: Metrics and Telemetry)
func (r *Registry) GetAllMetrics() map[string]CapabilityMetrics {
	if r == nil || r.metrics == nil {
		return make(map[string]CapabilityMetrics)
	}
	return r.metrics.GetAllMetrics()
}

// RecordExecution records a capability execution with duration (Gap 4: Metrics and Telemetry)
func (r *Registry) RecordExecution(capabilityID string, durationMs float64) {
	if r == nil || r.metrics == nil {
		return
	}
	r.metrics.RecordExecution(capabilityID, durationMs)
}

// RecordError records a capability error (Gap 4: Metrics and Telemetry)
func (r *Registry) RecordError(capabilityID string, err error) {
	if r == nil || r.metrics == nil {
		return
	}
	r.metrics.RecordError(capabilityID, err)
}

// RecordApproval records an approval event (Gap 4: Metrics and Telemetry)
func (r *Registry) RecordApproval(capabilityID string, approved bool) {
	if r == nil || r.metrics == nil {
		return
	}
	r.metrics.RecordApproval(capabilityID, approved)
}

func isSeparator(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == ':'
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

// FilterToolsByCRUD filters tools based on CRUD flags from classification.
// Phase 3 Optimization: Reduces exposed tools by only showing relevant CRUD operations.
//
// Logic:
// - If no CRUD flags: return all tools (no filtering)
// - If CRUD flags present: return only tools matching at least one flag
// - Safety: If filtering results in 0 tools, return all tools (prevents over-filtering)
func FilterToolsByCRUD(tools []ToolDefinition, crudFlags map[string]bool) []ToolDefinition {
	// No CRUD filtering if flags are empty
	if len(crudFlags) == 0 {
		return tools
	}

	filtered := []ToolDefinition{}
	for _, tool := range tools {
		// Tools without tags are always included (backward compatibility)
		if len(tool.Tags) == 0 {
			filtered = append(filtered, tool)
			continue
		}

		// Check if tool has at least one matching CRUD tag
		hasMatch := false
		for _, tag := range tool.Tags {
			if crudFlags[tag] {
				hasMatch = true
				break
			}
		}

		if hasMatch {
			filtered = append(filtered, tool)
		}
	}

	// Safety: If filtering removes ALL tools, return original set
	// This prevents over-filtering from breaking functionality
	if len(filtered) == 0 {
		return tools
	}

	return filtered
}

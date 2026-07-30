package capabilities

import (
	"context"
	"fmt"
	log "log/slog"
	"sort"
	"sync"
)

// RoutingState is the minimal routing context shared between the core Ask pipeline and capabilities.
type RoutingState struct {
	Domain         string
	Gate           string
	CapabilityHint string
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

// ParticipationResult captures the outcome of a capability participation hook.
type ParticipationResult struct {
	Summary string
	Data    map[string]any
}

// Participant extends capabilities with an optional lightweight hook for Ask continuity.
// Implementations can use this to contribute pending-state, approval notes, or follow-up context
// without taking over the core Ask routing loop.
type Participant interface {
	Capability
	Participate(ctx context.Context, query string, state RoutingState) (ParticipationResult, bool, error)
}

// WorkflowPlanner extends capabilities with post-execution hooks for workflow setup.
// After Run() completes (near end of Ask/ReAct loop), OnFinish() is called to:
// - Convert plan to workflow Steps
// - Configure WorkflowManager
// - Enqueue trigger to start workflow in next Ask iteration
// This ensures 100% reuse of Ask pipeline for workflow execution.
type WorkflowPlanner interface {
	Capability
	OnFinish(ctx context.Context, result CapabilityResult) error
}

// Capability is the plug-in contract for Ask domains.
//
// Capabilities participate in the Ask pipeline by supplying context, tools, and
// instructions. Execution is delegated to the LLM/tool loop rather than being
// driven from a capability-level Run hook.
type Capability interface {
	ID() string
	Name() string
	Entitlement() Entitlement
	Classify(query string, state RoutingState) ClassificationResult
	BuildContext(query string, state RoutingState) string
	ExposeTools() []ToolDefinition
	BuildInstructions(query string, state RoutingState) string
	Handoff(ctx context.Context, query string, state RoutingState) *Handoff
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

// TryParticipate asks the routed capability whether it wants to participate in the current Ask.
// If no explicit capability is supplied, it falls back to the first capability that can handle
// the current routing state, preserving backward-compatible behavior for existing callers.
func (r *Registry) TryParticipate(ctx context.Context, query string, state RoutingState) (ParticipationResult, bool, error) {
	return r.TryParticipateFor(ctx, query, state, nil)
}

// TryParticipateFor asks a specific capability whether it wants to participate in the current Ask.
// This is the path used by the Ask pipeline after routing has already resolved the selected capability.
func (r *Registry) TryParticipateFor(ctx context.Context, query string, state RoutingState, selected Capability) (ParticipationResult, bool, error) {
	if r == nil {
		return ParticipationResult{}, false, nil
	}

	if selected != nil {
		if r.metrics != nil {
			r.metrics.RecordSelection(selected.ID())
		}
		participant, ok := selected.(Participant)
		if !ok {
			return ParticipationResult{}, false, nil
		}
		result, handled, err := participant.Participate(ctx, query, state)
		if handled {
			return result, true, err
		}
		if err != nil {
			return ParticipationResult{}, true, err
		}
		return ParticipationResult{}, false, nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, cap := range r.capabilities {
		result := cap.Classify(query, state)
		if !result.Relevant {
			continue
		}
		if r.metrics != nil {
			r.metrics.RecordSelection(cap.ID())
		}
		participant, ok := cap.(Participant)
		if !ok {
			continue
		}
		participation, handled, err := participant.Participate(ctx, query, state)
		if handled {
			return participation, true, err
		}
		if err != nil {
			return ParticipationResult{}, true, err
		}
	}
	return ParticipationResult{}, false, nil
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

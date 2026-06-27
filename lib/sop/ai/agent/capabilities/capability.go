package capabilities

import (
	"sort"
	"sync"
)

// RoutingState is the minimal routing context shared between the core Ask pipeline and capabilities.
type RoutingState struct {
	Domain string
	Gate   string
}

// Entitlement describes whether a capability is available under the current plan.
type Entitlement struct {
	Premium          bool
	EnabledByDefault bool
	Tier             string
}

// ToolDefinition is a lightweight tool contract that capabilities can contribute.
type ToolDefinition struct {
	Name        string
	Description string
	Schema      string
}

// CapabilityResult captures the output produced by a capability run.
type CapabilityResult struct {
	Summary string
	Data    map[string]any
}

// Capability is the plug-in contract for Ask domains.
type Capability interface {
	ID() string
	Name() string
	Entitlement() Entitlement
	CanHandle(query string, state RoutingState) bool
	BuildContext(query string, state RoutingState) string
	ExposeTools() []ToolDefinition
	BuildInstructions(query string, state RoutingState) string
	Run(query string, state RoutingState) (CapabilityResult, error)
}

// Registry stores capabilities and resolves them based on routing state.
type Registry struct {
	mu           sync.RWMutex
	capabilities []Capability
}

// NewRegistry creates a registry and optionally registers initial capabilities.
func NewRegistry(caps ...Capability) *Registry {
	reg := &Registry{}
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
}

// Resolve returns the first capability that can handle the request.
func (r *Registry) Resolve(query string, state RoutingState) (Capability, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, cap := range r.capabilities {
		if cap.CanHandle(query, state) {
			return cap, true
		}
	}
	return nil, false
}

// Compose returns all enabled capabilities that can handle the request, sorted by precedence.
func (r *Registry) Compose(query string, state RoutingState, entitlements map[string]bool) []Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var matched []Capability
	for _, cap := range r.capabilities {
		if !cap.CanHandle(query, state) {
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

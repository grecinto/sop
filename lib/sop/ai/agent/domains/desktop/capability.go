package desktop

import (
	"github.com/sharedcode/sop/ai/agent/capabilities"
)

const (
	CapabilityID   = "desktop"
	CapabilityName = "Desktop Automation"
)

type Capability struct{}

func NewCapability() *Capability { return &Capability{} }

func (c *Capability) ID() string   { return CapabilityID }
func (c *Capability) Name() string { return CapabilityName }
func (c *Capability) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{EnabledByDefault: true, Tier: "base"}
}
func (c *Capability) CanHandle(query string, state capabilities.RoutingState) bool {
	if state.Domain == "" {
		return false
	}
	return state.Domain == "Desktop" || state.Domain == "Desktop Automation" || state.Domain == CapabilityID || state.Domain == CapabilityName
}
func (c *Capability) BuildContext(query string, state capabilities.RoutingState) string {
	return "Structured Context: Desktop Automation\n- Use local automation tools for shell, git, file, and workspace tasks when the request involves repository work, local file editing, or developer workflow operations."
}
func (c *Capability) ExposeTools() []capabilities.ToolDefinition {
	return []capabilities.ToolDefinition{{Name: "execute_local_command", Description: "Executes a shell command on the user's local machine after approval.", Schema: "{}"}}
}
func (c *Capability) BuildInstructions(query string, state capabilities.RoutingState) string {
	return ""
}
func (c *Capability) Run(query string, state capabilities.RoutingState) (capabilities.CapabilityResult, error) {
	return capabilities.CapabilityResult{Summary: "desktop automation capability placeholder"}, nil
}

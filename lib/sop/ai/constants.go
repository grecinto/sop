package ai

import "strings"

const (
	// Default Models
	DefaultModelOpenAI = "gpt-5.4"
	DefaultModelGemini = "gemini-3.5-flash"
	DefaultModelOllama = "llama3"

	// Default Application Configurations
	DefaultScriptCategory = "general"

	// Agent Types
	AgentTypeCopilot = "copilot"
	AgentIDOmni      = "omni"
	IntentOmni       = "OMNI"

	// Knowledge Base Constants
	DefaultKBName = "Core Playbook"
)

// CanonicalKBName returns the canonical system KB identifier for known names.
func CanonicalKBName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "sop", "core playbook", "core-playbook":
		return DefaultKBName
	default:
		return strings.TrimSpace(name)
	}
}

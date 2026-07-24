package sopbackend

import "testing"

func TestIsReadOnlyContext(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		expected bool
	}{
		// core_playbook variations
		{"Core Playbook", "Core Playbook", true},
		{"core playbook lowercase", "core playbook", true},
		{"core_playbook underscore", "core_playbook", true},
		{"core-playbook hyphen", "core-playbook", true},
		{"CORE_PLAYBOOK uppercase", "CORE_PLAYBOOK", true},
		{"Core-Playbook mixed", "Core-Playbook", true},

		// Shared Memory variations
		{"Shared Memory", "Shared Memory", true},
		{"shared memory lowercase", "shared memory", true},
		{"shared_memory underscore", "shared_memory", true},
		{"shared-memory hyphen", "shared-memory", true},
		{"SHARED_MEMORY uppercase", "SHARED_MEMORY", true},

		// Memory prefixed
		{"memory_ prefix", "memory_test", true},
		{"MEMORY_ prefix uppercase", "MEMORY_test", true},

		// LTM/STM suffixes
		{"user_ltm suffix", "user_ltm", true},
		{"USER_LTM uppercase", "USER_LTM", true},
		{"john_ltm", "john_ltm", true},
		{"user_stm suffix", "user_stm", true},
		{"USER_STM uppercase", "USER_STM", true},
		{"alice_stm", "alice_stm", true},

		// Non-readonly spaces (including user-created "SOP")
		{"sop lowercase - user space", "sop", false},
		{"SOP uppercase - user space", "SOP", false},
		{"custom space", "custom", false},
		{"user space", "user_space", false},
		{"medical space", "medical", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isReadOnlyContext(tt.id)
			if got != tt.expected {
				t.Errorf("isReadOnlyContext(%q) = %v, want %v", tt.id, got, tt.expected)
			}
		})
	}
}

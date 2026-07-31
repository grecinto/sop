package planner

import (
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

func TestPlannerAcceptsAllDomains(t *testing.T) {
	planner := NewPlannerCapability(nil)

	testCases := []struct {
		name     string
		query    string
		domain   string
		expected bool
	}{
		{
			name:     "coding domain with fix keyword",
			query:    "fix the bug in auth.go",
			domain:   "coding_workflow",
			expected: true,
		},
		{
			name:     "stores domain with query",
			query:    "find all stores with status=active",
			domain:   "stores",
			expected: false, // Planner doesn't match query without planning keywords
		},
		{
			name:     "stores domain with plan keyword",
			query:    "plan a query for stores",
			domain:   "stores",
			expected: true,
		},
		{
			name:     "spaces domain with design keyword",
			query:    "design a knowledge base structure",
			domain:   "spaces",
			expected: true,
		},
		{
			name:     "desktop domain with workflow keyword",
			query:    "workflow for git automation",
			domain:   "desktop",
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			state := capabilities.RoutingState{Domain: tc.domain}
			result := planner.Classify(tc.query, state)

			if tc.expected && !result.Relevant {
				t.Errorf("Expected Planner to match %q in domain %q, but it didn't", tc.query, tc.domain)
			}
			if !tc.expected && result.Relevant {
				t.Errorf("Expected Planner NOT to match %q in domain %q, but it did", tc.query, tc.domain)
			}
		})
	}
}

func TestPhaseCapabilityDomainMatching(t *testing.T) {
	testCases := []struct {
		name        string
		phaseID     string
		phaseDomain string
		queryDomain string
		query       string
		shouldMatch bool
	}{
		{
			name:        "coding investigation matches coding domain",
			phaseID:     "investigation",
			phaseDomain: "coding_workflow",
			queryDomain: "coding_workflow",
			query:       "investigate the bug",
			shouldMatch: true,
		},
		{
			name:        "stores investigation matches stores domain",
			phaseID:     "investigation",
			phaseDomain: "stores",
			queryDomain: "stores",
			query:       "investigate store structure",
			shouldMatch: true,
		},
		{
			name:        "stores investigation rejects coding domain",
			phaseID:     "investigation",
			phaseDomain: "stores",
			queryDomain: "coding_workflow",
			query:       "investigate the bug",
			shouldMatch: false,
		},
		{
			name:        "coding execution rejects stores domain",
			phaseID:     "execution",
			phaseDomain: "coding_workflow",
			queryDomain: "stores",
			query:       "execute query",
			shouldMatch: false,
		},
		{
			name:        "spaces execution matches spaces domain",
			phaseID:     "execution",
			phaseDomain: "spaces",
			queryDomain: "spaces",
			query:       "execute mint operation",
			shouldMatch: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			phase := NewPhase(tc.phaseDomain, tc.phaseID, nil)
			state := capabilities.RoutingState{Domain: tc.queryDomain}
			result := phase.Classify(tc.query, state)

			if tc.shouldMatch && !result.Relevant {
				t.Errorf("Expected phase %s (domain=%s) to match query domain %s, but it didn't",
					tc.phaseID, tc.phaseDomain, tc.queryDomain)
			}
			if !tc.shouldMatch && result.Relevant {
				t.Errorf("Expected phase %s (domain=%s) NOT to match query domain %s, but it did",
					tc.phaseID, tc.phaseDomain, tc.queryDomain)
			}
		})
	}
}

func TestCodingToolAdapter(t *testing.T) {
	adapter := NewCodingPhaseToolAdapter()

	testCases := []struct {
		phaseID      string
		expectedTool string
	}{
		{phaseID: "investigation", expectedTool: "investigation_build_context"},
		{phaseID: "investigation", expectedTool: "investigation_inspect_repo"},
		{phaseID: "planning", expectedTool: "planning_plan_changes"},
		{phaseID: "execution", expectedTool: "execution_apply_patch"},
		{phaseID: "review", expectedTool: "review_run_tests"},
	}

	for _, tc := range testCases {
		t.Run(tc.phaseID+"_has_"+tc.expectedTool, func(t *testing.T) {
			tools := adapter.GetTools(tc.phaseID)
			found := false
			for _, tool := range tools {
				if tool.Name == tc.expectedTool {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected %s to have tool %s, but it didn't. Available tools: %v",
					tc.phaseID, tc.expectedTool, toolNames(tools))
			}
		})
	}
}

func TestStoresToolAdapter(t *testing.T) {
	adapter := NewStoresPhaseToolAdapter()

	t.Run("investigation_tools_are_read_only", func(t *testing.T) {
		tools := adapter.GetTools("investigation")

		expectedTools := []string{
			"investigation_list_stores",
			"investigation_scan",
			"investigation_describe",
		}

		for _, expected := range expectedTools {
			found := false
			for _, tool := range tools {
				if tool.Name == expected {
					found = true
					// Verify tool description mentions READ-ONLY
					if tool.Description == "" || !contains(tool.Description, "READ-ONLY") {
						t.Errorf("Tool %s should be marked as READ-ONLY in description", tool.Name)
					}
					break
				}
			}
			if !found {
				t.Errorf("Expected investigation to have tool %s. Available: %v",
					expected, toolNames(tools))
			}
		}

		// Verify NO execution tools in investigation phase
		for _, tool := range tools {
			if contains(tool.Name, "execute") {
				t.Errorf("Investigation phase should not have execution tools, found: %s", tool.Name)
			}
		}
	})

	t.Run("execution_tools_are_mutations", func(t *testing.T) {
		tools := adapter.GetTools("execution")

		expectedTools := []string{
			"execution_execute_script",
		}

		for _, expected := range expectedTools {
			found := false
			for _, tool := range tools {
				if tool.Name == expected {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected execution to have tool %s. Available: %v",
					expected, toolNames(tools))
			}
		}
	})

	t.Run("planning_has_design_tools", func(t *testing.T) {
		tools := adapter.GetTools("planning")

		if len(tools) == 0 {
			t.Error("Planning phase should have at least one tool")
		}

		found := false
		for _, tool := range tools {
			if tool.Name == "planning_design_query" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected planning to have design_query tool. Available: %v", toolNames(tools))
		}
	})
}

func TestSpacesToolAdapter(t *testing.T) {
	adapter := NewSpacesPhaseToolAdapter()

	t.Run("investigation_is_read_only", func(t *testing.T) {
		tools := adapter.GetTools("investigation")

		expectedTools := []string{
			"investigation_read_config",
			"investigation_list_items",
		}

		for _, expected := range expectedTools {
			found := false
			for _, tool := range tools {
				if tool.Name == expected {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected investigation to have tool %s. Available: %v",
					expected, toolNames(tools))
			}
		}
	})

	t.Run("execution_has_mutations", func(t *testing.T) {
		tools := adapter.GetTools("execution")

		expectedTools := []string{
			"execution_mint_to_space",
			"execution_enrich_space",
		}

		for _, expected := range expectedTools {
			found := false
			for _, tool := range tools {
				if tool.Name == expected {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected execution to have tool %s. Available: %v",
					expected, toolNames(tools))
			}
		}
	})
}

func TestDesktopToolAdapter(t *testing.T) {
	adapter := NewDesktopPhaseToolAdapter()

	t.Run("planning_has_script_creation", func(t *testing.T) {
		tools := adapter.GetTools("planning")

		if len(tools) == 0 {
			t.Error("Planning phase should have at least one tool")
		}

		found := false
		for _, tool := range tools {
			if tool.Name == "planning_create_script" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected planning to have create_script tool. Available: %v", toolNames(tools))
		}
	})

	t.Run("execution_has_command_execution", func(t *testing.T) {
		tools := adapter.GetTools("execution")

		if len(tools) == 0 {
			t.Error("Execution phase should have at least one tool")
		}

		found := false
		for _, tool := range tools {
			if tool.Name == "execution_execute_command" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected execution to have execute_command tool. Available: %v", toolNames(tools))
		}
	})
}

func TestPhaseCapabilityUsesCorrectAdapter(t *testing.T) {
	testCases := []struct {
		name         string
		phaseID      string
		domain       string
		expectedTool string
	}{
		{
			name:         "coding investigation uses coding tools",
			phaseID:      "investigation",
			domain:       "coding_workflow",
			expectedTool: "investigation_inspect_repo",
		},
		{
			name:         "stores investigation uses stores tools",
			phaseID:      "investigation",
			domain:       "stores",
			expectedTool: "investigation_list_stores",
		},
		{
			name:         "spaces execution uses spaces tools",
			phaseID:      "execution",
			domain:       "spaces",
			expectedTool: "execution_create_content",
		},
		{
			name:         "desktop execution uses desktop tools",
			phaseID:      "execution",
			domain:       "desktop",
			expectedTool: "execution_execute_command",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			phase := NewPhase(tc.domain, tc.phaseID, nil)
			tools := phase.ExposeTools()

			found := false
			for _, tool := range tools {
				if tool.Name == tc.expectedTool {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("Expected phase %s (domain=%s) to expose tool %s. Available: %v",
					tc.phaseID, tc.domain, tc.expectedTool, toolNames(tools))
			}
		})
	}
}

// Helper functions

func toolNames(tools []capabilities.ToolDefinition) []string {
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name
	}
	return names
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && s != "" && substr != "" &&
		len(s) >= len(substr) && (s == substr || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

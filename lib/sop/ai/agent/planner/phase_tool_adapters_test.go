package planner

import (
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
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

func TestCodingPhaseExecutionHidesInvestigationToolsAfterDiagnosisCaptured(t *testing.T) {
	phaseArtifacts := map[string]map[string]any{
		"execution": {
			"structured_data": map[string]any{
				"diagnosis": map[string]any{
					"root_cause":   "auth setup mismatch",
					"evidence":     []any{"targeted test failure", "auth.go read"},
					"fix_target":   map[string]any{"focus_symbol": "AuthenticateUser"},
					"shared_with":  []any{"active execution context"},
					"blast_radius": map[string]any{"scope": "local to tools/sopbackend"},
				},
			},
		},
	}
	phase := newCodingPhase("execution", &cw.SharedInfrastructure{STMAccessor: func() cw.STMReader {
		return &phaseSTMReader{thread: &phaseWorkStreamReader{ctx: &phaseWorkStreamContext{phaseArtifacts: phaseArtifacts}}}
	}})

	tools := phase.ExposeTools()
	toolSet := toolNames(tools)

	for _, forbidden := range []string{
		"execution_build_context",
		"execution_inspect_repo",
		"execution_list_changed_files",
		"execution_index_repo_structure",
		"execution_compile_artifact_inventory",
		"execution_select_inventory_item",
		"execution_complete_inventory_item",
		"execution_list_directory",
	} {
		if containsStringInTest(toolSet, forbidden) {
			t.Fatalf("expected diagnosis-complete execution to hide %s, got %v", forbidden, toolSet)
		}
	}

	// read_file and search_repo must stay exposed: patch generation needs exact current file
	// content for old_content, and the diagnosis often lacks an exact line number, so hiding
	// search_repo too would leave the model with no way to locate the fix_target region.
	for _, required := range []string{"execution_read_file", "execution_search_repo", "execution_run_tests", "execution_preview_patch", "execution_apply_patch", "execution_apply_multi_file_patch"} {
		if !containsStringInTest(toolSet, required) {
			t.Fatalf("expected diagnosis-complete execution to expose %s, got %v", required, toolSet)
		}
	}
}

// TestCodingPhaseExecutionHidesInvestigationToolsUsingRealCapturedArtifact reproduces the
// production storage path: runCapture persists via SharedInfrastructure.Memory under
// "phase-artifact:<phaseID>", not WorkStreamContext.PhaseArtifacts. shouldExposeInvestigationTools
// must consult that same store, or it always sees an empty artifact and never hides these tools.
func TestCodingPhaseExecutionHidesInvestigationToolsUsingRealCapturedArtifact(t *testing.T) {
	infra := &cw.SharedInfrastructure{Memory: cw.NewInMemorySessionMemory()}
	phase := newCodingPhase("execution", infra)

	if _, err := phase.runCapture(map[string]any{
		"summary":  "repair auth mismatch",
		"status":   "exploring",
		"evidence": []any{"targeted test failure", "auth.go read"},
		"structured_data": map[string]any{
			"root_cause":   "auth setup mismatch",
			"fix_target":   map[string]any{"focus_symbol": "AuthenticateUser"},
			"shared_with":  []any{"active execution context"},
			"blast_radius": map[string]any{"scope": "local to tools/sopbackend"},
		},
	}); err != nil {
		t.Fatalf("failed to capture diagnosis artifact: %v", err)
	}

	tools := phase.ExposeTools()
	toolSet := toolNames(tools)
	if containsStringInTest(toolSet, "execution_list_directory") {
		t.Fatalf("expected diagnosis-complete execution (via real capture path) to hide execution_list_directory, got %v", toolSet)
	}
	// read_file and search_repo must stay exposed even via the real capture path: patch
	// generation needs read_file for exact content, and search_repo may still be needed once
	// to locate the fix_target line when the diagnosis doesn't include it.
	for _, required := range []string{"execution_read_file", "execution_search_repo", "execution_run_tests", "execution_preview_patch", "execution_apply_patch"} {
		if !containsStringInTest(toolSet, required) {
			t.Fatalf("expected diagnosis-complete execution to expose %s, got %v", required, toolSet)
		}
	}
}

func TestCodingPhaseExecutionHidesWholeRepoScanToolsEvenBeforeDiagnosis(t *testing.T) {
	phase := newCodingPhase("execution", &cw.SharedInfrastructure{})

	tools := phase.ExposeTools()
	toolSet := toolNames(tools)

	for _, forbidden := range []string{
		"execution_build_context",
		"execution_inspect_repo",
		"execution_list_changed_files",
		"execution_index_repo_structure",
		"execution_compile_artifact_inventory",
		"execution_select_inventory_item",
		"execution_complete_inventory_item",
	} {
		if containsStringInTest(toolSet, forbidden) {
			t.Fatalf("expected execution phase to never expose whole-repo scan tool %s, got %v", forbidden, toolSet)
		}
	}

	for _, required := range []string{"execution_read_file", "execution_list_directory", "execution_search_repo", "execution_run_tests", "execution_preview_patch", "execution_apply_patch"} {
		if !containsStringInTest(toolSet, required) {
			t.Fatalf("expected execution phase to expose narrow tool %s before diagnosis, got %v", required, toolSet)
		}
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

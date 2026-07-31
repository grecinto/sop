package capabilities

import (
	"testing"
)

func TestFilterToolsByCRUD(t *testing.T) {
	// Define test tools with various CRUD tags
	tools := []ToolDefinition{
		{Name: "list_stores", Tags: []string{"R"}},
		{Name: "create_store", Tags: []string{"C"}},
		{Name: "update_store", Tags: []string{"U"}},
		{Name: "delete_store", Tags: []string{"D"}},
		{Name: "upsert_store", Tags: []string{"C", "U"}},
		{Name: "migrate_data", Tags: []string{"R", "C", "D"}},
		{Name: "no_tags_tool", Tags: []string{}},
		{Name: "nil_tags_tool", Tags: nil},
	}

	tests := []struct {
		name          string
		crudFlags     map[string]bool
		expectedTools []string
		description   string
	}{
		{
			name:          "Empty CRUD flags returns all tools",
			crudFlags:     map[string]bool{},
			expectedTools: []string{"list_stores", "create_store", "update_store", "delete_store", "upsert_store", "migrate_data", "no_tags_tool", "nil_tags_tool"},
			description:   "No filtering when CRUD flags are empty",
		},
		{
			name:          "Read-only CRUD flag",
			crudFlags:     map[string]bool{"R": true},
			expectedTools: []string{"list_stores", "migrate_data", "no_tags_tool", "nil_tags_tool"},
			description:   "Only R-tagged tools + untagged tools",
		},
		{
			name:          "Create CRUD flag",
			crudFlags:     map[string]bool{"C": true},
			expectedTools: []string{"create_store", "upsert_store", "migrate_data", "no_tags_tool", "nil_tags_tool"},
			description:   "Only C-tagged tools + untagged tools",
		},
		{
			name:          "Update CRUD flag",
			crudFlags:     map[string]bool{"U": true},
			expectedTools: []string{"update_store", "upsert_store", "no_tags_tool", "nil_tags_tool"},
			description:   "Only U-tagged tools + untagged tools",
		},
		{
			name:          "Delete CRUD flag",
			crudFlags:     map[string]bool{"D": true},
			expectedTools: []string{"delete_store", "migrate_data", "no_tags_tool", "nil_tags_tool"},
			description:   "Only D-tagged tools + untagged tools",
		},
		{
			name:          "Multiple CRUD flags (R+U)",
			crudFlags:     map[string]bool{"R": true, "U": true},
			expectedTools: []string{"list_stores", "update_store", "upsert_store", "migrate_data", "no_tags_tool", "nil_tags_tool"},
			description:   "R or U tagged tools + untagged tools",
		},
		{
			name:          "All CRUD flags",
			crudFlags:     map[string]bool{"C": true, "R": true, "U": true, "D": true},
			expectedTools: []string{"list_stores", "create_store", "update_store", "delete_store", "upsert_store", "migrate_data", "no_tags_tool", "nil_tags_tool"},
			description:   "All tools when all CRUD flags present",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := FilterToolsByCRUD(tools, tt.crudFlags)

			// Verify expected tool count
			if len(filtered) != len(tt.expectedTools) {
				t.Errorf("%s: expected %d tools, got %d", tt.description, len(tt.expectedTools), len(filtered))
			}

			// Verify expected tools are present
			filteredMap := make(map[string]bool)
			for _, tool := range filtered {
				filteredMap[tool.Name] = true
			}

			for _, expectedName := range tt.expectedTools {
				if !filteredMap[expectedName] {
					t.Errorf("%s: expected tool '%s' not found in filtered set", tt.description, expectedName)
				}
			}
		})
	}
}

func TestFilterToolsByCRUD_SafetyFallback(t *testing.T) {
	// Test safety fallback: if filtering removes ALL tools, return original set
	tools := []ToolDefinition{
		{Name: "read_tool", Tags: []string{"R"}},
		{Name: "write_tool", Tags: []string{"U"}},
	}

	// Request delete operations when only R/U tools exist
	crudFlags := map[string]bool{"D": true}

	filtered := FilterToolsByCRUD(tools, crudFlags)

	// Safety fallback should return ALL tools
	if len(filtered) != len(tools) {
		t.Errorf("Safety fallback failed: expected %d tools (all), got %d", len(tools), len(filtered))
	}

	// Verify both tools are present
	if filtered[0].Name != "read_tool" && filtered[1].Name != "read_tool" {
		t.Errorf("Safety fallback: read_tool not found")
	}
	if filtered[0].Name != "write_tool" && filtered[1].Name != "write_tool" {
		t.Errorf("Safety fallback: write_tool not found")
	}
}

func TestFilterToolsByCRUD_BackwardCompatibility(t *testing.T) {
	// Tools without tags should always be included (backward compatibility)
	tools := []ToolDefinition{
		{Name: "legacy_tool_1", Tags: nil},
		{Name: "legacy_tool_2", Tags: []string{}},
		{Name: "new_tagged_tool", Tags: []string{"R"}},
	}

	// Filter for Create operations
	crudFlags := map[string]bool{"C": true}

	filtered := FilterToolsByCRUD(tools, crudFlags)

	// Both legacy tools should be included (no tags = always included)
	foundLegacy1 := false
	foundLegacy2 := false
	foundTagged := false

	for _, tool := range filtered {
		if tool.Name == "legacy_tool_1" {
			foundLegacy1 = true
		}
		if tool.Name == "legacy_tool_2" {
			foundLegacy2 = true
		}
		if tool.Name == "new_tagged_tool" {
			foundTagged = true
		}
	}

	if !foundLegacy1 {
		t.Error("Backward compatibility failed: legacy_tool_1 should always be included")
	}
	if !foundLegacy2 {
		t.Error("Backward compatibility failed: legacy_tool_2 should always be included")
	}
	if foundTagged {
		t.Error("new_tagged_tool should NOT be included (has 'R' tag, not 'C')")
	}
}

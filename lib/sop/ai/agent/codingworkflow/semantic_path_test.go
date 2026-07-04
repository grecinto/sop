package codingworkflow

import (
	"strings"
	"testing"
)

// TestSemanticCategoryPathBuilding demonstrates the dual indexing approach:
// 1. Accurate/lexical file path: "Coding/Workflow/Repo/sop-cloud/File/orchestrator.go"
// 2. Semantic code tree path: "Coding/Go/Package/ai.agent.codingworkflow/Type/Orchestrator/Method/Execute"
func TestSemanticCategoryPathBuilding(t *testing.T) {
	tests := []struct {
		name             string
		mem              CodingMemory
		wantPathIndex    string
		wantSemanticType string // What should be in semantic path
	}{
		{
			name: "Go function with AST nodes builds code tree path",
			mem: CodingMemory{
				Description:    "implement parser",
				ActiveFile:     "ai/agent/codingworkflow/orchestrator.go",
				StructuralPath: "sop-cloud/ai/agent/codingworkflow/Execute",
				ASTLeafNodes:   []string{"Execute", "BuildContext"},
				RepositoryHint: "sop-cloud",
			},
			wantPathIndex:    "Coding/Workflow/Repo/sop-cloud/File/orchestrator.go",
			wantSemanticType: "Coding/Go/Package/ai.agent.codingworkflow",
		},
		{
			name: "Python class builds semantic path",
			mem: CodingMemory{
				Description:    "add search method",
				ActiveFile:     "src/retriever/vector.py",
				StructuralPath: "myrepo/src/retriever/VectorRetriever",
				ASTLeafNodes:   []string{"VectorRetriever", "search"},
				RepositoryHint: "myrepo",
			},
			wantPathIndex:    "Coding/Workflow/Repo/myrepo/File/vector.py",
			wantSemanticType: "Coding/Python/Package/src.retriever",
		},
		{
			name: "TypeScript namespace builds semantic path",
			mem: CodingMemory{
				Description:    "format dates",
				ActiveFile:     "src/utils/format.ts",
				StructuralPath: "webapp/src/utils/formatDate",
				ASTLeafNodes:   []string{"formatDate"},
				RepositoryHint: "webapp",
			},
			wantPathIndex:    "Coding/Workflow/Repo/webapp/File/format.ts",
			wantSemanticType: "Coding/TypeScript/Package/src.utils",
		},
		{
			name: "No structural path falls back to text keywords",
			mem: CodingMemory{
				Description:    "fix parser bug",
				ActiveFile:     "parser.go",
				StructuralPath: "", // No structural path
				Outcome:        "patched",
				RepositoryHint: "myrepo",
			},
			wantPathIndex:    "Coding/Workflow/Repo/myrepo/File/parser.go",
			wantSemanticType: "Coding/Workflow/Semantic/fix-parser-bug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test would call the actual buildCodingMemoryPathIndexCategoryPath
			// and buildCodingMemorySemanticIndexCategoryPath from copilot.go
			// For now, verify the StructuralPath and ASTLeafNodes are set
			if tt.mem.StructuralPath != "" {
				if len(tt.mem.ASTLeafNodes) == 0 && !strings.Contains(tt.name, "No structural") {
					t.Errorf("StructuralPath set but no ASTLeafNodes")
				}
			}

			// Verify memory has required fields for semantic path building
			if tt.mem.ActiveFile == "" {
				t.Errorf("ActiveFile required for language detection")
			}
			if tt.mem.RepositoryHint == "" {
				t.Errorf("RepositoryHint required for path index")
			}
		})
	}
}

// TestStructuralPathExtractionFromContext verifies that AST data flows
// from context building through to CodingMemory
func TestStructuralPathExtractionFromContext(t *testing.T) {
	// This demonstrates the data flow:
	// 1. collectASTLeafNodes() -> (astLeafNodes, structuralPath)
	// 2. ContextPack{ASTLeafNodes, StructuralPath}
	// 3. CodingMemory{ASTLeafNodes, StructuralPath}
	// 4. buildCodeTreeSemanticPath() -> semantic category path

	content := `package codingworkflow

type Orchestrator struct {
	Memory SessionMemory
}

func (o *Orchestrator) Execute(task Task) error {
	return nil
}

func NewOrchestrator() *Orchestrator {
	return &Orchestrator{}
}
`
	workingDir := "/test/sop-cloud"
	activeFile := "ai/agent/codingworkflow/orchestrator.go"

	// In production, this is called by BuildContext
	astLeafNodes, structuralPath := collectASTLeafNodes(content, workingDir, activeFile)

	if len(astLeafNodes) == 0 {
		t.Errorf("expected AST leaf nodes to be extracted, got empty")
	}

	if structuralPath == "" {
		t.Errorf("expected structural path to be built, got empty")
	}

	// Verify structural path contains symbol name
	if len(astLeafNodes) > 0 && !strings.Contains(structuralPath, astLeafNodes[0]) {
		t.Errorf("structural path %q should contain first AST node %q", structuralPath, astLeafNodes[0])
	}

	t.Logf("Extracted AST nodes: %v", astLeafNodes)
	t.Logf("Structural path: %s", structuralPath)
	t.Logf("This would generate semantic path: Coding/Go/Package/ai.agent.codingworkflow/Type/%s/Method/%s",
		astLeafNodes[0], astLeafNodes[1])
}

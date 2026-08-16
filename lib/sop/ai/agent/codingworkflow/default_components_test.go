package codingworkflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultExpertiseRetriever_Retrieve_UsesQAAndCodingGuidance(t *testing.T) {
	retriever := &DefaultExpertiseRetriever{}

	qaCapsule, err := retriever.Retrieve(
		Task{Description: "review parser changes and test regressions"},
		RunState{},
		ContextPack{TaskType: "code", ActiveFile: "parser.go"},
	)
	if err != nil {
		t.Fatalf("expected qa guidance retrieval to succeed: %v", err)
	}
	if qaCapsule.Category != "qa" {
		t.Fatalf("expected qa category, got %q", qaCapsule.Category)
	}
	if !strings.Contains(qaCapsule.Summary, "Review the change") {
		t.Fatalf("expected QA summary guidance, got %q", qaCapsule.Summary)
	}
	if !hasString(qaCapsule.Sources, "local:qa_guidance") {
		t.Fatalf("expected qa source to be included, got %#v", qaCapsule.Sources)
	}

	codingCapsule, err := retriever.Retrieve(
		Task{Description: "implement parser fix"},
		RunState{},
		ContextPack{TaskType: "code", ActiveFile: "parser.go"},
	)
	if err != nil {
		t.Fatalf("expected coding guidance retrieval to succeed: %v", err)
	}
	if codingCapsule.Category != "coding" {
		t.Fatalf("expected coding category, got %q", codingCapsule.Category)
	}
	if !strings.Contains(codingCapsule.Summary, "keep the edit focused") {
		t.Fatalf("expected coding summary guidance, got %q", codingCapsule.Summary)
	}
	if !hasString(codingCapsule.Sources, "local:coding_guidance") {
		t.Fatalf("expected coding source to be included, got %#v", codingCapsule.Sources)
	}
}

func TestDefaultContextManager_BuildContext_UsesResolvedFileAndTestDiscovery(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "parser.go"), []byte("package demo\n\nfunc Parse() {}\n"), 0o644); err != nil {
		t.Fatalf("write parser.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "parser_test.go"), []byte("package demo\n\nfunc TestParse() {}\n"), 0o644); err != nil {
		t.Fatalf("write parser_test.go: %v", err)
	}

	manager := &DefaultContextManager{}
	pack, err := manager.BuildContext(
		Task{WorkingDir: tempDir, ActiveFile: "parser.go", Description: "implement parser fix"},
		RunState{},
	)
	if err != nil {
		t.Fatalf("build context failed: %v", err)
	}
	if pack.ActiveFile != "parser.go" {
		t.Fatalf("expected active file to be preserved, got %q", pack.ActiveFile)
	}
	if pack.TaskType != "code" {
		t.Fatalf("expected code task classification, got %q", pack.TaskType)
	}
	if !hasString(pack.TestFiles, "parser_test.go") {
		t.Fatalf("expected related test file to be discovered, got %#v", pack.TestFiles)
	}
	if len(pack.RelatedFiles) == 0 {
		t.Fatalf("expected related files to include the active file")
	}
}

func TestDefaultContextManager_BuildContext_ResumesFromStateMetadata(t *testing.T) {
	tempDir := t.TempDir()
	resumeFile := filepath.Join(tempDir, "resume.go")
	if err := os.WriteFile(resumeFile, []byte("package demo\n\ntype Resume struct{}\n"), 0o644); err != nil {
		t.Fatalf("write resume.go: %v", err)
	}

	manager := &DefaultContextManager{}
	pack, err := manager.BuildContext(
		Task{WorkingDir: tempDir, Description: "continue the prior implementation"},
		RunState{Metadata: map[string]string{"active_file": "resume.go", "continuation_summary": "resume from prior plan"}},
	)
	if err != nil {
		t.Fatalf("resume build context failed: %v", err)
	}
	if !pack.ResumedFromState {
		t.Fatal("expected resumed state to be flagged")
	}
	if pack.ActiveFile != "resume.go" {
		t.Fatalf("expected active file from metadata, got %q", pack.ActiveFile)
	}
	if pack.TaskType != "analysis" {
		t.Fatalf("expected continuation summary to classify task as analysis, got %q", pack.TaskType)
	}
}

func TestClassifyTaskType_AndInferTaskInputs_HandleModeAndPathTokens(t *testing.T) {
	state := RunState{Metadata: map[string]string{"mode": "planning"}}
	if got := classifyTaskType(Task{Description: "implement the new store API"}, state, ""); got != "code" {
		t.Fatalf("expected planning mode implementation to classify as code, got %q", got)
	}
	if got := classifyTaskType(Task{Description: "design a new API"}, state, ""); got != "planning" {
		t.Fatalf("expected planning mode design request to classify as planning, got %q", got)
	}

	workingDir, activeFile := inferTaskInputs("fix ./src/parser.go and related tests")
	if workingDir != "./src" {
		t.Fatalf("expected inferred working dir ./src, got %q", workingDir)
	}
	if activeFile != "./src/parser.go" {
		t.Fatalf("expected inferred active file ./src/parser.go, got %q", activeFile)
	}
}

func hasString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

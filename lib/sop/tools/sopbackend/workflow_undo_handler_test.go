package sopbackend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sharedcode/sop/ai/agent/codingworkflow"
)

func TestBuildUndoHistorySummaryIncludesPreview(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(filePath, []byte("package main\n\nfunc main() {\n    fmt.Println(\"hello\")\n}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	txn := codingworkflow.WorkflowTransaction{
		ID:           "txn-1",
		Timestamp:    time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
		Description:  "Fix parser regression",
		WorkflowType: "bug_fix",
		Status:       "completed",
		ModifiedFiles: []codingworkflow.ModifiedFileRecord{{
			FilePath:    "main.go",
			ChangeType:  "modify",
			DiffSummary: "updated parser branch",
			DiffStats:   "+3 -1",
			DeltaFormat: "text-raw",
			Delta:       "package main\n\nfunc main() {\n    fmt.Println(\"hello\")\n}\n",
		}},
	}

	summary := buildUndoHistorySummary([]codingworkflow.WorkflowTransaction{txn}, tempDir)
	if len(summary) != 1 {
		t.Fatalf("expected one summary item, got %d", len(summary))
	}

	if len(summary[0].Files) != 1 {
		t.Fatalf("expected one file summary, got %d", len(summary[0].Files))
	}

	preview := summary[0].Files[0].Preview
	if preview == "" {
		t.Fatalf("expected a preview to be generated from the delta payload")
	}

	if !strings.Contains(preview, "func main") {
		t.Fatalf("expected preview content to include the delta snippet, got %q", preview)
	}

	diffPreview := summary[0].Files[0].DiffPreview
	if !strings.Contains(diffPreview, "---") && !strings.Contains(diffPreview, "+") {
		t.Fatalf("expected a patch-style diff preview, got %q", diffPreview)
	}
}

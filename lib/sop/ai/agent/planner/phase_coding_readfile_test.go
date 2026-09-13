package planner

import (
	"os"
	"path/filepath"
	"testing"

	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

func TestResolveReadFilePathUsesActiveFileDirectoryForBareSiblingNames(t *testing.T) {
	tmpDir := t.TempDir()
	packageDir := filepath.Join(tmpDir, "tools", "sopbackend")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatalf("mkdir package dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "auth.go"), []byte("package sopbackend\n"), 0o644); err != nil {
		t.Fatalf("write auth.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "auth_test.go"), []byte("package sopbackend\n"), 0o644); err != nil {
		t.Fatalf("write auth_test.go: %v", err)
	}

	task := cw.Task{
		WorkingDir: tmpDir,
		ActiveFile: filepath.Join("tools", "sopbackend", "auth_test.go"),
	}

	got := resolveReadFilePath(task, "auth.go")
	want := filepath.Join(packageDir, "auth.go")
	if got != want {
		t.Fatalf("resolveReadFilePath() = %q, want %q", got, want)
	}
}

func TestResolveReadFilePathFallsBackToWorkingDirJoin(t *testing.T) {
	tmpDir := t.TempDir()
	task := cw.Task{WorkingDir: tmpDir}

	got := resolveReadFilePath(task, filepath.Join("tools", "sopbackend", "auth.go"))
	want := filepath.Join(tmpDir, "tools", "sopbackend", "auth.go")
	if got != want {
		t.Fatalf("resolveReadFilePath() = %q, want %q", got, want)
	}
}

func TestResolveListDirectoryPathUsesWorkingDirForEmptyPath(t *testing.T) {
	tmpDir := t.TempDir()
	task := cw.Task{WorkingDir: tmpDir}

	got := resolveListDirectoryPath(task, "")
	if got != tmpDir {
		t.Fatalf("resolveListDirectoryPath() = %q, want %q", got, tmpDir)
	}
}

func TestResolveListDirectoryPathJoinsRelativePathAgainstWorkingDir(t *testing.T) {
	tmpDir := t.TempDir()
	task := cw.Task{WorkingDir: tmpDir}

	got := resolveListDirectoryPath(task, filepath.Join("tools", "sopbackend"))
	want := filepath.Join(tmpDir, "tools", "sopbackend")
	if got != want {
		t.Fatalf("resolveListDirectoryPath() = %q, want %q", got, want)
	}
}

func TestResolveReadFilePathStripsRepeatedModuleRootPrefix(t *testing.T) {
	tmpDir := t.TempDir()
	workingDir := filepath.Join(tmpDir, "lib", "sop")
	if err := os.MkdirAll(filepath.Join(workingDir, "tools", "sopbackend"), 0o755); err != nil {
		t.Fatalf("mkdir nested package dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workingDir, "tools", "sopbackend", "auth.go"), []byte("package sopbackend\n"), 0o644); err != nil {
		t.Fatalf("write auth.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workingDir, "tools", "sopbackend", "auth_test.go"), []byte("package sopbackend\n"), 0o644); err != nil {
		t.Fatalf("write auth_test.go: %v", err)
	}

	task := cw.Task{
		WorkingDir: workingDir,
		ActiveFile: filepath.Join("lib", "sop", "tools", "sopbackend", "auth_test.go"),
	}

	got := resolveReadFilePath(task, "auth.go")
	want := filepath.Join(workingDir, "tools", "sopbackend", "auth.go")
	if got != want {
		t.Fatalf("resolveReadFilePath() with repeated module prefix = %q, want %q", got, want)
	}

	gotDir := resolveListDirectoryPath(task, filepath.Join("lib", "sop", "tools", "sopbackend"))
	wantDir := filepath.Join(workingDir, "tools", "sopbackend")
	if gotDir != wantDir {
		t.Fatalf("resolveListDirectoryPath() with repeated module prefix = %q, want %q", gotDir, wantDir)
	}
}

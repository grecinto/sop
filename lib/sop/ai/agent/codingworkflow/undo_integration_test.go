package codingworkflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnableUndoForSharedInfrastructureSeedsPatchEngine(t *testing.T) {
	infra := &SharedInfrastructure{}
	EnableUndoForSharedInfrastructure(infra, nil)
	if infra.PatchEngine == nil {
		t.Fatal("expected undo initialization to seed a patch engine")
	}
	if _, ok := infra.PatchEngine.(*UndoAwarePatchEngine); !ok {
		t.Fatalf("expected undo-aware wrapper, got %T", infra.PatchEngine)
	}
}

type recordingPatchEngine struct {
	ctx   context.Context
	task  Task
	patch *Patch
	state RunState
}

func (r *recordingPatchEngine) Apply(ctx context.Context, task Task, patch *Patch, state RunState) error {
	r.ctx = ctx
	r.task = task
	r.patch = patch
	r.state = state
	return nil
}

func TestUndoAwarePatchEnginePreservesCallerContext(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "tracked.txt"), []byte("before"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	spaceClient := NewMockSpaceClient()
	undoManager, err := NewUndoManager(tmpDir, "testuser", spaceClient, nil)
	if err != nil {
		t.Fatalf("NewUndoManager failed: %v", err)
	}
	if _, err := undoManager.BeginTransaction(context.Background(), "test undo wrapper", "implementation"); err != nil {
		t.Fatalf("BeginTransaction failed: %v", err)
	}

	engine := &recordingPatchEngine{}
	wrapper := NewUndoAwarePatchEngine(engine, undoManager)
	marker := struct{}{}
	ctx := context.WithValue(context.Background(), contextKey("marker"), marker)
	patch := &Patch{FilePath: "tracked.txt", NewContent: "after"}
	if err := wrapper.Apply(ctx, Task{WorkingDir: tmpDir, Description: "test"}, patch, RunState{}); err != nil {
		t.Fatalf("wrapper.Apply failed: %v", err)
	}
	if got := engine.ctx.Value(contextKey("marker")); got != marker {
		t.Fatalf("base patch engine received wrong context value: %#v", got)
	}
	if engine.patch != patch {
		t.Fatal("base patch engine did not receive the original patch")
	}
}

func TestUndoManagerBackupFileHonorsCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "tracked.txt"), []byte("before"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	spaceClient := NewMockSpaceClient()
	undoManager, err := NewUndoManager(tmpDir, "testuser", spaceClient, nil)
	if err != nil {
		t.Fatalf("NewUndoManager failed: %v", err)
	}
	if _, err := undoManager.BeginTransaction(context.Background(), "test undo backup", "implementation"); err != nil {
		t.Fatalf("BeginTransaction failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := undoManager.BackupFile(ctx, "tracked.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("BackupFile() error = %v, want context.Canceled", err)
	}
}

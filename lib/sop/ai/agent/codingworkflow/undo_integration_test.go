package codingworkflow

import "testing"

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

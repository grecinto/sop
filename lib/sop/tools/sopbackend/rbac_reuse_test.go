package sopbackend

import (
	"context"
	"testing"

	"github.com/sharedcode/sop"
)

func TestBackendInitializeRestoresBuiltinRBACBlueprints(t *testing.T) {
	sop.RegisterAssetRBAC(sop.AssetBlueprint{AssetType: "space", Description: "override"})
	sop.RegisterAssetRBAC(sop.AssetBlueprint{AssetType: "store", Description: "override"})

	backend := NewBackend(&Config{}, &ModelCatalog{})
	if err := backend.Initialize(context.Background()); err != nil {
		t.Fatalf("backend.Initialize() error = %v", err)
	}

	spaceBP, ok := sop.GetAssetBlueprint("space")
	if !ok {
		t.Fatal("expected space asset blueprint to be registered after backend initialization")
	}
	if len(spaceBP.Actions) == 0 {
		t.Fatal("expected builtin space RBAC actions after backend initialization")
	}

	storeBP, ok := sop.GetAssetBlueprint("store")
	if !ok {
		t.Fatal("expected store asset blueprint to be registered after backend initialization")
	}
	if len(storeBP.Actions) == 0 {
		t.Fatal("expected builtin store RBAC actions after backend initialization")
	}
}

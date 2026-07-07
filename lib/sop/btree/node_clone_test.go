package btree

import (
	"testing"

	"github.com/sharedcode/sop"
)

func TestNodeCloneCopiesSlotsAndValues(t *testing.T) {
	original := &Node[int, string]{
		ID:       sop.NewUUID(),
		ParentID: sop.NewUUID(),
		Slots: []Item[int, string]{
			{ID: sop.NewUUID(), Key: 1, Value: stringPtr("one"), Version: 1},
			{ID: sop.NewUUID(), Key: 2, Value: stringPtr("two"), Version: 2},
		},
		Count:       2,
		Version:     7,
		ChildrenIDs: []sop.UUID{sop.NewUUID(), sop.NewUUID()},
	}

	clone := original.Clone()
	if clone == nil {
		t.Fatal("expected clone to be non-nil")
	}
	if clone == original {
		t.Fatal("expected clone to be a distinct node")
	}

	clone.Slots[0].Key = 99
	clone.Slots[0].Value = stringPtr("changed")
	clone.ChildrenIDs[0] = sop.NilUUID

	if original.Slots[0].Key != 1 {
		t.Fatalf("expected original key to remain unchanged, got %d", original.Slots[0].Key)
	}
	if original.Slots[0].Value == nil || *original.Slots[0].Value != "one" {
		t.Fatalf("expected original value to remain unchanged, got %v", original.Slots[0].Value)
	}
	if original.ChildrenIDs[0] == sop.NilUUID {
		t.Fatalf("expected original children IDs to remain unchanged")
	}
}

func TestNodeCopyToCopiesSlotsAndChildrenWithoutAlias(t *testing.T) {
	original := &Node[int, string]{
		ID:       sop.NewUUID(),
		ParentID: sop.NewUUID(),
		Slots: []Item[int, string]{
			{ID: sop.NewUUID(), Key: 1, Value: stringPtr("one"), Version: 1},
		},
		Count:       1,
		Version:     7,
		ChildrenIDs: []sop.UUID{sop.NewUUID()},
	}

	target := &Node[int, string]{
		Slots:       make([]Item[int, string], 2),
		ChildrenIDs: make([]sop.UUID, 2),
	}

	if !original.CopyTo(target) {
		t.Fatal("expected CopyTo to succeed")
	}

	target.Slots[0].Key = 99
	target.Slots[0].Value = stringPtr("changed")
	target.ChildrenIDs[0] = sop.NilUUID

	if original.Slots[0].Key != 1 {
		t.Fatalf("expected original key to remain unchanged, got %d", original.Slots[0].Key)
	}
	if original.Slots[0].Value == nil || *original.Slots[0].Value != "one" {
		t.Fatalf("expected original value to remain unchanged, got %v", original.Slots[0].Value)
	}
	if original.ChildrenIDs[0] == sop.NilUUID {
		t.Fatalf("expected original child ID to remain unchanged")
	}
}

func stringPtr(v string) *string {
	return &v
}

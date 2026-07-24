package memory

import (
	"context"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/inmemory"
)

// TestEnsureCategoryHierarchy verifies that EnsureCategory creates complete hierarchical structures
func TestEnsureCategoryHierarchy(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory store
	categoriesTree := inmemory.NewBtree[sop.UUID, *Category](true)
	categoriesByPathTree := inmemory.NewBtree[string, sop.UUID](false)
	categoriesByDistanceTree := inmemory.NewBtree[DistanceKey, byte](false)
	vectorsTree := inmemory.NewBtree[VectorKey, Vector](false)
	itemsTree := inmemory.NewBtree[ItemKey, Item[string]](false)
	documentsTree := inmemory.NewBtree[sop.UUID, Document](false)

	store := NewStore[string](
		"test_kb",
		nil,
		categoriesTree.Btree,
		categoriesByPathTree.Btree,
		categoriesByDistanceTree.Btree,
		vectorsTree.Btree,
		itemsTree.Btree,
		documentsTree.Btree,
	)

	mgr := NewMemoryManager[string](store, nil, nil)

	// Test: Create deep category path
	deepPath := "Playbooks/TestDebugging/Go/UnitTests"
	leafID, err := mgr.EnsureCategory(ctx, deepPath)
	if err != nil {
		t.Fatalf("EnsureCategory failed: %v", err)
	}
	if leafID.IsNil() {
		t.Fatal("Expected non-nil leaf category ID")
	}

	// Verify all intermediate categories were created (preserves original casing)
	expectedPaths := []string{
		"Playbooks",
		"Playbooks/TestDebugging",
		"Playbooks/TestDebugging/Go",
		"Playbooks/TestDebugging/Go/UnitTests",
	}

	for _, path := range expectedPaths {
		catID, err := mgr.findCategoryByExactPath(ctx, path)
		if err != nil {
			t.Fatalf("Error finding category %s: %v", path, err)
		}
		if catID.IsNil() {
			t.Errorf("Expected category %s to exist, but it was not found", path)
		}
	}

	// Verify parent relationships
	cats, _ := store.Categories(ctx)

	// Check leaf has correct parent
	cats.Find(ctx, leafID, false)
	leafCat, _ := cats.GetCurrentValue(ctx)
	if len(leafCat.ParentIDs) != 1 {
		t.Errorf("Expected leaf to have 1 parent, got %d", len(leafCat.ParentIDs))
	}

	// Verify parent path is correct
	if len(leafCat.ParentIDs) > 0 {
		parentID := leafCat.ParentIDs[0].ParentID
		cats.Find(ctx, parentID, false)
		parentCat, _ := cats.GetCurrentValue(ctx)
		expectedParentPath := "Playbooks/TestDebugging/Go"
		if parentCat.Path != expectedParentPath {
			t.Errorf("Expected parent path %s, got %s", expectedParentPath, parentCat.Path)
		}
	}

	// Test idempotence: calling EnsureCategory again should return existing ID
	leafID2, err := mgr.EnsureCategory(ctx, deepPath)
	if err != nil {
		t.Fatalf("Second EnsureCategory call failed: %v", err)
	}
	if leafID2 != leafID {
		t.Errorf("Expected same leaf ID on second call, got different ID")
	}

	// Verify no duplicate categories were created
	catCount := 0
	ok, _ := cats.First(ctx)
	for ok {
		catCount++
		ok, _ = cats.Next(ctx)
	}
	if catCount != len(expectedPaths) {
		t.Errorf("Expected %d categories, got %d", len(expectedPaths), catCount)
	}
}

// TestEnsureCategorySimplePath verifies backward compatibility with simple (non-hierarchical) paths
func TestEnsureCategorySimplePath(t *testing.T) {
	ctx := context.Background()

	categoriesTree := inmemory.NewBtree[sop.UUID, *Category](true)
	categoriesByPathTree := inmemory.NewBtree[string, sop.UUID](false)
	categoriesByDistanceTree := inmemory.NewBtree[DistanceKey, byte](false)
	vectorsTree := inmemory.NewBtree[VectorKey, Vector](false)
	itemsTree := inmemory.NewBtree[ItemKey, Item[string]](false)
	documentsTree := inmemory.NewBtree[sop.UUID, Document](false)

	store := NewStore[string](
		"test_kb",
		nil,
		categoriesTree.Btree,
		categoriesByPathTree.Btree,
		categoriesByDistanceTree.Btree,
		vectorsTree.Btree,
		itemsTree.Btree,
		documentsTree.Btree,
	)

	mgr := NewMemoryManager[string](store, nil, nil)

	// Test: Simple single-level category (preserves original casing)
	simplePath := "SimpleCategory"
	catID, err := mgr.EnsureCategory(ctx, simplePath)
	if err != nil {
		t.Fatalf("EnsureCategory failed for simple path: %v", err)
	}
	if catID.IsNil() {
		t.Fatal("Expected non-nil category ID")
	}

	// Verify category was created with original case preserved
	cats, _ := store.Categories(ctx)
	cats.Find(ctx, catID, false)
	cat, _ := cats.GetCurrentValue(ctx)

	expectedName := "SimpleCategory"
	if cat.Name != expectedName {
		t.Errorf("Expected name %s, got %s", expectedName, cat.Name)
	}
	if cat.Path != expectedName {
		t.Errorf("Expected path %s, got %s", expectedName, cat.Path)
	}
	if len(cat.ParentIDs) != 0 {
		t.Errorf("Expected no parents for root category, got %d", len(cat.ParentIDs))
	}
}

// TestEnsureCategoryEmptyPath verifies error handling for empty paths
func TestEnsureCategoryEmptyPath(t *testing.T) {
	ctx := context.Background()

	categoriesTree := inmemory.NewBtree[sop.UUID, *Category](true)
	categoriesByPathTree := inmemory.NewBtree[string, sop.UUID](false)
	categoriesByDistanceTree := inmemory.NewBtree[DistanceKey, byte](false)
	vectorsTree := inmemory.NewBtree[VectorKey, Vector](false)
	itemsTree := inmemory.NewBtree[ItemKey, Item[string]](false)
	documentsTree := inmemory.NewBtree[sop.UUID, Document](false)

	store := NewStore[string](
		"test_kb",
		nil,
		categoriesTree.Btree,
		categoriesByPathTree.Btree,
		categoriesByDistanceTree.Btree,
		vectorsTree.Btree,
		itemsTree.Btree,
		documentsTree.Btree,
	)

	mgr := NewMemoryManager[string](store, nil, nil)

	// Test: Empty path should return error
	_, err := mgr.EnsureCategory(ctx, "")
	if err == nil {
		t.Error("Expected error for empty path, got nil")
	}

	// Test: Whitespace-only path should return error
	_, err = mgr.EnsureCategory(ctx, "   ")
	if err == nil {
		t.Error("Expected error for whitespace path, got nil")
	}
}

// TestEnsureCategoryCaseInsensitive verifies case-insensitive lookups with original case preservation
func TestEnsureCategoryCaseInsensitive(t *testing.T) {
	ctx := context.Background()

	categoriesTree := inmemory.NewBtree[sop.UUID, *Category](true)
	categoriesByPathTree := inmemory.NewBtree[string, sop.UUID](false)
	categoriesByDistanceTree := inmemory.NewBtree[DistanceKey, byte](false)
	vectorsTree := inmemory.NewBtree[VectorKey, Vector](false)
	itemsTree := inmemory.NewBtree[ItemKey, Item[string]](false)
	documentsTree := inmemory.NewBtree[sop.UUID, Document](false)

	store := NewStore[string](
		"test_kb",
		nil,
		categoriesTree.Btree,
		categoriesByPathTree.Btree,
		categoriesByDistanceTree.Btree,
		vectorsTree.Btree,
		itemsTree.Btree,
		documentsTree.Btree,
	)

	mgr := NewMemoryManager[string](store, nil, nil)

	// Test: First call establishes the canonical casing (Title Case)
	path1 := "Coding Best Practices/Go/Error Handling"
	id1, err := mgr.EnsureCategory(ctx, path1)
	if err != nil {
		t.Fatalf("EnsureCategory failed: %v", err)
	}

	// Verify the stored casing matches the first version
	cats, _ := store.Categories(ctx)
	cats.Find(ctx, id1, false)
	cat, _ := cats.GetCurrentValue(ctx)
	if cat.Path != path1 {
		t.Errorf("Expected path %s, got %s", path1, cat.Path)
	}

	// Test: Same path with different casing returns same ID
	path2 := "coding best practices/go/error handling"
	id2, err := mgr.EnsureCategory(ctx, path2)
	if err != nil {
		t.Fatalf("EnsureCategory failed for lowercase path: %v", err)
	}
	if id1 != id2 {
		t.Errorf("Expected same ID for case-insensitive match, got %s and %s", id1, id2)
	}

	// Test: All-caps version also returns same ID
	path3 := "CODING BEST PRACTICES/GO/ERROR HANDLING"
	id3, err := mgr.EnsureCategory(ctx, path3)
	if err != nil {
		t.Fatalf("EnsureCategory failed for uppercase path: %v", err)
	}
	if id1 != id3 {
		t.Errorf("Expected same ID for case-insensitive match, got %s and %s", id1, id3)
	}

	// Verify stored path still has original Title Case from first call
	cats.Find(ctx, id3, false)
	cat, _ = cats.GetCurrentValue(ctx)
	if cat.Path != path1 {
		t.Errorf("Expected path to remain %s (first version), got %s", path1, cat.Path)
	}

	// Verify no duplicate categories were created
	catCount := 0
	ok, _ := cats.First(ctx)
	for ok {
		catCount++
		ok, _ = cats.Next(ctx)
	}
	// Should have 3 categories: "Coding Best Practices", "Coding Best Practices/Go", "Coding Best Practices/Go/Error Handling"
	expectedCount := 3
	if catCount != expectedCount {
		t.Errorf("Expected %d categories (no duplicates), got %d", expectedCount, catCount)
	}
}

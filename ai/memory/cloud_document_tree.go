package memory

import (
	"context"
	"fmt"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/btree"
)

// CloudDocumentTree is a document-tree implementation for reference-style documents
// that are stored in an external cloud backend. It keeps the document metadata in the
// main document tree while exposing chunk metadata and chunk access through the same
// interface as the other document-tree backends.
//
// The initial implementation is intentionally lightweight and only models the metadata
// semantics needed for Apple Cloud and similar reference-backed documents.
type CloudDocumentTree struct {
	inner DocumentTree
}

// NewCloudDocumentTree wraps an existing document tree with cloud-reference backed chunk access.
func NewCloudDocumentTree(inner DocumentTree) *CloudDocumentTree {
	if inner == nil {
		return &CloudDocumentTree{}
	}
	return &CloudDocumentTree{inner: inner}
}

func (c *CloudDocumentTree) Add(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	if c.inner == nil {
		return false, fmt.Errorf("cloud document tree has no underlying document store")
	}
	return c.inner.Add(ctx, key, value)
}

func (c *CloudDocumentTree) AddIfNotExist(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	if c.inner == nil {
		return false, fmt.Errorf("cloud document tree has no underlying document store")
	}
	return c.inner.AddIfNotExist(ctx, key, value)
}

func (c *CloudDocumentTree) Update(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	if c.inner == nil {
		return false, fmt.Errorf("cloud document tree has no underlying document store")
	}
	return c.inner.Update(ctx, key, value)
}

func (c *CloudDocumentTree) UpdateKey(ctx context.Context, key sop.UUID) (bool, error) {
	if c.inner == nil {
		return false, fmt.Errorf("cloud document tree has no underlying document store")
	}
	return c.inner.UpdateKey(ctx, key)
}

func (c *CloudDocumentTree) UpdateCurrentKey(ctx context.Context, key sop.UUID) (bool, error) {
	if c.inner == nil {
		return false, fmt.Errorf("cloud document tree has no underlying document store")
	}
	return c.inner.UpdateCurrentKey(ctx, key)
}

func (c *CloudDocumentTree) UpdateCurrentValue(ctx context.Context, newValue Document) (bool, error) {
	if c.inner == nil {
		return false, fmt.Errorf("cloud document tree has no underlying document store")
	}
	return c.inner.UpdateCurrentValue(ctx, newValue)
}

func (c *CloudDocumentTree) UpdateCurrentItem(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	if c.inner == nil {
		return false, fmt.Errorf("cloud document tree has no underlying document store")
	}
	return c.inner.UpdateCurrentItem(ctx, key, value)
}

func (c *CloudDocumentTree) Upsert(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	if c.inner == nil {
		return false, fmt.Errorf("cloud document tree has no underlying document store")
	}
	return c.inner.Upsert(ctx, key, value)
}

func (c *CloudDocumentTree) Remove(ctx context.Context, key sop.UUID) (bool, error) {
	if c.inner == nil {
		return false, fmt.Errorf("cloud document tree has no underlying document store")
	}
	return c.inner.Remove(ctx, key)
}

func (c *CloudDocumentTree) RemoveCurrentItem(ctx context.Context) (bool, error) {
	if c.inner == nil {
		return false, fmt.Errorf("cloud document tree has no underlying document store")
	}
	return c.inner.RemoveCurrentItem(ctx)
}

func (c *CloudDocumentTree) Find(ctx context.Context, key sop.UUID, firstItemWithKey bool) (bool, error) {
	if c.inner == nil {
		return false, nil
	}
	return c.inner.Find(ctx, key, firstItemWithKey)
}

func (c *CloudDocumentTree) FindWithID(ctx context.Context, key sop.UUID, id sop.UUID) (bool, error) {
	if c.inner == nil {
		return false, nil
	}
	return c.inner.FindWithID(ctx, key, id)
}

func (c *CloudDocumentTree) FindInDescendingOrder(ctx context.Context, key sop.UUID) (bool, error) {
	if c.inner == nil {
		return false, nil
	}
	return c.inner.FindInDescendingOrder(ctx, key)
}

func (c *CloudDocumentTree) GetCurrentKey() btree.Item[sop.UUID, Document] {
	if c.inner == nil {
		return btree.Item[sop.UUID, Document]{}
	}
	return c.inner.GetCurrentKey()
}

func (c *CloudDocumentTree) GetCurrentValue(ctx context.Context) (Document, error) {
	if c.inner == nil {
		return Document{}, nil
	}
	return c.inner.GetCurrentValue(ctx)
}

func (c *CloudDocumentTree) GetCurrentValueNoLock(ctx context.Context) (Document, error) {
	if c.inner == nil {
		return Document{}, nil
	}
	return c.inner.GetCurrentValueNoLock(ctx)
}

func (c *CloudDocumentTree) GetCurrentItem(ctx context.Context) (btree.Item[sop.UUID, Document], error) {
	if c.inner == nil {
		return btree.Item[sop.UUID, Document]{}, nil
	}
	return c.inner.GetCurrentItem(ctx)
}

func (c *CloudDocumentTree) GetCurrentItemNoLock(ctx context.Context) (btree.Item[sop.UUID, Document], error) {
	if c.inner == nil {
		return btree.Item[sop.UUID, Document]{}, nil
	}
	return c.inner.GetCurrentItemNoLock(ctx)
}

func (c *CloudDocumentTree) RLockCurrentItem(ctx context.Context) error {
	if c.inner == nil {
		return nil
	}
	return c.inner.RLockCurrentItem(ctx)
}

func (c *CloudDocumentTree) First(ctx context.Context) (bool, error) {
	if c.inner == nil {
		return false, nil
	}
	return c.inner.First(ctx)
}

func (c *CloudDocumentTree) Last(ctx context.Context) (bool, error) {
	if c.inner == nil {
		return false, nil
	}
	return c.inner.Last(ctx)
}

func (c *CloudDocumentTree) Next(ctx context.Context) (bool, error) {
	if c.inner == nil {
		return false, nil
	}
	return c.inner.Next(ctx)
}

func (c *CloudDocumentTree) Previous(ctx context.Context) (bool, error) {
	if c.inner == nil {
		return false, nil
	}
	return c.inner.Previous(ctx)
}

func (c *CloudDocumentTree) IsUnique() bool {
	if c.inner == nil {
		return true
	}
	return c.inner.IsUnique()
}

func (c *CloudDocumentTree) Count() int64 {
	if c.inner == nil {
		return 0
	}
	return c.inner.Count()
}

func (c *CloudDocumentTree) GetStoreInfo() *sop.StoreInfo {
	if c.inner == nil {
		return &sop.StoreInfo{}
	}
	return c.inner.GetStoreInfo()
}

// GetDocumentChunkMetadata returns a single metadata item for cloud-backed documents,
// because the payload is treated as an external object rather than a local chunk set.
func (c *CloudDocumentTree) GetDocumentChunkMetadata(key sop.UUID) ([]DocumentChunkMetadata, error) {
	ctx := context.Background()
	if c.inner == nil {
		return nil, fmt.Errorf("cloud document tree has no underlying document store")
	}
	found, err := c.inner.Find(ctx, key, false)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	doc, err := c.inner.GetCurrentValue(ctx)
	if err != nil {
		return nil, err
	}
	if doc.PayloadRef == "" && doc.CloudSource.ObjectKey == "" {
		return nil, nil
	}
	return []DocumentChunkMetadata{{Index: 0, Size: 0}}, nil
}

// GetDocumentChunk returns nil for cloud-backed documents because the actual payload is
// resolved by the cloud provider rather than through this adapter.
func (c *CloudDocumentTree) GetDocumentChunk(key sop.UUID, chunkIndex int) ([]byte, error) {
	ctx := context.Background()
	if c.inner == nil {
		return nil, fmt.Errorf("cloud document tree has no underlying document store")
	}
	found, err := c.inner.Find(ctx, key, false)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	doc, err := c.inner.GetCurrentValue(ctx)
	if err != nil {
		return nil, err
	}
	if doc.PayloadRef == "" && doc.CloudSource.ObjectKey == "" {
		return nil, nil
	}
	return nil, nil
}

var _ btree.BtreeInterface[sop.UUID, Document] = (*CloudDocumentTree)(nil)

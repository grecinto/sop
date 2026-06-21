package memory

import (
	"context"
	"fmt"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/btree"
)

// documentBackendTree routes document chunk access to the appropriate backend based on
// the document's storage mode while keeping the canonical document record in the main tree.
type documentBackendTree struct {
	inner DocumentTree
	fs    *FilesystemDocumentTree
	cloud *CloudDocumentTree
}

// NewDocumentBackendTree creates a DocumentTree that dispatches chunk access to the
// appropriate backend implementation for the current document storage mode.
func NewDocumentBackendTree(inner DocumentTree) DocumentTree {
	if inner == nil {
		return nil
	}
	return &documentBackendTree{
		inner: inner,
		fs:    NewFilesystemDocumentTree(inner),
		cloud: NewCloudDocumentTree(inner),
	}
}

func (d *documentBackendTree) Add(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	if d.inner == nil {
		return false, fmt.Errorf("document backend tree has no underlying document store")
	}
	return d.inner.Add(ctx, key, value)
}

func (d *documentBackendTree) AddIfNotExist(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	if d.inner == nil {
		return false, fmt.Errorf("document backend tree has no underlying document store")
	}
	return d.inner.AddIfNotExist(ctx, key, value)
}

func (d *documentBackendTree) Update(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	if d.inner == nil {
		return false, fmt.Errorf("document backend tree has no underlying document store")
	}
	return d.inner.Update(ctx, key, value)
}

func (d *documentBackendTree) UpdateKey(ctx context.Context, key sop.UUID) (bool, error) {
	if d.inner == nil {
		return false, fmt.Errorf("document backend tree has no underlying document store")
	}
	return d.inner.UpdateKey(ctx, key)
}

func (d *documentBackendTree) UpdateCurrentKey(ctx context.Context, key sop.UUID) (bool, error) {
	if d.inner == nil {
		return false, fmt.Errorf("document backend tree has no underlying document store")
	}
	return d.inner.UpdateCurrentKey(ctx, key)
}

func (d *documentBackendTree) UpdateCurrentValue(ctx context.Context, newValue Document) (bool, error) {
	if d.inner == nil {
		return false, fmt.Errorf("document backend tree has no underlying document store")
	}
	return d.inner.UpdateCurrentValue(ctx, newValue)
}

func (d *documentBackendTree) UpdateCurrentItem(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	if d.inner == nil {
		return false, fmt.Errorf("document backend tree has no underlying document store")
	}
	return d.inner.UpdateCurrentItem(ctx, key, value)
}

func (d *documentBackendTree) Upsert(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	if d.inner == nil {
		return false, fmt.Errorf("document backend tree has no underlying document store")
	}
	return d.inner.Upsert(ctx, key, value)
}

func (d *documentBackendTree) Remove(ctx context.Context, key sop.UUID) (bool, error) {
	if d.inner == nil {
		return false, fmt.Errorf("document backend tree has no underlying document store")
	}
	return d.inner.Remove(ctx, key)
}

func (d *documentBackendTree) RemoveCurrentItem(ctx context.Context) (bool, error) {
	if d.inner == nil {
		return false, fmt.Errorf("document backend tree has no underlying document store")
	}
	return d.inner.RemoveCurrentItem(ctx)
}

func (d *documentBackendTree) Find(ctx context.Context, key sop.UUID, firstItemWithKey bool) (bool, error) {
	if d.inner == nil {
		return false, nil
	}
	return d.inner.Find(ctx, key, firstItemWithKey)
}

func (d *documentBackendTree) FindWithID(ctx context.Context, key sop.UUID, id sop.UUID) (bool, error) {
	if d.inner == nil {
		return false, nil
	}
	return d.inner.FindWithID(ctx, key, id)
}

func (d *documentBackendTree) FindInDescendingOrder(ctx context.Context, key sop.UUID) (bool, error) {
	if d.inner == nil {
		return false, nil
	}
	return d.inner.FindInDescendingOrder(ctx, key)
}

func (d *documentBackendTree) GetCurrentKey() btree.Item[sop.UUID, Document] {
	if d.inner == nil {
		return btree.Item[sop.UUID, Document]{}
	}
	return d.inner.GetCurrentKey()
}

func (d *documentBackendTree) GetCurrentValue(ctx context.Context) (Document, error) {
	if d.inner == nil {
		return Document{}, nil
	}
	return d.inner.GetCurrentValue(ctx)
}

func (d *documentBackendTree) GetCurrentValueNoLock(ctx context.Context) (Document, error) {
	if d.inner == nil {
		return Document{}, nil
	}
	return d.inner.GetCurrentValueNoLock(ctx)
}

func (d *documentBackendTree) GetCurrentItem(ctx context.Context) (btree.Item[sop.UUID, Document], error) {
	if d.inner == nil {
		return btree.Item[sop.UUID, Document]{}, nil
	}
	return d.inner.GetCurrentItem(ctx)
}

func (d *documentBackendTree) GetCurrentItemNoLock(ctx context.Context) (btree.Item[sop.UUID, Document], error) {
	if d.inner == nil {
		return btree.Item[sop.UUID, Document]{}, nil
	}
	return d.inner.GetCurrentItemNoLock(ctx)
}

func (d *documentBackendTree) RLockCurrentItem(ctx context.Context) error {
	if d.inner == nil {
		return nil
	}
	return d.inner.RLockCurrentItem(ctx)
}

func (d *documentBackendTree) First(ctx context.Context) (bool, error) {
	if d.inner == nil {
		return false, nil
	}
	return d.inner.First(ctx)
}

func (d *documentBackendTree) Last(ctx context.Context) (bool, error) {
	if d.inner == nil {
		return false, nil
	}
	return d.inner.Last(ctx)
}

func (d *documentBackendTree) Next(ctx context.Context) (bool, error) {
	if d.inner == nil {
		return false, nil
	}
	return d.inner.Next(ctx)
}

func (d *documentBackendTree) Previous(ctx context.Context) (bool, error) {
	if d.inner == nil {
		return false, nil
	}
	return d.inner.Previous(ctx)
}

func (d *documentBackendTree) IsUnique() bool {
	if d.inner == nil {
		return true
	}
	return d.inner.IsUnique()
}

func (d *documentBackendTree) Count() int64 {
	if d.inner == nil {
		return 0
	}
	return d.inner.Count()
}

func (d *documentBackendTree) GetStoreInfo() sop.StoreInfo {
	if d.inner == nil {
		return sop.StoreInfo{}
	}
	return d.inner.GetStoreInfo()
}

func (d *documentBackendTree) GetDocumentChunkMetadata(key sop.UUID) ([]DocumentChunkMetadata, error) {
	ctx := context.Background()
	if d.inner == nil {
		return nil, fmt.Errorf("document backend tree has no underlying document store")
	}
	found, err := d.inner.Find(ctx, key, false)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	doc, err := d.inner.GetCurrentValue(ctx)
	if err != nil {
		return nil, err
	}
	switch {
	case doc.IsFilesystemReference():
		if d.fs == nil {
			return nil, nil
		}
		return d.fs.GetDocumentChunkMetadata(key)
	case doc.IsAppleCloud() || doc.IsExternalMetadataOnly():
		if d.cloud == nil {
			return nil, nil
		}
		return d.cloud.GetDocumentChunkMetadata(key)
	default:
		return d.inner.GetDocumentChunkMetadata(key)
	}
}

func (d *documentBackendTree) GetDocumentChunk(key sop.UUID, chunkIndex int) ([]byte, error) {
	ctx := context.Background()
	if d.inner == nil {
		return nil, fmt.Errorf("document backend tree has no underlying document store")
	}
	found, err := d.inner.Find(ctx, key, false)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	doc, err := d.inner.GetCurrentValue(ctx)
	if err != nil {
		return nil, err
	}
	switch {
	case doc.IsFilesystemReference():
		if d.fs == nil {
			return nil, nil
		}
		return d.fs.GetDocumentChunk(key, chunkIndex)
	case doc.IsAppleCloud() || doc.IsExternalMetadataOnly():
		if d.cloud == nil {
			return nil, nil
		}
		return d.cloud.GetDocumentChunk(key, chunkIndex)
	default:
		return d.inner.GetDocumentChunk(key, chunkIndex)
	}
}

var _ btree.BtreeInterface[sop.UUID, Document] = (*documentBackendTree)(nil)

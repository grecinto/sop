package memory

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/btree"
)

const filesystemDocumentChunkSize = 4096

// FilesystemDocumentTree is a document-tree implementation that keeps a document
// record in the main document store while reading the payload bytes from a filesystem
// path when chunk data is requested. This is distinct from the B-tree-backed streaming
// document store and is intended to support reference-style documents backed by
// ordinary files and later cloud object stores through a common adapter interface.
type FilesystemDocumentTree struct {
	inner DocumentTree
}

// NewFilesystemDocumentTree wraps an existing document tree with filesystem-backed chunk access.
func NewFilesystemDocumentTree(inner DocumentTree) *FilesystemDocumentTree {
	if inner == nil {
		return &FilesystemDocumentTree{}
	}
	return &FilesystemDocumentTree{inner: inner}
}

func (f *FilesystemDocumentTree) Add(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	if f.inner == nil {
		return false, fmt.Errorf("filesystem document tree has no underlying document store")
	}
	return f.inner.Add(ctx, key, value)
}

func (f *FilesystemDocumentTree) AddIfNotExist(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	if f.inner == nil {
		return false, fmt.Errorf("filesystem document tree has no underlying document store")
	}
	return f.inner.AddIfNotExist(ctx, key, value)
}

func (f *FilesystemDocumentTree) Update(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	if f.inner == nil {
		return false, fmt.Errorf("filesystem document tree has no underlying document store")
	}
	return f.inner.Update(ctx, key, value)
}

func (f *FilesystemDocumentTree) UpdateKey(ctx context.Context, key sop.UUID) (bool, error) {
	if f.inner == nil {
		return false, fmt.Errorf("filesystem document tree has no underlying document store")
	}
	return f.inner.UpdateKey(ctx, key)
}

func (f *FilesystemDocumentTree) UpdateCurrentKey(ctx context.Context, key sop.UUID) (bool, error) {
	if f.inner == nil {
		return false, fmt.Errorf("filesystem document tree has no underlying document store")
	}
	return f.inner.UpdateCurrentKey(ctx, key)
}

func (f *FilesystemDocumentTree) UpdateCurrentValue(ctx context.Context, newValue Document) (bool, error) {
	if f.inner == nil {
		return false, fmt.Errorf("filesystem document tree has no underlying document store")
	}
	return f.inner.UpdateCurrentValue(ctx, newValue)
}

func (f *FilesystemDocumentTree) UpdateCurrentItem(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	if f.inner == nil {
		return false, fmt.Errorf("filesystem document tree has no underlying document store")
	}
	return f.inner.UpdateCurrentItem(ctx, key, value)
}

func (f *FilesystemDocumentTree) Upsert(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	if f.inner == nil {
		return false, fmt.Errorf("filesystem document tree has no underlying document store")
	}
	return f.inner.Upsert(ctx, key, value)
}

func (f *FilesystemDocumentTree) Remove(ctx context.Context, key sop.UUID) (bool, error) {
	if f.inner == nil {
		return false, fmt.Errorf("filesystem document tree has no underlying document store")
	}
	return f.inner.Remove(ctx, key)
}

func (f *FilesystemDocumentTree) RemoveCurrentItem(ctx context.Context) (bool, error) {
	if f.inner == nil {
		return false, fmt.Errorf("filesystem document tree has no underlying document store")
	}
	return f.inner.RemoveCurrentItem(ctx)
}

func (f *FilesystemDocumentTree) Find(ctx context.Context, key sop.UUID, firstItemWithKey bool) (bool, error) {
	if f.inner == nil {
		return false, nil
	}
	return f.inner.Find(ctx, key, firstItemWithKey)
}

func (f *FilesystemDocumentTree) FindWithID(ctx context.Context, key sop.UUID, id sop.UUID) (bool, error) {
	if f.inner == nil {
		return false, nil
	}
	return f.inner.FindWithID(ctx, key, id)
}

func (f *FilesystemDocumentTree) FindInDescendingOrder(ctx context.Context, key sop.UUID) (bool, error) {
	if f.inner == nil {
		return false, nil
	}
	return f.inner.FindInDescendingOrder(ctx, key)
}

func (f *FilesystemDocumentTree) GetCurrentKey() btree.Item[sop.UUID, Document] {
	if f.inner == nil {
		return btree.Item[sop.UUID, Document]{}
	}
	return f.inner.GetCurrentKey()
}

func (f *FilesystemDocumentTree) GetCurrentValue(ctx context.Context) (Document, error) {
	if f.inner == nil {
		return Document{}, nil
	}
	return f.inner.GetCurrentValue(ctx)
}

func (f *FilesystemDocumentTree) GetCurrentValueNoLock(ctx context.Context) (Document, error) {
	if f.inner == nil {
		return Document{}, nil
	}
	return f.inner.GetCurrentValueNoLock(ctx)
}

func (f *FilesystemDocumentTree) GetCurrentItem(ctx context.Context) (btree.Item[sop.UUID, Document], error) {
	if f.inner == nil {
		return btree.Item[sop.UUID, Document]{}, nil
	}
	return f.inner.GetCurrentItem(ctx)
}

func (f *FilesystemDocumentTree) GetCurrentItemNoLock(ctx context.Context) (btree.Item[sop.UUID, Document], error) {
	if f.inner == nil {
		return btree.Item[sop.UUID, Document]{}, nil
	}
	return f.inner.GetCurrentItemNoLock(ctx)
}

func (f *FilesystemDocumentTree) RLockCurrentItem(ctx context.Context) error {
	if f.inner == nil {
		return nil
	}
	return f.inner.RLockCurrentItem(ctx)
}

func (f *FilesystemDocumentTree) First(ctx context.Context) (bool, error) {
	if f.inner == nil {
		return false, nil
	}
	return f.inner.First(ctx)
}

func (f *FilesystemDocumentTree) Last(ctx context.Context) (bool, error) {
	if f.inner == nil {
		return false, nil
	}
	return f.inner.Last(ctx)
}

func (f *FilesystemDocumentTree) Next(ctx context.Context) (bool, error) {
	if f.inner == nil {
		return false, nil
	}
	return f.inner.Next(ctx)
}

func (f *FilesystemDocumentTree) Previous(ctx context.Context) (bool, error) {
	if f.inner == nil {
		return false, nil
	}
	return f.inner.Previous(ctx)
}

func (f *FilesystemDocumentTree) IsUnique() bool {
	if f.inner == nil {
		return true
	}
	return f.inner.IsUnique()
}

func (f *FilesystemDocumentTree) Count() int64 {
	if f.inner == nil {
		return 0
	}
	return f.inner.Count()
}

func (f *FilesystemDocumentTree) GetStoreInfo() *sop.StoreInfo {
	if f.inner == nil {
		return &sop.StoreInfo{}
	}
	return f.inner.GetStoreInfo()
}

func (f *FilesystemDocumentTree) GetDocumentChunkMetadata(key sop.UUID) ([]DocumentChunkMetadata, error) {
	ctx := context.Background()
	if f.inner == nil {
		return nil, fmt.Errorf("filesystem document tree has no underlying document store")
	}
	found, err := f.inner.Find(ctx, key, false)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	doc, err := f.inner.GetCurrentValue(ctx)
	if err != nil {
		return nil, err
	}
	if doc.PayloadRef == "" {
		return nil, nil
	}

	filePath := doc.PayloadRef
	if !filepath.IsAbs(filePath) {
		var absErr error
		filePath, absErr = filepath.Abs(filePath)
		if absErr != nil {
			return nil, absErr
		}
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}
	if info.Size() <= filesystemDocumentChunkSize {
		return []DocumentChunkMetadata{{Index: 0, Size: int(info.Size())}}, nil
	}

	chunkCount := int((info.Size() + int64(filesystemDocumentChunkSize) - 1) / int64(filesystemDocumentChunkSize))
	metadata := make([]DocumentChunkMetadata, 0, chunkCount)
	for i := 0; i < chunkCount; i++ {
		metadata = append(metadata, DocumentChunkMetadata{Index: i, Size: filesystemDocumentChunkSize})
	}
	metadata[chunkCount-1].Size = int(info.Size() - int64((chunkCount-1)*filesystemDocumentChunkSize))
	return metadata, nil
}

func (f *FilesystemDocumentTree) GetDocumentChunk(key sop.UUID, chunkIndex int) ([]byte, error) {
	ctx := context.Background()
	if f.inner == nil {
		return nil, fmt.Errorf("filesystem document tree has no underlying document store")
	}
	found, err := f.inner.Find(ctx, key, false)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	doc, err := f.inner.GetCurrentValue(ctx)
	if err != nil {
		return nil, err
	}
	if doc.PayloadRef == "" {
		return nil, nil
	}

	filePath := doc.PayloadRef
	if !filepath.IsAbs(filePath) {
		var absErr error
		filePath, absErr = filepath.Abs(filePath)
		if absErr != nil {
			return nil, absErr
		}
	}
	fh, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer fh.Close()

	offset := int64(chunkIndex) * filesystemDocumentChunkSize
	if _, err := fh.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	buf := make([]byte, filesystemDocumentChunkSize)
	n, err := fh.Read(buf)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:n], nil
}

var _ btree.BtreeInterface[sop.UUID, Document] = (*FilesystemDocumentTree)(nil)

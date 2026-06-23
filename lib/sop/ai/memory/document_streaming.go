package memory

import (
	"context"
	"fmt"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/btree"
	sd "github.com/sharedcode/sop/streamingdata"
)

// DocumentChunkMetadata describes the location of a chunk within a streamed document.
type DocumentChunkMetadata struct {
	Index int `json:"index"`
	Size  int `json:"size"`
}

// streamingDocumentBtree persists Document values as chunked JSON using the
// generic streaming-data store. This lets large document payloads be split into
// many ordered chunks that are stored in the underlying B-tree and can be
// reconstructed incrementally.
type streamingDocumentBtree struct {
	store     *sd.StreamingDataStore[sop.UUID]
	chunkSize int
}

const DefaultDocumentChunkSize = 1024 * 1024

// NewStreamingDocumentBtree wraps an underlying chunked B-tree so Document values
// are persisted as streamed JSON payloads instead of single large in-memory values.
func NewStreamingDocumentBtree(inner btree.BtreeInterface[sd.StreamingDataKey[sop.UUID], []byte], chunkSize ...int) DocumentTree {
	size := DefaultDocumentChunkSize
	if len(chunkSize) > 0 && chunkSize[0] > 0 {
		size = chunkSize[0]
	}
	return &streamingDocumentBtree{
		store:     &sd.StreamingDataStore[sop.UUID]{BtreeInterface: inner},
		chunkSize: size,
	}
}

// DocumentChunkSizeForErasureConfig returns a chunk size tuned for the database's
// erasure coding topology. More shards (more disks) use larger chunks; fewer shards
// stay at the baseline 1MB minimum.
func DocumentChunkSizeForErasureConfig(erasureConfig map[string]sop.ErasureCodingConfig) int {
	if len(erasureConfig) == 0 {
		return DefaultDocumentChunkSize
	}
	maxShards := 0
	for _, cfg := range erasureConfig {
		totalShards := cfg.DataShardsCount + cfg.ParityShardsCount
		if totalShards > maxShards {
			maxShards = totalShards
		}
	}
	switch {
	case maxShards <= 3:
		return DefaultDocumentChunkSize
	case maxShards <= 6:
		return 2 * DefaultDocumentChunkSize
	default:
		return 4 * DefaultDocumentChunkSize
	}
}

func (s *streamingDocumentBtree) Add(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	if key.IsNil() {
		key = sop.NewUUID()
	}
	if found, err := s.store.FindOne(ctx, key); err != nil {
		return false, err
	} else if found {
		return false, nil
	}
	enc, err := s.store.Add(ctx, key)
	if err != nil {
		return false, err
	}
	if err := enc.Encode(value); err != nil {
		return false, err
	}
	return true, nil
}

func (s *streamingDocumentBtree) AddIfNotExist(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	if key.IsNil() {
		key = sop.NewUUID()
	}
	if found, err := s.store.FindOne(ctx, key); err != nil {
		return false, err
	} else if found {
		return false, nil
	}
	return s.Add(ctx, key, value)
}

func (s *streamingDocumentBtree) Update(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	if key.IsNil() {
		return false, nil
	}
	if found, err := s.store.FindOne(ctx, key); err != nil {
		return false, err
	} else if !found {
		return false, nil
	}
	enc, err := s.store.Update(ctx, key)
	if err != nil {
		return false, err
	}
	if err := enc.Encode(value); err != nil {
		return false, err
	}
	if err := enc.Close(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *streamingDocumentBtree) UpdateKey(ctx context.Context, key sop.UUID) (bool, error) {
	return false, fmt.Errorf("streaming document tree does not support key rewrites")
}

func (s *streamingDocumentBtree) UpdateCurrentKey(ctx context.Context, key sop.UUID) (bool, error) {
	return false, fmt.Errorf("streaming document tree does not support key rewrites")
}

func (s *streamingDocumentBtree) UpdateCurrentValue(ctx context.Context, newValue Document) (bool, error) {
	enc, err := s.store.UpdateCurrentValue(ctx)
	if err != nil {
		return false, err
	}
	if err := enc.Encode(newValue); err != nil {
		return false, err
	}
	if err := enc.Close(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *streamingDocumentBtree) UpdateCurrentItem(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	if s.store.BtreeInterface.Count() == 0 {
		return false, nil
	}
	if found, err := s.store.FindOne(ctx, key); err != nil {
		return false, err
	} else if !found {
		return false, nil
	}
	return s.UpdateCurrentValue(ctx, value)
}

func (s *streamingDocumentBtree) Upsert(ctx context.Context, key sop.UUID, value Document) (bool, error) {
	enc, err := s.store.Upsert(ctx, key)
	if err != nil {
		return false, err
	}
	if err := enc.Encode(value); err != nil {
		return false, err
	}
	if err := enc.Close(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *streamingDocumentBtree) Remove(ctx context.Context, key sop.UUID) (bool, error) {
	return s.store.Remove(ctx, key)
}

func (s *streamingDocumentBtree) RemoveCurrentItem(ctx context.Context) (bool, error) {
	return s.store.RemoveCurrentItem(ctx)
}

func (s *streamingDocumentBtree) Find(ctx context.Context, key sop.UUID, firstItemWithKey bool) (bool, error) {
	return s.store.FindOne(ctx, key)
}

func (s *streamingDocumentBtree) FindWithID(ctx context.Context, key sop.UUID, id sop.UUID) (bool, error) {
	return s.Find(ctx, key, false)
}

func (s *streamingDocumentBtree) FindInDescendingOrder(ctx context.Context, key sop.UUID) (bool, error) {
	return s.store.BtreeInterface.FindInDescendingOrder(ctx, sd.StreamingDataKey[sop.UUID]{Key: key})
}

func (s *streamingDocumentBtree) GetCurrentKey() btree.Item[sop.UUID, Document] {
	if s.store.BtreeInterface.Count() == 0 {
		return btree.Item[sop.UUID, Document]{}
	}
	key := s.store.GetCurrentKey(context.Background())
	return btree.Item[sop.UUID, Document]{Key: key}
}

func (s *streamingDocumentBtree) GetCurrentValue(ctx context.Context) (Document, error) {
	dec, err := s.store.GetCurrentValue(ctx)
	if err != nil {
		return Document{}, err
	}
	var doc Document
	if err := dec.Decode(&doc); err != nil {
		return Document{}, err
	}
	return doc, nil
}

func (s *streamingDocumentBtree) GetCurrentValueNoLock(ctx context.Context) (Document, error) {
	return s.GetCurrentValue(ctx)
}

func (s *streamingDocumentBtree) GetCurrentItem(ctx context.Context) (btree.Item[sop.UUID, Document], error) {
	key := s.store.GetCurrentKey(ctx)
	val, err := s.GetCurrentValue(ctx)
	if err != nil {
		return btree.Item[sop.UUID, Document]{}, err
	}
	return btree.Item[sop.UUID, Document]{Key: key, Value: &val}, nil
}

func (s *streamingDocumentBtree) GetCurrentItemNoLock(ctx context.Context) (btree.Item[sop.UUID, Document], error) {
	return s.GetCurrentItem(ctx)
}

func (s *streamingDocumentBtree) RLockCurrentItem(ctx context.Context) error {
	return nil
}

func (s *streamingDocumentBtree) First(ctx context.Context) (bool, error) {
	return s.store.BtreeInterface.First(ctx)
}

func (s *streamingDocumentBtree) Last(ctx context.Context) (bool, error) {
	return s.store.BtreeInterface.Last(ctx)
}

func (s *streamingDocumentBtree) Next(ctx context.Context) (bool, error) {
	if s.store.BtreeInterface.Count() == 0 {
		return false, nil
	}
	currentKey := s.store.BtreeInterface.GetCurrentKey().Key.Key
	for {
		ok, err := s.store.BtreeInterface.Next(ctx)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
		if s.store.BtreeInterface.GetCurrentKey().Key.Key.Compare(currentKey) != 0 {
			return true, nil
		}
	}
}

func (s *streamingDocumentBtree) Previous(ctx context.Context) (bool, error) {
	if s.store.BtreeInterface.Count() == 0 {
		return false, nil
	}
	currentKey := s.store.BtreeInterface.GetCurrentKey().Key.Key
	for {
		ok, err := s.store.BtreeInterface.Previous(ctx)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
		if s.store.BtreeInterface.GetCurrentKey().Key.Key.Compare(currentKey) != 0 {
			return true, nil
		}
	}
}

func (s *streamingDocumentBtree) IsUnique() bool {
	return true
}

func (s *streamingDocumentBtree) Count() int64 {
	if s.store.BtreeInterface.Count() == 0 {
		return 0
	}
	ctx := context.Background()
	count := int64(0)
	if ok, err := s.store.BtreeInterface.First(ctx); err == nil && ok {
		count++
		prevKey := s.store.BtreeInterface.GetCurrentKey().Key.Key
		for {
			ok, err := s.store.BtreeInterface.Next(ctx)
			if err != nil || !ok {
				break
			}
			currKey := s.store.BtreeInterface.GetCurrentKey().Key.Key
			if currKey.Compare(prevKey) != 0 {
				count++
				prevKey = currKey
			}
		}
	}
	return count
}

func (s *streamingDocumentBtree) GetStoreInfo() sop.StoreInfo {
	if s.store == nil || s.store.BtreeInterface == nil {
		return sop.StoreInfo{}
	}
	return s.store.BtreeInterface.GetStoreInfo()
}

// GetDocumentChunkMetadata lists the chunk indexes and sizes for a streamed document.
func (s *streamingDocumentBtree) GetDocumentChunkMetadata(key sop.UUID) ([]DocumentChunkMetadata, error) {
	ctx := context.Background()
	if key.IsNil() {
		return nil, fmt.Errorf("document key cannot be nil")
	}
	if found, err := s.store.FindOne(ctx, key); err != nil {
		return nil, err
	} else if !found {
		return nil, nil
	}

	var metadata []DocumentChunkMetadata
	for {
		chunkKey := s.store.BtreeInterface.GetCurrentKey().Key
		chunkValue, err := s.store.BtreeInterface.GetCurrentValue(ctx)
		if err != nil {
			return nil, err
		}
		metadata = append(metadata, DocumentChunkMetadata{Index: chunkKey.ChunkIndex, Size: len(chunkValue)})
		ok, err := s.store.BtreeInterface.Next(ctx)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		if s.store.BtreeInterface.GetCurrentKey().Key.Key.Compare(key) != 0 {
			break
		}
	}
	return metadata, nil
}

// GetDocumentChunk returns the payload bytes for a specific chunk index.
func (s *streamingDocumentBtree) GetDocumentChunk(key sop.UUID, chunkIndex int) ([]byte, error) {
	ctx := context.Background()
	if key.IsNil() {
		return nil, fmt.Errorf("document key cannot be nil")
	}
	found, err := s.store.FindChunk(ctx, key, chunkIndex)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return s.store.BtreeInterface.GetCurrentValue(ctx)
}

var _ btree.BtreeInterface[sop.UUID, Document] = (*streamingDocumentBtree)(nil)

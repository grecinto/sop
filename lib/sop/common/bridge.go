package common

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/btree"
)

// BridgeEntry describes a single value attached to a parent key in a simple bridge store.
type BridgeEntry[TK comparable, TV comparable] struct {
	Key   TK
	Value TV
}

// BridgeDeleteRequest removes one or more values from a key's bridge list.
type BridgeDeleteRequest[TK comparable, TV comparable] struct {
	Key    TK
	Values []TV
}

// SimpleBridgeStore manages a many-to-one bridge index using a B-tree of key -> []value.
type SimpleBridgeStore[TK comparable, TV comparable] struct {
	tree btree.BtreeInterface[TK, []TV]
}

// NewSimpleBridgeStore creates a helper for managing simple bridge tables backed by a B-tree.
func NewSimpleBridgeStore[TK comparable, TV comparable](tree btree.BtreeInterface[TK, []TV]) *SimpleBridgeStore[TK, TV] {
	if tree == nil {
		return nil
	}
	return &SimpleBridgeStore[TK, TV]{tree: tree}
}

// Upsert adds one or more values for a key, preserving uniqueness per value.
func (s *SimpleBridgeStore[TK, TV]) Upsert(ctx context.Context, entries []BridgeEntry[TK, TV]) error {
	if s == nil || s.tree == nil {
		return fmt.Errorf("bridge store is not initialized")
	}
	for _, entry := range entries {
		found, err := s.tree.Find(ctx, entry.Key, true)
		if err != nil {
			return err
		}
		values := make([]TV, 0)
		if found {
			current, err := s.tree.GetCurrentValue(ctx)
			if err != nil {
				return err
			}
			values = append(values, current...)
		}
		if !containsBridgeValue(values, entry.Value) {
			values = append(values, entry.Value)
		}
		if _, err := s.tree.Upsert(ctx, entry.Key, values); err != nil {
			return err
		}
	}
	return nil
}

// ListValues returns all bridge values for a key.
func (s *SimpleBridgeStore[TK, TV]) ListValues(ctx context.Context, key TK) ([]TV, error) {
	if s == nil || s.tree == nil {
		return nil, nil
	}
	found, err := s.tree.Find(ctx, key, true)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	current, err := s.tree.GetCurrentValue(ctx)
	if err != nil {
		return nil, err
	}
	return append([]TV(nil), current...), nil
}

// Delete removes one or more values from a key's bridge list.
func (s *SimpleBridgeStore[TK, TV]) Delete(ctx context.Context, requests []BridgeDeleteRequest[TK, TV]) error {
	if s == nil || s.tree == nil {
		return nil
	}
	for _, req := range requests {
		found, err := s.tree.Find(ctx, req.Key, true)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		current, err := s.tree.GetCurrentValue(ctx)
		if err != nil {
			return err
		}
		remaining := make([]TV, 0, len(current))
		for _, value := range current {
			shouldRemove := false
			for _, remove := range req.Values {
				if value == remove {
					shouldRemove = true
					break
				}
			}
			if !shouldRemove {
				remaining = append(remaining, value)
			}
		}
		if len(remaining) == 0 {
			_, err = s.tree.Remove(ctx, req.Key)
		} else {
			_, err = s.tree.Upsert(ctx, req.Key, remaining)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func containsBridgeValue[TV comparable](values []TV, target TV) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// normalizeBridgeValue trims strings for simple bridge stores that use string values.
func normalizeBridgeValue(value string) string {
	return strings.TrimSpace(value)
}

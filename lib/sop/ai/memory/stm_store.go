package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	log "log/slog"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/btree"
)

const (
	stmBatchMaxItems = 1000
)

var stmBatchWindow = 30 * time.Second

type ShortTermMemoryStore struct {
	agentID string
	userID  string
	primary btree.BtreeInterface[string, any]
	byTime  btree.BtreeInterface[string, any]
	maxAge  time.Duration

	queue      chan map[string]any
	systemDB   Database
	lastCommit atomic.Int64
	mu         sync.RWMutex

	// Hook for session state persistence (work streams, conversation threads)
	sessionStatePersister func(context.Context, btree.BtreeInterface[string, any]) error
}

var (
	stmRegistryMu sync.RWMutex
	stmRegistry   = make(map[*ShortTermMemoryStore]struct{})
)

func RegisterShortTermMemoryStore(store *ShortTermMemoryStore) {
	if store == nil {
		return
	}
	stmRegistryMu.Lock()
	defer stmRegistryMu.Unlock()
	stmRegistry[store] = struct{}{}
}

func UnregisterShortTermMemoryStore(store *ShortTermMemoryStore) {
	if store == nil {
		return
	}
	stmRegistryMu.Lock()
	defer stmRegistryMu.Unlock()
	delete(stmRegistry, store)
}

func FlushExpiredShortTermMemoryStores(ctx context.Context) error {
	stmRegistryMu.RLock()
	stores := make([]*ShortTermMemoryStore, 0, len(stmRegistry))
	for store := range stmRegistry {
		stores = append(stores, store)
	}
	stmRegistryMu.RUnlock()

	for _, store := range stores {
		if err := store.FlushIfExpired(ctx); err != nil {
			return err
		}
	}
	return nil
}

func buildMemoryScopeID(agentID string, userID string) string {
	agentID = strings.TrimSpace(agentID)
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return agentID
	}
	if agentID == "" {
		return userID
	}
	return fmt.Sprintf("%s_%s", agentID, userID)
}

func BuildSTMStoreName(agentID string, userID string) string {
	scope := strings.TrimSpace(userID)
	if scope == "" {
		scope = strings.TrimSpace(agentID)
	}
	if scope == "" {
		return "default_stm"
	}
	return fmt.Sprintf("%s_stm", strings.NewReplacer(" ", "_", "/", "_", "\\", "_").Replace(scope))
}

func BuildSTMTimeIndexName(agentID string, userID string) string {
	return fmt.Sprintf("%s_by_time", BuildSTMStoreName(agentID, userID))
}

func BuildLTMStoreName(agentID string, userID string) string {
	scope := strings.TrimSpace(userID)
	if scope == "" {
		scope = strings.TrimSpace(agentID)
	}
	if scope == "" {
		return "default_ltm"
	}
	return fmt.Sprintf("%s_ltm", strings.NewReplacer(" ", "_", "/", "_", "\\", "_").Replace(scope))
}

func STMTimeIndexKey(createdAt int64, itemID string) string {
	return fmt.Sprintf("%020d:%s", createdAt, itemID)
}

func PruneSTMOlderThan(ctx context.Context, stm btree.BtreeInterface[string, any], stmByTime btree.BtreeInterface[string, any], now time.Time, maxAge time.Duration) (int, error) {
	if stm == nil || stmByTime == nil || maxAge <= 0 {
		return 0, nil
	}

	cutoffKey := STMTimeIndexKey(now.Add(-maxAge).UnixMilli(), "")
	var primaryIDs []string
	var indexKeys []string

	ok, err := stmByTime.First(ctx)
	if err != nil {
		return 0, err
	}
	for ok {
		key := stmByTime.GetCurrentKey()
		if key.Key >= cutoffKey {
			break
		}
		if key.Key != "root_anchor" {
			indexKeys = append(indexKeys, key.Key)
			if value, err := stmByTime.GetCurrentValue(ctx); err != nil {
				return 0, err
			} else if id, ok := value.(string); ok && id != "" {
				primaryIDs = append(primaryIDs, id)
			} else if sep := len(key.Key); sep > 21 {
				primaryIDs = append(primaryIDs, key.Key[21:])
			}
		}
		ok, err = stmByTime.Next(ctx)
		if err != nil {
			return 0, err
		}
	}

	for _, id := range primaryIDs {
		if _, err := stm.Remove(ctx, id); err != nil {
			return 0, err
		}
	}
	for _, key := range indexKeys {
		if _, err := stmByTime.Remove(ctx, key); err != nil {
			return 0, err
		}
	}

	return len(indexKeys), nil
}

func ExtractSTMCreatedAt(payload map[string]any) (int64, bool) {
	switch value := payload["created_at"].(type) {
	case int64:
		return value, true
	case int:
		return int64(value), true
	case int32:
		return int64(value), true
	case float64:
		return int64(value), true
	case float32:
		return int64(value), true
	default:
		return 0, false
	}
}

func NewShortTermMemoryStore(agentID string, maxAge time.Duration) *ShortTermMemoryStore {
	return &ShortTermMemoryStore{agentID: agentID, maxAge: maxAge}
}

func (s *ShortTermMemoryStore) FlushIfExpired(ctx context.Context) error {
	if s == nil || s.systemDB == nil || s.queue == nil {
		return nil
	}
	if s.lastCommit.Load() == 0 {
		s.lastCommit.Store(time.Now().UnixMilli())
	}
	if time.Since(time.UnixMilli(s.lastCommit.Load())) < stmBatchWindow {
		return nil
	}
	return s.flushPending(ctx)
}

func (s *ShortTermMemoryStore) flushPending(ctx context.Context) error {
	if s == nil || s.systemDB == nil || s.queue == nil {
		return nil
	}
	select {
	case payload := <-s.queue:
		if payload == nil {
			return nil
		}
		return s.commitBatch(ctx, payload)
	default:
		return nil
	}
}

func (s *ShortTermMemoryStore) commitBatch(ctx context.Context, firstPayload map[string]any) error {
	if s == nil || s.systemDB == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	tx, err := s.systemDB.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		return err
	}
	if err := s.Open(ctx, s.systemDB, tx); err != nil {
		tx.Rollback(ctx)
		return err
	}

	batch := []map[string]any{firstPayload}
	for {
		select {
		case payload := <-s.queue:
			if payload == nil {
				continue
			}
			batch = append(batch, payload)
		default:
			goto commit
		}
	}

commit:
	for _, payload := range batch {
		if err := s.UpsertEpisode(ctx, payload); err != nil {
			log.Warn("ShortTermMemoryStore: failed to add item", "agent_id", s.agentID, "error", err)
		}
	}
	if s.sessionStatePersister != nil {
		if err := s.sessionStatePersister(ctx, s.primary); err != nil {
			log.Warn("ShortTermMemoryStore: failed to persist session state", "agent_id", s.agentID, "error", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.lastCommit.Store(time.Now().UnixMilli())
	s.Close()
	return nil
}

func (s *ShortTermMemoryStore) SetUserID(userID string) {
	if s == nil {
		return
	}
	s.userID = strings.TrimSpace(userID)
}

func (s *ShortTermMemoryStore) StoreName() string {
	return BuildSTMStoreName(s.agentID, s.userID)
}

func (s *ShortTermMemoryStore) TimeIndexName() string {
	return BuildSTMTimeIndexName(s.agentID, s.userID)
}

// SetSessionStatePersister allows external code to hook into batch commits
// for persisting session state (work streams, conversation threads)
func (s *ShortTermMemoryStore) SetSessionStatePersister(fn func(context.Context, btree.BtreeInterface[string, any]) error) {
	if s == nil {
		return
	}
	s.sessionStatePersister = fn
}

func (s *ShortTermMemoryStore) Primary() btree.BtreeInterface[string, any] {
	if s == nil {
		return nil
	}
	return s.primary
}

func (s *ShortTermMemoryStore) Open(ctx context.Context, systemDB Database, tx sop.Transaction) error {
	if s == nil {
		return nil
	}
	if s.primary != nil && s.byTime != nil {
		return nil
	}
	primary, err := systemDB.NewBtree(ctx, s.StoreName(), tx)
	if err != nil {
		return fmt.Errorf("failed to create/open isolated STM BTree: %w", err)
	}
	byTime, err := systemDB.NewBtree(ctx, s.TimeIndexName(), tx)
	if err != nil {
		return fmt.Errorf("failed to create/open isolated STM time index BTree: %w", err)
	}
	s.primary = primary
	s.byTime = byTime
	return nil
}

func (s *ShortTermMemoryStore) Attach(primary btree.BtreeInterface[string, any], byTime btree.BtreeInterface[string, any]) {
	if s == nil {
		return
	}
	s.primary = primary
	s.byTime = byTime
}

func (s *ShortTermMemoryStore) Close() {
	if s == nil {
		return
	}
	UnregisterShortTermMemoryStore(s)
	s.primary = nil
	s.byTime = nil
	s.queue = nil
	s.systemDB = nil
}

func (s *ShortTermMemoryStore) PruneExpired(ctx context.Context, now time.Time) (int, error) {
	if s == nil {
		return 0, nil
	}
	return PruneSTMOlderThan(ctx, s.primary, s.byTime, now, s.maxAge)
}

func (s *ShortTermMemoryStore) UpsertEpisode(ctx context.Context, payload map[string]any) error {
	if s == nil || s.primary == nil || s.byTime == nil {
		return nil
	}
	itemID, _ := payload["id"].(string)
	if itemID == "" {
		return fmt.Errorf("stm episode payload missing id")
	}
	createdAt, _ := ExtractSTMCreatedAt(payload)

	ok, err := s.primary.Add(ctx, itemID, payload)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	_, _ = s.byTime.Add(ctx, STMTimeIndexKey(createdAt, itemID), itemID)
	return nil
}

func (s *ShortTermMemoryStore) RemoveEpisode(ctx context.Context, itemID string, payload map[string]any) error {
	if s == nil || s.primary == nil || s.byTime == nil || itemID == "" {
		return nil
	}
	if _, err := s.primary.Remove(ctx, itemID); err != nil {
		return err
	}
	if createdAt, ok := ExtractSTMCreatedAt(payload); ok {
		if _, err := s.byTime.Remove(ctx, STMTimeIndexKey(createdAt, itemID)); err != nil {
			return err
		}
	}
	return nil
}

func (s *ShortTermMemoryStore) StartPeriodicCommitter(ctx context.Context, systemDB Database, queue <-chan map[string]any) error {
	if s == nil {
		return nil
	}
	if systemDB == nil {
		return fmt.Errorf("systemDB can't be nil")
	}
	if queue == nil {
		return fmt.Errorf("stm episode queue can't be nil")
	}

	if ch, ok := any(queue).(chan map[string]any); ok {
		s.queue = ch
	}
	s.systemDB = systemDB
	RegisterShortTermMemoryStore(s)

	go func() {
		log.Info("ShortTermMemoryStore: Periodic committer started", "agent_id", s.agentID)

		for {
			select {
			case <-ctx.Done():
				log.Info("ShortTermMemoryStore: Periodic committer interrupted", "agent_id", s.agentID)
				return
			case firstPayload := <-queue:
				if firstPayload == nil {
					continue
				}
				if err := s.commitBatch(ctx, firstPayload); err != nil {
					log.Warn("ShortTermMemoryStore: commit failed", "agent_id", s.agentID, "error", err)
				}
			}
		}
	}()

	return nil
}

package sopbackend

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartBackgroundServiceWithContextRunsRegisteredHooks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls int32
	RegisterBackgroundServiceHook(func(context.Context) {
		atomic.AddInt32(&calls, 1)
	})

	StartBackgroundServiceWithContext(ctx, 5*time.Millisecond, time.Hour)

	deadline := time.Now().Add(500 * time.Millisecond)
	for atomic.LoadInt32(&calls) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	if atomic.LoadInt32(&calls) == 0 {
		t.Fatalf("expected registered background service hook to run")
	}
}

func TestCleanupStaleSessionsRemovesExpiredRuntimeSessions(t *testing.T) {
	oldSessions := activeSessions
	activeSessions = NewSessionManager(10)
	defer func() { activeSessions = oldSessions }()

	activeSessions.Put("stale-session", nil)
	activeSessions.Put("fresh-session", nil)

	sm := activeSessions
	sm.mu.Lock()
	if node, ok := sm.lookup["stale-session"]; ok {
		node.lastUse = time.Now().Add(-2 * time.Hour)
	}
	if node, ok := sm.lookup["fresh-session"]; ok {
		node.lastUse = time.Now()
	}
	sm.mu.Unlock()

	removed := CleanupStaleSessions(time.Hour)
	if removed != 1 {
		t.Fatalf("expected one stale session to be removed, got %d", removed)
	}
	if _, ok := activeSessions.Get("stale-session"); ok {
		t.Fatalf("expected stale session to be removed")
	}
	if _, ok := activeSessions.Get("fresh-session"); !ok {
		t.Fatalf("expected fresh session to remain")
	}
}

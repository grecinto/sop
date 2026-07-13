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

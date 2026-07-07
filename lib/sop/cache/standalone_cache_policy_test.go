package cache

import (
	"testing"

	"github.com/sharedcode/sop"
)

func TestStandaloneInMemoryCacheUsesSmallerDefaults(t *testing.T) {
	globalL1Locker.Lock()
	globalL1CacheRegistry = make(map[sop.L2CacheType]*L1Cache)
	globalL1Locker.Unlock()

	l1 := GetGlobalL1Cache(NewStandaloneL2InMemoryCache())

	if l1.mru.minCapacity != 32 {
		t.Fatalf("expected standalone L1 min capacity 32, got %d", l1.mru.minCapacity)
	}
	if l1.mru.maxCapacity != 64 {
		t.Fatalf("expected standalone L1 max capacity 64, got %d", l1.mru.maxCapacity)
	}
}

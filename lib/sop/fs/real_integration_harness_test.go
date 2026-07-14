package fs

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/common/mocks"
)

type realIntegrationHarness struct {
	t *testing.T

	ctx         context.Context
	activeDir   string
	passiveDir  string
	tracker     *replicationTracker
	repository  *StoreRepository
	cache       sop.L2Cache
	storeName   string
	registryURL string

	previousGlobalReplicationDetails *ReplicationTrackedDetails
	previousRecoveryStateCleanupPath string
}

func newRealIntegrationHarness(t *testing.T) *realIntegrationHarness {
	t.Helper()

	baseDir := filepath.Join(t.TempDir(), "real-integration")
	activeDir := filepath.Join(baseDir, "active")
	passiveDir := filepath.Join(baseDir, "passive")
	if err := os.MkdirAll(activeDir, 0o755); err != nil {
		t.Fatalf("mkdir active dir: %v", err)
	}
	if err := os.MkdirAll(passiveDir, 0o755); err != nil {
		t.Fatalf("mkdir passive dir: %v", err)
	}

	globalReplicationDetailsLocker.RLock()
	previousGlobal := GlobalReplicationDetails
	globalReplicationDetailsLocker.RUnlock()
	if previousGlobal != nil {
		previousGlobal = &ReplicationTrackedDetails{
			FailedToReplicate:   previousGlobal.FailedToReplicate,
			ActiveFolderToggler: previousGlobal.ActiveFolderToggler,
			LogCommitChanges:    previousGlobal.LogCommitChanges,
		}
	}

	recoveryStateCleanupPathMu.RLock()
	previousCleanupPath := recoveryStateCleanupPath
	recoveryStateCleanupPathMu.RUnlock()

	globalReplicationDetailsLocker.Lock()
	GlobalReplicationDetails = nil
	globalReplicationDetailsLocker.Unlock()

	cache := mocks.NewMockClient()
	tracker, err := NewReplicationTracker(context.Background(), []string{activeDir, passiveDir}, true, cache)
	if err != nil {
		t.Fatalf("new replication tracker: %v", err)
	}

	repository, err := NewStoreRepository(context.Background(), tracker, nil, cache, 64)
	if err != nil {
		t.Fatalf("new store repository: %v", err)
	}

	harness := &realIntegrationHarness{
		t:                                t,
		ctx:                              context.Background(),
		activeDir:                        activeDir,
		passiveDir:                       passiveDir,
		tracker:                          tracker,
		repository:                       repository,
		cache:                            cache,
		storeName:                        "store-a",
		registryURL:                      "registry-a",
		previousGlobalReplicationDetails: previousGlobal,
		previousRecoveryStateCleanupPath: previousCleanupPath,
	}

	t.Cleanup(func() {
		globalReplicationDetailsLocker.Lock()
		GlobalReplicationDetails = harness.previousGlobalReplicationDetails
		globalReplicationDetailsLocker.Unlock()
		SetRecoveryStateCleanupPath(harness.previousRecoveryStateCleanupPath)
		SetRecoveryStateConfigUpdater(nil)
	})

	return harness
}

func (h *realIntegrationHarness) seedStoreRepository() {
	h.t.Helper()

	store := sop.StoreInfo{Name: h.storeName, RegistryTable: h.registryURL}
	if err := h.repository.Add(h.ctx, store); err != nil {
		h.t.Fatalf("add store: %v", err)
	}

	regDir := filepath.Join(h.activeDir, h.registryURL)
	if err := os.MkdirAll(regDir, 0o755); err != nil {
		h.t.Fatalf("mkdir registry dir: %v", err)
	}
	payload := []byte("segment-data")
	segmentPath := filepath.Join(regDir, h.registryURL+"-1"+registryFileExtension)
	if err := os.WriteFile(segmentPath, payload, 0o644); err != nil {
		h.t.Fatalf("write registry segment: %v", err)
	}
}

func (h *realIntegrationHarness) verifyInitialReplication() {
	h.t.Helper()

	storeListPath := filepath.Join(h.passiveDir, storeListFilename)
	if _, err := os.Stat(storeListPath); err != nil {
		h.t.Fatalf("expected replicated store list at %s: %v", storeListPath, err)
	}
	storeInfoPath := filepath.Join(h.passiveDir, h.storeName, StoreInfoFilename)
	if _, err := os.Stat(storeInfoPath); err != nil {
		h.t.Fatalf("expected replicated store info at %s: %v", storeInfoPath, err)
	}
}

func (h *realIntegrationHarness) simulateFailoverAndReinstate() {
	h.t.Helper()

	SetRecoveryStateCleanupPath(filepath.Join(h.activeDir, "recovery"))
	h.t.Cleanup(func() { SetRecoveryStateCleanupPath(h.previousRecoveryStateCleanupPath) })

	ioErr := sop.Error{Code: sop.FailoverQualifiedError + 1, Err: syscall.EROFS}
	h.tracker.HandleReplicationRelatedError(h.ctx, ioErr, nil, false)

	if !h.tracker.FailedToReplicate {
		h.t.Fatal("expected tracker to enter failed-to-replicate state after failover")
	}
	if got := h.tracker.getActiveBaseFolder(); got != h.passiveDir {
		h.t.Fatalf("expected active folder %q after failover, got %q", h.passiveDir, got)
	}

	if err := h.tracker.ReinstateFailedDrives(h.ctx); err != nil {
		h.t.Fatalf("reinstate failed: %v", err)
	}
	if h.tracker.FailedToReplicate {
		h.t.Fatal("expected reinstate to clear failed-to-replicate state")
	}
}

func TestRealIntegrationHarness_FailoverAndReinstate(t *testing.T) {
	harness := newRealIntegrationHarness(t)
	harness.seedStoreRepository()
	harness.verifyInitialReplication()
	harness.simulateFailoverAndReinstate()

	if _, err := os.Stat(filepath.Join(harness.passiveDir, harness.storeName, StoreInfoFilename)); err != nil {
		t.Fatalf("expected store info to remain available after reinstate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(harness.activeDir, harness.registryURL, harness.registryURL+"-1"+registryFileExtension)); err != nil {
		t.Fatalf("expected registry segment to be visible after reinstate: %v", err)
	}
}

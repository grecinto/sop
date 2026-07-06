package infs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/cache"
	"github.com/sharedcode/sop/encoding"
	"github.com/sharedcode/sop/fs"
)

func init() {
	sop.RegisterL2CacheFactory(sop.InMemory, func(sop.TransactionOptions) sop.L2Cache { return cache.NewL2InMemoryCache() })
}

func TestReinstateFailedDrivesRequiresExactlyTwoStoreFolders(t *testing.T) {
	err := ReinstateFailedDrives(context.Background(), []string{t.TempDir()}, sop.InMemory)
	if err == nil {
		t.Fatal("expected error when fewer than two store folders are provided")
	}
	if !strings.Contains(err.Error(), "two strings") {
		t.Fatalf("expected two-folder error, got %v", err)
	}
}

func TestReinstateFailedDrivesReturnsErrorWhenNotInFailedState(t *testing.T) {
	active := t.TempDir()
	passive := t.TempDir()

	err := ReinstateFailedDrives(context.Background(), []string{active, passive}, sop.InMemory)
	if err == nil {
		t.Fatal("expected error when replication has not entered FailedToReplicate state")
	}
	if !strings.Contains(err.Error(), "FailedToReplicate") {
		t.Fatalf("expected FailedToReplicate precondition error, got %v", err)
	}
}

func TestReinstateFailedDrivesSucceedsForPreparedReplicationState(t *testing.T) {
	prev := fs.GlobalReplicationDetails
	fs.GlobalReplicationDetails = nil
	t.Cleanup(func() {
		fs.GlobalReplicationDetails = prev
	})

	active := t.TempDir()
	passive := t.TempDir()
	statusPath := filepath.Join(active, "replstat.txt")
	status := fs.ReplicationTrackedDetails{FailedToReplicate: true, ActiveFolderToggler: true, LogCommitChanges: false}
	payload, err := encoding.DefaultMarshaler.Marshal(status)
	if err != nil {
		t.Fatalf("marshal replication status: %v", err)
	}
	if err := os.WriteFile(statusPath, payload, 0o644); err != nil {
		t.Fatalf("write replication status: %v", err)
	}

	err = ReinstateFailedDrives(context.Background(), []string{active, passive}, sop.InMemory)
	if err != nil {
		t.Fatalf("ReinstateFailedDrives returned error: %v", err)
	}
	if fs.GlobalReplicationDetails == nil {
		t.Fatal("expected global replication state to be initialized after reinstate")
	}
	if fs.GlobalReplicationDetails.FailedToReplicate {
		t.Fatal("expected FailedToReplicate to be cleared after reinstate")
	}
	if fs.GlobalReplicationDetails.LogCommitChanges {
		t.Fatal("expected commit-change logging to be cleared after reinstate")
	}
}

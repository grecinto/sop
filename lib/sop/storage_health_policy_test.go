package sop

import (
	"syscall"
	"testing"
)

func TestStorageHealthPolicyShouldAutoRetryRespectsThresholds(t *testing.T) {
	policy := NewStorageHealthPolicy()

	if policy.ShouldAutoRetry("write", "/tmp/data") {
		t.Fatal("expected no auto-retry before any degradation threshold is crossed")
	}

	policy.RecordFailure("write", "/tmp/data", syscall.EIO)
	policy.RecordFailure("write", "/tmp/data", syscall.EIO)
	if policy.ShouldAutoRetry("write", "/tmp/data") {
		t.Fatal("expected no auto-retry before the threshold is reached")
	}

	policy.RecordFailure("write", "/tmp/data", syscall.EIO)
	if !policy.ShouldAutoRetry("write", "/tmp/data") {
		t.Fatal("expected auto-retry to be enabled once the policy threshold is reached")
	}

	policy.RecordFailure("write", "/tmp/data", syscall.EIO)
	if policy.ShouldAutoRetry("write", "/tmp/data") {
		t.Fatal("expected auto-retry to stop once the retry budget is exhausted")
	}
}

func TestStorageHealthPolicyShouldAutoRetryDisablesForReadOnlyErrors(t *testing.T) {
	policy := NewStorageHealthPolicy()
	policy.RecordFailure("write", "/tmp/data", syscall.EROFS)

	if policy.ShouldAutoRetry("write", "/tmp/data") {
		t.Fatal("expected read-only errors to disable auto-retry")
	}
}

func TestReportStoragePathFailureRecordsSharedPolicyState(t *testing.T) {
	restore := SetDefaultStorageHealthPolicy(NewStorageHealthPolicy())
	defer restore()

	ReportStoragePathFailure("write", "/tmp/shared-store", syscall.EROFS)

	policy := DefaultStorageHealthPolicy()
	state := policy.StoragePathState("write", "/tmp/shared-store")
	if state.Status != StorageHealthReadOnly {
		t.Fatalf("expected shared policy status %q, got %q", StorageHealthReadOnly, state.Status)
	}
	if policy.ShouldAutoRetry("write", "/tmp/shared-store") {
		t.Fatal("expected shared policy to disable auto-retry for read-only failures")
	}
}

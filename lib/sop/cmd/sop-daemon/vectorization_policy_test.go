package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeVectorizationFailurePolicyPersistsStateAcrossInstances(t *testing.T) {
	tmpDir := t.TempDir()

	policy1 := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 2; i++ {
		skip, escalate := policy1.HandleFailure("kb", "category", "cat-1", os.ErrNotExist)
		if !skip {
			t.Fatalf("expected first skip to be true")
		}
		if escalate {
			t.Fatalf("expected no escalation before threshold")
		}
	}

	policy2 := newRuntimeVectorizationFailurePolicy(tmpDir)
	skip, escalate := policy2.HandleFailure("kb", "category", "cat-1", os.ErrNotExist)
	if !skip {
		t.Fatalf("expected skip on repeated failure")
	}
	if !escalate {
		t.Fatalf("expected escalation after repeated failures")
	}
}

func TestRuntimeVectorizationFailurePolicyWritesStateFile(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	policy.HandleFailure("kb", "category", "cat-1", os.ErrNotExist)

	statePath := filepath.Join(tmpDir, "vectorization-failure-state.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected state file to be created: %v", err)
	}
}

func TestRuntimeVectorizationFailurePolicyExposesRecoveryState(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)

	_, escalate := policy.HandleFailure("kb", "category", "cat-1", os.ErrNotExist)
	if escalate {
		t.Fatalf("expected no escalation before threshold")
	}

	recoveryState, ok := policy.(interface {
		RecoveryState() runtimeVectorizationRecoveryState
	})
	if !ok {
		t.Fatalf("expected policy to expose recovery state")
	}

	state := recoveryState.RecoveryState()
	if state.TargetID != "cat-1" {
		t.Fatalf("expected target id to be preserved, got %q", state.TargetID)
	}
	if state.Count != 1 {
		t.Fatalf("expected initial count to be 1, got %d", state.Count)
	}
	if state.Status != "degraded" {
		t.Fatalf("expected degraded status, got %q", state.Status)
	}
}

func TestRuntimeVectorizationFailurePolicyTracksExplicitStatusTransitions(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)

	policy.HandleFailure("kb", "category", "cat-2", os.ErrNotExist)
	policy.(interface {
		MarkIgnored(kbName, targetType, targetID string)
		RecoveryState() runtimeVectorizationRecoveryState
	}).MarkIgnored("kb", "category", "cat-2")

	state := policy.(interface {
		RecoveryState() runtimeVectorizationRecoveryState
	}).RecoveryState()
	if state.Status != "ignored" {
		t.Fatalf("expected ignored status, got %q", state.Status)
	}
	if state.TargetID != "cat-2" {
		t.Fatalf("expected target id to be preserved, got %q", state.TargetID)
	}

	policy.(interface {
		MarkRecovered(kbName, targetType, targetID string)
	}).MarkRecovered("kb", "category", "cat-2")
	state = policy.(interface {
		RecoveryState() runtimeVectorizationRecoveryState
	}).RecoveryState()
	if state.Status != "recovered" {
		t.Fatalf("expected recovered status, got %q", state.Status)
	}
}

func TestRuntimeVectorizationFailurePolicyListsAllRecoveryStates(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)

	policy.HandleFailure("kb", "category", "cat-1", os.ErrNotExist)
	policy.HandleFailure("kb", "category", "cat-2", os.ErrNotExist)
	policy.(interface {
		MarkIgnored(kbName, targetType, targetID string)
	}).MarkIgnored("kb", "category", "cat-2")

	states, ok := policy.(interface {
		RecoveryStates() []runtimeVectorizationRecoveryState
	})
	if !ok {
		t.Fatalf("expected policy to expose recovery states")
	}

	allStates := states.RecoveryStates()
	if len(allStates) != 2 {
		t.Fatalf("expected 2 recovery states, got %d", len(allStates))
	}

	foundIgnored := false
	for _, state := range allStates {
		if state.TargetID == "cat-2" && state.Status == "ignored" {
			foundIgnored = true
		}
	}
	if !foundIgnored {
		t.Fatalf("expected ignored state to be listed")
	}
}

func TestRuntimeVectorizationFailurePolicyReportsNeedsRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	policy.HandleFailure("kb", "category", "cat-1", os.ErrNotExist)
	policy.HandleFailure("kb", "category", "cat-1", os.ErrNotExist)
	policy.HandleFailure("kb", "category", "cat-1", os.ErrNotExist)

	needsRecovery, ok := policy.(interface{ NeedsRecovery() bool })
	if !ok {
		t.Fatalf("expected policy to expose needs recovery status")
	}
	if !needsRecovery.NeedsRecovery() {
		t.Fatalf("expected policy to report recovery is needed")
	}
}

func TestRuntimeVectorizationFailurePolicyExposesRecoveryPrompt(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)

	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-3", os.ErrNotExist)
	}

	recoveryState, ok := policy.(interface {
		RecoveryState() runtimeVectorizationRecoveryState
	})
	if !ok {
		t.Fatalf("expected policy to expose recovery state")
	}

	state := recoveryState.RecoveryState()
	if state.Prompt == "" {
		t.Fatalf("expected recovery prompt after repeated failures")
	}
	if !strings.Contains(state.Prompt, "cat-3") {
		t.Fatalf("expected recovery prompt to mention target id, got %q", state.Prompt)
	}
}

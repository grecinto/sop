package database

import (
	"errors"
	"testing"
)

type stubVectorizationFailurePolicy struct {
	t        *testing.T
	skip     bool
	escalate bool
}

func (p stubVectorizationFailurePolicy) HandleFailure(kbName, targetType, targetID string, err error) (bool, bool) {
	if p.t != nil {
		if kbName != "daemon-kb" {
			p.t.Fatalf("unexpected kb name: %s", kbName)
		}
		if targetType != "category" {
			p.t.Fatalf("unexpected target type: %s", targetType)
		}
		if targetID == "" {
			p.t.Fatal("expected target id")
		}
		if err == nil || err.Error() != "boom" {
			p.t.Fatalf("unexpected error: %v", err)
		}
	}
	return p.skip, p.escalate
}

func TestHandleVectorizationFailure_SkipsWhenPolicyRequestsSkip(t *testing.T) {
	unregister := RegisterVectorizationFailurePolicy(stubVectorizationFailurePolicy{t: t, skip: true})
	defer unregister()

	skip, escalate := handleVectorizationFailure("daemon-kb", "category", "target-id", errors.New("boom"))
	if !skip {
		t.Fatal("expected vectorization failure to be marked as skipped")
	}
	if escalate {
		t.Fatal("expected no escalation when the policy only skips")
	}
}

func TestHandleVectorizationFailure_ReturnsEscalationSignal(t *testing.T) {
	unregister := RegisterVectorizationFailurePolicy(stubVectorizationFailurePolicy{t: t, escalate: true})
	defer unregister()

	skip, escalate := handleVectorizationFailure("daemon-kb", "category", "target-id", errors.New("boom"))
	if skip {
		t.Fatal("expected no skip when the policy only escalates")
	}
	if !escalate {
		t.Fatal("expected escalation signal to be returned")
	}
}

func TestHandleVectorizationFailure_ReturnsFalseWhenNoPolicyIsRegistered(t *testing.T) {
	unregister := RegisterVectorizationFailurePolicy(nil)
	defer unregister()

	skip, escalate := handleVectorizationFailure("daemon-kb", "category", "target-id", errors.New("boom"))
	if skip {
		t.Fatal("expected no skip when no policy is registered")
	}
	if escalate {
		t.Fatal("expected no escalation when no policy is registered")
	}
}

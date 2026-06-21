package main

import (
	"testing"
	"time"
)

func TestSessionRegistryRegisterAndHeartbeat(t *testing.T) {
	reg := NewSessionRegistry(SubscriptionPolicy{MaxRequests: 5, Window: time.Minute})
	sess, err := reg.RegisterSession("host-1", "sub-1")
	if err != nil {
		t.Fatalf("RegisterSession() error = %v", err)
	}
	if sess.SubscriptionID != "sub-1" {
		t.Fatalf("SubscriptionID = %q, want %q", sess.SubscriptionID, "sub-1")
	}
	if sess.RateLimitRemaining != 5 {
		t.Fatalf("RateLimitRemaining = %d, want %d", sess.RateLimitRemaining, 5)
	}

	if err := reg.Heartbeat(sess.ID); err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if got := reg.GetSession(sess.ID); got == nil || !got.LastSeen.After(sess.CreatedAt) {
		t.Fatalf("expected heartbeat to update LastSeen")
	}
}

func TestSessionRegistryEnforcesRateLimit(t *testing.T) {
	reg := NewSessionRegistry(SubscriptionPolicy{MaxRequests: 2, Window: time.Minute})
	sess, err := reg.RegisterSession("host-2", "sub-2")
	if err != nil {
		t.Fatalf("RegisterSession() error = %v", err)
	}

	if err := reg.ConsumeRequest(sess.ID); err != nil {
		t.Fatalf("first ConsumeRequest() error = %v", err)
	}
	if err := reg.ConsumeRequest(sess.ID); err != nil {
		t.Fatalf("second ConsumeRequest() error = %v", err)
	}
	if err := reg.ConsumeRequest(sess.ID); err == nil {
		t.Fatalf("expected third ConsumeRequest() to fail rate limit")
	}
}

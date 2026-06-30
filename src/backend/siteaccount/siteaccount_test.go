package siteaccount

import (
	"context"
	"testing"
)

func TestRegisterUserWithRequestPersistsUserAndBillingState(t *testing.T) {
	cfg := NewConfig()
	user, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         "site-account-user",
		Password:         "secret123",
		Email:            "site@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	})
	if err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}
	if user == nil {
		t.Fatal("RegisterUserWithRequest() returned nil user")
	}
	if user.BillingStatus != "pending" {
		t.Fatalf("BillingStatus = %q, want %q", user.BillingStatus, "pending")
	}

	found, ok, err := cfg.FindUserRecord(context.Background(), "site-account-user")
	if err != nil {
		t.Fatalf("FindUserRecord() error = %v", err)
	}
	if !ok || found == nil {
		t.Fatal("FindUserRecord() did not find created user")
	}
	if found.Username != "site-account-user" {
		t.Fatalf("FindUserRecord() username = %q, want %q", found.Username, "site-account-user")
	}
}

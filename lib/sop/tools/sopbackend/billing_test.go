package sopbackend

import (
	"context"
	"testing"
)

func TestCreateCheckoutSession_StoresPendingBillingState(t *testing.T) {
	cfg := Config{UsersStore: NewUserStore()}
	user, err := cfg.RegisterUser(context.Background(), "billing", "pw123", "billing@example.com", "user", "starter")
	if err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}

	session, err := cfg.CreateCheckoutSession(context.Background(), user.Username, "starter")
	if err != nil {
		t.Fatalf("CreateCheckoutSession() error = %v", err)
	}
	if session == nil {
		t.Fatal("CreateCheckoutSession() returned nil session")
	}
	if session.Status != "pending" {
		t.Fatalf("CreateCheckoutSession() status = %q, want %q", session.Status, "pending")
	}
	if session.URL == "" {
		t.Fatal("CreateCheckoutSession() returned empty checkout URL")
	}

	updated, ok, err := cfg.FindUserRecord(context.Background(), user.Username)
	if err != nil {
		t.Fatalf("FindUserRecord() error = %v", err)
	}
	if !ok || updated == nil {
		t.Fatal("FindUserRecord() returned no user")
	}
	if updated.SubscriptionPlan != "starter" {
		t.Fatalf("SubscriptionPlan = %q, want %q", updated.SubscriptionPlan, "starter")
	}
	if updated.BillingStatus != "pending" {
		t.Fatalf("BillingStatus = %q, want %q", updated.BillingStatus, "pending")
	}
}

func TestHandleBillingWebhook_ActivatesSubscription(t *testing.T) {
	cfg := Config{UsersStore: NewUserStore()}
	user, err := cfg.RegisterUser(context.Background(), "webhook", "pw123", "webhook@example.com", "user", "none")
	if err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}

	err = cfg.HandleBillingWebhook(context.Background(), BillingWebhookEvent{
		EventType: "checkout.session.completed",
		EventID:   "evt_test",
		UserID:    user.Username,
		Plan:      "starter",
	})
	if err != nil {
		t.Fatalf("HandleBillingWebhook() error = %v", err)
	}

	updated, ok, err := cfg.FindUserRecord(context.Background(), user.Username)
	if err != nil {
		t.Fatalf("FindUserRecord() error = %v", err)
	}
	if !ok || updated == nil {
		t.Fatal("FindUserRecord() returned no user")
	}
	if updated.SubscriptionPlan != "starter" {
		t.Fatalf("SubscriptionPlan = %q, want %q", updated.SubscriptionPlan, "starter")
	}
	if updated.BillingStatus != "active" {
		t.Fatalf("BillingStatus = %q, want %q", updated.BillingStatus, "active")
	}
	if updated.LastWebhookEvent != "checkout.session.completed" {
		t.Fatalf("LastWebhookEvent = %q, want %q", updated.LastWebhookEvent, "checkout.session.completed")
	}
}

func TestHandleBillingWebhook_ExpiresPendingCheckout(t *testing.T) {
	cfg := Config{UsersStore: NewUserStore()}
	user, err := cfg.RegisterUser(context.Background(), "expired", "pw123", "expired@example.com", "user", "starter")
	if err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}

	err = cfg.HandleBillingWebhook(context.Background(), BillingWebhookEvent{
		EventType: "checkout.session.expired",
		EventID:   "evt_expired",
		UserID:    user.Username,
		Plan:      "starter",
	})
	if err != nil {
		t.Fatalf("HandleBillingWebhook() error = %v", err)
	}

	updated, ok, err := cfg.FindUserRecord(context.Background(), user.Username)
	if err != nil {
		t.Fatalf("FindUserRecord() error = %v", err)
	}
	if !ok || updated == nil {
		t.Fatal("FindUserRecord() returned no user")
	}
	if updated.BillingStatus != "inactive" {
		t.Fatalf("BillingStatus = %q, want %q", updated.BillingStatus, "inactive")
	}
	if updated.FailureReason != "checkout session expired" {
		t.Fatalf("FailureReason = %q, want %q", updated.FailureReason, "checkout session expired")
	}
}

package sopbackend

import (
	"context"
	"testing"
	"time"

	"github.com/sharedcode/sop"
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

func TestCreateCheckoutSession_PersistsSubscriptionAndPaymentRecords(t *testing.T) {
	cfg := Config{UsersStore: NewUserStore(), SubscriptionsStore: NewSubscriptionStore(), PaymentsStore: NewPaymentStore()}
	user, err := cfg.RegisterUser(context.Background(), "persisted-billing", "pw123", "persisted-billing@example.com", "user", "starter")
	if err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}

	if _, err := cfg.CreateCheckoutSession(context.Background(), user.Username, "starter"); err != nil {
		t.Fatalf("CreateCheckoutSession() error = %v", err)
	}

	subscriptions, err := cfg.ensureSubscriptionStore().ListByUser(context.Background(), user.Username)
	if err != nil {
		t.Fatalf("ListByUser() subscriptions error = %v", err)
	}
	if len(subscriptions) != 1 {
		t.Fatalf("expected 1 persisted subscription, got %d", len(subscriptions))
	}

	payments, err := cfg.ensurePaymentStore().ListByUser(context.Background(), user.Username)
	if err != nil {
		t.Fatalf("ListByUser() payments error = %v", err)
	}
	if len(payments) != 1 {
		t.Fatalf("expected 1 persisted payment, got %d", len(payments))
	}
}

func TestUnregisterUser_RemovesBillingRecordsForUser(t *testing.T) {
	cfg := Config{UsersStore: NewUserStore(), SubscriptionsStore: NewSubscriptionStore(), PaymentsStore: NewPaymentStore()}
	user, err := cfg.RegisterUser(context.Background(), "cleanup-billing", "pw123", "cleanup-billing@example.com", "user", "starter")
	if err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}

	if _, err := cfg.CreateCheckoutSession(context.Background(), user.Username, "starter"); err != nil {
		t.Fatalf("CreateCheckoutSession() error = %v", err)
	}
	if err := cfg.UnregisterUser(context.Background(), user.Username); err != nil {
		t.Fatalf("UnregisterUser() error = %v", err)
	}

	subscriptions, err := cfg.ensureSubscriptionStore().ListByUser(context.Background(), user.Username)
	if err != nil {
		t.Fatalf("ListByUser() subscriptions error = %v", err)
	}
	if len(subscriptions) != 0 {
		t.Fatalf("expected zero subscriptions after unregister, got %d", len(subscriptions))
	}

	payments, err := cfg.ensurePaymentStore().ListByUser(context.Background(), user.Username)
	if err != nil {
		t.Fatalf("ListByUser() payments error = %v", err)
	}
	if len(payments) != 0 {
		t.Fatalf("expected zero payments after unregister, got %d", len(payments))
	}
}

func TestAccountTransaction_RollsBackPartialWrites(t *testing.T) {
	cfg := Config{UsersStore: NewUserStore(), SubscriptionsStore: NewSubscriptionStore(), PaymentsStore: NewPaymentStore()}
	tx, err := cfg.BeginAccountTransaction()
	if err != nil {
		t.Fatalf("BeginAccountTransaction() error = %v", err)
	}

	user := &UserRecord{Username: "tx-user", Role: "user", PasswordHash: SecuredString("hash"), Status: "active"}
	if err := tx.SaveUser(user); err != nil {
		t.Fatalf("SaveUser() error = %v", err)
	}
	if err := tx.SaveSubscription(&SubscriptionRecord{ID: "sub_tx_1", UserID: user.Username, PlanCode: "starter", Status: "pending"}); err != nil {
		t.Fatalf("SaveSubscription() error = %v", err)
	}
	if err := tx.SavePayment(&PaymentRecord{ID: "pay_tx_1", UserID: user.Username, Amount: 5, Currency: "USD", Status: "pending"}); err != nil {
		t.Fatalf("SavePayment() error = %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	if _, ok, err := cfg.findUserRecord(context.Background(), user.Username); err != nil {
		t.Fatalf("findUserRecord() error = %v", err)
	} else if ok {
		t.Fatal("expected user to be rolled back")
	}
	if subscriptions, err := cfg.ensureSubscriptionStore().ListByUser(context.Background(), user.Username); err != nil {
		t.Fatalf("ListByUser() subscriptions error = %v", err)
	} else if len(subscriptions) != 0 {
		t.Fatalf("expected zero subscriptions after rollback, got %d", len(subscriptions))
	}
	if payments, err := cfg.ensurePaymentStore().ListByUser(context.Background(), user.Username); err != nil {
		t.Fatalf("ListByUser() payments error = %v", err)
	} else if len(payments) != 0 {
		t.Fatalf("expected zero payments after rollback, got %d", len(payments))
	}
}

type noopTransaction struct{}

func (noopTransaction) Begin(context.Context) error                           { return nil }
func (noopTransaction) Commit(context.Context) error                          { return nil }
func (noopTransaction) Rollback(context.Context) error                        { return nil }
func (noopTransaction) HasBegun() bool                                        { return true }
func (noopTransaction) GetPhasedTransaction() sop.TwoPhaseCommitTransaction   { return nil }
func (noopTransaction) AddPhasedTransaction(...sop.TwoPhaseCommitTransaction) {}
func (noopTransaction) GetStores(context.Context) ([]string, error)           { return nil, nil }
func (noopTransaction) Close() error                                          { return nil }
func (noopTransaction) GetID() sop.UUID                                       { return sop.UUID{} }
func (noopTransaction) CommitMaxDuration() time.Duration                      { return 0 }
func (noopTransaction) OnCommit(func(context.Context) error)                  {}

func TestAccountRepository_ProvidesWorkflowCRUDSurface(t *testing.T) {
	cfg := Config{UsersStore: NewUserStore(), SubscriptionsStore: NewSubscriptionStore(), PaymentsStore: NewPaymentStore()}
	repo := NewAccountRepository(&cfg)
	tx := noopTransaction{}

	user := &UserRecord{Username: "repo-user", Role: "user", PasswordHash: SecuredString("hash"), Status: "active"}
	if err := repo.SaveUser(context.Background(), tx, user); err != nil {
		t.Fatalf("SaveUser() error = %v", err)
	}

	subscription := &SubscriptionRecord{ID: "sub_repo_1", UserID: user.Username, PlanCode: "starter", Status: "pending"}
	if err := repo.SaveSubscription(context.Background(), tx, subscription); err != nil {
		t.Fatalf("SaveSubscription() error = %v", err)
	}

	payment := &PaymentRecord{ID: "pay_repo_1", UserID: user.Username, Amount: 5, Currency: "USD", Status: "pending"}
	if err := repo.SavePayment(context.Background(), tx, payment); err != nil {
		t.Fatalf("SavePayment() error = %v", err)
	}

	storedUser, ok, err := repo.FindUser(context.Background(), tx, user.Username)
	if err != nil {
		t.Fatalf("FindUser() error = %v", err)
	}
	if !ok || storedUser == nil {
		t.Fatal("FindUser() returned no user")
	}

	subscriptions, err := repo.ListSubscriptionsByUser(context.Background(), tx, user.Username)
	if err != nil {
		t.Fatalf("ListSubscriptionsByUser() error = %v", err)
	}
	if len(subscriptions) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subscriptions))
	}

	payments, err := repo.ListPaymentsByUser(context.Background(), tx, user.Username)
	if err != nil {
		t.Fatalf("ListPaymentsByUser() error = %v", err)
	}
	if len(payments) != 1 {
		t.Fatalf("expected 1 payment, got %d", len(payments))
	}

	if err := repo.DeletePayment(context.Background(), tx, payment.ID); err != nil {
		t.Fatalf("DeletePayment() error = %v", err)
	}
	if err := repo.DeleteSubscription(context.Background(), tx, subscription.ID); err != nil {
		t.Fatalf("DeleteSubscription() error = %v", err)
	}
	if err := repo.DeleteUser(context.Background(), tx, user.Username); err != nil {
		t.Fatalf("DeleteUser() error = %v", err)
	}

	if _, ok, err := repo.FindUser(context.Background(), tx, user.Username); err != nil {
		t.Fatalf("FindUser() after delete error = %v", err)
	} else if ok {
		t.Fatal("expected user to be removed")
	}
}

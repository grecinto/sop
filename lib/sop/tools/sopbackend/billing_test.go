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

func TestCollectScheduledBilling_ProcessesDueAccountsWithSuccessAndFailure(t *testing.T) {
	cfg := Config{UsersStore: NewUserStore(), SubscriptionsStore: NewSubscriptionStore(), PaymentsStore: NewPaymentStore()}
	userA, err := cfg.RegisterUser(context.Background(), "monthly-success", "pw123", "monthly-success@example.com", "user", "starter")
	if err != nil {
		t.Fatalf("RegisterUser(success) error = %v", err)
	}
	userB, err := cfg.RegisterUser(context.Background(), "monthly-fail", "pw123", "monthly-fail@example.com", "user", "starter")
	if err != nil {
		t.Fatalf("RegisterUser(fail) error = %v", err)
	}

	userA.BillingStatus = "active"
	userA.BillingNotice = ""
	userA.UpdatedAt = time.Date(2026, time.July, 1, 9, 0, 0, 0, time.UTC)
	if err := cfg.ensureUserStore().Save(context.Background(), userA); err != nil {
		t.Fatalf("Save(success user) error = %v", err)
	}
	for i := range cfg.Users {
		if cfg.Users[i].Username == userA.Username {
			cfg.Users[i] = *userA
			break
		}
	}

	userB.BillingStatus = "active"
	userB.BillingNotice = ""
	userB.UpdatedAt = time.Date(2026, time.July, 1, 9, 0, 0, 0, time.UTC)
	if err := cfg.ensureUserStore().Save(context.Background(), userB); err != nil {
		t.Fatalf("Save(fail user) error = %v", err)
	}
	for i := range cfg.Users {
		if cfg.Users[i].Username == userB.Username {
			cfg.Users[i] = *userB
			break
		}
	}

	if _, err := cfg.SetBillingDay(context.Background(), userA.Username, 15); err != nil {
		t.Fatalf("SetBillingDay(success) error = %v", err)
	}
	if _, err := cfg.SetBillingDay(context.Background(), userB.Username, 15); err != nil {
		t.Fatalf("SetBillingDay(fail) error = %v", err)
	}

	if _, err := cfg.SetBillingCollectionMode(context.Background(), userB.Username, "fail"); err != nil {
		t.Fatalf("SetBillingCollectionMode() error = %v", err)
	}

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	results, err := cfg.CollectScheduledBilling(context.Background(), now, 10)
	if err != nil {
		t.Fatalf("CollectScheduledBilling() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 billing collection results, got %d", len(results))
	}

	updatedA, ok, err := cfg.FindUserRecord(context.Background(), userA.Username)
	if err != nil {
		t.Fatalf("FindUserRecord(success) error = %v", err)
	}
	if !ok || updatedA == nil {
		t.Fatal("FindUserRecord(success) returned no user")
	}
	if updatedA.BillingStatus != "active" {
		t.Fatalf("expected success account to stay active, got %q", updatedA.BillingStatus)
	}

	updatedB, ok, err := cfg.FindUserRecord(context.Background(), userB.Username)
	if err != nil {
		t.Fatalf("FindUserRecord(fail) error = %v", err)
	}
	if !ok || updatedB == nil {
		t.Fatal("FindUserRecord(fail) returned no user")
	}
	if updatedB.BillingStatus != "on_hold" {
		t.Fatalf("expected fail account to be placed on hold, got %q", updatedB.BillingStatus)
	}

	payments, err := cfg.ensurePaymentStore().ListByUser(context.Background(), userA.Username)
	if err != nil {
		t.Fatalf("ListByUser(success payments) error = %v", err)
	}
	if len(payments) != 1 {
		t.Fatalf("expected one payment for success account, got %d", len(payments))
	}
	if payments[0].Status != "succeeded" {
		t.Fatalf("expected success payment status succeeded, got %q", payments[0].Status)
	}

	failPayments, err := cfg.ensurePaymentStore().ListByUser(context.Background(), userB.Username)
	if err != nil {
		t.Fatalf("ListByUser(fail payments) error = %v", err)
	}
	if len(failPayments) != 1 {
		t.Fatalf("expected one payment for fail account, got %d", len(failPayments))
	}
	if failPayments[0].Status != "failed" {
		t.Fatalf("expected fail payment status failed, got %q", failPayments[0].Status)
	}
}

func TestCollectScheduledBilling_ResumesFromPersistedCursorAfterFailure(t *testing.T) {
	cfg := Config{UsersStore: NewUserStore(), SubscriptionsStore: NewSubscriptionStore(), PaymentsStore: NewPaymentStore()}
	userA, err := cfg.RegisterUser(context.Background(), "resume-a", "pw123", "resume-a@example.com", "user", "starter")
	if err != nil {
		t.Fatalf("RegisterUser(A) error = %v", err)
	}
	userB, err := cfg.RegisterUser(context.Background(), "resume-b", "pw123", "resume-b@example.com", "user", "starter")
	if err != nil {
		t.Fatalf("RegisterUser(B) error = %v", err)
	}

	for _, user := range []*UserRecord{userA, userB} {
		user.BillingStatus = "active"
		user.BillingNotice = ""
		user.UpdatedAt = time.Date(2026, time.July, 1, 9, 0, 0, 0, time.UTC)
		if err := cfg.ensureUserStore().Save(context.Background(), user); err != nil {
			t.Fatalf("Save(%s) error = %v", user.Username, err)
		}
		for i := range cfg.Users {
			if cfg.Users[i].Username == user.Username {
				cfg.Users[i] = *user
				break
			}
		}
	}
	if _, err := cfg.SetBillingDay(context.Background(), userA.Username, 15); err != nil {
		t.Fatalf("SetBillingDay(A) error = %v", err)
	}
	if _, err := cfg.SetBillingDay(context.Background(), userB.Username, 15); err != nil {
		t.Fatalf("SetBillingDay(B) error = %v", err)
	}

	userA.BillingCollectionCursor = userA.Username
	if err := cfg.ensureUserStore().Save(context.Background(), userA); err != nil {
		t.Fatalf("Save(cursor for A) error = %v", err)
	}
	for i := range cfg.Users {
		if cfg.Users[i].Username == userA.Username {
			cfg.Users[i].BillingCollectionCursor = userA.Username
			cfg.Users[i].UpdatedAt = time.Now().UTC()
			break
		}
	}

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	restartedCfg := Config{UsersStore: cfg.UsersStore, SubscriptionsStore: cfg.SubscriptionsStore, PaymentsStore: cfg.PaymentsStore, Users: cfg.Users}
	resumedResults, err := restartedCfg.CollectScheduledBilling(context.Background(), now, 1)
	if err != nil {
		t.Fatalf("resumed CollectScheduledBilling() error = %v", err)
	}
	if len(resumedResults) != 1 {
		t.Fatalf("expected resumed batch to process one account, got %d", len(resumedResults))
	}
	if resumedResults[0]["username"] != userA.Username {
		t.Fatalf("expected resumed batch to continue with %q, got %#v", userA.Username, resumedResults[0]["username"])
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

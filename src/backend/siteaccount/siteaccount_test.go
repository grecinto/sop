package siteaccount

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/sharedcode/sop"
)

func uniqueTestUsername(t *testing.T, prefix string) string {
	t.Helper()
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func TestRegisterUserWithRequestPersistsUserAndBillingState(t *testing.T) {
	cfg := NewConfig()
	username := uniqueTestUsername(t, "site-account-user")
	user, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
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

	found, ok, err := cfg.FindUserRecord(context.Background(), username)
	if err != nil {
		t.Fatalf("FindUserRecord() error = %v", err)
	}
	if !ok || found == nil {
		t.Fatal("FindUserRecord() did not find created user")
	}
	if found.Username != username {
		t.Fatalf("FindUserRecord() username = %q, want %q", found.Username, username)
	}
}

func TestStoreBatchGetAndDeleteSupportSlices(t *testing.T) {
	cfg := NewConfig()
	ctx := context.Background()

	db, err := getSiteAccountDB(ctx)
	if err != nil {
		t.Fatalf("getSiteAccountDB() error = %v", err)
	}
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	defer tx.Rollback(ctx)

	username := uniqueTestUsername(t, "batch-store-user")
	userStore, err := cfg.ensureUserStore(ctx, tx)
	if err != nil {
		t.Fatalf("ensureUserStore() error = %v", err)
	}
	if err := userStore.Upsert(ctx, []UserRecord{{Username: username, Role: "user", Status: "active"}}); err != nil {
		t.Fatalf("UpsertBatch user error = %v", err)
	}
	users, err := userStore.Fetch(ctx, []string{username, "missing-user"})
	if err != nil {
		t.Fatalf("Get(users) error = %v", err)
	}
	if len(users) != 1 || users[0].Username != username {
		t.Fatalf("Get(users) = %+v, want a single record for %q", users, username)
	}

	subscriptionStore, err := cfg.ensureSubscriptionStore(ctx, tx)
	if err != nil {
		t.Fatalf("ensureSubscriptionStore() error = %v", err)
	}
	subAID := fmt.Sprintf("sub-batch-a-%d", time.Now().UnixNano())
	subBID := fmt.Sprintf("sub-batch-b-%d", time.Now().UnixNano())
	if err := subscriptionStore.Upsert(ctx, []SubscriptionRecord{{ID: subAID, UserID: username, PlanCode: "starter", Status: "active"}, {ID: subBID, UserID: username, PlanCode: "pro", Status: "active"}}); err != nil {
		t.Fatalf("UpsertBatch subscriptions error = %v", err)
	}
	foundSubscriptions, err := subscriptionStore.Fetch(ctx, []string{subAID, "missing-sub", subBID})
	if err != nil {
		t.Fatalf("Get(subscriptions) error = %v", err)
	}
	if len(foundSubscriptions) != 2 {
		t.Fatalf("Get(subscriptions) len = %d, want 2", len(foundSubscriptions))
	}
	if err := subscriptionStore.Delete(ctx, []string{subAID, "missing-sub"}); err != nil {
		t.Fatalf("Delete(subscriptions) error = %v", err)
	}
	remainingSubscriptions, err := subscriptionStore.Fetch(ctx, []string{subAID, subBID})
	if err != nil {
		t.Fatalf("Get(subscriptions after delete) error = %v", err)
	}
	if len(remainingSubscriptions) != 1 || remainingSubscriptions[0].ID != subBID {
		t.Fatalf("Get(subscriptions after delete) = %+v, want only %q", remainingSubscriptions, subBID)
	}
}

func TestUserBillingAndPaymentIndexStores(t *testing.T) {
	cfg := NewConfig()
	ctx := context.Background()

	db, err := getSiteAccountDB(ctx)
	if err != nil {
		t.Fatalf("getSiteAccountDB() error = %v", err)
	}
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	defer tx.Rollback(ctx)

	username := uniqueTestUsername(t, "indexed-user")
	user := &UserRecord{Username: username, Role: "user", Email: "indexed@example.com", BillingStatus: "active", Status: "active"}
	userStore, err := cfg.ensureUserStore(ctx, tx)
	if err != nil {
		t.Fatalf("ensureUserStore() error = %v", err)
	}
	if err := userStore.Upsert(ctx, []UserRecord{*user}); err != nil {
		t.Fatalf("UpsertBatch user error = %v", err)
	}

	subscriptionID := fmt.Sprintf("sub-idx-%d", time.Now().UnixNano())
	subscription := &SubscriptionRecord{ID: subscriptionID, UserID: username, PlanCode: "starter", Status: "active"}
	subscriptionStore, err := cfg.ensureSubscriptionStore(ctx, tx)
	if err != nil {
		t.Fatalf("ensureSubscriptionStore() error = %v", err)
	}
	if err := subscriptionStore.Upsert(ctx, []SubscriptionRecord{*subscription}); err != nil {
		t.Fatalf("UpsertBatch subscription error = %v", err)
	}

	paymentID := fmt.Sprintf("pay-idx-%d", time.Now().UnixNano())
	payment := &PaymentRecord{ID: paymentID, UserID: username, SubscriptionID: subscriptionID, Amount: 100, Currency: "USD", Status: "succeeded"}
	paymentStore, err := cfg.ensurePaymentStore(ctx, tx)
	if err != nil {
		t.Fatalf("ensurePaymentStore() error = %v", err)
	}
	if err := paymentStore.Upsert(ctx, []PaymentRecord{*payment}); err != nil {
		t.Fatalf("UpsertBatch payment error = %v", err)
	}

	userSubscriptionIndexStore, err := cfg.ensureUserSubscriptionIndexStore(ctx, tx)
	if err != nil {
		t.Fatalf("ensureUserSubscriptionIndexStore() error = %v", err)
	}
	secondarySubscriptionID := fmt.Sprintf("sub-idx-secondary-%d", time.Now().UnixNano())
	if err := userSubscriptionIndexStore.Upsert(ctx, []UserSubscriptionEntry{{Username: username, SubscriptionID: subscriptionID}, {Username: username, SubscriptionID: secondarySubscriptionID}}); err != nil {
		t.Fatalf("UpsertBatch user subscription index error = %v", err)
	}
	if err := userSubscriptionIndexStore.Delete(ctx, []UserEntityIDs{{Username: username, EntityIDs: []string{subscriptionID, "missing-subscription"}}}); err != nil {
		t.Fatalf("Delete(user subscription index) error = %v", err)
	}
	remainingSubscriptionIDs, err := userSubscriptionIndexStore.ListIDs(ctx, username)
	if err != nil {
		t.Fatalf("ListIDs(user subscription index) error = %v", err)
	}
	if !reflect.DeepEqual(remainingSubscriptionIDs, []string{secondarySubscriptionID}) {
		t.Fatalf("remaining subscription IDs = %v, want %v", remainingSubscriptionIDs, []string{secondarySubscriptionID})
	}
	userPaymentIndexStore, err := cfg.ensureUserPaymentIndexStore(ctx, tx)
	if err != nil {
		t.Fatalf("ensureUserPaymentIndexStore() error = %v", err)
	}
	secondaryPaymentID := fmt.Sprintf("pay-idx-secondary-%d", time.Now().UnixNano())
	if err := userPaymentIndexStore.Upsert(ctx, []UserPaymentEntry{{Username: username, PaymentID: paymentID}, {Username: username, PaymentID: secondaryPaymentID}}); err != nil {
		t.Fatalf("UpsertBatch user payment index error = %v", err)
	}
	if err := userPaymentIndexStore.Delete(ctx, []UserEntityIDs{{Username: username, EntityIDs: []string{paymentID, "missing-payment"}}}); err != nil {
		t.Fatalf("Delete(user payment index) error = %v", err)
	}
	remainingPaymentIDs, err := userPaymentIndexStore.ListIDs(ctx, username)
	if err != nil {
		t.Fatalf("ListIDs(user payment index) error = %v", err)
	}
	if !reflect.DeepEqual(remainingPaymentIDs, []string{secondaryPaymentID}) {
		t.Fatalf("remaining payment IDs = %v, want %v", remainingPaymentIDs, []string{secondaryPaymentID})
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	rtx, err := db.BeginTransaction(ctx, sop.ForReading, -1)
	if err != nil {
		t.Fatalf("BeginTransaction(read) error = %v", err)
	}
	defer rtx.Rollback(ctx)

	ruserSubscriptionIndexStore, err := cfg.ensureUserSubscriptionIndexStore(ctx, rtx)
	if err != nil {
		t.Fatalf("ensureUserSubscriptionIndexStore(read) error = %v", err)
	}
	subIDs, err := ruserSubscriptionIndexStore.ListIDs(ctx, username)
	if err != nil {
		t.Fatalf("List subscription IDs error = %v", err)
	}
	if !reflect.DeepEqual(subIDs, []string{secondarySubscriptionID}) {
		t.Fatalf("subscription IDs = %v, want %v", subIDs, []string{secondarySubscriptionID})
	}

	ruserPaymentIndexStore, err := cfg.ensureUserPaymentIndexStore(ctx, rtx)
	if err != nil {
		t.Fatalf("ensureUserPaymentIndexStore(read) error = %v", err)
	}
	payIDs, err := ruserPaymentIndexStore.ListIDs(ctx, username)
	if err != nil {
		t.Fatalf("List payment IDs error = %v", err)
	}
	if !reflect.DeepEqual(payIDs, []string{secondaryPaymentID}) {
		t.Fatalf("payment IDs = %v, want %v", payIDs, []string{secondaryPaymentID})
	}
}

func TestSiteAccountStoresPersistAcrossFreshConfigInstances(t *testing.T) {
	cfg := NewConfig()
	username := uniqueTestUsername(t, "persisted-site-user")
	if _, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "persisted@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	}); err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}

	db, err := getSiteAccountDB(context.Background())
	if err != nil {
		t.Fatalf("getSiteAccountDB() error = %v", err)
	}
	tx, err := db.BeginTransaction(context.Background(), sop.ForWriting, -1)
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	defer tx.Rollback(context.Background())

	subscriptionID := fmt.Sprintf("sub-%d", time.Now().UnixNano())
	paymentID := fmt.Sprintf("pay-%d", time.Now().UnixNano())
	subscriptionStore, err := cfg.ensureSubscriptionStore(context.Background(), tx)
	if err != nil {
		t.Fatalf("ensureSubscriptionStore() error = %v", err)
	}
	if err := subscriptionStore.Upsert(context.Background(), []SubscriptionRecord{{
		ID:                   subscriptionID,
		UserID:               username,
		PlanCode:             "starter",
		Status:               "active",
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
		CurrentPeriodStart:   time.Now().UTC(),
		CurrentPeriodEnd:     time.Now().UTC().Add(24 * time.Hour),
		CancelAtPeriodEnd:    false,
		ProviderSubscription: "sub_provider_1",
	}}); err != nil {
		t.Fatalf("Save subscription error = %v", err)
	}

	paymentStore, err := cfg.ensurePaymentStore(context.Background(), tx)
	if err != nil {
		t.Fatalf("ensurePaymentStore() error = %v", err)
	}
	if err := paymentStore.Upsert(context.Background(), []PaymentRecord{{
		ID:                paymentID,
		UserID:            username,
		SubscriptionID:    subscriptionID,
		ProviderPaymentID: "pay_provider_1",
		Amount:            500,
		Currency:          "USD",
		Status:            "succeeded",
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("UpsertBatch payment error = %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	reloaded := NewConfig()
	found, ok, err := reloaded.FindUserRecord(context.Background(), username)
	if err != nil {
		t.Fatalf("FindUserRecord() after reload error = %v", err)
	}
	if !ok || found == nil {
		t.Fatal("expected persisted user record to be available after reload")
	}

	db, err = getSiteAccountDB(context.Background())
	if err != nil {
		t.Fatalf("getSiteAccountDB(read) error = %v", err)
	}
	rtx, err := db.BeginTransaction(context.Background(), sop.ForReading, -1)
	if err != nil {
		t.Fatalf("BeginTransaction(read) error = %v", err)
	}
	defer rtx.Rollback(context.Background())

	subscriptionStore, err = reloaded.ensureSubscriptionStore(context.Background(), rtx)
	if err != nil {
		t.Fatalf("ensureSubscriptionStore() error = %v", err)
	}
	subscriptionsByUser, err := subscriptionStore.FetchByUser(context.Background(), []string{username})
	if err != nil {
		t.Fatalf("ListByUser subscriptions error = %v", err)
	}
	if len(subscriptionsByUser) != 1 {
		t.Fatalf("expected 1 subscription group after reload, got %d", len(subscriptionsByUser))
	}
	if len(subscriptionsByUser[0].Value) != 1 {
		t.Fatalf("expected 1 subscription after reload, got %d", len(subscriptionsByUser[0].Value))
	}

	paymentStore, err = reloaded.ensurePaymentStore(context.Background(), rtx)
	if err != nil {
		t.Fatalf("ensurePaymentStore() error = %v", err)
	}
	paymentsByUser, err := paymentStore.FetchByUser(context.Background(), []string{username})
	if err != nil {
		t.Fatalf("ListByUser payments error = %v", err)
	}
	if len(paymentsByUser) != 1 {
		t.Fatalf("expected 1 payment group after reload, got %d", len(paymentsByUser))
	}
	if len(paymentsByUser[0].Value) != 1 {
		t.Fatalf("expected 1 payment after reload, got %d", len(paymentsByUser[0].Value))
	}
}

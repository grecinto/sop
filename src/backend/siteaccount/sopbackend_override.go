package siteaccount

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/tools/sopbackend"
)

// SopbackendUserStoreAdapter bridges the site account persistence layer to the
// shared sopbackend user-store interface so setup-wizard and auth flows can use
// the site's persisted user management.
type SopbackendUserStoreAdapter struct {
	cfg *Config
}

// NewSopbackendUserStoreAdapter creates a sopbackend user-store adapter backed by
// the site account persistence layer.
func NewSopbackendUserStoreAdapter(cfg *Config) *SopbackendUserStoreAdapter {
	return &SopbackendUserStoreAdapter{cfg: cfg}
}

// InstallSopbackendUserStoreOverride installs the site-backed user-store adapter
// into the shared sopbackend runtime so the new site can own user persistence.
func InstallSopbackendUserStoreOverride() {
	sopbackend.SetUserStoreOverride(NewSopbackendUserStoreAdapter(GetConfig()))
}

func (a *SopbackendUserStoreAdapter) Save(ctx context.Context, user *sopbackend.UserRecord) error {
	if a == nil || a.cfg == nil || user == nil {
		return fmt.Errorf("site user store adapter is not initialized")
	}
	if normalizeUsername(user.Username) == "" {
		return fmt.Errorf("username is required")
	}

	db, err := getSiteAccountDB(ctx)
	if err != nil {
		return err
	}
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	store, err := a.cfg.ensureUserStore(ctx, tx)
	if err != nil {
		return err
	}
	siteUser := convertSopbackendUserRecord(user)
	if err := store.Upsert(ctx, []UserRecord{siteUser}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	upsertSiteUserRecord(a.cfg, siteUser)
	return nil
}

func (a *SopbackendUserStoreAdapter) Get(ctx context.Context, username string) (*sopbackend.UserRecord, bool, error) {
	if a == nil || a.cfg == nil {
		return nil, false, fmt.Errorf("site user store adapter is not initialized")
	}
	db, err := getSiteAccountDB(ctx)
	if err != nil {
		return nil, false, err
	}
	tx, err := db.BeginTransaction(ctx, sop.ForReading, -1)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)

	store, err := a.cfg.ensureUserStore(ctx, tx)
	if err != nil {
		return nil, false, err
	}
	users, err := store.Fetch(ctx, []string{username})
	if err != nil {
		return nil, false, err
	}
	if len(users) > 0 {
		converted := convertSiteUserRecord(users[0])
		return &converted, true, nil
	}
	for _, existing := range a.cfg.Users {
		if normalizeUsername(existing.Username) == normalizeUsername(username) {
			converted := convertSiteUserRecord(existing)
			return &converted, true, nil
		}
	}
	return nil, false, nil
}

func (a *SopbackendUserStoreAdapter) Delete(ctx context.Context, username string) error {
	if a == nil || a.cfg == nil {
		return fmt.Errorf("site user store adapter is not initialized")
	}
	db, err := getSiteAccountDB(ctx)
	if err != nil {
		return err
	}
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	store, err := a.cfg.ensureUserStore(ctx, tx)
	if err != nil {
		return err
	}
	if err := store.Delete(ctx, []string{username}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	removeSiteUserRecord(a.cfg, username)
	return nil
}

func (a *SopbackendUserStoreAdapter) List(ctx context.Context) ([]sopbackend.UserRecord, error) {
	if a == nil || a.cfg == nil {
		return nil, fmt.Errorf("site user store adapter is not initialized")
	}
	db, err := getSiteAccountDB(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTransaction(ctx, sop.ForReading, -1)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	store, err := a.cfg.ensureUserStore(ctx, tx)
	if err != nil {
		return nil, err
	}
	users, err := store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]sopbackend.UserRecord, 0, len(users))
	for _, user := range users {
		out = append(out, convertSiteUserRecord(user))
	}
	if len(out) == 0 && len(a.cfg.Users) > 0 {
		for _, user := range a.cfg.Users {
			out = append(out, convertSiteUserRecord(user))
		}
	}
	return out, nil
}

func convertSiteUserRecord(user UserRecord) sopbackend.UserRecord {
	return sopbackend.UserRecord{
		Username:             user.Username,
		Role:                 user.Role,
		PasswordHash:         sopbackend.SecuredString(user.PasswordHash),
		Email:                sopbackend.SecuredString(user.Email),
		SubscriptionPlan:     user.SubscriptionPlan,
		SubscriptionFeatures: append([]string(nil), user.SubscriptionFeatures...),
		BillingStatus:        user.BillingStatus,
		CheckoutURL:          user.CheckoutURL,
		LastWebhookEvent:     user.LastWebhookEvent,
		LastWebhookAt:        user.LastWebhookAt,
		FailureReason:        user.FailureReason,
		StripeCustomerID:     sopbackend.SecuredString(user.StripeCustomerID),
		StripeSubID:          sopbackend.SecuredString(user.StripeSubID),
		Status:               user.Status,
		CreatedAt:            user.CreatedAt,
		UpdatedAt:            user.UpdatedAt,
	}
}

func convertSopbackendUserRecord(user *sopbackend.UserRecord) UserRecord {
	if user == nil {
		return UserRecord{}
	}
	return UserRecord{
		Username:             strings.TrimSpace(user.Username),
		Role:                 user.Role,
		PasswordHash:         string(user.PasswordHash),
		Email:                string(user.Email),
		SubscriptionPlan:     user.SubscriptionPlan,
		SubscriptionFeatures: append([]string(nil), user.SubscriptionFeatures...),
		BillingStatus:        user.BillingStatus,
		CheckoutURL:          user.CheckoutURL,
		LastWebhookEvent:     user.LastWebhookEvent,
		LastWebhookAt:        user.LastWebhookAt,
		FailureReason:        user.FailureReason,
		StripeCustomerID:     string(user.StripeCustomerID),
		StripeSubID:          string(user.StripeSubID),
		Status:               user.Status,
		CreatedAt:            user.CreatedAt,
		UpdatedAt:            user.UpdatedAt,
	}
}

func upsertSiteUserRecord(cfg *Config, user UserRecord) {
	if cfg == nil {
		return
	}
	for i := range cfg.Users {
		if normalizeUsername(cfg.Users[i].Username) == normalizeUsername(user.Username) {
			cfg.Users[i] = user
			return
		}
	}
	cfg.Users = append(cfg.Users, user)
}

func removeSiteUserRecord(cfg *Config, username string) {
	if cfg == nil {
		return
	}
	username = normalizeUsername(username)
	filtered := cfg.Users[:0]
	for _, existing := range cfg.Users {
		if normalizeUsername(existing.Username) == username {
			continue
		}
		filtered = append(filtered, existing)
	}
	cfg.Users = filtered
}

package siteaccount

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sharedcode/sop"
	aidb "github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/btree"
	"github.com/sharedcode/sop/common"
	"github.com/sharedcode/sop/database"
	"github.com/sharedcode/sop/tools/sopbackend"
	"golang.org/x/crypto/bcrypt"
)

var (
	configMu          sync.RWMutex
	globalConfig      *Config
	siteAccountDBMu   sync.RWMutex
	siteAccountDBInst *aidb.Database
)

const (
	userStoreName                  = "site_users"
	subscriptionStoreName          = "site_subscriptions"
	paymentStoreName               = "site_payments"
	userSubscriptionIndexStoreName = "site_user_subscription_ids"
	userPaymentIndexStoreName      = "site_user_payment_ids"
)

// Config stores the site-specific account state for the React/site app.
type Config struct {
	Users                      []UserRecord
	Subscriptions              []SubscriptionRecord
	Payments                   []PaymentRecord
	UsersStore                 UserStore
	SubscriptionsStore         SubscriptionStore
	PaymentsStore              PaymentStore
	UserSubscriptionIndexStore UserSubscriptionStore
	UserPaymentIndexStore      UserPaymentStore
}

// UserRecord stores minimal account, billing, and role data for the site app.
type UserRecord struct {
	Username             string    `json:"username"`
	Role                 string    `json:"role"`
	PasswordHash         string    `json:"password_hash,omitempty"`
	Email                string    `json:"email,omitempty"`
	SubscriptionPlan     string    `json:"subscription_plan,omitempty"`
	SubscriptionFeatures []string  `json:"subscription_features,omitempty"`
	BillingStatus        string    `json:"billing_status,omitempty"`
	CheckoutURL          string    `json:"checkout_url,omitempty"`
	LastWebhookEvent     string    `json:"last_webhook_event,omitempty"`
	LastWebhookAt        time.Time `json:"last_webhook_at,omitempty"`
	FailureReason        string    `json:"failure_reason,omitempty"`
	RequestOrigin        string    `json:"request_origin,omitempty"`
	StripeCustomerID     string    `json:"stripe_customer_id,omitempty"`
	StripeSubID          string    `json:"stripe_subscription_id,omitempty"`
	Status               string    `json:"status,omitempty"`
	CreatedAt            time.Time `json:"created_at,omitempty"`
	UpdatedAt            time.Time `json:"updated_at,omitempty"`
}

// RegisterUserRequest is the explicit registration contract for site account flows.
type RegisterUserRequest struct {
	Username             string
	Password             string
	Email                string
	Role                 string
	SubscriptionPlan     string
	SubscriptionFeatures []string
	RequestOrigin        string
}

// UnregisterUserRequest removes a site account by username.
type UnregisterUserRequest struct {
	Username string
}

// UpdateSubscriptionPlanRequest updates the plan for a site account.
type UpdateSubscriptionPlanRequest struct {
	Username             string
	SubscriptionPlan     string
	SubscriptionFeatures []string
	RequestOrigin        string
}

// SubscriptionRecord tracks billing subscriptions for a site account.
type SubscriptionRecord struct {
	ID                   string    `json:"id"`
	UserID               string    `json:"user_id"`
	PlanCode             string    `json:"plan_code"`
	ProviderSubscription string    `json:"provider_subscription_id,omitempty"`
	Status               string    `json:"status"`
	CurrentPeriodStart   time.Time `json:"current_period_start,omitempty"`
	CurrentPeriodEnd     time.Time `json:"current_period_end,omitempty"`
	CancelAtPeriodEnd    bool      `json:"cancel_at_period_end,omitempty"`
	CreatedAt            time.Time `json:"created_at,omitempty"`
	UpdatedAt            time.Time `json:"updated_at,omitempty"`
}

// PaymentRecord tracks billing payments for a site account.
type PaymentRecord struct {
	ID                string    `json:"id"`
	UserID            string    `json:"user_id"`
	SubscriptionID    string    `json:"subscription_id,omitempty"`
	ProviderPaymentID string    `json:"provider_payment_id,omitempty"`
	Amount            float64   `json:"amount"`
	Currency          string    `json:"currency,omitempty"`
	Status            string    `json:"status"`
	ProviderEventID   string    `json:"provider_event_id,omitempty"`
	CreatedAt         time.Time `json:"created_at,omitempty"`
	UpdatedAt         time.Time `json:"updated_at,omitempty"`
}

// BillingCheckoutSession is the site-specific checkout payload.
type BillingCheckoutSession struct {
	ID     string
	URL    string
	Status string
}

// BillingWebhookEvent represents a billing callback.
type BillingWebhookEvent struct {
	EventType string
	EventID   string
	UserID    string
	Plan      string
}

// UserStore persists user identity records.
type UserStore interface {
	Upsert(ctx context.Context, users []UserRecord) error
	Fetch(ctx context.Context, usernames []string) ([]UserRecord, error)
	Delete(ctx context.Context, usernames []string) error
	List(ctx context.Context) ([]UserRecord, error)
}

// SubscriptionStore persists subscription records.
type SubscriptionStore interface {
	Upsert(ctx context.Context, subscriptions []SubscriptionRecord) error
	Fetch(ctx context.Context, ids []string) ([]SubscriptionRecord, error)
	Delete(ctx context.Context, ids []string) error
	FetchByUser(ctx context.Context, username []string) ([]sop.KeyValuePair[string, []SubscriptionRecord], error)
	List(ctx context.Context) ([]SubscriptionRecord, error)
}

// PaymentStore persists payment records.
type PaymentStore interface {
	Upsert(ctx context.Context, payments []PaymentRecord) error
	Fetch(ctx context.Context, ids []string) ([]PaymentRecord, error)
	Delete(ctx context.Context, ids []string) error
	FetchByUser(ctx context.Context, username []string) ([]sop.KeyValuePair[string, []PaymentRecord], error)
	List(ctx context.Context) ([]PaymentRecord, error)
}

type UserEntityIDs struct {
	Username  string
	EntityIDs []string
}

// UserSubscriptionStore provides a user-scoped index of subscription IDs.
type UserSubscriptionStore interface {
	Upsert(ctx context.Context, entries []UserSubscriptionEntry) error
	ListIDs(ctx context.Context, username string) ([]string, error)
	Delete(ctx context.Context, userSubscriptions []UserEntityIDs) error
}

// UserPaymentStore provides a user-scoped index of payment IDs.
type UserPaymentStore interface {
	Upsert(ctx context.Context, entries []UserPaymentEntry) error
	ListIDs(ctx context.Context, username string) ([]string, error)
	Delete(ctx context.Context, userPayments []UserEntityIDs) error
}

type UserSubscriptionEntry struct {
	Username       string
	SubscriptionID string
}

type UserPaymentEntry struct {
	Username  string
	PaymentID string
}

func getSiteAccountDB(ctx context.Context) (*aidb.Database, error) {
	siteAccountDBMu.RLock()
	db := siteAccountDBInst
	siteAccountDBMu.RUnlock()
	if db != nil {
		return db, nil
	}

	siteAccountDBMu.Lock()
	defer siteAccountDBMu.Unlock()
	if siteAccountDBInst != nil {
		return siteAccountDBInst, nil
	}

	dbOpts, err := sopbackend.GetSystemDBOptions(ctx)
	if err != nil {
		return nil, err
	}
	siteAccountDBInst = aidb.NewDatabase(dbOpts)
	return siteAccountDBInst, nil
}

func newStringKeyedStore[T any](ctx context.Context, db *aidb.Database, tx sop.Transaction, storeName string) (btree.BtreeInterface[string, T], error) {
	if db == nil {
		return nil, fmt.Errorf("database is not initialized")
	}
	so := sop.ConfigureStore(storeName, true, btree.DefaultSlotLength, "", sop.SmallData, "")
	return database.NewBtree[string, T](ctx, db.Options(), storeName, tx, nil, so)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type relationshipIndexEntry struct {
	username string
	entityID string
}

type relationshipIndexStoreImpl struct {
	storeName string
	tx        sop.Transaction
	bridge    *common.SimpleBridgeStore[string, string]
}

func newSimpleRelationshipIndexStore(ctx context.Context, tx sop.Transaction, storeName string) (*relationshipIndexStoreImpl, error) {
	db, err := getSiteAccountDB(ctx)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, fmt.Errorf("transaction is required")
	}
	store, err := newStringKeyedStore[[]string](ctx, db, tx, storeName)
	if err != nil {
		return nil, err
	}
	return &relationshipIndexStoreImpl{storeName: storeName, tx: tx, bridge: common.NewSimpleBridgeStore[string, string](store)}, nil
}

func (s *relationshipIndexStoreImpl) upsert(ctx context.Context, entries []relationshipIndexEntry) error {
	if s == nil || s.bridge == nil {
		return fmt.Errorf("relationship index store is not initialized")
	}
	bridgeEntries := make([]common.BridgeEntry[string, string], 0, len(entries))
	for _, entry := range entries {
		username := normalizeUsername(entry.username)
		entityID := strings.TrimSpace(entry.entityID)
		if username == "" || entityID == "" {
			continue
		}
		bridgeEntries = append(bridgeEntries, common.BridgeEntry[string, string]{Key: username, Value: entityID})
	}
	return s.bridge.Upsert(ctx, bridgeEntries)
}

func (s *relationshipIndexStoreImpl) listIDs(ctx context.Context, username string) ([]string, error) {
	if s == nil || s.bridge == nil {
		return nil, nil
	}
	username = normalizeUsername(username)
	if username == "" {
		return nil, nil
	}
	return s.bridge.ListValues(ctx, username)
}

func (s *relationshipIndexStoreImpl) delete(ctx context.Context, userEntities []UserEntityIDs) error {
	if s == nil || s.bridge == nil {
		return nil
	}
	requests := make([]common.BridgeDeleteRequest[string, string], 0, len(userEntities))
	for _, userEntity := range userEntities {
		username := normalizeUsername(userEntity.Username)
		if username == "" || len(userEntity.EntityIDs) == 0 {
			continue
		}
		values := make([]string, 0, len(userEntity.EntityIDs))
		for _, entityID := range userEntity.EntityIDs {
			entityID = strings.TrimSpace(entityID)
			if entityID != "" {
				values = append(values, entityID)
			}
		}
		if len(values) > 0 {
			requests = append(requests, common.BridgeDeleteRequest[string, string]{Key: username, Values: values})
		}
	}
	return s.bridge.Delete(ctx, requests)
}

type userSubscriptionIndexStoreImpl struct {
	*relationshipIndexStoreImpl
}

func newUserSubscriptionIndexStore(ctx context.Context, tx sop.Transaction) (*userSubscriptionIndexStoreImpl, error) {
	store, err := newSimpleRelationshipIndexStore(ctx, tx, userSubscriptionIndexStoreName)
	if err != nil {
		return nil, err
	}
	return &userSubscriptionIndexStoreImpl{relationshipIndexStoreImpl: store}, nil
}

func (s *userSubscriptionIndexStoreImpl) Upsert(ctx context.Context, entries []UserSubscriptionEntry) error {
	normalized := make([]relationshipIndexEntry, 0, len(entries))
	for _, entry := range entries {
		username := normalizeUsername(entry.Username)
		entityID := strings.TrimSpace(entry.SubscriptionID)
		if username == "" || entityID == "" {
			continue
		}
		normalized = append(normalized, relationshipIndexEntry{username: username, entityID: entityID})
	}
	return s.relationshipIndexStoreImpl.upsert(ctx, normalized)
}

func (s *userSubscriptionIndexStoreImpl) ListIDs(ctx context.Context, username string) ([]string, error) {
	return s.relationshipIndexStoreImpl.listIDs(ctx, username)
}

func (s *userSubscriptionIndexStoreImpl) Delete(ctx context.Context, userSubscriptions []UserEntityIDs) error {
	return s.relationshipIndexStoreImpl.delete(ctx, userSubscriptions)
}

type userPaymentIndexStoreImpl struct {
	*relationshipIndexStoreImpl
}

func newUserPaymentIndexStore(ctx context.Context, tx sop.Transaction) (*userPaymentIndexStoreImpl, error) {
	store, err := newSimpleRelationshipIndexStore(ctx, tx, userPaymentIndexStoreName)
	if err != nil {
		return nil, err
	}
	return &userPaymentIndexStoreImpl{relationshipIndexStoreImpl: store}, nil
}

func (s *userPaymentIndexStoreImpl) Upsert(ctx context.Context, entries []UserPaymentEntry) error {
	normalized := make([]relationshipIndexEntry, 0, len(entries))
	for _, entry := range entries {
		username := normalizeUsername(entry.Username)
		entityID := strings.TrimSpace(entry.PaymentID)
		if username == "" || entityID == "" {
			continue
		}
		normalized = append(normalized, relationshipIndexEntry{username: username, entityID: entityID})
	}
	return s.relationshipIndexStoreImpl.upsert(ctx, normalized)
}

func (s *userPaymentIndexStoreImpl) ListIDs(ctx context.Context, username string) ([]string, error) {
	return s.relationshipIndexStoreImpl.listIDs(ctx, username)
}

func (s *userPaymentIndexStoreImpl) Delete(ctx context.Context, userPayments []UserEntityIDs) error {
	return s.relationshipIndexStoreImpl.delete(ctx, userPayments)
}

type userStoreImpl struct {
	storeName string
	tx        sop.Transaction
	tree      btree.BtreeInterface[string, UserRecord]
}

func newUserStore(ctx context.Context, tx sop.Transaction) (*userStoreImpl, error) {
	db, err := getSiteAccountDB(ctx)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, fmt.Errorf("transaction is required")
	}
	store, err := newStringKeyedStore[UserRecord](ctx, db, tx, userStoreName)
	if err != nil {
		return nil, err
	}
	return &userStoreImpl{storeName: userStoreName, tx: tx, tree: store}, nil
}

func (s *userStoreImpl) Upsert(ctx context.Context, users []UserRecord) error {
	if s == nil || s.tree == nil {
		return fmt.Errorf("user store is not initialized")
	}
	for _, user := range users {
		if user.Username == "" {
			continue
		}
		copyUser := user
		copyUser.Username = normalizeUsername(copyUser.Username)
		if copyUser.Username == "" {
			continue
		}
		copyUser.UpdatedAt = time.Now().UTC()
		if copyUser.CreatedAt.IsZero() {
			copyUser.CreatedAt = copyUser.UpdatedAt
		}
		if _, err := s.tree.Upsert(ctx, copyUser.Username, copyUser); err != nil {
			return err
		}
	}
	return nil
}

func (s *userStoreImpl) Fetch(ctx context.Context, usernames []string) ([]UserRecord, error) {
	if s == nil || s.tree == nil {
		return nil, nil
	}
	results := make([]UserRecord, 0, len(usernames))
	for _, username := range usernames {
		normalized := normalizeUsername(username)
		if normalized == "" {
			continue
		}
		found, err := s.tree.Find(ctx, normalized, true)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		value, err := s.tree.GetCurrentValue(ctx)
		if err != nil {
			return nil, err
		}
		results = append(results, value)
	}
	return results, nil
}

func (s *userStoreImpl) Delete(ctx context.Context, usernames []string) error {
	if s == nil || s.tree == nil {
		return nil
	}
	for _, username := range usernames {
		normalized := normalizeUsername(username)
		if normalized == "" {
			continue
		}
		if _, err := s.tree.Remove(ctx, normalized); err != nil {
			return err
		}
	}
	return nil
}

func (s *userStoreImpl) List(ctx context.Context) ([]UserRecord, error) {
	if s == nil || s.tree == nil {
		return nil, nil
	}
	users := make([]UserRecord, 0, s.tree.Count())
	ok, err := s.tree.First(ctx)
	if err != nil {
		return nil, err
	}
	for ok {
		value, err := s.tree.GetCurrentValue(ctx)
		if err != nil {
			return nil, err
		}
		users = append(users, value)
		ok, err = s.tree.Next(ctx)
		if err != nil {
			return nil, err
		}
	}
	return users, nil
}

type subscriptionStoreImpl struct {
	storeName string
	tx        sop.Transaction
	tree      btree.BtreeInterface[string, SubscriptionRecord]
}

func newSubscriptionStore(ctx context.Context, tx sop.Transaction) (*subscriptionStoreImpl, error) {
	db, err := getSiteAccountDB(ctx)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, fmt.Errorf("transaction is required")
	}
	store, err := newStringKeyedStore[SubscriptionRecord](ctx, db, tx, subscriptionStoreName)
	if err != nil {
		return nil, err
	}
	return &subscriptionStoreImpl{storeName: subscriptionStoreName, tx: tx, tree: store}, nil
}

func (s *subscriptionStoreImpl) Upsert(ctx context.Context, subscriptions []SubscriptionRecord) error {
	if s == nil || s.tree == nil {
		return fmt.Errorf("subscription store is not initialized")
	}
	indexEntries := make([]UserSubscriptionEntry, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		id := strings.TrimSpace(subscription.ID)
		if id == "" {
			continue
		}
		copySubscription := subscription
		copySubscription.UserID = normalizeUsername(copySubscription.UserID)
		copySubscription.UpdatedAt = time.Now().UTC()
		if copySubscription.CreatedAt.IsZero() {
			copySubscription.CreatedAt = copySubscription.UpdatedAt
		}
		if _, err := s.tree.Upsert(ctx, id, copySubscription); err != nil {
			return err
		}
		indexEntries = append(indexEntries, UserSubscriptionEntry{Username: copySubscription.UserID, SubscriptionID: id})
	}
	if len(indexEntries) > 0 {
		indexStore, err := newUserSubscriptionIndexStore(ctx, s.tx)
		if err != nil {
			return err
		}
		if err := indexStore.Upsert(ctx, indexEntries); err != nil {
			return err
		}
	}
	return nil
}

func (s *subscriptionStoreImpl) Fetch(ctx context.Context, ids []string) ([]SubscriptionRecord, error) {
	if s == nil || s.tree == nil {
		return nil, nil
	}
	results := make([]SubscriptionRecord, 0, len(ids))
	for _, id := range ids {
		normalized := strings.TrimSpace(id)
		if normalized == "" {
			continue
		}
		found, err := s.tree.Find(ctx, normalized, true)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		value, err := s.tree.GetCurrentValue(ctx)
		if err != nil {
			return nil, err
		}
		results = append(results, value)
	}
	return results, nil
}

func (s *subscriptionStoreImpl) Delete(ctx context.Context, ids []string) error {
	if s == nil || s.tree == nil {
		return nil
	}
	for _, id := range ids {
		normalized := strings.TrimSpace(id)
		if normalized == "" {
			continue
		}
		if _, err := s.tree.Remove(ctx, normalized); err != nil {
			return err
		}
	}
	return nil
}

func (s *subscriptionStoreImpl) FetchByUser(ctx context.Context, usernames []string) ([]sop.KeyValuePair[string, []SubscriptionRecord], error) {
	if s == nil || s.tree == nil {
		return nil, nil
	}
	results := make([]sop.KeyValuePair[string, []SubscriptionRecord], 0, len(usernames))
	for _, username := range usernames {
		normalized := normalizeUsername(username)
		if normalized == "" {
			continue
		}
		indexStore, err := newUserSubscriptionIndexStore(ctx, s.tx)
		if err != nil {
			return nil, err
		}
		ids, err := indexStore.ListIDs(ctx, normalized)
		if err != nil {
			return nil, err
		}
		userSubscriptions := make([]SubscriptionRecord, 0, len(ids))
		for _, id := range ids {
			found, err := s.tree.Find(ctx, id, true)
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
			value, err := s.tree.GetCurrentValue(ctx)
			if err != nil {
				return nil, err
			}
			userSubscriptions = append(userSubscriptions, value)
		}
		results = append(results, sop.KeyValuePair[string, []SubscriptionRecord]{Key: normalized, Value: userSubscriptions})
	}
	return results, nil
}

func (s *subscriptionStoreImpl) List(ctx context.Context) ([]SubscriptionRecord, error) {
	if s == nil || s.tree == nil {
		return nil, nil
	}
	results := make([]SubscriptionRecord, 0, s.tree.Count())
	ok, err := s.tree.First(ctx)
	if err != nil {
		return nil, err
	}
	for ok {
		value, err := s.tree.GetCurrentValue(ctx)
		if err != nil {
			return nil, err
		}
		results = append(results, value)
		ok, err = s.tree.Next(ctx)
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

type paymentStoreImpl struct {
	storeName string
	tx        sop.Transaction
	tree      btree.BtreeInterface[string, PaymentRecord]
}

func newPaymentStore(ctx context.Context, tx sop.Transaction) (*paymentStoreImpl, error) {
	db, err := getSiteAccountDB(ctx)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, fmt.Errorf("transaction is required")
	}
	store, err := newStringKeyedStore[PaymentRecord](ctx, db, tx, paymentStoreName)
	if err != nil {
		return nil, err
	}
	return &paymentStoreImpl{storeName: paymentStoreName, tx: tx, tree: store}, nil
}

func (s *paymentStoreImpl) Upsert(ctx context.Context, payments []PaymentRecord) error {
	if s == nil || s.tree == nil {
		return fmt.Errorf("payment store is not initialized")
	}
	indexEntries := make([]UserPaymentEntry, 0, len(payments))
	for _, payment := range payments {
		id := strings.TrimSpace(payment.ID)
		if id == "" {
			continue
		}
		copyPayment := payment
		copyPayment.UserID = normalizeUsername(copyPayment.UserID)
		copyPayment.UpdatedAt = time.Now().UTC()
		if copyPayment.CreatedAt.IsZero() {
			copyPayment.CreatedAt = copyPayment.UpdatedAt
		}
		if _, err := s.tree.Upsert(ctx, id, copyPayment); err != nil {
			return err
		}
		indexEntries = append(indexEntries, UserPaymentEntry{Username: copyPayment.UserID, PaymentID: id})
	}
	if len(indexEntries) > 0 {
		indexStore, err := newUserPaymentIndexStore(ctx, s.tx)
		if err != nil {
			return err
		}
		if err := indexStore.Upsert(ctx, indexEntries); err != nil {
			return err
		}
	}
	return nil
}

func (s *paymentStoreImpl) Fetch(ctx context.Context, ids []string) ([]PaymentRecord, error) {
	if s == nil || s.tree == nil {
		return nil, nil
	}
	results := make([]PaymentRecord, 0, len(ids))
	for _, id := range ids {
		normalized := strings.TrimSpace(id)
		if normalized == "" {
			continue
		}
		found, err := s.tree.Find(ctx, normalized, true)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		value, err := s.tree.GetCurrentValue(ctx)
		if err != nil {
			return nil, err
		}
		results = append(results, value)
	}
	return results, nil
}

func (s *paymentStoreImpl) Delete(ctx context.Context, ids []string) error {
	if s == nil || s.tree == nil {
		return nil
	}
	for _, id := range ids {
		normalized := strings.TrimSpace(id)
		if normalized == "" {
			continue
		}
		if _, err := s.tree.Remove(ctx, normalized); err != nil {
			return err
		}
	}
	return nil
}

func (s *paymentStoreImpl) FetchByUser(ctx context.Context, usernames []string) ([]sop.KeyValuePair[string, []PaymentRecord], error) {
	if s == nil || s.tree == nil {
		return nil, nil
	}
	results := make([]sop.KeyValuePair[string, []PaymentRecord], 0, len(usernames))
	for _, username := range usernames {
		normalized := normalizeUsername(username)
		if normalized == "" {
			continue
		}
		indexStore, err := newUserPaymentIndexStore(ctx, s.tx)
		if err != nil {
			return nil, err
		}
		ids, err := indexStore.ListIDs(ctx, normalized)
		if err != nil {
			return nil, err
		}
		userPayments := make([]PaymentRecord, 0, len(ids))
		for _, id := range ids {
			found, err := s.tree.Find(ctx, id, true)
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
			value, err := s.tree.GetCurrentValue(ctx)
			if err != nil {
				return nil, err
			}
			userPayments = append(userPayments, value)
		}
		results = append(results, sop.KeyValuePair[string, []PaymentRecord]{Key: normalized, Value: userPayments})
	}
	return results, nil
}

func (s *paymentStoreImpl) List(ctx context.Context) ([]PaymentRecord, error) {
	if s == nil || s.tree == nil {
		return nil, nil
	}
	results := make([]PaymentRecord, 0, s.tree.Count())
	ok, err := s.tree.First(ctx)
	if err != nil {
		return nil, err
	}
	for ok {
		value, err := s.tree.GetCurrentValue(ctx)
		if err != nil {
			return nil, err
		}
		results = append(results, value)
		ok, err = s.tree.Next(ctx)
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

// NewConfig creates a new site-specific account config.
func NewConfig() *Config {
	return &Config{}
}

// GetConfig returns the singleton site-specific account config.
func GetConfig() *Config {
	configMu.RLock()
	cfg := globalConfig
	configMu.RUnlock()
	if cfg != nil {
		return cfg
	}
	configMu.Lock()
	defer configMu.Unlock()
	if globalConfig == nil {
		globalConfig = NewConfig()
	}
	return globalConfig
}

// SetConfig replaces the singleton site-specific account config.
func SetConfig(cfg *Config) {
	configMu.Lock()
	defer configMu.Unlock()
	globalConfig = cfg
}

func normalizeUsername(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}

func hashSitePassword(password string) (string, error) {
	trimmed := strings.TrimSpace(password)
	if trimmed == "" {
		return "", fmt.Errorf("password is required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(trimmed), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func containsUserRecord(users []UserRecord, username string) bool {
	username = normalizeUsername(username)
	for _, existing := range users {
		if normalizeUsername(existing.Username) == username {
			return true
		}
	}
	return false
}

func normalizeRequestOrigin(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "human", "site", "signup", "site-signup", "human-signup":
		return "human"
	default:
		return ""
	}
}

func (c *Config) ensureUserStore(ctx context.Context, tx sop.Transaction) (UserStore, error) {
	if c == nil {
		return nil, fmt.Errorf("site account config is not initialized")
	}
	if tx == nil {
		return nil, fmt.Errorf("transaction is required")
	}
	return newUserStore(ctx, tx)
}

func (c *Config) ensureSubscriptionStore(ctx context.Context, tx sop.Transaction) (SubscriptionStore, error) {
	if c == nil {
		return nil, fmt.Errorf("site account config is not initialized")
	}
	if tx == nil {
		return nil, fmt.Errorf("transaction is required")
	}
	return newSubscriptionStore(ctx, tx)
}

func (c *Config) ensurePaymentStore(ctx context.Context, tx sop.Transaction) (PaymentStore, error) {
	if c == nil {
		return nil, fmt.Errorf("site account config is not initialized")
	}
	if tx == nil {
		return nil, fmt.Errorf("transaction is required")
	}
	return newPaymentStore(ctx, tx)
}

func (c *Config) ensureUserSubscriptionIndexStore(ctx context.Context, tx sop.Transaction) (UserSubscriptionStore, error) {
	if c == nil {
		return nil, fmt.Errorf("site account config is not initialized")
	}
	if tx == nil {
		return nil, fmt.Errorf("transaction is required")
	}
	return newUserSubscriptionIndexStore(ctx, tx)
}

func (c *Config) ensureUserPaymentIndexStore(ctx context.Context, tx sop.Transaction) (UserPaymentStore, error) {
	if c == nil {
		return nil, fmt.Errorf("site account config is not initialized")
	}
	if tx == nil {
		return nil, fmt.Errorf("transaction is required")
	}
	return newUserPaymentIndexStore(ctx, tx)
}

// FindUserRecord returns a site user by username.
func (c *Config) FindUserRecord(ctx context.Context, username string) (*UserRecord, bool, error) {
	if c == nil {
		return nil, false, nil
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
	store, err := c.ensureUserStore(ctx, tx)
	if err != nil {
		return nil, false, err
	}
	if store != nil {
		users, err := store.Fetch(ctx, []string{username})
		if err != nil {
			return nil, false, err
		}
		if len(users) > 0 && normalizeUsername(users[0].Username) != "" {
			return &users[0], true, nil
		}
	}
	for i := range c.Users {
		if normalizeUsername(c.Users[i].Username) == normalizeUsername(username) {
			copyUser := c.Users[i]
			return &copyUser, true, nil
		}
	}
	return nil, false, nil
}

// RegisterUserWithRequest creates a site account and keeps the shared backend auth user in sync.
func (c *Config) RegisterUserWithRequest(ctx context.Context, req RegisterUserRequest) (*UserRecord, error) {
	if c == nil {
		return nil, fmt.Errorf("site account config is not initialized")
	}
	username := normalizeUsername(req.Username)
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}

	db, err := getSiteAccountDB(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	existing, found, err := c.FindUserRecord(ctx, username)
	if err != nil {
		return nil, err
	}
	if found && existing != nil {
		if strings.TrimSpace(existing.PasswordHash) != "" {
			return nil, fmt.Errorf("user already exists")
		}
		passwordHash, err := hashSitePassword(req.Password)
		if err != nil {
			return nil, err
		}
		existing.Role = req.Role
		existing.Email = req.Email
		existing.SubscriptionPlan = req.SubscriptionPlan
		existing.SubscriptionFeatures = req.SubscriptionFeatures
		existing.PasswordHash = passwordHash
		existing.BillingStatus = "pending"
		existing.RequestOrigin = normalizeRequestOrigin(req.RequestOrigin)
		existing.Status = "active"
		existing.UpdatedAt = time.Now().UTC()

		store, err := c.ensureUserStore(ctx, tx)
		if err != nil {
			return nil, err
		}
		if err := store.Upsert(ctx, []UserRecord{*existing}); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		upsertSiteUserRecord(c, *existing)
		return existing, nil
	}

	passwordHash, err := hashSitePassword(req.Password)
	if err != nil {
		return nil, err
	}

	user := &UserRecord{
		Username:             username,
		Role:                 req.Role,
		PasswordHash:         passwordHash,
		Email:                req.Email,
		SubscriptionPlan:     req.SubscriptionPlan,
		SubscriptionFeatures: req.SubscriptionFeatures,
		BillingStatus:        "pending",
		RequestOrigin:        normalizeRequestOrigin(req.RequestOrigin),
		Status:               "active",
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
	}
	store, err := c.ensureUserStore(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := store.Upsert(ctx, []UserRecord{*user}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	upsertSiteUserRecord(c, *user)

	sharedCfg := sopbackend.GetConfig()
	_, err = sharedCfg.RegisterUserWithRequest(ctx, sopbackend.RegisterUserRequest{
		Username:             username,
		Password:             req.Password,
		Email:                req.Email,
		Role:                 req.Role,
		SubscriptionPlan:     req.SubscriptionPlan,
		SubscriptionFeatures: req.SubscriptionFeatures,
	})
	if err != nil {
		return nil, err
	}
	sopbackend.SetConfig(sharedCfg)
	return user, nil
}

// UnregisterUserWithRequest removes a site account and its related records.
func (c *Config) UnregisterUserWithRequest(ctx context.Context, req UnregisterUserRequest) error {
	if c == nil {
		return fmt.Errorf("site account config is not initialized")
	}
	username := normalizeUsername(req.Username)
	if username == "" {
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
	store, err := c.ensureUserStore(ctx, tx)
	if err != nil {
		return err
	}
	if store != nil {
		if err := store.Delete(ctx, []string{username}); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	filteredUsers := c.Users[:0]
	for _, existing := range c.Users {
		if normalizeUsername(existing.Username) == username {
			continue
		}
		filteredUsers = append(filteredUsers, existing)
	}
	c.Users = filteredUsers

	sharedCfg := sopbackend.GetConfig()
	_ = sharedCfg.UnregisterUserWithRequest(ctx, sopbackend.UnregisterUserRequest{Username: username})
	sopbackend.SetConfig(sharedCfg)
	return nil
}

// UpdateSubscriptionPlanWithRequest updates a site's stored subscription plan.
func (c *Config) UpdateSubscriptionPlanWithRequest(ctx context.Context, req UpdateSubscriptionPlanRequest) (*UserRecord, error) {
	if c == nil {
		return nil, fmt.Errorf("site account config is not initialized")
	}
	username := normalizeUsername(req.Username)
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}
	user, ok, err := c.FindUserRecord(ctx, username)
	if err != nil {
		return nil, err
	}
	if !ok || user == nil {
		return nil, fmt.Errorf("user not found")
	}
	db, err := getSiteAccountDB(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	user.SubscriptionPlan = req.SubscriptionPlan
	user.SubscriptionFeatures = req.SubscriptionFeatures
	if normalized := normalizeRequestOrigin(req.RequestOrigin); normalized != "" {
		user.RequestOrigin = normalized
	}
	user.UpdatedAt = time.Now().UTC()
	store, err := c.ensureUserStore(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := store.Upsert(ctx, []UserRecord{*user}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	for i := range c.Users {
		if normalizeUsername(c.Users[i].Username) == username {
			c.Users[i] = *user
			break
		}
	}

	sharedCfg := sopbackend.GetConfig()
	if _, err := sharedCfg.UpdateSubscriptionPlanWithRequest(ctx, sopbackend.UpdateSubscriptionPlanRequest{
		Username:             username,
		SubscriptionPlan:     req.SubscriptionPlan,
		SubscriptionFeatures: append([]string(nil), req.SubscriptionFeatures...),
	}); err != nil {
		return nil, err
	}
	sopbackend.SetConfig(sharedCfg)
	return user, nil
}

// CreateCheckoutSession creates a pending billing checkout session for an existing authenticated site account.
func (c *Config) CreateCheckoutSession(ctx context.Context, username, plan string) (*BillingCheckoutSession, error) {
	if c == nil {
		return nil, fmt.Errorf("site account config is not initialized")
	}
	username = normalizeUsername(username)
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}

	user, ok, err := c.FindUserRecord(ctx, username)
	if err != nil {
		return nil, err
	}
	if !ok || user == nil {
		return nil, fmt.Errorf("account not found")
	}

	db, err := getSiteAccountDB(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	user.SubscriptionPlan = plan
	user.BillingStatus = "pending"
	if user.RequestOrigin == "" {
		user.RequestOrigin = "human"
	}
	user.UpdatedAt = time.Now().UTC()

	checkoutURL := ""
	checkoutID := fmt.Sprintf("checkout-%d", time.Now().UnixNano())
	checkoutStatus := "pending"
	if checkoutSession, err := createStripeCheckoutSession(ctx, username, plan); err == nil && checkoutSession != nil {
		checkoutURL = checkoutSession.URL
		checkoutID = checkoutSession.ID
		checkoutStatus = checkoutSession.Status
	} else {
		checkoutPlan := strings.ToLower(strings.TrimSpace(plan))
		if checkoutPlan == "" {
			checkoutPlan = "starter"
		}
		checkoutURL = fmt.Sprintf("https://checkout.stripe.com/c/pay/cs_test_%s?session_id=%s", normalizeUsername(username), fmt.Sprintf("cs_%d", time.Now().UnixNano()))
	}
	user.CheckoutURL = checkoutURL
	store, err := c.ensureUserStore(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := store.Upsert(ctx, []UserRecord{*user}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	for i := range c.Users {
		if normalizeUsername(c.Users[i].Username) == normalizeUsername(username) {
			c.Users[i] = *user
			break
		}
	}
	if !containsUserRecord(c.Users, username) {
		c.Users = append(c.Users, *user)
	}
	session := &BillingCheckoutSession{ID: checkoutID, URL: checkoutURL, Status: checkoutStatus}
	return session, nil
}

func createStripeCheckoutSession(ctx context.Context, username, plan string) (*BillingCheckoutSession, error) {
	apiBaseURL := strings.TrimSpace(os.Getenv("STRIPE_API_BASE_URL"))
	if apiBaseURL == "" {
		apiBaseURL = "https://api.stripe.com"
	}
	secretKey := strings.TrimSpace(os.Getenv("STRIPE_SECRET_KEY"))
	if secretKey == "" {
		return nil, fmt.Errorf("stripe secret key not configured")
	}
	priceID := strings.TrimSpace(os.Getenv("STRIPE_PRICE_ID"))
	if priceID == "" {
		return nil, fmt.Errorf("stripe price id not configured")
	}
	returnURL := strings.TrimSpace(os.Getenv("STRIPE_SUCCESS_URL"))
	if returnURL == "" {
		returnURL = "https://example.com/success"
	}
	cancelURL := strings.TrimSpace(os.Getenv("STRIPE_CANCEL_URL"))
	if cancelURL == "" {
		cancelURL = "https://example.com/cancel"
	}

	payload := map[string]any{
		"mode":        "subscription",
		"success_url": returnURL,
		"cancel_url":  cancelURL,
		"line_items": []map[string]any{{
			"price":    priceID,
			"quantity": 1,
		}},
		"customer_email": username + "@local.invalid",
		"metadata": map[string]any{
			"username": username,
			"plan":     plan,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(apiBaseURL, "/") + "/v1/checkout/sessions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+secretKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("stripe checkout session creation failed with status %d", resp.StatusCode)
	}
	var result struct {
		ID     string `json:"id"`
		URL    string `json:"url"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.URL == "" {
		return nil, fmt.Errorf("stripe checkout session response did not include a url")
	}
	return &BillingCheckoutSession{ID: result.ID, URL: result.URL, Status: result.Status}, nil
}

// HandleBillingWebhook updates the site account billing state based on a webhook event.
func (c *Config) HandleBillingWebhook(ctx context.Context, event BillingWebhookEvent) error {
	if c == nil {
		return fmt.Errorf("site account config is not initialized")
	}
	user, ok, err := c.FindUserRecord(ctx, event.UserID)
	if err != nil {
		return err
	}
	if !ok || user == nil {
		return fmt.Errorf("user not found")
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
	user.LastWebhookEvent = event.EventType
	user.LastWebhookAt = time.Now().UTC()
	user.BillingStatus = "inactive"
	user.FailureReason = ""
	if event.EventType == "checkout.session.completed" {
		user.BillingStatus = "active"
		user.SubscriptionPlan = event.Plan
	} else if event.EventType == "checkout.session.expired" {
		user.FailureReason = "checkout session expired"
	}
	user.UpdatedAt = time.Now().UTC()
	store, err := c.ensureUserStore(ctx, tx)
	if err != nil {
		return err
	}
	if err := store.Upsert(ctx, []UserRecord{*user}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	for i := range c.Users {
		if normalizeUsername(c.Users[i].Username) == normalizeUsername(event.UserID) {
			c.Users[i] = *user
			break
		}
	}
	return nil
}

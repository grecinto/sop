package siteaccount

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
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
	billingInfoStoreName           = "billing_info"
	subscriptionStoreName          = "site_subscriptions"
	paymentStoreName               = "site_payments"
	productStoreName               = "site_products"
	userSubscriptionIndexStoreName = "site_user_subscription_ids"
	userPaymentIndexStoreName      = "site_user_payment_ids"
	SiteSeatLimit                  = 500
	siteSeatLimitEnvVar            = "SOP_SITE_SEAT_LIMIT"
)

// Config stores the site-specific account state for the React/site app.
type Config struct {
	BillingInfo                []BillingInfo
	Subscriptions              []SubscriptionRecord
	Payments                   []PaymentRecord
	Products                   []ProductRecord
	BillingInfoStore           BillingInfoStore
	SubscriptionsStore         SubscriptionStore
	PaymentsStore              PaymentStore
	ProductsStore              ProductStore
	UserSubscriptionIndexStore UserSubscriptionStore
	UserPaymentIndexStore      UserPaymentStore
}

// BillingInfo stores billing and subscription data for a site user.
// Auth data (username, password, role, email) is stored in sopbackend's users store.
type BillingInfo struct {
	Username                    string    `json:"username"`
	FullName                    string    `json:"full_name,omitempty"`
	SubscriptionPlan            string    `json:"subscription_plan,omitempty"`
	SubscriptionFeatures        []string  `json:"subscription_features,omitempty"`
	BillingStatus               string    `json:"billing_status,omitempty"`
	CheckoutURL                 string    `json:"checkout_url,omitempty"`
	PendingCheckoutSessionID    string    `json:"pending_checkout_session_id,omitempty"`
	StripeCheckoutSessionID     string    `json:"stripe_checkout_session_id,omitempty"`
	LastWebhookEvent            string    `json:"last_webhook_event,omitempty"`
	LastWebhookAt               time.Time `json:"last_webhook_at,omitempty"`
	LastPingAt                  time.Time `json:"last_ping_at,omitempty"`
	PendingCheckoutPingDeadline time.Time `json:"pending_checkout_ping_deadline,omitempty"`
	FailureReason               string    `json:"failure_reason,omitempty"`
	RequestOrigin               string    `json:"request_origin,omitempty"`
	StripeCustomerID            string    `json:"stripe_customer_id,omitempty"`
	StripeSubID                 string    `json:"stripe_subscription_id,omitempty"`
	Status                      string    `json:"status,omitempty"`
	CreatedAt                   time.Time `json:"created_at,omitempty"`
	UpdatedAt                   time.Time `json:"updated_at,omitempty"`
}

// RegisterUserRequest is the explicit registration contract for site account flows.
type RegisterUserRequest struct {
	Username             string
	FullName             string
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

// ProductRecord stores the catalog of available products and plans.
type ProductRecord struct {
	ID          string    `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	PriceCents  int       `json:"price_cents"`
	Currency    string    `json:"currency"`
	Interval    string    `json:"interval"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
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

// UserRecord combines auth data (from sopbackend) with billing data for API responses.
// This is NOT stored - it's constructed by fetching from both sopbackend users and billing_info stores.
type UserRecord struct {
	// Auth fields (from sopbackend.UserRecord)
	Username     string `json:"username"`
	Role         string `json:"role"`
	PasswordHash string `json:"password_hash,omitempty"`
	Email        string `json:"email,omitempty"`
	// Billing fields (from BillingInfo)
	FullName                    string    `json:"full_name,omitempty"`
	SubscriptionPlan            string    `json:"subscription_plan,omitempty"`
	SubscriptionFeatures        []string  `json:"subscription_features,omitempty"`
	BillingStatus               string    `json:"billing_status,omitempty"`
	CheckoutURL                 string    `json:"checkout_url,omitempty"`
	PendingCheckoutSessionID    string    `json:"pending_checkout_session_id,omitempty"`
	StripeCheckoutSessionID     string    `json:"stripe_checkout_session_id,omitempty"`
	LastWebhookEvent            string    `json:"last_webhook_event,omitempty"`
	LastWebhookAt               time.Time `json:"last_webhook_at,omitempty"`
	LastPingAt                  time.Time `json:"last_ping_at,omitempty"`
	PendingCheckoutPingDeadline time.Time `json:"pending_checkout_ping_deadline,omitempty"`
	FailureReason               string    `json:"failure_reason,omitempty"`
	RequestOrigin               string    `json:"request_origin,omitempty"`
	StripeCustomerID            string    `json:"stripe_customer_id,omitempty"`
	StripeSubID                 string    `json:"stripe_subscription_id,omitempty"`
	Status                      string    `json:"status,omitempty"`
	CreatedAt                   time.Time `json:"created_at,omitempty"`
	UpdatedAt                   time.Time `json:"updated_at,omitempty"`
}

// BillingInfoStore persists billing information keyed by username.
type BillingInfoStore interface {
	Upsert(ctx context.Context, billingInfo []BillingInfo) error
	Fetch(ctx context.Context, usernames []string) ([]BillingInfo, error)
	Delete(ctx context.Context, usernames []string) error
	List(ctx context.Context) ([]BillingInfo, error)
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

// ProductStore persists product catalog entries.
type ProductStore interface {
	Upsert(ctx context.Context, products []ProductRecord) error
	Fetch(ctx context.Context, ids []string) ([]ProductRecord, error)
	Delete(ctx context.Context, ids []string) error
	List(ctx context.Context) ([]ProductRecord, error)
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

	dbOpts, err := resolveSiteAccountDBOptions(ctx)
	if err != nil {
		return nil, err
	}
	siteAccountDBInst = aidb.NewDatabase(dbOpts)
	return siteAccountDBInst, nil
}

func resolveSiteAccountDBOptions(ctx context.Context) (sop.DatabaseOptions, error) {
	cfg := sopbackend.GetConfig()
	for i := range cfg.Databases {
		dbCfg := cfg.Databases[i]
		if dbCfg.IsSystem || strings.EqualFold(dbCfg.Name, sopbackend.SystemDBName) || strings.TrimSpace(dbCfg.Path) == "" {
			continue
		}
		return getDBOptionsFromConfig(ctx, &dbCfg)
	}
	if cfg.SystemDB != nil {
		return getDBOptionsFromConfig(ctx, cfg.SystemDB)
	}
	return sopbackend.GetSystemDBOptions(ctx)
}

func getDBOptionsFromConfig(ctx context.Context, db *sopbackend.DatabaseConfig) (sop.DatabaseOptions, error) {
	if db == nil {
		return sop.DatabaseOptions{}, fmt.Errorf("database config is nil")
	}

	if loadedOpts, err := database.GetOptions(ctx, db.Path); err == nil {
		if db.Mode == "clustered" {
			loadedOpts.Type = sop.Clustered
			loadedOpts.CacheType = sop.Redis
			loadedOpts.RedisConfig = &sop.RedisCacheConfig{Address: db.RedisURL}
		}
		return loadedOpts, nil
	}

	opts := sop.DatabaseOptions{
		Type:          sop.Standalone,
		StoresFolders: []string{db.Path},
	}
	if db.Mode == "clustered" {
		opts.Type = sop.Clustered
		opts.CacheType = sop.Redis
		opts.RedisConfig = &sop.RedisCacheConfig{Address: db.RedisURL}
	}
	return opts, nil
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

func getSiteSeatLimit() int {
	value := strings.TrimSpace(os.Getenv(siteSeatLimitEnvVar))
	if value == "" {
		return SiteSeatLimit
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return SiteSeatLimit
	}
	return parsed
}

func buildSiteSeatLimitMessage(limit int) string {
	return fmt.Sprintf("We intentionally limited early-access seats to %d users while we expand availability. Please try again next week when more seats are available.", limit)
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

type billingInfoStoreImpl struct {
	storeName string
	tx        sop.Transaction
	tree      btree.BtreeInterface[string, BillingInfo]
}

func newBillingInfoStore(ctx context.Context, tx sop.Transaction) (*billingInfoStoreImpl, error) {
	db, err := getSiteAccountDB(ctx)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, fmt.Errorf("transaction is required")
	}
	store, err := newStringKeyedStore[BillingInfo](ctx, db, tx, billingInfoStoreName)
	if err != nil {
		return nil, err
	}
	return &billingInfoStoreImpl{storeName: billingInfoStoreName, tx: tx, tree: store}, nil
}

func (s *billingInfoStoreImpl) Upsert(ctx context.Context, billingInfos []BillingInfo) error {
	if s == nil || s.tree == nil {
		return fmt.Errorf("billing info store is not initialized")
	}
	for _, info := range billingInfos {
		if info.Username == "" {
			continue
		}
		copyInfo := info
		copyInfo.Username = normalizeUsername(copyInfo.Username)
		if copyInfo.Username == "" {
			continue
		}
		copyInfo.UpdatedAt = time.Now().UTC()
		if copyInfo.CreatedAt.IsZero() {
			copyInfo.CreatedAt = copyInfo.UpdatedAt
		}
		if _, err := s.tree.Upsert(ctx, copyInfo.Username, copyInfo); err != nil {
			return err
		}
	}
	return nil
}

func (s *billingInfoStoreImpl) Fetch(ctx context.Context, usernames []string) ([]BillingInfo, error) {
	if s == nil || s.tree == nil {
		return nil, nil
	}
	results := make([]BillingInfo, 0, len(usernames))
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

func (s *billingInfoStoreImpl) Delete(ctx context.Context, usernames []string) error {
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

func (s *billingInfoStoreImpl) List(ctx context.Context) ([]BillingInfo, error) {
	if s == nil || s.tree == nil {
		return nil, nil
	}
	infos := make([]BillingInfo, 0, s.tree.Count())
	ok, err := s.tree.First(ctx)
	if err != nil {
		return nil, err
	}
	for ok {
		value, err := s.tree.GetCurrentValue(ctx)
		if err != nil {
			return nil, err
		}
		infos = append(infos, value)
		ok, err = s.tree.Next(ctx)
		if err != nil {
			return nil, err
		}
	}
	return infos, nil
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

type productStoreImpl struct {
	storeName string
	tx        sop.Transaction
	tree      btree.BtreeInterface[string, ProductRecord]
}

func newProductStore(ctx context.Context, tx sop.Transaction) (*productStoreImpl, error) {
	db, err := getSiteAccountDB(ctx)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, fmt.Errorf("transaction is required")
	}
	store, err := newStringKeyedStore[ProductRecord](ctx, db, tx, productStoreName)
	if err != nil {
		return nil, err
	}
	return &productStoreImpl{storeName: productStoreName, tx: tx, tree: store}, nil
}

func (s *productStoreImpl) Upsert(ctx context.Context, products []ProductRecord) error {
	if s == nil || s.tree == nil {
		return fmt.Errorf("product store is not initialized")
	}
	for _, product := range products {
		id := strings.TrimSpace(product.ID)
		if id == "" {
			continue
		}
		copyProduct := product
		copyProduct.UpdatedAt = time.Now().UTC()
		if copyProduct.CreatedAt.IsZero() {
			copyProduct.CreatedAt = copyProduct.UpdatedAt
		}
		if _, err := s.tree.Upsert(ctx, id, copyProduct); err != nil {
			return err
		}
	}
	return nil
}

func (s *productStoreImpl) Fetch(ctx context.Context, ids []string) ([]ProductRecord, error) {
	if s == nil || s.tree == nil {
		return nil, nil
	}
	results := make([]ProductRecord, 0, len(ids))
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

func (s *productStoreImpl) Delete(ctx context.Context, ids []string) error {
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

func (s *productStoreImpl) List(ctx context.Context) ([]ProductRecord, error) {
	if s == nil || s.tree == nil {
		return nil, nil
	}
	results := make([]ProductRecord, 0, s.tree.Count())
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

func normalizeRequestOrigin(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "human", "site", "signup", "site-signup", "human-signup":
		return "human"
	default:
		return ""
	}
}

func (c *Config) ensureBillingInfoStore(ctx context.Context, tx sop.Transaction) (BillingInfoStore, error) {
	if c == nil {
		return nil, fmt.Errorf("site account config is not initialized")
	}
	if tx == nil {
		return nil, fmt.Errorf("transaction is required")
	}
	return newBillingInfoStore(ctx, tx)
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

func (c *Config) ensureProductStore(ctx context.Context, tx sop.Transaction) (ProductStore, error) {
	if c == nil {
		return nil, fmt.Errorf("site account config is not initialized")
	}
	if tx == nil {
		return nil, fmt.Errorf("transaction is required")
	}
	return newProductStore(ctx, tx)
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

// FindUserRecord returns a composite user record by querying both sopbackend (auth) and billing_info stores.
func (c *Config) FindUserRecord(ctx context.Context, username string) (*UserRecord, bool, error) {
	if c == nil {
		return nil, false, nil
	}
	username = normalizeUsername(username)
	if username == "" {
		return nil, false, nil
	}

	// Query sopbackend for auth data using its resilient FindUserRecord method
	sopbackendCfg := sopbackend.GetConfig()
	authUser, found, err := sopbackendCfg.FindUserRecord(ctx, username)
	if err != nil {
		return nil, false, fmt.Errorf("failed to query sopbackend user: %w", err)
	}
	if !found {
		return nil, false, nil
	}

	// Query billing_info store for billing data
	db, err := getSiteAccountDB(ctx)
	if err != nil {
		return nil, false, err
	}
	tx, err := db.BeginTransaction(ctx, sop.ForReading, -1)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)

	billingStore, err := c.ensureBillingInfoStore(ctx, tx)
	if err != nil {
		return nil, false, err
	}

	billingInfos, err := billingStore.Fetch(ctx, []string{username})
	if err != nil {
		return nil, false, fmt.Errorf("failed to query billing info: %w", err)
	}

	// Combine auth + billing into UserRecord
	user := &UserRecord{
		Username:     authUser.Username,
		Role:         authUser.Role,
		PasswordHash: string(authUser.PasswordHash),
		Email:        string(authUser.Email),
		CreatedAt:    authUser.CreatedAt,
		UpdatedAt:    authUser.UpdatedAt,
	}

	// Add billing fields if they exist
	if len(billingInfos) > 0 {
		billing := billingInfos[0]
		user.FullName = billing.FullName
		user.SubscriptionPlan = billing.SubscriptionPlan
		user.SubscriptionFeatures = billing.SubscriptionFeatures
		user.BillingStatus = billing.BillingStatus
		user.CheckoutURL = billing.CheckoutURL
		user.PendingCheckoutSessionID = billing.PendingCheckoutSessionID
		user.StripeCheckoutSessionID = billing.StripeCheckoutSessionID
		user.LastWebhookEvent = billing.LastWebhookEvent
		user.LastWebhookAt = billing.LastWebhookAt
		user.LastPingAt = billing.LastPingAt
		user.PendingCheckoutPingDeadline = billing.PendingCheckoutPingDeadline
		user.FailureReason = billing.FailureReason
		user.RequestOrigin = billing.RequestOrigin
		user.StripeCustomerID = billing.StripeCustomerID
		user.StripeSubID = billing.StripeSubID
		user.Status = billing.Status
	}

	return user, true, nil
}

func generatePendingCheckoutSessionID() string {
	return fmt.Sprintf("pending-checkout-%d", time.Now().UnixNano())
}

// FindUserRecordByPendingCheckoutSession returns a site user by the backend-issued pending checkout session ID.
func (c *Config) FindUserRecordByPendingCheckoutSession(ctx context.Context, checkoutSessionID string) (*UserRecord, bool, error) {
	if c == nil {
		return nil, false, nil
	}
	checkoutSessionID = strings.TrimSpace(checkoutSessionID)
	if checkoutSessionID == "" {
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

	billingStore, err := c.ensureBillingInfoStore(ctx, tx)
	if err != nil {
		return nil, false, err
	}

	// Scan all billing records to find matching checkout session ID
	allBillingInfos, err := billingStore.List(ctx)
	if err != nil {
		return nil, false, err
	}

	for _, billing := range allBillingInfos {
		if billing.PendingCheckoutSessionID == checkoutSessionID {
			// Found matching billing record, now get auth data
			return c.FindUserRecord(ctx, billing.Username)
		}
	}

	return nil, false, nil
}

// FindUserRecordByCheckoutSession returns a site user by either the backend-issued pending checkout session ID or the Stripe checkout session ID.
func (c *Config) FindUserRecordByCheckoutSession(ctx context.Context, checkoutSessionID string) (*UserRecord, bool, error) {
	// Try pending checkout session ID first
	user, ok, err := c.FindUserRecordByPendingCheckoutSession(ctx, checkoutSessionID)
	if err != nil || ok || user != nil {
		return user, ok, err
	}

	if c == nil {
		return nil, false, nil
	}
	checkoutSessionID = strings.TrimSpace(checkoutSessionID)
	if checkoutSessionID == "" {
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

	billingStore, err := c.ensureBillingInfoStore(ctx, tx)
	if err != nil {
		return nil, false, err
	}

	// Scan all billing records to find matching Stripe checkout session ID
	allBillingInfos, err := billingStore.List(ctx)
	if err != nil {
		return nil, false, err
	}

	for _, billing := range allBillingInfos {
		if strings.EqualFold(strings.TrimSpace(billing.StripeCheckoutSessionID), checkoutSessionID) {
			// Found matching billing record, now get full user data
			return c.FindUserRecord(ctx, billing.Username)
		}
	}

	return nil, false, nil
}

// UpsertUserRecord persists a site user record by updating both sopbackend (auth) and billing_info stores.
func (c *Config) UpsertUserRecord(ctx context.Context, user *UserRecord) error {
	if c == nil {
		return fmt.Errorf("site account config is not initialized")
	}
	if user == nil {
		return fmt.Errorf("user is required")
	}

	// Update auth in sopbackend using the resilient user store access
	sopbackendCfg := sopbackend.GetConfig()
	store := sopbackendCfg.UsersStore
	if store == nil {
		// If UsersStore is not initialized, the system is not properly set up
		// This should not happen in production, but we handle it gracefully
		return fmt.Errorf("sopbackend users store not initialized")
	}

	authUser := &sopbackend.UserRecord{
		Username:     user.Username,
		Role:         user.Role,
		PasswordHash: sopbackend.SecuredString(user.PasswordHash),
		Email:        sopbackend.SecuredString(user.Email),
		CreatedAt:    user.CreatedAt,
		UpdatedAt:    time.Now().UTC(),
	}
	if authUser.CreatedAt.IsZero() {
		authUser.CreatedAt = authUser.UpdatedAt
	}

	if err := store.Save(ctx, authUser); err != nil {
		return fmt.Errorf("failed to save sopbackend user: %w", err)
	}

	// Update billing info in siteaccount
	db, err := getSiteAccountDB(ctx)
	if err != nil {
		return err
	}
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	billingStore, err := c.ensureBillingInfoStore(ctx, tx)
	if err != nil {
		return err
	}

	billing := BillingInfo{
		Username:                    user.Username,
		FullName:                    user.FullName,
		SubscriptionPlan:            user.SubscriptionPlan,
		SubscriptionFeatures:        user.SubscriptionFeatures,
		BillingStatus:               user.BillingStatus,
		CheckoutURL:                 user.CheckoutURL,
		PendingCheckoutSessionID:    user.PendingCheckoutSessionID,
		StripeCheckoutSessionID:     user.StripeCheckoutSessionID,
		LastWebhookEvent:            user.LastWebhookEvent,
		LastWebhookAt:               user.LastWebhookAt,
		LastPingAt:                  user.LastPingAt,
		PendingCheckoutPingDeadline: user.PendingCheckoutPingDeadline,
		FailureReason:               user.FailureReason,
		RequestOrigin:               user.RequestOrigin,
		StripeCustomerID:            user.StripeCustomerID,
		StripeSubID:                 user.StripeSubID,
		Status:                      user.Status,
		CreatedAt:                   user.CreatedAt,
		UpdatedAt:                   time.Now().UTC(),
	}

	if err := billingStore.Upsert(ctx, []BillingInfo{billing}); err != nil {
		return fmt.Errorf("failed to save billing info: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit billing info: %w", err)
	}

	return nil
}

// RegisterUserWithRequest creates a site account and keeps the shared backend auth user in sync.
func seedDefaultProducts(ctx context.Context, tx sop.Transaction) error {
	store, err := newProductStore(ctx, tx)
	if err != nil {
		return err
	}
	products, err := store.List(ctx)
	if err != nil {
		return err
	}
	if len(products) > 0 {
		return nil
	}
	product := ProductRecord{
		ID:          "prod_ai_copilot_starter",
		Code:        "ai-copilot-starter",
		Name:        "Northstar AI Copilot",
		Description: "Current AI Copilot plan with unlimited local agent access and shared workspace features.",
		Category:    "ai",
		PriceCents:  500,
		Currency:    "USD",
		Interval:    "month",
		Active:      true,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	return store.Upsert(ctx, []ProductRecord{product})
}

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
		billingStore, err := c.ensureBillingInfoStore(ctx, tx)
		if err != nil {
			return nil, err
		}
		billingInfos, err := billingStore.Fetch(ctx, []string{username})
		if err != nil {
			return nil, err
		}
		if len(billingInfos) > 0 {
			if strings.TrimSpace(existing.PasswordHash) != "" {
				return nil, fmt.Errorf("user already exists")
			}
		}
		passwordHash, err := hashSitePassword(req.Password)
		if err != nil {
			return nil, err
		}

		// Update sopbackend auth record
		sharedCfg := sopbackend.GetConfig()
		if sharedUser, ok, findErr := sharedCfg.FindUserRecord(ctx, username); findErr == nil && ok && sharedUser != nil {
			sharedUser.Role = req.Role
			sharedUser.Email = sopbackend.SecuredString(req.Email)
			sharedUser.PasswordHash = sopbackend.SecuredString(passwordHash)
			sharedUser.UpdatedAt = time.Now().UTC()
			if store := sharedCfg.UsersStore; store != nil {
				if err := store.Save(ctx, sharedUser); err != nil {
					return nil, fmt.Errorf("failed to update sopbackend user: %w", err)
				}
			}
		}
		sopbackend.SetConfig(sharedCfg)

		// Update billing info
		existing.SubscriptionPlan = req.SubscriptionPlan
		existing.SubscriptionFeatures = req.SubscriptionFeatures
		existing.BillingStatus = "pending"
		existing.RequestOrigin = normalizeRequestOrigin(req.RequestOrigin)
		existing.Status = "active"
		existing.UpdatedAt = time.Now().UTC()

		billingInfo := BillingInfo{
			Username:             existing.Username,
			FullName:             existing.FullName,
			SubscriptionPlan:     existing.SubscriptionPlan,
			SubscriptionFeatures: existing.SubscriptionFeatures,
			BillingStatus:        existing.BillingStatus,
			RequestOrigin:        existing.RequestOrigin,
			Status:               existing.Status,
			UpdatedAt:            existing.UpdatedAt,
		}
		if err := billingStore.Upsert(ctx, []BillingInfo{billingInfo}); err != nil {
			return nil, fmt.Errorf("failed to update billing info: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return existing, nil
	}

	seatLimit := getSiteSeatLimit()
	if seatLimit > 0 {
		// Check seat limit from sopbackend user store
		sharedCfg := sopbackend.GetConfig()
		if sharedCfg.UsersStore != nil {
			users, err := sharedCfg.UsersStore.List(ctx)
			if err != nil {
				return nil, err
			}
			if len(users) >= seatLimit {
				return nil, fmt.Errorf("%s", buildSiteSeatLimitMessage(seatLimit))
			}
		}
		sopbackend.SetConfig(sharedCfg)
	}

	passwordHash, err := hashSitePassword(req.Password)
	if err != nil {
		return nil, err
	}

	// Create or repair the shared auth user before creating billing info.
	sharedCfg := sopbackend.GetConfig()
	sharedUser, sharedFound, sharedErr := sharedCfg.FindUserRecord(ctx, username)
	if sharedErr != nil {
		return nil, fmt.Errorf("failed to inspect sopbackend user: %w", sharedErr)
	}
	if sharedFound && sharedUser != nil {
		sharedUser.Role = req.Role
		sharedUser.Email = sopbackend.SecuredString(req.Email)
		if strings.TrimSpace(string(sharedUser.PasswordHash)) == "" {
			sharedUser.PasswordHash = sopbackend.SecuredString(passwordHash)
		}
		sharedUser.UpdatedAt = time.Now().UTC()
		if store := sharedCfg.UsersStore; store != nil {
			if err := store.Save(ctx, sharedUser); err != nil {
				return nil, fmt.Errorf("failed to update sopbackend user: %w", err)
			}
		}
	} else {
		_, err = sharedCfg.RegisterUserWithRequest(ctx, sopbackend.RegisterUserRequest{
			Username: username,
			FullName: req.FullName,
			Password: req.Password,
			Email:    req.Email,
			Role:     req.Role,
		})
		if err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "already exists") {
				return nil, fmt.Errorf("failed to create sopbackend user: %w", err)
			}
			sharedUser, sharedFound, sharedErr = sharedCfg.FindUserRecord(ctx, username)
			if sharedErr != nil {
				return nil, fmt.Errorf("failed to inspect sopbackend user after duplicate registration: %w", sharedErr)
			}
			if sharedFound && sharedUser != nil {
				sharedUser.Role = req.Role
				sharedUser.Email = sopbackend.SecuredString(req.Email)
				if strings.TrimSpace(string(sharedUser.PasswordHash)) == "" {
					sharedUser.PasswordHash = sopbackend.SecuredString(passwordHash)
				}
				sharedUser.UpdatedAt = time.Now().UTC()
				if store := sharedCfg.UsersStore; store != nil {
					if err := store.Save(ctx, sharedUser); err != nil {
						return nil, fmt.Errorf("failed to update sopbackend user: %w", err)
					}
				}
			}
		}
	}
	sopbackend.SetConfig(sharedCfg)

	// Create billing info in siteaccount
	billing := BillingInfo{
		Username:             username,
		FullName:             strings.TrimSpace(req.FullName),
		SubscriptionPlan:     req.SubscriptionPlan,
		SubscriptionFeatures: req.SubscriptionFeatures,
		BillingStatus:        "pending",
		RequestOrigin:        normalizeRequestOrigin(req.RequestOrigin),
		Status:               "active",
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
	}

	billingStore, err := c.ensureBillingInfoStore(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := billingStore.Upsert(ctx, []BillingInfo{billing}); err != nil {
		return nil, fmt.Errorf("failed to create billing info: %w", err)
	}
	if err := seedDefaultProducts(ctx, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit billing info: %w", err)
	}

	// Return composite UserRecord
	user := &UserRecord{
		Username:             username,
		FullName:             billing.FullName,
		Role:                 req.Role,
		PasswordHash:         passwordHash,
		Email:                req.Email,
		SubscriptionPlan:     billing.SubscriptionPlan,
		SubscriptionFeatures: billing.SubscriptionFeatures,
		BillingStatus:        billing.BillingStatus,
		RequestOrigin:        billing.RequestOrigin,
		Status:               billing.Status,
		CreatedAt:            billing.CreatedAt,
		UpdatedAt:            billing.UpdatedAt,
	}
	return user, nil
}

// UnregisterUserWithRequest removes a site account and its related records from both stores.
func (c *Config) UnregisterUserWithRequest(ctx context.Context, req UnregisterUserRequest) error {
	if c == nil {
		return fmt.Errorf("site account config is not initialized")
	}
	username := normalizeUsername(req.Username)
	if username == "" {
		return fmt.Errorf("username is required")
	}

	// Delete from siteaccount stores
	db, err := getSiteAccountDB(ctx)
	if err != nil {
		return err
	}
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Delete billing info
	billingStore, err := c.ensureBillingInfoStore(ctx, tx)
	if err != nil {
		return err
	}
	if billingStore != nil {
		if err := billingStore.Delete(ctx, []string{username}); err != nil {
			return fmt.Errorf("failed to delete billing info: %w", err)
		}
	}

	subscriptionStore, err := c.ensureSubscriptionStore(ctx, tx)
	if err != nil {
		return err
	}
	paymentStore, err := c.ensurePaymentStore(ctx, tx)
	if err != nil {
		return err
	}

	subscriptionIDs := make([]string, 0)
	if subscriptionStore != nil {
		userSubscriptions, err := subscriptionStore.FetchByUser(ctx, []string{username})
		if err != nil {
			return err
		}
		for _, entry := range userSubscriptions {
			for _, subscription := range entry.Value {
				if strings.TrimSpace(subscription.ID) != "" {
					subscriptionIDs = append(subscriptionIDs, subscription.ID)
				}
			}
		}
		if len(subscriptionIDs) > 0 {
			if err := subscriptionStore.Delete(ctx, subscriptionIDs); err != nil {
				return err
			}
			userSubscriptionIndexStore, err := c.ensureUserSubscriptionIndexStore(ctx, tx)
			if err != nil {
				return err
			}
			if userSubscriptionIndexStore != nil {
				if err := userSubscriptionIndexStore.Delete(ctx, []UserEntityIDs{{Username: username, EntityIDs: subscriptionIDs}}); err != nil {
					return err
				}
			}
		}
	}

	paymentIDs := make([]string, 0)
	if paymentStore != nil {
		userPayments, err := paymentStore.FetchByUser(ctx, []string{username})
		if err != nil {
			return err
		}
		for _, entry := range userPayments {
			for _, payment := range entry.Value {
				if strings.TrimSpace(payment.ID) != "" {
					paymentIDs = append(paymentIDs, payment.ID)
				}
			}
		}
		if len(paymentIDs) > 0 {
			if err := paymentStore.Delete(ctx, paymentIDs); err != nil {
				return err
			}
			userPaymentIndexStore, err := c.ensureUserPaymentIndexStore(ctx, tx)
			if err != nil {
				return err
			}
			if userPaymentIndexStore != nil {
				if err := userPaymentIndexStore.Delete(ctx, []UserEntityIDs{{Username: username, EntityIDs: paymentIDs}}); err != nil {
					return err
				}
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit siteaccount deletions: %w", err)
	}

	// Delete from sopbackend auth store
	sharedCfg := sopbackend.GetConfig()
	if sharedCfg.UsersStore != nil {
		if err := sharedCfg.UsersStore.Delete(ctx, username); err != nil {
			// Log but don't fail if sopbackend deletion fails
			slog.Warn("failed to delete sopbackend user", "username", username, "error", err)
		}
	}
	sopbackend.SetConfig(sharedCfg)

	return nil
}

// ChangePasswordRequest updates a site's stored password.
type ChangePasswordRequest struct {
	Username        string
	CurrentPassword string
	NewPassword     string
}

// ChangePasswordWithRequest updates a site's stored password and syncs the shared backend auth record.
func (c *Config) ChangePasswordWithRequest(ctx context.Context, req ChangePasswordRequest) (*UserRecord, error) {
	if c == nil {
		return nil, fmt.Errorf("site account config is not initialized")
	}
	username := normalizeUsername(req.Username)
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}
	currentPassword := strings.TrimSpace(req.CurrentPassword)
	newPassword := strings.TrimSpace(req.NewPassword)
	if currentPassword == "" || newPassword == "" {
		return nil, fmt.Errorf("current and new passwords are required")
	}
	user, ok, err := c.FindUserRecord(ctx, username)
	if err != nil {
		return nil, err
	}
	if !ok || user == nil {
		return nil, fmt.Errorf("user not found")
	}
	if user.PasswordHash == "" {
		return nil, fmt.Errorf("password is not set")
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)) != nil {
		return nil, fmt.Errorf("current password is incorrect")
	}
	passwordHash, err := hashSitePassword(newPassword)
	if err != nil {
		return nil, err
	}

	// Update password in sopbackend only (password is auth data, not billing)
	sharedCfg := sopbackend.GetConfig()
	if sharedUser, ok, findErr := sharedCfg.FindUserRecord(ctx, username); findErr == nil && ok && sharedUser != nil {
		sharedUser.PasswordHash = sopbackend.SecuredString(passwordHash)
		sharedUser.UpdatedAt = time.Now().UTC()
		if store := sharedCfg.UsersStore; store != nil {
			if err := store.Save(ctx, sharedUser); err != nil {
				return nil, fmt.Errorf("failed to update password in sopbackend: %w", err)
			}
		}
	}
	sopbackend.SetConfig(sharedCfg)

	// Update user record with new password hash for response
	user.PasswordHash = passwordHash
	user.UpdatedAt = time.Now().UTC()
	return user, nil
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

	// Update billing info only (subscription plan is billing data)
	billingStore, err := c.ensureBillingInfoStore(ctx, tx)
	if err != nil {
		return nil, err
	}

	billingInfos, err := billingStore.Fetch(ctx, []string{username})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch billing info: %w", err)
	}
	if len(billingInfos) == 0 {
		return nil, fmt.Errorf("billing info not found for user: %s", username)
	}
	billingInfo := billingInfos[0]
	billingInfo.SubscriptionPlan = req.SubscriptionPlan
	billingInfo.SubscriptionFeatures = req.SubscriptionFeatures
	if normalized := normalizeRequestOrigin(req.RequestOrigin); normalized != "" {
		billingInfo.RequestOrigin = normalized
	}
	billingInfo.UpdatedAt = time.Now().UTC()

	if err := billingStore.Upsert(ctx, []BillingInfo{billingInfo}); err != nil {
		return nil, fmt.Errorf("failed to update billing info: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	// Update user record with new billing data for response
	user.SubscriptionPlan = billingInfo.SubscriptionPlan
	user.SubscriptionFeatures = billingInfo.SubscriptionFeatures
	user.RequestOrigin = billingInfo.RequestOrigin
	user.UpdatedAt = billingInfo.UpdatedAt

	// NOTE: Subscription plan management is site-backend-only.
	// sopbackend only tracks basic auth/role data, not billing/subscription.
	return user, nil
}

// CreateCheckoutSession creates a pending billing checkout session for an existing authenticated site account.
func (c *Config) CreateCheckoutSession(ctx context.Context, username, plan string, opts ...string) (*BillingCheckoutSession, error) {
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

	providedEmail := ""
	if len(opts) > 0 {
		providedEmail = strings.TrimSpace(opts[0])
	}
	if providedEmail == "" {
		providedEmail = strings.TrimSpace(user.Email)
	}

	pendingCheckoutSessionID := generatePendingCheckoutSessionID()
	checkoutURL := ""
	checkoutStatus := "pending"
	checkoutSession, err := createStripeCheckoutSession(ctx, username, providedEmail, plan)
	if err != nil {
		return nil, err
	}
	stripeCheckoutSessionID := ""
	if checkoutSession != nil {
		checkoutURL = checkoutSession.URL
		checkoutStatus = checkoutSession.Status
		stripeCheckoutSessionID = checkoutSession.ID
	}

	// Update billing info with checkout session details
	billingStore, err := c.ensureBillingInfoStore(ctx, tx)
	if err != nil {
		return nil, err
	}

	billingInfos, err := billingStore.Fetch(ctx, []string{username})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch billing info: %w", err)
	}
	if len(billingInfos) == 0 {
		return nil, fmt.Errorf("billing info not found for user: %s", username)
	}
	billingInfo := billingInfos[0]
	billingInfo.SubscriptionPlan = plan
	billingInfo.BillingStatus = "pending"
	if billingInfo.RequestOrigin == "" {
		billingInfo.RequestOrigin = "human"
	}
	billingInfo.StripeCheckoutSessionID = stripeCheckoutSessionID
	billingInfo.CheckoutURL = checkoutURL
	billingInfo.PendingCheckoutSessionID = pendingCheckoutSessionID
	billingInfo.UpdatedAt = time.Now().UTC()

	if err := billingStore.Upsert(ctx, []BillingInfo{billingInfo}); err != nil {
		return nil, fmt.Errorf("failed to update billing info: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	session := &BillingCheckoutSession{ID: pendingCheckoutSessionID, URL: checkoutURL, Status: checkoutStatus}
	return session, nil
}

func createStripeCheckoutSession(ctx context.Context, username, email, plan string) (*BillingCheckoutSession, error) {
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
	if !strings.HasPrefix(priceID, "price_") {
		return nil, fmt.Errorf("stripe price id must be a recurring price id like price_123, got %q", priceID)
	}
	returnURL := strings.TrimSpace(os.Getenv("STRIPE_SUCCESS_URL"))
	if returnURL == "" {
		returnURL = "https://example.com/success"
	}
	cancelURL := strings.TrimSpace(os.Getenv("STRIPE_CANCEL_URL"))
	if cancelURL == "" {
		cancelURL = "https://example.com/cancel"
	}

	customerEmail := strings.TrimSpace(os.Getenv("STRIPE_CUSTOMER_EMAIL"))
	if customerEmail == "" {
		customerEmail = strings.TrimSpace(email)
	}
	if customerEmail == "" {
		customerEmail = username
	}
	form := url.Values{}
	form.Set("mode", "subscription")
	form.Set("success_url", returnURL)
	form.Set("cancel_url", cancelURL)
	form.Set("line_items[0][price]", priceID)
	form.Set("line_items[0][quantity]", "1")
	form.Set("customer_email", customerEmail)
	form.Set("metadata[username]", username)
	form.Set("metadata[plan]", plan)
	body := form.Encode()
	endpoint := strings.TrimRight(apiBaseURL, "/") + "/v1/checkout/sessions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+secretKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bodyBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody := strings.TrimSpace(string(bodyBytes))
		if responseBody != "" {
			return nil, fmt.Errorf("stripe checkout session creation failed with status %d: %s", resp.StatusCode, responseBody)
		}
		return nil, fmt.Errorf("stripe checkout session creation failed with status %d", resp.StatusCode)
	}
	var result struct {
		ID     string `json:"id"`
		URL    string `json:"url"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
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

	// Update billing info only (webhook events are billing data)
	billingStore, err := c.ensureBillingInfoStore(ctx, tx)
	if err != nil {
		return err
	}

	billingInfos, err := billingStore.Fetch(ctx, []string{event.UserID})
	if err != nil {
		return err
	}
	if len(billingInfos) == 0 {
		return fmt.Errorf("billing info not found for user: %s", event.UserID)
	}
	billingInfo := billingInfos[0]

	billingInfo.LastWebhookEvent = event.EventType
	billingInfo.LastWebhookAt = time.Now().UTC()
	billingInfo.BillingStatus = "inactive"
	billingInfo.FailureReason = ""
	if event.EventType == "checkout.session.completed" {
		billingInfo.BillingStatus = "active"
		billingInfo.SubscriptionPlan = event.Plan
	} else if event.EventType == "checkout.session.expired" {
		billingInfo.FailureReason = "checkout session expired"
	}
	billingInfo.UpdatedAt = time.Now().UTC()

	if err := billingStore.Upsert(ctx, []BillingInfo{billingInfo}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

// SyncSubscriptionStatusFromStripe polls Stripe to verify the subscription status for a user with pending billing.
// This is a fallback for when webhooks are missed (e.g. backend was down or unreachable).
func (c *Config) SyncSubscriptionStatusFromStripe(ctx context.Context, username string) error {
	if c == nil {
		return fmt.Errorf("site account config is not initialized")
	}
	slog.Info("siteaccount.stripe.sync.start", "username", username)
	user, ok, err := c.FindUserRecord(ctx, username)
	if err != nil {
		slog.Error("siteaccount.stripe.sync.find_user_error", "username", username, "error", err)
		return err
	}
	if !ok || user == nil {
		slog.Warn("siteaccount.stripe.sync.user_not_found", "username", username)
		return fmt.Errorf("user not found")
	}
	// Only sync if billing is pending OR active (to verify active status with Stripe)
	if user.BillingStatus != "pending" && user.BillingStatus != "active" {
		slog.Info("siteaccount.stripe.sync.not_syncable", "username", username, "billing_status", user.BillingStatus)
		return nil
	}

	slog.Info("siteaccount.stripe.sync.user_state", "username", username, "checkout_session_id", user.StripeCheckoutSessionID, "pending_checkout_id", user.PendingCheckoutSessionID, "customer_id", user.StripeCustomerID, "subscription_id", user.StripeSubID)

	secretKey := strings.TrimSpace(os.Getenv("STRIPE_SECRET_KEY"))
	if secretKey == "" {
		slog.Error("siteaccount.stripe.sync.no_secret_key", "username", username)
		return nil
	}

	apiBaseURL := strings.TrimSpace(os.Getenv("STRIPE_API_BASE_URL"))
	if apiBaseURL == "" {
		apiBaseURL = "https://api.stripe.com"
	}

	// If we have a checkout session ID, retrieve it from Stripe to get subscription details
	if user.StripeCheckoutSessionID != "" && user.StripeSubID == "" {
		slog.Info("siteaccount.stripe.sync.checkout_session", "username", username, "checkout_session_id", user.StripeCheckoutSessionID)
		sessionURL := fmt.Sprintf("%s/v1/checkout/sessions/%s?expand[]=subscription", strings.TrimRight(apiBaseURL, "/"), user.StripeCheckoutSessionID)
		req, err := http.NewRequestWithContext(ctx, "GET", sessionURL, nil)
		if err != nil {
			slog.Error("siteaccount.stripe.sync.checkout_session_request_error", "username", username, "error", err)
			return err
		}
		req.Header.Set("Authorization", "Bearer "+secretKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			slog.Error("siteaccount.stripe.sync.checkout_session_http_error", "username", username, "error", err)
			return err
		}
		defer resp.Body.Close()
		slog.Info("siteaccount.stripe.sync.checkout_session_response", "username", username, "status", resp.StatusCode)
		if resp.StatusCode == 200 {
			var session struct {
				ID           string `json:"id"`
				Status       string `json:"status"`
				Customer     string `json:"customer"`
				Subscription struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				} `json:"subscription"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&session); err == nil {
				slog.Info("siteaccount.stripe.sync.checkout_session_data", "username", username, "session_status", session.Status, "subscription_id", session.Subscription.ID, "subscription_status", session.Subscription.Status, "customer", session.Customer)
				// Store the customer and subscription IDs for future queries
				if session.Customer != "" {
					user.StripeCustomerID = session.Customer
				}
				if session.Subscription.ID != "" {
					user.StripeSubID = session.Subscription.ID
				}
				// If the checkout session is complete and subscription is active, activate billing
				if session.Status == "complete" && (session.Subscription.Status == "active" || session.Subscription.Status == "trialing") {
					slog.Info("siteaccount.stripe.sync.activating_billing", "username", username)
					return c.activateUserBilling(ctx, user, username)
				} else {
					slog.Info("siteaccount.stripe.sync.not_ready_to_activate", "username", username, "session_status", session.Status, "subscription_status", session.Subscription.Status)
				}
			} else {
				slog.Error("siteaccount.stripe.sync.checkout_session_decode_error", "username", username, "error", err)
			}
		} else {
			slog.Warn("siteaccount.stripe.sync.checkout_session_non_200", "username", username, "status", resp.StatusCode)
		}
	} else {
		slog.Info("siteaccount.stripe.sync.skipping_checkout_session", "username", username, "has_checkout_id", user.StripeCheckoutSessionID != "", "has_sub_id", user.StripeSubID != "")
	}

	// Try querying by customer ID if we have it but no subscription ID
	if user.StripeSubID == "" && user.StripeCustomerID != "" {
		slog.Info("siteaccount.stripe.sync.customer_subscriptions", "username", username, "customer_id", user.StripeCustomerID)
		// Query customer's subscriptions
		subscriptionsURL := fmt.Sprintf("%s/v1/subscriptions?customer=%s&limit=1", strings.TrimRight(apiBaseURL, "/"), user.StripeCustomerID)
		req, err := http.NewRequestWithContext(ctx, "GET", subscriptionsURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+secretKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			var result struct {
				Data []struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				} `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && len(result.Data) > 0 {
				user.StripeSubID = result.Data[0].ID
				if result.Data[0].Status == "active" || result.Data[0].Status == "trialing" {
					return c.activateUserBilling(ctx, user, username)
				}
			}
		}
	}

	// If we have a subscription ID, check the subscription status directly
	if user.StripeSubID != "" {
		slog.Info("siteaccount.stripe.sync.subscription", "username", username, "subscription_id", user.StripeSubID)
		subscriptionURL := fmt.Sprintf("%s/v1/subscriptions/%s", strings.TrimRight(apiBaseURL, "/"), user.StripeSubID)
		req, err := http.NewRequestWithContext(ctx, "GET", subscriptionURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+secretKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			// Subscription not found or error - mark as inactive
			slog.Warn("siteaccount.stripe.sync.subscription_not_found", "username", username, "subscription_id", user.StripeSubID, "status", resp.StatusCode)
			if user.BillingStatus == "active" {
				return c.deactivateUserBilling(ctx, user, username, "subscription not found in Stripe")
			}
			return nil
		}
		var subscription struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&subscription); err != nil {
			return err
		}
		slog.Info("siteaccount.stripe.sync.subscription_status", "username", username, "stripe_status", subscription.Status)
		// If the subscription is active on Stripe, update local billing state
		if subscription.Status == "active" || subscription.Status == "trialing" {
			return c.activateUserBilling(ctx, user, username)
		}
		// If subscription is not active (cancelled, expired, etc.), deactivate locally
		if user.BillingStatus == "active" {
			return c.deactivateUserBilling(ctx, user, username, fmt.Sprintf("stripe subscription status: %s", subscription.Status))
		}
	}
	slog.Info("siteaccount.stripe.sync.complete", "username", username, "billing_status", user.BillingStatus)
	return nil
}

func (c *Config) activateUserBilling(ctx context.Context, user *UserRecord, username string) error {
	slog.Info("siteaccount.activate_billing.start", "username", username, "current_status", user.BillingStatus)
	db, err := getSiteAccountDB(ctx)
	if err != nil {
		slog.Error("siteaccount.activate_billing.db_error", "username", username, "error", err)
		return err
	}
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		slog.Error("siteaccount.activate_billing.tx_error", "username", username, "error", err)
		return err
	}
	defer tx.Rollback(ctx)

	// Update billing info only
	billingStore, err := c.ensureBillingInfoStore(ctx, tx)
	if err != nil {
		slog.Error("siteaccount.activate_billing.store_error", "username", username, "error", err)
		return err
	}

	billingInfos, err := billingStore.Fetch(ctx, []string{username})
	if err != nil {
		slog.Error("siteaccount.activate_billing.fetch_error", "username", username, "error", err)
		return err
	}
	if len(billingInfos) == 0 {
		return fmt.Errorf("billing info not found for user: %s", username)
	}
	billingInfo := billingInfos[0]

	billingInfo.BillingStatus = "active"
	billingInfo.LastWebhookEvent = "subscription.synced"
	billingInfo.LastWebhookAt = time.Now().UTC()
	billingInfo.UpdatedAt = time.Now().UTC()
	slog.Info("siteaccount.activate_billing.updating", "username", username, "new_status", billingInfo.BillingStatus)

	if err := billingStore.Upsert(ctx, []BillingInfo{billingInfo}); err != nil {
		slog.Error("siteaccount.activate_billing.upsert_error", "username", username, "error", err)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		slog.Error("siteaccount.activate_billing.commit_error", "username", username, "error", err)
		return err
	}
	slog.Info("siteaccount.activate_billing.db_committed", "username", username)
	slog.Info("siteaccount.activate_billing.success", "username", username, "billing_status", billingInfo.BillingStatus)
	return nil
}

func (c *Config) deactivateUserBilling(ctx context.Context, user *UserRecord, username string, reason string) error {
	slog.Info("siteaccount.deactivate_billing.start", "username", username, "current_status", user.BillingStatus, "reason", reason)
	db, err := getSiteAccountDB(ctx)
	if err != nil {
		slog.Error("siteaccount.deactivate_billing.db_error", "username", username, "error", err)
		return err
	}
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		slog.Error("siteaccount.deactivate_billing.tx_error", "username", username, "error", err)
		return err
	}
	defer tx.Rollback(ctx)

	// Update billing info only
	billingStore, err := c.ensureBillingInfoStore(ctx, tx)
	if err != nil {
		slog.Error("siteaccount.deactivate_billing.store_error", "username", username, "error", err)
		return err
	}

	billingInfos, err := billingStore.Fetch(ctx, []string{username})
	if err != nil {
		slog.Error("siteaccount.deactivate_billing.fetch_error", "username", username, "error", err)
		return err
	}
	if len(billingInfos) == 0 {
		return fmt.Errorf("billing info not found for user: %s", username)
	}
	billingInfo := billingInfos[0]

	billingInfo.BillingStatus = "inactive"
	billingInfo.FailureReason = reason
	billingInfo.LastWebhookEvent = "subscription.sync_failed"
	billingInfo.LastWebhookAt = time.Now().UTC()
	billingInfo.UpdatedAt = time.Now().UTC()
	slog.Info("siteaccount.deactivate_billing.updating", "username", username, "new_status", billingInfo.BillingStatus)

	if err := billingStore.Upsert(ctx, []BillingInfo{billingInfo}); err != nil {
		slog.Error("siteaccount.deactivate_billing.upsert_error", "username", username, "error", err)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		slog.Error("siteaccount.deactivate_billing.commit_error", "username", username, "error", err)
		return err
	}
	slog.Info("siteaccount.deactivate_billing.success", "username", username, "billing_status", billingInfo.BillingStatus)
	return nil
}

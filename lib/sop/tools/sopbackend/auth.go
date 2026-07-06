package sopbackend

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	log "log/slog"

	"github.com/sharedcode/sop"
	aidb "github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/btree"
	"github.com/sharedcode/sop/database"
	"golang.org/x/crypto/bcrypt"
)

func getSystemDB(ctx context.Context) (*aidb.Database, error) {
	dbOpts, err := GetSystemDBOptions(ctx)
	if err != nil {
		return nil, err
	}
	return aidb.NewDatabase(dbOpts), nil
}

// UserRecord stores minimal credential and role data for the built-in auth model.
type UserRecord struct {
	Username                string        `json:"username"`
	Role                    string        `json:"role"`
	PasswordHash            SecuredString `json:"password_hash,omitempty"`
	Email                   SecuredString `json:"email,omitempty"`
	SubscriptionPlan        string        `json:"subscription_plan,omitempty"`
	SubscriptionFeatures    []string      `json:"subscription_features,omitempty"`
	BillingStatus           string        `json:"billing_status,omitempty"`
	BillingNotice           string        `json:"billing_notice,omitempty"`
	BillingDayOfMonth       int           `json:"billing_day_of_month,omitempty"`
	BillingCollectionMode   string        `json:"billing_collection_mode,omitempty"`
	LastBillingCollectedAt  time.Time     `json:"last_billing_collected_at,omitempty"`
	NextBillingAt           time.Time     `json:"next_billing_at,omitempty"`
	BillingCollectionCursor string        `json:"billing_collection_cursor,omitempty"`
	CheckoutURL             string        `json:"checkout_url,omitempty"`
	LastWebhookEvent        string        `json:"last_webhook_event,omitempty"`
	LastWebhookAt           time.Time     `json:"last_webhook_at,omitempty"`
	FailureReason           string        `json:"failure_reason,omitempty"`
	StripeCustomerID        SecuredString `json:"stripe_customer_id,omitempty"`
	StripeSubID             SecuredString `json:"stripe_subscription_id,omitempty"`
	Status                  string        `json:"status,omitempty"`
	CreatedAt               time.Time     `json:"created_at,omitempty"`
	UpdatedAt               time.Time     `json:"updated_at,omitempty"`
}

type RegisterUserRequest struct {
	Username             string
	Password             string
	Email                string
	Role                 string
	SubscriptionPlan     string
	SubscriptionFeatures []string
}

type UnregisterUserRequest struct {
	Username string
}

type UpdateSubscriptionPlanRequest struct {
	Username             string
	SubscriptionPlan     string
	SubscriptionFeatures []string
}

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

type SubscriptionStore interface {
	Save(ctx context.Context, subscription *SubscriptionRecord) error
	Get(ctx context.Context, id string) (*SubscriptionRecord, bool, error)
	Delete(ctx context.Context, id string) error
	ListByUser(ctx context.Context, username string) ([]SubscriptionRecord, error)
	List(ctx context.Context) ([]SubscriptionRecord, error)
}

type PaymentStore interface {
	Save(ctx context.Context, payment *PaymentRecord) error
	Get(ctx context.Context, id string) (*PaymentRecord, bool, error)
	Delete(ctx context.Context, id string) error
	ListByUser(ctx context.Context, username string) ([]PaymentRecord, error)
	List(ctx context.Context) ([]PaymentRecord, error)
}

type SubscriptionStoreImpl struct {
	mu            sync.RWMutex
	subscriptions map[string]SubscriptionRecord
}

func NewSubscriptionStore() *SubscriptionStoreImpl {
	return &SubscriptionStoreImpl{subscriptions: make(map[string]SubscriptionRecord)}
}

func (s *SubscriptionStoreImpl) Save(ctx context.Context, subscription *SubscriptionRecord) error {
	if subscription == nil {
		return fmt.Errorf("subscription is nil")
	}
	id := strings.TrimSpace(subscription.ID)
	if id == "" {
		return fmt.Errorf("subscription id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	copySubscription := *subscription
	copySubscription.UpdatedAt = time.Now().UTC()
	if copySubscription.CreatedAt.IsZero() {
		copySubscription.CreatedAt = copySubscription.UpdatedAt
	}
	s.subscriptions[id] = copySubscription
	return nil
}

func (s *SubscriptionStoreImpl) Get(ctx context.Context, id string) (*SubscriptionRecord, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	subscription, ok := s.subscriptions[id]
	if !ok {
		return nil, false, nil
	}
	copySubscription := subscription
	return &copySubscription, true, nil
}

func (s *SubscriptionStoreImpl) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subscriptions, id)
	return nil
}

func (s *SubscriptionStoreImpl) ListByUser(ctx context.Context, username string) ([]SubscriptionRecord, error) {
	username = normalizeUsername(username)
	if username == "" {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	results := make([]SubscriptionRecord, 0)
	for _, subscription := range s.subscriptions {
		if normalizeUsername(subscription.UserID) == username {
			results = append(results, subscription)
		}
	}
	return results, nil
}

func (s *SubscriptionStoreImpl) List(ctx context.Context) ([]SubscriptionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	results := make([]SubscriptionRecord, 0, len(s.subscriptions))
	for _, subscription := range s.subscriptions {
		results = append(results, subscription)
	}
	return results, nil
}

type PaymentStoreImpl struct {
	mu       sync.RWMutex
	payments map[string]PaymentRecord
}

func NewPaymentStore() *PaymentStoreImpl {
	return &PaymentStoreImpl{payments: make(map[string]PaymentRecord)}
}

func (s *PaymentStoreImpl) Save(ctx context.Context, payment *PaymentRecord) error {
	if payment == nil {
		return fmt.Errorf("payment is nil")
	}
	id := strings.TrimSpace(payment.ID)
	if id == "" {
		return fmt.Errorf("payment id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	copyPayment := *payment
	copyPayment.UpdatedAt = time.Now().UTC()
	if copyPayment.CreatedAt.IsZero() {
		copyPayment.CreatedAt = copyPayment.UpdatedAt
	}
	s.payments[id] = copyPayment
	return nil
}

func (s *PaymentStoreImpl) Get(ctx context.Context, id string) (*PaymentRecord, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	payment, ok := s.payments[id]
	if !ok {
		return nil, false, nil
	}
	copyPayment := payment
	return &copyPayment, true, nil
}

func (s *PaymentStoreImpl) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.payments, id)
	return nil
}

func (s *PaymentStoreImpl) ListByUser(ctx context.Context, username string) ([]PaymentRecord, error) {
	username = normalizeUsername(username)
	if username == "" {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	results := make([]PaymentRecord, 0)
	for _, payment := range s.payments {
		if normalizeUsername(payment.UserID) == username {
			results = append(results, payment)
		}
	}
	return results, nil
}

func (s *PaymentStoreImpl) List(ctx context.Context) ([]PaymentRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	results := make([]PaymentRecord, 0, len(s.payments))
	for _, payment := range s.payments {
		results = append(results, payment)
	}
	return results, nil
}

type BillingCheckoutSession struct {
	ID     string
	URL    string
	Status string
}

type AccountTransaction interface {
	SaveUser(user *UserRecord) error
	SaveSubscription(subscription *SubscriptionRecord) error
	SavePayment(payment *PaymentRecord) error
	Commit() error
	Rollback() error
}

type AccountTransactionImpl struct {
	cfg                  *Config
	userStore            UserStore
	subscriptionStore    SubscriptionStore
	paymentStore         PaymentStore
	pendingUser          *UserRecord
	pendingSubscriptions []SubscriptionRecord
	pendingPayments      []PaymentRecord
	committed            bool
}

type AccountRepository struct {
	cfg *Config
}

func NewAccountRepository(cfg *Config) *AccountRepository {
	if cfg == nil {
		cfg = &Config{}
	}
	return &AccountRepository{cfg: cfg}
}

func (r *AccountRepository) SaveUser(ctx context.Context, tx sop.Transaction, user *UserRecord) error {
	if r == nil || r.cfg == nil {
		return fmt.Errorf("account repository is not initialized")
	}
	if user == nil {
		return fmt.Errorf("user is nil")
	}
	copyUser := *user
	copyUser.Username = normalizeUsername(copyUser.Username)
	if copyUser.Username == "" {
		return fmt.Errorf("username is required")
	}
	if err := r.cfg.ensureUserStore().Save(ctx, &copyUser); err != nil {
		return err
	}
	for i := range r.cfg.Users {
		if normalizeUsername(r.cfg.Users[i].Username) == copyUser.Username {
			r.cfg.Users[i] = copyUser
			return nil
		}
	}
	r.cfg.Users = append(r.cfg.Users, copyUser)
	return nil
}

func (r *AccountRepository) SaveSubscription(ctx context.Context, tx sop.Transaction, subscription *SubscriptionRecord) error {
	if r == nil || r.cfg == nil {
		return fmt.Errorf("account repository is not initialized")
	}
	if subscription == nil {
		return fmt.Errorf("subscription is nil")
	}
	copySubscription := *subscription
	if copySubscription.ID == "" {
		return fmt.Errorf("subscription id is required")
	}
	if err := r.cfg.ensureSubscriptionStore().Save(ctx, &copySubscription); err != nil {
		return err
	}
	for i := range r.cfg.Subscriptions {
		if r.cfg.Subscriptions[i].ID == copySubscription.ID {
			r.cfg.Subscriptions[i] = copySubscription
			return nil
		}
	}
	r.cfg.Subscriptions = append(r.cfg.Subscriptions, copySubscription)
	return nil
}

func (r *AccountRepository) SavePayment(ctx context.Context, tx sop.Transaction, payment *PaymentRecord) error {
	if r == nil || r.cfg == nil {
		return fmt.Errorf("account repository is not initialized")
	}
	if payment == nil {
		return fmt.Errorf("payment is nil")
	}
	copyPayment := *payment
	if copyPayment.ID == "" {
		return fmt.Errorf("payment id is required")
	}
	if err := r.cfg.ensurePaymentStore().Save(ctx, &copyPayment); err != nil {
		return err
	}
	for i := range r.cfg.Payments {
		if r.cfg.Payments[i].ID == copyPayment.ID {
			r.cfg.Payments[i] = copyPayment
			return nil
		}
	}
	r.cfg.Payments = append(r.cfg.Payments, copyPayment)
	return nil
}

func (r *AccountRepository) FindUser(ctx context.Context, tx sop.Transaction, username string) (*UserRecord, bool, error) {
	if r == nil || r.cfg == nil {
		return nil, false, fmt.Errorf("account repository is not initialized")
	}
	return r.cfg.findUserRecord(ctx, username)
}

func (r *AccountRepository) ListSubscriptionsByUser(ctx context.Context, tx sop.Transaction, username string) ([]SubscriptionRecord, error) {
	if r == nil || r.cfg == nil {
		return nil, fmt.Errorf("account repository is not initialized")
	}
	return r.cfg.ensureSubscriptionStore().ListByUser(ctx, username)
}

func (r *AccountRepository) ListPaymentsByUser(ctx context.Context, tx sop.Transaction, username string) ([]PaymentRecord, error) {
	if r == nil || r.cfg == nil {
		return nil, fmt.Errorf("account repository is not initialized")
	}
	return r.cfg.ensurePaymentStore().ListByUser(ctx, username)
}

func (r *AccountRepository) DeleteUser(ctx context.Context, tx sop.Transaction, username string) error {
	if r == nil || r.cfg == nil {
		return fmt.Errorf("account repository is not initialized")
	}
	if store := r.cfg.UsersStore; store != nil {
		if err := store.Delete(ctx, username); err != nil {
			return err
		}
	}
	users := r.cfg.Users[:0]
	for _, existing := range r.cfg.Users {
		if normalizeUsername(existing.Username) == normalizeUsername(username) {
			continue
		}
		users = append(users, existing)
	}
	r.cfg.Users = users
	return nil
}

func (r *AccountRepository) DeleteSubscription(ctx context.Context, tx sop.Transaction, id string) error {
	if r == nil || r.cfg == nil {
		return fmt.Errorf("account repository is not initialized")
	}
	if err := r.cfg.ensureSubscriptionStore().Delete(ctx, id); err != nil {
		return err
	}
	subscriptions := r.cfg.Subscriptions[:0]
	for _, existing := range r.cfg.Subscriptions {
		if existing.ID == id {
			continue
		}
		subscriptions = append(subscriptions, existing)
	}
	r.cfg.Subscriptions = subscriptions
	return nil
}

func (r *AccountRepository) DeletePayment(ctx context.Context, tx sop.Transaction, id string) error {
	if r == nil || r.cfg == nil {
		return fmt.Errorf("account repository is not initialized")
	}
	if err := r.cfg.ensurePaymentStore().Delete(ctx, id); err != nil {
		return err
	}
	payments := r.cfg.Payments[:0]
	for _, existing := range r.cfg.Payments {
		if existing.ID == id {
			continue
		}
		payments = append(payments, existing)
	}
	r.cfg.Payments = payments
	return nil
}

func (c *Config) BeginAccountTransaction() (AccountTransaction, error) {
	return &AccountTransactionImpl{
		cfg:                  c,
		userStore:            c.ensureUserStore(),
		subscriptionStore:    c.ensureSubscriptionStore(),
		paymentStore:         c.ensurePaymentStore(),
		pendingSubscriptions: make([]SubscriptionRecord, 0),
		pendingPayments:      make([]PaymentRecord, 0),
	}, nil
}

func (tx *AccountTransactionImpl) SaveUser(user *UserRecord) error {
	if user == nil {
		return fmt.Errorf("user is nil")
	}
	if err := tx.userStore.Save(context.Background(), user); err != nil {
		return err
	}
	tx.pendingUser = user
	return nil
}

func (tx *AccountTransactionImpl) SaveSubscription(subscription *SubscriptionRecord) error {
	if subscription == nil {
		return fmt.Errorf("subscription is nil")
	}
	if err := tx.subscriptionStore.Save(context.Background(), subscription); err != nil {
		return err
	}
	tx.pendingSubscriptions = append(tx.pendingSubscriptions, *subscription)
	return nil
}

func (tx *AccountTransactionImpl) SavePayment(payment *PaymentRecord) error {
	if payment == nil {
		return fmt.Errorf("payment is nil")
	}
	if err := tx.paymentStore.Save(context.Background(), payment); err != nil {
		return err
	}
	tx.pendingPayments = append(tx.pendingPayments, *payment)
	return nil
}

func (tx *AccountTransactionImpl) Commit() error {
	if tx.committed {
		return nil
	}
	if tx.cfg != nil {
		if tx.pendingUser != nil {
			tx.cfg.Users = append(tx.cfg.Users, *tx.pendingUser)
		}
		for _, subscription := range tx.pendingSubscriptions {
			tx.cfg.Subscriptions = append(tx.cfg.Subscriptions, subscription)
		}
		for _, payment := range tx.pendingPayments {
			tx.cfg.Payments = append(tx.cfg.Payments, payment)
		}
	}
	tx.committed = true
	return nil
}

func (tx *AccountTransactionImpl) Rollback() error {
	if tx.committed {
		return nil
	}
	if tx.cfg != nil {
		if tx.pendingUser != nil {
			if store := tx.userStore; store != nil {
				_ = store.Delete(context.Background(), tx.pendingUser.Username)
			}
		}
		for _, subscription := range tx.pendingSubscriptions {
			if store := tx.subscriptionStore; store != nil {
				_ = store.Delete(context.Background(), subscription.ID)
			}
		}
		for _, payment := range tx.pendingPayments {
			if store := tx.paymentStore; store != nil {
				_ = store.Delete(context.Background(), payment.ID)
			}
		}
	}
	tx.committed = true
	return nil
}

type BillingWebhookEvent struct {
	EventType string
	EventID   string
	UserID    string
	Plan      string
}

type AuthenticatedAccount struct {
	Username string
	Roles    []string
}

// UserStore persists user identity records for the built-in auth model.
type UserStore interface {
	Save(ctx context.Context, user *UserRecord) error
	Get(ctx context.Context, username string) (*UserRecord, bool, error)
	Delete(ctx context.Context, username string) error
	List(ctx context.Context) ([]UserRecord, error)
}

type SystemUserStore struct {
	storeName string
}

func NewSystemUserStore() *SystemUserStore {
	return &SystemUserStore{storeName: "users"}
}

func (s *SystemUserStore) Save(ctx context.Context, user *UserRecord) error {
	if user == nil {
		return fmt.Errorf("user is nil")
	}
	username := normalizeUsername(user.Username)
	if username == "" {
		return fmt.Errorf("username is required")
	}

	db, err := getSystemDB(ctx)
	if err != nil {
		return fmt.Errorf("system user store failed to get system db: %w", err)
	}
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		return fmt.Errorf("system user store failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	options := sop.ConfigureStore(s.storeName, true, 1000, "System users", sop.SmallData, "")
	options.IsPrimitiveKey = true
	store, err := database.NewBtree[string, UserRecord](ctx, db.Options(), s.storeName, tx, nil, options)
	if err != nil {
		return fmt.Errorf("system user store failed to open/create store: %w", err)
	}
	copyUser := *user
	copyUser.Username = username
	copyUser.UpdatedAt = time.Now().UTC()
	if copyUser.CreatedAt.IsZero() {
		copyUser.CreatedAt = copyUser.UpdatedAt
	}
	if _, err := store.Upsert(ctx, username, copyUser); err != nil {
		return fmt.Errorf("system user store failed to upsert user: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("system user store failed to commit: %w", err)
	}
	return nil
}

func (s *SystemUserStore) Get(ctx context.Context, username string) (*UserRecord, bool, error) {
	username = normalizeUsername(username)
	if username == "" {
		return nil, false, nil
	}

	db, err := getSystemDB(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("system user store failed to get system db: %w", err)
	}
	tx, err := db.BeginTransaction(ctx, sop.ForReading, -1)
	if err != nil {
		return nil, false, fmt.Errorf("system user store failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	options := sop.ConfigureStore(s.storeName, true, 1000, "System users", sop.SmallData, "")
	options.IsPrimitiveKey = true
	store, err := database.NewBtree[string, UserRecord](ctx, db.Options(), s.storeName, tx, nil, options)
	if err != nil {
		return nil, false, fmt.Errorf("system user store failed to open/create store: %w", err)
	}
	if found, err := store.Find(ctx, username, true); err != nil {
		return nil, false, fmt.Errorf("system user store failed to find user: %w", err)
	} else if !found {
		return nil, false, nil
	}
	value, err := store.GetCurrentValue(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("system user store failed to read user: %w", err)
	}
	copyUser := value
	return &copyUser, true, nil
}

func (s *SystemUserStore) Delete(ctx context.Context, username string) error {
	username = normalizeUsername(username)
	if username == "" {
		return nil
	}

	db, err := getSystemDB(ctx)
	if err != nil {
		return fmt.Errorf("system user store failed to get system db: %w", err)
	}
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		return fmt.Errorf("system user store failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	options := sop.ConfigureStore(s.storeName, true, 1000, "System users", sop.SmallData, "")
	options.IsPrimitiveKey = true
	store, err := database.NewBtree[string, UserRecord](ctx, db.Options(), s.storeName, tx, nil, options)
	if err != nil {
		return fmt.Errorf("system user store failed to open/create store: %w", err)
	}
	if _, err := store.Remove(ctx, username); err != nil {
		return fmt.Errorf("system user store failed to remove user: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("system user store failed to commit: %w", err)
	}
	return nil
}

func (s *SystemUserStore) List(ctx context.Context) ([]UserRecord, error) {
	db, err := getSystemDB(ctx)
	if err != nil {
		return nil, fmt.Errorf("system user store failed to get system db: %w", err)
	}
	tx, err := db.BeginTransaction(ctx, sop.ForReading, -1)
	if err != nil {
		return nil, fmt.Errorf("system user store failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	options := sop.ConfigureStore(s.storeName, true, 1000, "System users", sop.SmallData, "")
	options.IsPrimitiveKey = true
	store, err := database.NewBtree[string, UserRecord](ctx, db.Options(), s.storeName, tx, nil, options)
	if err != nil {
		return nil, fmt.Errorf("system user store failed to open/create store: %w", err)
	}
	ok, err := store.First(ctx)
	if err != nil {
		return nil, fmt.Errorf("system user store failed to seek first user: %w", err)
	}
	if !ok {
		return nil, nil
	}
	results := []UserRecord{}
	for {
		value, err := store.GetCurrentValue(ctx)
		if err != nil {
			return nil, fmt.Errorf("system user store failed to read user: %w", err)
		}
		results = append(results, value)
		if ok, err := store.Next(ctx); err != nil {
			return nil, fmt.Errorf("system user store failed to iterate users: %w", err)
		} else if !ok {
			break
		}
	}
	return results, nil
}

var userStoreOverride UserStore

// SetUserStoreOverride installs a custom user store for shared auth flows.
// When nil, the built-in default store is used.
func SetUserStoreOverride(store UserStore) {
	userStoreOverride = store
}

// GetUserStoreOverride returns the currently installed user-store override.
func GetUserStoreOverride() UserStore {
	return userStoreOverride
}

type UserStoreImpl struct {
	mu    sync.RWMutex
	users map[string]UserRecord
}

func NewUserStore() *UserStoreImpl {
	return &UserStoreImpl{users: make(map[string]UserRecord)}
}

func (s *UserStoreImpl) Save(ctx context.Context, user *UserRecord) error {
	if user == nil {
		return fmt.Errorf("user is nil")
	}
	username := normalizeUsername(user.Username)
	if username == "" {
		return fmt.Errorf("username is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	copyUser := *user
	copyUser.Username = username
	copyUser.UpdatedAt = time.Now().UTC()
	if copyUser.CreatedAt.IsZero() {
		copyUser.CreatedAt = copyUser.UpdatedAt
	}
	s.users[username] = copyUser
	return nil
}

func (s *UserStoreImpl) Get(ctx context.Context, username string) (*UserRecord, bool, error) {
	username = normalizeUsername(username)
	if username == "" {
		return nil, false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.users[username]
	if !ok {
		return nil, false, nil
	}
	copyUser := user
	return &copyUser, true, nil
}

func (s *UserStoreImpl) Delete(ctx context.Context, username string) error {
	username = normalizeUsername(username)
	if username == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.users, username)
	return nil
}

func (s *UserStoreImpl) List(ctx context.Context) ([]UserRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	users := make([]UserRecord, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, user)
	}
	return users, nil
}

// SessionRecord holds a bearer token for the short-lived V1 login facade.
// A B-tree-backed implementation can replace this later for cross-machine enforcement.
type SessionRecord struct {
	Token            string
	RefreshToken     string
	Username         string
	Role             string
	IssuedAt         time.Time
	ExpiresAt        time.Time
	RefreshExpiresAt time.Time
}

type TokenFacade interface {
	CreateToken(ctx context.Context, username, role string) (string, error)
	CreateSession(ctx context.Context, username, role string) (string, string, error)
	Refresh(ctx context.Context, refreshToken string) (string, string, error)
	ValidateToken(ctx context.Context, token string) (*UserRecord, error)
	RevokeToken(ctx context.Context, token string)
}

const (
	defaultSessionTTL = 30 * time.Minute
	defaultRefreshTTL = 7 * 24 * time.Hour
	sessionStoreName  = "sessions"
)

type SessionStore struct {
	mu         sync.RWMutex
	ttl        time.Duration
	refreshTTL time.Duration
	storeName  string
	gcCursor   string
}

func (s *SessionStore) collectStaleSessions(ctx context.Context, grace time.Duration, maxRecords int) (int, error) {
	s.mu.Lock()
	cursor := s.gcCursor
	s.mu.Unlock()

	store, tx, err := s.getStore(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var records []SessionRecord
	var startFromFirst bool
	var lastProcessedKey string
	if cursor == "" {
		startFromFirst = true
	}

	if startFromFirst {
		if ok, err := store.First(ctx); err != nil {
			return 0, fmt.Errorf("failed to iterate sessions for garbage collection: %v", err)
		} else if !ok {
			_ = tx.Commit(ctx)
			return 0, nil
		}
	} else {
		if ok, err := store.Find(ctx, cursor, false); err != nil {
			if ok, err := store.First(ctx); err != nil {
				return 0, fmt.Errorf("failed to reset session garbage collection cursor: %v", err)
			} else if !ok {
				_ = tx.Commit(ctx)
				return 0, nil
			}
		} else if !ok {
			if ok, err := store.First(ctx); err != nil {
				return 0, fmt.Errorf("failed to reset session garbage collection cursor: %v", err)
			} else if !ok {
				_ = tx.Commit(ctx)
				return 0, nil
			}
		} else {
			if ok, err := store.Next(ctx); err != nil || !ok {
				if ok, err := store.First(ctx); err != nil {
					return 0, fmt.Errorf("failed to wrap session garbage collection cursor: %v", err)
				} else if !ok {
					_ = tx.Commit(ctx)
					return 0, nil
				}
			}
		}
	}

	for len(records) < maxRecords {
		record, err := store.GetCurrentValue(ctx)
		if err != nil {
			break
		}
		records = append(records, record)
		if record.Token != "" {
			lastProcessedKey = record.Token
		}
		if ok, err := store.Next(ctx); err != nil || !ok {
			break
		}
	}

	removed := 0
	now := time.Now().UTC()
	for _, record := range records {
		if now.Add(-grace).Before(record.ExpiresAt) && now.Add(-grace).Before(record.RefreshExpiresAt) {
			continue
		}
		if err := removeSessionRecord(ctx, store, record); err != nil {
			log.Warn("session GC: failed to remove stale session", "token_prefix", tokenPrefix(record.Token), "error", err)
			continue
		}
		removed++
	}

	if lastProcessedKey != "" {
		s.mu.Lock()
		s.gcCursor = lastProcessedKey
		s.mu.Unlock()
	}

	if err := tx.Commit(ctx); err != nil {
		return removed, fmt.Errorf("failed to commit session garbage collection: %v", err)
	}
	return removed, nil
}

func NewSessionStore(ttl time.Duration) *SessionStore {
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	return &SessionStore{ttl: ttl, refreshTTL: defaultRefreshTTL, storeName: sessionStoreName}
}

func newToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

type signedAccessClaims struct {
	Subject   string `json:"sub"`
	Role      string `json:"role"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
	TokenID   string `json:"jti"`
}

func tokenSigningSecret() []byte {
	if secret := strings.TrimSpace(os.Getenv("SOP_SESSION_SECRET")); secret != "" {
		return []byte(secret)
	}
	if secret := strings.TrimSpace(config.SessionSecret.Plaintext()); secret != "" {
		return []byte(secret)
	}
	return []byte("sop-session-default-secret")
}

func base64urlEncode(data []byte) string {
	return strings.TrimRight(base64.RawURLEncoding.EncodeToString(data), "=")
}

func base64urlDecode(data string) ([]byte, error) {
	if missing := len(data) % 4; missing != 0 {
		data += strings.Repeat("=", 4-missing)
	}
	return base64.URLEncoding.DecodeString(data)
}

func signAccessToken(username, role string, issuedAt, expiresAt time.Time, tokenID string) (string, error) {
	header := base64urlEncode([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims := signedAccessClaims{
		Subject:   username,
		Role:      role,
		IssuedAt:  issuedAt.Unix(),
		ExpiresAt: expiresAt.Unix(),
		TokenID:   tokenID,
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64urlEncode(claimsJSON)
	input := header + "." + payload
	mac := hmac.New(sha256.New, tokenSigningSecret())
	_, _ = mac.Write([]byte(input))
	signature := base64urlEncode(mac.Sum(nil))
	return input + "." + signature, nil
}

func parseAndVerifySignedAccessToken(token string) (*signedAccessClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid signed token")
	}
	header, payload, signature := parts[0], parts[1], parts[2]
	mac := hmac.New(sha256.New, tokenSigningSecret())
	_, _ = mac.Write([]byte(header + "." + payload))
	expected := base64urlEncode(mac.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return nil, fmt.Errorf("invalid signature")
	}

	claimsJSON, err := base64urlDecode(payload)
	if err != nil {
		return nil, fmt.Errorf("invalid payload")
	}
	var claims signedAccessClaims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return nil, fmt.Errorf("invalid claims")
	}
	if claims.Subject == "" || claims.Role == "" || claims.TokenID == "" {
		return nil, fmt.Errorf("invalid claims")
	}
	if time.Now().UTC().Unix() >= claims.ExpiresAt {
		return nil, fmt.Errorf("expired session")
	}
	return &claims, nil
}

func (s *SessionStore) CreateToken(ctx context.Context, username, role string) (string, error) {
	store, tx, err := s.getStore(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	opaqueToken, err := newToken()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	accessToken, err := signAccessToken(username, role, now, now.Add(s.ttl), opaqueToken)
	if err != nil {
		return "", err
	}
	record := SessionRecord{Token: accessToken, Username: username, Role: role, IssuedAt: now, ExpiresAt: now.Add(s.ttl)}

	if ok, err := store.Add(ctx, accessToken, record); !ok || err != nil {
		if err != nil {
			return "", fmt.Errorf("failed to create token: %v", err)
		}
		return "", fmt.Errorf("failed to create token, collision")
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("failed to commit token: %v", err)
	}
	return accessToken, nil
}

func (s *SessionStore) CreateSession(ctx context.Context, username, role string) (string, string, error) {
	store, tx, err := s.getStore(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC()
	opaqueAccessToken, err := newToken()
	if err != nil {
		return "", "", err
	}
	accessToken, err := signAccessToken(username, role, now, now.Add(s.ttl), opaqueAccessToken)
	if err != nil {
		return "", "", err
	}
	refreshToken, err := newToken()
	if err != nil {
		return "", "", err
	}
	record := SessionRecord{
		Token:            accessToken,
		RefreshToken:     refreshToken,
		Username:         username,
		Role:             role,
		IssuedAt:         now,
		ExpiresAt:        now.Add(s.ttl),
		RefreshExpiresAt: now.Add(s.refreshTTL),
	}

	if ok, err := store.Add(ctx, accessToken, record); !ok || err != nil {
		if err != nil {
			return "", "", fmt.Errorf("failed to create session: %v", err)
		}
		return "", "", fmt.Errorf("failed to create session, token may already exist")
	}
	if ok, err := store.Add(ctx, refreshToken, record); !ok || err != nil {
		// Attempt to roll back the access token if the refresh token fails
		store.Remove(ctx, accessToken)
		if err != nil {
			return "", "", fmt.Errorf("failed to create refresh session: %v", err)
		}
		return "", "", fmt.Errorf("failed to create refresh session, token may already exist")
	}

	if err := tx.Commit(ctx); err != nil {
		return "", "", fmt.Errorf("failed to commit session: %v", err)
	}
	return accessToken, refreshToken, nil
}

func (s *SessionStore) Refresh(ctx context.Context, refreshToken string) (string, string, error) {
	store, tx, err := s.getStore(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(ctx)

	found, err := store.Find(ctx, refreshToken, false)
	if err != nil {
		return "", "", fmt.Errorf("failed to get refresh token details: %v", err)
	}
	if !found {
		return "", "", fmt.Errorf("invalid refresh token")
	}
	r, err := store.GetCurrentValue(ctx)
	if err != nil {
		return "", "", fmt.Errorf("failed to get refresh token value: %v", err)
	}

	if time.Now().UTC().After(r.RefreshExpiresAt) {
		if err := removeSessionRecord(ctx, store, r); err != nil {
			log.Warn("Refresh: failed to remove expired refresh session", "error", err)
		}
		if err := tx.Commit(ctx); err != nil {
			log.Warn("Refresh: failed to commit expired refresh token cleanup", "error", err)
		}
		return "", "", fmt.Errorf("expired refresh token")
	}

	now := time.Now().UTC()
	opaqueAccessToken, err := newToken()
	if err != nil {
		return "", "", err
	}
	accessToken, err := signAccessToken(r.Username, r.Role, now, r.ExpiresAt, opaqueAccessToken)
	if err != nil {
		return "", "", err
	}
	newRefreshToken, err := newToken()
	if err != nil {
		return "", "", err
	}

	newRecord := SessionRecord{
		Token:            accessToken,
		RefreshToken:     newRefreshToken,
		Username:         r.Username,
		Role:             r.Role,
		IssuedAt:         now,
		ExpiresAt:        r.ExpiresAt,
		RefreshExpiresAt: r.RefreshExpiresAt,
	}

	// Add new tokens
	if ok, err := store.Add(ctx, accessToken, newRecord); !ok || err != nil {
		if err != nil {
			return "", "", fmt.Errorf("failed to create new access token: %v", err)
		}
		return "", "", fmt.Errorf("failed to create new access token, collision")
	}
	if ok, err := store.Add(ctx, newRefreshToken, newRecord); !ok || err != nil {
		store.Remove(ctx, accessToken) // Rollback
		if err != nil {
			return "", "", fmt.Errorf("failed to create new refresh token: %v", err)
		}
		return "", "", fmt.Errorf("failed to create new refresh token, collision")
	}

	// Remove old tokens
	store.Remove(ctx, r.Token)
	store.Remove(ctx, r.RefreshToken)

	if err := tx.Commit(ctx); err != nil {
		return "", "", fmt.Errorf("failed to commit refresh: %v", err)
	}
	return accessToken, newRefreshToken, nil
}

func isUserAccountActive(ctx context.Context, username, role string) bool {
	username = normalizeUsername(username)
	if username == "" {
		return false
	}
	if strings.EqualFold(username, "root") {
		return true
	}
	if strings.EqualFold(username, "ui-client") && strings.EqualFold(role, "daemon-user") {
		return true
	}
	if strings.EqualFold(role, "daemon-user") {
		return true
	}
	user, ok, err := config.findUserRecord(ctx, username)
	if err != nil || !ok || user == nil {
		return false
	}
	if strings.TrimSpace(user.Status) == "" {
		return true
	}
	return strings.EqualFold(user.Status, "active")
}

func (s *SessionStore) ValidateToken(ctx context.Context, token string) (*UserRecord, error) {
	return s.validateToken(ctx, token, true)
}

func (s *SessionStore) ValidateTokenWithoutAccountCheck(ctx context.Context, token string) (*UserRecord, error) {
	return s.validateToken(ctx, token, false)
}

func (s *SessionStore) validateToken(ctx context.Context, token string, requireActiveAccount bool) (*UserRecord, error) {
	if claims, err := parseAndVerifySignedAccessToken(token); err == nil {
		if requireActiveAccount && !isUserAccountActive(ctx, claims.Subject, claims.Role) {
			return nil, fmt.Errorf("invalid session")
		}
		if user, ok, lookupErr := config.findUserRecord(ctx, claims.Subject); lookupErr == nil && ok && user != nil {
			if strings.TrimSpace(user.Username) == "" {
				user.Username = claims.Subject
			}
			if strings.TrimSpace(user.Role) == "" {
				user.Role = claims.Role
			}
			return user, nil
		}
		return &UserRecord{Username: claims.Subject, Role: claims.Role}, nil
	}

	store, tx, err := s.getStore(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	found, err := store.Find(ctx, token, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get token details: %v", err)
	}
	if !found {
		return nil, fmt.Errorf("invalid session")
	}
	r, err := store.GetCurrentValue(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get token value: %v", err)
	}

	now := time.Now().UTC()
	if now.After(r.ExpiresAt) {
		log.Debug("ValidateToken: expired session",
			"token_prefix", tokenPrefix(token),
			"expires_at", r.ExpiresAt)
		if err := removeSessionRecord(ctx, store, r); err != nil {
			log.Warn("ValidateToken: failed to remove expired session", "error", err)
		}
		if err := tx.Commit(ctx); err != nil {
			log.Warn("ValidateToken: failed to commit expired session cleanup", "error", err)
		}
		return nil, fmt.Errorf("expired session")
	}
	if requireActiveAccount && !isUserAccountActive(ctx, r.Username, r.Role) {
		if err := removeSessionRecord(ctx, store, r); err != nil {
			log.Warn("ValidateToken: failed to remove revoked session", "error", err)
		}
		if err := tx.Commit(ctx); err != nil {
			log.Warn("ValidateToken: failed to commit revoked session cleanup", "error", err)
		}
		return nil, fmt.Errorf("invalid session")
	}

	if user, ok, lookupErr := config.findUserRecord(ctx, r.Username); lookupErr == nil && ok && user != nil {
		if strings.TrimSpace(user.Username) == "" {
			user.Username = r.Username
		}
		if strings.TrimSpace(user.Role) == "" {
			user.Role = r.Role
		}
		return user, nil
	}

	if err := tx.Commit(ctx); err != nil {
		log.Warn("failed to commit token validation", "error", err)
	}

	log.Debug("ValidateToken: successful validation",
		"token_prefix", tokenPrefix(token),
		"expires_at", r.ExpiresAt)

	return &UserRecord{Username: r.Username, Role: r.Role}, nil
}

func (s *SessionStore) RevokeToken(ctx context.Context, token string) {
	store, tx, err := s.getStore(ctx)
	if err != nil {
		log.Warn("failed to get store for token revocation", "error", err)
		return
	}
	defer tx.Rollback(ctx)

	found, err := store.Find(ctx, token, false)
	if err != nil {
		log.Warn("failed to get token details for revocation", "error", err)
		return
	}
	if !found {
		// If the token itself is not found, it might have been a refresh token.
		// We don't have an index for that, so we can't easily revoke.
		// This is a limitation of the current design.
		// A full implementation might require iterating or a secondary index.
		return
	}
	r, err := store.GetCurrentValue(ctx)
	if err != nil {
		log.Warn("failed to get token value for revocation", "error", err)
		return
	}

	// Remove both the access and refresh tokens to fully invalidate the session.
	store.Remove(ctx, r.Token)
	store.Remove(ctx, r.RefreshToken)
	if err := tx.Commit(ctx); err != nil {
		log.Warn("failed to commit token revocation", "error", err)
	}
}

func (s *SessionStore) revokeSessionsForUser(ctx context.Context, username string) {
	store, tx, err := s.getStore(ctx)
	if err != nil {
		log.Warn("failed to get store for user session revocation", "error", err)
		return
	}
	defer tx.Rollback(ctx)

	var records []SessionRecord
	if ok, err := store.First(ctx); err != nil {
		log.Warn("failed to iterate sessions for user revocation", "error", err)
		return
	} else if !ok {
		_ = tx.Commit(ctx)
		return
	}

	for {
		record, err := store.GetCurrentValue(ctx)
		if err != nil {
			break
		}
		if strings.EqualFold(record.Username, username) {
			records = append(records, record)
		}
		if ok, err := store.Next(ctx); err != nil || !ok {
			break
		}
	}

	for _, record := range records {
		if err := removeSessionRecord(ctx, store, record); err != nil {
			log.Warn("failed to revoke user session", "user", username, "error", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		log.Warn("failed to commit user session revocation", "error", err)
	}
}

var (
	tokenFacade     TokenFacade
	tokenFacadeOnce sync.Once
)

func currentTokenFacade() TokenFacade {
	tokenFacadeOnce.Do(func() {
		tokenFacade = NewSessionStore(sessionTTL())
	})
	if tokenFacade == nil {
		tokenFacade = NewSessionStore(sessionTTL())
	}
	// Allow TTL to be updated if config changes, without resetting the whole store.
	if s, ok := tokenFacade.(*SessionStore); ok && s != nil && s.ttl != sessionTTL() {
		s.mu.Lock()
		s.ttl = sessionTTL()
		s.mu.Unlock()
	}
	return tokenFacade
}

func sessionTTL() time.Duration {
	if config.SessionTokenTTLMinutes > 0 {
		return time.Duration(config.SessionTokenTTLMinutes) * time.Minute
	}
	return defaultSessionTTL
}

// CreateSession issues a shared backend access/refresh token pair for the given user.
func CreateSession(ctx context.Context, username, role string) (string, string, error) {
	return currentTokenFacade().CreateSession(ctx, username, role)
}

// RefreshSession rotates a shared backend refresh token into a new access/refresh pair.
func RefreshSession(ctx context.Context, refreshToken string) (string, string, error) {
	return currentTokenFacade().Refresh(ctx, refreshToken)
}

// ValidateSessionToken verifies a shared backend session token using the singleton token facade.
func ValidateSessionToken(ctx context.Context, token string) (*UserRecord, error) {
	return currentTokenFacade().ValidateToken(ctx, token)
}

func tokenPrefix(token string) string {
	if len(token) <= 8 {
		return token
	}
	return token[:8]
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func normalizeRole(role string) string {
	role = strings.TrimSpace(role)
	switch role {
	case sop.RoleAdmin, sop.RoleSystemAdmin, sop.RoleUser, sop.RoleGuest:
		return role
	default:
		return sop.RoleUser
	}
}

func roleLevel(role string) int {
	switch normalizeRole(role) {
	case sop.RoleSystemAdmin:
		return 3
	case sop.RoleAdmin:
		return 2
	case sop.RoleUser:
		return 1
	case sop.RoleGuest:
		return 0
	default:
		return 1
	}
}

func canManageRoleChange(ctx context.Context, actorUsername, actorRole, targetRole string) bool {
	actorUsername = normalizeUsername(actorUsername)
	actorRole = normalizeRole(actorRole)
	targetRole = normalizeRole(targetRole)

	if actorUsername == "root" {
		return true
	}
	if strings.EqualFold(actorRole, sop.RoleSystemAdmin) {
		return true
	}
	if !sop.IsAdminRole(actorRole) {
		return false
	}
	return roleLevel(targetRole) <= roleLevel(actorRole)
}

func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(strings.TrimSpace(password)), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func extractBearerToken(authHeader string) string {
	parts := strings.SplitN(strings.TrimSpace(authHeader), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func (c *Config) authFromAuthorization(authHeader string) (string, string) {
	// Protected endpoints should use real session-token validation only.
	// Do not accept legacy username:password bearer tokens here.
	return "", ""
}

func (c *Config) SetUser(username, password, role string) error {
	return c.SetUserWithFeatures(username, password, role, nil)
}

func (c *Config) SetUserWithFeatures(username, password, role string, features []string) error {
	username = normalizeUsername(username)
	if username == "" || strings.TrimSpace(password) == "" {
		return fmt.Errorf("username and password are required")
	}

	for _, existing := range c.Users {
		if normalizeUsername(existing.Username) == username {
			return fmt.Errorf("username already exists")
		}
	}

	hash, err := hashPassword(password)
	if err != nil {
		return err
	}

	filtered := make([]string, 0, len(features))
	seen := make(map[string]struct{}, len(features))
	for _, feature := range features {
		trimmed := strings.TrimSpace(strings.ToLower(feature))
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		filtered = append(filtered, trimmed)
	}

	record := UserRecord{
		Username:             username,
		Role:                 normalizeRole(role),
		PasswordHash:         SecuredString(hash),
		SubscriptionFeatures: filtered,
		Status:               "active",
	}
	c.Users = append(c.Users, record)
	if store := c.ensureUserStore(); store != nil {
		if err := store.Save(context.Background(), &record); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) ensureUserStore() UserStore {
	if userStoreOverride != nil {
		c.UsersStore = userStoreOverride
		return c.UsersStore
	}
	if c.SystemDB != nil || config.SystemDB != nil {
		if _, isSystemStore := c.UsersStore.(*SystemUserStore); !isSystemStore {
			if c.UsersStore != nil {
				if legacyStore, ok := c.UsersStore.(*UserStoreImpl); ok {
					users := make([]UserRecord, 0, len(legacyStore.users))
					for _, user := range legacyStore.users {
						users = append(users, user)
					}
					c.UsersStore = NewSystemUserStore()
					for i := range users {
						_ = c.UsersStore.Save(context.Background(), &users[i])
					}
				} else {
					c.UsersStore = NewSystemUserStore()
				}
			} else {
				c.UsersStore = NewSystemUserStore()
			}
		}
		return c.UsersStore
	}
	if c.UsersStore != nil {
		return c.UsersStore
	}
	c.UsersStore = NewUserStore()
	return c.UsersStore
}

func (c *Config) syncUsersStoreFromConfig(ctx context.Context) error {
	if c == nil {
		return nil
	}
	store := c.ensureUserStore()
	if store == nil {
		return nil
	}
	if len(c.Users) == 0 {
		return nil
	}
	for i := range c.Users {
		if err := store.Save(ctx, &c.Users[i]); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) syncUsersFromStore(ctx context.Context) error {
	if c == nil {
		return nil
	}
	store := c.ensureUserStore()
	if store == nil {
		return nil
	}
	persisted, err := store.List(ctx)
	if err != nil {
		return err
	}
	if len(persisted) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(persisted))
	merged := make([]UserRecord, 0, len(persisted)+len(c.Users))
	for _, user := range persisted {
		normalized := normalizeUsername(user.Username)
		if normalized == "" {
			continue
		}
		seen[normalized] = struct{}{}
		merged = append(merged, user)
	}
	for _, user := range c.Users {
		normalized := normalizeUsername(user.Username)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		merged = append(merged, user)
	}
	c.Users = merged
	return nil
}

func (c *Config) ensureProvisionedUsersStore(ctx context.Context) error {
	if c == nil {
		return nil
	}
	store := c.ensureUserStore()
	if store == nil {
		return nil
	}
	if _, err := store.List(ctx); err != nil {
		return fmt.Errorf("failed to initialize users store: %w", err)
	}
	return nil
}

func (c *Config) ensureSubscriptionStore() SubscriptionStore {
	if c.SubscriptionsStore != nil {
		return c.SubscriptionsStore
	}
	c.SubscriptionsStore = NewSubscriptionStore()
	return c.SubscriptionsStore
}

func (c *Config) ensurePaymentStore() PaymentStore {
	if c.PaymentsStore != nil {
		return c.PaymentsStore
	}
	c.PaymentsStore = NewPaymentStore()
	return c.PaymentsStore
}

func (c *Config) findUserRecord(ctx context.Context, username string) (*UserRecord, bool, error) {
	username = normalizeUsername(username)
	if username == "" {
		return nil, false, nil
	}
	if err := c.syncUsersStoreFromConfig(ctx); err != nil {
		return nil, false, err
	}
	if store := c.UsersStore; store != nil {
		user, ok, err := store.Get(ctx, username)
		if err != nil {
			return nil, false, err
		}
		if ok {
			return user, true, nil
		}
	}
	return nil, false, nil
}

func (c *Config) FindUserRecord(ctx context.Context, username string) (*UserRecord, bool, error) {
	return c.findUserRecord(ctx, username)
}

// RegisterUser creates a user record using the shared auth store and default account semantics.
func (c *Config) RegisterUser(ctx context.Context, username, password, email, role, subscriptionPlan string) (*UserRecord, error) {
	return c.RegisterUserWithRequest(ctx, RegisterUserRequest{
		Username:         username,
		Password:         password,
		Email:            email,
		Role:             role,
		SubscriptionPlan: subscriptionPlan,
	})
}

// RegisterUserWithRequest creates a user record from an explicit request object for shared backend callers.
func (c *Config) RegisterUserWithRequest(ctx context.Context, req RegisterUserRequest) (*UserRecord, error) {
	username := normalizeUsername(req.Username)
	if username == "" || strings.TrimSpace(req.Password) == "" {
		return nil, fmt.Errorf("username and password are required")
	}
	if _, ok, err := c.findUserRecord(ctx, username); err != nil {
		return nil, err
	} else if ok {
		return nil, fmt.Errorf("username already exists")
	}

	role := normalizeRole(req.Role)
	if role == "" {
		role = sop.RoleUser
	}
	subscriptionPlan := strings.TrimSpace(req.SubscriptionPlan)
	if subscriptionPlan == "" {
		subscriptionPlan = "starter"
	}

	hash, err := hashPassword(req.Password)
	if err != nil {
		return nil, err
	}

	billingStatus := "inactive"
	if subscriptionPlan != "" && !strings.EqualFold(subscriptionPlan, "none") {
		billingStatus = "pending"
	}

	record := UserRecord{
		Username:             username,
		Role:                 role,
		PasswordHash:         SecuredString(hash),
		Email:                SecuredString(strings.TrimSpace(req.Email)),
		SubscriptionPlan:     subscriptionPlan,
		SubscriptionFeatures: append([]string(nil), req.SubscriptionFeatures...),
		BillingStatus:        billingStatus,
		Status:               "active",
	}
	if err := c.ensureUserStore().Save(ctx, &record); err != nil {
		return nil, err
	}
	c.Users = append(c.Users, record)
	return &record, nil
}

// UnregisterUser removes a user from the shared auth store and revokes any related sessions.
func (c *Config) UnregisterUser(ctx context.Context, username string) error {
	return c.UnregisterUserWithRequest(ctx, UnregisterUserRequest{Username: username})
}

// UnregisterUserWithRequest removes a user using an explicit request object for shared backend callers.
func (c *Config) UnregisterUserWithRequest(ctx context.Context, req UnregisterUserRequest) error {
	username := normalizeUsername(req.Username)
	if username == "" {
		return nil
	}

	if store := c.UsersStore; store != nil {
		if err := store.Delete(ctx, username); err != nil {
			return err
		}
	}
	if store := c.ensureSubscriptionStore(); store != nil {
		subscriptions, err := store.ListByUser(ctx, username)
		if err != nil {
			return err
		}
		for _, subscription := range subscriptions {
			if err := store.Delete(ctx, subscription.ID); err != nil {
				return err
			}
		}
	}
	if store := c.ensurePaymentStore(); store != nil {
		payments, err := store.ListByUser(ctx, username)
		if err != nil {
			return err
		}
		for _, payment := range payments {
			if err := store.Delete(ctx, payment.ID); err != nil {
				return err
			}
		}
	}

	users := c.Users[:0]
	for _, existing := range c.Users {
		if normalizeUsername(existing.Username) == username {
			continue
		}
		users = append(users, existing)
	}
	c.Users = users

	if tokenFacade != nil {
		if store, ok := tokenFacade.(*SessionStore); ok {
			store.revokeSessionsForUser(ctx, username)
		}
	}
	return nil
}

// UpdateSubscriptionPlan changes a user's stored subscription plan using the shared auth/user store.
func (c *Config) UpdateSubscriptionPlan(ctx context.Context, username, subscriptionPlan string) (*UserRecord, error) {
	return c.UpdateSubscriptionPlanWithRequest(ctx, UpdateSubscriptionPlanRequest{
		Username:         username,
		SubscriptionPlan: subscriptionPlan,
	})
}

// UpdateSubscriptionPlanWithRequest changes a user's stored subscription plan from an explicit request object.
func (c *Config) UpdateSubscriptionPlanWithRequest(ctx context.Context, req UpdateSubscriptionPlanRequest) (*UserRecord, error) {
	username := normalizeUsername(req.Username)
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}
	plan := strings.TrimSpace(req.SubscriptionPlan)
	if plan == "" {
		plan = "none"
	}

	user, ok, err := c.findUserRecord(ctx, username)
	if err != nil {
		return nil, err
	}
	if !ok || user == nil {
		return nil, fmt.Errorf("user not found")
	}

	user.SubscriptionPlan = plan
	if req.SubscriptionFeatures != nil {
		user.SubscriptionFeatures = append([]string(nil), req.SubscriptionFeatures...)
	}
	if plan == "none" {
		user.BillingStatus = "inactive"
		user.StripeSubID = ""
	} else if user.BillingStatus == "" || user.BillingStatus == "inactive" {
		user.BillingStatus = "pending"
	}
	user.UpdatedAt = time.Now().UTC()
	if err := c.ensureUserStore().Save(ctx, user); err != nil {
		return nil, err
	}
	for i := range c.Users {
		if normalizeUsername(c.Users[i].Username) == username {
			c.Users[i].SubscriptionPlan = plan
			c.Users[i].SubscriptionFeatures = append([]string(nil), user.SubscriptionFeatures...)
			c.Users[i].BillingStatus = user.BillingStatus
			c.Users[i].StripeSubID = user.StripeSubID
			c.Users[i].UpdatedAt = user.UpdatedAt
			break
		}
	}
	return user, nil
}

func (c *Config) SubmitPayment(ctx context.Context, username, plan string, amount float64, currency, status, providerPaymentID, eventID string, occurredAt time.Time) (*PaymentRecord, *UserRecord, error) {
	username = normalizeUsername(username)
	if username == "" {
		return nil, nil, fmt.Errorf("username is required")
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if strings.TrimSpace(plan) == "" {
		plan = "starter"
	}

	user, ok, err := c.findUserRecord(ctx, username)
	if err != nil {
		return nil, nil, err
	}
	if !ok || user == nil {
		return nil, nil, fmt.Errorf("user not found")
	}
	if strings.TrimSpace(plan) == "" {
		plan = strings.TrimSpace(user.SubscriptionPlan)
	}
	if strings.TrimSpace(plan) == "" {
		plan = "starter"
	}

	normalizedStatus := strings.ToLower(strings.TrimSpace(status))
	paymentStatus := "pending"
	billingStatus := "pending"
	billingNotice := ""
	switch normalizedStatus {
	case "succeeded", "success", "successful", "completed", "complete", "paid":
		paymentStatus = "succeeded"
		billingStatus = "active"
		billingNotice = "Billing restored. Copilot and daemon workflows are available again."
	case "declined", "failed", "failure", "error", "expired", "requires_payment_method", "requires_action":
		paymentStatus = "failed"
		billingStatus = "on_hold"
		billingNotice = fmt.Sprintf("Payment failed (%s). Your account is on hold until a successful payment is recorded.", strings.TrimSpace(status))
	default:
		paymentStatus = "pending"
		billingStatus = "pending"
		billingNotice = "Payment is being processed."
	}

	user.SubscriptionPlan = strings.TrimSpace(plan)
	user.BillingStatus = billingStatus
	user.BillingNotice = billingNotice
	user.UpdatedAt = occurredAt
	if billingStatus == "active" {
		user.FailureReason = ""
	}
	if err := c.ensureUserStore().Save(ctx, user); err != nil {
		return nil, nil, err
	}
	for i := range c.Users {
		if normalizeUsername(c.Users[i].Username) == username {
			c.Users[i].SubscriptionPlan = user.SubscriptionPlan
			c.Users[i].BillingStatus = user.BillingStatus
			c.Users[i].BillingNotice = user.BillingNotice
			c.Users[i].FailureReason = user.FailureReason
			c.Users[i].UpdatedAt = user.UpdatedAt
			break
		}
	}

	paymentID := strings.TrimSpace(providerPaymentID)
	if paymentID == "" {
		paymentID = fmt.Sprintf("pay_%s_%d", username, occurredAt.UnixNano())
	}
	payment := PaymentRecord{
		ID:                paymentID,
		UserID:            user.Username,
		SubscriptionID:    fmt.Sprintf("sub_%s_%s", username, strings.ToLower(strings.TrimSpace(plan))),
		ProviderPaymentID: paymentID,
		Amount:            amount,
		Currency:          strings.TrimSpace(currency),
		Status:            paymentStatus,
		ProviderEventID:   strings.TrimSpace(eventID),
		CreatedAt:         occurredAt,
		UpdatedAt:         occurredAt,
	}
	if err := c.ensurePaymentStore().Save(ctx, &payment); err != nil {
		return nil, nil, err
	}
	c.Payments = append(c.Payments, payment)

	if store := c.ensureSubscriptionStore(); store != nil {
		subscriptionID := fmt.Sprintf("sub_%s_%s", username, strings.ToLower(strings.TrimSpace(plan)))
		subscription := SubscriptionRecord{
			ID:                 subscriptionID,
			UserID:             user.Username,
			PlanCode:           strings.TrimSpace(plan),
			Status:             map[bool]string{true: "active", false: "pending"}[billingStatus == "active"],
			CurrentPeriodStart: occurredAt,
			CurrentPeriodEnd:   occurredAt.AddDate(0, 1, 0),
			CreatedAt:          occurredAt,
			UpdatedAt:          occurredAt,
		}
		if billingStatus == "on_hold" {
			subscription.Status = "on_hold"
		}
		_ = store.Save(ctx, &subscription)
		c.Subscriptions = append(c.Subscriptions, subscription)
	}

	return &payment, user, nil
}

func (c *Config) SetBillingDay(ctx context.Context, username string, day int) (*UserRecord, error) {
	username = normalizeUsername(username)
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}
	if day < 1 || day > 28 {
		return nil, fmt.Errorf("billing day must be between 1 and 28")
	}

	user, ok, err := c.findUserRecord(ctx, username)
	if err != nil {
		return nil, err
	}
	if !ok || user == nil {
		return nil, fmt.Errorf("user not found")
	}

	user.BillingDayOfMonth = day
	user.UpdatedAt = time.Now().UTC()
	if err := c.ensureUserStore().Save(ctx, user); err != nil {
		return nil, err
	}
	for i := range c.Users {
		if normalizeUsername(c.Users[i].Username) == username {
			c.Users[i].BillingDayOfMonth = user.BillingDayOfMonth
			c.Users[i].UpdatedAt = user.UpdatedAt
			break
		}
	}
	return user, nil
}

func (c *Config) SetBillingCollectionMode(ctx context.Context, username, mode string) (*UserRecord, error) {
	username = normalizeUsername(username)
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "live"
	}
	if mode != "live" && mode != "fail" {
		return nil, fmt.Errorf("unsupported billing collection mode")
	}

	user, ok, err := c.findUserRecord(ctx, username)
	if err != nil {
		return nil, err
	}
	if !ok || user == nil {
		return nil, fmt.Errorf("user not found")
	}

	user.BillingCollectionMode = mode
	user.UpdatedAt = time.Now().UTC()
	if err := c.ensureUserStore().Save(ctx, user); err != nil {
		return nil, err
	}
	for i := range c.Users {
		if normalizeUsername(c.Users[i].Username) == username {
			c.Users[i].BillingCollectionMode = user.BillingCollectionMode
			c.Users[i].UpdatedAt = user.UpdatedAt
			break
		}
	}
	return user, nil
}

func (c *Config) CollectScheduledBilling(ctx context.Context, asOf time.Time, maxBatch int) ([]map[string]any, error) {
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	if maxBatch <= 0 {
		maxBatch = 10
	}

	if err := c.syncUsersStoreFromConfig(ctx); err != nil {
		return nil, err
	}
	if err := c.syncUsersFromStore(ctx); err != nil {
		return nil, err
	}

	results := make([]map[string]any, 0)
	eligible := make([]UserRecord, 0)
	for _, user := range c.Users {
		if user.BillingDayOfMonth <= 0 {
			continue
		}
		if !strings.EqualFold(user.SubscriptionPlan, "starter") && !strings.EqualFold(user.SubscriptionPlan, "pro") && !strings.EqualFold(user.SubscriptionPlan, "enterprise") {
			continue
		}
		if user.BillingStatus == "inactive" || user.BillingStatus == "pending" || strings.EqualFold(user.BillingStatus, "on_hold") {
			continue
		}
		if user.BillingDayOfMonth != asOf.Day() {
			continue
		}
		if !user.LastBillingCollectedAt.IsZero() && user.LastBillingCollectedAt.Year() == asOf.Year() && user.LastBillingCollectedAt.Month() == asOf.Month() {
			continue
		}
		eligible = append(eligible, user)
	}

	sort.SliceStable(eligible, func(i, j int) bool {
		return normalizeUsername(eligible[i].Username) < normalizeUsername(eligible[j].Username)
	})

	cursor := ""
	for i := range eligible {
		if strings.TrimSpace(eligible[i].BillingCollectionCursor) != "" {
			cursor = strings.TrimSpace(eligible[i].BillingCollectionCursor)
			break
		}
	}
	if cursor != "" {
		ordered := make([]UserRecord, 0, len(eligible))
		for _, user := range eligible {
			if normalizeUsername(user.Username) == cursor {
				ordered = append(ordered, user)
			}
		}
		for _, user := range eligible {
			if normalizeUsername(user.Username) != cursor {
				ordered = append(ordered, user)
			}
		}
		eligible = ordered
	}

	for _, user := range eligible {
		if len(results) >= maxBatch {
			break
		}

		if strings.TrimSpace(user.BillingCollectionCursor) != "" && normalizeUsername(user.Username) != cursor {
			continue
		}
		if strings.TrimSpace(user.BillingCollectionCursor) == "" && cursor != "" && normalizeUsername(user.Username) != cursor {
			continue
		}

		windowStart := time.Date(asOf.Year(), asOf.Month(), 1, 0, 0, 0, 0, time.UTC)
		windowEnd := windowStart.AddDate(0, 1, 0)
		payments, err := c.ensurePaymentStore().ListByUser(ctx, user.Username)
		if err == nil {
			for _, payment := range payments {
				if payment.CreatedAt.IsZero() {
					continue
				}
				if payment.CreatedAt.Before(windowStart) || !payment.CreatedAt.Before(windowEnd) {
					continue
				}
				if payment.Status == "succeeded" || payment.Status == "failed" {
					for i := range c.Users {
						if normalizeUsername(c.Users[i].Username) == user.Username {
							c.Users[i].LastBillingCollectedAt = asOf
							c.Users[i].NextBillingAt = asOf.AddDate(0, 1, 0)
							c.Users[i].BillingCollectionCursor = ""
							c.Users[i].UpdatedAt = asOf
							break
						}
					}
					continue
				}
			}
		}

		if strings.TrimSpace(user.BillingCollectionCursor) != "" && normalizeUsername(user.Username) == cursor {
			// resume the in-flight account from a previous interrupted run
		} else {
			user.BillingCollectionCursor = normalizeUsername(user.Username)
			if err := c.ensureUserStore().Save(ctx, &user); err != nil {
				continue
			}
			for i := range c.Users {
				if normalizeUsername(c.Users[i].Username) == user.Username {
					c.Users[i].BillingCollectionCursor = normalizeUsername(user.Username)
					c.Users[i].UpdatedAt = asOf
					break
				}
			}
		}

		status := "succeeded"
		if strings.EqualFold(user.BillingCollectionMode, "fail") {
			status = "failed"
		}

		payment, updatedUser, err := c.SubmitPayment(ctx, user.Username, user.SubscriptionPlan, 5.0, "USD", status, fmt.Sprintf("pay_sched_%s_%d", user.Username, asOf.Unix()), fmt.Sprintf("evt_sched_%s_%d", user.Username, asOf.Unix()), asOf)
		if err != nil {
			continue
		}
		if payment == nil || updatedUser == nil {
			continue
		}
		for i := range c.Users {
			if normalizeUsername(c.Users[i].Username) == user.Username {
				c.Users[i].LastBillingCollectedAt = asOf
				c.Users[i].NextBillingAt = asOf.AddDate(0, 1, 0)
				c.Users[i].BillingCollectionCursor = ""
				c.Users[i].UpdatedAt = asOf
				break
			}
		}
		results = append(results, map[string]any{
			"username":       user.Username,
			"status":         payment.Status,
			"billing_status": updatedUser.BillingStatus,
			"billing_notice": updatedUser.BillingNotice,
		})
	}
	return results, nil
}

func (c *Config) CreateCheckoutSession(ctx context.Context, username, plan string) (*BillingCheckoutSession, error) {
	username = normalizeUsername(username)
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}
	if strings.TrimSpace(plan) == "" {
		plan = "starter"
	}

	user, ok, err := c.findUserRecord(ctx, username)
	if err != nil {
		return nil, err
	}
	if !ok || user == nil {
		return nil, fmt.Errorf("user not found")
	}

	user.SubscriptionPlan = strings.TrimSpace(plan)
	if strings.EqualFold(strings.TrimSpace(plan), "none") {
		user.BillingStatus = "inactive"
		user.CheckoutURL = ""
	} else {
		user.BillingStatus = "pending"
	}
	user.UpdatedAt = time.Now().UTC()
	if err := c.ensureUserStore().Save(ctx, user); err != nil {
		return nil, err
	}
	for i := range c.Users {
		if normalizeUsername(c.Users[i].Username) == username {
			c.Users[i].SubscriptionPlan = user.SubscriptionPlan
			c.Users[i].BillingStatus = user.BillingStatus
			c.Users[i].UpdatedAt = user.UpdatedAt
			break
		}
	}

	subscriptionID := fmt.Sprintf("sub_%s_%s", username, strings.ToLower(strings.TrimSpace(plan)))
	subscription := SubscriptionRecord{
		ID:                 subscriptionID,
		UserID:             user.Username,
		PlanCode:           strings.TrimSpace(plan),
		Status:             "pending",
		CurrentPeriodStart: time.Now().UTC(),
		CurrentPeriodEnd:   time.Now().UTC().AddDate(0, 1, 0),
		CancelAtPeriodEnd:  false,
		CreatedAt:          time.Now().UTC(),
		UpdatedAt:          time.Now().UTC(),
	}
	if err := c.ensureSubscriptionStore().Save(ctx, &subscription); err != nil {
		return nil, err
	}
	c.Subscriptions = append(c.Subscriptions, subscription)
	payment := PaymentRecord{
		ID:             fmt.Sprintf("pay_%s", username),
		UserID:         user.Username,
		SubscriptionID: subscriptionID,
		Amount:         5,
		Currency:       "USD",
		Status:         "pending",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	if err := c.ensurePaymentStore().Save(ctx, &payment); err != nil {
		return nil, err
	}
	c.Payments = append(c.Payments, payment)

	user.CheckoutURL = fmt.Sprintf("https://example.test/checkout/%s", username)
	user.UpdatedAt = time.Now().UTC()
	if err := c.ensureUserStore().Save(ctx, user); err != nil {
		return nil, err
	}
	for i := range c.Users {
		if normalizeUsername(c.Users[i].Username) == username {
			c.Users[i].CheckoutURL = user.CheckoutURL
			c.Users[i].UpdatedAt = user.UpdatedAt
			break
		}
	}

	return &BillingCheckoutSession{
		ID:     fmt.Sprintf("cs_%s", username),
		URL:    user.CheckoutURL,
		Status: "pending",
	}, nil
}

func (c *Config) HandleBillingWebhook(ctx context.Context, event BillingWebhookEvent) error {
	if strings.TrimSpace(event.UserID) == "" {
		return fmt.Errorf("user id is required")
	}
	user, ok, err := c.findUserRecord(ctx, normalizeUsername(event.UserID))
	if err != nil {
		return err
	}
	if !ok || user == nil {
		return fmt.Errorf("user not found")
	}

	plan := strings.TrimSpace(event.Plan)
	if plan == "" {
		plan = "starter"
	}
	user.LastWebhookEvent = event.EventType
	user.LastWebhookAt = time.Now().UTC()
	user.FailureReason = ""
	if strings.EqualFold(event.EventType, "checkout.session.completed") {
		user.SubscriptionPlan = plan
		user.BillingStatus = "active"
		user.BillingNotice = ""
		user.UpdatedAt = time.Now().UTC()
		if err := c.ensureUserStore().Save(ctx, user); err != nil {
			return err
		}
		for i := range c.Users {
			if normalizeUsername(c.Users[i].Username) == normalizeUsername(event.UserID) {
				c.Users[i].SubscriptionPlan = user.SubscriptionPlan
				c.Users[i].BillingStatus = user.BillingStatus
				c.Users[i].LastWebhookEvent = user.LastWebhookEvent
				c.Users[i].LastWebhookAt = user.LastWebhookAt
				c.Users[i].FailureReason = user.FailureReason
				c.Users[i].UpdatedAt = user.UpdatedAt
				break
			}
		}
		for i := range c.Subscriptions {
			if c.Subscriptions[i].UserID == normalizeUsername(event.UserID) && c.Subscriptions[i].PlanCode == plan {
				c.Subscriptions[i].Status = "active"
				c.Subscriptions[i].UpdatedAt = time.Now().UTC()
				if store := c.ensureSubscriptionStore(); store != nil {
					if existing, ok, err := store.Get(ctx, c.Subscriptions[i].ID); err == nil && ok && existing != nil {
						existing.Status = "active"
						existing.UpdatedAt = c.Subscriptions[i].UpdatedAt
						_ = store.Save(ctx, existing)
					}
				}
				break
			}
		}
		for i := range c.Payments {
			if c.Payments[i].UserID == normalizeUsername(event.UserID) {
				c.Payments[i].Status = "succeeded"
				c.Payments[i].ProviderEventID = event.EventID
				c.Payments[i].UpdatedAt = time.Now().UTC()
				if store := c.ensurePaymentStore(); store != nil {
					if existing, ok, err := store.Get(ctx, c.Payments[i].ID); err == nil && ok && existing != nil {
						existing.Status = "succeeded"
						existing.ProviderEventID = event.EventID
						existing.UpdatedAt = c.Payments[i].UpdatedAt
						_ = store.Save(ctx, existing)
					}
				}
				break
			}
		}
		return nil
	}
	if strings.EqualFold(event.EventType, "customer.subscription.deleted") || strings.EqualFold(event.EventType, "checkout.session.expired") {
		user.SubscriptionPlan = "none"
		user.BillingStatus = "inactive"
		user.BillingNotice = ""
		user.FailureReason = "checkout session expired"
		if strings.EqualFold(event.EventType, "customer.subscription.deleted") {
			user.FailureReason = "subscription canceled"
		}
		user.UpdatedAt = time.Now().UTC()
		if err := c.ensureUserStore().Save(ctx, user); err != nil {
			return err
		}
		for i := range c.Users {
			if normalizeUsername(c.Users[i].Username) == normalizeUsername(event.UserID) {
				c.Users[i].SubscriptionPlan = user.SubscriptionPlan
				c.Users[i].BillingStatus = user.BillingStatus
				c.Users[i].LastWebhookEvent = user.LastWebhookEvent
				c.Users[i].LastWebhookAt = user.LastWebhookAt
				c.Users[i].FailureReason = user.FailureReason
				c.Users[i].UpdatedAt = user.UpdatedAt
				break
			}
		}
		for i := range c.Subscriptions {
			if c.Subscriptions[i].UserID == normalizeUsername(event.UserID) {
				c.Subscriptions[i].Status = "canceled"
				c.Subscriptions[i].UpdatedAt = time.Now().UTC()
				if store := c.ensureSubscriptionStore(); store != nil {
					if existing, ok, err := store.Get(ctx, c.Subscriptions[i].ID); err == nil && ok && existing != nil {
						existing.Status = "canceled"
						existing.UpdatedAt = c.Subscriptions[i].UpdatedAt
						_ = store.Save(ctx, existing)
					}
				}
				break
			}
		}
		for i := range c.Payments {
			if c.Payments[i].UserID == normalizeUsername(event.UserID) {
				c.Payments[i].Status = "expired"
				c.Payments[i].ProviderEventID = event.EventID
				c.Payments[i].UpdatedAt = time.Now().UTC()
				if store := c.ensurePaymentStore(); store != nil {
					if existing, ok, err := store.Get(ctx, c.Payments[i].ID); err == nil && ok && existing != nil {
						existing.Status = "expired"
						existing.ProviderEventID = event.EventID
						existing.UpdatedAt = c.Payments[i].UpdatedAt
						_ = store.Save(ctx, existing)
					}
				}
				break
			}
		}
	}
	return nil
}

func (c *Config) UpdateUserRole(ctx context.Context, username, role string) (*UserRecord, error) {
	username = normalizeUsername(username)
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}
	role = normalizeRole(role)
	if role == "" {
		role = sop.RoleUser
	}

	authCtx := sop.GetAuthFromContext(ctx)
	actorUsername := normalizeUsername(authCtx.UserID)
	actorRole := ""
	if len(authCtx.Roles) > 0 {
		actorRole = normalizeRole(authCtx.Roles[0])
	}
	if actorRole == "" && actorUsername != "" {
		if actor, ok, err := c.findUserRecord(ctx, actorUsername); err == nil && ok && actor != nil {
			actorRole = normalizeRole(actor.Role)
		}
	}
	if !canManageRoleChange(ctx, actorUsername, actorRole, role) {
		return nil, fmt.Errorf("permission denied")
	}

	user, ok, err := c.findUserRecord(ctx, username)
	if err != nil {
		return nil, err
	}
	if !ok || user == nil {
		return nil, fmt.Errorf("user not found")
	}

	user.Role = role
	user.UpdatedAt = time.Now().UTC()
	if err := c.ensureUserStore().Save(ctx, user); err != nil {
		return nil, err
	}
	for i := range c.Users {
		if normalizeUsername(c.Users[i].Username) == username {
			c.Users[i].Role = role
			c.Users[i].UpdatedAt = user.UpdatedAt
			break
		}
	}
	return user, nil
}

func isAdminRoleInContext(ctx context.Context) bool {
	auth := sop.GetAuthFromContext(ctx)
	for _, role := range auth.Roles {
		if sop.IsAdminRole(role) {
			return true
		}
	}
	return auth.IsSystem
}

func isRegularAdminRoleInContext(ctx context.Context) bool {
	auth := sop.GetAuthFromContext(ctx)
	for _, role := range auth.Roles {
		if strings.EqualFold(role, sop.RoleAdmin) {
			return true
		}
	}
	return false
}

func isSystemAdminRoleInContext(ctx context.Context) bool {
	auth := sop.GetAuthFromContext(ctx)
	for _, role := range auth.Roles {
		if strings.EqualFold(role, sop.RoleSystemAdmin) {
			return true
		}
	}
	return auth.IsSystem
}

func (c *Config) IsAdminOverride(ctx context.Context, username, password, legacyToken string) bool {
	if ok, user, err := c.Authenticate(username, password); err == nil && ok && user != nil && (sop.IsAdminRole(user.Role) || strings.EqualFold(user.Username, "root")) {
		return true
	}

	if legacyToken != "" {
		if ok, user, err := c.AuthenticateBearerToken(ctx, legacyToken); err == nil && ok && user != nil && (sop.IsAdminRole(user.Role) || strings.EqualFold(user.Username, "root")) {
			return true
		}
	}

	return false
}

func (c *Config) AuthenticateBearerToken(ctx context.Context, token string) (bool, *UserRecord, error) {
	if token == "" {
		log.Warn("AuthenticateBearerToken: empty token")
		return false, nil, nil
	}

	user, err := currentTokenFacade().ValidateToken(ctx, token)
	if err != nil {
		log.Warn("AuthenticateBearerToken: token rejected", "error", err)
		return false, nil, err
	}
	return true, user, nil
}

func (c *Config) Authenticate(username, password string) (bool, *UserRecord, error) {
	provider, err := c.resolveAuthProvider()
	if err != nil {
		return false, nil, err
	}
	return provider.Authenticate(context.Background(), username, password)
}

func authTokenFromRequest(r *http.Request) string {
	if token := extractBearerToken(r.Header.Get("Authorization")); token != "" {
		return token
	}
	if cookie, err := r.Cookie("sop_access_token"); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	return ""
}

func AuthTokenFromRequest(r *http.Request) string {
	return authTokenFromRequest(r)
}

func authenticatedAccountFromRequest(r *http.Request) AuthenticatedAccount {
	authCtx := sop.GetAuthFromContext(r.Context())
	username := normalizeUsername(authCtx.UserID)
	roles := make([]string, 0, len(authCtx.Roles))
	for _, role := range authCtx.Roles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		roles = append(roles, normalizeRole(role))
	}
	return AuthenticatedAccount{Username: username, Roles: roles}
}

func CanManageEnvironments(auth sop.AuthContext) bool {
	if auth.Permissions != nil {
		if allowed, ok := auth.Permissions["manage_environments"]; ok {
			return allowed
		}
	}
	if config.DisableAuth {
		return false
	}
	return true
}

func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return RequireAuth(next)
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	if HasConfigFileOnDisk() {
		handleLoginPage(w, r)
		return
	}
	http.Redirect(w, r, "/app", http.StatusFound)
}

func handleLoginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html>
  <head>
    <meta charset="utf-8">
    <title>SOP Login</title>
  </head>
  <body>
    <h1>SOP Login</h1>
    <p>Please sign in.</p>
  </body>
</html>`)
}

func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Debug("requireAuth: handler invocation", "handler", handlerName(next), "method", r.Method, "path", r.URL.Path)
		if !HasConfigFileOnDisk() {
			log.Debug("requireAuth: skipping session validation", "path", r.URL.Path, "method", r.Method, "reason", "config file not present on disk")
			next(w, r)
			return
		}

		token := authTokenFromRequest(r)
		log.Debug("requireAuth: checking auth token", "path", r.URL.Path, "has_token", token != "")
		if token == "" {
			log.Warn("requireAuth: unauthenticated redirect", "path", r.URL.Path, "reason", "missing session token", "method", r.Method)
			http.Redirect(w, r, "/login?return="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
			return
		}

		log.Debug("requireAuth: executing token validation", "path", r.URL.Path, "token_prefix", tokenPrefix(token), "cookie_present", r.Header.Get("Cookie") != "")
		ok, user, err := config.AuthenticateBearerToken(r.Context(), token)
		if err != nil || !ok {
			reason := "invalid or expired session token"
			if err != nil {
				reason = strings.ToLower(err.Error())
			}
			log.Warn("requireAuth: unauthenticated redirect", "path", r.URL.Path, "reason", reason, "error", err, "ok", ok, "method", r.Method, "token_prefix", tokenPrefix(token))
			http.Redirect(w, r, "/login?return="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
			return
		}
		log.Debug("requireAuth: token accepted", "path", r.URL.Path, "user", user.Username, "role", user.Role)

		// Preserve any existing auth policy from upstream middleware and merge
		// in the user identity for downstream handlers.
		authCtx := sop.GetAuthFromContext(r.Context())
		authCtx.UserID = user.Username
		authCtx.Roles = []string{user.Role}
		authCtx.SubscriptionPlan = user.SubscriptionPlan
		authCtx.SubscriptionFeatures = append([]string(nil), user.SubscriptionFeatures...)
		authCtx.IsSystem = strings.EqualFold(user.Role, sop.RoleSystemAdmin) || strings.EqualFold(user.Username, "root")
		if authCtx.Permissions == nil {
			authCtx.Permissions = map[string]bool{}
		}
		if _, ok := authCtx.Permissions["manage_environments"]; !ok {
			authCtx.Permissions["manage_environments"] = !config.DisableAuth
		}
		ctx := sop.ContextWithAuth(r.Context(), authCtx)
		next(w, r.WithContext(ctx))
	}
}

func parseRefreshRequest(r *http.Request) (string, error) {
	if cookie, err := r.Cookie("sop_refresh_token"); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return strings.TrimSpace(cookie.Value), nil
	}

	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return "", err
	}
	return strings.TrimSpace(req.RefreshToken), nil
}

func setSessionCookies(w http.ResponseWriter, accessToken, refreshToken string, secure bool) {
	accessTTL := sessionTTL()
	if accessTTL <= 0 {
		accessTTL = defaultSessionTTL
	}
	refreshTTL := defaultRefreshTTL

	http.SetCookie(w, &http.Cookie{
		Name:     "sop_access_token",
		Value:    accessToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(accessTTL.Seconds()),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "sop_refresh_token",
		Value:    refreshToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(refreshTTL.Seconds()),
	})
}

// handleRefresh issues new access tokens for the shared runtime auth session flow.
func handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	refreshToken, err := parseRefreshRequest(r)
	if err != nil || refreshToken == "" {
		http.Error(w, "invalid refresh token", http.StatusBadRequest)
		return
	}

	accessToken, newRefreshToken, err := currentTokenFacade().Refresh(r.Context(), refreshToken)
	if err != nil {
		http.Error(w, "invalid or expired refresh token", http.StatusUnauthorized)
		return
	}

	secure := r.TLS != nil
	setSessionCookies(w, accessToken, newRefreshToken, secure)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":        "ok",
		"access_token":  accessToken,
		"refresh_token": newRefreshToken,
		"token_type":    "Bearer",
	})
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": message, "message": message})
}

func parseLoginRequest(r *http.Request) (string, string, error) {
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err != nil {
			return "", "", err
		}
		return r.FormValue("username"), r.FormValue("password"), nil
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return "", "", err
	}
	return req.Username, req.Password, nil
}

func parseRegisterRequest(r *http.Request) (string, string, string, string, string, error) {
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err != nil {
			return "", "", "", "", "", err
		}
		return r.FormValue("username"), r.FormValue("password"), r.FormValue("email"), r.FormValue("role"), r.FormValue("subscription_plan"), nil
	}

	var req struct {
		Username         string `json:"username"`
		Password         string `json:"password"`
		Email            string `json:"email"`
		Role             string `json:"role"`
		SubscriptionPlan string `json:"subscription_plan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return "", "", "", "", "", err
	}
	return req.Username, req.Password, req.Email, req.Role, req.SubscriptionPlan, nil
}

func parseRoleUpdateRequest(r *http.Request) (string, string, error) {
	var req struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return "", "", err
	}
	return strings.TrimSpace(req.Username), strings.TrimSpace(req.Role), nil
}

// handleRegister is the shared runtime registration entrypoint for user creation.
func handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username, password, email, role, subscriptionPlan, err := parseRegisterRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := config.RegisterUserWithRequest(r.Context(), RegisterUserRequest{
		Username:         username,
		Password:         password,
		Email:            email,
		Role:             role,
		SubscriptionPlan: subscriptionPlan,
	})
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "already exists") {
			status = http.StatusConflict
		}
		recordProductMetric("auth_register_failures")
		writeJSONError(w, status, err.Error())
		return
	}

	recordProductMetric("auth_register_successes")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"user": map[string]any{
			"username":          user.Username,
			"role":              user.Role,
			"email":             user.Email.Plaintext(),
			"subscription_plan": user.SubscriptionPlan,
			"billing_status":    user.BillingStatus,
			"billing_notice":    user.BillingNotice,
		},
	})
}

// handleCurrentUser returns the current authenticated user from the shared runtime auth context.
func handleCurrentUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authAccount := authenticatedAccountFromRequest(r)
	if authAccount.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	user, ok, err := config.findUserRecord(r.Context(), authAccount.Username)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || user == nil {
		writeJSONError(w, http.StatusNotFound, "user not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"user": map[string]any{
			"username":          user.Username,
			"role":              user.Role,
			"email":             user.Email.Plaintext(),
			"subscription_plan": user.SubscriptionPlan,
			"billing_status":    user.BillingStatus,
			"billing_notice":    user.BillingNotice,
			"status":            user.Status,
			"created_at":        user.CreatedAt,
			"updated_at":        user.UpdatedAt,
		},
	})
}

// handleSubscriptionUpdate updates a user's subscription plan through the shared runtime auth state.
func handleBillingPayment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authAccount := authenticatedAccountFromRequest(r)
	if authAccount.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req struct {
		Plan              string  `json:"plan"`
		Amount            float64 `json:"amount"`
		Currency          string  `json:"currency"`
		Status            string  `json:"status"`
		ProviderPaymentID string  `json:"provider_payment_id"`
		EventID           string  `json:"event_id"`
		OccurredAt        string  `json:"occurred_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	occurredAt := time.Now().UTC()
	if strings.TrimSpace(req.OccurredAt) != "" {
		if parsed, err := time.Parse(time.RFC3339, req.OccurredAt); err == nil {
			occurredAt = parsed.UTC()
		}
	}

	payment, user, err := config.SubmitPayment(r.Context(), authAccount.Username, req.Plan, req.Amount, req.Currency, req.Status, req.ProviderPaymentID, req.EventID, occurredAt)
	if err != nil {
		recordProductMetric("billing_payment_failures")
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if payment == nil || user == nil {
		recordProductMetric("billing_payment_failures")
		writeJSONError(w, http.StatusInternalServerError, "payment was not recorded")
		return
	}

	recordProductMetric("billing_payment_successes")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"payment": map[string]any{
			"id":                  payment.ID,
			"provider_payment_id": payment.ProviderPaymentID,
			"status":              payment.Status,
			"amount":              payment.Amount,
			"currency":            payment.Currency,
		},
		"user": map[string]any{
			"username":          user.Username,
			"subscription_plan": user.SubscriptionPlan,
			"billing_status":    user.BillingStatus,
			"billing_notice":    user.BillingNotice,
			"updated_at":        user.UpdatedAt,
		},
	})
}

func handleBillingDaemonAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authAccount := authenticatedAccountFromRequest(r)
	if authAccount.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	user, ok, err := config.findUserRecord(r.Context(), authAccount.Username)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || user == nil {
		writeJSONError(w, http.StatusNotFound, "user not found")
		return
	}

	allowed := true
	reason := ""
	if strings.EqualFold(user.BillingStatus, "on_hold") {
		allowed = false
		reason = "account is on hold due to a failed or declined payment"
		if strings.TrimSpace(user.BillingNotice) != "" {
			reason = user.BillingNotice
		}
	} else if strings.EqualFold(user.BillingStatus, "pending") {
		reason = "billing is still processing"
	} else if strings.EqualFold(user.BillingStatus, "inactive") {
		allowed = false
		reason = "billing is not active"
	}

	if allowed && strings.TrimSpace(user.BillingNotice) != "" && strings.Contains(strings.ToLower(user.BillingNotice), "restored") {
		user.BillingNotice = "Billing restored. Copilot and daemon workflows are available again."
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"allowed": allowed,
		"reason":  reason,
		"user": map[string]any{
			"username":                user.Username,
			"billing_status":          user.BillingStatus,
			"billing_notice":          user.BillingNotice,
			"subscription_plan":       user.SubscriptionPlan,
			"billing_day_of_month":    user.BillingDayOfMonth,
			"billing_collection_mode": user.BillingCollectionMode,
		},
	})
}

func handleBillingStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authAccount := authenticatedAccountFromRequest(r)
	if authAccount.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	user, ok, err := config.findUserRecord(r.Context(), authAccount.Username)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || user == nil {
		writeJSONError(w, http.StatusNotFound, "user not found")
		return
	}

	payments, err := config.ensurePaymentStore().ListByUser(r.Context(), authAccount.Username)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"user": map[string]any{
			"username":          user.Username,
			"subscription_plan": user.SubscriptionPlan,
			"billing_status":    user.BillingStatus,
			"billing_notice":    user.BillingNotice,
			"failure_reason":    user.FailureReason,
			"updated_at":        user.UpdatedAt,
		},
		"payments": payments,
	})
}

func handleBillingDayUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authAccount := authenticatedAccountFromRequest(r)
	if authAccount.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req struct {
		BillingDayOfMonth int `json:"billing_day_of_month"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := config.SetBillingDay(r.Context(), authAccount.Username, req.BillingDayOfMonth)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"user": map[string]any{
			"username":             updated.Username,
			"billing_day_of_month": updated.BillingDayOfMonth,
		},
	})
}

func handleBillingCollectionModeUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authAccount := authenticatedAccountFromRequest(r)
	if authAccount.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := config.SetBillingCollectionMode(r.Context(), authAccount.Username, req.Mode)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"user": map[string]any{
			"username":                updated.Username,
			"billing_collection_mode": updated.BillingCollectionMode,
		},
	})
}

func handleSubscriptionUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authAccount := authenticatedAccountFromRequest(r)
	if authAccount.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req struct {
		Plan string `json:"plan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := config.UpdateSubscriptionPlanWithRequest(r.Context(), UpdateSubscriptionPlanRequest{
		Username:         authAccount.Username,
		SubscriptionPlan: req.Plan,
	})
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		}
		recordProductMetric("auth_subscription_failures")
		writeJSONError(w, status, err.Error())
		return
	}

	recordProductMetric("auth_subscription_successes")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"user": map[string]any{
			"username":          updated.Username,
			"subscription_plan": updated.SubscriptionPlan,
			"billing_status":    updated.BillingStatus,
			"billing_notice":    updated.BillingNotice,
			"updated_at":        updated.UpdatedAt,
		},
	})
}

// handleUnregister removes an authenticated user from the shared runtime auth state.
func handleUnregister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authAccount := authenticatedAccountFromRequest(r)
	if authAccount.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	if err := config.UnregisterUserWithRequest(r.Context(), UnregisterUserRequest{Username: authAccount.Username}); err != nil {
		recordProductMetric("auth_unregister_failures")
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	recordProductMetric("auth_unregister_successes")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "message": "account removed"})
}

// handleUpdateUserRole adjusts a user's role in the shared runtime auth store.
func handleUpdateUserRole(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	targetUsername, role, err := parseRoleUpdateRequest(r)
	if err != nil || targetUsername == "" || role == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	authAccount := authenticatedAccountFromRequest(r)
	actorUsername := authAccount.Username
	actorRole := ""
	if len(authAccount.Roles) > 0 {
		actorRole = authAccount.Roles[0]
	}
	if actorRole == "" && actorUsername != "" {
		if actor, ok, actorErr := config.findUserRecord(r.Context(), actorUsername); actorErr == nil && ok && actor != nil {
			actorRole = normalizeRole(actor.Role)
		}
	}
	if !canManageRoleChange(r.Context(), actorUsername, actorRole, role) {
		writeJSONError(w, http.StatusForbidden, "permission denied")
		return
	}

	updatedUser, err := config.UpdateUserRole(r.Context(), targetUsername, role)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		} else if strings.Contains(strings.ToLower(err.Error()), "permission denied") {
			status = http.StatusForbidden
		}
		writeJSONError(w, status, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"user": map[string]any{
			"username": updatedUser.Username,
			"role":     updatedUser.Role,
		},
	})
}

// handleLogin authenticates a user against the shared runtime auth store.
func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username, password, err := parseLoginRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ok, user, err := config.Authenticate(username, password)
	if err != nil {
		recordProductMetric("auth_login_failures")
		writeJSONError(w, http.StatusInternalServerError, "authentication failed")
		return
	}
	if !ok || user == nil {
		recordProductMetric("auth_login_failures")
		writeJSONError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	recordProductMetric("auth_login_successes")

	if existingToken := authTokenFromRequest(r); existingToken != "" {
		validatedUser, validateErr := currentTokenFacade().ValidateToken(r.Context(), existingToken)
		if validateErr == nil && validatedUser != nil && strings.EqualFold(validatedUser.Username, user.Username) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":        "ok",
				"access_token":  existingToken,
				"refresh_token": "",
				"token_type":    "Bearer",
				"user": map[string]any{
					"username":              validatedUser.Username,
					"role":                  validatedUser.Role,
					"subscription_features": user.SubscriptionFeatures,
				},
			})
			return
		}
	}

	accessToken, refreshToken, err := currentTokenFacade().CreateSession(r.Context(), user.Username, user.Role)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	secure := r.TLS != nil
	setSessionCookies(w, accessToken, refreshToken, secure)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":        "ok",
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"user": map[string]any{
			"username":              user.Username,
			"role":                  user.Role,
			"subscription_features": user.SubscriptionFeatures,
			"billing_status":        user.BillingStatus,
			"billing_notice":        user.BillingNotice,
		},
	})
}

func removeSessionRecord(ctx context.Context, store btree.BtreeInterface[string, SessionRecord], record SessionRecord) error {
	if removed, err := store.Remove(ctx, record.Token); err != nil {
		return fmt.Errorf("failed to remove access token: %w", err)
	} else if !removed {
		log.Debug("removeSessionRecord: access token already absent", "token_prefix", tokenPrefix(record.Token))
	}

	if record.RefreshToken != "" {
		if removed, err := store.Remove(ctx, record.RefreshToken); err != nil {
			return fmt.Errorf("failed to remove refresh token: %w", err)
		} else if !removed {
			log.Debug("removeSessionRecord: refresh token already absent", "token_prefix", tokenPrefix(record.RefreshToken))
		}
	}

	return nil
}

func (s *SessionStore) getStore(ctx context.Context) (btree.BtreeInterface[string, SessionRecord], sop.Transaction, error) {
	db, err := getSystemDB(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("session store failed to get system db: %v", err)
	}
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		return nil, nil, fmt.Errorf("session store failed to begin transaction: %v", err)
	}

	exists, err := db.StoreExists(ctx, s.storeName)
	if err != nil {
		tx.Rollback(ctx)
		return nil, nil, fmt.Errorf("session store failed to inspect store existence: %v", err)
	}

	if exists {
		store, err := database.OpenBtree[string, SessionRecord](ctx, db.Options(), s.storeName, tx, nil)
		if err != nil {
			tx.Rollback(ctx)
			return nil, nil, fmt.Errorf("session store failed to open existing store: %v", err)
		}
		return store, tx, nil
	}

	options := sop.ConfigureStore(s.storeName, true, 1000, "System sessions", sop.SmallData, "")
	options.IsPrimitiveKey = true
	store, err := database.NewBtree[string, SessionRecord](ctx, db.Options(), s.storeName, tx, nil, options)
	if err != nil {
		tx.Rollback(ctx)
		return nil, nil, fmt.Errorf("session store failed to create store: %v", err)
	}

	return store, tx, nil
}

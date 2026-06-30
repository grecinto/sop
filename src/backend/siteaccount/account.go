package siteaccount

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sharedcode/sop/tools/sopbackend"
)

var (
	configMu     sync.RWMutex
	globalConfig *Config
)

// Config stores the site-specific account state for the React/site app.
type Config struct {
	Users              []UserRecord
	Subscriptions      []SubscriptionRecord
	Payments           []PaymentRecord
	UsersStore         UserStore
	SubscriptionsStore SubscriptionStore
	PaymentsStore      PaymentStore
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
	Save(ctx context.Context, user *UserRecord) error
	Get(ctx context.Context, username string) (*UserRecord, bool, error)
	Delete(ctx context.Context, username string) error
	List(ctx context.Context) ([]UserRecord, error)
}

// SubscriptionStore persists subscription records.
type SubscriptionStore interface {
	Save(ctx context.Context, subscription *SubscriptionRecord) error
	Get(ctx context.Context, id string) (*SubscriptionRecord, bool, error)
	Delete(ctx context.Context, id string) error
	ListByUser(ctx context.Context, username string) ([]SubscriptionRecord, error)
	List(ctx context.Context) ([]SubscriptionRecord, error)
}

// PaymentStore persists payment records.
type PaymentStore interface {
	Save(ctx context.Context, payment *PaymentRecord) error
	Get(ctx context.Context, id string) (*PaymentRecord, bool, error)
	Delete(ctx context.Context, id string) error
	ListByUser(ctx context.Context, username string) ([]PaymentRecord, error)
	List(ctx context.Context) ([]PaymentRecord, error)
}

type userStoreImpl struct {
	mu    sync.RWMutex
	users map[string]UserRecord
}

func newUserStore() *userStoreImpl {
	return &userStoreImpl{users: make(map[string]UserRecord)}
}

func (s *userStoreImpl) Save(ctx context.Context, user *UserRecord) error {
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

func (s *userStoreImpl) Get(ctx context.Context, username string) (*UserRecord, bool, error) {
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

func (s *userStoreImpl) Delete(ctx context.Context, username string) error {
	username = normalizeUsername(username)
	if username == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.users, username)
	return nil
}

func (s *userStoreImpl) List(ctx context.Context) ([]UserRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	users := make([]UserRecord, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, user)
	}
	return users, nil
}

type subscriptionStoreImpl struct {
	mu            sync.RWMutex
	subscriptions map[string]SubscriptionRecord
}

func newSubscriptionStore() *subscriptionStoreImpl {
	return &subscriptionStoreImpl{subscriptions: make(map[string]SubscriptionRecord)}
}

func (s *subscriptionStoreImpl) Save(ctx context.Context, subscription *SubscriptionRecord) error {
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

func (s *subscriptionStoreImpl) Get(ctx context.Context, id string) (*SubscriptionRecord, bool, error) {
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

func (s *subscriptionStoreImpl) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subscriptions, id)
	return nil
}

func (s *subscriptionStoreImpl) ListByUser(ctx context.Context, username string) ([]SubscriptionRecord, error) {
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

func (s *subscriptionStoreImpl) List(ctx context.Context) ([]SubscriptionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	results := make([]SubscriptionRecord, 0, len(s.subscriptions))
	for _, subscription := range s.subscriptions {
		results = append(results, subscription)
	}
	return results, nil
}

type paymentStoreImpl struct {
	mu       sync.RWMutex
	payments map[string]PaymentRecord
}

func newPaymentStore() *paymentStoreImpl {
	return &paymentStoreImpl{payments: make(map[string]PaymentRecord)}
}

func (s *paymentStoreImpl) Save(ctx context.Context, payment *PaymentRecord) error {
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

func (s *paymentStoreImpl) Get(ctx context.Context, id string) (*PaymentRecord, bool, error) {
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

func (s *paymentStoreImpl) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.payments, id)
	return nil
}

func (s *paymentStoreImpl) ListByUser(ctx context.Context, username string) ([]PaymentRecord, error) {
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

func (s *paymentStoreImpl) List(ctx context.Context) ([]PaymentRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	results := make([]PaymentRecord, 0, len(s.payments))
	for _, payment := range s.payments {
		results = append(results, payment)
	}
	return results, nil
}

// NewConfig creates a new site-specific account config.
func NewConfig() *Config {
	return &Config{
		UsersStore:         newUserStore(),
		SubscriptionsStore: newSubscriptionStore(),
		PaymentsStore:      newPaymentStore(),
	}
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

func (c *Config) ensureUserStore() UserStore {
	if c == nil {
		return nil
	}
	if c.UsersStore != nil {
		return c.UsersStore
	}
	c.UsersStore = newUserStore()
	return c.UsersStore
}

func (c *Config) ensureSubscriptionStore() SubscriptionStore {
	if c == nil {
		return nil
	}
	if c.SubscriptionsStore != nil {
		return c.SubscriptionsStore
	}
	c.SubscriptionsStore = newSubscriptionStore()
	return c.SubscriptionsStore
}

func (c *Config) ensurePaymentStore() PaymentStore {
	if c == nil {
		return nil
	}
	if c.PaymentsStore != nil {
		return c.PaymentsStore
	}
	c.PaymentsStore = newPaymentStore()
	return c.PaymentsStore
}

// FindUserRecord returns a site user by username.
func (c *Config) FindUserRecord(ctx context.Context, username string) (*UserRecord, bool, error) {
	if c == nil {
		return nil, false, nil
	}
	if store := c.ensureUserStore(); store != nil {
		user, ok, err := store.Get(ctx, username)
		if err != nil {
			return nil, false, err
		}
		if ok && user != nil {
			return user, true, nil
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
	if _, ok, err := c.FindUserRecord(ctx, username); err != nil {
		return nil, err
	} else if ok {
		return nil, fmt.Errorf("user already exists")
	}

	user := &UserRecord{
		Username:             username,
		Role:                 req.Role,
		Email:                req.Email,
		SubscriptionPlan:     req.SubscriptionPlan,
		SubscriptionFeatures: req.SubscriptionFeatures,
		BillingStatus:        "pending",
		Status:               "active",
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
	}
	if err := c.ensureUserStore().Save(ctx, user); err != nil {
		return nil, err
	}
	c.Users = append(c.Users, *user)

	if sharedCfg := sopbackend.GetConfig(); true {
		_, err := sharedCfg.RegisterUserWithRequest(ctx, sopbackend.RegisterUserRequest{
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
	}
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
	if store := c.ensureUserStore(); store != nil {
		_ = store.Delete(ctx, username)
	}
	filteredUsers := c.Users[:0]
	for _, existing := range c.Users {
		if normalizeUsername(existing.Username) == username {
			continue
		}
		filteredUsers = append(filteredUsers, existing)
	}
	c.Users = filteredUsers

	if sharedCfg := sopbackend.GetConfig(); true {
		_ = sharedCfg.UnregisterUserWithRequest(ctx, sopbackend.UnregisterUserRequest{Username: username})
	}
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
	user.SubscriptionPlan = req.SubscriptionPlan
	user.SubscriptionFeatures = req.SubscriptionFeatures
	user.UpdatedAt = time.Now().UTC()
	if err := c.ensureUserStore().Save(ctx, user); err != nil {
		return nil, err
	}
	for i := range c.Users {
		if normalizeUsername(c.Users[i].Username) == username {
			c.Users[i] = *user
			break
		}
	}
	return user, nil
}

// CreateCheckoutSession creates a pending billing checkout session for a site account.
func (c *Config) CreateCheckoutSession(ctx context.Context, username, plan string) (*BillingCheckoutSession, error) {
	if c == nil {
		return nil, fmt.Errorf("site account config is not initialized")
	}
	user, ok, err := c.FindUserRecord(ctx, username)
	if err != nil {
		return nil, err
	}
	if !ok || user == nil {
		return nil, fmt.Errorf("user not found")
	}
	user.SubscriptionPlan = plan
	user.BillingStatus = "pending"
	user.CheckoutURL = fmt.Sprintf("https://checkout.example.test/%s", normalizeUsername(username))
	user.UpdatedAt = time.Now().UTC()
	if err := c.ensureUserStore().Save(ctx, user); err != nil {
		return nil, err
	}
	for i := range c.Users {
		if normalizeUsername(c.Users[i].Username) == normalizeUsername(username) {
			c.Users[i] = *user
			break
		}
	}
	session := &BillingCheckoutSession{ID: fmt.Sprintf("checkout-%d", time.Now().UnixNano()), URL: user.CheckoutURL, Status: "pending"}
	return session, nil
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
	if err := c.ensureUserStore().Save(ctx, user); err != nil {
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

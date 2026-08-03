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
	Username     string        `json:"username"`
	Role         string        `json:"role"`
	PasswordHash SecuredString `json:"password_hash,omitempty"`
	Email        SecuredString `json:"email,omitempty"`
	CreatedAt    time.Time     `json:"created_at,omitempty"`
	UpdatedAt    time.Time     `json:"updated_at,omitempty"`
}

type RegisterUserRequest struct {
	Username string
	FullName string
	Password string
	Email    string
	Role     string
}

type UnregisterUserRequest struct {
	Username string
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

type DatabaseUserStore struct {
	storeName string
	opts      sop.DatabaseOptions
}

func NewSystemUserStore() *SystemUserStore {
	return &SystemUserStore{storeName: "users"}
}

func NewDatabaseUserStore(opts sop.DatabaseOptions) *DatabaseUserStore {
	return &DatabaseUserStore{storeName: "users", opts: opts}
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
	copyUser.Role = normalizeRole(copyUser.Role)
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

	// Check if store exists first
	exists, err := db.StoreExists(ctx, s.storeName)
	if err != nil {
		return nil, false, fmt.Errorf("system user store failed to check store existence: %w", err)
	}
	if !exists {
		// Store doesn't exist yet, user not found
		return nil, false, nil
	}

	tx, err := db.BeginTransaction(ctx, sop.ForReading, -1)
	if err != nil {
		return nil, false, fmt.Errorf("system user store failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Use OpenBtree for read-only access to existing store
	store, err := database.OpenBtree[string, UserRecord](ctx, db.Options(), s.storeName, tx, nil)
	if err != nil {
		return nil, false, fmt.Errorf("system user store failed to open store: %w", err)
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
	copyUser.Role = normalizeRole(copyUser.Role)
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

func (s *DatabaseUserStore) Save(ctx context.Context, user *UserRecord) error {
	if user == nil {
		return fmt.Errorf("user is nil")
	}
	username := normalizeUsername(user.Username)
	if username == "" {
		return fmt.Errorf("username is required")
	}

	db := aidb.NewDatabase(s.opts)
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		return fmt.Errorf("database user store failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	options := sop.ConfigureStore(s.storeName, true, 1000, "Database users", sop.SmallData, "")
	options.IsPrimitiveKey = true
	store, err := database.NewBtree[string, UserRecord](ctx, db.Options(), s.storeName, tx, nil, options)
	if err != nil {
		return fmt.Errorf("database user store failed to open/create store: %w", err)
	}
	copyUser := *user
	copyUser.Username = username
	copyUser.Role = normalizeRole(copyUser.Role)
	copyUser.UpdatedAt = time.Now().UTC()
	if copyUser.CreatedAt.IsZero() {
		copyUser.CreatedAt = copyUser.UpdatedAt
	}
	if _, err := store.Upsert(ctx, username, copyUser); err != nil {
		return fmt.Errorf("database user store failed to upsert user: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("database user store failed to commit: %w", err)
	}
	return nil
}

func (s *DatabaseUserStore) Get(ctx context.Context, username string) (*UserRecord, bool, error) {
	username = normalizeUsername(username)
	if username == "" {
		return nil, false, nil
	}

	db := aidb.NewDatabase(s.opts)
	exists, err := db.StoreExists(ctx, s.storeName)
	if err != nil {
		return nil, false, fmt.Errorf("database user store failed to check store existence: %w", err)
	}
	if !exists {
		return nil, false, nil
	}

	tx, err := db.BeginTransaction(ctx, sop.ForReading, -1)
	if err != nil {
		return nil, false, fmt.Errorf("database user store failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	store, err := database.OpenBtree[string, UserRecord](ctx, db.Options(), s.storeName, tx, nil)
	if err != nil {
		return nil, false, fmt.Errorf("database user store failed to open store: %w", err)
	}
	if found, err := store.Find(ctx, username, true); err != nil {
		return nil, false, fmt.Errorf("database user store failed to find user: %w", err)
	} else if !found {
		return nil, false, nil
	}
	value, err := store.GetCurrentValue(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("database user store failed to read user: %w", err)
	}
	copyUser := value
	copyUser.Role = normalizeRole(copyUser.Role)
	return &copyUser, true, nil
}

func (s *DatabaseUserStore) Delete(ctx context.Context, username string) error {
	username = normalizeUsername(username)
	if username == "" {
		return nil
	}

	db := aidb.NewDatabase(s.opts)
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		return fmt.Errorf("database user store failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	options := sop.ConfigureStore(s.storeName, true, 1000, "Database users", sop.SmallData, "")
	options.IsPrimitiveKey = true
	store, err := database.NewBtree[string, UserRecord](ctx, db.Options(), s.storeName, tx, nil, options)
	if err != nil {
		return fmt.Errorf("database user store failed to open/create store: %w", err)
	}
	if _, err := store.Remove(ctx, username); err != nil {
		return fmt.Errorf("database user store failed to remove user: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("database user store failed to commit: %w", err)
	}
	return nil
}

func (s *DatabaseUserStore) List(ctx context.Context) ([]UserRecord, error) {
	db := aidb.NewDatabase(s.opts)
	tx, err := db.BeginTransaction(ctx, sop.ForReading, -1)
	if err != nil {
		return nil, fmt.Errorf("database user store failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	options := sop.ConfigureStore(s.storeName, true, 1000, "Database users", sop.SmallData, "")
	options.IsPrimitiveKey = true
	store, err := database.NewBtree[string, UserRecord](ctx, db.Options(), s.storeName, tx, nil, options)
	if err != nil {
		return nil, fmt.Errorf("database user store failed to open/create store: %w", err)
	}
	ok, err := store.First(ctx)
	if err != nil {
		return nil, fmt.Errorf("database user store failed to seek first user: %w", err)
	}
	if !ok {
		return nil, nil
	}
	results := []UserRecord{}
	for {
		value, err := store.GetCurrentValue(ctx)
		if err != nil {
			return nil, fmt.Errorf("database user store failed to read user: %w", err)
		}
		results = append(results, value)
		if ok, err := store.Next(ctx); err != nil {
			return nil, fmt.Errorf("database user store failed to iterate users: %w", err)
		} else if !ok {
			break
		}
	}
	return results, nil
}

func (s *DatabaseUserStore) EnsureProvisioned(ctx context.Context) error {
	db := aidb.NewDatabase(s.opts)
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		return fmt.Errorf("database user store failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	options := sop.ConfigureStore(s.storeName, true, 1000, "Database users", sop.SmallData, "")
	options.IsPrimitiveKey = true
	if _, err := database.NewBtree[string, UserRecord](ctx, db.Options(), s.storeName, tx, nil, options); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already exists") || strings.Contains(strings.ToLower(err.Error()), "exists") {
			if err := tx.Commit(ctx); err != nil {
				return fmt.Errorf("database user store failed to commit: %w", err)
			}
			return nil
		}
		return fmt.Errorf("database user store failed to open/create store: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("database user store failed to commit: %w", err)
	}
	return nil
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
	copyUser.Role = normalizeRole(copyUser.Role)
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
	copyUser.Role = normalizeRole(copyUser.Role)
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
	defaultSessionTTL = time.Hour
	defaultRefreshTTL = 14 * 24 * time.Hour
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

	store, tx, err := s.getStoreWithMode(ctx, sop.ForWriting)
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
	role = normalizeRole(role)
	store, tx, err := s.getStoreWithMode(ctx, sop.ForWriting)
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
	// Retry logic for transaction conflicts (e.g., concurrent login attempts)
	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 10ms, 20ms, 40ms
			time.Sleep(time.Duration(10<<uint(attempt-1)) * time.Millisecond)
			log.Debug("CreateSession: retrying after conflict", "attempt", attempt+1, "username", username)
		}

		accessToken, refreshToken, err := s.createSessionAttempt(ctx, username, role)
		if err == nil {
			return accessToken, refreshToken, nil
		}

		// Check if it's a retryable transaction conflict
		if strings.Contains(err.Error(), "conflict") || strings.Contains(err.Error(), "lock") {
			lastErr = err
			continue
		}

		// Non-retryable error
		return "", "", err
	}

	return "", "", fmt.Errorf("failed to create session after %d retries: %v", maxRetries, lastErr)
}

func (s *SessionStore) createSessionAttempt(ctx context.Context, username, role string) (string, string, error) {
	role = normalizeRole(role)
	store, tx, err := s.getStoreWithMode(ctx, sop.ForWriting)
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
	store, tx, err := s.getStoreWithMode(ctx, sop.ForWriting)
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
	normalizedRole := normalizeRole(r.Role)
	accessToken, err := signAccessToken(r.Username, normalizedRole, now, r.ExpiresAt, opaqueAccessToken)
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
		Role:             normalizedRole,
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
	return true
}

func (s *SessionStore) ValidateToken(ctx context.Context, token string) (*UserRecord, error) {
	return s.validateToken(ctx, token, true)
}

func (s *SessionStore) ValidateTokenWithoutAccountCheck(ctx context.Context, token string) (*UserRecord, error) {
	return s.validateToken(ctx, token, false)
}

func (s *SessionStore) validateToken(ctx context.Context, token string, requireActiveAccount bool) (*UserRecord, error) {
	if isTokenRevoked(token) {
		return nil, fmt.Errorf("invalid session")
	}

	if claims, err := parseAndVerifySignedAccessToken(token); err == nil {
		claims.Role = normalizeRole(claims.Role)
		if requireActiveAccount && !isUserAccountActive(ctx, claims.Subject, claims.Role) {
			return nil, fmt.Errorf("invalid session")
		}
		if user, ok, lookupErr := config.findUserRecord(ctx, claims.Subject); lookupErr == nil && ok && user != nil {
			if strings.TrimSpace(user.Username) == "" {
				user.Username = claims.Subject
			}
			if strings.TrimSpace(user.Role) == "" {
				user.Role = claims.Role
			} else {
				user.Role = normalizeRole(user.Role)
			}
			return user, nil
		}
		return &UserRecord{Username: claims.Subject, Role: normalizeRole(claims.Role)}, nil
	}

	store, tx, err := s.getStoreWithMode(ctx, sop.NoCheck)
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
		// Don't cleanup here - ValidateToken should be read-only
		// Expired sessions will be cleaned up by refresh flow or background job
		return nil, fmt.Errorf("expired session")
	}
	r.Role = normalizeRole(r.Role)
	if requireActiveAccount && !isUserAccountActive(ctx, r.Username, r.Role) {
		// Don't cleanup here - ValidateToken should be read-only
		// Invalid sessions will be cleaned up by refresh flow or background job
		return nil, fmt.Errorf("invalid session")
	}

	if user, ok, lookupErr := config.findUserRecord(ctx, r.Username); lookupErr == nil && ok && user != nil {
		if strings.TrimSpace(user.Username) == "" {
			user.Username = r.Username
		}
		if strings.TrimSpace(user.Role) == "" {
			user.Role = r.Role
		} else {
			user.Role = normalizeRole(user.Role)
		}
		return user, nil
	}

	if err := tx.Commit(ctx); err != nil {
		log.Warn("failed to commit token validation", "error", err)
	}

	log.Debug("ValidateToken: successful validation",
		"token_prefix", tokenPrefix(token),
		"expires_at", r.ExpiresAt)

	return &UserRecord{Username: r.Username, Role: normalizeRole(r.Role)}, nil
}

func (s *SessionStore) RevokeToken(ctx context.Context, token string) {
	markTokenRevoked(token)

	store, tx, err := s.getStoreWithMode(ctx, sop.ForWriting)
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

	markSessionTokensRevoked(r)

	// Remove both the access and refresh tokens to fully invalidate the session.
	store.Remove(ctx, r.Token)
	store.Remove(ctx, r.RefreshToken)
	if err := tx.Commit(ctx); err != nil {
		log.Warn("failed to commit token revocation", "error", err)
	}
}

func (s *SessionStore) revokeSessionsForUser(ctx context.Context, username string) {
	store, tx, err := s.getStoreWithMode(ctx, sop.ForWriting)
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
		markSessionTokensRevoked(record)
		if err := removeSessionRecord(ctx, store, record); err != nil {
			log.Warn("failed to revoke user session", "user", username, "error", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		log.Warn("failed to commit user session revocation", "error", err)
	}
}

var (
	tokenFacade       TokenFacade
	tokenFacadeOnce   sync.Once
	revokedTokensMu   sync.RWMutex
	revokedSessionIDs = make(map[string]struct{})
)

func isTokenRevoked(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	revokedTokensMu.RLock()
	defer revokedTokensMu.RUnlock()
	_, ok := revokedSessionIDs[token]
	return ok
}

func markTokenRevoked(token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	revokedTokensMu.Lock()
	defer revokedTokensMu.Unlock()
	revokedSessionIDs[token] = struct{}{}
}

func markSessionTokensRevoked(record SessionRecord) {
	markTokenRevoked(record.Token)
	markTokenRevoked(record.RefreshToken)
}

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

// RevokeToken invalidates a stored session token so it can no longer be used to authenticate requests.
func RevokeToken(ctx context.Context, token string) {
	if token == "" {
		return
	}
	currentTokenFacade().RevokeToken(ctx, token)
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
	normalized := sop.NormalizeRole(strings.TrimSpace(role))
	switch {
	case normalized == "":
		return sop.RoleUser
	case strings.EqualFold(normalized, sop.RoleAdmin), strings.EqualFold(normalized, sop.RoleSystemAdmin), strings.EqualFold(normalized, sop.RoleUser), strings.EqualFold(normalized, sop.RoleGuest):
		return normalized
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

	if _, ok, err := c.findUserRecord(context.Background(), username); err != nil {
		return err
	} else if ok {
		return fmt.Errorf("username already exists")
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
		Username:     username,
		Role:         normalizeRole(role),
		PasswordHash: SecuredString(hash),
	}
	if err := c.persistUserRecord(context.Background(), &record); err != nil {
		return err
	}
	return nil
}

func (c *Config) ensureUserStore() UserStore {
	if c == nil {
		return nil
	}
	if userStoreOverride != nil {
		if c.UsersStore == nil || c.UsersStore != userStoreOverride {
			c.UsersStore = userStoreOverride
		}
		return c.UsersStore
	}
	if c.UsersStore != nil {
		return c.UsersStore
	}
	return c.ensurePersistentUserStore()
}

func (c *Config) ensurePersistentUserStore() UserStore {
	if c == nil {
		return nil
	}

	// Use cached persistent store if available
	if c.persistentUsersStore != nil {
		return c.persistentUsersStore
	}

	if c.UsersStore != nil && c.UsersStore != userStoreOverride {
		c.persistentUsersStore = c.UsersStore
		return c.persistentUsersStore
	}

	var targetStore UserStore

	// Try to use first non-system database
	if len(c.Databases) > 0 {
		for i := range c.Databases {
			if c.Databases[i].IsSystem {
				continue
			}
			opts, err := GetDBOptionsFromConfig(context.Background(), &c.Databases[i])
			if err != nil {
				// Database is configured but we can't get options - this is a FATAL error
				log.Error(fmt.Sprintf("CRITICAL: Failed to get DB options for '%s' at '%s': %v",
					c.Databases[i].Name, c.Databases[i].Path, err))
				log.Error("Cannot create persistent users store - setup is broken")
				return nil
			}
			targetStore = NewDatabaseUserStore(opts)
			log.Info(fmt.Sprintf("Users store will be created in database '%s' at '%s'",
				c.Databases[i].Name, c.Databases[i].Path))
			break
		}
	}

	// If no user database, use system database
	if targetStore == nil && (c.SystemDB != nil || config.SystemDB != nil) {
		log.Info("Using system database for users store")
		targetStore = NewSystemUserStore()
	}

	// If still no store, setup hasn't been completed yet - return nil
	if targetStore == nil {
		log.Debug("No databases configured yet - setup wizard needs to run")
		return nil
	}

	c.persistentUsersStore = targetStore
	return c.persistentUsersStore
}

func (c *Config) persistUserRecord(ctx context.Context, user *UserRecord) error {
	if c == nil || user == nil {
		return nil
	}
	if err := c.ensureProvisionedUsersStore(ctx); err != nil {
		return err
	}

	backingStore := c.ensurePersistentUserStore()
	if backingStore != nil {
		if err := backingStore.Save(ctx, user); err != nil {
			return err
		}
	}

	overrideStore := c.ensureUserStore()
	if overrideStore != nil && overrideStore != backingStore {
		if err := overrideStore.Save(ctx, user); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) syncUsersStoreFromConfig(ctx context.Context) error {
	// No longer needed - users are only stored in the persistent store
	return nil
}

func (c *Config) syncUsersFromStore(ctx context.Context) error {
	// No longer needed - we query the persistent store directly
	return nil
}

func (c *Config) ensureProvisionedUsersStore(ctx context.Context) error {
	if c == nil {
		return nil
	}
	store := c.ensurePersistentUserStore()
	if store == nil {
		// No databases configured yet - setup wizard hasn't run
		if len(c.Databases) > 0 || c.SystemDB != nil {
			// Databases ARE configured but we still got nil - something is wrong
			return fmt.Errorf("failed to determine user store location: databases configured but no valid user store backend found")
		}
		// No databases, no store - this is expected before setup wizard runs
		return nil
	}

	if dbStore, ok := store.(*DatabaseUserStore); ok {
		log.Debug(fmt.Sprintf("Provisioning users store in database at: %s", dbStore.opts.StoresFolders))
		if err := dbStore.EnsureProvisioned(ctx); err != nil {
			return fmt.Errorf("failed to initialize users store: %w", err)
		}
	} else if systemStore, ok := store.(*SystemUserStore); ok {
		log.Debug("Provisioning users store in system database")
		if err := systemStore.Save(ctx, &UserRecord{Username: "__provisioning_probe__", Role: sop.RoleGuest}); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "already exists") {
				return nil
			}
			return fmt.Errorf("failed to initialize users store: %w", err)
		}
		if err := systemStore.Delete(ctx, "__provisioning_probe__"); err != nil {
			return fmt.Errorf("failed to initialize users store: %w", err)
		}
	}
	if _, err := store.List(ctx); err != nil {
		return fmt.Errorf("failed to initialize users store: %w", err)
	}
	log.Info("Users store provisioned successfully")
	return nil
}

func (c *Config) findUserRecord(ctx context.Context, username string) (*UserRecord, bool, error) {
	username = normalizeUsername(username)
	if username == "" {
		return nil, false, nil
	}
	store := c.ensureUserStore()
	if store == nil {
		return nil, false, nil
	}
	user, ok, err := store.Get(ctx, username)
	if err != nil {
		return nil, false, err
	}
	if ok {
		return user, true, nil
	}
	return nil, false, nil
}

func (c *Config) FindUserRecord(ctx context.Context, username string) (*UserRecord, bool, error) {
	return c.findUserRecord(ctx, username)
}

// RegisterUser creates a user record using the shared auth store and default account semantics.
func (c *Config) RegisterUser(ctx context.Context, username, password, email, role string) (*UserRecord, error) {
	return c.RegisterUserWithRequest(ctx, RegisterUserRequest{
		Username: username,
		Password: password,
		Email:    email,
		Role:     role,
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

	hash, err := hashPassword(req.Password)
	if err != nil {
		return nil, err
	}

	record := UserRecord{
		Username:     username,
		Role:         role,
		PasswordHash: SecuredString(hash),
		Email:        SecuredString(strings.TrimSpace(req.Email)),
	}
	if err := c.persistUserRecord(ctx, &record); err != nil {
		return nil, err
	}
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

	store := c.ensureUserStore()
	if store == nil {
		return nil
	}

	if err := store.Delete(ctx, username); err != nil {
		return fmt.Errorf("delete user from store: %w", err)
	}

	// Note: Session revocation handled by token expiry
	return nil
}

// UpdateUserRole updates a user's role with permission checks.
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
		log.Info("auth.provider.trace", "username", normalizeUsername(username), "provider_error", err)
		return false, nil, err
	}
	log.Info("auth.provider.trace", "username", normalizeUsername(username), "provider", fmt.Sprintf("%T", provider))
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
	if !CurrentProvisioningDecision().Required {
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

func parseRegisterRequest(r *http.Request) (string, string, string, string, error) {
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err != nil {
			return "", "", "", "", err
		}
		return r.FormValue("username"), r.FormValue("password"), r.FormValue("email"), r.FormValue("role"), nil
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return "", "", "", "", err
	}
	return req.Username, req.Password, req.Email, req.Role, nil
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

	username, password, email, role, err := parseRegisterRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := config.RegisterUserWithRequest(r.Context(), RegisterUserRequest{
		Username: username,
		Password: password,
		Email:    email,
		Role:     role,
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
			"username": user.Username,
			"role":     user.Role,
			"email":    user.Email.Plaintext(),
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
			"username":   user.Username,
			"role":       user.Role,
			"email":      user.Email.Plaintext(),
			"created_at": user.CreatedAt,
			"updated_at": user.UpdatedAt,
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
		log.Warn("auth.login.parse_failed", "path", r.URL.Path, "method", r.Method, "remote_addr", r.RemoteAddr, "content_type", r.Header.Get("Content-Type"), "error", err)
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	log.Info("auth.login.request", "path", r.URL.Path, "method", r.Method, "remote_addr", r.RemoteAddr, "content_type", r.Header.Get("Content-Type"), "username", username)
	log.Info("auth.login.trace", "path", r.URL.Path, "method", r.Method, "username", username, "password_length", len(password))

	ok, user, err := config.Authenticate(username, password)
	if err != nil {
		log.Warn("auth.login.authenticate_error", "path", r.URL.Path, "method", r.Method, "remote_addr", r.RemoteAddr, "username", username, "error", err)
		recordProductMetric("auth_login_failures")
		writeJSONError(w, http.StatusInternalServerError, "authentication failed")
		return
	}
	if !ok || user == nil {
		log.Info("auth.login.invalid_credentials", "path", r.URL.Path, "method", r.Method, "remote_addr", r.RemoteAddr, "username", username)
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
					"username": validatedUser.Username,
					"role":     validatedUser.Role,
				},
			})
			return
		}
	}

	accessToken, refreshToken, err := currentTokenFacade().CreateSession(r.Context(), user.Username, user.Role)
	if err != nil {
		log.Warn("auth.login.create_session_error", "path", r.URL.Path, "method", r.Method, "remote_addr", r.RemoteAddr, "username", username, "user_role", user.Role, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	log.Info("auth.login.session_created", "path", r.URL.Path, "method", r.Method, "remote_addr", r.RemoteAddr, "username", username, "user_role", user.Role)

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
			"username": user.Username,
			"role":     user.Role,
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
	return s.getStoreWithMode(ctx, sop.ForWriting)
}

func (s *SessionStore) getStoreWithMode(ctx context.Context, mode sop.TransactionMode) (btree.BtreeInterface[string, SessionRecord], sop.Transaction, error) {
	db, err := getSystemDB(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("session store failed to get system db: %v", err)
	}
	tx, err := db.BeginTransaction(ctx, mode, -1)
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

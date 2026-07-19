package sopbackend

import (
	"context"
	"fmt"
	"strings"
	"sync"

	log "log/slog"

	"golang.org/x/crypto/bcrypt"
)

// AuthProvider is the extension point for external identity providers such as AD,
// Entra ID, LDAP, SAML, or OIDC.
//
// The built-in local provider remains the default, while additional providers can
// be registered without changing the main auth flow.
type AuthProvider interface {
	Name() string
	Authenticate(ctx context.Context, username, password string) (bool, *UserRecord, error)
}

type LocalAuthProvider struct {
	cfg *Config
}

func (p *LocalAuthProvider) Name() string {
	return "local"
}

func (p *LocalAuthProvider) Authenticate(ctx context.Context, username, password string) (bool, *UserRecord, error) {
	username = normalizeUsername(username)
	password = strings.TrimSpace(password)
	log.Info("auth.provider.trace", "provider", "local", "username", username, "password_length", len(password))
	if username == "" || password == "" {
		log.Info("auth.provider.trace", "provider", "local", "username", username, "reason", "empty_credentials")
		return false, nil, nil
	}

	user, ok, err := p.cfg.findUserRecord(ctx, username)
	if err != nil {
		log.Info("auth.provider.trace", "provider", "local", "username", username, "error", err)
		return false, nil, err
	}
	if !ok || user == nil {
		log.Info("auth.provider.trace", "provider", "local", "username", username, "reason", "user_not_found")
		return false, nil, nil
	}
	if user.PasswordHash.Plaintext() == "" {
		log.Info("auth.provider.trace", "provider", "local", "username", username, "reason", "empty_password_hash")
		return false, nil, nil
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash.Plaintext()), []byte(password)); err == nil {
		log.Info("auth.provider.trace", "provider", "local", "username", username, "reason", "password_match")
		return true, user, nil
	}
	log.Info("auth.provider.trace", "provider", "local", "username", username, "reason", "password_mismatch")
	return false, nil, nil
}

var (
	authProviderRegistry = map[string]func(*Config) (AuthProvider, error){}
	authProviderMu       sync.RWMutex
)

func RegisterAuthProvider(name string, factory func(*Config) (AuthProvider, error)) {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		panic("auth provider name cannot be empty")
	}
	authProviderMu.Lock()
	defer authProviderMu.Unlock()
	authProviderRegistry[name] = factory
}

func (c *Config) resolveAuthProvider() (AuthProvider, error) {
	name := strings.TrimSpace(strings.ToLower(c.AuthProviderName))
	if name == "" {
		return &LocalAuthProvider{cfg: c}, nil
	}

	authProviderMu.RLock()
	factory, ok := authProviderRegistry[name]
	authProviderMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown auth provider %q", name)
	}
	return factory(c)
}

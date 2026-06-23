package main

import (
	"fmt"
	"sync"
	"time"
)

type SubscriptionPolicy struct {
	MaxRequests int
	Window      time.Duration
}

type Session struct {
	ID                 string
	HostID             string
	SubscriptionID     string
	CreatedAt          time.Time
	LastSeen           time.Time
	RateLimitRemaining int
	RateLimitWindow    time.Duration
	RateLimitResetAt   time.Time
}

type SessionRegistry struct {
	mu       sync.RWMutex
	policy   SubscriptionPolicy
	sessions map[string]*Session
}

func NewSessionRegistry(policy SubscriptionPolicy) *SessionRegistry {
	if policy.MaxRequests <= 0 {
		policy.MaxRequests = 5
	}
	if policy.Window <= 0 {
		policy.Window = time.Minute
	}
	return &SessionRegistry{
		policy:   policy,
		sessions: make(map[string]*Session),
	}
}

func (r *SessionRegistry) RegisterSession(hostID, subscriptionID string) (*Session, error) {
	if hostID == "" || subscriptionID == "" {
		return nil, fmt.Errorf("host and subscription ids are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	sessionKey := fmt.Sprintf("%s:%s", hostID, subscriptionID)
	if sess, ok := r.sessions[sessionKey]; ok {
		sess.LastSeen = now
		r.resetIfNeeded(sess, now)
		return cloneSession(sess), nil
	}

	sess := &Session{
		ID:                 sessionKey,
		HostID:             hostID,
		SubscriptionID:     subscriptionID,
		CreatedAt:          now,
		LastSeen:           now,
		RateLimitRemaining: r.policy.MaxRequests,
		RateLimitWindow:    r.policy.Window,
		RateLimitResetAt:   now.Add(r.policy.Window),
	}
	r.sessions[sess.ID] = sess
	return cloneSession(sess), nil
}

func (r *SessionRegistry) Heartbeat(sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	sess, ok := r.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	now := time.Now()
	r.resetIfNeeded(sess, now)
	sess.LastSeen = now
	return nil
}

func (r *SessionRegistry) GetSession(sessionID string) *Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sess, ok := r.sessions[sessionID]
	if !ok {
		return nil
	}
	return cloneSession(sess)
}

func (r *SessionRegistry) ConsumeRequest(sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	sess, ok := r.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	now := time.Now()
	r.resetIfNeeded(sess, now)
	if sess.RateLimitRemaining <= 0 {
		return fmt.Errorf("rate limit exceeded")
	}
	sess.RateLimitRemaining--
	return nil
}

func (r *SessionRegistry) resetIfNeeded(sess *Session, now time.Time) {
	if sess.RateLimitResetAt.IsZero() || !now.Before(sess.RateLimitResetAt) {
		sess.RateLimitRemaining = r.policy.MaxRequests
		sess.RateLimitResetAt = now.Add(r.policy.Window)
	}
}

func cloneSession(sess *Session) *Session {
	if sess == nil {
		return nil
	}
	cloned := *sess
	return &cloned
}

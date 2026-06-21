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
	sess := &Session{
		ID:                 fmt.Sprintf("%s-%d", hostID, now.UnixNano()),
		HostID:             hostID,
		SubscriptionID:     subscriptionID,
		CreatedAt:          now,
		LastSeen:           now,
		RateLimitRemaining: r.policy.MaxRequests,
		RateLimitWindow:    r.policy.Window,
		RateLimitResetAt:   now.Add(r.policy.Window),
	}
	r.sessions[sess.ID] = sess
	return sess, nil
}

func (r *SessionRegistry) Heartbeat(sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	sess, ok := r.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	sess.LastSeen = time.Now()
	return nil
}

func (r *SessionRegistry) GetSession(sessionID string) *Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sess, ok := r.sessions[sessionID]
	if !ok {
		return nil
	}
	return &Session{
		ID:                 sess.ID,
		HostID:             sess.HostID,
		SubscriptionID:     sess.SubscriptionID,
		CreatedAt:          sess.CreatedAt,
		LastSeen:           sess.LastSeen,
		RateLimitRemaining: sess.RateLimitRemaining,
		RateLimitWindow:    sess.RateLimitWindow,
		RateLimitResetAt:   sess.RateLimitResetAt,
	}
}

func (r *SessionRegistry) ConsumeRequest(sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	sess, ok := r.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	if sess.RateLimitRemaining <= 0 {
		return fmt.Errorf("rate limit exceeded")
	}
	sess.RateLimitRemaining--
	return nil
}

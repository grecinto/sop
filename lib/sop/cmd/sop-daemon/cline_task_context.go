package main

import (
	"strings"
	"sync"
	"time"

	"github.com/sharedcode/sop/ai/agent/domains/ideworker"
)

const (
	clineTaskContextTTL   = 24 * time.Hour
	clineTaskContextLimit = 512
)

type clineTaskContext struct {
	WorkstreamID  string
	SessionID     string
	RequestID     string
	Task          string
	WorkspaceRoot string
	ActiveFile    string
	SelectedText  string
	OpenFiles     []string
	Mode          string
	Phase         string
	Status        string
	Message       string
	Validation    map[string]any
	UpdatedAt     time.Time
}

type clineTaskContextStore struct {
	mu       sync.RWMutex
	contexts map[string]clineTaskContext
}

func (s *clineTaskContextStore) PutFromRequest(req ideworker.TaskRequest, status, message string) {
	if s == nil {
		return
	}
	key := clineTaskContextKey(req.WorkstreamID, req.SessionID)
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	previous := s.contexts[key]
	validation := map[string]any(nil)
	if previous.RequestID == strings.TrimSpace(req.RequestID) {
		validation = cloneAnyMap(previous.Validation)
	}
	if validation == nil {
		validation = make(map[string]any)
	}
	if previous.RequestID == strings.TrimSpace(req.RequestID) && previous.Status == "stale" {
		status = previous.Status
		message = previous.Message
	}
	s.contexts[key] = clineTaskContext{
		WorkstreamID:  strings.TrimSpace(req.WorkstreamID),
		SessionID:     strings.TrimSpace(req.SessionID),
		RequestID:     strings.TrimSpace(req.RequestID),
		Task:          strings.TrimSpace(req.Task),
		WorkspaceRoot: strings.TrimSpace(req.WorkspaceRoot),
		ActiveFile:    strings.TrimSpace(req.ActiveFile),
		SelectedText:  strings.TrimSpace(req.SelectedText),
		OpenFiles:     append([]string(nil), req.OpenFiles...),
		Mode:          strings.TrimSpace(req.Mode),
		Phase:         strings.TrimSpace(req.RequestedPhase),
		Status:        strings.TrimSpace(status),
		Message:       strings.TrimSpace(message),
		Validation:    validation,
		UpdatedAt:     time.Now().UTC(),
	}
	if len(s.contexts) > clineTaskContextLimit {
		s.pruneOldestLocked(len(s.contexts) - clineTaskContextLimit)
	}
}

func (s *clineTaskContextStore) Get(workstreamID, sessionID string) (clineTaskContext, bool) {
	if s == nil {
		return clineTaskContext{}, false
	}
	key := clineTaskContextKey(workstreamID, sessionID)
	s.mu.RLock()
	context, ok := s.contexts[key]
	s.mu.RUnlock()
	if !ok || !clineTaskContextMatches(context, workstreamID, sessionID) || time.Since(context.UpdatedAt) > clineTaskContextTTL {
		return clineTaskContext{}, false
	}
	context.OpenFiles = append([]string(nil), context.OpenFiles...)
	context.Validation = cloneAnyMap(context.Validation)
	return context, true
}

func (s *clineTaskContextStore) RecordValidation(workstreamID, sessionID string, validation map[string]any) (clineTaskContext, bool) {
	if s == nil {
		return clineTaskContext{}, false
	}
	key := clineTaskContextKey(workstreamID, sessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	context, ok := s.contexts[key]
	if !ok || !clineTaskContextMatches(context, workstreamID, sessionID) || time.Since(context.UpdatedAt) > clineTaskContextTTL {
		return clineTaskContext{}, false
	}
	context.Validation = cloneAnyMap(validation)
	context.UpdatedAt = time.Now().UTC()
	s.contexts[key] = context
	return context, true
}

func (s *clineTaskContextStore) MarkStale(workstreamID, sessionID, message string) (clineTaskContext, bool) {
	if s == nil {
		return clineTaskContext{}, false
	}
	key := clineTaskContextKey(workstreamID, sessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	context, ok := s.contexts[key]
	if !ok || !clineTaskContextMatches(context, workstreamID, sessionID) || time.Since(context.UpdatedAt) > clineTaskContextTTL {
		return clineTaskContext{}, false
	}
	context.Status = "stale"
	context.Message = strings.TrimSpace(message)
	context.UpdatedAt = time.Now().UTC()
	s.contexts[key] = context
	return context, true
}

func clineTaskContextKey(workstreamID, sessionID string) string {
	if workstreamID = strings.TrimSpace(workstreamID); workstreamID != "" {
		return workstreamID
	}
	return strings.TrimSpace(sessionID)
}

func clineTaskContextMatches(context clineTaskContext, workstreamID, sessionID string) bool {
	if strings.TrimSpace(sessionID) != "" && context.SessionID != strings.TrimSpace(sessionID) {
		return false
	}
	return strings.TrimSpace(workstreamID) == "" || context.WorkstreamID == strings.TrimSpace(workstreamID)
}

func (s *clineTaskContextStore) pruneLocked(now time.Time) {
	for key, context := range s.contexts {
		if now.Sub(context.UpdatedAt) > clineTaskContextTTL {
			delete(s.contexts, key)
		}
	}
}

func (s *clineTaskContextStore) pruneOldestLocked(count int) {
	for range count {
		oldestKey := ""
		var oldest time.Time
		for key, context := range s.contexts {
			if oldestKey == "" || context.UpdatedAt.Before(oldest) {
				oldestKey, oldest = key, context.UpdatedAt
			}
		}
		if oldestKey == "" {
			return
		}
		delete(s.contexts, oldestKey)
	}
}

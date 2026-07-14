package sop

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"time"
)

// StorageHealthState models the health of a storage path.
type StorageHealthState string

const (
	StorageHealthHealthy  StorageHealthState = "healthy"
	StorageHealthDegraded StorageHealthState = "degraded"
	StorageHealthReadOnly StorageHealthState = "read_only"
)

// StoragePathHealthState describes the current health state of a particular
// storage path and the event that caused it to be surfaced.
type StoragePathHealthState struct {
	Operation        string             `json:"operation"`
	Path             string             `json:"path"`
	Count            int                `json:"count"`
	Status           StorageHealthState `json:"status"`
	Prompt           string             `json:"prompt,omitempty"`
	LastUpdatedAt    int64              `json:"last_updated_at,omitempty"`
	LastError        string             `json:"last_error,omitempty"`
	SuggestedAction  string             `json:"suggested_action,omitempty"`
	NeedsAttention   bool               `json:"needs_attention,omitempty"`
	AutoRetryEnabled bool               `json:"auto_retry_enabled,omitempty"`
	MaxRetryCount    int                `json:"max_retry_count,omitempty"`
}

// StorageHealthPolicy owns the shared storage-health policy for the data layer.
type StorageHealthPolicy interface {
	RecordFailure(operation, targetPath string, err error)
	StoragePathState(operation, targetPath string) StoragePathHealthState
	StoragePathStates() []StoragePathHealthState
	NeedsAttention() bool
	ShouldAutoRetry(operation, targetPath string) bool
}

type storageHealthPolicy struct {
	mu     sync.Mutex
	counts map[string]int
	states map[string]StoragePathHealthState
}

func NewStorageHealthPolicy() StorageHealthPolicy {
	return &storageHealthPolicy{
		counts: make(map[string]int),
		states: make(map[string]StoragePathHealthState),
	}
}

var (
	defaultStorageHealthPolicyMu sync.RWMutex
	defaultStorageHealthPolicy   StorageHealthPolicy
	storagePathFailureHandlersMu sync.RWMutex
	storagePathFailureHandlers   []func(operation, targetPath string, err error)
)

// DefaultStorageHealthPolicy returns the shared storage-health policy owned by
// the data layer. Higher layers can read from it and surface its state.
func DefaultStorageHealthPolicy() StorageHealthPolicy {
	defaultStorageHealthPolicyMu.RLock()
	policy := defaultStorageHealthPolicy
	defaultStorageHealthPolicyMu.RUnlock()
	if policy != nil {
		return policy
	}

	defaultStorageHealthPolicyMu.Lock()
	defer defaultStorageHealthPolicyMu.Unlock()
	if defaultStorageHealthPolicy == nil {
		defaultStorageHealthPolicy = NewStorageHealthPolicy()
	}
	return defaultStorageHealthPolicy
}

// SetDefaultStorageHealthPolicy installs a custom storage-health policy and
// returns a restore function.
func SetDefaultStorageHealthPolicy(policy StorageHealthPolicy) func() {
	defaultStorageHealthPolicyMu.Lock()
	defer defaultStorageHealthPolicyMu.Unlock()
	previous := defaultStorageHealthPolicy
	defaultStorageHealthPolicy = policy
	return func() {
		defaultStorageHealthPolicyMu.Lock()
		defer defaultStorageHealthPolicyMu.Unlock()
		defaultStorageHealthPolicy = previous
	}
}

// RegisterStoragePathFailureHandler installs a callback that is invoked whenever
// the shared storage-health policy records a storage path failure.
func RegisterStoragePathFailureHandler(handler func(operation, targetPath string, err error)) {
	if handler == nil {
		return
	}
	storagePathFailureHandlersMu.Lock()
	defer storagePathFailureHandlersMu.Unlock()
	storagePathFailureHandlers = append(storagePathFailureHandlers, handler)
}

func notifyStoragePathFailureHandlers(operation, targetPath string, err error) {
	storagePathFailureHandlersMu.RLock()
	handlers := append([]func(operation, targetPath string, err error){}, storagePathFailureHandlers...)
	storagePathFailureHandlersMu.RUnlock()
	for _, handler := range handlers {
		if handler != nil {
			handler(operation, targetPath, err)
		}
	}
}

// ReportStoragePathFailure records a failover-qualified storage-path failure in the
// shared policy so higher layers can inspect the current health state.
func ReportStoragePathFailure(operation, targetPath string, err error) {
	DefaultStorageHealthPolicy().RecordFailure(operation, targetPath, err)
	notifyStoragePathFailureHandlers(operation, targetPath, err)
}

func (p *storageHealthPolicy) RecordFailure(operation, targetPath string, err error) {
	if p == nil {
		return
	}

	key := storageHealthStateKey(operation, targetPath)
	p.mu.Lock()
	defer p.mu.Unlock()

	p.counts[key]++
	count := p.counts[key]
	state := p.states[key]
	state.Operation = strings.TrimSpace(operation)
	state.Path = strings.TrimSpace(targetPath)
	state.Count = count
	state.LastUpdatedAt = time.Now().Unix()
	state.LastError = ""
	if err != nil {
		state.LastError = err.Error()
	}

	if storagePathErrorIsReadOnly(err) {
		state.Status = StorageHealthReadOnly
		state.NeedsAttention = true
		state.AutoRetryEnabled = false
		state.SuggestedAction = "stop_writes"
		state.MaxRetryCount = 3
		state.Prompt = fmt.Sprintf("Storage path %s is now read-only during %s; stop writes to this path and inspect the device or mount before recovery.", state.Path, state.Operation)
	} else {
		if state.Status == "" || state.Status == StorageHealthHealthy {
			state.Status = StorageHealthDegraded
		}
		state.NeedsAttention = count >= 3
		if state.MaxRetryCount <= 0 {
			state.MaxRetryCount = 3
		}
		state.AutoRetryEnabled = count >= 3 && count <= state.MaxRetryCount
		state.SuggestedAction = "inspect_storage"
		if count >= 3 {
			state.Prompt = fmt.Sprintf("Storage path %s is repeatedly failing during %s; if a healthy passive replica exists, fail over to it; otherwise stop writes to this path and inspect the device or mount.", state.Path, state.Operation)
		} else {
			state.Prompt = ""
		}
	}
	p.states[key] = state
}

func (p *storageHealthPolicy) StoragePathState(operation, targetPath string) StoragePathHealthState {
	if p == nil {
		return StoragePathHealthState{Status: StorageHealthHealthy}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	key := storageHealthStateKey(operation, targetPath)
	state, ok := p.states[key]
	if !ok {
		return StoragePathHealthState{Status: StorageHealthHealthy}
	}
	return state
}

func (p *storageHealthPolicy) StoragePathStates() []StoragePathHealthState {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	states := make([]StoragePathHealthState, 0, len(p.states))
	for _, state := range p.states {
		states = append(states, state)
	}
	return states
}

func (p *storageHealthPolicy) NeedsAttention() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, state := range p.states {
		if state.NeedsAttention {
			return true
		}
	}
	return false
}

func (p *storageHealthPolicy) ShouldAutoRetry(operation, targetPath string) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	state, ok := p.states[storageHealthStateKey(operation, targetPath)]
	if !ok {
		return false
	}
	if state.Status == StorageHealthReadOnly || !state.AutoRetryEnabled {
		return false
	}
	if state.MaxRetryCount <= 0 {
		state.MaxRetryCount = 3
	}
	return state.Count >= 3 && state.Count <= state.MaxRetryCount
}

func storageHealthStateKey(operation, targetPath string) string {
	return fmt.Sprintf("%s:%s", strings.TrimSpace(operation), strings.TrimSpace(targetPath))
}

func storagePathErrorIsReadOnly(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EROFS) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "read-only") || strings.Contains(msg, "readonly") || strings.Contains(msg, "write-protected")
}

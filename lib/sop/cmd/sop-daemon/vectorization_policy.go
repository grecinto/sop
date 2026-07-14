package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// runtimeVectorizationFailurePolicy implements the shared runtime behavior for
// semantic vectorization failures: the first failure is skipped, repeated
// failures escalate after a small threshold.
type runtimeVectorizationFailurePolicy struct {
	mu                sync.Mutex
	counts            map[string]int
	states            map[string]runtimeVectorizationRecoveryState
	storagePathStates map[string]runtimeDataPathRecoveryState
	stateDir          string
}

type runtimeVectorizationRecoveryState struct {
	KBName           string `json:"kb_name"`
	TargetType       string `json:"target_type"`
	TargetID         string `json:"target_id"`
	Count            int    `json:"count"`
	Status           string `json:"status"`
	Prompt           string `json:"prompt"`
	RetryCount       int    `json:"retry_count"`
	NextRetryAt      int64  `json:"next_retry_at,omitempty"`
	LastAttemptAt    int64  `json:"last_attempt_at,omitempty"`
	LastUpdatedAt    int64  `json:"last_updated_at,omitempty"`
	LastError        string `json:"last_error,omitempty"`
	SuggestedAction  string `json:"suggested_action,omitempty"`
	NeedsAttention   bool   `json:"needs_attention,omitempty"`
	AutoRetryEnabled bool   `json:"auto_retry_enabled,omitempty"`
	MaxRetryCount    int    `json:"max_retry_count,omitempty"`
}

type runtimeDataPathRecoveryState struct {
	Operation        string `json:"operation"`
	Path             string `json:"path"`
	Count            int    `json:"count"`
	Status           string `json:"status"`
	Prompt           string `json:"prompt,omitempty"`
	LastUpdatedAt    int64  `json:"last_updated_at,omitempty"`
	LastError        string `json:"last_error,omitempty"`
	SuggestedAction  string `json:"suggested_action,omitempty"`
	NeedsAttention   bool   `json:"needs_attention,omitempty"`
	AutoRetryEnabled bool   `json:"auto_retry_enabled,omitempty"`
	MaxRetryCount    int    `json:"max_retry_count,omitempty"`
}

type runtimeVectorizationFailureState struct {
	Counts            map[string]int                               `json:"counts"`
	States            map[string]runtimeVectorizationRecoveryState `json:"states"`
	StoragePathStates map[string]runtimeDataPathRecoveryState      `json:"storage_path_states,omitempty"`
}

func newRuntimeVectorizationFailurePolicy(stateDir string) runtimeVectorizationFailurePolicyHandle {
	if strings.TrimSpace(stateDir) == "" {
		stateDir = os.TempDir()
	}
	policy := &runtimeVectorizationFailurePolicy{counts: make(map[string]int), states: make(map[string]runtimeVectorizationRecoveryState), storagePathStates: make(map[string]runtimeDataPathRecoveryState), stateDir: stateDir}
	policy.load()
	return policy
}

func (p *runtimeVectorizationFailurePolicy) HandleFailure(kbName, targetType, targetID string, err error) (bool, bool) {
	if p == nil {
		return false, false
	}

	key := fmt.Sprintf("%s:%s:%s", kbName, targetType, targetID)
	p.mu.Lock()
	defer p.mu.Unlock()

	p.counts[key]++
	count := p.counts[key]
	if err := p.save(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to persist daemon vectorization failure state: %v\n", err)
	}
	state := p.currentState(key)
	if state.Status == "" {
		state.Status = "degraded"
	}
	state.Count = count
	state.KBName = kbName
	state.TargetType = targetType
	state.TargetID = targetID
	state.LastUpdatedAt = time.Now().Unix()
	state.LastError = err.Error()
	state.NeedsAttention = count >= 3
	state.AutoRetryEnabled = count >= 3
	state.SuggestedAction = "recover"
	if state.MaxRetryCount <= 0 {
		state.MaxRetryCount = 3
	}
	if count >= 3 {
		state.Prompt = fmt.Sprintf("Vectorization for %s %s is repeatedly failing; consider rebuilding the semantic index.", targetType, targetID)
		state.NextRetryAt = time.Now().Add(vectorizationRecoveryRetryBackoff).Unix()
	} else {
		state.Prompt = ""
	}
	p.states[key] = state
	if count >= 3 {
		return true, true
	}
	return true, false
}

func (p *runtimeVectorizationFailurePolicy) RecordRetryAttempt(kbName, targetType, targetID string) {
	if p == nil {
		return
	}
	key := fmt.Sprintf("%s:%s:%s", kbName, targetType, targetID)
	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.currentState(key)
	state.KBName = kbName
	state.TargetType = targetType
	state.TargetID = targetID
	state.RetryCount++
	state.LastAttemptAt = time.Now().Unix()
	state.LastUpdatedAt = state.LastAttemptAt
	state.AutoRetryEnabled = true
	if state.MaxRetryCount <= 0 {
		state.MaxRetryCount = 3
	}
	state.NextRetryAt = time.Now().Add(vectorizationRecoveryRetryBackoff).Unix()
	if state.Status == "" {
		state.Status = "degraded"
	}
	p.states[key] = state
	_ = p.save()
}

func (p *runtimeVectorizationFailurePolicy) MarkIgnored(kbName, targetType, targetID string) {
	if p == nil {
		return
	}
	key := fmt.Sprintf("%s:%s:%s", kbName, targetType, targetID)
	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.currentState(key)
	state.KBName = kbName
	state.TargetType = targetType
	state.TargetID = targetID
	state.Status = "ignored"
	p.states[key] = state
	_ = p.save()
}

func (p *runtimeVectorizationFailurePolicy) MarkScheduled(kbName, targetType, targetID string) {
	if p == nil {
		return
	}
	key := fmt.Sprintf("%s:%s:%s", kbName, targetType, targetID)
	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.currentState(key)
	state.KBName = kbName
	state.TargetType = targetType
	state.TargetID = targetID
	state.Status = "scheduled"
	state.SuggestedAction = "recover"
	state.NeedsAttention = true
	state.AutoRetryEnabled = false
	state.NextRetryAt = time.Now().Add(15 * time.Minute).Unix()
	state.Prompt = fmt.Sprintf("Retry for %s %s has been scheduled for later.", targetType, targetID)
	p.states[key] = state
	_ = p.save()
}

func (p *runtimeVectorizationFailurePolicy) MarkAbandoned(kbName, targetType, targetID string) {
	if p == nil {
		return
	}
	key := fmt.Sprintf("%s:%s:%s", kbName, targetType, targetID)
	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.currentState(key)
	state.KBName = kbName
	state.TargetType = targetType
	state.TargetID = targetID
	state.Status = "abandoned"
	state.SuggestedAction = ""
	state.NeedsAttention = false
	state.AutoRetryEnabled = false
	state.NextRetryAt = 0
	state.Prompt = fmt.Sprintf("Recovery for %s %s was abandoned after repeated failed attempts.", targetType, targetID)
	p.states[key] = state
	_ = p.save()
}

func (p *runtimeVectorizationFailurePolicy) MarkRecovered(kbName, targetType, targetID string) {
	if p == nil {
		return
	}
	key := fmt.Sprintf("%s:%s:%s", kbName, targetType, targetID)
	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.currentState(key)
	state.KBName = kbName
	state.TargetType = targetType
	state.TargetID = targetID
	state.Status = "recovered"
	state.LastUpdatedAt = time.Now().Unix()
	state.NeedsAttention = false
	state.AutoRetryEnabled = false
	state.SuggestedAction = ""
	state.NextRetryAt = 0
	p.states[key] = state
	_ = p.save()
}

func (p *runtimeVectorizationFailurePolicy) RecoveryState() runtimeVectorizationRecoveryState {
	if p == nil {
		return runtimeVectorizationRecoveryState{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	state := runtimeVectorizationRecoveryState{Status: "healthy"}
	if len(p.counts) == 0 {
		return state
	}

	for key, count := range p.counts {
		if count > state.Count {
			state.Count = count
			savedState, ok := p.states[key]
			if ok && strings.TrimSpace(savedState.Status) != "" {
				state.Status = savedState.Status
			} else {
				state.Status = "degraded"
			}
			parts := strings.Split(key, ":")
			if len(parts) >= 3 {
				state.KBName = parts[0]
				state.TargetType = parts[1]
				state.TargetID = parts[2]
			}
			if savedState := p.states[key]; savedState.KBName != "" {
				state.KBName = savedState.KBName
			}
			if savedState := p.states[key]; savedState.TargetType != "" {
				state.TargetType = savedState.TargetType
			}
			if savedState := p.states[key]; savedState.TargetID != "" {
				state.TargetID = savedState.TargetID
			}
			if savedState := p.states[key]; savedState.Prompt != "" {
				state.Prompt = savedState.Prompt
			}
		}
	}
	return state
}

func (p *runtimeVectorizationFailurePolicy) RecoveryStates() []runtimeVectorizationRecoveryState {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	states := make([]runtimeVectorizationRecoveryState, 0, len(p.counts))
	for key, count := range p.counts {
		state := p.currentState(key)
		if state.Status == "" {
			state.Status = "degraded"
		}
		state.Count = count
		parts := strings.Split(key, ":")
		if len(parts) >= 3 {
			state.KBName = parts[0]
			state.TargetType = parts[1]
			state.TargetID = parts[2]
		}
		states = append(states, state)
	}
	return states
}

func (p *runtimeVectorizationFailurePolicy) NeedsRecovery() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, state := range p.storagePathStates {
		if state.Status != "" && state.Status != "healthy" {
			return true
		}
	}
	for key, count := range p.counts {
		state := p.currentState(key)
		if count >= 3 || strings.EqualFold(state.Status, "degraded") || strings.EqualFold(state.Status, "ignored") || strings.EqualFold(state.Status, "scheduled") {
			return true
		}
	}
	return false
}

func (p *runtimeVectorizationFailurePolicy) RecordStoragePathFailure(operation, targetPath string, err error) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	key := fmt.Sprintf("storage:%s:%s", strings.TrimSpace(operation), strings.TrimSpace(targetPath))
	state := runtimeDataPathRecoveryState{Operation: operation, Path: targetPath}
	if existing, ok := p.storagePathStates[key]; ok {
		state = existing
	}
	state.Count++
	state.LastUpdatedAt = time.Now().Unix()
	state.LastError = ""
	if err != nil {
		state.LastError = err.Error()
	}
	if strings.Contains(strings.ToLower(state.LastError), "read-only") || strings.Contains(strings.ToLower(state.LastError), "readonly") || strings.Contains(strings.ToLower(state.LastError), "write-protected") || errors.Is(err, syscall.EROFS) {
		state.Status = "read_only"
		state.NeedsAttention = true
		state.AutoRetryEnabled = false
		state.SuggestedAction = "stop_writes"
		state.MaxRetryCount = 3
		state.Prompt = fmt.Sprintf("Storage path %s is now read-only during %s; stop writes to this path and inspect the device or mount before recovery.", state.Path, state.Operation)
	} else {
		state.Status = "degraded"
		state.SuggestedAction = "inspect_storage"
		state.NeedsAttention = state.Count >= 3
		state.AutoRetryEnabled = state.Count >= 3 && state.Count <= 3
		state.MaxRetryCount = 3
		if state.Count >= 3 {
			state.Prompt = fmt.Sprintf("Storage path %s is repeatedly failing during %s; if a healthy passive replica exists, fail over to it; otherwise stop writes to this path and inspect the device or mount.", state.Path, state.Operation)
		}
	}
	p.storagePathStates[key] = state
	_ = p.save()
}

func (p *runtimeVectorizationFailurePolicy) StoragePathRecoveryState() runtimeDataPathRecoveryState {
	if p == nil {
		return runtimeDataPathRecoveryState{Status: "healthy"}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, state := range p.storagePathStates {
		if state.Status != "healthy" {
			return state
		}
	}
	return runtimeDataPathRecoveryState{Status: "healthy"}
}

func (p *runtimeVectorizationFailurePolicy) StoragePathRecoveryStates() []runtimeDataPathRecoveryState {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	states := make([]runtimeDataPathRecoveryState, 0, len(p.storagePathStates))
	for _, state := range p.storagePathStates {
		if state.Status != "healthy" {
			states = append(states, state)
		}
	}
	return states
}

func (p *runtimeVectorizationFailurePolicy) currentState(key string) runtimeVectorizationRecoveryState {
	state, ok := p.states[key]
	if !ok {
		return runtimeVectorizationRecoveryState{}
	}
	return state
}

func (p *runtimeVectorizationFailurePolicy) load() {
	if p == nil {
		return
	}
	if strings.TrimSpace(p.stateDir) == "" {
		return
	}
	path := filepath.Join(p.stateDir, "vectorization-failure-state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var state runtimeVectorizationFailureState
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}
	if state.Counts == nil {
		state.Counts = make(map[string]int)
	}
	for key, count := range state.Counts {
		p.counts[key] = count
	}
	if state.States == nil {
		state.States = make(map[string]runtimeVectorizationRecoveryState)
	}
	for key, savedState := range state.States {
		p.states[key] = savedState
	}
	if state.StoragePathStates == nil {
		state.StoragePathStates = make(map[string]runtimeDataPathRecoveryState)
	}
	for key, savedState := range state.StoragePathStates {
		p.storagePathStates[key] = savedState
	}
}

func (p *runtimeVectorizationFailurePolicy) save() error {
	if p == nil {
		return nil
	}
	if strings.TrimSpace(p.stateDir) == "" {
		return nil
	}
	if err := os.MkdirAll(p.stateDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(p.stateDir, "vectorization-failure-state.json")
	data, err := json.Marshal(runtimeVectorizationFailureState{Counts: p.counts, States: p.states, StoragePathStates: p.storagePathStates})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// KBEffectiveness tracks the success rate of a KB for a specific task type
type KBEffectiveness struct {
	TaskType      string    `json:"task_type"`      // e.g., "implementation", "debugging"
	KBName        string    `json:"kb_name"`        // Knowledge Base identifier
	SuccessCount  int       `json:"success_count"`  // Number of successful outcomes
	FailureCount  int       `json:"failure_count"`  // Number of failed outcomes
	TotalScore    float64   `json:"total_score"`    // Sum of compliance scores
	LastUsed      time.Time `json:"last_used"`      // Most recent usage timestamp
	LastUpdated   time.Time `json:"last_updated"`   // Most recent update timestamp
	RecentSuccess []bool    `json:"recent_success"` // Last 10 outcomes (for recency weighting)
}

// EffectivenessScore computes the success rate with recency weighting
func (e *KBEffectiveness) EffectivenessScore() float64 {
	total := e.SuccessCount + e.FailureCount
	if total == 0 {
		return 0.5 // No data, neutral score
	}

	// Base success rate
	baseRate := float64(e.SuccessCount) / float64(total)

	// Apply recency weighting using recent outcomes
	if len(e.RecentSuccess) > 0 {
		recentSuccessCount := 0
		for _, success := range e.RecentSuccess {
			if success {
				recentSuccessCount++
			}
		}
		recentRate := float64(recentSuccessCount) / float64(len(e.RecentSuccess))

		// Weight: 70% base rate, 30% recent rate
		return 0.7*baseRate + 0.3*recentRate
	}

	return baseRate
}

// AverageComplianceScore returns the average compliance score
func (e *KBEffectiveness) AverageComplianceScore() float64 {
	total := e.SuccessCount + e.FailureCount
	if total == 0 {
		return 0.0
	}
	return e.TotalScore / float64(total)
}

// KBEffectivenessTracker tracks and queries KB effectiveness metrics
type KBEffectivenessTracker struct {
	mu           sync.RWMutex
	data         map[string]*KBEffectiveness // key: taskType:kbName
	storePath    string                      // Path to persistence file
	autoSave     bool                        // Whether to auto-save on updates
	saveInterval time.Duration               // Interval for auto-save
	stopChan     chan struct{}               // Channel to stop background save
}

// NewKBEffectivenessTracker creates a new tracker
func NewKBEffectivenessTracker(storePath string, autoSave bool) (*KBEffectivenessTracker, error) {
	tracker := &KBEffectivenessTracker{
		data:         make(map[string]*KBEffectiveness),
		storePath:    storePath,
		autoSave:     autoSave,
		saveInterval: 5 * time.Minute,
		stopChan:     make(chan struct{}),
	}

	// Load existing data if available
	if err := tracker.Load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load effectiveness data: %w", err)
	}

	// Start background save if auto-save enabled
	if autoSave {
		go tracker.backgroundSave()
	}

	return tracker, nil
}

// Close stops background operations
func (t *KBEffectivenessTracker) Close() error {
	if t.autoSave {
		close(t.stopChan)
	}
	return t.Save()
}

// RecordOutcome records an outcome for a KB and task type
func (t *KBEffectivenessTracker) RecordOutcome(taskType, kbName string, success bool, complianceScore float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := fmt.Sprintf("%s:%s", taskType, kbName)
	entry, exists := t.data[key]
	if !exists {
		entry = &KBEffectiveness{
			TaskType:      taskType,
			KBName:        kbName,
			SuccessCount:  0,
			FailureCount:  0,
			TotalScore:    0.0,
			RecentSuccess: []bool{},
		}
		t.data[key] = entry
	}

	// Update counts
	if success {
		entry.SuccessCount++
	} else {
		entry.FailureCount++
	}

	// Update compliance score total
	entry.TotalScore += complianceScore

	// Update recent success tracking (keep last 10)
	entry.RecentSuccess = append(entry.RecentSuccess, success)
	if len(entry.RecentSuccess) > 10 {
		entry.RecentSuccess = entry.RecentSuccess[1:]
	}

	// Update timestamps
	now := time.Now()
	entry.LastUsed = now
	entry.LastUpdated = now
}

// GetBestKBsForTask returns the best performing KBs for a task type
func (t *KBEffectivenessTracker) GetBestKBsForTask(taskType string, limit int) []KBRanking {
	t.mu.RLock()
	defer t.mu.RUnlock()

	rankings := []KBRanking{}

	for _, entry := range t.data {
		if entry.TaskType == taskType {
			rankings = append(rankings, KBRanking{
				KBName:             entry.KBName,
				EffectivenessScore: entry.EffectivenessScore(),
				SuccessRate:        float64(entry.SuccessCount) / float64(entry.SuccessCount+entry.FailureCount),
				AvgCompliance:      entry.AverageComplianceScore(),
				UsageCount:         entry.SuccessCount + entry.FailureCount,
				LastUsed:           entry.LastUsed,
			})
		}
	}

	// Sort by effectiveness score (descending)
	sortKBRankings(rankings)

	// Apply limit if specified
	if limit > 0 && len(rankings) > limit {
		rankings = rankings[:limit]
	}

	return rankings
}

// GetKBEffectiveness retrieves effectiveness data for a specific KB and task type
func (t *KBEffectivenessTracker) GetKBEffectiveness(taskType, kbName string) *KBEffectiveness {
	t.mu.RLock()
	defer t.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", taskType, kbName)
	entry, exists := t.data[key]
	if !exists {
		return nil
	}

	// Return a copy to avoid external mutation
	copy := *entry
	return &copy
}

// GetAllKBs returns all tracked KBs across all task types
func (t *KBEffectivenessTracker) GetAllKBs() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	kbSet := make(map[string]bool)
	for _, entry := range t.data {
		kbSet[entry.KBName] = true
	}

	kbs := []string{}
	for kb := range kbSet {
		kbs = append(kbs, kb)
	}

	return kbs
}

// GetPoorPerformers returns KBs with success rate below threshold
func (t *KBEffectivenessTracker) GetPoorPerformers(minUsageCount int, maxSuccessRate float64) []KBEffectiveness {
	t.mu.RLock()
	defer t.mu.RUnlock()

	poorPerformers := []KBEffectiveness{}

	for _, entry := range t.data {
		usageCount := entry.SuccessCount + entry.FailureCount
		if usageCount < minUsageCount {
			continue // Not enough data
		}

		successRate := float64(entry.SuccessCount) / float64(usageCount)
		if successRate < maxSuccessRate {
			copy := *entry
			poorPerformers = append(poorPerformers, copy)
		}
	}

	return poorPerformers
}

// Save persists the effectiveness data to disk
func (t *KBEffectivenessTracker) Save() error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.storePath == "" {
		return nil // No persistence configured
	}

	// Ensure directory exists
	dir := filepath.Dir(t.storePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// Marshal data
	data, err := json.MarshalIndent(t.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal data: %w", err)
	}

	// Write to temp file first
	tempPath := t.storePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, t.storePath); err != nil {
		return fmt.Errorf("rename file: %w", err)
	}

	return nil
}

// Load reads effectiveness data from disk
func (t *KBEffectivenessTracker) Load() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.storePath == "" {
		return nil // No persistence configured
	}

	data, err := os.ReadFile(t.storePath)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, &t.data); err != nil {
		return fmt.Errorf("unmarshal data: %w", err)
	}

	return nil
}

// backgroundSave periodically saves data to disk
func (t *KBEffectivenessTracker) backgroundSave() {
	ticker := time.NewTicker(t.saveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := t.Save(); err != nil {
				// Log error but don't crash
				fmt.Fprintf(os.Stderr, "kb_effectiveness: background save failed: %v\n", err)
			}
		case <-t.stopChan:
			return
		}
	}
}

// KBRanking represents a ranked KB recommendation
type KBRanking struct {
	KBName             string    `json:"kb_name"`
	EffectivenessScore float64   `json:"effectiveness_score"` // 0.0-1.0 with recency weighting
	SuccessRate        float64   `json:"success_rate"`        // Raw success rate
	AvgCompliance      float64   `json:"avg_compliance"`      // Average compliance score
	UsageCount         int       `json:"usage_count"`         // Total usage count
	LastUsed           time.Time `json:"last_used"`           // Most recent usage
}

// Helper functions

func sortKBRankings(rankings []KBRanking) {
	// Simple bubble sort - fine for small lists
	for i := 0; i < len(rankings); i++ {
		for j := i + 1; j < len(rankings); j++ {
			if rankings[j].EffectivenessScore > rankings[i].EffectivenessScore {
				rankings[i], rankings[j] = rankings[j], rankings[i]
			}
		}
	}
}

package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKBEffectiveness_EffectivenessScore(t *testing.T) {
	e := &KBEffectiveness{
		SuccessCount: 8,
		FailureCount: 2,
		RecentSuccess: []bool{true, true, true, false, true},
	}
	
	score := e.EffectivenessScore()
	
	// Base rate: 8/10 = 0.8
	// Recent rate: 4/5 = 0.8
	// Weighted: 0.7*0.8 + 0.3*0.8 = 0.8
	if score < 0.75 || score > 0.85 {
		t.Errorf("expected score around 0.8, got %.2f", score)
	}
}

func TestKBEffectiveness_EffectivenessScore_NoData(t *testing.T) {
	e := &KBEffectiveness{}
	
	score := e.EffectivenessScore()
	
	// No data should return neutral 0.5
	if score != 0.5 {
		t.Errorf("expected 0.5 for no data, got %.2f", score)
	}
}

func TestKBEffectiveness_EffectivenessScore_RecencyBias(t *testing.T) {
	e := &KBEffectiveness{
		SuccessCount:  10,
		FailureCount:  0,
		RecentSuccess: []bool{false, false, false, false, false}, // Recent failures
	}
	
	score := e.EffectivenessScore()
	
	// Base rate: 10/10 = 1.0
	// Recent rate: 0/5 = 0.0
	// Weighted: 0.7*1.0 + 0.3*0.0 = 0.7
	if score < 0.65 || score > 0.75 {
		t.Errorf("expected score around 0.7, got %.2f", score)
	}
}

func TestKBEffectiveness_AverageComplianceScore(t *testing.T) {
	e := &KBEffectiveness{
		SuccessCount: 3,
		FailureCount: 2,
		TotalScore:   4.0, // Average: 4.0 / 5 = 0.8
	}
	
	avg := e.AverageComplianceScore()
	
	if avg != 0.8 {
		t.Errorf("expected 0.8, got %.2f", avg)
	}
}

func TestKBEffectivenessTracker_RecordOutcome(t *testing.T) {
	tracker, err := NewKBEffectivenessTracker("", false) // No persistence
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	
	tracker.RecordOutcome("implementation", "go_best_practices", true, 0.95)
	tracker.RecordOutcome("implementation", "go_best_practices", true, 0.90)
	tracker.RecordOutcome("implementation", "go_best_practices", false, 0.60)
	
	entry := tracker.GetKBEffectiveness("implementation", "go_best_practices")
	if entry == nil {
		t.Fatal("expected entry to exist")
	}
	if entry.SuccessCount != 2 {
		t.Errorf("expected 2 successes, got %d", entry.SuccessCount)
	}
	if entry.FailureCount != 1 {
		t.Errorf("expected 1 failure, got %d", entry.FailureCount)
	}
	if entry.TotalScore != 2.45 {
		t.Errorf("expected total score 2.45, got %.2f", entry.TotalScore)
	}
	if len(entry.RecentSuccess) != 3 {
		t.Errorf("expected 3 recent outcomes, got %d", len(entry.RecentSuccess))
	}
}

func TestKBEffectivenessTracker_RecordOutcome_RecentLimit(t *testing.T) {
	tracker, err := NewKBEffectivenessTracker("", false)
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	
	// Record 15 outcomes (should keep only last 10)
	for i := 0; i < 15; i++ {
		tracker.RecordOutcome("testing", "testing_patterns", true, 1.0)
	}
	
	entry := tracker.GetKBEffectiveness("testing", "testing_patterns")
	if entry == nil {
		t.Fatal("expected entry to exist")
	}
	if len(entry.RecentSuccess) != 10 {
		t.Errorf("expected 10 recent outcomes, got %d", len(entry.RecentSuccess))
	}
}

func TestKBEffectivenessTracker_GetBestKBsForTask(t *testing.T) {
	tracker, err := NewKBEffectivenessTracker("", false)
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	
	// Record outcomes for different KBs
	tracker.RecordOutcome("implementation", "go_best_practices", true, 0.95)
	tracker.RecordOutcome("implementation", "go_best_practices", true, 0.90)
	
	tracker.RecordOutcome("implementation", "architecture_patterns", true, 0.80)
	tracker.RecordOutcome("implementation", "architecture_patterns", false, 0.50)
	
	tracker.RecordOutcome("debugging", "debugging_guide", true, 0.85)
	
	rankings := tracker.GetBestKBsForTask("implementation", 0)
	
	if len(rankings) != 2 {
		t.Fatalf("expected 2 KBs for implementation, got %d", len(rankings))
	}
	
	// First should be go_best_practices (100% success rate)
	if rankings[0].KBName != "go_best_practices" {
		t.Errorf("expected go_best_practices first, got %s", rankings[0].KBName)
	}
	if rankings[0].SuccessRate != 1.0 {
		t.Errorf("expected 1.0 success rate, got %.2f", rankings[0].SuccessRate)
	}
	
	// Second should be architecture_patterns (50% success rate)
	if rankings[1].KBName != "architecture_patterns" {
		t.Errorf("expected architecture_patterns second, got %s", rankings[1].KBName)
	}
}

func TestKBEffectivenessTracker_GetBestKBsForTask_Limit(t *testing.T) {
	tracker, err := NewKBEffectivenessTracker("", false)
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	
	tracker.RecordOutcome("implementation", "kb1", true, 0.9)
	tracker.RecordOutcome("implementation", "kb2", true, 0.8)
	tracker.RecordOutcome("implementation", "kb3", true, 0.7)
	
	rankings := tracker.GetBestKBsForTask("implementation", 2)
	
	if len(rankings) != 2 {
		t.Errorf("expected limit of 2, got %d rankings", len(rankings))
	}
}

func TestKBEffectivenessTracker_GetKBEffectiveness_NotFound(t *testing.T) {
	tracker, err := NewKBEffectivenessTracker("", false)
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	
	entry := tracker.GetKBEffectiveness("implementation", "nonexistent_kb")
	
	if entry != nil {
		t.Error("expected nil for nonexistent KB")
	}
}

func TestKBEffectivenessTracker_GetAllKBs(t *testing.T) {
	tracker, err := NewKBEffectivenessTracker("", false)
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	
	tracker.RecordOutcome("implementation", "go_best_practices", true, 0.9)
	tracker.RecordOutcome("testing", "testing_patterns", true, 0.8)
	tracker.RecordOutcome("implementation", "architecture_patterns", false, 0.5)
	
	kbs := tracker.GetAllKBs()
	
	if len(kbs) != 3 {
		t.Errorf("expected 3 unique KBs, got %d", len(kbs))
	}
}

func TestKBEffectivenessTracker_GetPoorPerformers(t *testing.T) {
	tracker, err := NewKBEffectivenessTracker("", false)
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	
	// Good performer
	for i := 0; i < 10; i++ {
		tracker.RecordOutcome("implementation", "good_kb", true, 0.9)
	}
	
	// Poor performer
	for i := 0; i < 10; i++ {
		tracker.RecordOutcome("implementation", "poor_kb", false, 0.3)
	}
	
	// Insufficient data
	tracker.RecordOutcome("implementation", "new_kb", false, 0.2)
	
	poorPerformers := tracker.GetPoorPerformers(5, 0.3)
	
	if len(poorPerformers) != 1 {
		t.Fatalf("expected 1 poor performer, got %d", len(poorPerformers))
	}
	if poorPerformers[0].KBName != "poor_kb" {
		t.Errorf("expected poor_kb, got %s", poorPerformers[0].KBName)
	}
}

func TestKBEffectivenessTracker_Persistence(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "kb_effectiveness.json")
	
	// Create tracker and record some data
	tracker1, err := NewKBEffectivenessTracker(storePath, false)
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	
	tracker1.RecordOutcome("implementation", "go_best_practices", true, 0.95)
	tracker1.RecordOutcome("implementation", "go_best_practices", true, 0.90)
	
	// Save
	if err := tracker1.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	
	// Create new tracker and load
	tracker2, err := NewKBEffectivenessTracker(storePath, false)
	if err != nil {
		t.Fatalf("create tracker2: %v", err)
	}
	
	entry := tracker2.GetKBEffectiveness("implementation", "go_best_practices")
	if entry == nil {
		t.Fatal("expected data to be loaded")
	}
	if entry.SuccessCount != 2 {
		t.Errorf("expected 2 successes after load, got %d", entry.SuccessCount)
	}
}

func TestKBEffectivenessTracker_AutoSave(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "kb_effectiveness.json")
	
	tracker, err := NewKBEffectivenessTracker(storePath, true)
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	tracker.saveInterval = 100 * time.Millisecond // Fast for testing
	defer tracker.Close()
	
	tracker.RecordOutcome("implementation", "test_kb", true, 0.9)
	
	// Wait for auto-save
	time.Sleep(200 * time.Millisecond)
	
	// Check file exists
	if _, err := os.Stat(storePath); os.IsNotExist(err) {
		t.Error("expected auto-save to create file")
	}
}

func TestKBEffectivenessTracker_ConcurrentAccess(t *testing.T) {
	tracker, err := NewKBEffectivenessTracker("", false)
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	
	// Concurrent writes
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				tracker.RecordOutcome("implementation", "concurrent_kb", true, 0.8)
			}
			done <- true
		}(i)
	}
	
	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
	
	entry := tracker.GetKBEffectiveness("implementation", "concurrent_kb")
	if entry == nil {
		t.Fatal("expected entry to exist")
	}
	if entry.SuccessCount != 1000 {
		t.Errorf("expected 1000 successes, got %d (potential race condition)", entry.SuccessCount)
	}
}

package memory

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestQualityCheckJob_BasicOperation(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForJob(t, tmpDir)

	// Add some test data
	recordOutcomes(tracker, "good-kb", "implementation", []bool{
		true, true, true, true, true,
		true, true, true, true, true,
	})
	recordOutcomes(tracker, "bad-kb", "implementation", []bool{
		false, false, false, false, false,
		false, false, false, false, false,
	})

	// Track notifications
	var notifications []QualityNotification
	var mu sync.Mutex

	config := DefaultQualityCheckConfig()
	config.Interval = 100 * time.Millisecond // Fast for testing
	config.NotifyOnCritical = true
	config.NotifyCallback = func(n QualityNotification) {
		mu.Lock()
		defer mu.Unlock()
		notifications = append(notifications, n)
	}

	job := NewQualityCheckJob(tracker, config)
	job.Start()

	// Wait for at least one check to complete
	time.Sleep(200 * time.Millisecond)

	job.Stop()

	// Verify notifications were sent
	mu.Lock()
	defer mu.Unlock()

	if len(notifications) == 0 {
		t.Error("Expected at least one notification")
	}

	// Check that we got critical notification for bad KB
	foundCritical := false
	for _, n := range notifications {
		if n.Severity == "critical" && n.KBName == "bad-kb" {
			foundCritical = true
		}
	}

	if !foundCritical {
		t.Error("Expected critical notification for bad-kb")
	}
}

func TestQualityCheckJob_RunNow(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForJob(t, tmpDir)

	// Add critical KB
	recordOutcomes(tracker, "critical-kb", "implementation", []bool{
		false, false, false, false, false,
		false, false, false, false, false,
	})

	notified := false
	config := DefaultQualityCheckConfig()
	config.Interval = 1 * time.Hour // Long interval - shouldn't fire during test
	config.NotifyOnCritical = true
	config.NotifyCallback = func(n QualityNotification) {
		if n.KBName == "critical-kb" {
			notified = true
		}
	}

	job := NewQualityCheckJob(tracker, config)
	job.Start()
	defer job.Stop()

	// Trigger manual run
	job.RunNow()

	// Wait for completion
	time.Sleep(100 * time.Millisecond)

	if !notified {
		t.Error("Expected notification from manual run")
	}
}

func TestQualityCheckJob_GetLastReport(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForJob(t, tmpDir)

	// Add some data
	recordOutcomes(tracker, "test-kb", "implementation", []bool{
		true, true, true, true, true,
		false, false, false, false, false,
	})

	config := DefaultQualityCheckConfig()
	config.Interval = 50 * time.Millisecond

	job := NewQualityCheckJob(tracker, config)
	job.Start()

	// Wait for first check
	time.Sleep(100 * time.Millisecond)

	job.Stop()

	report, lastRun := job.GetLastReport()

	if report == nil {
		t.Fatal("Expected report to be available")
	}

	if lastRun.IsZero() {
		t.Error("Expected lastRun to be set")
	}

	if report.TotalKBs == 0 {
		t.Error("Expected report to have KB data")
	}
}

func TestQualityCheckJob_AutoPruning(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForJob(t, tmpDir)

	// Add critical KB that should be auto-pruned
	recordOutcomes(tracker, "auto-prune-kb", "implementation", []bool{
		false, false, false, false, false,
		false, false, false, false, false,
	})

	var prunedNotification *QualityNotification
	config := DefaultQualityCheckConfig()
	config.Interval = 100 * time.Millisecond
	config.AutoPrune = true
	config.NotifyCallback = func(n QualityNotification) {
		if n.Title == "Auto-Pruning Executed" {
			prunedNotification = &n
		}
	}

	job := NewQualityCheckJob(tracker, config)
	job.Start()

	// Wait for check and auto-prune
	time.Sleep(200 * time.Millisecond)

	job.Stop()

	if prunedNotification == nil {
		t.Error("Expected auto-pruning notification")
	}
}

func TestQualityCheckJob_NoNotificationWhenNoIssues(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForJob(t, tmpDir)

	// Add only good KBs
	recordOutcomes(tracker, "excellent-kb", "implementation", []bool{
		true, true, true, true, true,
		true, true, true, true, true,
	})

	notificationCount := 0
	config := DefaultQualityCheckConfig()
	config.Interval = 100 * time.Millisecond
	config.NotifyOnCritical = true
	config.NotifyCallback = func(n QualityNotification) {
		notificationCount++
	}

	job := NewQualityCheckJob(tracker, config)
	job.Start()

	// Wait for check
	time.Sleep(200 * time.Millisecond)

	job.Stop()

	// Should not send notifications when everything is fine
	if notificationCount > 0 {
		t.Errorf("Expected no notifications for healthy KBs, got %d", notificationCount)
	}
}

func TestQualityCheckJob_StopGracefully(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTrackerForJob(t, tmpDir)

	config := DefaultQualityCheckConfig()
	config.Interval = 10 * time.Millisecond

	job := NewQualityCheckJob(tracker, config)
	job.Start()

	// Let it run briefly
	time.Sleep(50 * time.Millisecond)

	// Stop should not hang
	done := make(chan struct{})
	go func() {
		job.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Stop() did not complete within timeout")
	}
}

func TestDefaultQualityCheckConfig(t *testing.T) {
	config := DefaultQualityCheckConfig()

	if config.Interval != 24*time.Hour {
		t.Errorf("Expected interval 24h, got %v", config.Interval)
	}

	if config.AutoPrune {
		t.Error("Expected AutoPrune to be false by default")
	}

	if !config.NotifyOnCritical {
		t.Error("Expected NotifyOnCritical to be true by default")
	}
}

func TestQualityNotification_Fields(t *testing.T) {
	notification := QualityNotification{
		Timestamp:   time.Now(),
		Severity:    "critical",
		Title:       "Test Alert",
		Message:     "Test message",
		KBName:      "test-kb",
		TaskType:    "implementation",
		SuccessRate: 0.15,
		TotalUses:   10,
		Actionable:  true,
	}

	if notification.Severity != "critical" {
		t.Error("Expected severity to be set")
	}

	if notification.KBName != "test-kb" {
		t.Error("Expected KB name to be set")
	}

	if !notification.Actionable {
		t.Error("Expected notification to be actionable")
	}
}

// Helper function to set up test tracker
func setupTestTrackerForJob(t *testing.T, tmpDir string) *KBEffectivenessTracker {
	t.Helper()
	persistPath := filepath.Join(tmpDir, "kb_effectiveness.json")
	tracker, err := NewKBEffectivenessTracker(persistPath, false)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	return tracker
}

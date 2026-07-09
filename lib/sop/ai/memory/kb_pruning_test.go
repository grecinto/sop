package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPruningAnalyzer_Analyze_PoorPerformers(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTracker(t, tmpDir)

	// Add a critical poor performer (< 30% success)
	recordOutcomes(tracker, "poor-kb", "implementation", []bool{
		true, false, false, false, false,
		false, false, false, false, false,
	})

	// Add a warning-level performer (30-50% success)
	recordOutcomes(tracker, "mediocre-kb", "implementation", []bool{
		true, false, true, false, false,
		true, false, false, true, false,
	})

	// Add a good performer (>70% success)
	recordOutcomes(tracker, "good-kb", "implementation", []bool{
		true, true, true, false, true,
		true, true, true, true, false,
	})

	analyzer := NewPruningAnalyzer(tracker, DefaultPruningThresholds())
	report, err := analyzer.Analyze()

	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if len(report.PoorPerformers) < 2 {
		t.Errorf("Expected at least 2 poor performers, got %d", len(report.PoorPerformers))
	}

	// Check critical performer is flagged
	foundCritical := false
	for _, candidate := range report.PoorPerformers {
		if candidate.KBName == "poor-kb" && candidate.Severity == "critical" {
			foundCritical = true
			if candidate.SuccessRate >= 0.30 {
				t.Errorf("Expected success rate < 0.30, got %.2f", candidate.SuccessRate)
			}
		}
	}

	if !foundCritical {
		t.Error("Expected to find critical poor performer 'poor-kb'")
	}

	// Check good performer is in top performers
	foundGood := false
	for _, kb := range report.TopPerformers {
		if kb.KBName == "good-kb" {
			foundGood = true
			if kb.EffectivenessScore() < 0.70 {
				t.Errorf("Expected success rate >= 0.70, got %.2f", kb.EffectivenessScore())
			}
		}
	}

	if !foundGood {
		t.Error("Expected to find top performer 'good-kb'")
	}
}

func TestPruningAnalyzer_Analyze_StaleKBs(t *testing.T) {
	t.Skip("Skipping test that requires manipulating private fields - staleness detection tested in integration")
	// This test would need a SetLastUsed method for testing or mock time
}

func TestPruningAnalyzer_Analyze_MinimumUsesThreshold(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTracker(t, tmpDir)

	// Add KB with only 5 uses (below minimum of 10)
	recordOutcomes(tracker, "new-kb", "implementation", []bool{
		false, false, false, false, false, // 0% success but too few uses
	})

	analyzer := NewPruningAnalyzer(tracker, DefaultPruningThresholds())
	report, err := analyzer.Analyze()

	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// Should not flag KB with insufficient data
	for _, candidate := range report.PoorPerformers {
		if candidate.KBName == "new-kb" {
			t.Error("Should not flag KB with insufficient uses (<10)")
		}
	}
}

func TestPruningAnalyzer_Analyze_RecentPerformanceDecline(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTracker(t, tmpDir)

	// Add KB with strong historical performance
	// First 20 uses: 18 successes = 90%
	for i := 0; i < 18; i++ {
		tracker.RecordOutcome("implementation", "declining-kb", true, 0.9)
	}
	for i := 0; i < 2; i++ {
		tracker.RecordOutcome("implementation", "declining-kb", false, 0.9)
	}

	// Recent 10 uses: 2 successes = 20% (sharp recent decline)
	for i := 0; i < 8; i++ {
		tracker.RecordOutcome("implementation", "declining-kb", false, 0.2)
	}
	for i := 0; i < 2; i++ {
		tracker.RecordOutcome("implementation", "declining-kb", true, 0.2)
	}

	analyzer := NewPruningAnalyzer(tracker, DefaultPruningThresholds())
	report, err := analyzer.Analyze()

	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// Overall: 20 successes in 30 uses = 66.7% base rate
	// Recent: 2 in 10 = 20%
	// Weighted: 0.7*0.667 + 0.3*0.20 = 0.527 = 52.7% (still >= 50%)
	foundDeclining := false
	for _, candidate := range report.PoorPerformers {
		if candidate.KBName == "declining-kb" {
			foundDeclining = true
			if !strings.Contains(candidate.Reason, "Recent performance decline") {
				t.Errorf("Expected 'Recent performance decline' in reason, got: %s", candidate.Reason)
			}
		}
	}

	if !foundDeclining {
		t.Error("Expected to find declining KB 'declining-kb' in poor performers")
	}
}

func TestPruningAnalyzer_CustomThresholds(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTracker(t, tmpDir)

	// Add KB with 40% success rate
	recordOutcomes(tracker, "test-kb", "implementation", []bool{
		true, false, true, false, false,
		true, false, true, false, false,
	})

	// With default threshold (30%), should not be critical
	defaultAnalyzer := NewPruningAnalyzer(tracker, DefaultPruningThresholds())
	defaultReport, _ := defaultAnalyzer.Analyze()

	foundInDefault := false
	for _, candidate := range defaultReport.PoorPerformers {
		if candidate.KBName == "test-kb" && candidate.Severity == "critical" {
			foundInDefault = true
		}
	}

	if foundInDefault {
		t.Error("Should not be critical with default threshold")
	}

	// With stricter threshold (50%), should be critical
	strictThresholds := DefaultPruningThresholds()
	strictThresholds.SuccessRateBelow = 0.50
	strictAnalyzer := NewPruningAnalyzer(tracker, strictThresholds)
	strictReport, _ := strictAnalyzer.Analyze()

	foundInStrict := false
	for _, candidate := range strictReport.PoorPerformers {
		if candidate.KBName == "test-kb" && candidate.Severity == "critical" {
			foundInStrict = true
		}
	}

	if !foundInStrict {
		t.Error("Should be critical with strict threshold")
	}
}

func TestPruningAnalyzer_FormatReport(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTracker(t, tmpDir)

	// Add some test data
	recordOutcomes(tracker, "poor-kb", "implementation", []bool{
		false, false, false, false, false,
		false, false, false, false, false,
	})
	recordOutcomes(tracker, "good-kb", "implementation", []bool{
		true, true, true, true, true,
		true, true, true, true, true,
	})

	analyzer := NewPruningAnalyzer(tracker, DefaultPruningThresholds())
	report, _ := analyzer.Analyze()

	formatted := analyzer.FormatReport(report)

	// Check that report contains expected sections
	if !strings.Contains(formatted, "KB Quality & Pruning Analysis Report") {
		t.Error("Report should contain title")
	}

	if !strings.Contains(formatted, "OVERALL STATISTICS") {
		t.Error("Report should contain statistics section")
	}

	if !strings.Contains(formatted, "POOR PERFORMERS") {
		t.Error("Report should contain poor performers section")
	}

	if !strings.Contains(formatted, "TOP PERFORMERS") {
		t.Error("Report should contain top performers section")
	}

	if !strings.Contains(formatted, "RECOMMENDATIONS") {
		t.Error("Report should contain recommendations section")
	}

	// Check that specific KBs appear
	if !strings.Contains(formatted, "poor-kb") {
		t.Error("Report should mention poor-kb")
	}

	if !strings.Contains(formatted, "good-kb") {
		t.Error("Report should mention good-kb")
	}
}

func TestPruningAnalyzer_ExecutePruning_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTracker(t, tmpDir)

	// Add critical poor performer
	recordOutcomes(tracker, "critical-kb", "implementation", []bool{
		false, false, false, false, false,
		false, false, false, false, false,
	})

	// Add warning-level performer (should not be pruned)
	recordOutcomes(tracker, "warning-kb", "implementation", []bool{
		true, false, true, false, false,
		true, false, false, true, false,
	})

	analyzer := NewPruningAnalyzer(tracker, DefaultPruningThresholds())

	// Dry run
	pruned, err := analyzer.ExecutePruning(true)
	if err != nil {
		t.Fatalf("ExecutePruning failed: %v", err)
	}

	if len(pruned) < 1 {
		t.Error("Expected at least 1 KB to be pruned in dry-run")
	}

	// Check that only critical KB is in pruned list
	foundCritical := false
	foundWarning := false
	for _, kb := range pruned {
		if strings.Contains(kb, "critical-kb") {
			foundCritical = true
		}
		if strings.Contains(kb, "warning-kb") {
			foundWarning = true
		}
	}

	if !foundCritical {
		t.Error("Expected critical-kb in pruned list")
	}

	if foundWarning {
		t.Error("Should not prune warning-level KB (only critical)")
	}
}

func TestPruningAnalyzer_Recommendations(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTracker(t, tmpDir)

	// Add critical poor performer
	recordOutcomes(tracker, "critical-kb", "implementation", []bool{
		false, false, false, false, false,
		false, false, false, false, false,
	})

	analyzer := NewPruningAnalyzer(tracker, DefaultPruningThresholds())
	report, _ := analyzer.Analyze()

	if len(report.Recommendations) == 0 {
		t.Error("Expected at least one recommendation")
	}

	// Check for critical warning in recommendations
	foundCriticalRec := false
	for _, rec := range report.Recommendations {
		if strings.Contains(rec, "critical") || strings.Contains(rec, "⚠️") {
			foundCriticalRec = true
		}
	}

	if !foundCriticalRec {
		t.Error("Expected critical warning in recommendations")
	}
}

func TestPruningAnalyzer_EmptyData(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := setupTestTracker(t, tmpDir)

	// No data added
	analyzer := NewPruningAnalyzer(tracker, DefaultPruningThresholds())
	report, err := analyzer.Analyze()

	if err != nil {
		t.Fatalf("Analyze failed on empty data: %v", err)
	}

	if report.TotalKBs != 0 {
		t.Errorf("Expected 0 total KBs, got %d", report.TotalKBs)
	}

	if len(report.PoorPerformers) != 0 {
		t.Errorf("Expected 0 poor performers, got %d", len(report.PoorPerformers))
	}

	if len(report.Recommendations) == 0 {
		t.Error("Expected at least one recommendation even with empty data")
	}
}

func TestDefaultPruningThresholds(t *testing.T) {
	thresholds := DefaultPruningThresholds()

	if thresholds.MinimumUses != 10 {
		t.Errorf("Expected MinimumUses=10, got %d", thresholds.MinimumUses)
	}

	if thresholds.SuccessRateBelow != 0.30 {
		t.Errorf("Expected SuccessRateBelow=0.30, got %.2f", thresholds.SuccessRateBelow)
	}

	if thresholds.InactivityDays != 90 {
		t.Errorf("Expected InactivityDays=90, got %d", thresholds.InactivityDays)
	}
}

// Helper function to set up a test tracker
func setupTestTracker(t *testing.T, tmpDir string) *KBEffectivenessTracker {
	t.Helper()
	persistPath := filepath.Join(tmpDir, "kb_effectiveness.json")
	tracker, err := NewKBEffectivenessTracker(persistPath, false)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	return tracker
}

// Helper function to record multiple outcomes
func recordOutcomes(tracker *KBEffectivenessTracker, kbName string, taskType string, outcomes []bool) {
	for _, success := range outcomes {
		complianceScore := 0.0
		if success {
			complianceScore = 0.8
		}
		tracker.RecordOutcome(taskType, kbName, success, complianceScore)
	}
}

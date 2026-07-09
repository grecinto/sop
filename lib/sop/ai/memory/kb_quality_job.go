package memory

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// QualityCheckConfig configures the periodic KB quality analysis job.
type QualityCheckConfig struct {
	Interval         time.Duration     // How often to run analysis
	Thresholds       PruningThresholds // Quality thresholds
	AutoPrune        bool              // Whether to auto-prune critical KBs
	NotifyOnCritical bool              // Send notifications for critical issues
	NotifyCallback   NotificationFunc  // Function to call for notifications
}

// NotificationFunc is called when critical KB issues are detected.
type NotificationFunc func(notification QualityNotification)

// QualityNotification represents an alert about KB quality issues.
type QualityNotification struct {
	Timestamp   time.Time
	Severity    string // "critical", "warning", "info"
	Title       string
	Message     string
	KBName      string
	TaskType    string
	SuccessRate float64
	TotalUses   int
	Actionable  bool
	ReportPath  string // Path to full report if saved
}

// DefaultQualityCheckConfig returns sensible defaults for periodic quality checks.
func DefaultQualityCheckConfig() QualityCheckConfig {
	return QualityCheckConfig{
		Interval:         24 * time.Hour, // Daily
		Thresholds:       DefaultPruningThresholds(),
		AutoPrune:        false, // Safety: require manual approval by default
		NotifyOnCritical: true,
		NotifyCallback:   nil,
	}
}

// QualityCheckJob runs periodic KB quality analysis and alerts admins of issues.
type QualityCheckJob struct {
	tracker    *KBEffectivenessTracker
	config     QualityCheckConfig
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.RWMutex
	lastRun    time.Time
	lastReport *PruningReport
}

// NewQualityCheckJob creates a new periodic quality check job.
func NewQualityCheckJob(tracker *KBEffectivenessTracker, config QualityCheckConfig) *QualityCheckJob {
	ctx, cancel := context.WithCancel(context.Background())
	return &QualityCheckJob{
		tracker: tracker,
		config:  config,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start begins the periodic quality check job.
func (j *QualityCheckJob) Start() {
	j.wg.Add(1)
	go j.run()
	log.Printf("[KB Quality Job] Started with interval: %v", j.config.Interval)
}

// Stop gracefully stops the quality check job.
func (j *QualityCheckJob) Stop() {
	j.cancel()
	j.wg.Wait()
	log.Println("[KB Quality Job] Stopped")
}

// run is the main loop that performs periodic quality checks.
func (j *QualityCheckJob) run() {
	defer j.wg.Done()

	ticker := time.NewTicker(j.config.Interval)
	defer ticker.Stop()

	// Run immediately on start
	j.performQualityCheck()

	for {
		select {
		case <-j.ctx.Done():
			return
		case <-ticker.C:
			j.performQualityCheck()
		}
	}
}

// performQualityCheck executes the analysis and takes configured actions.
func (j *QualityCheckJob) performQualityCheck() {
	log.Println("[KB Quality Job] Starting quality check...")

	analyzer := NewPruningAnalyzer(j.tracker, j.config.Thresholds)
	report, err := analyzer.Analyze()
	if err != nil {
		log.Printf("[KB Quality Job] Error during analysis: %v", err)
		return
	}

	// Store report
	j.mu.Lock()
	j.lastRun = time.Now()
	j.lastReport = report
	j.mu.Unlock()

	// Log summary
	log.Printf("[KB Quality Job] Analysis complete: %d total KBs, %d poor performers, %.1f%% avg success rate",
		report.TotalKBs, len(report.PoorPerformers), report.AverageSuccessRate*100)

	// Process critical issues
	criticalCount := 0
	warningCount := 0

	for _, candidate := range report.PoorPerformers {
		if candidate.Severity == "critical" {
			criticalCount++
			if j.config.NotifyOnCritical {
				j.sendNotification(candidate, report)
			}
		} else if candidate.Severity == "warning" {
			warningCount++
		}
	}

	// Auto-prune if enabled and there are critical issues
	if j.config.AutoPrune && criticalCount > 0 {
		pruned, err := analyzer.ExecutePruning(false)
		if err != nil {
			log.Printf("[KB Quality Job] Error during auto-pruning: %v", err)
		} else if len(pruned) > 0 {
			log.Printf("[KB Quality Job] Auto-pruned %d critical KBs: %v", len(pruned), pruned)

			// Send notification about auto-pruning
			if j.config.NotifyCallback != nil {
				j.config.NotifyCallback(QualityNotification{
					Timestamp:  time.Now(),
					Severity:   "info",
					Title:      "Auto-Pruning Executed",
					Message:    fmt.Sprintf("Automatically pruned %d critical KB(s)", len(pruned)),
					Actionable: false,
				})
			}
		}
	}

	// Send summary notification if there are issues
	if (criticalCount > 0 || warningCount > 0) && j.config.NotifyCallback != nil {
		j.sendSummaryNotification(report, criticalCount, warningCount)
	}
}

// sendNotification sends a notification for a specific KB issue.
func (j *QualityCheckJob) sendNotification(candidate PruningCandidate, report *PruningReport) {
	if j.config.NotifyCallback == nil {
		return
	}

	notification := QualityNotification{
		Timestamp:   time.Now(),
		Severity:    candidate.Severity,
		Title:       fmt.Sprintf("KB Quality Alert: %s", candidate.KBName),
		Message:     fmt.Sprintf("%s\n\nRecommendation: %s", candidate.Reason, candidate.Recommendation),
		KBName:      candidate.KBName,
		TaskType:    candidate.TaskType,
		SuccessRate: candidate.SuccessRate,
		TotalUses:   candidate.TotalUses,
		Actionable:  true,
	}

	j.config.NotifyCallback(notification)
}

// sendSummaryNotification sends a summary of all quality issues.
func (j *QualityCheckJob) sendSummaryNotification(report *PruningReport, criticalCount, warningCount int) {
	title := "KB Quality Check Summary"
	message := fmt.Sprintf(
		"Quality check completed at %s\n\n"+
			"Total KBs: %d\n"+
			"Average Success Rate: %.1f%%\n"+
			"Critical Issues: %d\n"+
			"Warnings: %d\n\n"+
			"Review the full report for details.",
		report.GeneratedAt.Format("2006-01-02 15:04:05"),
		report.TotalKBs,
		report.AverageSuccessRate*100,
		criticalCount,
		warningCount,
	)

	severity := "info"
	if criticalCount > 0 {
		severity = "critical"
	} else if warningCount > 0 {
		severity = "warning"
	}

	notification := QualityNotification{
		Timestamp:  time.Now(),
		Severity:   severity,
		Title:      title,
		Message:    message,
		Actionable: criticalCount > 0 || warningCount > 0,
	}

	j.config.NotifyCallback(notification)
}

// GetLastReport returns the most recent quality check report.
func (j *QualityCheckJob) GetLastReport() (*PruningReport, time.Time) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.lastReport, j.lastRun
}

// RunNow triggers an immediate quality check (doesn't affect schedule).
func (j *QualityCheckJob) RunNow() {
	log.Println("[KB Quality Job] Manual quality check triggered")
	go j.performQualityCheck()
}

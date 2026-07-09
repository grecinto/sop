package memory
package memory_test

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/sharedcode/sop/ai/memory"
)

// Example_kbQualityIntegration demonstrates how to integrate all Phase 3 components:
// KB effectiveness tracking, quality analysis, background jobs, and REST API.
func Example_kbQualityIntegration() {
	// 1. Initialize KB effectiveness tracker
	tracker, err := memory.NewKBEffectivenessTracker("/tmp/kb_effectiveness.json", true)
	if err != nil {
		log.Fatal(err)
	}
	defer tracker.Close()
	
	// 2. Record some outcomes (normally done by orchestrator)
	tracker.RecordOutcome("implementation", "go-best-practices", true, 0.85)
	tracker.RecordOutcome("debugging", "error-handling-guide", true, 0.92)
	tracker.RecordOutcome("implementation", "legacy-patterns", false, 0.25)
	
	// 3. Set up quality check job with notifications
	jobConfig := memory.DefaultQualityCheckConfig()
	jobConfig.Interval = 24 * time.Hour // Daily checks
	jobConfig.AutoPrune = false         // Require manual approval
	jobConfig.NotifyOnCritical = true
	jobConfig.NotifyCallback = func(notification memory.QualityNotification) {
		// Send notification to admin (email, Slack, etc.)
		log.Printf("[ALERT] %s: %s", notification.Title, notification.Message)
	}
	
	job := memory.NewQualityCheckJob(tracker, jobConfig)
	job.Start()
	defer job.Stop()
	
	// 4. Set up REST API endpoints
	api := memory.NewKBQualityAPI(tracker, memory.DefaultPruningThresholds())
	api.SetQualityJob(job) // Enable trigger and status endpoints
	
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)
	
	// 5. Start HTTP server
	go func() {
		log.Println("KB Quality API listening on :8080")
		http.ListenAndServe(":8080", mux)
	}()
	
	// API endpoints now available:
	// GET  /api/kb/quality/report           - Full quality report
	// GET  /api/kb/quality/analyze          - Analysis with filtering
	// GET  /api/kb/quality/candidates       - Critical pruning candidates
	// GET  /api/kb/effectiveness/all        - All KB records
	// GET  /api/kb/effectiveness/best       - Top performers
	// GET  /api/kb/effectiveness/poor       - Poor performers
	// GET  /api/kb/effectiveness/kb         - Specific KB details
	// POST /api/kb/quality/trigger          - Trigger manual check
	// GET  /api/kb/quality/status           - Job status
	
	fmt.Println("KB Quality system initialized")
	// Output: KB Quality system initialized
}

// Example_manualQualityAnalysis shows how to perform one-off quality analysis.
func Example_manualQualityAnalysis() {
	tracker, _ := memory.NewKBEffectivenessTracker("/tmp/kb_effectiveness.json", false)
	defer tracker.Close()
	
	// Configure analysis thresholds
	thresholds := memory.PruningThresholds{
		MinimumUses:      10,
		SuccessRateBelow: 0.30,
		InactivityDays:   90,
	}
	
	analyzer := memory.NewPruningAnalyzer(tracker, thresholds)
	
	// Generate report
	report, err := analyzer.Analyze()
	if err != nil {
		log.Fatal(err)
	}
	
	// Display formatted report
	formatted := analyzer.FormatReport(report)
	fmt.Println(formatted)
	
	// Or check for critical issues programmatically
	criticalCount := 0
	for _, candidate := range report.PoorPerformers {
		if candidate.Severity == "critical" {
			criticalCount++
			fmt.Printf("Critical: %s [%s] - %.1f%% success\n",
				candidate.KBName, candidate.TaskType, candidate.SuccessRate*100)
		}
	}
	
	if criticalCount > 0 {
		fmt.Printf("\n%d KB(s) require immediate attention\n", criticalCount)
	}
}

// Example_cliTool demonstrates using the kb-admin CLI tool.
func Example_cliTool() {
	// From command line:
	
	// Generate comprehensive report
	// $ kb-admin report --data /path/to/kb_effectiveness.json
	
	// Quick analysis of critical issues
	// $ kb-admin analyze --threshold 0.30 --min-uses 10
	
	// Dry-run pruning (safe)
	// $ kb-admin prune --dry-run
	
	// Execute pruning after review
	// $ kb-admin prune --execute
	
	// List all KBs in table format
	// $ kb-admin list --format table
	
	// Export data for external analysis
	// $ kb-admin export --output kb_data.json
	
	fmt.Println("See kb-admin help for full command reference")
	// Output: See kb-admin help for full command reference
}

// Example_programmaticPruning shows how to identify and handle poor performers.
func Example_programmaticPruning() {
	tracker, _ := memory.NewKBEffectivenessTracker("/tmp/kb_effectiveness.json", false)
	defer tracker.Close()
	
	// Get poor performers directly
	poorPerformers := tracker.GetPoorPerformers(10, 0.30)
	
	for _, kb := range poorPerformers {
		fmt.Printf("Poor Performer: %s [%s]\n", kb.KBName, kb.TaskType)
		fmt.Printf("  Success: %d | Failure: %d | Rate: %.1f%%\n",
			kb.SuccessCount, kb.FailureCount, kb.EffectivenessScore()*100)
		fmt.Println("  Action: Review and improve content or disable")
	}
	
	// Or use full pruning analysis
	analyzer := memory.NewPruningAnalyzer(tracker, memory.DefaultPruningThresholds())
	
	// Dry-run to see what would be pruned
	pruned, err := analyzer.ExecutePruning(true)
	if err != nil {
		log.Fatal(err)
	}
	
	fmt.Printf("\nWould prune %d KB(s): %v\n", len(pruned), pruned)
}

// Example_apiUsage demonstrates consuming the REST API.
func Example_apiUsage() {
	// From any HTTP client:
	
	// Get comprehensive quality report (JSON)
	// GET /api/kb/quality/report
	// Response: { generated_at, total_kbs, poor_performers, top_performers, ... }
	
	// Get only critical issues
	// GET /api/kb/quality/analyze?severity=critical
	// Response: { poor_performers: [...], recommendations: [...] }
	
	// Get best KBs for implementation tasks
	// GET /api/kb/effectiveness/best?task_type=implementation&limit=5
	// Response: [{ kb_name, score, success_count, ... }]
	
	// Get details for specific KB
	// GET /api/kb/effectiveness/kb?task_type=implementation&kb_name=go-best-practices
	// Response: { kb_name, success_count, failure_count, effectiveness_score, ... }
	
	// Trigger manual quality check
	// POST /api/kb/quality/trigger
	// Response: { status: "triggered", message: "Quality check initiated" }
	
	// Check job status
	// GET /api/kb/quality/status
	// Response: { last_run, next_run, last_report: {...} }
	
	fmt.Println("See API documentation for full endpoint reference")
	// Output: See API documentation for full endpoint reference
}

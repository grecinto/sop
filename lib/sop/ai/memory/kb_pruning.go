package memory

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// PruningThresholds defines criteria for identifying poor-performing KBs.
type PruningThresholds struct {
	MinimumUses      int     // Minimum number of uses before considering for pruning
	SuccessRateBelow float64 // Success rate threshold (e.g., 0.30 = 30%)
	InactivityDays   int     // Days of inactivity before considering stale
	RecentUsesWindow int     // Number of recent uses to consider (0 = use all history)
}

// DefaultPruningThresholds returns sensible defaults for pruning analysis.
func DefaultPruningThresholds() PruningThresholds {
	return PruningThresholds{
		MinimumUses:      10,   // Need at least 10 uses for statistical significance
		SuccessRateBelow: 0.30, // Flag KBs with <30% success rate
		InactivityDays:   90,   // Flag KBs unused for 90+ days
		RecentUsesWindow: 0,    // Consider all history by default
	}
}

// PruningCandidate represents a KB that should be reviewed or pruned.
type PruningCandidate struct {
	KBName         string
	TaskType       string // Task type as string for reporting
	SuccessRate    float64
	TotalUses      int
	DaysSinceUse   int
	Reason         string
	Severity       string // "critical", "warning", "info"
	Recommendation string
}

// PruningReport contains analysis results and recommendations.
type PruningReport struct {
	GeneratedAt        time.Time
	TotalKBs           int
	ActiveKBs          int
	StaleKBs           int
	PoorPerformers     []PruningCandidate
	TopPerformers      []KBEffectiveness
	AverageSuccessRate float64
	Recommendations    []string
}

// PruningAnalyzer analyzes KB effectiveness data and identifies pruning candidates.
type PruningAnalyzer struct {
	Tracker    *KBEffectivenessTracker
	Thresholds PruningThresholds
}

// NewPruningAnalyzer creates a new analyzer with the given tracker and thresholds.
func NewPruningAnalyzer(tracker *KBEffectivenessTracker, thresholds PruningThresholds) *PruningAnalyzer {
	return &PruningAnalyzer{
		Tracker:    tracker,
		Thresholds: thresholds,
	}
}

// Analyze performs comprehensive analysis and generates a pruning report.
func (p *PruningAnalyzer) Analyze() (*PruningReport, error) {
	allRecords := p.Tracker.GetAllRecords()

	report := &PruningReport{
		GeneratedAt:     time.Now(),
		TotalKBs:        len(allRecords),
		PoorPerformers:  []PruningCandidate{},
		TopPerformers:   []KBEffectiveness{},
		Recommendations: []string{},
	}

	var totalSuccessRate float64
	activeCount := 0
	staleCount := 0

	// Analyze each KB
	for _, record := range allRecords {
		totalUses := record.SuccessCount + record.FailureCount

		// Skip if not enough data
		if totalUses < p.Thresholds.MinimumUses {
			continue
		}

		daysSinceUse := int(time.Since(record.LastUsed).Hours() / 24)
		successRate := record.EffectivenessScore()

		// Track statistics
		totalSuccessRate += successRate

		if daysSinceUse <= p.Thresholds.InactivityDays {
			activeCount++
		} else {
			staleCount++
		}

		// Identify poor performers
		candidate := p.evaluateRecord(record, daysSinceUse)
		if candidate != nil {
			report.PoorPerformers = append(report.PoorPerformers, *candidate)
		}

		// Track top performers
		if successRate >= 0.70 && totalUses >= p.Thresholds.MinimumUses {
			report.TopPerformers = append(report.TopPerformers, record)
		}
	}

	report.ActiveKBs = activeCount
	report.StaleKBs = staleCount

	if len(allRecords) > 0 {
		report.AverageSuccessRate = totalSuccessRate / float64(len(allRecords))
	}

	// Sort poor performers by severity and success rate
	sort.Slice(report.PoorPerformers, func(i, j int) bool {
		if report.PoorPerformers[i].Severity != report.PoorPerformers[j].Severity {
			return severityRank(report.PoorPerformers[i].Severity) > severityRank(report.PoorPerformers[j].Severity)
		}
		return report.PoorPerformers[i].SuccessRate < report.PoorPerformers[j].SuccessRate
	})

	// Sort top performers by success rate
	sort.Slice(report.TopPerformers, func(i, j int) bool {
		return report.TopPerformers[i].EffectivenessScore() > report.TopPerformers[j].EffectivenessScore()
	})

	// Generate recommendations
	report.Recommendations = p.generateRecommendations(report)

	return report, nil
}

// evaluateRecord evaluates a single KB record and returns a pruning candidate if applicable.
func (p *PruningAnalyzer) evaluateRecord(record KBEffectiveness, daysSinceUse int) *PruningCandidate {
	totalUses := record.SuccessCount + record.FailureCount
	successRate := record.EffectivenessScore()

	var reason string
	var severity string
	var recommendation string

	// Check for poor success rate
	if successRate < p.Thresholds.SuccessRateBelow {
		severity = "critical"
		reason = fmt.Sprintf("Very low success rate (%.1f%%) after %d uses", successRate*100, totalUses)
		recommendation = "Consider disabling or reviewing KB content quality"
	} else if successRate < 0.50 {
		severity = "warning"
		reason = fmt.Sprintf("Below average success rate (%.1f%%) after %d uses", successRate*100, totalUses)
		recommendation = "Review KB content and consider improvements"
	}

	// Check for staleness
	if daysSinceUse > p.Thresholds.InactivityDays {
		if severity == "" {
			severity = "info"
		}
		reason += fmt.Sprintf(" | Unused for %d days", daysSinceUse)
		if recommendation == "" {
			recommendation = "Consider archiving or updating KB"
		}
	}

	// Check recent performance decline
	if len(record.RecentSuccess) >= 5 {
		recentSuccessCount := 0
		for _, success := range record.RecentSuccess {
			if success {
				recentSuccessCount++
			}
		}
		recentRate := float64(recentSuccessCount) / float64(len(record.RecentSuccess))

		if recentRate < 0.30 && successRate >= 0.50 {
			severity = "warning"
			reason += fmt.Sprintf(" | Recent performance decline (%.1f%% recent vs %.1f%% overall)", recentRate*100, successRate*100)
			recommendation = "Recent quality issues detected - investigate recent changes"
		}
	}

	if severity != "" {
		return &PruningCandidate{
			KBName:         record.KBName,
			TaskType:       record.TaskType,
			SuccessRate:    successRate,
			TotalUses:      totalUses,
			DaysSinceUse:   daysSinceUse,
			Reason:         reason,
			Severity:       severity,
			Recommendation: recommendation,
		}
	}

	return nil
}

// generateRecommendations creates actionable recommendations based on report data.
func (p *PruningAnalyzer) generateRecommendations(report *PruningReport) []string {
	recommendations := []string{}

	criticalCount := 0
	warningCount := 0

	for _, candidate := range report.PoorPerformers {
		if candidate.Severity == "critical" {
			criticalCount++
		} else if candidate.Severity == "warning" {
			warningCount++
		}
	}

	if criticalCount > 0 {
		recommendations = append(recommendations,
			fmt.Sprintf("⚠️  %d critical KB(s) require immediate attention (<%d%% success rate)",
				criticalCount, int(p.Thresholds.SuccessRateBelow*100)))
	}

	if warningCount > 0 {
		recommendations = append(recommendations,
			fmt.Sprintf("⚡ %d KB(s) show below-average performance and should be reviewed", warningCount))
	}

	if report.StaleKBs > 0 {
		recommendations = append(recommendations,
			fmt.Sprintf("📦 %d KB(s) haven't been used in %d+ days - consider archiving",
				report.StaleKBs, p.Thresholds.InactivityDays))
	}

	if len(report.TopPerformers) > 0 {
		recommendations = append(recommendations,
			fmt.Sprintf("✨ %d KB(s) performing excellently (>70%% success) - use as quality benchmarks",
				len(report.TopPerformers)))
	}

	if report.AverageSuccessRate < 0.60 {
		recommendations = append(recommendations,
			fmt.Sprintf("📊 Overall success rate (%.1f%%) is below target - review KB quality standards",
				report.AverageSuccessRate*100))
	}

	if len(recommendations) == 0 {
		recommendations = append(recommendations, "✓ No immediate actions required - KB quality is healthy")
	}

	return recommendations
}

// FormatReport generates a human-readable text report.
func (p *PruningAnalyzer) FormatReport(report *PruningReport) string {
	var sb strings.Builder

	sb.WriteString("═══════════════════════════════════════════════════════\n")
	sb.WriteString("        KB Quality & Pruning Analysis Report\n")
	sb.WriteString("═══════════════════════════════════════════════════════\n\n")

	sb.WriteString(fmt.Sprintf("Generated: %s\n\n", report.GeneratedAt.Format(time.RFC3339)))

	sb.WriteString("📊 OVERALL STATISTICS\n")
	sb.WriteString("───────────────────────────────────────────────────────\n")
	sb.WriteString(fmt.Sprintf("Total KBs Analyzed:    %d\n", report.TotalKBs))
	sb.WriteString(fmt.Sprintf("Active KBs:            %d\n", report.ActiveKBs))
	sb.WriteString(fmt.Sprintf("Stale KBs:             %d\n", report.StaleKBs))
	sb.WriteString(fmt.Sprintf("Average Success Rate:  %.1f%%\n\n", report.AverageSuccessRate*100))

	if len(report.PoorPerformers) > 0 {
		sb.WriteString("⚠️  POOR PERFORMERS (PRUNING CANDIDATES)\n")
		sb.WriteString("───────────────────────────────────────────────────────\n")
		for _, candidate := range report.PoorPerformers {
			severityIcon := "ℹ️ "
			if candidate.Severity == "critical" {
				severityIcon = "❌"
			} else if candidate.Severity == "warning" {
				severityIcon = "⚠️ "
			}

			sb.WriteString(fmt.Sprintf("\n%s %s [%s]\n", severityIcon, candidate.KBName, candidate.TaskType))
			sb.WriteString(fmt.Sprintf("   Success Rate: %.1f%% (%d uses)\n", candidate.SuccessRate*100, candidate.TotalUses))
			sb.WriteString(fmt.Sprintf("   Issue: %s\n", candidate.Reason))
			sb.WriteString(fmt.Sprintf("   Action: %s\n", candidate.Recommendation))
		}
		sb.WriteString("\n")
	}

	if len(report.TopPerformers) > 0 {
		sb.WriteString("✨ TOP PERFORMERS\n")
		sb.WriteString("───────────────────────────────────────────────────────\n")
		displayCount := 5
		if len(report.TopPerformers) < displayCount {
			displayCount = len(report.TopPerformers)
		}
		for i := 0; i < displayCount; i++ {
			kb := report.TopPerformers[i]
			totalUses := kb.SuccessCount + kb.FailureCount
			sb.WriteString(fmt.Sprintf("%d. %s [%s]: %.1f%% (%d uses)\n",
				i+1, kb.KBName, kb.TaskType, kb.EffectivenessScore()*100, totalUses))
		}
		sb.WriteString("\n")
	}

	if len(report.Recommendations) > 0 {
		sb.WriteString("💡 RECOMMENDATIONS\n")
		sb.WriteString("───────────────────────────────────────────────────────\n")
		for _, rec := range report.Recommendations {
			sb.WriteString(fmt.Sprintf("%s\n", rec))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("═══════════════════════════════════════════════════════\n")

	return sb.String()
}

// ExecutePruning marks poor-performing KBs for pruning (dry-run or actual).
// In dry-run mode, returns what would be pruned without making changes.
func (p *PruningAnalyzer) ExecutePruning(dryRun bool) ([]string, error) {
	report, err := p.Analyze()
	if err != nil {
		return nil, err
	}

	prunedKBs := []string{}

	// Only prune critical candidates
	for _, candidate := range report.PoorPerformers {
		if candidate.Severity == "critical" {
			kbIdentifier := fmt.Sprintf("%s [%s]", candidate.KBName, candidate.TaskType)
			prunedKBs = append(prunedKBs, kbIdentifier)

			if !dryRun {
				// In actual execution, we'd mark the KB as disabled or flagged
				// For now, we just log it since we don't have a disabled flag in KBEffectiveness
				// In a real implementation, you'd add a "Disabled" or "UnderReview" field
				// to KBEffectiveness and set it here
			}
		}
	}

	return prunedKBs, nil
}

// severityRank returns numeric rank for sorting (higher = more severe).
func severityRank(severity string) int {
	switch severity {
	case "critical":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

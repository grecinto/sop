package metrics

import (
	"sync"
	"time"
)

// PhaseAwareContextMetrics tracks optimization effectiveness
// Added Aug 31, 2026 for phase-aware context generation validation
type PhaseAwareContextMetrics struct {
	mu sync.Mutex

	// Context Size Metrics
	InvestigationContextSizes []int
	ExecutionContextSizes     []int
	OtherPhaseContextSizes    []int

	// Execution Phase Metrics
	ExecutionIterationsBeforePatch  []int // Iterations before preview_patch call
	ExecutionPatchGenerationSuccess int   // Count of successful patch generations
	ExecutionPatchGenerationFailure int   // Count of failed attempts

	// Failure Tracking Metrics
	FailureClusterProgressions  []FailureClusterProgression // Cluster-by-cluster progress
	AverageIterationsPerCluster float64

	// Budget Metrics
	ContextTrimmedInvestigation int // How many times investigation context was trimmed
	ContextTrimmedExecution     int // How many times execution context was trimmed

	// Timing Metrics
	SessionStartTime time.Time
	PhaseTransitions []PhaseTransition
}

type FailureClusterProgression struct {
	Cluster          string
	IterationsToFix  int
	PatchesAttempted int
	TestRunsRequired int
	Success          bool
	TimeToFix        time.Duration
}

type PhaseTransition struct {
	FromPhase string
	ToPhase   string
	Timestamp time.Time
	Reason    string // "success", "manual_override", "failure"
}

var GlobalMetrics = &PhaseAwareContextMetrics{
	SessionStartTime: time.Now(),
}

// RecordContextSize tracks context size by phase
func (m *PhaseAwareContextMetrics) RecordContextSize(phase string, size int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch phase {
	case "investigation":
		m.InvestigationContextSizes = append(m.InvestigationContextSizes, size)
	case "execution":
		m.ExecutionContextSizes = append(m.ExecutionContextSizes, size)
	default:
		m.OtherPhaseContextSizes = append(m.OtherPhaseContextSizes, size)
	}
}

// RecordPatchGeneration tracks when patches are generated
func (m *PhaseAwareContextMetrics) RecordPatchGeneration(iteration int, success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ExecutionIterationsBeforePatch = append(m.ExecutionIterationsBeforePatch, iteration)
	if success {
		m.ExecutionPatchGenerationSuccess++
	} else {
		m.ExecutionPatchGenerationFailure++
	}
}

// RecordClusterProgression tracks individual cluster fix progress
func (m *PhaseAwareContextMetrics) RecordClusterProgression(prog FailureClusterProgression) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.FailureClusterProgressions = append(m.FailureClusterProgressions, prog)

	// Recalculate average
	if len(m.FailureClusterProgressions) > 0 {
		total := 0
		for _, p := range m.FailureClusterProgressions {
			total += p.IterationsToFix
		}
		m.AverageIterationsPerCluster = float64(total) / float64(len(m.FailureClusterProgressions))
	}
}

// RecordContextTrim tracks budget trimming events
func (m *PhaseAwareContextMetrics) RecordContextTrim(phase string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if phase == "investigation" {
		m.ContextTrimmedInvestigation++
	} else if phase == "execution" {
		m.ContextTrimmedExecution++
	}
}

// RecordPhaseTransition tracks phase changes
func (m *PhaseAwareContextMetrics) RecordPhaseTransition(from, to, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.PhaseTransitions = append(m.PhaseTransitions, PhaseTransition{
		FromPhase: from,
		ToPhase:   to,
		Timestamp: time.Now(),
		Reason:    reason,
	})
}

// GetSummary returns a human-readable metrics summary
func (m *PhaseAwareContextMetrics) GetSummary() map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()

	avgInvestigation := 0
	if len(m.InvestigationContextSizes) > 0 {
		sum := 0
		for _, s := range m.InvestigationContextSizes {
			sum += s
		}
		avgInvestigation = sum / len(m.InvestigationContextSizes)
	}

	avgExecution := 0
	if len(m.ExecutionContextSizes) > 0 {
		sum := 0
		for _, s := range m.ExecutionContextSizes {
			sum += s
		}
		avgExecution = sum / len(m.ExecutionContextSizes)
	}

	avgIterationsBeforePatch := 0.0
	if len(m.ExecutionIterationsBeforePatch) > 0 {
		sum := 0
		for _, i := range m.ExecutionIterationsBeforePatch {
			sum += i
		}
		avgIterationsBeforePatch = float64(sum) / float64(len(m.ExecutionIterationsBeforePatch))
	}

	return map[string]interface{}{
		"context_sizes": map[string]interface{}{
			"investigation_avg":   avgInvestigation,
			"investigation_count": len(m.InvestigationContextSizes),
			"execution_avg":       avgExecution,
			"execution_count":     len(m.ExecutionContextSizes),
			"size_reduction_percent": func() float64 {
				if avgInvestigation == 0 {
					return 0
				}
				return (float64(avgInvestigation-avgExecution) / float64(avgInvestigation)) * 100
			}(),
		},
		"patch_generation": map[string]interface{}{
			"avg_iterations_before_patch": avgIterationsBeforePatch,
			"success_count":               m.ExecutionPatchGenerationSuccess,
			"failure_count":               m.ExecutionPatchGenerationFailure,
			"success_rate": func() float64 {
				total := m.ExecutionPatchGenerationSuccess + m.ExecutionPatchGenerationFailure
				if total == 0 {
					return 0
				}
				return (float64(m.ExecutionPatchGenerationSuccess) / float64(total)) * 100
			}(),
		},
		"failure_clusters": map[string]interface{}{
			"total_fixed":                len(m.FailureClusterProgressions),
			"avg_iterations_per_cluster": m.AverageIterationsPerCluster,
			"progressions":               m.FailureClusterProgressions,
		},
		"budget_trimming": map[string]interface{}{
			"investigation_trims": m.ContextTrimmedInvestigation,
			"execution_trims":     m.ContextTrimmedExecution,
		},
		"phase_transitions": len(m.PhaseTransitions),
		"session_duration":  time.Since(m.SessionStartTime).String(),
	}
}

// ResetMetrics clears all collected metrics (for testing)
func ResetMetrics() {
	GlobalMetrics.mu.Lock()
	defer GlobalMetrics.mu.Unlock()

	GlobalMetrics = &PhaseAwareContextMetrics{
		SessionStartTime: time.Now(),
	}
}

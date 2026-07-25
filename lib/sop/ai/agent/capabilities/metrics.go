package capabilities

import (
	"sync"
	"time"
)

// CapabilityMetrics captures performance and usage metrics for a capability
type CapabilityMetrics struct {
	SelectionCount       int64
	ExecutionCount       int64
	ErrorCount           int64
	ApprovalCount        int64
	ApprovalDeniedCount  int64
	TotalExecutionTimeMs float64
	AvgExecutionTimeMs   float64
	MinExecutionTimeMs   float64
	MaxExecutionTimeMs   float64
	LastExecuted         time.Time
	LastError            error
	LastErrorTime        time.Time
}

// MetricsCollector collects and aggregates capability metrics
type MetricsCollector struct {
	mu      sync.RWMutex
	metrics map[string]*CapabilityMetrics
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		metrics: make(map[string]*CapabilityMetrics),
	}
}

// RecordSelection records a capability selection event
func (mc *MetricsCollector) RecordSelection(capabilityID string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	m := mc.getOrCreateMetrics(capabilityID)
	m.SelectionCount++
}

// RecordExecution records a capability execution with duration
func (mc *MetricsCollector) RecordExecution(capabilityID string, durationMs float64) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	m := mc.getOrCreateMetrics(capabilityID)
	m.ExecutionCount++
	m.TotalExecutionTimeMs += durationMs
	m.AvgExecutionTimeMs = m.TotalExecutionTimeMs / float64(m.ExecutionCount)
	m.LastExecuted = time.Now()

	// Update min/max
	if m.MinExecutionTimeMs == 0 || durationMs < m.MinExecutionTimeMs {
		m.MinExecutionTimeMs = durationMs
	}
	if durationMs > m.MaxExecutionTimeMs {
		m.MaxExecutionTimeMs = durationMs
	}
}

// RecordError records a capability error
func (mc *MetricsCollector) RecordError(capabilityID string, err error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	m := mc.getOrCreateMetrics(capabilityID)
	m.ErrorCount++
	m.LastError = err
	m.LastErrorTime = time.Now()
}

// RecordApproval records an approval event (approved or denied)
func (mc *MetricsCollector) RecordApproval(capabilityID string, approved bool) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	m := mc.getOrCreateMetrics(capabilityID)
	if approved {
		m.ApprovalCount++
	} else {
		m.ApprovalDeniedCount++
	}
}

// GetMetrics returns metrics for a specific capability
func (mc *MetricsCollector) GetMetrics(capabilityID string) CapabilityMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if m, ok := mc.metrics[capabilityID]; ok {
		return *m
	}
	return CapabilityMetrics{}
}

// GetAllMetrics returns metrics for all capabilities
func (mc *MetricsCollector) GetAllMetrics() map[string]CapabilityMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	result := make(map[string]CapabilityMetrics)
	for id, m := range mc.metrics {
		result[id] = *m
	}
	return result
}

// Reset resets metrics for a specific capability
func (mc *MetricsCollector) Reset(capabilityID string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	delete(mc.metrics, capabilityID)
}

// ResetAll resets all metrics
func (mc *MetricsCollector) ResetAll() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.metrics = make(map[string]*CapabilityMetrics)
}

// getOrCreateMetrics gets existing metrics or creates new ones
func (mc *MetricsCollector) getOrCreateMetrics(capabilityID string) *CapabilityMetrics {
	if m, ok := mc.metrics[capabilityID]; ok {
		return m
	}
	m := &CapabilityMetrics{}
	mc.metrics[capabilityID] = m
	return m
}

// GetSuccessRate returns the success rate for a capability (1.0 - error_rate)
func (mc *MetricsCollector) GetSuccessRate(capabilityID string) float64 {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if m, ok := mc.metrics[capabilityID]; ok {
		if m.ExecutionCount == 0 {
			return 0.0
		}
		return 1.0 - (float64(m.ErrorCount) / float64(m.ExecutionCount))
	}
	return 0.0
}

// GetApprovalRate returns the approval rate for a capability
func (mc *MetricsCollector) GetApprovalRate(capabilityID string) float64 {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if m, ok := mc.metrics[capabilityID]; ok {
		total := m.ApprovalCount + m.ApprovalDeniedCount
		if total == 0 {
			return 0.0
		}
		return float64(m.ApprovalCount) / float64(total)
	}
	return 0.0
}

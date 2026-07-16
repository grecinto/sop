# KB Quality Observability - Prometheus & Grafana

**Purpose:** Monitor KB Quality system health, performance, and effectiveness in production.

---

## Architecture Overview

```
┌─────────────┐     ┌──────────────┐     ┌─────────────┐
│   sop-      │────>│  Prometheus  │────>│  Grafana    │
│   daemon    │     │   Scraper    │     │  Dashboard  │
└─────────────┘     └──────────────┘     └─────────────┘
       │                    │                     │
       │ /metrics          │ Query               │ Visualize
       │                    │                     │
       v                    v                     v
  Expose Metrics      Store Time-Series    Display Charts
```

---

## Prometheus Metrics

### Metrics Exporter Implementation

**File:** `lib/sop/ai/memory/kb_quality_metrics.go` (NEW)

```go
package memory

import (
	"sync"
	"time"
)

// KBQualityMetrics collects metrics for Prometheus export
type KBQualityMetrics struct {
	mu sync.RWMutex
	
	// Validation metrics
	validationTotal         int64
	validationPassTotal     int64
	validationFailTotal     int64
	validationDurationTotal float64 // milliseconds
	
	// Revision metrics
	revisionTotal     int64
	revisionSuccess   int64
	revisionFailed    int64
	
	// Task classification metrics
	taskClassificationTotal   int64
	taskClassificationByType  map[string]int64 // implementation, debugging, testing, etc.
	
	// KB selection metrics
	kbSelectionTotal        int64
	kbSelectionByKB         map[string]int64 // count per KB
	kbSelectionDuration     float64          // milliseconds
	
	// Effectiveness metrics
	feedbackTotal           int64
	feedbackPositiveTotal   int64
	feedbackNegativeTotal   int64
	feedbackByKB            map[string]*KBFeedbackMetrics
	
	// Background job metrics
	qualityCheckRunTotal    int64
	qualityCheckDuration    float64  // seconds
	qualityCheckLastRun     int64    // unix timestamp
	
	// Error metrics
	errorTotal              int64
	errorByType             map[string]int64
}

type KBFeedbackMetrics struct {
	Positive int64
	Negative int64
	Total    int64
}

// NewKBQualityMetrics creates a new metrics collector
func NewKBQualityMetrics() *KBQualityMetrics {
	return &KBQualityMetrics{
		taskClassificationByType: make(map[string]int64),
		kbSelectionByKB:          make(map[string]int64),
		feedbackByKB:             make(map[string]*KBFeedbackMetrics),
		errorByType:              make(map[string]int64),
	}
}

// RecordValidation records a validation outcome
func (m *KBQualityMetrics) RecordValidation(passed bool, durationMs float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.validationTotal++
	if passed {
		m.validationPassTotal++
	} else {
		m.validationFailTotal++
	}
	m.validationDurationTotal += durationMs
}

// RecordRevision records a revision attempt
func (m *KBQualityMetrics) RecordRevision(success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.revisionTotal++
	if success {
		m.revisionSuccess++
	} else {
		m.revisionFailed++
	}
}

// RecordTaskClassification records a task classification
func (m *KBQualityMetrics) RecordTaskClassification(taskType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.taskClassificationTotal++
	m.taskClassificationByType[taskType]++
}

// RecordKBSelection records a KB selection
func (m *KBQualityMetrics) RecordKBSelection(kbName string, durationMs float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.kbSelectionTotal++
	m.kbSelectionByKB[kbName]++
	m.kbSelectionDuration += durationMs
}

// RecordFeedback records user feedback
func (m *KBQualityMetrics) RecordFeedback(kbName string, positive bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.feedbackTotal++
	if positive {
		m.feedbackPositiveTotal++
	} else {
		m.feedbackNegativeTotal++
	}
	
	if m.feedbackByKB[kbName] == nil {
		m.feedbackByKB[kbName] = &KBFeedbackMetrics{}
	}
	m.feedbackByKB[kbName].Total++
	if positive {
		m.feedbackByKB[kbName].Positive++
	} else {
		m.feedbackByKB[kbName].Negative++
	}
}

// RecordQualityCheck records a quality check job run
func (m *KBQualityMetrics) RecordQualityCheck(durationSec float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.qualityCheckRunTotal++
	m.qualityCheckDuration = durationSec
	m.qualityCheckLastRun = time.Now().Unix()
}

// RecordError records an error
func (m *KBQualityMetrics) RecordError(errorType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.errorTotal++
	m.errorByType[errorType]++
}

// ExportPrometheus exports metrics in Prometheus text format
func (m *KBQualityMetrics) ExportPrometheus() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	var out string
	
	// Validation metrics
	out += "# HELP kb_quality_validation_total Total number of validations performed\n"
	out += "# TYPE kb_quality_validation_total counter\n"
	out += fmt.Sprintf("kb_quality_validation_total %d\n", m.validationTotal)
	
	out += "# HELP kb_quality_validation_pass_total Total number of validations that passed\n"
	out += "# TYPE kb_quality_validation_pass_total counter\n"
	out += fmt.Sprintf("kb_quality_validation_pass_total %d\n", m.validationPassTotal)
	
	out += "# HELP kb_quality_validation_fail_total Total number of validations that failed\n"
	out += "# TYPE kb_quality_validation_fail_total counter\n"
	out += fmt.Sprintf("kb_quality_validation_fail_total %d\n", m.validationFailTotal)
	
	avgDuration := 0.0
	if m.validationTotal > 0 {
		avgDuration = m.validationDurationTotal / float64(m.validationTotal)
	}
	out += "# HELP kb_quality_validation_duration_avg Average validation duration in milliseconds\n"
	out += "# TYPE kb_quality_validation_duration_avg gauge\n"
	out += fmt.Sprintf("kb_quality_validation_duration_avg %.2f\n", avgDuration)
	
	// Revision metrics
	out += "# HELP kb_quality_revision_total Total number of revision attempts\n"
	out += "# TYPE kb_quality_revision_total counter\n"
	out += fmt.Sprintf("kb_quality_revision_total %d\n", m.revisionTotal)
	
	out += "# HELP kb_quality_revision_success_total Total number of successful revisions\n"
	out += "# TYPE kb_quality_revision_success_total counter\n"
	out += fmt.Sprintf("kb_quality_revision_success_total %d\n", m.revisionSuccess)
	
	// Task classification metrics
	out += "# HELP kb_quality_task_classification_total Total number of task classifications\n"
	out += "# TYPE kb_quality_task_classification_total counter\n"
	out += fmt.Sprintf("kb_quality_task_classification_total %d\n", m.taskClassificationTotal)
	
	out += "# HELP kb_quality_task_classification_by_type Task classifications by type\n"
	out += "# TYPE kb_quality_task_classification_by_type counter\n"
	for taskType, count := range m.taskClassificationByType {
		out += fmt.Sprintf("kb_quality_task_classification_by_type{task_type=\"%s\"} %d\n", taskType, count)
	}
	
	// KB selection metrics
	out += "# HELP kb_quality_kb_selection_total Total number of KB selections\n"
	out += "# TYPE kb_quality_kb_selection_total counter\n"
	out += fmt.Sprintf("kb_quality_kb_selection_total %d\n", m.kbSelectionTotal)
	
	out += "# HELP kb_quality_kb_selection_by_kb KB selections by KB name\n"
	out += "# TYPE kb_quality_kb_selection_by_kb counter\n"
	for kbName, count := range m.kbSelectionByKB {
		out += fmt.Sprintf("kb_quality_kb_selection_by_kb{kb_name=\"%s\"} %d\n", kbName, count)
	}
	
	// Feedback metrics
	out += "# HELP kb_quality_feedback_total Total user feedback submissions\n"
	out += "# TYPE kb_quality_feedback_total counter\n"
	out += fmt.Sprintf("kb_quality_feedback_total %d\n", m.feedbackTotal)
	
	out += "# HELP kb_quality_feedback_positive_total Total positive feedback\n"
	out += "# TYPE kb_quality_feedback_positive_total counter\n"
	out += fmt.Sprintf("kb_quality_feedback_positive_total %d\n", m.feedbackPositiveTotal)
	
	out += "# HELP kb_quality_feedback_negative_total Total negative feedback\n"
	out += "# TYPE kb_quality_feedback_negative_total counter\n"
	out += fmt.Sprintf("kb_quality_feedback_negative_total %d\n", m.feedbackNegativeTotal)
	
	feedbackRate := 0.0
	if m.feedbackTotal > 0 {
		feedbackRate = float64(m.feedbackPositiveTotal) / float64(m.feedbackTotal) * 100
	}
	out += "# HELP kb_quality_feedback_positive_rate Positive feedback rate (percentage)\n"
	out += "# TYPE kb_quality_feedback_positive_rate gauge\n"
	out += fmt.Sprintf("kb_quality_feedback_positive_rate %.2f\n", feedbackRate)
	
	out += "# HELP kb_quality_feedback_by_kb Feedback by KB name\n"
	out += "# TYPE kb_quality_feedback_by_kb counter\n"
	for kbName, metrics := range m.feedbackByKB {
		out += fmt.Sprintf("kb_quality_feedback_by_kb{kb_name=\"%s\",feedback_type=\"positive\"} %d\n", kbName, metrics.Positive)
		out += fmt.Sprintf("kb_quality_feedback_by_kb{kb_name=\"%s\",feedback_type=\"negative\"} %d\n", kbName, metrics.Negative)
	}
	
	// Quality check metrics
	out += "# HELP kb_quality_check_run_total Total quality check runs\n"
	out += "# TYPE kb_quality_check_run_total counter\n"
	out += fmt.Sprintf("kb_quality_check_run_total %d\n", m.qualityCheckRunTotal)
	
	out += "# HELP kb_quality_check_duration_seconds Last quality check duration in seconds\n"
	out += "# TYPE kb_quality_check_duration_seconds gauge\n"
	out += fmt.Sprintf("kb_quality_check_duration_seconds %.2f\n", m.qualityCheckDuration)
	
	out += "# HELP kb_quality_check_last_run_timestamp Unix timestamp of last quality check\n"
	out += "# TYPE kb_quality_check_last_run_timestamp gauge\n"
	out += fmt.Sprintf("kb_quality_check_last_run_timestamp %d\n", m.qualityCheckLastRun)
	
	// Error metrics
	out += "# HELP kb_quality_error_total Total errors encountered\n"
	out += "# TYPE kb_quality_error_total counter\n"
	out += fmt.Sprintf("kb_quality_error_total %d\n", m.errorTotal)
	
	out += "# HELP kb_quality_error_by_type Errors by type\n"
	out += "# TYPE kb_quality_error_by_type counter\n"
	for errorType, count := range m.errorByType {
		out += fmt.Sprintf("kb_quality_error_by_type{error_type=\"%s\"} %d\n", errorType, count)
	}
	
	return out
}
```

### HTTP Handler for /metrics Endpoint

**File:** `lib/sop/tools/sopbackend/kb_quality_metrics_handler.go` (NEW)

```go
package sopbackend

import (
	"net/http"
	
	"github.com/sharedcode/sop/ai/memory"
)

var globalKBMetrics *memory.KBQualityMetrics

func init() {
	globalKBMetrics = memory.NewKBQualityMetrics()
}

// GetKBMetrics returns the global metrics instance
func GetKBMetrics() *memory.KBQualityMetrics {
	return globalKBMetrics
}

// HandleKBQualityMetrics serves Prometheus metrics at /metrics
func HandleKBQualityMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	metrics := globalKBMetrics.ExportPrometheus()
	
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(metrics))
}
```

### Integration into Backend

**Modify:** `lib/sop/tools/sopbackend/backend.go`

```go
// Add to RegisterHandlers method
func (b *Backend) RegisterHandlers(mux *http.ServeMux) {
	// ... existing routes ...
	
	// Prometheus metrics endpoint
	mux.HandleFunc("/metrics", HandleKBQualityMetrics)
	
	// ... rest of routes ...
}
```

### Instrumentation Points

**Modify:** `lib/sop/ai/agent/codingworkflow/orchestrator.go`

```go
import "github.com/sharedcode/sop/tools/sopbackend"

func (o *Orchestrator) Execute(task Task) (ExecutionResult, error) {
	// ... existing code ...
	
	// Instrument validation
	if o.ValidationOrchestrator != nil {
		startTime := time.Now()
		validationResult, revisionAttempts = o.ValidationOrchestrator.ValidateAndEnrich(
			task, pack, result.Message, o.ExpertiseRetriever, o.KBEffectiveness,
		)
		durationMs := time.Since(startTime).Milliseconds()
		
		// Record metrics
		metrics := sopbackend.GetKBMetrics()
		metrics.RecordValidation(validationResult.Passed, float64(durationMs))
		if revisionAttempts > 0 {
			metrics.RecordRevision(validationResult.Passed)
		}
	}
	
	// Instrument task classification
	if o.TaskClassifier != nil {
		classification := o.TaskClassifier.Classify(task, pack)
		metrics := sopbackend.GetKBMetrics()
		metrics.RecordTaskClassification(classification.TaskType)
	}
	
	// Instrument KB selection
	if len(result.KBsConsulted) > 0 {
		metrics := sopbackend.GetKBMetrics()
		for _, kbName := range result.KBsConsulted {
			metrics.RecordKBSelection(kbName, 0) // duration tracked elsewhere
		}
	}
	
	// ... existing code ...
}
```

**Modify:** `lib/sop/tools/sopbackend/kb_feedback_handler.go`

```go
func HandleKBFeedback(w http.ResponseWriter, r *http.Request) {
	// ... existing code ...
	
	// Record feedback metrics
	metrics := GetKBMetrics()
	metrics.RecordFeedback(req.KBName, req.Feedback == "positive")
	
	// ... existing code ...
}
```

**Modify:** `lib/sop/ai/memory/kb_quality_job.go`

```go
import "github.com/sharedcode/sop/tools/sopbackend"

func (job *QualityCheckJob) performQualityCheck() {
	startTime := time.Now()
	
	// ... existing check logic ...
	
	durationSec := time.Since(startTime).Seconds()
	
	// Record metrics
	metrics := sopbackend.GetKBMetrics()
	metrics.RecordQualityCheck(durationSec)
}
```

---

## Prometheus Configuration

### Scrape Configuration

**File:** `config/prometheus/prometheus.yml`

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s
  external_labels:
    monitor: 'sop-kb-quality'

# Alertmanager configuration
alerting:
  alertmanagers:
    - static_configs:
        - targets:
            - localhost:9093

# Load rules once and periodically evaluate them
rule_files:
  - 'kb_quality_alerts.yml'

# Scrape configurations
scrape_configs:
  # sop-daemon metrics
  - job_name: 'sop-daemon'
    static_configs:
      - targets:
          - 'localhost:9090'
    metrics_path: '/metrics'
    scrape_interval: 10s
    scrape_timeout: 5s
    
  # sop-httpserver metrics (if running separately)
  - job_name: 'sop-httpserver'
    static_configs:
      - targets:
          - 'localhost:8080'
    metrics_path: '/metrics'
    scrape_interval: 10s
    scrape_timeout: 5s
```

### Alert Rules

**File:** `config/prometheus/kb_quality_alerts.yml`

```yaml
groups:
  - name: kb_quality_alerts
    interval: 30s
    rules:
      # Validation Performance
      - alert: HighValidationLatency
        expr: kb_quality_validation_duration_avg > 100
        for: 5m
        labels:
          severity: warning
          component: kb_quality
        annotations:
          summary: "KB validation latency is high"
          description: "Average validation duration is {{ $value }}ms (threshold: 100ms)"
          
      - alert: CriticalValidationLatency
        expr: kb_quality_validation_duration_avg > 200
        for: 2m
        labels:
          severity: critical
          component: kb_quality
        annotations:
          summary: "KB validation latency is critical"
          description: "Average validation duration is {{ $value }}ms (threshold: 200ms)"
          
      # Validation Success Rate
      - alert: LowValidationPassRate
        expr: (kb_quality_validation_pass_total / kb_quality_validation_total) < 0.85
        for: 10m
        labels:
          severity: warning
          component: kb_quality
        annotations:
          summary: "KB validation pass rate is low"
          description: "Validation pass rate is {{ $value | humanizePercentage }} (threshold: 85%)"
          
      - alert: CriticalValidationPassRate
        expr: (kb_quality_validation_pass_total / kb_quality_validation_total) < 0.75
        for: 5m
        labels:
          severity: critical
          component: kb_quality
        annotations:
          summary: "KB validation pass rate is critical"
          description: "Validation pass rate is {{ $value | humanizePercentage }} (threshold: 75%)"
          
      # Feedback Quality
      - alert: LowPositiveFeedbackRate
        expr: kb_quality_feedback_positive_rate < 60
        for: 30m
        labels:
          severity: warning
          component: kb_quality
        annotations:
          summary: "User feedback sentiment is declining"
          description: "Positive feedback rate is {{ $value }}% (threshold: 60%)"
          
      - alert: CriticalPositiveFeedbackRate
        expr: kb_quality_feedback_positive_rate < 50
        for: 15m
        labels:
          severity: critical
          component: kb_quality
        annotations:
          summary: "User feedback sentiment is critically low"
          description: "Positive feedback rate is {{ $value }}% (threshold: 50%)"
          
      # Background Job Health
      - alert: QualityCheckJobNotRunning
        expr: time() - kb_quality_check_last_run_timestamp > 86400
        for: 1h
        labels:
          severity: warning
          component: kb_quality
        annotations:
          summary: "Quality check job has not run recently"
          description: "Last quality check was {{ $value | humanizeDuration }} ago (threshold: 24h)"
          
      - alert: QualityCheckJobSlow
        expr: kb_quality_check_duration_seconds > 300
        for: 5m
        labels:
          severity: warning
          component: kb_quality
        annotations:
          summary: "Quality check job is running slowly"
          description: "Last quality check took {{ $value }}s (threshold: 300s)"
          
      # Error Rate
      - alert: HighKBQualityErrorRate
        expr: rate(kb_quality_error_total[5m]) > 0.01
        for: 5m
        labels:
          severity: warning
          component: kb_quality
        annotations:
          summary: "KB Quality error rate is elevated"
          description: "Error rate is {{ $value | humanize }} errors/sec"
          
      - alert: CriticalKBQualityErrorRate
        expr: rate(kb_quality_error_total[5m]) > 0.05
        for: 2m
        labels:
          severity: critical
          component: kb_quality
        annotations:
          summary: "KB Quality error rate is critical"
          description: "Error rate is {{ $value | humanize }} errors/sec"
          
      # Revision Loop Frequency
      - alert: HighRevisionFrequency
        expr: (rate(kb_quality_revision_total[10m]) / rate(kb_quality_validation_total[10m])) > 0.2
        for: 15m
        labels:
          severity: warning
          component: kb_quality
        annotations:
          summary: "Revision loop frequency is high"
          description: "{{ $value | humanizePercentage }} of validations require revisions (threshold: 20%)"
```

---

## Grafana Dashboard

### Dashboard JSON

**File:** `config/grafana/dashboards/kb_quality_dashboard.json`

```json
{
  "dashboard": {
    "title": "KB Quality Monitoring",
    "tags": ["kb-quality", "ai", "code-generation"],
    "timezone": "browser",
    "panels": [
      {
        "title": "Validation Performance",
        "type": "graph",
        "gridPos": {"x": 0, "y": 0, "w": 12, "h": 8},
        "targets": [
          {
            "expr": "kb_quality_validation_duration_avg",
            "legendFormat": "Avg Duration (ms)"
          },
          {
            "expr": "rate(kb_quality_validation_total[5m]) * 60",
            "legendFormat": "Validations/min"
          }
        ],
        "yaxes": [
          {"label": "Duration (ms)", "format": "ms"},
          {"label": "Rate", "format": "short"}
        ]
      },
      {
        "title": "Validation Pass/Fail Rate",
        "type": "graph",
        "gridPos": {"x": 12, "y": 0, "w": 12, "h": 8},
        "targets": [
          {
            "expr": "rate(kb_quality_validation_pass_total[5m]) / rate(kb_quality_validation_total[5m]) * 100",
            "legendFormat": "Pass Rate (%)"
          },
          {
            "expr": "rate(kb_quality_validation_fail_total[5m]) / rate(kb_quality_validation_total[5m]) * 100",
            "legendFormat": "Fail Rate (%)"
          }
        ],
        "yaxes": [
          {"label": "Percentage", "format": "percent", "min": 0, "max": 100}
        ],
        "alert": {
          "conditions": [
            {
              "evaluator": {"type": "lt", "params": [85]},
              "query": {"params": ["A", "5m", "now"]},
              "reducer": {"type": "avg"}
            }
          ]
        }
      },
      {
        "title": "Task Classification Distribution",
        "type": "piechart",
        "gridPos": {"x": 0, "y": 8, "w": 8, "h": 8},
        "targets": [
          {
            "expr": "kb_quality_task_classification_by_type",
            "legendFormat": "{{task_type}}"
          }
        ]
      },
      {
        "title": "KB Selection Frequency",
        "type": "bargauge",
        "gridPos": {"x": 8, "y": 8, "w": 8, "h": 8},
        "targets": [
          {
            "expr": "topk(10, kb_quality_kb_selection_by_kb)",
            "legendFormat": "{{kb_name}}"
          }
        ],
        "options": {
          "orientation": "horizontal",
          "displayMode": "gradient"
        }
      },
      {
        "title": "User Feedback Sentiment",
        "type": "stat",
        "gridPos": {"x": 16, "y": 8, "w": 8, "h": 8},
        "targets": [
          {
            "expr": "kb_quality_feedback_positive_rate",
            "legendFormat": "Positive Rate"
          }
        ],
        "options": {
          "graphMode": "area",
          "colorMode": "value",
          "unit": "percent",
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {"value": 0, "color": "red"},
              {"value": 50, "color": "yellow"},
              {"value": 70, "color": "green"}
            ]
          }
        }
      },
      {
        "title": "Feedback Volume Over Time",
        "type": "graph",
        "gridPos": {"x": 0, "y": 16, "w": 12, "h": 8},
        "targets": [
          {
            "expr": "rate(kb_quality_feedback_positive_total[5m]) * 60",
            "legendFormat": "Positive/min"
          },
          {
            "expr": "rate(kb_quality_feedback_negative_total[5m]) * 60",
            "legendFormat": "Negative/min"
          }
        ]
      },
      {
        "title": "Feedback by KB (Top 10)",
        "type": "table",
        "gridPos": {"x": 12, "y": 16, "w": 12, "h": 8},
        "targets": [
          {
            "expr": "sum by (kb_name) (kb_quality_feedback_by_kb)",
            "format": "table",
            "instant": true
          }
        ],
        "transformations": [
          {
            "id": "organize",
            "options": {
              "excludeByName": {"Time": true},
              "indexByName": {},
              "renameByName": {
                "kb_name": "KB Name",
                "Value": "Total Feedback"
              }
            }
          }
        ]
      },
      {
        "title": "Revision Loop Frequency",
        "type": "graph",
        "gridPos": {"x": 0, "y": 24, "w": 12, "h": 8},
        "targets": [
          {
            "expr": "rate(kb_quality_revision_total[5m]) * 60",
            "legendFormat": "Revisions/min"
          },
          {
            "expr": "rate(kb_quality_revision_success[5m]) / rate(kb_quality_revision_total[5m]) * 100",
            "legendFormat": "Success Rate (%)"
          }
        ]
      },
      {
        "title": "Quality Check Job Status",
        "type": "stat",
        "gridPos": {"x": 12, "y": 24, "w": 6, "h": 8},
        "targets": [
          {
            "expr": "time() - kb_quality_check_last_run_timestamp",
            "legendFormat": "Time Since Last Run"
          }
        ],
        "options": {
          "graphMode": "none",
          "colorMode": "background",
          "unit": "s",
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {"value": 0, "color": "green"},
              {"value": 86400, "color": "yellow"},
              {"value": 172800, "color": "red"}
            ]
          }
        }
      },
      {
        "title": "Error Rate",
        "type": "graph",
        "gridPos": {"x": 18, "y": 24, "w": 6, "h": 8},
        "targets": [
          {
            "expr": "rate(kb_quality_error_total[5m])",
            "legendFormat": "Errors/sec"
          }
        ],
        "alert": {
          "conditions": [
            {
              "evaluator": {"type": "gt", "params": [0.01]},
              "query": {"params": ["A", "5m", "now"]},
              "reducer": {"type": "avg"}
            }
          ]
        }
      }
    ],
    "refresh": "30s",
    "time": {"from": "now-6h", "to": "now"}
  }
}
```

### Dashboard Provisioning

**File:** `config/grafana/provisioning/dashboards/kb_quality.yml`

```yaml
apiVersion: 1

providers:
  - name: 'KB Quality'
    orgId: 1
    folder: 'AI & Code Generation'
    type: file
    disableDeletion: false
    updateIntervalSeconds: 30
    allowUiUpdates: true
    options:
      path: /etc/grafana/dashboards/kb_quality
```

---

## Deployment

### Docker Compose Configuration

**File:** `docker-compose.observability.yml`

```yaml
version: '3.8'

services:
  prometheus:
    image: prom/prometheus:latest
    container_name: sop-prometheus
    volumes:
      - ./config/prometheus:/etc/prometheus
      - prometheus-data:/prometheus
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
      - '--web.console.libraries=/usr/share/prometheus/console_libraries'
      - '--web.console.templates=/usr/share/prometheus/consoles'
    ports:
      - "9091:9090"
    restart: unless-stopped
    networks:
      - sop-network
      
  grafana:
    image: grafana/grafana:latest
    container_name: sop-grafana
    volumes:
      - ./config/grafana/provisioning:/etc/grafana/provisioning
      - ./config/grafana/dashboards:/etc/grafana/dashboards
      - grafana-data:/var/lib/grafana
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
      - GF_USERS_ALLOW_SIGN_UP=false
      - GF_SERVER_ROOT_URL=http://localhost:3000
    ports:
      - "3000:3000"
    restart: unless-stopped
    networks:
      - sop-network
    depends_on:
      - prometheus
      
  alertmanager:
    image: prom/alertmanager:latest
    container_name: sop-alertmanager
    volumes:
      - ./config/alertmanager:/etc/alertmanager
    command:
      - '--config.file=/etc/alertmanager/config.yml'
      - '--storage.path=/alertmanager'
    ports:
      - "9093:9093"
    restart: unless-stopped
    networks:
      - sop-network

volumes:
  prometheus-data:
  grafana-data:

networks:
  sop-network:
    driver: bridge
```

### Alertmanager Configuration

**File:** `config/alertmanager/config.yml`

```yaml
global:
  resolve_timeout: 5m
  
route:
  group_by: ['alertname', 'severity', 'component']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 12h
  receiver: 'email-notifications'
  routes:
    - match:
        severity: critical
      receiver: 'pagerduty'
      continue: true
    - match:
        severity: warning
      receiver: 'slack-notifications'
      
receivers:
  - name: 'email-notifications'
    email_configs:
      - to: 'ops-team@example.com'
        from: 'alertmanager@example.com'
        smarthost: 'smtp.example.com:587'
        auth_username: 'alertmanager@example.com'
        auth_password: '${SMTP_PASSWORD}'
        
  - name: 'slack-notifications'
    slack_configs:
      - api_url: '${SLACK_WEBHOOK_URL}'
        channel: '#kb-quality-alerts'
        title: 'KB Quality Alert'
        text: '{{ range .Alerts }}{{ .Annotations.description }}{{ end }}'
        
  - name: 'pagerduty'
    pagerduty_configs:
      - service_key: '${PAGERDUTY_SERVICE_KEY}'
        description: '{{ .CommonAnnotations.summary }}'

inhibit_rules:
  - source_match:
      severity: 'critical'
    target_match:
      severity: 'warning'
    equal: ['alertname', 'component']
```

---

## Usage Guide

### Starting Observability Stack

```bash
# Start Prometheus, Grafana, and Alertmanager
docker-compose -f docker-compose.observability.yml up -d

# Check status
docker-compose -f docker-compose.observability.yml ps

# View logs
docker-compose -f docker-compose.observability.yml logs -f
```

### Accessing Dashboards

- **Grafana**: http://localhost:3000 (admin/admin)
- **Prometheus**: http://localhost:9091
- **Alertmanager**: http://localhost:9093

### Verifying Metrics Export

```bash
# Check metrics endpoint
curl http://localhost:9090/metrics | grep kb_quality

# Expected output:
# kb_quality_validation_total 1234
# kb_quality_validation_pass_total 1100
# kb_quality_feedback_positive_rate 75.50
# ...
```

### Querying Metrics in Prometheus

```promql
# Validation pass rate over time
rate(kb_quality_validation_pass_total[5m]) / rate(kb_quality_validation_total[5m]) * 100

# Top 5 most-used KBs
topk(5, kb_quality_kb_selection_by_kb)

# Feedback sentiment trend
kb_quality_feedback_positive_rate

# Average validation latency
kb_quality_validation_duration_avg
```

---

## Maintenance

### Regular Tasks

**Daily:**
- Review alert notifications
- Check dashboard for anomalies
- Verify metrics are being collected

**Weekly:**
- Analyze feedback trends
- Review KB performance metrics
- Adjust alert thresholds if needed

**Monthly:**
- Export metrics for long-term analysis
- Update dashboard visualizations
- Review and update alert rules

### Troubleshooting

**Metrics not appearing:**
1. Check sop-daemon is running: `curl http://localhost:9090/metrics`
2. Verify Prometheus can scrape: Check Prometheus UI → Status → Targets
3. Review Prometheus logs: `docker logs sop-prometheus`

**Alerts not firing:**
1. Check alert rules syntax: Prometheus UI → Status → Rules
2. Verify Alertmanager connection: Prometheus UI → Status → Alertmanagers
3. Test alert manually: Prometheus UI → Alerts → Force alert

**Dashboard not loading:**
1. Verify Grafana can reach Prometheus: Grafana UI → Configuration → Data Sources
2. Test data source connection
3. Check dashboard JSON syntax

---

## Best Practices

1. **Set Appropriate Alert Thresholds**: Start conservative, adjust based on baseline
2. **Use Alert Grouping**: Avoid alert fatigue by grouping related alerts
3. **Document Runbooks**: Link alert annotations to resolution procedures
4. **Regular Review**: Weekly review of metrics trends and alert patterns
5. **Backup Configurations**: Version control Prometheus/Grafana configs
6. **Test Alerts**: Regularly test alert delivery end-to-end
7. **Monitor the Monitors**: Set up uptime checks for Prometheus/Grafana

---

## Next Steps

1. Deploy observability stack to staging
2. Collect baseline metrics for 1 week
3. Tune alert thresholds based on real data
4. Train team on dashboard usage
5. Set up on-call rotation for critical alerts
6. Document incident response procedures

package sopbackend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

type observabilityMetrics struct {
	mu       sync.Mutex
	requests int64
	latency  time.Duration
	status   map[string]int64
	routes   map[string]int64
	product  map[string]int64
}

var observabilityState = &observabilityMetrics{
	status:  make(map[string]int64),
	routes:  make(map[string]int64),
	product: make(map[string]int64),
}

var observabilityProcessStart = time.Now()

func resetObservabilityStateForTesting() {
	observabilityState.mu.Lock()
	defer observabilityState.mu.Unlock()
	observabilityState.requests = 0
	observabilityState.latency = 0
	observabilityState.status = make(map[string]int64)
	observabilityState.routes = make(map[string]int64)
	observabilityState.product = make(map[string]int64)
}

// ResetObservabilityStateForTesting clears shared observability state for tests.
func ResetObservabilityStateForTesting() {
	resetObservabilityStateForTesting()
}

// RegisterMonitorabilityHandlers exposes the standard monitorability contract on the provided mux.
func RegisterMonitorabilityHandlers(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.HandleFunc("/healthz", HandleHealthz)
	mux.HandleFunc("/readyz", HandleReadyz)
	mux.HandleFunc("/info", HandleInfo)
	mux.HandleFunc("/metrics", HandleMetrics)
}

func NewObservabilityHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		path := strings.TrimSpace(r.URL.Path)
		if path == "" {
			path = "/"
		}

		switch path {
		case "/healthz":
			HandleHealthz(rw, r)
			recordObservability(path, rw.statusCode, time.Since(start))
			return
		case "/readyz":
			HandleReadyz(rw, r)
			recordObservability(path, rw.statusCode, time.Since(start))
			return
		case "/info":
			HandleInfo(rw, r)
			recordObservability(path, rw.statusCode, time.Since(start))
			return
		case "/metrics":
			HandleMetrics(rw, r)
			recordObservability(path, rw.statusCode, time.Since(start))
			return
		}

		next.ServeHTTP(rw, r)
		recordObservability(path, rw.statusCode, time.Since(start))
	})
}

func recordObservability(path string, statusCode int, elapsed time.Duration) {
	observabilityState.mu.Lock()
	defer observabilityState.mu.Unlock()
	observabilityState.requests++
	observabilityState.latency += elapsed
	observabilityState.status[http.StatusText(statusCode)]++
	observabilityState.routes[path]++
}

func recordProductMetric(metricName string) {
	observabilityState.mu.Lock()
	defer observabilityState.mu.Unlock()
	observabilityState.product[metricName]++
}

// RecordProductMetric increments a product-level counter exposed via /metrics.
func RecordProductMetric(metricName string) {
	recordProductMetric(metricName)
}

func HandleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "ok",
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"ha":        collectHAStatus(),
	})
}

func HandleReadyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "ready",
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"ha":        collectHAStatus(),
	})
}

func HandleInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":         "ok",
		"timestamp":      time.Now().UTC().Format(time.RFC3339Nano),
		"service":        getServiceName(),
		"version":        getVersionString(),
		"environment":    getEnvironmentName(),
		"hostname":       getHostname(),
		"uptime_seconds": int64(time.Since(observabilityProcessStart).Seconds()),
		"runtime": map[string]any{
			"goos":   runtimeGOOS(),
			"goarch": runtimeGOARCH(),
		},
		"ha": collectHAStatus(),
	})
}

func HandleMetrics(w http.ResponseWriter, r *http.Request) {
	observabilityState.mu.Lock()
	defer observabilityState.mu.Unlock()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte("# HELP sop_http_requests_total Total HTTP requests processed by the SOP backend\n"))
	_, _ = w.Write([]byte("# TYPE sop_http_requests_total counter\n"))
	_, _ = w.Write([]byte(fmt.Sprintf("sop_http_requests_total %d\n", observabilityState.requests)))
	_, _ = w.Write([]byte("# HELP sop_http_request_latency_ms Average HTTP request latency in milliseconds\n"))
	_, _ = w.Write([]byte("# TYPE sop_http_request_latency_ms gauge\n"))
	_, _ = w.Write([]byte(fmt.Sprintf("sop_http_request_latency_ms %0.3f\n", float64(observabilityState.latency.Milliseconds())/float64(max(observabilityState.requests, 1)))))

	for metricName, count := range observabilityState.product {
		_, _ = w.Write([]byte(fmt.Sprintf("sop_product_metric_total{name=\"%s\"} %d\n", sanitizeMetricLabel(metricName), count)))
	}

	for status, count := range observabilityState.status {
		_, _ = w.Write([]byte(fmt.Sprintf("sop_http_response_status_total{status=\"%s\"} %d\n", sanitizeMetricLabel(status), count)))
	}
	for path, count := range observabilityState.routes {
		_, _ = w.Write([]byte(fmt.Sprintf("sop_http_route_requests_total{path=\"%s\"} %d\n", sanitizeMetricLabel(path), count)))
	}

	haStatus := collectHAStatus()
	_, _ = w.Write([]byte("# HELP sop_ha_status HA status for the active/passive deployment\n"))
	_, _ = w.Write([]byte("# TYPE sop_ha_status gauge\n"))
	_, _ = w.Write([]byte(fmt.Sprintf("sop_ha_status{mode=\"%s\",enabled=\"%t\"} 1\n", sanitizeMetricLabel(fmt.Sprintf("%v", haStatus["mode"])), haStatus["enabled"].(bool))))
}

func collectHAStatus() map[string]any {
	configPath := firstNonEmpty(os.Getenv("SOP_HA_CONFIG_FILE"), os.Getenv("CONFIG_FILE"), "/etc/sop-ha/sop-ha.conf")
	stateDir := firstNonEmpty(os.Getenv("SOP_HA_STATE_DIR"), os.Getenv("STATE_DIR"), "/var/lib/sop-ha")

	status := map[string]any{
		"enabled":               false,
		"mode":                  "standalone",
		"storage_layout":        "active_writes_primary_passive_stands_by_on_replica",
		"config_path":           configPath,
		"state_dir":             stateDir,
		"state":                 "unknown",
		"peer_host":             "unset",
		"peer_last_seen":        "never",
		"heartbeat_age_seconds": "unknown",
	}

	if configBytes, err := os.ReadFile(configPath); err == nil {
		status["enabled"] = true
		status["mode"] = "active-passive"
		for _, line := range strings.Split(string(configBytes), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "PEER_HOST=") {
				status["peer_host"] = strings.TrimSpace(strings.TrimPrefix(trimmed, "PEER_HOST="))
			}
			if strings.HasPrefix(trimmed, "SERVICE_NAME=") {
				status["service_name"] = strings.TrimSpace(strings.TrimPrefix(trimmed, "SERVICE_NAME="))
			}
		}
	}

	stateFile := filepath.Join(stateDir, "state")
	if stateBytes, err := os.ReadFile(stateFile); err == nil {
		status["state"] = strings.TrimSpace(string(stateBytes))
	}

	heartbeatPath := filepath.Join(stateDir, "peer-heartbeat")
	if heartbeatBytes, err := os.ReadFile(heartbeatPath); err == nil {
		status["peer_last_seen"] = time.Unix(0, 0).UTC().Format(time.RFC3339Nano)
		if len(heartbeatBytes) > 0 {
			status["peer_last_seen"] = strings.TrimSpace(string(heartbeatBytes))
		}
	} else if status["state"] == "unknown" {
		status["peer_last_seen"] = "never"
	}

	if heartbeatInfo, err := os.Stat(heartbeatPath); err == nil {
		status["heartbeat_age_seconds"] = int64(time.Since(heartbeatInfo.ModTime()).Seconds())
	}

	return status
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func sanitizeMetricLabel(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return value
}

func getVersionString() string {
	return strings.TrimSpace(os.Getenv("SOP_VERSION"))
}

func getServiceName() string {
	if value := strings.TrimSpace(os.Getenv("SOP_SERVICE_NAME")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("SERVICE_NAME")); value != "" {
		return value
	}
	return "sop"
}

func getEnvironmentName() string {
	if value := strings.TrimSpace(os.Getenv("SOP_ENVIRONMENT")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("ENVIRONMENT")); value != "" {
		return value
	}
	return "unknown"
}

func getHostname() string {
	if value := strings.TrimSpace(os.Getenv("SOP_HOSTNAME")); value != "" {
		return value
	}
	if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
		return hostname
	}
	return "unknown"
}

func runtimeGOOS() string {
	if os.Getenv("SOP_RUNTIME_GOOS") != "" {
		return os.Getenv("SOP_RUNTIME_GOOS")
	}
	return runtime.GOOS
}

func runtimeGOARCH() string {
	if os.Getenv("SOP_RUNTIME_GOARCH") != "" {
		return os.Getenv("SOP_RUNTIME_GOARCH")
	}
	return runtime.GOARCH
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

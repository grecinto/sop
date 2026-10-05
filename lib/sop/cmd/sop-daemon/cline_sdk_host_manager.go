package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sharedcode/sop/ai/agent/domains/ideworker"
)

const (
	maxClineSDKHostSessions = 32
	clineCancelGracePeriod  = 15 * time.Second
	clineHostIdleTTL        = 30 * time.Minute
	clineHostSweepInterval  = time.Minute
)

type clineSDKHostManager struct {
	mu        sync.Mutex
	hosts     map[string]*clineSDKHostProcess
	requests  map[string]*clineSDKHostProcess
	cancelled map[string]bool
	bootID    string
	closed    bool
	nodeBin   string
	stopSweep chan struct{}
	sweepDone chan struct{}
	starting  int
	startWG   sync.WaitGroup
}

type clineSDKHostProcess struct {
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	events         chan clineSDKHostMessage
	done           chan struct{}
	writeMu        sync.Mutex
	runMu          sync.Mutex
	waitErrMu      sync.Mutex
	waitErr        error
	clineSessionID string
	active         int
	lastUsed       time.Time
}

type clineSDKHostMessage struct {
	event clineSDKHostEvent
	raw   []byte
	err   error
}

type clineSDKHostEvent struct {
	Type      string          `json:"type"`
	RequestID string          `json:"requestId,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Message   string          `json:"message,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}

func newClineSDKHostManager() *clineSDKHostManager {
	bootBytes := make([]byte, 16)
	if _, err := rand.Read(bootBytes); err != nil {
		bootBytes = fmt.Appendf(nil, "%d", time.Now().UnixNano())
	}
	nodeBin, _ := exec.LookPath("node")
	manager := &clineSDKHostManager{
		hosts:     make(map[string]*clineSDKHostProcess),
		requests:  make(map[string]*clineSDKHostProcess),
		cancelled: make(map[string]bool),
		bootID:    hex.EncodeToString(bootBytes),
		nodeBin:   nodeBin,
		stopSweep: make(chan struct{}),
		sweepDone: make(chan struct{}),
	}
	go manager.sweepIdleHosts()
	return manager
}

func clineSDKSessionScope(req ideworker.TaskRequest) string {
	workstreamID := strings.TrimSpace(req.WorkstreamID)
	if workstreamID == "" {
		workstreamID = strings.TrimSpace(req.SessionID)
	}
	if workstreamID == "" {
		workstreamID = strings.TrimSpace(req.RequestID)
	}
	workspace := strings.TrimSpace(req.WorkspaceRoot)
	if absolute, err := filepath.Abs(workspace); err == nil {
		workspace = filepath.Clean(absolute)
	}
	if resolved, err := filepath.EvalSymlinks(workspace); err == nil {
		workspace = resolved
	}
	apiKeyHash := sha256.Sum256([]byte(clineAPIKey(req)))
	parts := []string{
		workstreamID,
		workspace,
		strings.TrimSpace(req.Provider),
		strings.TrimSpace(req.Model),
		strings.TrimSpace(req.BaseURL),
		hex.EncodeToString(apiKeyHash[:]),
		strings.TrimSpace(req.MCPURL),
		boolString(strings.EqualFold(strings.TrimSpace(req.Mode), "yolo")),
	}
	return strings.Join(parts, "\x00")
}

func clineAPIKey(req ideworker.TaskRequest) string {
	if value := strings.TrimSpace(req.APIKey); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("SOP_CLINE_API_KEY")); value != "" {
		return value
	}
	switch strings.ToLower(strings.TrimSpace(req.Provider)) {
	case "openai-native":
		return strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	case "anthropic":
		return strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	case "gemini":
		return strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	default:
		return ""
	}
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func clineSessionIDForScope(bootID, scope string) string {
	hash := sha256.Sum256([]byte(bootID + "\x00" + scope))
	return "sop-" + hex.EncodeToString(hash[:16])
}

func (m *clineSDKHostManager) Run(ctx context.Context, hostPath string, req ideworker.TaskRequest, progress func(string)) (string, error) {
	if m == nil {
		return "", fmt.Errorf("Cline SDK host manager is not initialized")
	}
	if err := validateClineWorkspaceRoot(req.WorkspaceRoot); err != nil {
		return "", err
	}
	if strings.TrimSpace(req.Task) == "" {
		return "", fmt.Errorf("Cline task requires a task")
	}
	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = newContinueRequestID()
		req.RequestID = requestID
	}
	m.mu.Lock()
	if m.cancelled[requestID] {
		delete(m.cancelled, requestID)
		m.mu.Unlock()
		return "", context.Canceled
	}
	m.mu.Unlock()
	scope := clineSDKSessionScope(req)
	host, err := m.hostFor(scope, hostPath)
	if err != nil {
		return "", err
	}
	defer m.releaseHost(scope, host)
	host.runMu.Lock()
	defer host.runMu.Unlock()
	m.mu.Lock()
	if m.cancelled[requestID] {
		delete(m.cancelled, requestID)
		m.mu.Unlock()
		return "", context.Canceled
	}
	m.mu.Unlock()
	clineSessionID := host.clineSessionID
	apiKey := clineAPIKey(req)
	payload := map[string]any{
		"type":              "run",
		"requestId":         requestID,
		"sessionId":         req.SessionID,
		"workstreamId":      req.WorkstreamID,
		"clineSessionId":    clineSessionID,
		"task":              req.Task,
		"workspaceRoot":     req.WorkspaceRoot,
		"providerId":        req.Provider,
		"modelId":           req.Model,
		"apiKey":            apiKey,
		"baseUrl":           req.BaseURL,
		"mcpUrl":            req.MCPURL,
		"mcpBearerToken":    req.MCPBearerToken,
		"mode":              clineSDKMode(req.Mode),
		"guidance":          req.Guidance,
		"enableCheckpoints": true,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal Cline SDK request: %w", err)
	}
	m.mu.Lock()
	if m.cancelled[requestID] {
		delete(m.cancelled, requestID)
		m.mu.Unlock()
		return "", context.Canceled
	}
	if m.closed {
		m.mu.Unlock()
		return "", fmt.Errorf("Cline SDK host manager is closed")
	}
	if err := host.writeLine(body); err != nil {
		m.mu.Unlock()
		m.dropHost(scope, host)
		_ = host.close()
		return "", fmt.Errorf("write Cline SDK request: %w", err)
	}
	m.requests[requestID] = host
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.requests, requestID)
		delete(m.cancelled, requestID)
		m.mu.Unlock()
	}()

	var output strings.Builder
	invalidateSession := false
	ctxDone := ctx.Done()
	var cancelTimer *time.Timer
	var cancelTimerC <-chan time.Time
	for {
		select {
		case message := <-host.events:
			if message.err != nil {
				m.dropHost(scope, host)
				return "", fmt.Errorf("read Cline SDK host: %w", message.err)
			}
			if message.event.RequestID != "" && message.event.RequestID != requestID {
				continue
			}
			output.Write(message.raw)
			output.WriteByte('\n')
			if message.event.Type == "progress" && progress != nil {
				progress(clineProgressText(message.event))
			}
			switch message.event.Type {
			case "session_invalidated":
				invalidateSession = true
			case "result", "error", "cancelled":
				if cancelTimer != nil {
					cancelTimer.Stop()
				}
				if invalidateSession {
					m.dropHost(scope, host)
					_ = host.close()
				}
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				return output.String(), nil
			}
		case <-ctxDone:
			ctxDone = nil
			cancelRequest, _ := json.Marshal(map[string]string{"type": "cancel", "requestId": requestID})
			if err := host.writeLine(cancelRequest); err != nil {
				m.dropHost(scope, host)
				_ = host.close()
				return "", ctx.Err()
			}
			cancelTimer = time.NewTimer(clineCancelGracePeriod)
			cancelTimerC = cancelTimer.C
		case <-cancelTimerC:
			m.dropHost(scope, host)
			_ = host.terminate()
			return "", fmt.Errorf("Cline session %s did not stop after cancellation", clineSessionID)
		case <-host.done:
			m.dropHost(scope, host)
			return "", fmt.Errorf("Cline SDK host exited: %w", host.exitError())
		}
	}
}

func (m *clineSDKHostManager) Cancel(requestID string) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return fmt.Errorf("Cline request ID is required for cancellation")
	}
	m.mu.Lock()
	host := m.requests[requestID]
	if host == nil {
		m.cancelled[requestID] = true
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	body, _ := json.Marshal(map[string]string{"type": "cancel", "requestId": requestID})
	return host.writeLine(body)
}

func clineProgressText(event clineSDKHostEvent) string {
	if strings.TrimSpace(event.Message) != "" {
		return event.Message
	}
	if len(event.Result) > 0 {
		return string(event.Result)
	}
	return event.Type
}

func (m *clineSDKHostManager) hostFor(scope, hostPath string) (*clineSDKHostProcess, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, fmt.Errorf("Cline SDK host manager is closed")
	}
	if host := m.hosts[scope]; host != nil {
		select {
		case <-host.done:
			delete(m.hosts, scope)
		default:
			host.active++
			host.lastUsed = time.Now()
			m.mu.Unlock()
			return host, nil
		}
	}
	if len(m.hosts)+m.starting >= maxClineSDKHostSessions {
		var evictKey string
		var evict *clineSDKHostProcess
		for key, candidate := range m.hosts {
			if candidate.active != 0 {
				continue
			}
			if evict == nil || candidate.lastUsed.Before(evict.lastUsed) {
				evictKey, evict = key, candidate
			}
		}
		if evict == nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("Cline SDK host session limit reached (%d); all sessions are active", maxClineSDKHostSessions)
		}
		delete(m.hosts, evictKey)
		m.mu.Unlock()
		_ = evict.close()
		return m.hostFor(scope, hostPath)
	}
	if m.nodeBin == "" {
		m.mu.Unlock()
		return nil, fmt.Errorf("Cline SDK host requires Node.js 22 or newer")
	}
	m.starting++
	m.startWG.Add(1)
	m.mu.Unlock()
	defer m.startWG.Done()
	host, err := startClineSDKHost(m.nodeBin, hostPath)
	m.mu.Lock()
	m.starting--
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	host.clineSessionID = clineSessionIDForScope(m.bootID, scope+"\x00"+newContinueRequestID())
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = host.close()
		return nil, fmt.Errorf("Cline SDK host manager is closed")
	}
	if existing := m.hosts[scope]; existing != nil {
		select {
		case <-existing.done:
			delete(m.hosts, scope)
		default:
			existing.active++
			existing.lastUsed = time.Now()
			m.mu.Unlock()
			_ = host.close()
			return existing, nil
		}
	}
	host.active = 1
	host.lastUsed = time.Now()
	m.hosts[scope] = host
	m.mu.Unlock()
	return host, nil
}

func (m *clineSDKHostManager) sweepIdleHosts() {
	ticker := time.NewTicker(clineHostSweepInterval)
	defer ticker.Stop()
	defer close(m.sweepDone)
	for {
		select {
		case <-m.stopSweep:
			return
		case now := <-ticker.C:
			var idle []*clineSDKHostProcess
			m.mu.Lock()
			for scope, host := range m.hosts {
				if host.active == 0 && now.Sub(host.lastUsed) >= clineHostIdleTTL {
					delete(m.hosts, scope)
					idle = append(idle, host)
				}
			}
			m.mu.Unlock()
			for _, host := range idle {
				_ = host.close()
			}
		}
	}
}

func (m *clineSDKHostManager) releaseHost(scope string, host *clineSDKHostProcess) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hosts[scope] == host && host.active > 0 {
		host.active--
		host.lastUsed = time.Now()
	}
}

func (m *clineSDKHostManager) dropHost(scope string, host *clineSDKHostProcess) {
	m.mu.Lock()
	if m.hosts[scope] == host {
		delete(m.hosts, scope)
	}
	m.mu.Unlock()
}

func (m *clineSDKHostManager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	hosts := make([]*clineSDKHostProcess, 0, len(m.hosts))
	for _, host := range m.hosts {
		hosts = append(hosts, host)
	}
	requests := make(map[string]*clineSDKHostProcess, len(m.requests))
	for requestID, host := range m.requests {
		requests[requestID] = host
	}
	m.hosts = make(map[string]*clineSDKHostProcess)
	m.requests = make(map[string]*clineSDKHostProcess)
	m.cancelled = make(map[string]bool)
	m.mu.Unlock()
	close(m.stopSweep)
	<-m.sweepDone
	m.startWG.Wait()
	for requestID, host := range requests {
		_ = host.writeLine(mustJSON(map[string]string{"type": "cancel", "requestId": requestID}))
	}
	var errs []string
	var wait sync.WaitGroup
	var errMu sync.Mutex
	for _, host := range hosts {
		wait.Add(1)
		go func(host *clineSDKHostProcess) {
			defer wait.Done()
			if err := host.close(); err != nil {
				errMu.Lock()
				errs = append(errs, err.Error())
				errMu.Unlock()
			}
		}(host)
	}
	wait.Wait()
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func startClineSDKHost(nodeBin, hostPath string) (*clineSDKHostProcess, error) {
	if strings.TrimSpace(hostPath) == "" {
		return nil, fmt.Errorf("Cline SDK host path is empty")
	}
	if info, err := os.Stat(hostPath); err != nil || info.IsDir() {
		if err == nil {
			err = fmt.Errorf("path is a directory")
		}
		return nil, fmt.Errorf("Cline SDK host is unavailable at %s: %w", hostPath, err)
	}
	cmd := exec.Command(nodeBin, hostPath)
	configureClineHostCommand(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create Cline SDK host stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("create Cline SDK host stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("create Cline SDK host stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, fmt.Errorf("start Cline SDK host: %w", err)
	}
	host := &clineSDKHostProcess{
		cmd: cmd, stdin: stdin, events: make(chan clineSDKHostMessage, 128), done: make(chan struct{}),
		lastUsed: time.Now(),
	}
	go host.readEvents(stdout)
	go host.drainStderr(stderr)
	go host.wait()
	return host, nil
}

func (h *clineSDKHostProcess) writeLine(line []byte) error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	select {
	case <-h.done:
		return fmt.Errorf("host process is not running")
	default:
	}
	if _, err := h.stdin.Write(line); err != nil {
		return err
	}
	_, err := h.stdin.Write([]byte{'\n'})
	return err
}

func (h *clineSDKHostProcess) readEvents(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		raw := append([]byte(nil), scanner.Bytes()...)
		var event clineSDKHostEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			continue
		}
		select {
		case h.events <- clineSDKHostMessage{event: event, raw: raw}:
		case <-h.done:
			return
		}
	}
	if err := scanner.Err(); err != nil {
		select {
		case h.events <- clineSDKHostMessage{err: err}:
		case <-h.done:
		}
	}
}

func (h *clineSDKHostProcess) drainStderr(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		if shouldLogVerbose() {
			log.Printf("[Cline SDK host pid=%d] %s", h.cmd.Process.Pid, scanner.Text())
		}
	}
	if err := scanner.Err(); err != nil && shouldLogVerbose() {
		log.Printf("[Cline SDK host pid=%d] read stderr: %v", h.cmd.Process.Pid, err)
	}
}

func (h *clineSDKHostProcess) wait() {
	err := h.cmd.Wait()
	h.waitErrMu.Lock()
	h.waitErr = err
	h.waitErrMu.Unlock()
	close(h.done)
}

func (h *clineSDKHostProcess) exitError() error {
	h.waitErrMu.Lock()
	defer h.waitErrMu.Unlock()
	if h.waitErr == nil {
		return fmt.Errorf("process exited")
	}
	return h.waitErr
}

func (h *clineSDKHostProcess) close() error {
	_ = h.writeLine(mustJSON(map[string]string{"type": "shutdown"}))
	select {
	case <-h.done:
		return nil
	case <-time.After(5 * time.Second):
		return killClineHostProcess(h.cmd)
	}
}

func (h *clineSDKHostProcess) terminate() error {
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return nil
	}
	err := killClineHostProcess(h.cmd)
	select {
	case <-h.done:
	case <-time.After(3 * time.Second):
	}
	return err
}

func mustJSON(value any) []byte {
	body, _ := json.Marshal(value)
	return body
}

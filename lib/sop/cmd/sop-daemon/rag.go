package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent"
	"github.com/sharedcode/sop/ai/agent/domains/ideworker"
	aidb "github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/generator"
	"github.com/sharedcode/sop/tools/sopbackend"
)

const defaultDaemonConfigFileName = "config.json"

var unifiedHunkHeaderPattern = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// WARNING FOR FUTURE COPILOTS / MAINTAINERS:
//
// The daemon is now a separate lightweight app with its own provisioning and
// lifecycle rules. Reusing helpers from sopbackend is fine, but do not infer
// that daemon should mirror httpserver/HQ behavior.
//
// In particular:
//   - daemon config/bootstrap behavior is daemon-owned
//   - daemon provisioning should remain daemon-specific
//   - shared helpers are implementation reuse, not an architectural mandate
//
// Daemon uses the shared SetupStream from sopbackend as a reusable transport
// helper only.

// isValidDaemonConfig checks if the config file has the required daemon structure
func isValidDaemonConfig(configPath string) bool {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}

	var config struct {
		SystemDB *struct {
			Name string `json:"name"`
			Path string `json:"path"`
			Mode string `json:"mode"`
		} `json:"system_db"`
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return false
	}

	// Valid daemon config must have system_db with path
	return config.SystemDB != nil && config.SystemDB.Path != ""
}

func resolveDaemonConfigLocation(explicitPath string, out io.Writer) (string, error) {
	if strings.TrimSpace(explicitPath) != "" {
		resolved := resolveConfigPath(explicitPath)
		if out != nil {
			fmt.Fprintln(out, resolved)
		}
		return resolved, nil
	}

	candidates := []string{}
	if v := os.Getenv("XDG_CONFIG_HOME"); strings.TrimSpace(v) != "" {
		candidates = append(candidates, filepath.Join(v, "sop", defaultDaemonConfigFileName))
	} else {
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, ".config", "sop", defaultDaemonConfigFileName))
			candidates = append(candidates, filepath.Join(home, "sop", defaultDaemonConfigFileName))
		}
		candidates = append(candidates, filepath.Join(cwd(), defaultDaemonConfigFileName))
		candidates = append(candidates, filepath.Join(cwd(), "sop", defaultDaemonConfigFileName))
	}

	var defaultPath string
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			if isValidDaemonConfig(candidate) {
				if out != nil {
					fmt.Fprintln(out, candidate)
				}
				return candidate, nil
			}
			log.Printf("Found invalid daemon config at %s, will recreate with proper structure", candidate)
			defaultPath = candidate
			break
		}
	}

	if defaultPath == "" {
		if v := os.Getenv("XDG_CONFIG_HOME"); strings.TrimSpace(v) != "" {
			defaultPath = filepath.Join(v, "sop", defaultDaemonConfigFileName)
		} else if home, err := os.UserHomeDir(); err == nil {
			defaultPath = filepath.Join(home, ".config", "sop", defaultDaemonConfigFileName)
		} else {
			defaultPath = filepath.Join(cwd(), defaultDaemonConfigFileName)
		}
	}

	if out != nil {
		fmt.Fprintln(out, defaultPath)
	}
	return defaultPath, nil
}

func ensureConfigPath(explicitPath string) (string, bool, error) {
	// NOTE: This helper reflects the current quick-provision behavior. Future
	// daemon provisioning work is expected to move this startup-side mutation
	// behind explicit daemon provisioning endpoints. Do not expand this into a
	// broader HQ/httpserver-style setup path.
	resolvedPath, err := resolveDaemonConfigLocation(explicitPath, nil)
	if err != nil {
		return "", false, err
	}

	if strings.TrimSpace(explicitPath) != "" {
		return resolvedPath, false, nil
	}

	if _, err := os.Stat(resolvedPath); err == nil {
		if isValidDaemonConfig(resolvedPath) {
			return resolvedPath, false, nil
		}
		log.Printf("Found invalid daemon config at %s, will recreate with proper structure", resolvedPath)
	}

	if err := os.MkdirAll(filepath.Dir(resolvedPath), 0o755); err != nil {
		return "", false, fmt.Errorf("create config directory: %w", err)
	}

	systemDBPath := filepath.Join(filepath.Dir(resolvedPath), "system")
	bootstrap := fmt.Sprintf(`{
  "port": 9090,
  "system_db": {
    "name": "system",
    "path": "%s",
    "mode": "standalone",
    "is_system": true
  },
  "databases": [],
  "pageSize": 40,
  "production_mode": true
}
`, systemDBPath)
	if err := os.WriteFile(resolvedPath, []byte(bootstrap), 0o644); err != nil {
		return "", false, fmt.Errorf("write default config: %w", err)
	}

	log.Printf("Created default daemon config at %s with standalone SystemDB", resolvedPath)
	return resolvedPath, true, nil
}

type AskRequest struct {
	Query string `json:"query"`
	Auth  struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	} `json:"auth"`
	HQ struct {
		BaseURL   string `json:"base_url"`
		Port      string `json:"port,omitempty"`
		UserID    string `json:"user_id"`
		SessionID string `json:"session_id"`
	} `json:"hq"`
	LLMProvider        string                   `json:"llm_provider,omitempty"`
	LLMAPIKey          string                   `json:"llm_api_key,omitempty"`
	LLMModel           string                   `json:"llm_model,omitempty"`
	LLMURL             string                   `json:"llm_url,omitempty"`
	LLMCredentials     map[string]llmCredential `json:"llm_credentials,omitempty"`
	LLMPoolVersion     int64                    `json:"llm_pool_version,omitempty"`
	LLMPoolUpdatedAtMS int64                    `json:"llm_pool_updated_at_ms,omitempty"`
	LLMPoolUpdatedBy   string                   `json:"llm_pool_updated_by,omitempty"`
	ThinkingLevel      string                   `json:"thinking_level,omitempty"`
	InputParts         []ai.ContentPart         `json:"input_parts,omitempty"`
}

type AskResponse struct {
	Answer string `json:"answer"`
	Error  string `json:"error,omitempty"`
}

type runtimeProviderCandidate struct {
	Provider    string
	APIKey      string
	Model       string
	URL         string
	UpdatedAtMS int64
}

func runtimeCandidatePreferenceRank(provider string) int {
	switch normalizeCredentialProviderKey(provider) {
	case "gemini":
		return 0
	case "anthropic":
		return 1
	case "openai":
		return 2
	default:
		return 100
	}
}

func buildRuntimeProviderChain(provider, apiKey, model, url string, pool map[string]llmCredential, thinkingLevel string) []runtimeProviderCandidate {
	ordered := make([]runtimeProviderCandidate, 0, 8)
	seen := make(map[string]struct{})

	add := func(p, k, m, u string, updatedAtMS int64) {
		normalizedProvider := normalizeCredentialProviderKey(p)
		if normalizedProvider == "" {
			return
		}
		normalizedModel := strings.TrimSpace(m)
		key := normalizedProvider + ":" + normalizedModel
		if normalizedModel == "" {
			key = normalizedProvider
		}
		if _, exists := seen[key]; exists {
			return
		}
		ordered = append(ordered, runtimeProviderCandidate{
			Provider:    normalizedProvider,
			APIKey:      strings.TrimSpace(k),
			Model:       normalizedModel,
			URL:         strings.TrimSpace(u),
			UpdatedAtMS: updatedAtMS,
		})
		seen[key] = struct{}{}
	}

	add(provider, apiKey, model, url, 0)

	for _, candidate := range []string{"openai", "anthropic", "gemini"} {
		cred, ok := pool[candidate]
		if !ok {
			continue
		}
		add(candidate, cred.APIKey, cred.Model, cred.URL, cred.UpdatedAtMS)
	}
	for providerName, cred := range pool {
		if providerName == "" {
			continue
		}
		add(providerName, cred.APIKey, cred.Model, cred.URL, cred.UpdatedAtMS)
	}

	if len(ordered) <= 1 {
		return ordered
	}

	primary := ordered[0]
	rest := append([]runtimeProviderCandidate(nil), ordered[1:]...)
	if len(rest) > 1 {
		sort.SliceStable(rest, func(i, j int) bool {
			return runtimeCandidateSortValue(rest[i], rest[j], thinkingLevel) < runtimeCandidateSortValue(rest[j], rest[i], thinkingLevel)
		})
	}
	ordered = append([]runtimeProviderCandidate{primary}, rest...)
	return ordered
}

func runtimeCandidateSortValue(a, b runtimeProviderCandidate, thinkingLevel string) int {
	// Selection is based on explicit primary choice, task eligibility, static provider
	// preference, configured credential recency, and catalog cost/capability only.
	// Runtime success/failure feedback is intentionally excluded so transient errors
	// cannot silently lower the future quality of the selected pool.
	if strings.TrimSpace(a.Provider) == "" || strings.TrimSpace(a.Model) == "" {
		return 1
	}
	if strings.TrimSpace(b.Provider) == "" || strings.TrimSpace(b.Model) == "" {
		return 0
	}

	entryA, _ := ai.GetModelCatalogRegistry().FindEntry(a.Provider, a.Model)
	entryB, _ := ai.GetModelCatalogRegistry().FindEntry(b.Provider, b.Model)

	requiredMin := runtimeRequiredIntelligenceScore(thinkingLevel)
	eligibleA := !isHighIntelligenceRequiredForCandidate(entryA, requiredMin)
	eligibleB := !isHighIntelligenceRequiredForCandidate(entryB, requiredMin)
	if eligibleA != eligibleB {
		if eligibleA {
			return -1
		}
		return 1
	}

	prefA := runtimeCandidatePreferenceRank(a.Provider)
	prefB := runtimeCandidatePreferenceRank(b.Provider)
	if prefA != prefB {
		return prefA - prefB
	}

	if a.UpdatedAtMS != b.UpdatedAtMS {
		if a.UpdatedAtMS == 0 {
			return 1
		}
		if b.UpdatedAtMS == 0 {
			return -1
		}
		if a.UpdatedAtMS != b.UpdatedAtMS {
			return int(b.UpdatedAtMS - a.UpdatedAtMS)
		}
	}

	costA := runtimeCandidateEstimatedCost(entryA, a)
	costB := runtimeCandidateEstimatedCost(entryB, b)
	if costA != costB {
		return int((costA - costB) * 1_000_000)
	}

	intA := runtimeCandidateIntelligenceScore(entryA)
	intB := runtimeCandidateIntelligenceScore(entryB)
	if intA != intB {
		return intB - intA
	}
	return 0
}

func runtimeCandidateEstimatedCost(entry ai.ModelCatalogEntry, candidate runtimeProviderCandidate) float64 {
	if entry.Provider == "" || entry.Model == "" {
		return 1_000_000_000.0
	}
	if entry.InputCostPer1MTokens <= 0 && entry.OutputCostPer1MTokens <= 0 && entry.LastKnownInputCostUSD <= 0 && entry.LastKnownOutputCostUSD <= 0 {
		return 1_000_000_000.0
	}
	if entry.InputCostPer1MTokens <= 0 {
		return entry.LastKnownInputCostUSD + entry.LastKnownOutputCostUSD
	}
	if entry.OutputCostPer1MTokens <= 0 {
		return entry.InputCostPer1MTokens + entry.LastKnownOutputCostUSD
	}
	return entry.InputCostPer1MTokens + entry.OutputCostPer1MTokens
}

func runtimeCandidateIntelligenceScore(entry ai.ModelCatalogEntry) int {
	if entry.Provider == "" || entry.Model == "" {
		return 0
	}
	if entry.ReasoningStrength > 0 {
		return entry.ReasoningStrength
	}
	switch strings.ToLower(strings.TrimSpace(entry.Tier)) {
	case "strong":
		return 5
	case "standard":
		return 3
	case "cheap":
		return 2
	default:
		return 1
	}
}

func runtimeRequiredIntelligenceScore(thinkingLevel string) int {
	switch strings.ToLower(strings.TrimSpace(thinkingLevel)) {
	case "low":
		return 1
	case "medium":
		return 3
	case "high":
		return 5
	default:
		return 1
	}
}

func isHighIntelligenceRequiredForCandidate(entry ai.ModelCatalogEntry, requiredMin int) bool {
	if requiredMin <= 0 {
		return false
	}
	if entry.Provider == "" || entry.Model == "" {
		return false
	}
	return runtimeCandidateIntelligenceScore(entry) < requiredMin
}

func newGeneratorForRuntimeProvider(candidate runtimeProviderCandidate) (ai.Generator, string, string, string, string, error) {
	providerName, modelName := agent.NormalizeProviderAndModel(candidate.Provider, candidate.Model)
	if providerName == "" {
		return nil, "", "", "", "", fmt.Errorf("provider is required")
	}

	decryptedAPIKey, err := resolveDaemonSecretValue(candidate.APIKey)
	if err != nil {
		return nil, "", "", "", "", err
	}

	generatorProvider := providerName
	if generatorProvider == "openai" {
		generatorProvider = "chatgpt"
	}

	options := make(map[string]any)
	if decryptedAPIKey != "" {
		options["api_key"] = decryptedAPIKey
	}
	if modelName != "" {
		options["model"] = modelName
	}
	if candidate.URL != "" {
		switch generatorProvider {
		case "ollama":
			options["base_url"] = candidate.URL
		default:
			options["api_url"] = candidate.URL
		}
	}

	gen, err := generator.New(generatorProvider, options)
	if err != nil {
		return nil, "", "", "", "", err
	}

	return gen, providerName, decryptedAPIKey, modelName, strings.TrimSpace(candidate.URL), nil
}

func buildRuntimeGeneratorChain(candidates []runtimeProviderCandidate) (ai.Generator, *agent.ProviderDetails) {
	resolvedGenerators := make([]ai.Generator, 0, len(candidates))
	var primaryDetails *agent.ProviderDetails

	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.Provider) == "" {
			continue
		}
		gen, providerName, decryptedAPIKey, modelName, resolvedURL, err := newGeneratorForRuntimeProvider(candidate)
		if err != nil {
			log.Printf("daemon handleAsk: provider init skipped provider=%q error=%v", candidate.Provider, err)
			continue
		}
		resolvedGenerators = append(resolvedGenerators, gen)
		if primaryDetails == nil {
			primaryDetails = &agent.ProviderDetails{
				Provider: providerName,
				APIKey:   decryptedAPIKey,
				Model:    modelName,
				BaseURL:  resolvedURL,
			}
		}
	}

	if len(resolvedGenerators) == 0 {
		return nil, nil
	}
	if len(resolvedGenerators) == 1 {
		return resolvedGenerators[0], primaryDetails
	}
	return generator.NewFallbackGenerator(resolvedGenerators[0], resolvedGenerators[1:]), primaryDetails
}

type continueChatToolFunction struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

type continueChatTool struct {
	Type         string                   `json:"type,omitempty"`
	DisplayTitle string                   `json:"displayTitle,omitempty"`
	Group        string                   `json:"group,omitempty"`
	Readonly     bool                     `json:"readonly,omitempty"`
	Function     continueChatToolFunction `json:"function,omitempty"`
}

type continueChatToolChoice struct {
	Type     string                   `json:"type,omitempty"`
	Function continueChatToolFunction `json:"function,omitempty"`
}

type continueChatRequest struct {
	SessionID          string                   `json:"session_id"`
	Message            string                   `json:"message"`
	Provider           string                   `json:"provider,omitempty"`
	APIKey             string                   `json:"api_key,omitempty"`
	Model              string                   `json:"model,omitempty"`
	URL                string                   `json:"url,omitempty"`
	LLMCredentials     map[string]llmCredential `json:"llm_credentials,omitempty"`
	LLMPoolVersion     int64                    `json:"llm_pool_version,omitempty"`
	LLMPoolUpdatedAtMS int64                    `json:"llm_pool_updated_at_ms,omitempty"`
	LLMPoolUpdatedBy   string                   `json:"llm_pool_updated_by,omitempty"`
	Workspace          struct {
		Root      string `json:"root"`
		Active    string `json:"active_file"`
		Selection struct {
			StartLine int `json:"start_line"`
			EndLine   int `json:"end_line"`
		} `json:"selection"`
	} `json:"workspace"`
	Context struct {
		OpenFiles    []string `json:"open_files"`
		SelectedText string   `json:"selected_text"`
	} `json:"context"`
	Auth struct {
		AccessToken  string `json:"access_token,omitempty"`
		RefreshToken string `json:"refresh_token,omitempty"`
	} `json:"auth,omitempty"`
	HQ struct {
		BaseURL   string `json:"base_url,omitempty"`
		Port      string `json:"port,omitempty"`
		UserID    string `json:"user_id,omitempty"`
		SessionID string `json:"session_id,omitempty"`
	} `json:"hq,omitempty"`
	Tools      []continueChatTool     `json:"tools,omitempty"`
	ToolChoice continueChatToolChoice `json:"tool_choice,omitempty"`
	Mode       string                 `json:"mode"`
}

type continueToolRequest struct {
	Path       string             `json:"path"`
	Query      string             `json:"query"`
	StartLine  int                `json:"start_line"`
	EndLine    int                `json:"end_line"`
	Recursive  bool               `json:"recursive"`
	Limit      int                `json:"limit"`
	SessionID  string             `json:"session_id"`
	Workspace  string             `json:"workspace"`
	Selection  string             `json:"selection"`
	ActiveFile string             `json:"active_file"`
	Find       string             `json:"find"`
	Replace    string             `json:"replace"`
	Edits      []continueTextEdit `json:"edits"`
}

type continueTextEdit struct {
	Find    string `json:"find"`
	Replace string `json:"replace"`
}

type localRAGService struct {
	service             ai.Agent[map[string]any]
	shellSessions       *ptyShellSessionManager
	continueState       *continueBridgeSessionStore
	continueWorkers     *continueBridgeWorkerStore
	clineTaskContextsMu sync.Mutex
	clineTaskContexts   *clineTaskContextStore
	clineBackend        ideworker.Backend
}

func (s *localRAGService) ensureClineTaskContextStore() *clineTaskContextStore {
	if s == nil {
		return nil
	}
	s.clineTaskContextsMu.Lock()
	defer s.clineTaskContextsMu.Unlock()
	if s.clineTaskContexts == nil {
		s.clineTaskContexts = &clineTaskContextStore{contexts: make(map[string]clineTaskContext)}
	}
	return s.clineTaskContexts
}

func (s *localRAGService) Close() error {
	if s == nil || s.clineBackend == nil {
		return nil
	}
	if closer, ok := s.clineBackend.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

var globalShellSessionManager = newPTYShellSessionManager()

func newShellSessionManager() *ptyShellSessionManager {
	return newPTYShellSessionManager()
}

// GetGlobalShellSessionManager returns the global shell session manager instance
func GetGlobalShellSessionManager() *ptyShellSessionManager {
	return globalShellSessionManager
}

func newLocalRAGService(configPath string) (*localRAGService, error) {
	if configPath == "" {
		return nil, fmt.Errorf("config path is required")
	}

	if svc, err := autoProvisionSharedCopilotService(context.Background()); err == nil {
		log.Printf("daemon newLocalRAGService: using auto-provisioned shared service")
		localService := &localRAGService{service: svc, shellSessions: newPTYShellSessionManager(), continueState: newContinueBridgeSessionStore(), continueWorkers: newContinueBridgeWorkerStore()}
		localService.clineBackend = newConfiguredIDEWorkerBackend(localService)
		injectIDEWorkerBackend(localService.service, localService.clineBackend)
		return localService, nil
	} else {
		log.Printf("daemon newLocalRAGService: shared RAG runtime unavailable: %v", err)
		return nil, fmt.Errorf("shared RAG runtime unavailable: %w", err)
	}
}

func autoProvisionSharedCopilotService(ctx context.Context) (ai.Agent[map[string]any], error) {
	log.Printf("daemon autoProvisionSharedCopilotService: starting")
	for _, agentID := range []string{"copilot", "sql_core"} {
		if agentSvc, ok := sopbackend.GetLoadedAgents()[agentID]; ok && agentSvc != nil {
			injectShellSessionManager(agentSvc)
			return agentSvc, nil
		}
	}
	for _, agentSvc := range sopbackend.GetLoadedAgents() {
		if agentSvc != nil {
			log.Printf("daemon autoProvisionSharedCopilotService: using fallback loaded agent")
			injectShellSessionManager(agentSvc)
			return agentSvc, nil
		}
	}

	policy := agent.AutomationPolicy{
		Enabled:               true,
		AllowShellCommands:    true,
		AllowEmail:            false,
		RequireApproval:       true,
		DefaultTimeoutSeconds: 600,
		MaxTimeoutSeconds:     1800,
	}
	if err := sopbackend.Initialize(ctx, sopbackend.RuntimeOptions{
		EnableLocalAutomation: true,
		AutomationPolicy:      &policy,
		EnableStoresFeature:   true,
	}); err != nil {
		log.Printf("daemon autoProvisionSharedCopilotService: shared backend bootstrap failed: %v", err)
	} else {
		log.Printf("daemon autoProvisionSharedCopilotService: shared backend initialized")
		for _, agentID := range []string{"copilot", "sql_core"} {
			if agentSvc, ok := sopbackend.GetLoadedAgents()[agentID]; ok && agentSvc != nil {
				log.Printf("daemon autoProvisionSharedCopilotService: using initialized agent: %s", agentID)
				injectShellSessionManager(agentSvc)
				return agentSvc, nil
			}
		}
		for _, agentSvc := range sopbackend.GetLoadedAgents() {
			if agentSvc != nil {
				log.Printf("daemon autoProvisionSharedCopilotService: using initialized fallback agent")
				injectShellSessionManager(agentSvc)
				return agentSvc, nil
			}
		}
	}

	candidates := []string{
		"ai/data/copilot_pipeline.json",
		filepath.Join(cwd(), "ai/data/copilot_pipeline.json"),
		filepath.Join(cwd(), "..", "ai/data/copilot_pipeline.json"),
		filepath.Join(cwd(), "..", "..", "ai/data/copilot_pipeline.json"),
		filepath.Join("/Volumes/T7/sop-cloud/lib/sop", "ai/data/copilot_pipeline.json"),
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		cfg, err := agent.LoadConfigFromFile(candidate)
		if err != nil {
			return nil, fmt.Errorf("load shared pipeline config %s: %w", candidate, err)
		}
		svc, err := agent.NewFromConfig(ctx, *cfg, agent.Dependencies{})
		if err != nil {
			return nil, fmt.Errorf("build shared pipeline agent %s: %w", candidate, err)
		}
		injectShellSessionManager(svc)
		return svc, nil
	}

	return nil, fmt.Errorf("shared copilot pipeline config not found")
}

// injectShellSessionManager injects the global shell session manager into a CopilotAgent
func injectShellSessionManager(agentSvc ai.Agent[map[string]any]) {
	if copilotAgent, ok := agentSvc.(*agent.CopilotAgent); ok {
		copilotAgent.InjectShellSessionManager(globalShellSessionManager)
		log.Printf("daemon: injected shell session manager into CopilotAgent")
		return
	}
	if service, ok := agentSvc.(interface {
		RegisteredAgents() map[string]ai.Agent[map[string]any]
	}); ok {
		injected := false
		for _, ag := range service.RegisteredAgents() {
			if copilotAgent, ok := ag.(*agent.CopilotAgent); ok {
				copilotAgent.InjectShellSessionManager(globalShellSessionManager)
				log.Printf("daemon: injected shell session manager into CopilotAgent via RegisteredAgents")
				injected = true
			}
		}
		if injected {
			return
		}
	}
	if service, ok := agentSvc.(interface {
		RegisteredAgent(string) ai.Agent[map[string]any]
	}); ok {
		for _, id := range []string{"copilot", "sql_core"} {
			if copilotAgent, ok := service.RegisteredAgent(id).(*agent.CopilotAgent); ok {
				copilotAgent.InjectShellSessionManager(globalShellSessionManager)
				log.Printf("daemon: injected shell session manager into CopilotAgent (%s)", id)
			}
		}
	}
}

type continueGuidanceProvider interface {
	BuildContinueDelegationGuidance(ctx context.Context, query, requestedPhase string, variables map[string]any) (agent.ContinueDelegationGuidance, error)
}

type continueKnowledgeBaseGuidanceProvider interface {
	BuildContinueKnowledgeBaseGuidance(ctx context.Context, query, requestedPhase string, variables map[string]any) (agent.ContinueDelegationGuidance, error)
}

type continueMemoryGuidanceProvider interface {
	BuildContinueMemoryGuidance(ctx context.Context, query, requestedPhase string, variables map[string]any) (agent.ContinueDelegationGuidance, error)
}

func (s *localRAGService) askBridgeChat(baseCtx context.Context, req bridgeAskRequest, eventStreamer func(string, any)) (string, error) {
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	if s == nil || s.service == nil {
		return "", fmt.Errorf("local RAG service is not initialized")
	}
	ctx := baseCtx
	cfg := ai.NewConfigMap()
	variables := cloneBridgeVariables(req.Variables)
	if req.ProviderName != "" {
		variables["llm_provider"] = req.ProviderName
	}
	if req.ModelName != "" {
		variables["llm_model"] = req.ModelName
	}
	if req.BaseURL != "" {
		variables["llm_url"] = req.BaseURL
	}
	sessionID := continueSessionContextKey(req.SessionID, req.HQSessionID, req.HQUserID)
	userID := firstNonEmpty(req.HQUserID, req.SessionID)
	payload := &ai.SessionPayload{CurrentUserQuery: req.UserPrompt, SessionID: sessionID, UserID: userID, Variables: variables}
	ctx = context.WithValue(ctx, "session_payload", payload)
	s.enrichContinueGuidanceVariables(ctx, req.UserPrompt, variables)
	cfg.Set("payload", payload)
	if req.AccessToken != "" {
		cfg.Set("access_token", req.AccessToken)
	}
	if req.RefreshToken != "" {
		cfg.Set("refresh_token", req.RefreshToken)
	}
	if req.HQBaseURL != "" {
		cfg.Set("hq_base_url", normalizeHQBaseURL(req.HQBaseURL, req.HQPort))
	}
	if req.HQPort != "" {
		cfg.Set("hq_port", req.HQPort)
	}
	if req.HQUserID != "" {
		cfg.Set("hq_user_id", req.HQUserID)
	}
	if req.HQSessionID != "" {
		cfg.Set("hq_session_id", req.HQSessionID)
	}
	if req.ProviderName != "" || req.APIKey != "" || req.ModelName != "" || req.BaseURL != "" {
		providerDetails := &agent.ProviderDetails{Provider: req.ProviderName, APIKey: req.APIKey, Model: req.ModelName, BaseURL: req.BaseURL}
		cfg.Set("provider_details", providerDetails)
		if req.ProviderName != "" {
			providerCtxValue := req.ProviderName
			if req.ModelName != "" {
				providerCtxValue = req.ProviderName + ":" + req.ModelName
			}
			ctx = context.WithValue(ctx, ai.CtxKeyProvider, providerCtxValue)
		}
		if req.APIKey != "" {
			ctx = context.WithValue(ctx, ai.CtxKeyAPIKey, req.APIKey)
		}
		if req.BaseURL != "" {
			ctx = context.WithValue(ctx, ai.CtxKeyBaseURL, req.BaseURL)
		}
	}
	if eventStreamer != nil {
		ctx = context.WithValue(ctx, ai.CtxKeyEventStreamer, eventStreamer)
	}
	chatReq := continueChatRequest{SessionID: req.SessionID}
	chatReq.Auth.AccessToken = req.AccessToken
	chatReq.Auth.RefreshToken = req.RefreshToken
	chatReq.HQ.BaseURL = req.HQBaseURL
	chatReq.HQ.Port = req.HQPort
	chatReq.HQ.UserID = req.HQUserID
	chatReq.HQ.SessionID = req.HQSessionID
	s.storeContinueSessionContext(chatReq)
	return s.service.Ask(ctx, req.FullPrompt, cfg)
}

func (s *localRAGService) enrichContinueGuidanceVariables(ctx context.Context, query string, variables map[string]any) {
	if s == nil || s.service == nil || len(variables) == 0 {
		return
	}
	requestedPhase, _ := variables["requested_phase"].(string)
	merged := agent.ContinueDelegationGuidance{}
	if provider, ok := s.service.(continueGuidanceProvider); ok {
		if guidance, err := provider.BuildContinueDelegationGuidance(ctx, query, requestedPhase, variables); err == nil {
			merged = mergeContinueDelegationGuidance(merged, guidance)
		}
	}
	if provider, ok := s.service.(continueKnowledgeBaseGuidanceProvider); ok {
		if guidance, err := provider.BuildContinueKnowledgeBaseGuidance(ctx, query, requestedPhase, variables); err == nil {
			merged = mergeContinueDelegationGuidance(merged, guidance)
		}
	}
	if provider, ok := s.service.(continueMemoryGuidanceProvider); ok {
		if guidance, err := provider.BuildContinueMemoryGuidance(ctx, query, requestedPhase, variables); err == nil {
			merged = mergeContinueDelegationGuidance(merged, guidance)
		}
	}
	if guidance := s.buildContinueExecutionMemoryGuidance(ctx, query, requestedPhase, variables); len(guidance.TeamPractices) > 0 || len(guidance.ReferenceContext) > 0 || len(guidance.ImplementationNotes) > 0 {
		merged = mergeContinueDelegationGuidance(merged, guidance)
	}
	applyContinueGuidanceVariables(variables, merged)
}

func (s *localRAGService) buildContinueExecutionMemoryGuidance(ctx context.Context, query, requestedPhase string, variables map[string]any) agent.ContinueDelegationGuidance {
	if s == nil {
		return agent.ContinueDelegationGuidance{}
	}
	payload := ai.GetSessionPayload(ctx)
	if payload == nil {
		return agent.ContinueDelegationGuidance{}
	}
	sessionKey := continueSessionContextKey(payload.SessionID, "", payload.UserID)
	if strings.TrimSpace(sessionKey) == "" {
		return agent.ContinueDelegationGuidance{}
	}
	daemonSessionStore.mu.Lock()
	stored, ok := daemonSessionStore.data[sessionKey]
	daemonSessionStore.mu.Unlock()
	if !ok || strings.TrimSpace(stored.ContinueMemorySummary) == "" {
		return agent.ContinueDelegationGuidance{}
	}
	brief := strings.TrimSpace(stored.ContinueMemorySummary)
	return agent.ContinueDelegationGuidance{
		TeamPractices: []string{
			"Preserve the last recorded Continue execution memory brief when starting the next coding turn.",
			"Carry forward the prior Continue outcome, validation, and file-change summary before making new edits.",
		},
		SuccessCriteria: []string{
			"Use the previous Continue execution brief when it is relevant to the current coding objective.",
		},
		Constraints: []string{
			"Do not discard the recorded Continue memory brief when preparing the next turn.",
		},
		ImplementationNotes:  []string{brief},
		ExpectedResultFormat: "structured_execution_result",
		ReferenceContext: []agent.ContinueDelegationReference{{
			Kind:    "continue_execution_memory",
			Source:  "daemon_session_store",
			Title:   "Continue execution memory brief",
			Content: brief,
			Metadata: map[string]any{
				"query":                         strings.TrimSpace(query),
				"requested_phase":               strings.TrimSpace(requestedPhase),
				"continue_memory_updated_at_ms": stored.ContinueMemoryUpdatedAtMS,
			},
		}},
		Metadata: map[string]any{
			"source": "continue_execution_memory",
		},
	}
}

func mergeContinueDelegationGuidance(base, overlay agent.ContinueDelegationGuidance) agent.ContinueDelegationGuidance {
	if len(overlay.TeamPractices) > 0 {
		base.TeamPractices = append(base.TeamPractices, overlay.TeamPractices...)
	}
	if len(overlay.SuccessCriteria) > 0 {
		base.SuccessCriteria = append(base.SuccessCriteria, overlay.SuccessCriteria...)
	}
	if len(overlay.Constraints) > 0 {
		base.Constraints = append(base.Constraints, overlay.Constraints...)
	}
	if len(overlay.ImplementationNotes) > 0 {
		base.ImplementationNotes = append(base.ImplementationNotes, overlay.ImplementationNotes...)
	}
	if strings.TrimSpace(overlay.ExpectedResultFormat) != "" {
		base.ExpectedResultFormat = strings.TrimSpace(overlay.ExpectedResultFormat)
	}
	if len(overlay.ReferenceContext) > 0 {
		base.ReferenceContext = append(base.ReferenceContext, overlay.ReferenceContext...)
	}
	if len(overlay.Metadata) > 0 {
		if base.Metadata == nil {
			base.Metadata = map[string]any{}
		}
		if source, ok := overlay.Metadata["source"].(string); ok && strings.TrimSpace(source) != "" {
			source = strings.TrimSpace(source)
			if existing, ok := base.Metadata["sources"].([]string); ok {
				base.Metadata["sources"] = appendUniqueStrings(existing, source)
			} else {
				base.Metadata["sources"] = appendUniqueStrings(nil, source)
			}
			if _, ok := base.Metadata["source"].(string); !ok || strings.TrimSpace(base.Metadata["source"].(string)) == "" {
				base.Metadata["source"] = source
			}
		}
		for key, value := range overlay.Metadata {
			if key == "source" {
				continue
			}
			base.Metadata[key] = value
		}
	}
	return base
}

func applyContinueGuidanceVariables(variables map[string]any, guidance agent.ContinueDelegationGuidance) {
	clearContinueGuidanceVariables(variables)
	if len(guidance.TeamPractices) > 0 {
		variables["continue_team_practices"] = append([]string(nil), guidance.TeamPractices...)
	}
	if len(guidance.SuccessCriteria) > 0 {
		variables["continue_success_criteria"] = append([]string(nil), guidance.SuccessCriteria...)
	}
	if len(guidance.Constraints) > 0 {
		variables["continue_constraints"] = append([]string(nil), guidance.Constraints...)
	}
	if len(guidance.ImplementationNotes) > 0 {
		variables["continue_implementation_notes"] = append([]string(nil), guidance.ImplementationNotes...)
	}
	if strings.TrimSpace(guidance.ExpectedResultFormat) != "" {
		variables["continue_expected_result_format"] = strings.TrimSpace(guidance.ExpectedResultFormat)
	}
	if len(guidance.ReferenceContext) > 0 {
		refs := make([]any, 0, len(guidance.ReferenceContext))
		for _, ref := range guidance.ReferenceContext {
			refs = append(refs, map[string]any{
				"kind":     ref.Kind,
				"source":   ref.Source,
				"title":    ref.Title,
				"content":  ref.Content,
				"metadata": cloneBridgeVariables(ref.Metadata),
			})
		}
		variables["continue_reference_context"] = refs
	}
	if len(guidance.ImplementationNotes) > 0 {
		variables["continue_execution_memory_context"] = append([]string(nil), guidance.ImplementationNotes...)
	}
	if len(guidance.Metadata) > 0 {
		variables["continue_guidance_metadata"] = cloneBridgeVariables(guidance.Metadata)
	}
}

func clearContinueGuidanceVariables(variables map[string]any) {
	delete(variables, "continue_team_practices")
	delete(variables, "continue_success_criteria")
	delete(variables, "continue_constraints")
	delete(variables, "continue_implementation_notes")
	delete(variables, "continue_expected_result_format")
	delete(variables, "continue_reference_context")
	delete(variables, "continue_execution_memory_context")
	delete(variables, "continue_guidance_metadata")
}

func cloneBridgeVariables(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(src))
	for key, value := range src {
		cloned[key] = value
	}
	return cloned
}

func (s *localRAGService) handleContinueChat(w http.ResponseWriter, r *http.Request) {
	s.handleBridgeChat(w, r, continueBridgeTransport{})
}

func continueSessionContextKey(sessionID, hqSessionID, hqUserID string) string {
	if trimmed := strings.TrimSpace(sessionID); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(hqSessionID); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(hqUserID); trimmed != "" {
		return trimmed
	}
	return "continue-bridge"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (s *localRAGService) storeContinueSessionContext(req continueChatRequest) {
	_ = s
	sessionKey := continueSessionContextKey(req.SessionID, req.HQ.SessionID, req.HQ.UserID)
	accessToken := strings.TrimSpace(req.Auth.AccessToken)
	refreshToken := strings.TrimSpace(req.Auth.RefreshToken)
	hqPort := strings.TrimSpace(req.HQ.Port)
	hqBaseURL := normalizeHQBaseURL(strings.TrimSpace(req.HQ.BaseURL), hqPort)
	normalizedCredentials, credentialsErr := normalizeAndValidateLLMCredentials(req.LLMCredentials)
	if credentialsErr != nil {
		log.Printf("continue session context: ignoring invalid llm_credentials payload: %v", credentialsErr)
		normalizedCredentials = nil
	}
	if accessToken == "" && refreshToken == "" && hqBaseURL == "" && strings.TrimSpace(req.HQ.UserID) == "" && strings.TrimSpace(req.HQ.SessionID) == "" {
		if strings.TrimSpace(req.Provider) == "" && strings.TrimSpace(req.APIKey) == "" && strings.TrimSpace(req.Model) == "" && strings.TrimSpace(req.URL) == "" && len(normalizedCredentials) == 0 {
			return
		}
	}

	var storedSessionContext daemonSessionContext
	daemonSessionStore.mu.Lock()
	if stored, ok := daemonSessionStore.data[sessionKey]; ok {
		storedSessionContext = stored
	}
	mergedCredentials, mergedPoolVersion, mergedPoolUpdatedAtMS, mergedPoolUpdatedBy, _ := mergeLLMPoolState(
		storedSessionContext,
		normalizedCredentials,
		req.LLMPoolVersion,
		req.LLMPoolUpdatedAtMS,
		req.LLMPoolUpdatedBy,
	)
	daemonSessionStore.data[sessionKey] = daemonSessionContext{
		AccessToken:               firstNonEmpty(accessToken, storedSessionContext.AccessToken),
		RefreshToken:              firstNonEmpty(refreshToken, storedSessionContext.RefreshToken),
		HQBaseURL:                 firstNonEmpty(hqBaseURL, storedSessionContext.HQBaseURL),
		HQPort:                    firstNonEmpty(hqPort, storedSessionContext.HQPort),
		HQUserID:                  firstNonEmpty(req.HQ.UserID, storedSessionContext.HQUserID),
		HQSessionID:               firstNonEmpty(req.HQ.SessionID, storedSessionContext.HQSessionID),
		LLMProvider:               firstNonEmpty(req.Provider, storedSessionContext.LLMProvider),
		LLMAPIKey:                 firstNonEmpty(req.APIKey, storedSessionContext.LLMAPIKey),
		LLMModel:                  firstNonEmpty(req.Model, storedSessionContext.LLMModel),
		LLMURL:                    firstNonEmpty(req.URL, storedSessionContext.LLMURL),
		LLMCredentials:            cloneLLMCredentialsMap(mergedCredentials),
		LLMPoolVersion:            mergedPoolVersion,
		LLMPoolUpdatedAtMS:        mergedPoolUpdatedAtMS,
		LLMPoolUpdatedBy:          mergedPoolUpdatedBy,
		ThinkingLevel:             storedSessionContext.ThinkingLevel,
		ContinueMemorySummary:     storedSessionContext.ContinueMemorySummary,
		ContinueMemoryUpdatedAtMS: storedSessionContext.ContinueMemoryUpdatedAtMS,
		LastUpdated:               time.Now().UTC(),
	}
	daemonSessionStore.mu.Unlock()
	if err := persistDaemonLLMPoolState(); err != nil {
		log.Printf("Warning: failed to persist daemon LLM pool state from Continue context sync: %v", err)
	}
}

func (s *localRAGService) maybeStoreContinueTeamPractice(ctx context.Context, message string) (bool, string, error) {
	practice, ok := extractContinueTeamPracticeContent(message)
	if !ok {
		return false, "", nil
	}
	if strings.TrimSpace(practice) == "" {
		return true, "", fmt.Errorf("team practice content is required")
	}
	agentSvc := s.continueTeamPracticeAgent()
	if agentSvc == nil {
		return true, "", fmt.Errorf("continue team practice storage is unavailable")
	}
	pattern, err := agentSvc.StoreContinueTeamPractice(ctx, practice)
	if err != nil {
		return true, "", err
	}
	return true, fmt.Sprintf("Stored team practice in %s.", pattern.BuildCategoryPath()), nil
}

func (s *localRAGService) continueTeamPracticeAgent() *agent.CopilotAgent {
	if s != nil {
		if provider, ok := s.service.(interface {
			RegisteredAgents() map[string]ai.Agent[map[string]any]
		}); ok {
			for _, ag := range provider.RegisteredAgents() {
				if copilotAgent, ok := ag.(*agent.CopilotAgent); ok {
					return copilotAgent
				}
			}
		}
		if provider, ok := s.service.(interface {
			RegisteredAgent(string) ai.Agent[map[string]any]
		}); ok {
			for _, id := range []string{"copilot", "sql_core"} {
				if copilotAgent, ok := provider.RegisteredAgent(id).(*agent.CopilotAgent); ok {
					return copilotAgent
				}
			}
		}
		if copilotAgent, ok := s.service.(*agent.CopilotAgent); ok {
			return copilotAgent
		}
	}
	for _, loaded := range sopbackend.GetLoadedAgents() {
		if copilotAgent, ok := loaded.(*agent.CopilotAgent); ok {
			return copilotAgent
		}
	}
	return nil
}

func extractContinueTeamPracticeContent(message string) (string, bool) {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return "", false
	}
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"team best practice", "team practice"} {
		if lower == prefix {
			return "", true
		}
		if strings.HasPrefix(lower, prefix) {
			content := strings.TrimSpace(trimmed[len(prefix):])
			content = strings.TrimLeft(content, ":-—– \t\r\n")
			return content, true
		}
	}
	return "", false
}

func (s *localRAGService) handleContinueTool(w http.ResponseWriter, r *http.Request, toolName string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req continueToolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Invalid JSON body: " + err.Error()})
		return
	}

	handlers := s.continueToolHandlers()
	handler, ok := handlers[toolName]
	if !ok {
		respondJSON(w, http.StatusNotImplemented, map[string]any{"ok": false, "error": "unsupported tool"})
		return
	}

	result, err := handler(req)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true, "tool": toolName, "result": result})
}

func readContinueFile(path string, startLine, endLine int) (map[string]any, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return nil, fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(cleanPath) {
		cleanPath = filepath.Clean(filepath.Join(cwd(), cleanPath))
	}
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	if startLine <= 0 {
		startLine = 1
	}
	if endLine <= 0 || endLine > len(lines) {
		endLine = len(lines)
	}
	selected := lines[startLine-1 : endLine]
	return map[string]any{
		"path":       cleanPath,
		"content":    strings.Join(selected, "\n"),
		"start_line": startLine,
		"end_line":   endLine,
	}, nil
}

func searchContinueCode(query, path string, limit int) (map[string]any, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query is required")
	}
	if limit <= 0 {
		limit = 20
	}
	root := strings.TrimSpace(path)
	if root == "" {
		root = cwd()
	}
	if !filepath.IsAbs(root) {
		root = filepath.Clean(filepath.Join(cwd(), root))
	}
	var hits []map[string]any
	filepath.WalkDir(root, func(currentPath string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(currentPath, ".git") || strings.HasSuffix(currentPath, ".png") || strings.HasSuffix(currentPath, ".jpg") || strings.HasSuffix(currentPath, ".jpeg") || strings.HasSuffix(currentPath, ".pdf") {
			return nil
		}
		data, readErr := os.ReadFile(currentPath)
		if readErr != nil {
			return nil
		}
		text := string(data)
		if strings.Contains(text, query) {
			hits = append(hits, map[string]any{"path": currentPath, "text": text[:min(200, len(text))]})
			if len(hits) >= limit {
				return filepath.SkipAll
			}
		}
		return nil
	})
	return map[string]any{"hits": hits}, nil
}

func listContinueFiles(path string, recursive bool) (map[string]any, error) {
	root := strings.TrimSpace(path)
	if root == "" {
		root = cwd()
	}
	if !filepath.IsAbs(root) {
		root = filepath.Clean(filepath.Join(cwd(), root))
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, entry := range entries {
		result = append(result, entry.Name())
	}
	return map[string]any{"path": root, "files": result, "recursive": recursive}, nil
}

func applyContinuePatch(path, patch string) (map[string]any, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return nil, fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(cleanPath) {
		cleanPath = filepath.Clean(filepath.Join(cwd(), cleanPath))
	}
	if strings.TrimSpace(patch) == "" {
		return nil, fmt.Errorf("patch is required")
	}
	originalContent, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, err
	}
	updatedContent, mode, err := applyContinuePatchContent(string(originalContent), patch)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(cleanPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(cleanPath, []byte(updatedContent), 0o644); err != nil {
		return nil, err
	}
	return map[string]any{"path": cleanPath, "applied": true, "content": updatedContent, "mode": mode}, nil
}

func previewContinuePatch(path, patch string) (map[string]any, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return nil, fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(cleanPath) {
		cleanPath = filepath.Clean(filepath.Join(cwd(), cleanPath))
	}
	if strings.TrimSpace(patch) == "" {
		return nil, fmt.Errorf("patch is required")
	}
	oldContent, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, err
	}
	updatedContent, mode, err := applyContinuePatchContent(string(oldContent), patch)
	if err != nil {
		return nil, err
	}
	preview := fmt.Sprintf("--- %s\n+++ %s\n-%s\n+%s\n", cleanPath, cleanPath, string(oldContent), updatedContent)
	return map[string]any{"path": cleanPath, "preview": preview, "applied": false, "mode": mode}, nil
}

func applyContinuePatchContent(original, patch string) (string, string, error) {
	if !looksLikeUnifiedDiffPatch(patch) {
		return patch, "replace", nil
	}
	updated, err := applyUnifiedDiffHunks(original, patch)
	if err != nil {
		return "", "", err
	}
	return updated, "unified_hunk", nil
}

func resolveContinueEditPath(path, activeFile string) string {
	if trimmed := strings.TrimSpace(path); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(activeFile)
}

func applyContinueSingleFindAndReplace(path, find, replace string) (map[string]any, error) {
	cleanPath, originalContent, err := readContinueEditableFile(path)
	if err != nil {
		return nil, err
	}
	updatedContent, replacements, err := replaceOnceOrFail(string(originalContent), find, replace)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(cleanPath, []byte(updatedContent), 0o644); err != nil {
		return nil, err
	}
	return map[string]any{"path": cleanPath, "applied": true, "content": updatedContent, "mode": "single_find_and_replace", "replacements": replacements}, nil
}

func applyContinueMultiEdit(path string, edits []continueTextEdit) (map[string]any, error) {
	cleanPath, originalContent, err := readContinueEditableFile(path)
	if err != nil {
		return nil, err
	}
	updatedContent := string(originalContent)
	totalReplacements := 0
	for _, edit := range edits {
		nextContent, replacements, err := replaceOnceOrFail(updatedContent, edit.Find, edit.Replace)
		if err != nil {
			return nil, err
		}
		updatedContent = nextContent
		totalReplacements += replacements
	}
	if err := os.WriteFile(cleanPath, []byte(updatedContent), 0o644); err != nil {
		return nil, err
	}
	return map[string]any{"path": cleanPath, "applied": true, "content": updatedContent, "mode": "multi_edit", "replacements": totalReplacements}, nil
}

func readContinueEditableFile(path string) (string, []byte, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return "", nil, fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(cleanPath) {
		cleanPath = resolveContinueEditablePath(cleanPath)
	}
	originalContent, err := os.ReadFile(cleanPath)
	if err != nil {
		return "", nil, err
	}
	return cleanPath, originalContent, nil
}

func resolveContinueEditablePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed)
	}
	if trimmed == ".continue-repo" || strings.HasPrefix(trimmed, ".continue-repo"+string(os.PathSeparator)) {
		return filepath.Clean(filepath.Join(resolveContinueRepoRootFrom(cwd()), trimmed))
	}
	return filepath.Clean(filepath.Join(cwd(), trimmed))
}

func resolveContinueRepoRootFrom(start string) string {
	current := strings.TrimSpace(start)
	if current == "" {
		current = cwd()
	}
	if !filepath.IsAbs(current) {
		current = filepath.Clean(filepath.Join(cwd(), current))
	}
	for {
		if info, err := os.Stat(filepath.Join(current, ".continue-repo")); err == nil && info.IsDir() {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return cwd()
		}
		current = parent
	}
}

func replaceOnceOrFail(content, find, replace string) (string, int, error) {
	needle := strings.TrimSpace(find)
	if needle == "" {
		return "", 0, fmt.Errorf("find is required")
	}
	count := strings.Count(content, find)
	if count == 0 {
		return "", 0, fmt.Errorf("find text not found")
	}
	if count > 1 {
		return "", 0, fmt.Errorf("find text matched %d locations; provide a more specific snippet or use multi_edit with narrower edits", count)
	}
	return strings.Replace(content, find, replace, 1), 1, nil
}

func looksLikeUnifiedDiffPatch(patch string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "@@ ") {
			return true
		}
	}
	return false
}

func applyUnifiedDiffHunks(original, patch string) (string, error) {
	originalLines, trailingNewline := splitContentLines(original)
	patchLines := strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n")

	result := make([]string, 0, len(originalLines))
	originalIdx := 0
	for i := 0; i < len(patchLines); i++ {
		line := patchLines[i]
		if strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "@@ ") {
			continue
		}

		match := unifiedHunkHeaderPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) < 3 {
			return "", fmt.Errorf("invalid unified diff hunk header: %s", line)
		}
		oldStart, err := strconv.Atoi(match[1])
		if err != nil {
			return "", fmt.Errorf("invalid unified diff hunk start: %w", err)
		}
		copyUntil := oldStart - 1
		if copyUntil < 0 {
			copyUntil = 0
		}
		if copyUntil > len(originalLines) {
			return "", fmt.Errorf("hunk start %d is beyond file length %d", oldStart, len(originalLines))
		}
		for originalIdx < copyUntil {
			result = append(result, originalLines[originalIdx])
			originalIdx++
		}

		for i+1 < len(patchLines) {
			nextLine := patchLines[i+1]
			if strings.HasPrefix(strings.TrimSpace(nextLine), "@@ ") {
				break
			}
			i++
			if strings.HasPrefix(nextLine, "\\") {
				continue
			}
			if nextLine == "" {
				return "", fmt.Errorf("invalid unified diff payload line")
			}
			switch nextLine[0] {
			case ' ':
				if originalIdx >= len(originalLines) || originalLines[originalIdx] != nextLine[1:] {
					return "", fmt.Errorf("context mismatch while applying unified diff")
				}
				result = append(result, originalLines[originalIdx])
				originalIdx++
			case '-':
				if originalIdx >= len(originalLines) || originalLines[originalIdx] != nextLine[1:] {
					return "", fmt.Errorf("delete mismatch while applying unified diff")
				}
				originalIdx++
			case '+':
				result = append(result, nextLine[1:])
			default:
				return "", fmt.Errorf("invalid unified diff line prefix: %q", nextLine[:1])
			}
		}
	}

	for originalIdx < len(originalLines) {
		result = append(result, originalLines[originalIdx])
		originalIdx++
	}

	return joinContentLines(result, trailingNewline), nil
}

func splitContentLines(content string) ([]string, bool) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	trailing := strings.HasSuffix(normalized, "\n")
	trimmed := normalized
	if trailing {
		trimmed = strings.TrimSuffix(trimmed, "\n")
	}
	if trimmed == "" {
		return nil, trailing
	}
	return strings.Split(trimmed, "\n"), trailing
}

func joinContentLines(lines []string, trailingNewline bool) string {
	content := strings.Join(lines, "\n")
	if trailingNewline {
		return content + "\n"
	}
	return content
}

func runContinueTest(command, sessionID string) (map[string]any, error) {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return nil, fmt.Errorf("command is required")
	}
	if sessionID == "" {
		sessionID = "continue-tests"
	}
	manager := newPTYShellSessionManager()
	output, stillRunning, err := manager.Run(sessionID, trimmed, 20*time.Second)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"command":       trimmed,
		"output":        output,
		"still_running": stillRunning,
		"exit_code":     0,
	}, nil
}

func (s *localRAGService) delegateContinueAgent(query, sessionID string) (map[string]any, error) {
	if s == nil || s.service == nil {
		return nil, fmt.Errorf("local RAG service is not initialized")
	}
	answer, err := s.service.Ask(context.Background(), query, ai.NewConfigMap())
	if err != nil {
		return nil, err
	}
	return map[string]any{"session_id": sessionID, "answer": answer}, nil
}

func (s *localRAGService) handleAsk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req AskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, AskResponse{Error: "Invalid JSON body: " + err.Error()})
		return
	}
	if req.Query == "" {
		respondJSON(w, http.StatusBadRequest, AskResponse{Error: "Query cannot be empty"})
		return
	}
	if s == nil || s.service == nil {
		respondJSON(w, http.StatusServiceUnavailable, AskResponse{Error: "local RAG service is not initialized"})
		return
	}
	requestCredentials, requestCredentialsErr := normalizeAndValidateLLMCredentials(req.LLMCredentials)
	if requestCredentialsErr != nil {
		respondJSON(w, http.StatusBadRequest, AskResponse{Error: requestCredentialsErr.Error()})
		return
	}

	// Setup streaming (same pattern as HQ's /api/ai/chat)
	// Always initialize streaming - client will receive NDJSON events
	sendEvent, hasStreamedContent := sopbackend.SetupStream(w)

	ctx := r.Context()
	sessionKey := strings.TrimSpace(req.HQ.SessionID)
	if sessionKey == "" {
		sessionKey = strings.TrimSpace(req.HQ.UserID)
	}
	if sessionKey == "" {
		sessionKey = "local-daemon"
	}

	var storedSessionContext daemonSessionContext
	daemonSessionStore.mu.Lock()
	if stored, ok := daemonSessionStore.data[sessionKey]; ok {
		storedSessionContext = stored
	}
	daemonSessionStore.mu.Unlock()

	payload := &ai.SessionPayload{
		CurrentUserQuery: req.Query,
		UserID:           strings.TrimSpace(req.HQ.UserID),
		SessionID:        sessionKey, // Use the already-determined sessionKey to ensure consistency
		CurrentDB:        "system",   // Default to system database for daemon mode
		Variables:        map[string]any{},
	}
	if len(req.InputParts) > 0 {
		payload.Variables["input_parts"] = req.InputParts
	}
	if payload.UserID == "" && storedSessionContext.HQUserID != "" {
		payload.UserID = storedSessionContext.HQUserID
	}
	if s.shellSessions == nil {
		s.shellSessions = newPTYShellSessionManager()
	}
	sessionID := strings.TrimSpace(strings.TrimSpace(r.Header.Get("X-SOP-Session-ID")))
	if sessionID == "" {
		sessionID = payload.SessionID
	}
	log.Printf("[RAG] Resolving working directory for session %q", sessionID)

	// Resolve workspace context from shell session PWD if available
	// Try the current session ID first, then fall back to any active shell session
	workspaceRoot, wdErr := s.shellSessions.GetWorkingDirectory(sessionID)
	log.Printf("[RAG] GetWorkingDirectory(%q) returned: workspaceRoot=%q, err=%v", sessionID, workspaceRoot, wdErr)
	if wdErr != nil || workspaceRoot == "" || workspaceRoot == "." {
		// Fallback: try any active shell session
		s.shellSessions.mu.Lock()
		sessionCount := len(s.shellSessions.sessions)
		if sessionCount > 0 {
			logVerbose("No working directory for session %q, trying %d active shell sessions", sessionID, sessionCount)
		}
		for sid := range s.shellSessions.sessions {
			s.shellSessions.mu.Unlock()
			wd, wdErr2 := s.shellSessions.GetWorkingDirectory(sid)
			if wdErr2 == nil && wd != "" && wd != "." {
				workspaceRoot = wd
				logVerbose("Using working directory from shell session %q: %s", sid, workspaceRoot)
				break
			}
			s.shellSessions.mu.Lock()
		}
		if len(s.shellSessions.sessions) > 0 {
			s.shellSessions.mu.Unlock()
		}
		if workspaceRoot == "" || workspaceRoot == "." {
			logVerbose("No working directory found from %d shell sessions", sessionCount)
		}
	} else {
		logVerbose("Using working directory for session %q: %s", sessionID, workspaceRoot)
	}

	daemonCwd, _ := os.Getwd()
	if daemonCwd == "" {
		daemonCwd = "."
	}

	// Ensure PTY session exists for this chat session
	// This allows the user to navigate directories in the terminal, and we'll pick up their CWD
	if (workspaceRoot == "" || workspaceRoot == ".") && sessionID != "" {
		// Ensure a PTY session exists with the chat session ID
		log.Printf("[RAG] Ensuring PTY session %q exists at %s", sessionID, daemonCwd)
		ensuredID, spawnErr := s.shellSessions.EnsureSession(sessionID, daemonCwd)
		log.Printf("[RAG] EnsureSession returned: ensuredID=%q, err=%v", ensuredID, spawnErr)
		if spawnErr == nil && ensuredID != "" {
			// Session exists or was spawned - get its CWD
			if wd, err := s.shellSessions.GetWorkingDirectory(ensuredID); err == nil && wd != "" && wd != "." {
				workspaceRoot = wd
				log.Printf("[RAG] Using working directory from PTY session %q: %s", ensuredID, workspaceRoot)
			}
		} else {
			log.Printf("[RAG] Failed to ensure PTY session: %v", spawnErr)
			// Final fallback: use daemon's CWD
			if daemonCwd != "" && daemonCwd != "." {
				workspaceRoot = daemonCwd
				logVerbose("Using daemon CWD as final fallback: %s", workspaceRoot)
			}
		}
	}

	if workspaceRoot != "" && workspaceRoot != "." {
		payload.Variables["working_dir"] = workspaceRoot
		if daemonCwd != "" && filepath.Clean(workspaceRoot) != filepath.Clean(daemonCwd) {
			payload.Variables["workspace_root"] = workspaceRoot
			payload.ActiveRepo = workspaceRoot
		}
		log.Printf("[RAG] ✓ Set SessionPayload working_dir=%s for session %s", workspaceRoot, sessionID)
	} else {
		log.Printf("[RAG] ✗ Could not determine working directory, workspaceRoot=%q sessionID=%q", workspaceRoot, sessionID)
	}

	// Update session store with latest tokens from this request
	// This ensures daemon always has fresh tokens for renewal
	accessToken := strings.TrimSpace(req.Auth.AccessToken)
	refreshToken := strings.TrimSpace(req.Auth.RefreshToken)
	hqBaseURL := strings.TrimSpace(req.HQ.BaseURL)
	hqPort := strings.TrimSpace(req.HQ.Port)
	if accessToken == "" && refreshToken == "" {
		accessToken = strings.TrimSpace(storedSessionContext.AccessToken)
		refreshToken = strings.TrimSpace(storedSessionContext.RefreshToken)
	}
	if hqBaseURL == "" {
		hqBaseURL = strings.TrimSpace(storedSessionContext.HQBaseURL)
	}
	if hqPort == "" {
		hqPort = strings.TrimSpace(storedSessionContext.HQPort)
	}
	hqBaseURL = normalizeHQBaseURL(hqBaseURL, hqPort)
	selectedCredentials, selectedPoolVersion, selectedPoolUpdatedAtMS, selectedPoolUpdatedBy, _ := mergeLLMPoolState(
		storedSessionContext,
		requestCredentials,
		req.LLMPoolVersion,
		req.LLMPoolUpdatedAtMS,
		req.LLMPoolUpdatedBy,
	)
	if len(selectedCredentials) == 0 {
		selectedCredentials = cloneLLMCredentialsMap(storedSessionContext.LLMCredentials)
	}

	if accessToken != "" || refreshToken != "" || hqBaseURL != "" || storedSessionContext.LLMProvider != "" || storedSessionContext.ThinkingLevel != "" || len(selectedCredentials) > 0 {
		ctxKey := payload.SessionID
		nextProvider := strings.TrimSpace(req.LLMProvider)
		if nextProvider == "" {
			nextProvider = strings.TrimSpace(storedSessionContext.LLMProvider)
		}
		nextAPIKey := strings.TrimSpace(req.LLMAPIKey)
		if nextAPIKey == "" {
			nextAPIKey = strings.TrimSpace(storedSessionContext.LLMAPIKey)
		}
		nextModel := strings.TrimSpace(req.LLMModel)
		if nextModel == "" {
			nextModel = strings.TrimSpace(storedSessionContext.LLMModel)
		}
		nextURL := strings.TrimSpace(req.LLMURL)
		if nextURL == "" {
			nextURL = strings.TrimSpace(storedSessionContext.LLMURL)
		}
		daemonSessionStore.mu.Lock()
		daemonSessionStore.data[ctxKey] = daemonSessionContext{
			AccessToken:               accessToken,
			RefreshToken:              refreshToken,
			HQBaseURL:                 hqBaseURL,
			HQPort:                    hqPort,
			HQUserID:                  strings.TrimSpace(req.HQ.UserID),
			HQSessionID:               payload.SessionID,
			LLMProvider:               nextProvider,
			LLMAPIKey:                 nextAPIKey,
			LLMModel:                  nextModel,
			LLMURL:                    nextURL,
			LLMCredentials:            cloneLLMCredentialsMap(selectedCredentials),
			LLMPoolVersion:            selectedPoolVersion,
			LLMPoolUpdatedAtMS:        selectedPoolUpdatedAtMS,
			LLMPoolUpdatedBy:          selectedPoolUpdatedBy,
			ThinkingLevel:             strings.TrimSpace(storedSessionContext.ThinkingLevel),
			ContinueMemorySummary:     storedSessionContext.ContinueMemorySummary,
			ContinueMemoryUpdatedAtMS: storedSessionContext.ContinueMemoryUpdatedAtMS,
			LastUpdated:               time.Now().UTC(),
		}
		daemonSessionStore.mu.Unlock()
		if err := persistDaemonLLMPoolState(); err != nil {
			log.Printf("Warning: failed to persist daemon LLM pool state from Ask sync: %v", err)
		}
	}

	ctx = context.WithValue(ctx, "session_payload", payload)
	// Inject streaming support into context (same as HQ's /api/ai/chat)
	ctx = sopbackend.InjectSharedRuntimeContext(ctx, sendEvent, w)
	mcpURL, mcpBearerToken := clineMCPContext(r)
	ctx = context.WithValue(ctx, ai.CtxKeyMCPURL, mcpURL)
	ctx = context.WithValue(ctx, ai.CtxKeyMCPBearerToken, mcpBearerToken)
	// Inject DirectFlushingWriter for additional flushing during execution
	ctx = context.WithValue(ctx, ai.CtxKeyWriter, sopbackend.NewDirectFlushingWriter(w))
	log.Printf("Daemon: streaming context injected, sendEvent=%v", sendEvent != nil)

	cfg := ai.NewConfigMap()
	cfg.Set("payload", payload)
	cfg.Set("access_token", accessToken)
	cfg.Set("refresh_token", refreshToken)
	trimmedThinkingLevel := strings.TrimSpace(req.ThinkingLevel)
	if trimmedThinkingLevel == "" {
		trimmedThinkingLevel = strings.TrimSpace(storedSessionContext.ThinkingLevel)
	}
	if trimmedThinkingLevel != "" {
		cfg.Set("thinking_level", trimmedThinkingLevel)
	}
	cfg.Set("hq_base_url", hqBaseURL)
	if hqPort != "" {
		cfg.Set("hq_port", hqPort)
	}
	cfg.Set("hq_user_id", strings.TrimSpace(req.HQ.UserID))
	cfg.Set("hq_session_id", strings.TrimSpace(req.HQ.SessionID))
	provider := strings.TrimSpace(req.LLMProvider)
	apiKey := strings.TrimSpace(req.LLMAPIKey)
	model := strings.TrimSpace(req.LLMModel)
	url := strings.TrimSpace(req.LLMURL)
	legacyFieldsProvided := provider != "" || apiKey != "" || model != "" || url != ""
	if !legacyFieldsProvided {
		provider = strings.TrimSpace(storedSessionContext.LLMProvider)
		apiKey = strings.TrimSpace(storedSessionContext.LLMAPIKey)
		model = strings.TrimSpace(storedSessionContext.LLMModel)
		url = strings.TrimSpace(storedSessionContext.LLMURL)
	}
	runtimeCredentialPool := selectedCredentials
	if len(requestCredentials) > 0 {
		runtimeCredentialPool = cloneLLMCredentialsMap(requestCredentials)
	} else if len(runtimeCredentialPool) == 0 {
		runtimeCredentialPool = cloneLLMCredentialsMap(storedSessionContext.LLMCredentials)
	}
	if !legacyFieldsProvided && (len(requestCredentials) > 0 || len(runtimeCredentialPool) > 0) {
		for _, candidate := range []string{"openai", "anthropic", "gemini"} {
			cred, ok := runtimeCredentialPool[candidate]
			if !ok {
				continue
			}
			if strings.TrimSpace(cred.APIKey) == "" && strings.TrimSpace(cred.Model) == "" && strings.TrimSpace(cred.URL) == "" {
				continue
			}
			provider = candidate
			apiKey = strings.TrimSpace(cred.APIKey)
			model = strings.TrimSpace(cred.Model)
			url = strings.TrimSpace(cred.URL)
			break
		}
	} else if !legacyFieldsProvided {
		provider = strings.TrimSpace(storedSessionContext.LLMProvider)
		apiKey = strings.TrimSpace(storedSessionContext.LLMAPIKey)
		model = strings.TrimSpace(storedSessionContext.LLMModel)
		url = strings.TrimSpace(storedSessionContext.LLMURL)
	}
	runtimeChain := buildRuntimeProviderChain(provider, apiKey, model, url, runtimeCredentialPool, trimmedThinkingLevel)
	runtimeGenerator, runtimePrimaryDetails := buildRuntimeGeneratorChain(runtimeChain)
	if runtimeGenerator != nil {
		cfg.Set("generator", runtimeGenerator)
	}

	providerDetails := runtimePrimaryDetails
	if providerDetails == nil && (provider != "" || apiKey != "" || model != "" || url != "") {
		decryptedAPIKey, decryptErr := resolveDaemonSecretValue(apiKey)
		if decryptErr != nil {
			respondJSON(w, http.StatusBadRequest, AskResponse{Error: "Invalid llm_api_key: " + decryptErr.Error()})
			return
		}
		providerDetails = &agent.ProviderDetails{
			Provider: provider,
			APIKey:   decryptedAPIKey,
			Model:    model,
			BaseURL:  url,
		}
	}

	if providerDetails != nil {
		cfg.Set("provider_details", providerDetails)
		providerName, modelName := agent.NormalizeProviderAndModel(providerDetails.Provider, providerDetails.Model)
		if providerName != "" {
			providerCtxValue := providerName
			if modelName != "" {
				providerCtxValue = providerName + ":" + modelName
			}
			ctx = context.WithValue(ctx, ai.CtxKeyProvider, providerCtxValue)
		}
		if providerDetails.APIKey != "" {
			ctx = context.WithValue(ctx, ai.CtxKeyAPIKey, providerDetails.APIKey)
		}
		if providerDetails.BaseURL != "" {
			ctx = context.WithValue(ctx, ai.CtxKeyBaseURL, providerDetails.BaseURL)
		}
	}

	normalizedQuery := normalizeAgentQuery(req.Query)
	routingQuery := normalizedQuery
	payload.CurrentUserQuery = req.Query
	cfg.Set("payload", payload)

	// Inject session_payload into context so tools can access SessionID
	ctx = context.WithValue(ctx, "session_payload", payload)

	askQuery := normalizedQuery
	if sharedContext, err := trySharedMemoryContext(ctx, normalizedQuery, payload.UserID); err == nil && strings.TrimSpace(sharedContext) != "" {
		cfg.Set("shared_memory_context", sharedContext)
		cfg.Set("shared_memory_enabled", true)
	}

	logVerbose("daemon handleAsk: request received query=%q session=%q normalized_query=%q streaming=true", req.Query, payload.SessionID, normalizedQuery)

	// Execute query with streaming support
	var answer string
	var err error

	if hasSharedBackendAgents() {
		if strings.HasPrefix(routingQuery, "/") || !shouldUseShellFallback(routingQuery) {
			answer, err = sopbackend.AskSharedAgent(ctx, routingQuery, cfg)
			if err != nil {
				log.Printf("daemon ask request failed: query=%q error=%v", req.Query, err)
				sendEvent("error", err.Error())
				return
			}
			// Fleet Learning disabled (2026-07-30) - No automatic promotion after Ask
			// if err := promoteSharedMemoryArtifactForAsk(ctx, req.Query, answer, payload); err != nil {
			// 	logVerbose("daemon shared-memory promotion failed: query=%q error=%v", req.Query, err)
			// }
			logVerbose("daemon handleAsk: completed via shared backend handler query=%q answer_len=%d", req.Query, len(answer))
		}
	}

	if answer == "" {
		// Direct bare command execution removed - commands must use slash prefixes like /c, /cmd, /shell, /exec
		// This prevents ambiguity between natural language prompts and shell commands
		if s.service != nil {
			answer, err = s.service.Ask(ctx, askQuery, cfg)
		} else {
			answer, err = sopbackend.AskSharedAgent(ctx, askQuery, cfg)
		}

		if err != nil {
			if providerDetails != nil {
				providerKey := normalizeCredentialProviderKey(providerDetails.Provider)
				if providerKey != "" {
					runtimeCredentialPool = recordLLMProviderOutcome(runtimeCredentialPool, providerKey, false, time.Now().UTC().UnixMilli())
					if len(runtimeCredentialPool) > 0 {
						daemonSessionStore.mu.Lock()
						stored := daemonSessionStore.data[sessionKey]
						stored.LLMCredentials = cloneLLMCredentialsMap(runtimeCredentialPool)
						stored.LLMPoolUpdatedAtMS = time.Now().UTC().UnixMilli()
						stored.LLMPoolUpdatedBy = "daemon"
						stored.LastUpdated = time.Now().UTC()
						daemonSessionStore.data[sessionKey] = stored
						daemonSessionStore.mu.Unlock()
						if persistErr := persistDaemonLLMPoolState(); persistErr != nil {
							log.Printf("Warning: failed to persist daemon LLM pool state after provider failure: %v", persistErr)
						}
					}
				}
			}
			log.Printf("daemon ask request failed: query=%q error=%v", req.Query, err)
			sendEvent("error", err.Error())
			return
		}
		// Fleet Learning disabled (2026-07-30) - No automatic promotion after Ask
		// if err := promoteSharedMemoryArtifactForAsk(ctx, req.Query, answer, payload); err != nil {
		// 	logVerbose("daemon shared-memory promotion failed: query=%q error=%v", req.Query, err)
		// }
		logVerbose("daemon handleAsk: completed via agent query=%q answer_len=%d", req.Query, len(answer))
	}
	if providerDetails != nil {
		providerKey := normalizeCredentialProviderKey(providerDetails.Provider)
		if providerKey != "" {
			runtimeCredentialPool = recordLLMProviderOutcome(runtimeCredentialPool, providerKey, true, time.Now().UTC().UnixMilli())
			if len(runtimeCredentialPool) > 0 {
				daemonSessionStore.mu.Lock()
				stored := daemonSessionStore.data[sessionKey]
				stored.LLMCredentials = cloneLLMCredentialsMap(runtimeCredentialPool)
				stored.LLMPoolUpdatedAtMS = time.Now().UTC().UnixMilli()
				stored.LLMPoolUpdatedBy = "daemon"
				stored.LastUpdated = time.Now().UTC()
				daemonSessionStore.data[sessionKey] = stored
				daemonSessionStore.mu.Unlock()
				if persistErr := persistDaemonLLMPoolState(); persistErr != nil {
					log.Printf("Warning: failed to persist daemon LLM pool state after provider success: %v", persistErr)
				}
			}
		}
	}

	// Send final content if nothing was streamed during execution
	// (same pattern as HQ's /api/ai/chat handler)
	if !hasStreamedContent() && strings.TrimSpace(answer) != "" {
		sendEvent("content", answer)
	}
	// Stream ends naturally - no explicit done event needed
}

func trySharedMemoryContext(ctx context.Context, query, userID string) (string, error) {
	if strings.TrimSpace(query) == "" {
		return "", nil
	}

	// Determine relevant categories based on query intent
	categories := selectRelevantCategories(query)

	// Use category-based retrieval for better context
	context, err := RetrieveFederatedContext(ctx, query, categories, 5)
	if err != nil {
		return "", err
	}

	// Also check federated Spaces
	spaceContext, err := retrieveFederatedSpaceContext(ctx, query, 3)
	if err != nil {
		log.Printf("daemon research: failed to retrieve federated Space context: %v", err)
	} else if strings.TrimSpace(spaceContext) != "" {
		if strings.TrimSpace(context) != "" {
			context = context + "\n\n" + spaceContext
		} else {
			context = spaceContext
		}
	}

	if strings.TrimSpace(context) == "" {
		return "", nil
	}

	return fmt.Sprintf("Relevant patterns from fleet knowledge:\n%s", context), nil
}

func promoteSharedMemoryArtifactForAsk(ctx context.Context, query, answer string, payload *ai.SessionPayload) error {
	if strings.TrimSpace(query) == "" || strings.TrimSpace(answer) == "" {
		return nil
	}

	category, confidence, shouldShare := categorizeFederationArtifact(query, answer)
	if !shouldShare {
		shouldShare = shouldPromoteFederationArtifact(query, answer, category, confidence)
	}

	metadata := map[string]any{
		"source": "daemon_ask",
	}
	if payload != nil {
		if payload.SessionID != "" {
			metadata["session_id"] = strings.TrimSpace(payload.SessionID)
		}
		if payload.UserID != "" {
			metadata["user_id"] = strings.TrimSpace(payload.UserID)
		}
	}

	artifact := FederatedArtifact{
		Category:    category,
		Summary:     fmt.Sprintf("Query: %s", truncate(query, 100)),
		Pattern:     fmt.Sprintf("Approach: %s", truncate(answer, 500)),
		Context:     "",
		Confidence:  confidence,
		ShouldShare: shouldShare,
		Metadata:    metadata,
	}

	// Promote to local shared memory
	if err := PromoteFederatedArtifact(ctx, artifact); err != nil {
		return err
	}

	// Also push derived content from any federated Spaces back to HQ
	if shouldShare && payload != nil && payload.SessionID != "" {
		federatedSpaces := GetFederatedSpaces()
		for _, space := range federatedSpaces {
			// If the query appears to be related to a federated Space, push derived content
			if strings.Contains(strings.ToLower(query), strings.ToLower(space.SpaceName)) {
				if err := pushSpaceContentToHQ(ctx, space.SpaceName, answer, string(category)); err != nil {
					log.Printf("daemon push: failed to send Space-derived content to HQ: %v", err)
				} else {
					log.Printf("daemon push: sent Space-derived content to HQ from %s", space.SpaceName)
				}
				break
			}
		}
	}

	return nil
}

func sharedMemoryDatabase(ctx context.Context) (*aidb.Database, error) {
	ctx = normalizeContext(ctx)

	sysOpts, err := sopbackend.GetSystemDBOptions(ctx)
	if err != nil {
		return nil, err
	}
	return aidb.NewDatabase(sysOpts), nil
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func looksLikeAgentRoutingQuery(query string) bool {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(lower, "sop:") ||
		strings.HasPrefix(lower, "omni:") ||
		strings.HasPrefix(lower, "omni/sop/") ||
		strings.HasPrefix(lower, "omni->sop->") ||
		lower == "sop"
}

func hasSharedBackendAgents() bool {
	for _, agentSvc := range sopbackend.GetLoadedAgents() {
		if agentSvc != nil {
			return true
		}
	}
	return false
}

func normalizeAgentQuery(query string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "/") {
		return trimmed
	}
	lower := strings.ToLower(trimmed)
	switch lower {
	case "list_tools", "list_databases", "list_stores", "switch_database", "last-tool", "last_tool",
		"create", "step", "save", "show", "parameterize", "refine", "save_as", "run",
		"insert_step", "delete_step", "update_step", "reorder_steps", "delete", "select", "add", "update", "delete_record",
		"list_scripts", "get_script_details", "save_script", "save_step", "save_last_step", "refactor_last_interaction",
		"set_verbose", "execute_script", "begin_tx", "commit_tx", "rollback_tx", "open_store", "scan", "filter", "join", "join_right", "project", "limit", "sort",
		"manage_transaction", "mint_to_space", "delete_space", "enrich_space", "update_space_config", "read_space_config",
		"vectorize_space", "vectorize_space_categories", "vectorize_space_items", "list_directory", "read_file", "write_file",
		"copy_file", "rename_file", "delete_file", "delete_directory", "create_directory", "path_exists", "stat_path",
		"search_repo_files", "grep_repo_text", "run_repo_tests", "execute_local_command", "send_email", "git_status", "git_branch",
		"git_checkout", "git_commit", "git_diff", "git_log", "git_pull", "git_push", "postgres_list_tables", "postgres_query",
		"splunk_list_saved_searches", "splunk_search", "handoff_to_avatar":
		return "/" + trimmed
	default:
		return trimmed
	}
}

func (s *localRAGService) trySimpleShellCommandFallback(query, sessionID string) (string, bool, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" || strings.HasPrefix(trimmed, "/") || looksLikeAgentRoutingQuery(trimmed) || normalizeAgentQuery(trimmed) != trimmed {
		return "", false, nil
	}
	if strings.Contains(trimmed, "\n") || strings.Contains(trimmed, "\r") {
		return "", false, nil
	}
	if !looksLikeSimpleShellCommand(trimmed) {
		return "", false, nil
	}
	if s.shellSessions == nil {
		s.shellSessions = newPTYShellSessionManager()
	}
	output, stillRunning, err := s.shellSessions.Run(sessionID, trimmed, 5*time.Second)
	if err != nil {
		return "", true, err
	}
	if output == "" && stillRunning {
		return "", true, nil
	}
	if output == "" {
		return "", true, nil
	}
	return output, true, nil
}

func shouldUseShellFallback(query string) bool {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return false
	}
	if strings.ContainsAny(trimmed, "|&;<>()$`") {
		return false
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}
	first := strings.ToLower(fields[0])
	if !isKnownSimpleShellCommand(first) {
		return false
	}
	for _, field := range fields[1:] {
		if strings.HasPrefix(field, "-") {
			continue
		}
		if strings.Contains(field, " ") || strings.Contains(field, "\t") {
			return false
		}
	}
	return true
}

func looksLikeSimpleShellCommand(query string) bool {
	return shouldUseShellFallback(query)
}

func isKnownSimpleShellCommand(command string) bool {
	switch command {
	case "echo", "pwd", "ls", "cd", "cat", "head", "tail", "grep", "find", "uname", "date", "whoami", "env", "git", "python", "python3", "node", "bash", "sh", "mkdir", "rm", "cp", "mv", "touch", "open":
		return true
	default:
		return false
	}
}

func resolveConfigPath(path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Clean(filepath.Join(cwd(), path))
}

func cwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

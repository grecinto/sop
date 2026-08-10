package sopbackend

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var modelCatalogFederationStateStore = newModelCatalogFederationState()

type modelCatalogFederationState struct {
	currentRevision string
	lastRefreshedAt time.Time
	lastSharedAt    time.Time
	lastSharedRev   string
}

func newModelCatalogFederationState() *modelCatalogFederationState {
	return &modelCatalogFederationState{}
}

func (s *modelCatalogFederationState) recordCatalogSnapshot(revision string, at time.Time) {
	if s == nil {
		return
	}
	s.currentRevision = revision
	if !at.IsZero() {
		s.lastRefreshedAt = at
	}
}

func (s *modelCatalogFederationState) markCatalogShared(revision string, at time.Time) {
	if s == nil {
		return
	}
	s.lastSharedRev = revision
	if !at.IsZero() {
		s.lastSharedAt = at
	}
}

func (s *modelCatalogFederationState) canFederate(revision string, now time.Time) bool {
	if s == nil {
		return false
	}
	if strings.TrimSpace(revision) == "" {
		return false
	}
	if strings.TrimSpace(s.lastSharedRev) == "" {
		return true
	}
	if strings.EqualFold(revision, s.lastSharedRev) {
		return false
	}
	if s.lastSharedAt.IsZero() || now.IsZero() {
		return true
	}
	if s.lastRefreshedAt.IsZero() {
		return true
	}
	return s.lastRefreshedAt.After(s.lastSharedAt)
}

func computeModelCatalogRevision(catalog ModelCatalog) string {
	payload, err := json.Marshal(catalog)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func refreshModelCatalogPricing(catalog *ModelCatalog) bool {
	if catalog == nil {
		return false
	}
	changed := false
	for i := range catalog.LLM {
		for j := range catalog.LLM[i].Options {
			option := &catalog.LLM[i].Options[j]
			provider, model, ok := parseModelCatalogProviderAndModel(option.Value)
			if !ok {
				continue
			}
			pricing := pricingSnapshotForProviderModel(provider, model)
			if pricing == nil {
				continue
			}
			if option.InputCostPer1MTokens != pricing.InputCostPer1MTokens {
				option.InputCostPer1MTokens = pricing.InputCostPer1MTokens
				changed = true
			}
			if option.OutputCostPer1MTokens != pricing.OutputCostPer1MTokens {
				option.OutputCostPer1MTokens = pricing.OutputCostPer1MTokens
				changed = true
			}
			if option.LastKnownInputCostUSD != pricing.InputCostPer1MTokens {
				option.LastKnownInputCostUSD = pricing.InputCostPer1MTokens
				changed = true
			}
			if option.LastKnownOutputCostUSD != pricing.OutputCostPer1MTokens {
				option.LastKnownOutputCostUSD = pricing.OutputCostPer1MTokens
				changed = true
			}
			if strings.TrimSpace(option.DefaultThinkingLevel) == "" && strings.Contains(strings.ToLower(option.Label), "mini") {
				option.DefaultThinkingLevel = "low"
			}
		}
	}
	return changed
}

type pricingSnapshot struct {
	InputCostPer1MTokens  float64
	OutputCostPer1MTokens float64
}

func parseModelCatalogProviderAndModel(value string) (string, string, bool) {
	providerPart, modelPart, ok := strings.Cut(strings.TrimSpace(value), ":")
	if !ok || strings.TrimSpace(providerPart) == "" || strings.TrimSpace(modelPart) == "" {
		return "", "", false
	}
	provider := strings.ToLower(strings.TrimSpace(providerPart))
	model := strings.ToLower(strings.TrimSpace(modelPart))
	if provider == "gemini" {
		provider = "google"
	}
	return provider, model, true
}

func pricingSnapshotForProviderModel(provider, model string) *pricingSnapshot {
	snapshots := map[string]map[string]pricingSnapshot{
		"google": {
			"gemini-2.5-pro":        {InputCostPer1MTokens: 1.25, OutputCostPer1MTokens: 10.0},
			"gemini-2.5-flash":      {InputCostPer1MTokens: 0.30, OutputCostPer1MTokens: 2.50},
			"gemini-2.5-flash-lite": {InputCostPer1MTokens: 0.10, OutputCostPer1MTokens: 0.40},
			"gemini-2.0-flash":      {InputCostPer1MTokens: 0.10, OutputCostPer1MTokens: 0.40},
			"gemini-2.0-flash-lite": {InputCostPer1MTokens: 0.075, OutputCostPer1MTokens: 0.30},
		},
		"anthropic": {
			"claude-opus-4-8":   {InputCostPer1MTokens: 5.0, OutputCostPer1MTokens: 25.0},
			"claude-opus-4-6":   {InputCostPer1MTokens: 5.0, OutputCostPer1MTokens: 25.0},
			"claude-sonnet-4-6": {InputCostPer1MTokens: 3.0, OutputCostPer1MTokens: 15.0},
			"claude-sonnet-4-5": {InputCostPer1MTokens: 3.0, OutputCostPer1MTokens: 15.0},
			"claude-3-7-sonnet": {InputCostPer1MTokens: 3.0, OutputCostPer1MTokens: 15.0},
			"claude-3-5-sonnet": {InputCostPer1MTokens: 3.0, OutputCostPer1MTokens: 15.0},
			"claude-3-5-haiku":  {InputCostPer1MTokens: 0.8, OutputCostPer1MTokens: 4.0},
			"claude-3-haiku":    {InputCostPer1MTokens: 0.25, OutputCostPer1MTokens: 1.25},
		},
		"openai": {
			"gpt-5.5":              {InputCostPer1MTokens: 5.0, OutputCostPer1MTokens: 30.0},
			"gpt-5.4":              {InputCostPer1MTokens: 2.5, OutputCostPer1MTokens: 15.0},
			"gpt-5.4-mini":         {InputCostPer1MTokens: 0.75, OutputCostPer1MTokens: 4.50},
			"gpt-4.1":              {InputCostPer1MTokens: 2.0, OutputCostPer1MTokens: 8.0},
			"gpt-4.1-mini":         {InputCostPer1MTokens: 0.4, OutputCostPer1MTokens: 1.6},
			"gpt-4.1-nano":         {InputCostPer1MTokens: 0.10, OutputCostPer1MTokens: 0.40},
			"gpt-4o":               {InputCostPer1MTokens: 2.5, OutputCostPer1MTokens: 10.0},
			"gpt-4o-mini":          {InputCostPer1MTokens: 0.15, OutputCostPer1MTokens: 0.60},
			"gpt-4o-audio-preview": {InputCostPer1MTokens: 2.5, OutputCostPer1MTokens: 10.0},
			"gpt-5.4-nano":         {InputCostPer1MTokens: 0.20, OutputCostPer1MTokens: 1.25},
		},
	}
	if providerSnapshots, ok := snapshots[provider]; ok {
		if snapshot, ok := providerSnapshots[model]; ok {
			return &snapshot
		}
	}
	return nil
}

func writeModelCatalogFile(catalog ModelCatalog, path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	data, err := json.MarshalIndent(catalog, "", "    ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func buildModelCatalogFederationPayload(catalog ModelCatalog) map[string]any {
	revision := computeModelCatalogRevision(catalog)
	now := time.Now().UTC()
	modelCatalogFederationStateStore.recordCatalogSnapshot(revision, now)
	rows := []map[string]any{}
	for _, group := range catalog.LLM {
		for _, option := range group.Options {
			rows = append(rows, map[string]any{
				"provider":                   strings.Split(option.Value, ":")[0],
				"model":                      option.Label,
				"value":                      option.Value,
				"pricing_status":             "unknown",
				"input_cost_per_1m_tokens":   option.InputCostPer1MTokens,
				"output_cost_per_1m_tokens":  option.OutputCostPer1MTokens,
				"last_known_input_cost_usd":  option.LastKnownInputCostUSD,
				"last_known_output_cost_usd": option.LastKnownOutputCostUSD,
			})
		}
	}
	return map[string]any{
		"catalog":           catalog,
		"rows":              rows,
		"revision":          revision,
		"last_refreshed_at": modelCatalogFederationStateStore.lastRefreshedAt.UTC().Format(time.RFC3339),
		"last_shared_at":    modelCatalogFederationStateStore.lastSharedAt.UTC().Format(time.RFC3339),
		"can_federate":      modelCatalogFederationStateStore.canFederate(revision, now),
	}
}

func HandleGetModelCatalogFederation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	payload := buildModelCatalogFederationPayload(modelCatalog)
	respondJSON(w, http.StatusOK, payload)
}

func HandleRefreshModelCatalogFederation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !refreshModelCatalogPricing(&modelCatalog) {
		// Still refresh the revision and state even if no new price data was found.
	}
	path := resolveModelCatalogPath(config.ConfigFile)
	if err := writeModelCatalogFile(modelCatalog, path); err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("write model catalog: %v", err)})
		return
	}
	now := time.Now().UTC()
	modelCatalogFederationStateStore.recordCatalogSnapshot(computeModelCatalogRevision(modelCatalog), now)
	payload := buildModelCatalogFederationPayload(modelCatalog)
	respondJSON(w, http.StatusOK, payload)
}

func HandleShareModelCatalogFederation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	revision := computeModelCatalogRevision(modelCatalog)
	now := time.Now().UTC()
	modelCatalogFederationStateStore.markCatalogShared(revision, now)
	payload := buildModelCatalogFederationPayload(modelCatalog)
	respondJSON(w, http.StatusOK, payload)
}

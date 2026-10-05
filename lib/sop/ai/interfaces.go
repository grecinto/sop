package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/btree"
	"github.com/sharedcode/sop/search"
)

// ContextKey is a type for context keys used in the AI package.
type ContextKey string

const (
	// CtxKeyProvider is the context key for overriding the AI provider.
	CtxKeyProvider ContextKey = "ai_provider"
	// CtxKeyAPIKey is the context key for passing a transient API key
	CtxKeyAPIKey ContextKey = "ai_api_key"
	// CtxKeyBaseURL is the context key for passing a transient Base URL
	CtxKeyBaseURL ContextKey = "ai_base_url"
	// CtxKeyAppBaseURL is the context key for passing the public app origin used to build absolute viewer links.
	CtxKeyAppBaseURL ContextKey = "ai_app_base_url"
	// CtxKeyExecutor is the context key for passing the ToolExecutor.
	CtxKeyExecutor ContextKey = "ai_executor"
	// CtxKeyDeobfuscator is the context key for passing the Deobfuscator.
	CtxKeyDeobfuscator ContextKey = "ai_deobfuscator"
	// CtxKeyHistory is the context key for passing conversation history.
	CtxKeyHistory ContextKey = "ai_history"
	// CtxKeyWriter is the context key for passing an io.Writer for streaming output.
	CtxKeyWriter ContextKey = "ai_writer"
	// CtxKeyDatabase is the context key for passing the target database for script execution.
	CtxKeyDatabase ContextKey = "ai_database"
	// CtxKeyResultStreamer is the context key for passing the ResultStreamer.
	CtxKeyResultStreamer ContextKey = "ai_result_streamer"
	// CtxKeyScriptRecorder is the context key for passing the ScriptRecorder.
	CtxKeyScriptRecorder ContextKey = "ai_script_recorder"
	// CtxKeyAutoFlush is the context key for enabling/disabling auto-flush (boolean).
	CtxKeyAutoFlush ContextKey = "ai_auto_flush"
	// CtxKeyDefaultFormat is the context key for carrying the requested default output format.
	CtxKeyDefaultFormat ContextKey = "ai_default_format"
	// CtxKeyProgressSink is the context key for passing a structured progress emitter callback.
	CtxKeyProgressSink ContextKey = "ai_progress_sink"
	// CtxKeyEventStreamer is the context key for passing a structured event emitter callback.
	CtxKeyEventStreamer ContextKey = "ai_event_streamer"
	// CtxKeyMCPURL carries the request-scoped SOP MCP endpoint for delegated agents.
	CtxKeyMCPURL ContextKey = "ai_mcp_url"
	// CtxKeyMCPBearerToken carries request-scoped auth for delegated SOP MCP calls.
	CtxKeyMCPBearerToken ContextKey = "ai_mcp_bearer_token"
	// CtxKeyNativeToolHints marks native Ask-loop tool execution that can consume structured progress hints.
	CtxKeyNativeToolHints ContextKey = "ai_native_tool_hints"
	// CtxKeyIsNewTopic carries the is-new-topic signal from the orchestrating Service down to pipeline agents.
	// When true it indicates the current request starts a fresh conversation thread rather than continuing one.
	CtxKeyIsNewTopic ContextKey = "ai_is_new_topic"
)

// ArtifactType represents the type of a database artifact.
type ArtifactType string

const (
	ArtifactTypeSpace ArtifactType = "Space"
	ArtifactTypeStore ArtifactType = "Store"
)

// ArtifactReference is a unified structure to pass globally referenced DB Artifacts (KB, Space, etc.)
// containing the exact location, avoiding the need to cycle through available databases.
type ArtifactReference struct {
	Name            string              `json:"name"`
	Type            ArtifactType        `json:"type"`
	DatabaseName    string              `json:"db_name"`
	DatabaseOptions sop.DatabaseOptions `json:"db_opts,omitempty"`
}

// KnowledgeDocument represents a single unit of embeddable contextual information
// commonly used to standardize preloading Vector DBs and RAG pipelines.
type KnowledgeDocument struct {
	ID          string                 `json:"id,omitempty"`
	Text        string                 `json:"text,omitempty"`
	PageContent string                 `json:"page_content,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// KnowledgeBasePayload represents the standard JSON payload structure
// used for streaming or batch-loading documents into a Vector DB.
type KnowledgeBasePayload struct {
	DatasetName string              `json:"dataset_name,omitempty"`
	Documents   []KnowledgeDocument `json:"documents"`
}

// ResultStreamer defines the interface for streaming tool results.
type ResultStreamer interface {
	// BeginArray starts a JSON array output.
	BeginArray()
	// SetMetadata sets metadata for the result (e.g. headers, record counts).
	// Should be called before the first WriteItem.
	SetMetadata(meta map[string]any)
	// WriteItem writes a single item to the output (e.g. an element of an array).
	WriteItem(item any)
	// EndArray ends the JSON array output.
	EndArray()
}

// Deobfuscator defines the interface for de-obfuscating text.
type Deobfuscator interface {
	Deobfuscate(text string) string
}

// Embeddings defines the interface for generating vector embeddings from text.
type Embeddings interface {
	// Name returns the name of the embedding model.
	Name() string
	// Dim returns the dimension of the embeddings.
	Dim() int
	// EmbedTexts generates embeddings for a batch of texts.
	EmbedTexts(ctx context.Context, texts []string) ([][]float32, error)
}

// EmbeddingModeSupport adds explicit storage/search/category embedding modes.
// Implementers may use different prefixes or truncation depths for each path.
type EmbeddingModeSupport interface {
	EmbedCategoryTexts(ctx context.Context, texts []string) ([][]float32, error)
	EmbedDocumentTexts(ctx context.Context, texts []string) ([][]float32, error)
	EmbedQueryTexts(ctx context.Context, texts []string) ([][]float32, error)
}

// Centroid represents a cluster center and its metadata.
type Centroid struct {
	Vector      []float32
	VectorCount int
}

// VectorKey is the key for the Vectors B-Tree.
type VectorKey struct {
	CentroidID         int
	DistanceToCentroid float32
	ItemID             string
	// IsDeleted marks this key as a Tombstone.
	// It ensures the Optimize process can find this entry and physically delete
	// the corresponding data from the Content store.
	IsDeleted bool
}

// ContentKey is the key for the Content B-Tree.
// It includes metadata to allow updates without modifying the Value segment (Payload).
//
// SOP's key structure handling allows apps to ride data in it that acts as persistent space
// as well without affecting the overall key/value pair behaviour and BTree ness.
// This means critical metadata (like Version, CentroidID, Deleted flag) is available
// during key traversal (e.g. Range scans) without needing to fetch the potentially large Value payload.
type ContentKey struct {
	ItemID         string  `json:"id"`
	CentroidID     int     `json:"cid"`
	Distance       float32 `json:"dist"`
	Version        int64   `json:"ver"`
	Deleted        bool    `json:"del"`
	NextCentroidID int     `json:"ncid"`
	NextDistance   float32 `json:"ndist"`
	NextVersion    int64   `json:"nver"`
}

// VectorStore defines the interface for a vector database domain (like a table).
type VectorStore[T any] interface {
	// Upsert adds or updates a single item in the store.
	// It now accepts an Item[T] which includes the Vector, Payload, and optional CentroidID.
	Upsert(ctx context.Context, item Item[T]) error
	// UpsertBatch adds or updates multiple items in the store efficiently.
	UpsertBatch(ctx context.Context, items []Item[T]) error
	// Get retrieves an item by its ID.
	Get(ctx context.Context, id string) (*Item[T], error)
	// Delete removes an item by its ID.
	Delete(ctx context.Context, id string) error
	// Query searches for the nearest neighbors to the given vector.
	// filters is a function that returns true if the item should be included.
	Query(ctx context.Context, vec []float32, k int, filter func(T) bool) ([]Hit[T], error)
	// Count returns the total number of items in the store.
	Count(ctx context.Context) (int64, error)
	// AddCentroid adds a new centroid to the store dynamically.
	// This allows for runtime expansion of the concept space without full rebalancing.
	AddCentroid(ctx context.Context, vec []float32) (int, error)

	// SplitCentroid reorganizes an overloaded centroid by running localized 2-Means
	// clustering, creating two new centroids, and reassigning its vectors.
	SplitCentroid(ctx context.Context, centroidID int) error

	// Optimize reorganizes the index to improve query performance.
	// It re-calculates centroids based on the full dataset and re-distributes vectors.
	// This is recommended after a large batch ingestion (BuildOnceQueryMany mode) to "Seal" the index.
	Optimize(ctx context.Context) error

	// Consolidate reads accumulated vectors from short-term memory (TempVectors),
	// dynamically routes them into existing Centroids using AssignAndIndex logic,
	// and clears them from short-term memory.
	Consolidate(ctx context.Context) error

	// UpdateEmbedderInfo updates the configuration defining which embedder was used
	// to index the vectors, persisting it in the system configuration of the store.
	UpdateEmbedderInfo(ctx context.Context, provider string, model string, dimensions int) error

	// SetDeduplication enables or disables the internal deduplication check during Upsert.
	// Disabling this can speed up ingestion for pristine data but may lead to ghost vectors if duplicates exist.
	SetDeduplication(enabled bool)

	// Centroids returns the Centroids B-Tree for advanced manipulation.
	Centroids(ctx context.Context) (btree.BtreeInterface[int, Centroid], error)
	// Vectors returns the Vectors B-Tree for advanced manipulation.
	Vectors(ctx context.Context) (btree.BtreeInterface[VectorKey, []float32], error)
	// Content returns the Content B-Tree for advanced manipulation.
	Content(ctx context.Context) (btree.BtreeInterface[ContentKey, string], error)
	// Lookup returns the Sequence Lookup B-Tree for advanced manipulation (e.g. random sampling).
	Lookup(ctx context.Context) (btree.BtreeInterface[int, string], error)

	// Version returns the Vector store's version number, which is a unix elapsed time.
	Version(ctx context.Context) (int64, error)
}

// UsageMode defines how the vector database is intended to be used.
type UsageMode int

const (
	// BuildOnceQueryMany optimizes for a single ingestion phase followed by read-only queries.
	// Temporary structures (TempVectors, Lookup) are deleted after indexing to save space.
	BuildOnceQueryMany UsageMode = iota

	// DynamicWithVectorCountTracking optimizes for continuous updates (CRUD).
	// It maintains necessary metadata (like vector counts per centroid) to support
	// future rebalancing and structural adjustments.
	DynamicWithVectorCountTracking

	// Dynamic optimizes for continuous updates (CRUD). Does not maintain metadata like vector counts.
	// Useful for scenarios where the Agent explicitly manages the Centroids & Vector assignments.
	Dynamic
)

// Item represents a vector item returned to the user.
type Item[T any] struct {
	ID         string
	Vector     []float32
	Payload    T
	CentroidID int // Optional: Explicitly assign to a centroid (0 = auto)
}

// Hit represents a search result.
type Hit[T any] struct {
	ID              string
	DocID           []string
	Score           float32
	Payload         T
	DocumentContent string
	SourceLabel     string
}

// Generator defines the interface for an LLM or text generation model.
type Generator interface {
	// Name returns the name of the generator.
	Name() string
	// Generate produces text based on the provided prompt and options.
	Generate(ctx context.Context, prompt string, opts GenOptions) (GenOutput, error)
	// PrewarmCache sends a request to the provider to cache the provided
	// options (e.g., SystemPrompt, Tools) without generating a response.
	// This is used for just-in-time, predictive caching.
	PrewarmCache(ctx context.Context, opts GenOptions) error
	// EstimateCost calculates the estimated cost of the generation.
	EstimateCost(inTokens, outTokens int) float64
}

type AI struct {
	// Provider is the default AI provider to use.
	Provider string `json:"Provider,omitempty"`
	// CacheTTL is the cache duration for AI providers that support it (e.g., "5m", "1h").
	CacheTTL string `json:"CacheTTL,omitempty"`
	// EndPoint is the AI provider's endpoint.
	EndPoint string `json:"Endpoint,omitempty"`
}

// GenOptions configures the generation process.
type ContentPart struct {
	Type     string
	Text     string
	MimeType string
	Data     string
	URL      string
}

// GenOptions configures the generation process.
type GenOptions struct {
	SystemPrompt     string
	MaxTokens        int
	Temperature      float32
	ForceTemperature bool
	TopP             float32
	Stop             []string
	Tools            []ToolDefinition // NEW: Pass the schemas via Native API
	CompactPrompt    bool
	// ToolCallContinuations carries provider-neutral tool-call/result turns that a
	// generator can translate into its provider-specific continuation format.
	ToolCallContinuations []ToolCallContinuation
	// InputParts carries optional multimodal content that should be supplied alongside the prompt.
	InputParts []ContentPart
	// ThinkingLevel controls internal reasoning intensity for Gemini 3.1 Pro (low/medium/high).
	// Use "low" for strict structured outputs, "high" for creative tasks.
	ThinkingLevel string
	// ResponseSchema enforces strict output structure by physically constraining token generation.
	// Pass a JSON schema to prevent hallucinated fields/keys.
	ResponseSchema map[string]any
}

// ToolCallContinuation preserves a completed tool-call turn so generators can
// continue a provider-native function-calling exchange without flattening the
// tool response back into plain prompt text.
type ToolCallContinuation struct {
	ToolCall ToolCall
	Response any
}

// ModelProfile captures a provider/model-specific policy for cost, prompt compactness,
// and loop budgeting.
type ModelProfile struct {
	Provider            string
	Model               string
	Tier                string
	MaxOutputTokens     int
	Temperature         float32
	CompactPrompt       bool
	MaxToolTurns        int
	ReasoningStrength   int
	SupportedTaskTypes  []string
	MediaCapability     int
	SupportedMediaTypes []string
}

// ModelProfileProvider exposes a resolved model profile for a generator.
type ModelProfileProvider interface {
	ModelProfile() ModelProfile
}

type ModelSelectionRequest struct {
	Provider              string
	TaskType              string
	NeedMultimodal        bool
	RequiredMediaTypes    []string
	PreferCheap           bool
	PreferStrong          bool
	EstimatedInputTokens  int
	EstimatedOutputTokens int
	MaxCostUSD            float64
}

type ModelCatalogEntry struct {
	Provider               string
	Model                  string
	Label                  string
	Tier                   string
	MaxOutputTokens        int
	Temperature            float32
	CompactPrompt          bool
	MaxToolTurns           int
	ReasoningStrength      int
	SupportedTaskTypes     []string
	MediaCapability        int
	SupportedMediaTypes    []string
	InputCostPer1MTokens   float64
	OutputCostPer1MTokens  float64
	PricingStatus          string
	PricingSource          string
	LastKnownInputCostUSD  float64
	LastKnownOutputCostUSD float64
}

type ModelCatalogRegistry struct {
	mu      sync.RWMutex
	entries []ModelCatalogEntry
}

var (
	modelCatalogRegistryOnce sync.Once
	modelCatalogRegistryInst *ModelCatalogRegistry
)

func GetModelCatalogRegistry() *ModelCatalogRegistry {
	modelCatalogRegistryOnce.Do(func() {
		modelCatalogRegistryInst = loadModelCatalogRegistry()
	})
	return modelCatalogRegistryInst
}

func (r *ModelCatalogRegistry) FindEntry(provider, model string) (ModelCatalogEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	for _, entry := range r.entries {
		if strings.EqualFold(strings.TrimSpace(entry.Provider), provider) && strings.EqualFold(strings.TrimSpace(entry.Model), model) {
			return entry, true
		}
	}
	return ModelCatalogEntry{}, false
}

func (r *ModelCatalogRegistry) Load() error {
	entries, err := loadModelCatalogEntries()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = entries
	return nil
}

func (r *ModelCatalogRegistry) SelectBest(req ModelSelectionRequest) (ModelCatalogEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.entries) == 0 {
		return ModelCatalogEntry{}, false
	}

	var best ModelCatalogEntry
	var bestScore int
	var bestEstimatedCost float64
	var found bool
	for _, entry := range r.entries {
		if strings.TrimSpace(req.Provider) != "" && !strings.EqualFold(strings.TrimSpace(req.Provider), "auto") {
			if !strings.EqualFold(strings.TrimSpace(entry.Provider), strings.TrimSpace(req.Provider)) {
				continue
			}
		}
		if req.TaskType != "" && !supportsTask(entry.SupportedTaskTypes, req.TaskType) {
			continue
		}
		if req.NeedMultimodal && !supportsMultimodal(entry, req.RequiredMediaTypes) {
			continue
		}
		if len(req.RequiredMediaTypes) > 0 && !supportsMedia(entry.SupportedMediaTypes, req.RequiredMediaTypes) {
			continue
		}
		estimatedCost := 0.0
		if req.MaxCostUSD > 0 {
			estimatedCost = estimateCatalogModelCost(entry, req.EstimatedInputTokens, req.EstimatedOutputTokens)
			if estimatedCost > req.MaxCostUSD {
				continue
			}
		}
		if req.PreferCheap && req.MaxCostUSD > 0 {
			if !found || estimatedCost < bestEstimatedCost {
				best = entry
				bestEstimatedCost = estimatedCost
				bestScore = 0
				found = true
			}
			continue
		}
		score := 0
		if entry.Tier == "strong" {
			score += 100
		} else if entry.Tier == "standard" {
			score += 60
		} else if entry.Tier == "cheap" {
			score += 20
		}
		if req.NeedMultimodal {
			score += entry.MediaCapability * 20
		}
		if req.PreferStrong {
			score += entry.ReasoningStrength * 10
		}
		if req.PreferCheap {
			score -= 20
		}
		if req.TaskType != "" && supportsTask(entry.SupportedTaskTypes, req.TaskType) {
			score += 10
		}
		if score > bestScore {
			best = entry
			bestScore = score
			found = true
		}
	}
	return best, found
}

func loadModelCatalogRegistry() *ModelCatalogRegistry {
	registry := &ModelCatalogRegistry{}
	if err := registry.Load(); err != nil {
		return registry
	}
	return registry
}

func loadModelCatalogEntries() ([]ModelCatalogEntry, error) {
	var entries []ModelCatalogEntry
	for _, candidate := range modelCatalogCandidatePaths() {
		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		var catalog modelCatalogFile
		if err := json.Unmarshal(data, &catalog); err != nil {
			continue
		}
		for _, group := range catalog.LLM {
			for _, option := range group.Options {
				provider := ""
				model := strings.TrimSpace(option.Value)
				if providerPart, modelPart, ok := strings.Cut(option.Value, ":"); ok {
					provider = normalizeModelCatalogProvider(strings.TrimSpace(providerPart))
					model = strings.TrimSpace(modelPart)
				}
				if provider == "" {
					continue
				}
				entries = append(entries, ModelCatalogEntry{
					Provider:               provider,
					Model:                  model,
					Label:                  option.Label,
					Tier:                   strings.TrimSpace(option.CapabilityTier),
					MaxOutputTokens:        option.MaxOutputTokens,
					Temperature:            0,
					CompactPrompt:          option.CompactPrompt != nil && *option.CompactPrompt,
					MaxToolTurns:           option.MaxToolTurns,
					ReasoningStrength:      option.ReasoningStrength,
					SupportedTaskTypes:     append([]string(nil), option.SupportedTaskTypes...),
					MediaCapability:        option.MediaCapability,
					SupportedMediaTypes:    append([]string(nil), option.SupportedMediaTypes...),
					InputCostPer1MTokens:   option.InputCostPer1MTokens,
					OutputCostPer1MTokens:  option.OutputCostPer1MTokens,
					PricingStatus:          option.PricingStatus,
					PricingSource:          option.PricingSource,
					LastKnownInputCostUSD:  option.LastKnownInputCostUSD,
					LastKnownOutputCostUSD: option.LastKnownOutputCostUSD,
				})
			}
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no model catalog entries found")
	}
	return entries, nil
}

func estimateCatalogModelCost(entry ModelCatalogEntry, inputTokens, outputTokens int) float64 {
	inputRate := entry.InputCostPer1MTokens
	if inputRate <= 0 {
		inputRate = entry.LastKnownInputCostUSD
	}
	outputRate := entry.OutputCostPer1MTokens
	if outputRate <= 0 {
		outputRate = entry.LastKnownOutputCostUSD
	}
	if inputRate <= 0 && outputRate <= 0 {
		return math.Inf(1)
	}
	inputCost := 0.0
	outputCost := 0.0
	if inputRate > 0 && inputTokens > 0 {
		inputCost = float64(inputTokens) / 1_000_000 * inputRate
	}
	if outputRate > 0 && outputTokens > 0 {
		outputCost = float64(outputTokens) / 1_000_000 * outputRate
	}
	return inputCost + outputCost
}

func supportsTask(taskTypes []string, taskType string) bool {
	if taskType == "" {
		return true
	}
	needle := strings.ToLower(strings.TrimSpace(taskType))
	for _, candidate := range taskTypes {
		if strings.EqualFold(strings.TrimSpace(candidate), needle) {
			return true
		}
	}
	return false
}

func supportsMultimodal(entry ModelCatalogEntry, required []string) bool {
	if entry.MediaCapability <= 0 {
		return false
	}
	if len(required) == 0 {
		return entry.MediaCapability >= 2 || entry.ReasoningStrength >= 4
	}
	return supportsMedia(entry.SupportedMediaTypes, required)
}

func supportsMedia(supported []string, required []string) bool {
	if len(required) == 0 {
		return true
	}
	for _, want := range required {
		want = strings.ToLower(strings.TrimSpace(want))
		if want == "" {
			continue
		}
		matched := false
		for _, have := range supported {
			if strings.EqualFold(strings.TrimSpace(have), want) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// ResolveModelProfile returns a model profile derived from explicit config and
// inferred heuristics. It is intentionally lightweight and does not require
// provider-specific transport knowledge.
func ResolveModelProfile(provider, model string, cfg map[string]any) ModelProfile {
	profile := ModelProfile{Provider: provider, Model: model}
	if strings.EqualFold(provider, "openai") {
		provider = "chatgpt"
	}
	profile.Provider = provider
	profile.Model = model

	if cfg != nil {
		if raw, ok := cfg["model_profile"]; ok {
			if mp, ok := raw.(map[string]any); ok {
				if tier, ok := mp["tier"].(string); ok && strings.TrimSpace(tier) != "" {
					profile.Tier = strings.TrimSpace(tier)
				}
				if maxTokens, ok := mp["max_output_tokens"].(int); ok {
					profile.MaxOutputTokens = maxTokens
				}
				if maxTokens, ok := mp["max_output_tokens"].(float64); ok {
					profile.MaxOutputTokens = int(maxTokens)
				}
				if temp, ok := mp["temperature"].(float64); ok {
					profile.Temperature = float32(temp)
				}
				if temp, ok := mp["temperature"].(float32); ok {
					profile.Temperature = temp
				}
				if compact, ok := mp["compact_prompt"].(bool); ok {
					profile.CompactPrompt = compact
				}
				if maxTurns, ok := mp["max_tool_turns"].(int); ok {
					profile.MaxToolTurns = maxTurns
				}
				if maxTurns, ok := mp["max_tool_turns"].(float64); ok {
					profile.MaxToolTurns = int(maxTurns)
				}
				if reasoningStrength, ok := mp["reasoning_strength"].(int); ok {
					profile.ReasoningStrength = reasoningStrength
				}
				if reasoningStrength, ok := mp["reasoning_strength"].(float64); ok {
					profile.ReasoningStrength = int(reasoningStrength)
				}
				if supported, ok := mp["supported_task_types"].([]string); ok {
					profile.SupportedTaskTypes = append([]string(nil), supported...)
				}
				if supported, ok := mp["supported_task_types"].([]any); ok {
					profile.SupportedTaskTypes = make([]string, 0, len(supported))
					for _, item := range supported {
						if s, ok := item.(string); ok {
							profile.SupportedTaskTypes = append(profile.SupportedTaskTypes, s)
						}
					}
				}
				if mediaCapability, ok := mp["media_capability"].(int); ok {
					profile.MediaCapability = mediaCapability
				}
				if mediaCapability, ok := mp["media_capability"].(float64); ok {
					profile.MediaCapability = int(mediaCapability)
				}
				if supported, ok := mp["supported_media_types"].([]string); ok {
					profile.SupportedMediaTypes = append([]string(nil), supported...)
				}
				if supported, ok := mp["supported_media_types"].([]any); ok {
					profile.SupportedMediaTypes = make([]string, 0, len(supported))
					for _, item := range supported {
						if s, ok := item.(string); ok {
							profile.SupportedMediaTypes = append(profile.SupportedMediaTypes, s)
						}
					}
				}
			}
		}
	}

	if strings.TrimSpace(profile.Tier) == "" {
		if strings.Contains(strings.ToLower(model), "haiku") || strings.Contains(strings.ToLower(model), "flash") || strings.Contains(strings.ToLower(model), "mini") {
			profile.Tier = "cheap"
		} else {
			profile.Tier = "standard"
		}
	}

	if catalogProfile := resolveCatalogModelProfile(provider, model); catalogProfile != nil {
		if profile.Tier == "standard" && strings.TrimSpace(catalogProfile.Tier) != "" {
			profile.Tier = catalogProfile.Tier
		}
		if profile.MaxOutputTokens == 0 && catalogProfile.MaxOutputTokens > 0 {
			profile.MaxOutputTokens = catalogProfile.MaxOutputTokens
		}
		if profile.MaxToolTurns == 0 && catalogProfile.MaxToolTurns > 0 {
			profile.MaxToolTurns = catalogProfile.MaxToolTurns
		}
		if profile.ReasoningStrength == 0 && catalogProfile.ReasoningStrength > 0 {
			profile.ReasoningStrength = catalogProfile.ReasoningStrength
		}
		if len(profile.SupportedTaskTypes) == 0 && len(catalogProfile.SupportedTaskTypes) > 0 {
			profile.SupportedTaskTypes = append([]string(nil), catalogProfile.SupportedTaskTypes...)
		}
		if profile.MediaCapability == 0 && catalogProfile.MediaCapability > 0 {
			profile.MediaCapability = catalogProfile.MediaCapability
		}
		if len(profile.SupportedMediaTypes) == 0 && len(catalogProfile.SupportedMediaTypes) > 0 {
			profile.SupportedMediaTypes = append([]string(nil), catalogProfile.SupportedMediaTypes...)
		}
		if !profile.CompactPrompt && catalogProfile.CompactPrompt {
			profile.CompactPrompt = true
		}
	}

	if profile.MaxOutputTokens == 0 {
		profile.MaxOutputTokens = 1024
	}
	if profile.Temperature == 0 {
		if profile.Tier == "cheap" {
			profile.Temperature = 0.1
		} else {
			profile.Temperature = 0.2
		}
	}
	if profile.MaxToolTurns == 0 {
		if profile.Tier == "cheap" {
			profile.MaxToolTurns = 3
		} else {
			profile.MaxToolTurns = 5
		}
	}
	if !profile.CompactPrompt {
		profile.CompactPrompt = profile.Tier == "cheap"
	}
	return profile
}

// ApplyModelProfile applies a model profile to generation options without
// overwriting explicitly set values.
type modelCatalogFile struct {
	LLM []modelCatalogGroup `json:"llm,omitempty"`
}

type modelCatalogGroup struct {
	Label   string               `json:"label"`
	Options []modelCatalogOption `json:"options,omitempty"`
}

type modelCatalogOption struct {
	Value                  string   `json:"value"`
	Label                  string   `json:"label"`
	DefaultThinkingLevel   string   `json:"default_thinking_level,omitempty"`
	ThinkingLevelRange     []string `json:"thinking_level_range,omitempty"`
	CapabilityTier         string   `json:"capability_tier,omitempty"`
	ReasoningStrength      int      `json:"reasoning_strength,omitempty"`
	MaxOutputTokens        int      `json:"max_output_tokens,omitempty"`
	MaxToolTurns           int      `json:"max_tool_turns,omitempty"`
	CompactPrompt          *bool    `json:"compact_prompt,omitempty"`
	SupportedTaskTypes     []string `json:"supported_task_types,omitempty"`
	MediaCapability        int      `json:"media_capability,omitempty"`
	SupportedMediaTypes    []string `json:"supported_media_types,omitempty"`
	InputCostPer1MTokens   float64  `json:"input_cost_per_1m_tokens,omitempty"`
	OutputCostPer1MTokens  float64  `json:"output_cost_per_1m_tokens,omitempty"`
	PricingStatus          string   `json:"pricing_status,omitempty"`
	PricingSource          string   `json:"pricing_source,omitempty"`
	LastKnownInputCostUSD  float64  `json:"last_known_input_cost_usd,omitempty"`
	LastKnownOutputCostUSD float64  `json:"last_known_output_cost_usd,omitempty"`
}

func resolveCatalogModelProfile(provider, model string) *ModelProfile {
	provider = normalizeModelCatalogProvider(provider)
	model = strings.ToLower(strings.TrimSpace(model))
	if provider == "" || model == "" {
		return nil
	}

	for _, candidate := range modelCatalogCandidatePaths() {
		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		var catalog modelCatalogFile
		if err := json.Unmarshal(data, &catalog); err != nil {
			continue
		}
		for _, group := range catalog.LLM {
			for _, option := range group.Options {
				if !modelCatalogOptionMatches(provider, model, option.Value) {
					continue
				}
				profile := ModelProfile{Provider: provider, Model: model}
				if strings.TrimSpace(option.CapabilityTier) != "" {
					profile.Tier = strings.TrimSpace(option.CapabilityTier)
				}
				if option.MaxOutputTokens > 0 {
					profile.MaxOutputTokens = option.MaxOutputTokens
				}
				if option.MaxToolTurns > 0 {
					profile.MaxToolTurns = option.MaxToolTurns
				}
				if option.ReasoningStrength > 0 {
					profile.ReasoningStrength = option.ReasoningStrength
				}
				if option.CompactPrompt != nil {
					profile.CompactPrompt = *option.CompactPrompt
				}
				if len(option.SupportedTaskTypes) > 0 {
					profile.SupportedTaskTypes = append([]string(nil), option.SupportedTaskTypes...)
				}
				if option.MediaCapability > 0 {
					profile.MediaCapability = option.MediaCapability
				}
				if len(option.SupportedMediaTypes) > 0 {
					profile.SupportedMediaTypes = append([]string(nil), option.SupportedMediaTypes...)
				}
				return &profile
			}
		}
	}
	return nil
}

func normalizeModelCatalogProvider(provider string) string {
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "openai" {
		return "chatgpt"
	}
	return provider
}

func modelCatalogCandidatePaths() []string {
	seen := map[string]struct{}{}
	var paths []string
	add := func(path string) {
		if path == "" {
			return
		}
		cleaned := filepath.Clean(path)
		if _, ok := seen[cleaned]; ok {
			return
		}
		seen[cleaned] = struct{}{}
		paths = append(paths, cleaned)
	}

	if cwd, err := os.Getwd(); err == nil {
		current := filepath.Clean(cwd)
		for {
			add(filepath.Join(current, "model_catalog.json"))
			add(filepath.Join(current, "lib", "sop", "model_catalog.json"))
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}

	if exe, err := os.Executable(); err == nil {
		current := filepath.Dir(filepath.Clean(exe))
		for {
			add(filepath.Join(current, "model_catalog.json"))
			add(filepath.Join(current, "lib", "sop", "model_catalog.json"))
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}

	add("model_catalog.json")
	return paths
}

func modelCatalogOptionMatches(provider, model, optionValue string) bool {
	provider = normalizeModelCatalogProvider(provider)
	model = strings.ToLower(strings.TrimSpace(model))
	optionValue = strings.TrimSpace(optionValue)
	if optionValue == "" {
		return false
	}

	if providerPart, modelPart, ok := strings.Cut(optionValue, ":"); ok {
		if normalizeModelCatalogProvider(providerPart) != provider {
			return false
		}
		return strings.ToLower(strings.TrimSpace(modelPart)) == model
	}

	return strings.ToLower(optionValue) == model
}

func ApplyModelProfile(opts GenOptions, profile ModelProfile) GenOptions {
	if opts.MaxTokens == 0 {
		opts.MaxTokens = profile.MaxOutputTokens
	}
	if opts.Temperature == 0 && profile.Temperature != 0 {
		opts.Temperature = profile.Temperature
	}
	if !opts.CompactPrompt {
		opts.CompactPrompt = profile.CompactPrompt
	}
	return opts
}

// ToolCall represents a native tool call requested by the LLM.
type ToolCall struct {
	Name string
	Args map[string]any
	// NativeID carries the provider-native tool/function call identifier when one exists.
	// It is intentionally transport-agnostic so engines can round-trip tool continuity
	// across Gemini, OpenAI, Anthropic, or future providers without hard-coding one API shape.
	NativeID string
	// TransportMeta preserves provider-specific fields that may be needed to continue a
	// native tool-calling session without flattening everything into prompt text.
	TransportMeta map[string]any
}

// GenOutput represents the result of a generation.
type GenOutput struct {
	Text       string
	TokensUsed int
	Raw        any
	ToolCalls  []ToolCall // NEW: Struct for natively parsed tool requests
}

type CarryoverMode string

const (
	CarryoverModeOff     CarryoverMode = "off"
	CarryoverModeCompact CarryoverMode = "compact"
	CarryoverModeLive    CarryoverMode = "live"
)

type CarryoverResetReason string

const (
	CarryoverResetNone                CarryoverResetReason = ""
	CarryoverResetDisabled            CarryoverResetReason = "disabled"
	CarryoverResetUnsupportedProvider CarryoverResetReason = "unsupported_provider"
	CarryoverResetMissingState        CarryoverResetReason = "missing_state"
	CarryoverResetLiveContinuation    CarryoverResetReason = "live_continuation"
	CarryoverResetTopicSwitch         CarryoverResetReason = "topic_switch"
	CarryoverResetProviderChanged     CarryoverResetReason = "provider_changed"
	CarryoverResetModelChanged        CarryoverResetReason = "model_changed"
	CarryoverResetKBChanged           CarryoverResetReason = "kb_changed"
	CarryoverResetExpired             CarryoverResetReason = "expired"
	CarryoverResetBudgetExceeded      CarryoverResetReason = "budget_exceeded"
	CarryoverResetCompactContinuation CarryoverResetReason = "compact_continuation"
)

type CarryoverCapability struct {
	Provider        string
	Model           string
	SupportsCompact bool
	SupportsLive    bool
}

// ProviderRuntimeCapability captures the higher-level runtime properties that
// matter when deciding how a provider should integrate with Ask/ReAct.
type ProviderRuntimeCapability struct {
	Provider                   string
	Model                      string
	LoopMode                   ReActLoopMode
	SupportsNativeToolCalls    bool
	SupportsNativeContinuation bool
	SupportsCompactCarryover   bool
	SupportsLiveCarryover      bool
	SupportsMultiToolTurns     bool
	SupportsStreaming          bool
}

// ProviderRuntimeCapabilityProvider is an optional generator extension that
// exposes the provider's runtime integration characteristics explicitly.
type ProviderRuntimeCapabilityProvider interface {
	ProviderRuntimeCapability() ProviderRuntimeCapability
}

// CarryoverCapabilityProvider is an optional generator extension that lets a
// provider declare whether it can participate in compact or live carryover
// across adjacent asks.
type CarryoverCapabilityProvider interface {
	CarryoverCapability() CarryoverCapability
}

type CarryoverPolicy struct {
	DefaultMode             CarryoverMode
	LiveCarryoverMaxAsks    int
	LiveCarryoverIdleTTL    time.Duration
	LiveCarryoverSoftTokens int
	LiveCarryoverHardTokens int
	MaxRawToolCarryTokens   int
}

func DefaultCarryoverPolicy() CarryoverPolicy {
	return CarryoverPolicy{
		DefaultMode:             CarryoverModeCompact,
		LiveCarryoverMaxAsks:    4,
		LiveCarryoverIdleTTL:    15 * time.Minute,
		LiveCarryoverSoftTokens: 8000,
		LiveCarryoverHardTokens: 12000,
		MaxRawToolCarryTokens:   2000,
	}
}

type CarryoverState struct {
	Mode                   CarryoverMode
	Provider               string
	Model                  string
	ConversationHandle     string
	ConversationID         string
	TopicFingerprint       string
	KBFingerprint          string
	StartedAtUnixMilli     int64
	LastUsedAtUnixMilli    int64
	AskCount               int
	EstimatedCarryTokens   int
	EstimatedRawToolTokens int
	LastOutcomeFacts       []string
	LastRecipeIDs          []string
	LastToolNames          []string
	RetryLearnings         []RetryLearning
	LastUserQuery          string
	LastAssistantSummary   string
	LastResetReason        CarryoverResetReason
}

// ReActLoop owns a full multi-turn ReAct loop lifecycle rather than just a
// single-turn prompt adaptation.
type ReActLoop interface {
	Run(ctx context.Context, req ReasoningRequest) (ReasoningResponse, error)
}

type ReasoningRequest struct {
	SystemPrompt   string
	ContextText    string
	HistoryText    string
	UserQuery      string
	Executor       ToolExecutor
	Generator      Generator
	ModelProfile   ModelProfile
	CarryoverMode  CarryoverMode
	CarryoverState *CarryoverState
	HydrationSink  MemoryHydrationSink
	Streamer       func(eventType string, data any) // Optional NDJSON callback
	// Verbose enables structured progress/tool-result streaming during the reasoning loop.
	Verbose bool
	// ForceToolCall instructs provider-owned loops to set tool_choice=required so
	// the model must emit a tool call rather than a conversational text response.
	ForceToolCall bool
}

type MemoryHydrationUpdate struct {
	FinalText      string
	ToolCalls      []ToolCall
	OutcomeFacts   []string
	OutcomeRecipes []LearnedRecipe
	CarryoverState *CarryoverState
}

type MemoryHydrationSink func(update MemoryHydrationUpdate)

// ReActToolResultExecutionMode controls how much of a tool result should be
// surfaced to the next model turn versus the UI stream.
//
// The modes are:
//   - summary: use a concise summary for investigatory tool calls.
//   - structured_summary: preserve a compact, structured preview for
//     intermediate results that are still meaningful but too large to replay
//     verbatim.
//   - full_small_only: keep the next turn compact and preserve only the
//     essential facts, while still passing through the full payload to the UI.
//   - full: preserve the full payload for the next model turn without
//     summarization, useful when the model needs complete access to the tool output.
type ReActToolResultExecutionMode string

const (
	ReActToolResultExecutionModeSummary           ReActToolResultExecutionMode = "summary"
	ReActToolResultExecutionModeStructuredSummary ReActToolResultExecutionMode = "structured_summary"
	ReActToolResultExecutionModeFullSmallOnly     ReActToolResultExecutionMode = "full_small_only"
	ReActToolResultExecutionModeFull              ReActToolResultExecutionMode = "full"
)

// ReActToolResult is the provider-neutral per-step state that the ReAct
// engine can expose to provider-specific loop adapters.
type ReActToolResult struct {
	Name          string
	Args          map[string]any
	Result        string // Inline result or summary if file-backed
	ResultFile    string // Path to full output file (if large)
	ResultSize    int64  // Byte count of original result
	Hint          *ToolProgressHint
	RetryGuidance *RetryGuidance
	ExecutionMode ReActToolResultExecutionMode
}

// ReActPendingRepair is a provider-neutral snapshot of a pending repair path.
type ReActPendingRepair struct {
	ToolName       string
	Strategy       string
	ResearchTool   string
	ResearchReason string
}

type ReActLoopPhase string

const (
	ReActLoopPhaseActive            ReActLoopPhase = "active"
	ReActLoopPhaseRepair            ReActLoopPhase = "repair"
	ReActLoopPhaseClarification     ReActLoopPhase = "clarification"
	ReActLoopPhaseMalformedToolCall ReActLoopPhase = "malformed_tool_call"
)

type ReActNextAction string

const (
	ReActNextActionCallTool         ReActNextAction = "call_tool"
	ReActNextActionRetrySameTool    ReActNextAction = "retry_same_tool"
	ReActNextActionAskClarification ReActNextAction = "ask_clarification"
	ReActNextActionAnswerUser       ReActNextAction = "answer_user"
)

// ReActLoopState captures the current loop-owned state so provider-owned loops
// can observe or eventually own the same transition surface.
type ReActLoopState struct {
	Iteration                int
	AllowedIterations        int
	MaxIterations            int
	RemainingToolCalls       int
	RepairAttempts           int
	Phase                    ReActLoopPhase
	AllowedNextActions       []ReActNextAction
	PendingRepair            *ReActPendingRepair
	PendingMalformedToolCall bool
	ToolResults              []ReActToolResult
	ToolCallContinuations    []ToolCallContinuation
}

// ReActTurn captures a single ReAct generation turn before it reaches a
// provider-specific generator. Providers can adapt the prompt/options while the
// engine retains ownership of retry policy and tool execution.
type ReActTurn struct {
	Iteration       int
	UserQuery       string
	Prompt          string
	RepairDirective string
	LoopState       ReActLoopState
	Options         GenOptions
	ToolResults     []ReActToolResult
	FinalTurn       bool
}

// ReActTurnStrategy is the shared loop-facing contract for shaping a single
// ReAct generation turn before it reaches the provider transport.
type ReActTurnStrategy interface {
	PrepareTurn(ctx context.Context, turn ReActTurn) ReActTurn
}

// ReActPromptBypassStrategy is an optional strategy hook that lets a provider
// skip generic synthetic prompt construction when carried tool-call state is
// sufficient to continue the interaction.
type ReActPromptBypassStrategy interface {
	ShouldBypassPrompt(turn ReActTurn) bool
}

// ReActTurnStrategyProvider lets generators return a provider-specific ReAct
// loop implementation while the engine keeps ownership of retry and execution policy.
type ReActTurnStrategyProvider interface {
	ReActTurnStrategy() ReActTurnStrategy
}

type ReActLoopMode string

const (
	ReActLoopModeShared ReActLoopMode = "shared"
	ReActLoopModeOwned  ReActLoopMode = "owned"
)

// ReActLoopModeProvider lets a generator declare whether it should run through
// the shared compatible loop or keep ownership of its provider-specific loop.
//
// Shared mode fits providers whose loop can be expressed via the common engine.
// Owned mode fits providers with transport-native session state, hydration, or
// multi-call semantics that still need a provider-specific outer loop.
type ReActLoopModeProvider interface {
	ReActLoopMode() ReActLoopMode
}

// ReActLoopProvider lets generators return a provider-owned ReAct loop that can
// take over retry, repair, continuation, and termination policy end-to-end.
type ReActLoopProvider interface {
	ReActLoop() ReActLoop
}

// DefaultReActTurnStrategy preserves the generic synthetic-prompt behavior.
// It exists as an explicit implementation so providers can extend or replace it.
type DefaultReActTurnStrategy struct{}

func (DefaultReActTurnStrategy) PrepareTurn(ctx context.Context, turn ReActTurn) ReActTurn {
	_ = ctx
	return turn
}

// LearnedRecipe captures a reusable protocol distilled from successful reasoning behavior.
// It is intentionally separate from grounded facts.
type LearnedRecipe struct {
	ID         string
	Kind       string
	Scope      string
	Domain     string
	Topic      string
	Trigger    string
	Protocol   []string
	Invariants []string
	Confidence float64
	Source     string
}

type ReasoningResponse struct {
	FinalText      string
	ToolCalls      []ToolCall // For audit/logging
	OutcomeFacts   []string   // Compact grounded facts safe to carry into outer MRU continuity
	OutcomeRecipes []LearnedRecipe
	// ClarificationRequest carries a structured clarification handoff emitted by
	// a loop when one missing fact blocks further progress.
	ClarificationRequest *ClarificationRequest
	// CarryoverState lets provider-owned loops return updated continuity metadata,
	// such as a reusable live conversation handle, without exposing transport
	// details to the service layer.
	CarryoverState *CarryoverState
	// EscalationState captures progressive retry escalation metadata when Tier 3
	// (Clarify) is reached, enabling phase succession to trigger backward transitions.
	// Fields: "tier" (int), "failed_tool" (string), "failure_count" (int),
	// "strategy" (string), "suggested_action" (string), "error_type" (string)
	EscalationState map[string]any
}

// PolicyEngine defines the interface for evaluating content against safety policies.
type PolicyEngine interface {
	// Evaluate checks the content against the policy rules.
	Evaluate(ctx context.Context, stage string, sample ContentSample, labels []Label) (PolicyDecision, error)
}

// Classifier defines the interface for content classification models.
type Classifier interface {
	// Name returns the name of the classifier.
	Name() string
	// Classify analyzes the content and returns a list of labels.
	Classify(ctx context.Context, sample ContentSample) ([]Label, error)
}

// ContentSample represents the content being evaluated for safety.
type ContentSample struct {
	Text string
	Meta map[string]any
}

// Label represents a classification result (e.g., "profanity", "hate_speech").
type Label struct {
	Name   string
	Score  float32
	Source string
}

// PolicyDecision represents the outcome of a policy evaluation.
type PolicyDecision struct {
	Action   string   // "allow", "block", "flag"
	Reasons  []string // Why the action was taken
	PolicyID string
}

// Domain represents a vertical or specific AI application domain.
type Domain[T any] interface {
	ID() string
	Name() string
	Embedder() Embeddings
	// Index returns the vector index used for retrieval.
	// It requires a transaction to be passed in.
	Index(ctx context.Context, tx sop.Transaction) (VectorStore[T], error)
	// Memory returns the new Cognitive Knowledge Base for retrieval and episodic reasoning.
	// Returns any to prevent circular dependencies with the memory package.
	Memory(ctx context.Context, tx sop.Transaction) (any, error)
	// TextIndex returns the text search index used for retrieval.
	TextIndex(ctx context.Context, tx sop.Transaction) (TextIndex, error)
	// BeginTransaction starts a new transaction for the domain's underlying storage.
	BeginTransaction(ctx context.Context, mode sop.TransactionMode) (sop.Transaction, error)
	Policies() PolicyEngine
	Classifier() Classifier
	Prompt(ctx context.Context, kind string) (string, error)
	DataPath() string
}

// ConfigMap holds configuration parameters for Agent.Ask calls.
// This is the "goo" type used at the Agent interface boundary for pipeline flexibility.
// Individual agent implementations should define their own typed structs and convert from ConfigMap.
type ConfigMap struct {
	values map[string]any
}

// NewConfigMap creates a new empty ConfigMap.
func NewConfigMap() *ConfigMap {
	return &ConfigMap{values: make(map[string]any)}
}

func (c *ConfigMap) Get(key string) (any, bool) {
	if c == nil || c.values == nil {
		return nil, false
	}
	value, exists := c.values[key]
	return value, exists
}

func (c *ConfigMap) Set(key string, value any) {
	if c == nil {
		return
	}
	if c.values == nil {
		c.values = make(map[string]any)
	}
	c.values[key] = value
}

// ToMap returns the underlying map for marshaling/conversion.
func (c *ConfigMap) ToMap() map[string]any {
	if c == nil || c.values == nil {
		return make(map[string]any)
	}
	return c.values
}

// IsNewTopicFromContext returns the is-new-topic flag stamped into the context by applyAskOptions.
// Returns false if the flag was not set (treat as topic continuation).
func IsNewTopicFromContext(ctx context.Context) bool {
	if v, ok := ctx.Value(CtxKeyIsNewTopic).(bool); ok {
		return v
	}
	return false
}

// Agent defines the interface for an AI agent service.
type Agent[T any] interface {
	// Lifecycle methods
	Open(ctx context.Context) error
	Close(ctx context.Context) error

	Search(ctx context.Context, query string, limit int) ([]Hit[T], error)
	Ask(ctx context.Context, query string, cfg *ConfigMap) (string, error)
}

// SessionPayload represents the context and state for an agent session.
// It carries domain-specific artifacts like database connections.
type SessionPayload struct {
	// CurrentDB is the active database name for the session.
	CurrentDB string
	// CurrentUserQuery stores the raw user request for the current Ask interaction.
	// It is used sparingly for narrow tool fallbacks when the model omits a required arg.
	CurrentUserQuery string
	// UserID identifies the user for privacy and memory isolation.
	UserID string
	// ClientID tags the origin client device or application.
	ClientID string
	// SessionID tracks the specific transient connection/session.
	SessionID string
	// AvatarScope tags the session with the active KBContextID.
	// Used by Omni-Protocol to know which sandbox the conversation occurred within.
	AvatarScope string
	// AgentID represents whether Omni or a specific Avatar generated this payload/memory
	AgentID string
	// ActiveDomain is the knowledge domain selected by the user.
	ActiveDomain string
	// ActiveRepo stores the currently active repository path or identifier when the session is working in a repo.
	ActiveRepo string
	// SelectedKBs are the explicit Knowledge Bases selected by the user from the dropdown.
	SelectedKBs []ArtifactReference
	// Transaction holds the active transaction for the session.
	// Deprecated: Migrate usage to TransactionPool.
	Transaction any
	// TransactionPool manages active transactions, their configurations, and pooling across the session.
	TransactionPool *TransactionPool
	// Transactions holds active transactions keyed by database name.
	// Deprecated: Migrate usage to TransactionPool.
	Transactions map[string]any
	// Variables holds session-scoped variables (e.g. cached store instances).
	Variables map[string]any
	// ExplicitTransaction marks when language bindings (Python, Rust, Java, .NET) are managing
	// their own transaction lifecycle via manage_transaction API.
	//
	// When ExplicitTransaction = true:
	//   - Transaction is managed EXTERNALLY by language binding code
	//   - SessionPayload.Close() will NOT auto-commit or auto-rollback
	//   - Language bindings must explicitly call commit/rollback via manage_transaction
	//   - This allows transactions to span multiple API calls (e.g., HTTP requests)
	//
	// When ExplicitTransaction = false (default):
	//   - Transaction is managed INTERNALLY by the agent session
	//   - SessionPayload.Close() will auto-commit the transaction
	//   - Transaction lifecycle is scoped to a single Ask/Execute call
	//
	// WARNING: ExplicitTransaction = true is prone to transaction leaks if language bindings
	// don't properly commit/rollback. Only use when you need multi-request transaction scope.
	ExplicitTransaction bool
	// RetryRewriteState tracks an explicit retry-the-same-ask rewrite.
	RetryRewriteState *RetryRewriteState
	// ClarificationState tracks an explicit pending clarification round before execution resumes.
	ClarificationState *ClarificationState
	// LastInteractionSteps tracks the number of steps added/executed in the last user interaction.
	LastInteractionSteps int
	// ConversationHistory stores the active memory/transcript for the session.
	ConversationHistory string
	// CognitiveState is the shared session-brain snapshot that can be observed by
	// the Ask pipeline, ReAct loop, workstreams, and governance policies.
	CognitiveState *SessionCognitiveState
}

// SessionCognitiveState is a compact, shared view of the current session's
// cognitive state. It is intentionally lightweight so it can be updated
// automatically from the existing Ask/ReAct/phase machinery without becoming a
// separate controller.
type SessionCognitiveState struct {
	CurrentTurn          int
	CurrentActor         string
	CurrentPhase         string
	CurrentQuery         string
	RecentQueries        []string
	PendingClarification bool
	ClarificationStatus  string
	QueuePaused          bool
	RetryTier            int
	EscalationTier       int
	LastToolOutcome      string
	TerminalReason       string
	LastDecision         string
	LastDecisionReason   string
	MemorySignals        []string
	RecentToolNames      []string
	RecentProgressDeltas []float64
	RecentFailureClasses []string
	LoopRisk             string
	LastAssessment       *SessionCognitiveAssessment
}

type CognitiveFailureClass string

const (
	CognitiveFailureClassNone              CognitiveFailureClass = "none"
	CognitiveFailureClassImplementationBug CognitiveFailureClass = "implementation_bug"
	CognitiveFailureClassMissingFact       CognitiveFailureClass = "missing_fact"
	CognitiveFailureClassWrongTool         CognitiveFailureClass = "wrong_tool"
	CognitiveFailureClassWrongPhase        CognitiveFailureClass = "wrong_phase"
	CognitiveFailureClassRequirementsShift CognitiveFailureClass = "requirements_shift"
	CognitiveFailureClassEnvironmentBlock  CognitiveFailureClass = "environment_block"
	CognitiveFailureClassTerminalFailure   CognitiveFailureClass = "terminal_failure"
	CognitiveFailureClassLooping           CognitiveFailureClass = "looping"
)

type CognitiveProgressState string

const (
	CognitiveProgressStateUnknown     CognitiveProgressState = "unknown"
	CognitiveProgressStateProgressing CognitiveProgressState = "progressing"
	CognitiveProgressStateStalled     CognitiveProgressState = "stalled"
	CognitiveProgressStateRegressing  CognitiveProgressState = "regressing"
	CognitiveProgressStateLooping     CognitiveProgressState = "looping"
	CognitiveProgressStateComplete    CognitiveProgressState = "complete"
)

type CognitivePhaseAction string

const (
	CognitivePhaseActionStayCurrent  CognitivePhaseAction = "stay_current"
	CognitivePhaseActionMoveForward  CognitivePhaseAction = "move_forward"
	CognitivePhaseActionMoveBackward CognitivePhaseAction = "move_backward"
	CognitivePhaseActionExit         CognitivePhaseAction = "exit"
)

type CognitiveRetryAction string

const (
	CognitiveRetryActionContinue         CognitiveRetryAction = "continue"
	CognitiveRetryActionRetrySameTool    CognitiveRetryAction = "retry_same_tool"
	CognitiveRetryActionRetryDifferent   CognitiveRetryAction = "retry_different_tool"
	CognitiveRetryActionResearchFirst    CognitiveRetryAction = "research_first"
	CognitiveRetryActionAskClarification CognitiveRetryAction = "ask_clarification"
	CognitiveRetryActionStop             CognitiveRetryAction = "stop"
)

// SessionCognitiveObservation is the factual runtime view assembled by the
// Ask/ReAct/phase machinery before any advisory policy interprets what to do next.
// It is intentionally descriptive rather than prescriptive.
type SessionCognitiveObservation struct {
	CurrentTurn          int               `json:"current_turn,omitempty"`
	CurrentActor         string            `json:"current_actor,omitempty"`
	CurrentPhase         string            `json:"current_phase,omitempty"`
	CurrentQuery         string            `json:"current_query,omitempty"`
	RecentQueries        []string          `json:"recent_queries,omitempty"`
	PendingClarification bool              `json:"pending_clarification,omitempty"`
	ClarificationStatus  string            `json:"clarification_status,omitempty"`
	QueuePaused          bool              `json:"queue_paused,omitempty"`
	RetryTier            int               `json:"retry_tier,omitempty"`
	EscalationTier       int               `json:"escalation_tier,omitempty"`
	LastToolOutcome      string            `json:"last_tool_outcome,omitempty"`
	TerminalReason       string            `json:"terminal_reason,omitempty"`
	RecentToolNames      []string          `json:"recent_tool_names,omitempty"`
	RecentOutcomes       []string          `json:"recent_outcomes,omitempty"`
	RecentProgressDeltas []float64         `json:"recent_progress_deltas,omitempty"`
	RecentFailureClasses []string          `json:"recent_failure_classes,omitempty"`
	MemorySignals        []string          `json:"memory_signals,omitempty"`
	LatestHint           *ToolProgressHint `json:"latest_hint,omitempty"`
	LatestRetryGuidance  *RetryGuidance    `json:"latest_retry_guidance,omitempty"`
	Metadata             map[string]any    `json:"metadata,omitempty"`
}

// SessionCognitiveAssessment is the interpreted view of a session situation.
// It is designed for structured LLM or hybrid policy output and can be persisted
// back into SessionCognitiveState without turning the policy layer into a controller.
type SessionCognitiveAssessment struct {
	FailureClass        CognitiveFailureClass  `json:"failure_class,omitempty"`
	ProgressState       CognitiveProgressState `json:"progress_state,omitempty"`
	PhaseAction         CognitivePhaseAction   `json:"phase_action,omitempty"`
	RetryAction         CognitiveRetryAction   `json:"retry_action,omitempty"`
	SuggestedPhase      string                 `json:"suggested_phase,omitempty"`
	ClarificationNeeded bool                   `json:"clarification_needed,omitempty"`
	LoopRisk            string                 `json:"loop_risk,omitempty"`
	Confidence          string                 `json:"confidence,omitempty"`
	Rationale           string                 `json:"rationale,omitempty"`
}

// ClarificationState tracks a pending assistant clarification before the target ask resumes.
type ClarificationState struct {
	TargetQuery        string
	AssistantQuestion  string
	OriginalUserQuery  string
	UserClarification  string
	EffectiveResumeAsk string
	Status             string
	Source             string
	MissingFact        string
	Reason             string
}

// ClarificationRequest carries a structured clarification handoff emitted by a
// reasoning loop when one missing fact blocks further tool progress.
type ClarificationRequest struct {
	TargetQuery       string `json:"target_query,omitempty"`
	AssistantQuestion string `json:"assistant_question,omitempty"`
	MissingFact       string `json:"missing_fact,omitempty"`
	Reason            string `json:"reason,omitempty"`
	Source            string `json:"source,omitempty"`
	Status            string `json:"status,omitempty"`
}

// RetryRewriteState tracks a retry-the-same-ask rewrite through Gate 0.
type RetryRewriteState struct {
	OriginalQuery string
	ResolvedQuery string
	Status        string
}

// Manages Payload's cleanup, e.g. Transaction Commit/Rollback.
func (sp *SessionPayload) Close(ctx context.Context) error {
	if sp.Transaction == nil {
		return nil
	}
	defer func() {
		sp.Transaction = nil
	}()

	if sp.Transactions != nil {
		// Iterate to remove if any global dbName holds this tx
		for k, v := range sp.Transactions {
			if v == sp.Transaction {
				delete(sp.Transactions, k)
			}
		}
	}

	if sp.ExplicitTransaction {
		// CRITICAL: Explicit transactions are managed externally by language bindings
		// via the manage_transaction API. They must NOT be auto-committed or auto-rolled back
		// by SessionPayload.Close() because:
		//   1. The transaction may span multiple API calls (multi-request scope)
		//   2. Language binding code is responsible for commit/rollback
		//   3. Auto-managing would break the external transaction lifecycle
		// See TestTransactionLifecycle_ExplicitTransaction for validation.
		return nil
	}

	// Auto-commit implicit (session-managed) transactions
	if tx, ok := sp.Transaction.(sop.Transaction); ok {
		if tx.HasBegun() {
			return tx.Commit(ctx)
		}
	}
	return nil
}

func (s *SessionPayload) GetDatabase() string {
	return s.CurrentDB
}

// GetSessionPayload retrieves the session payload from the context.
func GetSessionPayload(ctx context.Context) *SessionPayload {
	if val := ctx.Value("session_payload"); val != nil {
		if p, ok := val.(*SessionPayload); ok {
			return p
		}
	}
	return nil
}

// ToolExecutor defines the interface for the application to expose capabilities to the Agent.
// This allows the Agent to "act" on the application (e.g. query DB, send email).
type ToolExecutor interface {
	// Execute runs a named tool with the provided arguments.
	Execute(ctx context.Context, toolName string, args map[string]any) (string, error)
	// ListTools returns the list of available tools.
	ListTools(ctx context.Context) ([]ToolDefinition, error)
}

// ToolProgressHint is an optional progress envelope that a tool can return to guide
// the native Ask loop toward the next narrowing step without directly mutating scratchpad state.
type ToolProgressHint struct {
	Status             string   `json:"status,omitempty"`
	CompletionDelta    float64  `json:"completion_delta,omitempty"`
	Confidence         float64  `json:"confidence,omitempty"`
	Missing            []string `json:"missing,omitempty"`
	Tips               []string `json:"tips,omitempty"`
	Clues              []string `json:"clues,omitempty"`
	SuggestedNextTools []string `json:"suggested_next_tools,omitempty"`
	// SuggestedAction guides retry strategy: "research_before_retry", "retry_with_changes", "give_up_and_clarify"
	SuggestedAction string `json:"suggested_action,omitempty"`
}

// ToolResultEnvelope is an optional JSON result shape that tools can return.
// If present, `tool_result` remains the user-visible payload while `progress_hint`
// feeds the engine's internal progress and retry budgeting, and `file_metadata`
// provides information about file-backed storage for large results.
type ToolResultEnvelope struct {
	ToolResult   json.RawMessage   `json:"tool_result,omitempty"`
	ProgressHint *ToolProgressHint `json:"progress_hint,omitempty"`
	FileMetadata *ToolFileMetadata `json:"file_metadata,omitempty"`
}

// ToolFileMetadata describes file-backed storage for large tool results.
type ToolFileMetadata struct {
	FilePath string `json:"file_path,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
}

// ToolDefinition describes a tool available to the agent.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Schema is a JSON schema string describing the arguments.
	Schema string `json:"schema"`
}

// AgentControl defines methods to manage the agent's lifecycle.
// This allows the Application to control the Agent (e.g. Stop/Pause).
type AgentControl interface {
	// Stop aborts the current operation.
	Stop() error
	// Pause suspends the agent's activities (if supported).
	Pause() error
	// Resume resumes the agent's activities.
	Resume() error
}

// TextIndex defines the interface for a text search index.
type TextIndex interface {
	// Add indexes a document.
	Add(ctx context.Context, docID string, text string) error
	// Search performs a text search.
	Search(ctx context.Context, query string) ([]search.TextSearchResult, error)
}

// ModelStore defines the interface for persisting and retrieving AI models.
// It allows for the management of "Skills" (small models) and "Brains" (large models).
type ModelStore interface {
	// Save persists a model with the given name and category.
	// The model can be any serializable object (e.g., Perceptron, NeuralNet).
	Save(ctx context.Context, category string, name string, model any) error

	// Load retrieves a model by name and category and populates the provided object.
	// The target parameter must be a pointer to the model struct.
	Load(ctx context.Context, category string, name string, target any) error

	// List returns the names of all stored models in a given category.
	List(ctx context.Context, category string) ([]string, error)

	// Delete removes a model from the store.
	Delete(ctx context.Context, category string, name string) error
}

// Script represents a recorded sequence of user interactions or a programmed script.
type Script struct {
	Description string   `json:"description"`
	Parameters  []string `json:"parameters"`         // Input parameters for the script
	Database    string   `json:"database,omitempty"` // Database to run the script against
	Portable    bool     `json:"portable,omitempty"` // If true, allows running on any database
	// TransactionMode specifies how transactions are handled for this script.
	// Values: "none" (default, manual), "single" (one tx for all steps), "per_step" (auto-commit each step)
	TransactionMode string       `json:"transaction_mode,omitempty"`
	Steps           []ScriptStep `json:"steps"`
}

// ScriptStep represents a single instruction in a script.
// It follows a stabilized schema for "Natural Language Programming".
type ScriptStep struct {
	// Type of the step.
	// Valid values: "ask", "set", "if", "loop", "fetch", "say", "command", "call_script" (or "script"), "block"
	Type string `json:"type"`

	// Name acts as a label or identifier for the step.
	// It can be used for UI display or referenced in complex flows.
	Name string `json:"name,omitempty"`

	// --- Fields for "ask" (LLM Interaction) ---
	// Prompt is the question or command to send to the LLM. Supports templating.
	Prompt string `json:"prompt,omitempty"`
	// OutputVariable is where the LLM's response will be stored.
	// If empty, the response is just printed.
	OutputVariable string `json:"output_variable,omitempty"`
	// InputVariable is the variable to use as input for this step.
	InputVariable string `json:"input_variable,omitempty"`

	// --- Fields for "set" (Variable Assignment) ---
	// Variable is the name of the variable to set.
	Variable string `json:"variable,omitempty"`
	// Value is the value to assign. Supports templating.
	Value string `json:"value,omitempty"`

	// --- Fields for "if" (Flow Branching) ---
	// Condition is a Go template expression that must evaluate to "true".
	// Example: "{{ gt .count 5 }}"
	Condition string `json:"condition,omitempty"`
	// Then is the list of steps to execute if the condition is true.
	Then []ScriptStep `json:"then,omitempty"`
	// Else is the list of steps to execute if the condition is false.
	Else []ScriptStep `json:"else,omitempty"`

	// --- Fields for "loop" (Iteration) ---
	// List is the variable name or expression evaluating to a list/slice to iterate over.
	List string `json:"list,omitempty"`
	// Iterator is the variable name for the current item in the loop.
	Iterator string `json:"iterator,omitempty"`
	// Steps is the list of steps to execute for each item.
	// Also used for "block" type to hold the sequence of steps.
	Steps []ScriptStep `json:"steps,omitempty"`

	// --- Fields for "fetch" (Data Retrieval) ---
	// Database specifies the database name (optional). If empty, uses the current context database.
	Database string `json:"database,omitempty"`
	// Source specifies the data source type (e.g., "btree", "vector_store").
	Source string `json:"source,omitempty"`
	// Resource specifies the name of the resource (e.g., table name).
	Resource string `json:"resource,omitempty"`
	// Filter is an optional filter expression (not yet fully implemented).
	Filter string `json:"filter,omitempty"`
	// (Uses Variable field to store the result)

	// --- Fields for "say" (Output) ---
	// Message is the text to output to the user. Supports templating.
	Message string `json:"message,omitempty"`

	// --- Fields for "command" (Direct Tool Execution) ---
	// Command is the name of the tool/command to execute.
	Command string `json:"command,omitempty"`
	// Args are the arguments for the command.
	Args map[string]any `json:"args,omitempty"`

	// --- Fields for "call_script" (Nested Script Execution) ---
	// ScriptName is the name of the script to execute.
	ScriptName string `json:"script_name,omitempty"`
	// ScriptArgs are the arguments to pass to the script.
	ScriptArgs map[string]string `json:"script_args,omitempty"`

	// --- Async Execution ---
	// IsAsync specifies if the step should be executed asynchronously.
	// If true, the step runs in a goroutine and the script continues to the next step.
	// All async steps are gathered (waited for) at the end of the script execution.
	IsAsync bool `json:"is_async,omitempty"`

	// ContinueOnError specifies if the script should continue executing if this step fails.
	// If false (default), the script stops and returns the error.
	// For async steps, if this is false and the step fails, it cancels the execution of other steps.
	ContinueOnError bool `json:"continue_on_error,omitempty"`

	// --- Documentation ---
	// Description provides a human-readable explanation of what this step does.
	// It is used for documentation and self-explanation of the script.
	Description string `json:"description,omitempty"`
}

// ScriptRecorder is an interface for recording script steps.
type ScriptRecorder interface {
	RecordStep(ctx context.Context, step ScriptStep)
	// RefactorLastSteps refactors the last N steps into a new structure (script or block).
	// count: number of steps to refactor.
	// mode: "script" (extract to new named script) or "block" (group into block step).
	// name: name of the new script (if mode is "script").
	RefactorLastSteps(count int, mode string, name string) error
}

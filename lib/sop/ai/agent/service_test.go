package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/memory"
	"github.com/sharedcode/sop/ai/obfuscation"
	"github.com/sharedcode/sop/btree"
)

// MockGenerator implements ai.Generator for testing.
type MockGenerator struct {
	Response string
}

func (m *MockGenerator) Name() string {
	return "mock"
}

func (m *MockGenerator) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	// For testing native tool calls since we moved away from JSON regex parsing
	var tc []ai.ToolCall

	// Obfuscation test match:
	// dbHash might be anything since it's md5. We just examine m.Response to see if it looks like the obfuscation test responses.
	if strings.Contains(m.Response, `{"tool": "select", "args": {"database": "`) && strings.Contains(m.Response, `"store": "`) {
		// Mock decoding the string back (test logic will check the obfuscated payload behavior)
		// For the purpose of the test, we mock what the LLM *would* have returned as ToolCalls natively:

		// Extract dbhash and storehash manually from m.Response since it's dynamic
		// Very brittle, but it's a test mock.
		dbStart := strings.Index(m.Response, `"database": "`) + 13
		dbEnd := strings.Index(m.Response[dbStart:], `"`) + dbStart
		storeStart := strings.Index(m.Response, `"store": "`) + 10
		storeEnd := strings.Index(m.Response[storeStart:], `"`) + storeStart

		tc = append(tc, ai.ToolCall{
			Name: "select",
			Args: map[string]any{
				"database": strings.ReplaceAll(m.Response[dbStart:dbEnd], `\u00a0`, "\u00a0"),
				"store":    m.Response[storeStart:storeEnd],
			},
		})
	}

	return ai.GenOutput{
		Text:      m.Response,
		ToolCalls: tc,
	}, nil
}

func (m *MockGenerator) EstimateCost(inTokens, outTokens int) float64 {
	return 0
}

func (m *MockGenerator) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
	return nil
}

// MockEmbedder implements ai.Embeddings for testing.
type MockEmbedder struct{}

func (m *MockEmbedder) Name() string { return "mock" }
func (m *MockEmbedder) Dim() int     { return 1536 }
func (m *MockEmbedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	return make([][]float32, len(texts)), nil
}

// MockDomain implements ai.Domain for testing.
type MockDomain struct{}

func (m *MockDomain) ID() string              { return "mock" }
func (m *MockDomain) Name() string            { return "Mock Domain" }
func (m *MockDomain) Embedder() ai.Embeddings { return &MockEmbedder{} }
func (m *MockDomain) Index(ctx context.Context, tx sop.Transaction) (ai.VectorStore[map[string]any], error) {
	return &MockVectorStore{}, nil
}
func (m *MockDomain) TextIndex(ctx context.Context, tx sop.Transaction) (ai.TextIndex, error) {
	return nil, nil
}
func (m *MockDomain) BeginTransaction(ctx context.Context, mode sop.TransactionMode) (sop.Transaction, error) {
	return &MockTransaction{}, nil
}
func (m *MockDomain) Policies() ai.PolicyEngine { return nil }
func (m *MockDomain) Classifier() ai.Classifier { return nil }
func (m *MockDomain) Prompt(ctx context.Context, kind string) (string, error) {
	return "System Prompt", nil
}
func (m *MockDomain) Memory(ctx context.Context, tx sop.Transaction) (any, error) { return nil, nil }
func (m *MockDomain) DataPath() string                                            { return "/tmp" }

// MockTransaction implements sop.Transaction for testing.
type MockTransaction struct{}

func (m *MockTransaction) Begin(ctx context.Context) error                                        { return nil }
func (m *MockTransaction) Commit(ctx context.Context) error                                       { return nil }
func (m *MockTransaction) Rollback(ctx context.Context) error                                     { return nil }
func (m *MockTransaction) HasBegun() bool                                                         { return true }
func (m *MockTransaction) GetPhasedTransaction() sop.TwoPhaseCommitTransaction                    { return nil }
func (m *MockTransaction) AddPhasedTransaction(otherTransaction ...sop.TwoPhaseCommitTransaction) {}
func (m *MockTransaction) GetStores(ctx context.Context) ([]string, error)                        { return nil, nil }
func (m *MockTransaction) Close() error                                                           { return nil }
func (m *MockTransaction) GetID() sop.UUID                                                        { return sop.UUID{} }
func (m *MockTransaction) CommitMaxDuration() time.Duration                                       { return 0 }
func (m *MockTransaction) OnCommit(callback func(ctx context.Context) error)                      {}

// MockVectorStore implements ai.VectorStore for testing.
type MockVectorStore struct{}

func (m *MockVectorStore) UpdateEmbedderInfo(ctx context.Context, provider string, model string, dimensions int) error {
	return nil
}

func (m *MockVectorStore) Upsert(ctx context.Context, item ai.Item[map[string]any]) error { return nil }
func (m *MockVectorStore) UpsertBatch(ctx context.Context, items []ai.Item[map[string]any]) error {
	return nil
}
func (m *MockVectorStore) Get(ctx context.Context, id string) (*ai.Item[map[string]any], error) {
	return nil, nil
}
func (m *MockVectorStore) Delete(ctx context.Context, id string) error { return nil }
func (m *MockVectorStore) Query(ctx context.Context, vec []float32, k int, filter func(map[string]any) bool) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}
func (m *MockVectorStore) Count(ctx context.Context) (int64, error)                    { return 0, nil }
func (m *MockVectorStore) AddCentroid(ctx context.Context, vec []float32) (int, error) { return 0, nil }
func (m *MockVectorStore) Optimize(ctx context.Context) error                          { return nil }
func (m *MockVectorStore) Consolidate(ctx context.Context) error                       { return nil }
func (m *MockVectorStore) SetDeduplication(enabled bool)                               {}
func (m *MockVectorStore) Centroids(ctx context.Context) (btree.BtreeInterface[int, ai.Centroid], error) {
	return nil, nil
}
func (m *MockVectorStore) Vectors(ctx context.Context) (btree.BtreeInterface[ai.VectorKey, []float32], error) {
	return nil, nil
}
func (m *MockVectorStore) Content(ctx context.Context) (btree.BtreeInterface[ai.ContentKey, string], error) {
	return nil, nil
}
func (m *MockVectorStore) Lookup(ctx context.Context) (btree.BtreeInterface[int, string], error) {
	return nil, nil
}
func (m *MockVectorStore) Version(ctx context.Context) (int64, error) { return 0, nil }

// MockExecutor implements ai.ToolExecutor for testing.
type MockExecutor struct {
	LastTool string
	LastArgs map[string]any
}

type listToolsTestAgent struct {
	tools []ai.ToolDefinition
}

func (a *listToolsTestAgent) Open(ctx context.Context) error  { return nil }
func (a *listToolsTestAgent) Close(ctx context.Context) error { return nil }
func (a *listToolsTestAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}
func (a *listToolsTestAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	return "", nil
}
func (a *listToolsTestAgent) Execute(ctx context.Context, toolName string, args map[string]any) (string, error) {
	return "", nil
}
func (a *listToolsTestAgent) ListTools(ctx context.Context) ([]ai.ToolDefinition, error) {
	return a.tools, nil
}

func (e *MockExecutor) Execute(ctx context.Context, toolName string, args map[string]any) (string, error) {
	e.LastTool = toolName
	e.LastArgs = args
	return "Executed", nil
}

func (e *MockExecutor) ListTools(ctx context.Context) ([]ai.ToolDefinition, error) {
	return nil, nil
}

func TestServiceToolExecutor_ListTools_ReflectsRegisteredAgents(t *testing.T) {
	expected := []ai.ToolDefinition{{Name: "alpha", Description: "alpha tool", Schema: `{"type":"object"}`}}
	svc := NewService(nil, nil, nil, nil, nil, map[string]ai.Agent[map[string]any]{
		"test": &listToolsTestAgent{tools: expected},
	}, false)

	tools, err := (&ServiceToolExecutor{s: svc}).ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != expected[0].Name {
		t.Fatalf("expected registered tool list to be returned, got %#v", tools)
	}
}

type recordingVectorStore struct {
	calls []string
	items []ai.Hit[map[string]any]
}

func (m *recordingVectorStore) UpdateEmbedderInfo(ctx context.Context, provider string, model string, dimensions int) error {
	return nil
}
func (m *recordingVectorStore) Upsert(ctx context.Context, item ai.Item[map[string]any]) error {
	return nil
}
func (m *recordingVectorStore) UpsertBatch(ctx context.Context, items []ai.Item[map[string]any]) error {
	return nil
}
func (m *recordingVectorStore) Get(ctx context.Context, id string) (*ai.Item[map[string]any], error) {
	return nil, nil
}
func (m *recordingVectorStore) Delete(ctx context.Context, id string) error { return nil }
func (m *recordingVectorStore) Query(ctx context.Context, vec []float32, k int, filter func(map[string]any) bool) ([]ai.Hit[map[string]any], error) {
	if filter != nil {
		filtered := make([]ai.Hit[map[string]any], 0, len(m.items))
		for _, hit := range m.items {
			if hit.Payload == nil {
				continue
			}
			if filter(hit.Payload) {
				filtered = append(filtered, hit)
			}
		}
		m.calls = append(m.calls, "filtered")
		return filtered, nil
	}
	m.calls = append(m.calls, "unfiltered")
	return m.items, nil
}
func (m *recordingVectorStore) Count(ctx context.Context) (int64, error) { return 0, nil }
func (m *recordingVectorStore) AddCentroid(ctx context.Context, vec []float32) (int, error) {
	return 0, nil
}
func (m *recordingVectorStore) Optimize(ctx context.Context) error    { return nil }
func (m *recordingVectorStore) Consolidate(ctx context.Context) error { return nil }
func (m *recordingVectorStore) SetDeduplication(enabled bool)         {}
func (m *recordingVectorStore) Centroids(ctx context.Context) (btree.BtreeInterface[int, ai.Centroid], error) {
	return nil, nil
}
func (m *recordingVectorStore) Vectors(ctx context.Context) (btree.BtreeInterface[ai.VectorKey, []float32], error) {
	return nil, nil
}
func (m *recordingVectorStore) Content(ctx context.Context) (btree.BtreeInterface[ai.ContentKey, string], error) {
	return nil, nil
}
func (m *recordingVectorStore) Lookup(ctx context.Context) (btree.BtreeInterface[int, string], error) {
	return nil, nil
}
func (m *recordingVectorStore) Version(ctx context.Context) (int64, error) { return 0, nil }

type recordingDomain struct {
	vectorStore *recordingVectorStore
}

func (m *recordingDomain) ID() string              { return "recording" }
func (m *recordingDomain) Name() string            { return "Recording Domain" }
func (m *recordingDomain) Embedder() ai.Embeddings { return &MockEmbedder{} }
func (m *recordingDomain) Index(ctx context.Context, tx sop.Transaction) (ai.VectorStore[map[string]any], error) {
	return m.vectorStore, nil
}
func (m *recordingDomain) TextIndex(ctx context.Context, tx sop.Transaction) (ai.TextIndex, error) {
	return nil, nil
}
func (m *recordingDomain) BeginTransaction(ctx context.Context, mode sop.TransactionMode) (sop.Transaction, error) {
	return &MockTransaction{}, nil
}
func (m *recordingDomain) Policies() ai.PolicyEngine                               { return nil }
func (m *recordingDomain) Classifier() ai.Classifier                               { return nil }
func (m *recordingDomain) Prompt(ctx context.Context, kind string) (string, error) { return "", nil }
func (m *recordingDomain) Memory(ctx context.Context, tx sop.Transaction) (any, error) {
	return nil, nil
}
func (m *recordingDomain) DataPath() string { return "/tmp" }

func TestBuildCategoryPathCandidates_UsesSemanticAndPathHints(t *testing.T) {
	candidates := buildCategoryPathCandidates("retry logic in Payments/Retry/Recovery")
	if len(candidates) == 0 {
		t.Fatal("expected semantic/path candidates, got none")
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		seen[c] = true
	}
	if !seen["Payments/Retry/Recovery"] {
		t.Fatalf("expected explicit path to be included, got %#v", candidates)
	}
	if !seen["Coding/Semantic/retry-logic"] && !seen["Semantic/retry-logic"] {
		t.Fatalf("expected semantic retry bucket to be included, got %#v", candidates)
	}
}

func TestServiceSearch_PrioritizesPathScopedResults(t *testing.T) {
	vs := &recordingVectorStore{items: []ai.Hit[map[string]any]{
		{ID: "path-hit", Score: 0.9, Payload: map[string]any{"category": "Payments/Retry/Recovery", "text": "retry audit path"}},
		{ID: "other-hit", Score: 0.8, Payload: map[string]any{"category": "General", "text": "other content"}},
	}}
	svc := &Service{domain: &recordingDomain{vectorStore: vs}}

	results, err := svc.Search(context.Background(), "path:Payments/Retry/Recovery retry logic", 10)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected path-scoped results")
	}
	if results[0].ID != "path-hit" {
		t.Fatalf("expected path-scoped result to rank first, got %#v", results)
	}
	if len(vs.calls) == 0 {
		t.Fatal("expected a scoped vector search call")
	}
}

func TestCategoryMatchesAnyPath_IsSegmentAwareForSemanticBuckets(t *testing.T) {
	if !categoryMatchesAnyPath("Engineering/Parsing/AST", []string{"Coding/Semantic/parsing"}) {
		t.Fatal("expected parsing semantic bucket to match the parsing hierarchy")
	}
	if categoryMatchesAnyPath("Engineering/UI/Navigation", []string{"Coding/Semantic/parsing"}) {
		t.Fatal("expected unrelated navigation category to avoid parsing semantic match")
	}
}

func TestRankMemoryRecallCandidates_PrefersExactPathAndSemanticMatches(t *testing.T) {
	candidates := []memoryRecallCandidate{
		{Source: "ltm", Label: "General", Summary: "generic retry guidance for shipping workflows", Category: "General", Recency: 0.5, Semantic: 0.25, TopicAffinity: 0.1, SourcePriority: 0.4, Continuity: 0.2, Outcome: 0.3},
		{Source: "mru", Label: "Payments/Retry/Recovery", Summary: "retry audit and recovery flow for payments", Category: "Payments/Retry/Recovery", Recency: 0.95, Semantic: 0.9, TopicAffinity: 0.9, SourcePriority: 0.85, Continuity: 0.9, Outcome: 0.9},
		{Source: "stm", Label: "Engineering/Payments", Summary: "last run showed payment retries are mirrored across retry workers", Category: "Engineering/Payments", Recency: 0.8, Semantic: 0.82, TopicAffinity: 0.8, SourcePriority: 0.7, Continuity: 0.8, Outcome: 0.75},
	}

	ranked := rankMemoryRecallCandidates("retry logic in Payments/Retry/Recovery", candidates, 3)
	if len(ranked) != 3 {
		t.Fatalf("expected 3 ranked candidates, got %d", len(ranked))
	}
	if ranked[0].Category != "Payments/Retry/Recovery" {
		t.Fatalf("expected exact path candidate to rank first, got %#v", ranked)
	}
	if ranked[1].Category != "Engineering/Payments" {
		t.Fatalf("expected semantic adjacent category to rank second, got %#v", ranked)
	}
}

func TestEvaluateMemoryRecallPolicy_ExpandsWhenExactPathMissing(t *testing.T) {
	policy := evaluateMemoryRecallPolicy("retry logic after payment errors", []memoryRecallCandidate{
		{Source: "ltm", Label: "General", Summary: "payment retries are common", Category: "General", Recency: 0.6, Semantic: 0.4, TopicAffinity: 0.3, SourcePriority: 0.4, Continuity: 0.2, Outcome: 0.2},
		{Source: "stm", Label: "Engineering/Payments", Summary: "retry worker issue", Category: "Engineering/Payments", Recency: 0.8, Semantic: 0.8, TopicAffinity: 0.7, SourcePriority: 0.6, Continuity: 0.7, Outcome: 0.6},
	}, 2)
	if !policy.Expand {
		t.Fatal("expected policy to trigger expansion when exact-path hit is absent")
	}
	if policy.ExactPathHits != 0 {
		t.Fatalf("expected zero exact-path hits, got %d", policy.ExactPathHits)
	}
	if policy.SemanticHits != 1 {
		t.Fatalf("expected one semantic hit, got %d", policy.SemanticHits)
	}
}

func TestSummarizeMemoryRecall_ReportsBudgetAndSourceMix(t *testing.T) {
	candidates := []memoryRecallCandidate{
		{Source: "mru", Label: "Payments/Retry/Recovery", Summary: "first retry recovered", Category: "Payments/Retry/Recovery", Recency: 0.9, Semantic: 0.8, TopicAffinity: 0.7, SourcePriority: 0.8, Continuity: 0.9, Outcome: 0.8},
		{Source: "stm", Label: "Engineering/Payments", Summary: "retry worker issue", Category: "Engineering/Payments", Recency: 0.8, Semantic: 0.8, TopicAffinity: 0.7, SourcePriority: 0.7, Continuity: 0.8, Outcome: 0.7},
		{Source: "ltm", Label: "Learned Knowledge", Summary: "system recovered after retry", Category: "LTM", Recency: 0.5, Semantic: 0.9, TopicAffinity: 0.6, SourcePriority: 0.5, Continuity: 0.5, Outcome: 0.6},
	}
	policy := evaluateMemoryRecallPolicy("retry logic in Payments/Retry/Recovery", candidates, 2)
	result := summarizeMemoryRecall("retry logic in Payments/Retry/Recovery", candidates, 2, policy)
	if result.TotalCandidates != 3 {
		t.Fatalf("expected 3 total candidates, got %d", result.TotalCandidates)
	}
	if result.BudgetLimit != 2 {
		t.Fatalf("expected budget limit 2, got %d", result.BudgetLimit)
	}
	if got := result.SourceCounts["mru"]; got != 1 {
		t.Fatalf("expected 1 mru hit, got %d", got)
	}
	if result.Expand {
		t.Fatal("expected no expansion when exact path hit exists")
	}
}

func TestPlanAskMemoryRecall_UsesExactPathFirstAndSemanticExpansion(t *testing.T) {
	withExact := planAskMemoryRecall("retry logic in Payments/Retry/Recovery", []memoryRecallCandidate{{Source: "mru", Label: "Payments/Retry/Recovery", Summary: "retry workflow", Category: "Payments/Retry/Recovery", Recency: 0.95, Semantic: 0.9, TopicAffinity: 0.9, SourcePriority: 0.85, Continuity: 0.9, Outcome: 0.9}, {Source: "stm", Label: "Engineering/Payments", Summary: "retry worker issue", Category: "Engineering/Payments", Recency: 0.8, Semantic: 0.8, TopicAffinity: 0.7, SourcePriority: 0.7, Continuity: 0.8, Outcome: 0.7}})
	if !withExact.Enabled {
		t.Fatal("expected exact-path recall to be enabled")
	}
	if withExact.Expand {
		t.Fatal("expected exact-path recall not to expand")
	}

	withSemanticOnly := planAskMemoryRecall("retry logic after payment errors", []memoryRecallCandidate{{Source: "ltm", Label: "General", Summary: "payment retry guidance", Category: "General", Recency: 0.6, Semantic: 0.4, TopicAffinity: 0.3, SourcePriority: 0.4, Continuity: 0.2, Outcome: 0.2}, {Source: "stm", Label: "Engineering/Payments", Summary: "retry worker issue", Category: "Engineering/Payments", Recency: 0.8, Semantic: 0.8, TopicAffinity: 0.7, SourcePriority: 0.6, Continuity: 0.7, Outcome: 0.6}})
	if !withSemanticOnly.Enabled {
		t.Fatal("expected semantic fallback recall to be enabled")
	}
	if !withSemanticOnly.Expand {
		t.Fatal("expected semantic fallback recall to expand")
	}

	staleOnly := planAskMemoryRecall("retry logic after payment errors", []memoryRecallCandidate{{Source: "ltm", Label: "General", Summary: "stale retry guidance", Category: "General", Recency: 0.2, Semantic: 0.2, TopicAffinity: 0.1, SourcePriority: 0.1, Continuity: 0.1, Outcome: 0.1, Stale: true}})
	if staleOnly.Enabled {
		t.Fatal("expected stale long-tail candidates to be excluded from Ask recall")
	}

	noSignals := planAskMemoryRecall("random unrelated query", []memoryRecallCandidate{{Source: "ltm", Label: "General", Summary: "unrelated knowledge", Category: "General", Recency: 0.1, Semantic: 0.1, TopicAffinity: 0.1, SourcePriority: 0.1, Continuity: 0.1, Outcome: 0.1}})
	if noSignals.Enabled {
		t.Fatal("expected unrelated candidates to be filtered out by the Ask recall gate")
	}
}

func TestBuildRecallPromotionCategoryPath_UsesSemanticBucketPattern(t *testing.T) {
	path := buildRecallPromotionCategoryPath("retry logic after payment errors")
	if !strings.Contains(path, "Coding") || !strings.Contains(path, "Semantic") {
		t.Fatalf("expected semantic promotion path to include Coding/Semantic, got %q", path)
	}
	if !strings.Contains(strings.ToLower(path), "retry") && !strings.Contains(strings.ToLower(path), "payment") {
		t.Fatalf("expected semantic path to reflect the active topic, got %q", path)
	}
}

func TestBuildMemoryRecallEval_ReportsPrecisionAndBudget(t *testing.T) {
	eval := buildMemoryRecallEval("retry logic in Payments/Retry/Recovery", []memoryRecallCandidate{
		{Source: "mru", Label: "Payments/Retry/Recovery", Summary: "first retry recovered", Category: "Payments/Retry/Recovery", Recency: 0.9, Semantic: 0.8, TopicAffinity: 0.7, SourcePriority: 0.8, Continuity: 0.9, Outcome: 0.8},
		{Source: "stm", Label: "Engineering/Payments", Summary: "retry worker issue", Category: "Engineering/Payments", Recency: 0.8, Semantic: 0.8, TopicAffinity: 0.7, SourcePriority: 0.7, Continuity: 0.8, Outcome: 0.7},
	}, 2, memoryRecallPolicy{ExactPathHits: 1, SemanticHits: 1, Expand: false})
	if eval.BudgetLimit != 2 {
		t.Fatalf("expected budget limit 2, got %d", eval.BudgetLimit)
	}
	if eval.TotalCandidates != 2 {
		t.Fatalf("expected 2 candidates, got %d", eval.TotalCandidates)
	}
	if eval.Precision <= 0 {
		t.Fatalf("expected positive precision score, got %f", eval.Precision)
	}
}

func TestBuildMemoryRecallQualityReport_TracksExactAndSemanticRates(t *testing.T) {
	report := buildMemoryRecallQualityReport("retry logic after payment errors", []memoryRecallCandidate{
		{Source: "ltm", Label: "General", Summary: "payment retry guidance", Category: "General", Recency: 0.6, Semantic: 0.4, TopicAffinity: 0.3, SourcePriority: 0.4, Continuity: 0.2, Outcome: 0.2},
		{Source: "stm", Label: "Engineering/Payments", Summary: "retry worker issue", Category: "Engineering/Payments", Recency: 0.8, Semantic: 0.8, TopicAffinity: 0.7, SourcePriority: 0.6, Continuity: 0.7, Outcome: 0.6},
	}, 2, memoryRecallPolicy{ExactPathHits: 0, SemanticHits: 1, Expand: true})
	if report.TotalCandidates != 2 {
		t.Fatalf("expected 2 candidates, got %d", report.TotalCandidates)
	}
	if report.Expand != true {
		t.Fatal("expected expansion to be reported")
	}
	if report.ExactHitRate != 0 {
		t.Fatalf("expected exact hit rate 0, got %f", report.ExactHitRate)
	}
	if report.SemanticHitRate <= 0 {
		t.Fatalf("expected semantic hit rate > 0, got %f", report.SemanticHitRate)
	}
}

func TestRunMemoryRecallQualityHarness_AggregatesExactSemanticAndExpansionRates(t *testing.T) {
	samples := buildMemoryRecallBenchmarkSamples()
	metrics := runMemoryRecallQualityHarness(samples)
	if metrics.TotalQueries != len(samples) {
		t.Fatalf("expected %d queries, got %d", len(samples), metrics.TotalQueries)
	}
	if metrics.ExpansionRate <= 0 {
		t.Fatalf("expected positive expansion rate, got %f", metrics.ExpansionRate)
	}
	if metrics.ExactHitRate <= 0 {
		t.Fatalf("expected positive exact-hit rate, got %f", metrics.ExactHitRate)
	}
	if metrics.SemanticHitRate <= 0 {
		t.Fatalf("expected positive semantic-hit rate, got %f", metrics.SemanticHitRate)
	}
	if metrics.FailRate <= 0 {
		t.Fatalf("expected stale-memory fail rate to be tracked, got %f", metrics.FailRate)
	}
}

func BenchmarkRunMemoryRecallQualityHarness(b *testing.B) {
	samples := buildMemoryRecallBenchmarkSamples()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = runMemoryRecallQualityHarness(samples)
	}
}

func BenchmarkPlanAskMemoryRecall(b *testing.B) {
	queries := []struct {
		query      string
		candidates []memoryRecallCandidate
	}{
		{
			query: "retry logic in Payments/Retry/Recovery",
			candidates: []memoryRecallCandidate{
				{Source: "mru", Label: "Payments/Retry/Recovery", Summary: "retry workflow", Category: "Payments/Retry/Recovery", Recency: 0.95, Semantic: 0.9, TopicAffinity: 0.9, SourcePriority: 0.85, Continuity: 0.9, Outcome: 0.9},
				{Source: "stm", Label: "Engineering/Payments", Summary: "retry worker issue", Category: "Engineering/Payments", Recency: 0.8, Semantic: 0.8, TopicAffinity: 0.7, SourcePriority: 0.7, Continuity: 0.8, Outcome: 0.7},
			},
		},
		{
			query: "retry logic after payment errors",
			candidates: []memoryRecallCandidate{
				{Source: "ltm", Label: "General", Summary: "payment retries are common", Category: "General", Recency: 0.6, Semantic: 0.4, TopicAffinity: 0.3, SourcePriority: 0.4, Continuity: 0.2, Outcome: 0.2},
				{Source: "stm", Label: "Engineering/Payments", Summary: "retry worker issue", Category: "Engineering/Payments", Recency: 0.8, Semantic: 0.8, TopicAffinity: 0.7, SourcePriority: 0.6, Continuity: 0.7, Outcome: 0.6},
			},
		},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, item := range queries {
			_ = planAskMemoryRecall(item.query, item.candidates)
		}
	}
}

func TestBuildVirtualMemoryRecallContext_IncludesRecallQualityMetrics(t *testing.T) {
	agent := &CopilotAgent{service: &Service{session: NewRunnerSession()}}
	agent.service.session.Memory = NewShortTermMemory()
	thread := &ConversationThread{ID: sop.NewUUID(), Category: "Engineering/Payments", Status: "open", Label: "Payments", Exchanges: []Interaction{{Role: RoleUser, Content: "retry logic after payment errors"}}}
	agent.service.session.Memory.AddThread(thread)
	agent.service.session.Memory.CurrentThreadID = thread.ID
	agent.service.session.Memory.AddRuntimeCaveat("Payments", "retry worker issue", "coding_workflow", "Engineering/Payments", "Payments")
	ctx := context.Background()
	summary := agent.buildVirtualMemoryRecallContext(ctx, "retry logic after payment errors")
	if !strings.Contains(summary, "drift:") || !strings.Contains(summary, "exact_hit_rate") || !strings.Contains(summary, "semantic_hit_rate") {
		t.Fatalf("expected drift summary plus exact_hit_rate and semantic_hit_rate metrics in memory neighborhood, got: %s", summary)
	}
}

func TestBuildContinueMemoryGuidance_UsesLTMRecallWhenSessionMemoryIsEmpty(t *testing.T) {
	ctx := context.Background()
	testDir := "./test_data/continue_memory_guidance"
	os.RemoveAll(testDir)
	defer os.RemoveAll(testDir)

	sysDBOptions := sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{testDir}}
	sysDB := database.NewDatabase(sysDBOptions)

	agent := NewCopilotAgent(Config{}, map[string]sop.DatabaseOptions{"sop": sysDBOptions}, sysDB)
	agent.service = &Service{
		domain:   &mockDomain{emb: &mockEmbeddings{}},
		session:  NewRunnerSession(),
		systemDB: sysDB,
	}
	agent.service.session.Memory = NewShortTermMemory()
	thread := &ConversationThread{
		ID:     sop.NewUUID(),
		Label:  "Continue memory",
		Status: "open",
		Exchanges: []Interaction{{
			Role:    RoleUser,
			Content: "Use small edits and validate before delivery",
		}},
	}
	agent.service.session.Memory.AddThread(thread)
	agent.service.session.Memory.CurrentThreadID = thread.ID
	agent.Memory = memory.NewMemoryUnit("agent123")
	agent.brain = &MockGenerator{}
	agent.service.generator = agent.brain

	tx, err := sysDB.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("begin write transaction failed: %v", err)
	}
	ltm, err := sysDB.OpenKnowledgeBase(ctx, agent.Memory.LongTermMemoryName(), tx, agent.brain, agent.service.domain.Embedder(), false)
	if err != nil {
		t.Fatalf("open ltm failed: %v", err)
	}
	if err := ltm.IngestThoughts(ctx, []memory.Thought[map[string]any]{
		{
			CategoryPath: "Team Patterns/Continue/Memory",
			Summaries:    []string{"Always keep workflow edits small and validate before delivery"},
			Data:         map[string]any{"source": "continue_memory_test"},
		},
	}, "system"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("ingest ltm thought failed: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit ltm thought failed: %v", err)
	}
	if err := sysDB.Vectorize(ctx, agent.Memory.LongTermMemoryName(), agent.brain, agent.service.domain.Embedder(), 50); err != nil {
		t.Fatalf("vectorize ltm failed: %v", err)
	}

	guidance, err := agent.BuildContinueMemoryGuidance(ctx, "Always keep workflow edits small and validate before delivery", "planning", map[string]any{"coding_task_type": "implementation"})
	if err != nil {
		t.Fatalf("BuildContinueMemoryGuidance failed: %v", err)
	}
	if len(guidance.ImplementationNotes) == 0 {
		t.Fatal("expected implementation notes from memory recall")
	}
	if len(guidance.ReferenceContext) == 0 {
		t.Fatal("expected reference context from memory recall")
	}
	foundRelevantReference := false
	for _, ref := range guidance.ReferenceContext {
		if ref.Kind == "memory_recall" || ref.Kind == "session_memory" {
			foundRelevantReference = true
			if strings.TrimSpace(ref.Content) == "" {
				t.Fatalf("expected non-empty reference content, got %+v", ref)
			}
		}
	}
	if !foundRelevantReference {
		t.Fatalf("expected a memory recall or session memory reference, got %+v", guidance.ReferenceContext)
	}
}

func TestPerformTopicRouting_WithNilSession_DoesNotPanic(t *testing.T) {
	svc := &Service{EnableHistoryInjection: true}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("performTopicRouting panicked with nil session: %v", r)
		}
	}()

	result := svc.performTopicRouting(context.Background(), "hello there")
	if result == nil {
		t.Fatal("performTopicRouting returned nil result")
	}
	if !result.isNewTopic {
		t.Fatal("expected topic routing to fall back to a new topic when session is unavailable")
	}
}

func TestIdentifyTopic_NilGeneratorFallsBackWithoutPanic(t *testing.T) {
	svc := &Service{session: NewRunnerSession()}
	svc.session.Memory = NewShortTermMemory()
	svc.session.Memory.AddThread(&ConversationThread{
		ID:     sop.NewUUID(),
		Label:  "Existing Topic",
		Status: "open",
		Exchanges: []Interaction{{
			Role:    RoleUser,
			Content: "previous question",
		}},
	})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("identifyTopic panicked with nil generator: %v", r)
		}
	}()

	assessment, err := svc.identifyTopic(context.Background(), "what about the other one?")
	if err != nil {
		t.Fatalf("identifyTopic returned error: %v", err)
	}
	if assessment == nil {
		t.Fatal("identifyTopic returned nil assessment")
	}
	if !assessment.IsNewTopic {
		t.Fatal("expected fallback classification to choose a new topic when no generator is available")
	}
}

func TestServiceToolExecutor_DoesNotInjectLegacyVerboseFallback(t *testing.T) {
	executor := &ServiceToolExecutor{}
	ctx := executor.injectToolContextToLegacyContext(context.Background(), &ToolExecutionContext{
		Session: &ai.SessionPayload{},
	})

	if v := ctx.Value("verbose"); v != nil {
		t.Fatalf("expected no legacy verbose fallback to be injected, got %#v", v)
	}
}

type eventAwareAgent struct{}

func (a *eventAwareAgent) Open(ctx context.Context) error  { return nil }
func (a *eventAwareAgent) Close(ctx context.Context) error { return nil }
func (a *eventAwareAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}
func (a *eventAwareAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	return "", nil
}
func (a *eventAwareAgent) Execute(ctx context.Context, toolName string, args map[string]any) (string, error) {
	if _, ok := ctx.Value(ai.CtxKeyEventStreamer).(func(string, any)); !ok {
		return "", fmt.Errorf("expected event streamer to be preserved in context")
	}
	return "ok", nil
}

func TestServiceToolExecutor_Execute_PreservesEventStreamer(t *testing.T) {

	eventStreamer := func(eventType string, payload any) {}
	ctx := context.WithValue(context.Background(), ai.CtxKeyEventStreamer, eventStreamer)
	ctx = context.WithValue(ctx, ai.CtxKeyExecutor, &MockExecutor{})

	svc := &Service{registry: map[string]ai.Agent[map[string]any]{"event_agent": &eventAwareAgent{}}}
	_, err := (&ServiceToolExecutor{s: svc}).Execute(ctx, "demo", map[string]any{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
}

func TestIsVerboseEnabled_UsesRunnerSessionPointer(t *testing.T) {
	ctx := context.WithValue(context.Background(), RunnerSessionKey, &RunnerSession{Verbose: true})

	if !isVerboseEnabled(ctx) {
		t.Fatal("expected verbose to resolve from the runner session pointer")
	}
}

type verboseRuntimeGenerator struct {
	calls int
}

func (m *verboseRuntimeGenerator) Name() string { return "verbose_runtime_mock" }

func (m *verboseRuntimeGenerator) EstimateCost(inTokens, outTokens int) float64 { return 0 }

func (m *verboseRuntimeGenerator) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
	return nil
}

func (m *verboseRuntimeGenerator) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	m.calls++
	if m.calls == 1 {
		return ai.GenOutput{ToolCalls: []ai.ToolCall{{Name: "echo_verbose", Args: map[string]any{}}}}, nil
	}
	return ai.GenOutput{Text: "Final answer: captured verbose state."}, nil
}

type verboseRuntimeExecutor struct {
	seen []bool
}

func (e *verboseRuntimeExecutor) Execute(ctx context.Context, tool string, args map[string]any) (string, error) {
	verbose := effectiveVerbose(ctx)
	e.seen = append(e.seen, verbose)
	if verbose {
		return "verbose_on", nil
	}
	return "verbose_off", nil
}

func (e *verboseRuntimeExecutor) ListTools(ctx context.Context) ([]ai.ToolDefinition, error) {
	return []ai.ToolDefinition{{Name: "echo_verbose"}}, nil
}

func TestServiceAsk_SeedsNewSessionPayloadFromPersistedVerbose(t *testing.T) {
	svc := NewService(&MockDomain{}, nil, nil, nil, nil, nil, false)
	svc.session.Verbose = true
	gen := &verboseRuntimeGenerator{}
	exec := &verboseRuntimeExecutor{}

	_, err := svc.ask(context.Background(), AskRequest{
		Query:     "check verbose runtime",
		Generator: gen,
		Executor:  exec,
	})
	if err != nil {
		t.Fatalf("ask failed: %v", err)
	}
	if !svc.session.IsVerbose() {
		t.Fatalf("expected ask-created session to inherit persisted verbose, got %#v", svc.session)
	}
	if len(exec.seen) != 1 || !exec.seen[0] {
		t.Fatalf("expected tool runtime to see persisted verbose=true, got %#v", exec.seen)
	}
}

func TestServiceAsk_ReusesRunnerSessionVerboseForNextRequest(t *testing.T) {
	svc := NewService(&MockDomain{}, nil, nil, nil, nil, nil, false)
	svc.session.SetVerbose(true)
	firstGen := &verboseRuntimeGenerator{}
	firstExec := &verboseRuntimeExecutor{}
	firstCfg := ai.NewConfigMap()
	firstCfg.Set("payload", &ai.SessionPayload{})
	firstCfg.Set("generator", firstGen)
	firstCfg.Set("executor", firstExec)

	if _, err := svc.Ask(context.Background(), "turn verbose on", firstCfg); err != nil {
		t.Fatalf("first Ask failed: %v", err)
	}
	if !svc.session.IsVerbose() {
		t.Fatal("expected service session to keep verbose=true for the next runtime step")
	}

	secondGen := &verboseRuntimeGenerator{}
	secondExec := &verboseRuntimeExecutor{}
	secondCfg := ai.NewConfigMap()
	secondCfg.Set("generator", secondGen)
	secondCfg.Set("executor", secondExec)

	if _, err := svc.Ask(context.Background(), "reuse verbose runtime", secondCfg); err != nil {
		t.Fatalf("second Ask failed: %v", err)
	}
	if len(secondExec.seen) != 1 || !secondExec.seen[0] {
		t.Fatalf("expected follow-up Ask to reuse persisted verbose=true, got %#v", secondExec.seen)
	}
}

func TestService_Ask_Obfuscation(t *testing.T) {
	// Setup Obfuscator
	obfuscation.GlobalObfuscator = obfuscation.NewMetadataObfuscator()
	obfuscation.GlobalObfuscator.RegisterResource("Python Complex DB", "DB")
	obfuscation.GlobalObfuscator.RegisterResource("My Store", "STORE")

	// Get the hashes
	dbHash := obfuscation.GlobalObfuscator.Obfuscate("Python Complex DB", "DB")
	storeHash := obfuscation.GlobalObfuscator.Obfuscate("My Store", "STORE")

	tests := []struct {
		name           string
		llmResponse    string
		expectedResult string // For text response
		expectedDB     string // For tool call
		expectedStore  string // For tool call
		isTool         bool
	}{
		{
			name:           "Normal Text Response",
			llmResponse:    "I found " + dbHash + " and " + storeHash,
			expectedResult: "I found Python Complex DB and My Store",
			isTool:         false,
		},
		{
			name:           "Markdown Bold Response",
			llmResponse:    "I found **" + dbHash + "** and _" + storeHash + "_",
			expectedResult: "I found **Python Complex DB** and _My Store_",
			isTool:         false,
		},
		{
			name:          "Tool Call Clean",
			llmResponse:   `{"tool": "select", "args": {"database": "` + dbHash + `", "store": "` + storeHash + `"}}`,
			expectedDB:    "Python Complex DB",
			expectedStore: "My Store",
			isTool:        true,
		},
		{
			name:          "Tool Call with Markdown Artifacts",
			llmResponse:   `{"tool": "select", "args": {"database": "**` + dbHash + `**", "store": "_` + storeHash + `_"}}`,
			expectedDB:    "Python Complex DB",
			expectedStore: "My Store",
			isTool:        true,
		},
		{
			name:          "Tool Call with NBSP",
			llmResponse:   `{"tool": "select", "args": {"database": "` + dbHash + `\u00a0", "store": "` + storeHash + `"}}`,
			expectedDB:    "Python Complex DB",
			expectedStore: "My Store",
			isTool:        true,
		},
		{
			name:          "Tool Call with Real Name (Leak) + Artifacts",
			llmResponse:   `{"tool": "select", "args": {"database": "**Python Complex DB**", "store": "My Store"}}`,
			expectedDB:    "Python Complex DB",
			expectedStore: "My Store",
			isTool:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen := &MockGenerator{Response: tt.llmResponse}
			// Enable obfuscation for tests
			svc := NewService(&MockDomain{}, nil, nil, gen, nil, nil, true)
			executor := &MockExecutor{}

			ctx := context.WithValue(context.Background(), ai.CtxKeyExecutor, executor)

			result, err := svc.Ask(ctx, "query", nil)
			if err != nil {
				t.Fatalf("Ask failed: %v", err)
			}

			if tt.isTool {
				// For tool calls, Ask returns the JSON string.
				// We need to parse it to verify the args were cleaned.
				// OR, if we move execution into Ask, we verify the executor was called.
				// Currently Ask returns the JSON string.

				// But wait, the current implementation in Service.Ask just does string replacement on the JSON.
				// It does NOT parse the JSON to clean specific fields.
				// So if the LLM returns `**DB_1**`, string replacement turns it into `**Python Complex DB**`.
				// It does NOT remove the `**`.
				// The User wants Copilot (Service) to handle this "woe".
				// So we expect the JSON returned by Ask to contain CLEAN names, without artifacts.

				// Let's see what we get currently.
				t.Logf("Result: %s", result)

				// If we want to verify the "robustness", we should check if the result contains the CLEAN name
				// AND if it's free of artifacts if we implement that logic.

				// Check captured args
				dbArg, ok := executor.LastArgs["database"].(string)
				if !ok {
					// Some tests might not have database arg, check if we expect it
					if tt.expectedDB != "" {
						t.Errorf("Expected 'database' arg to be string, got %T", executor.LastArgs["database"])
					}
				} else {
					if dbArg != tt.expectedDB {
						t.Errorf("Expected database arg '%s', got '%s'", tt.expectedDB, dbArg)
					}
					if strings.Contains(dbArg, "**") || strings.Contains(dbArg, "`") {
						t.Errorf("Database arg still contains artifacts: %s", dbArg)
					}
				}
			} else {
				if result != tt.expectedResult {
					t.Errorf("Expected '%s', got '%s'", tt.expectedResult, result)
				}
			}
		})
	}
}

func TestService_Ask_NoObfuscation(t *testing.T) {
	// Setup
	mockGen := &MockGenerator{Response: "Some response with DB_123"}
	mockDomain := &MockDomain{}

	// Create Service with Obfuscation DISABLED
	svc := NewService(mockDomain, nil, nil, mockGen, nil, nil, false)

	ctx := context.Background()

	// Execute
	result, err := svc.Ask(ctx, "query", nil)
	if err != nil {
		t.Fatalf("Ask failed: %v", err)
	}

	// Verify
	// Since obfuscation is disabled, the output should be exactly what the generator returned
	if result != "Some response with DB_123" {
		t.Errorf("Expected raw response 'Some response with DB_123', got '%s'", result)
	}
}

// SplitCentroid mocks the base method.
func (m *MockVectorStore) SplitCentroid(ctx context.Context, centroidID int) error {
	return nil
}

func (m *recordingVectorStore) SplitCentroid(ctx context.Context, centroidID int) error {
	return nil
}

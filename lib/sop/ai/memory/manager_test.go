package memory

import (
	"context"
	"fmt"
	"strings"

	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/inmemory"
)

type MockEmbedder struct{}

func (m *MockEmbedder) Name() string { return "MockEmbedder" }
func (m *MockEmbedder) Dim() int     { return 3 }
func (m *MockEmbedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	vecs := make([][]float32, len(texts))
	for i, text := range texts {
		lower := fmt.Sprintf("%s", text)
		if lower == "subcat1" {
			vecs[i] = []float32{0.1, 0.2, 0.3} // Close to test vector
		} else if lower == "fruits" {
			vecs[i] = []float32{0.2, 0.3, 0.4} // Distinct vector for Fruits category
		} else if lower == "vehicles" {
			vecs[i] = []float32{0.8, 0.9, 1.0} // Distinct vector for Vehicles category
		} else {
			vecs[i] = []float32{0.5, 0.6, 0.7} // Default mismatch
		}
	}
	return vecs, nil
}

type MockLLM struct{}

func (m *MockLLM) Name() string                                 { return "MockLLM" }
func (m *MockLLM) EstimateCost(inTokens, outTokens int) float64 { return 0.0 }
func (m *MockLLM) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
	return nil
}
func (m *MockLLM) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	// Let's pretend it always categorizes into "MockCategory"
	return ai.GenOutput{Text: "MockCategory"}, nil
}

type FailingLLM struct{}

func (m *FailingLLM) Name() string                                 { return "FailingLLM" }
func (m *FailingLLM) EstimateCost(inTokens, outTokens int) float64 { return 0.0 }
func (m *FailingLLM) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
	return nil
}
func (m *FailingLLM) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	return ai.GenOutput{}, fmt.Errorf("mock llm failure")
}

type testLLM struct{ category string }

func (m *testLLM) Name() string                                 { return "test-llm" }
func (m *testLLM) EstimateCost(inTokens, outTokens int) float64 { return 0.0 }
func (m *testLLM) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
	return nil
}
func (m *testLLM) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	return ai.GenOutput{Text: m.category}, nil
}

type FailingEmbedder struct{}

func (m *FailingEmbedder) Name() string { return "FailingEmbedder" }
func (m *FailingEmbedder) Dim() int     { return 3 }
func (m *FailingEmbedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, fmt.Errorf("mock embedder failure")
}

type staticCategorizer struct {
	calls []CategorizeOptions
}

func (c *staticCategorizer) Name() string { return "static" }
func (c *staticCategorizer) Categorize(text string, opts CategorizeOptions) (string, error) {
	c.calls = append(c.calls, opts)
	return "Static/Category", nil
}
func (c *staticCategorizer) CategorizeMany(texts []string, opts CategorizeOptions) ([]string, error) {
	out := make([]string, len(texts))
	for i := range texts {
		out[i] = "Static/Category"
	}
	return out, nil
}

type kbAwareCategorizer struct {
	seenKB any
}

func (c *kbAwareCategorizer) Name() string            { return "kb-aware" }
func (c *kbAwareCategorizer) SetKnowledgeBase(kb any) { c.seenKB = kb }
func (c *kbAwareCategorizer) Categorize(text string, opts CategorizeOptions) (string, error) {
	if c.seenKB == nil {
		return "", fmt.Errorf("knowledge base not injected")
	}
	if _, ok := c.seenKB.(*KnowledgeBase[string]); !ok {
		return "", fmt.Errorf("unexpected knowledge base type %T", c.seenKB)
	}
	return "kb-aware-category", nil
}
func (c *kbAwareCategorizer) CategorizeMany(texts []string, opts CategorizeOptions) ([]string, error) {
	out := make([]string, len(texts))
	for i := range texts {
		out[i] = "kb-aware-category"
	}
	return out, nil
}

func TestRuleBasedCategorizer_UsesNestedTaxonomyAndConfidence(t *testing.T) {
	cat := NewRuleBasedCategorizer([]RuleCategory{
		{Path: "engineering/software/deployment", Keywords: []string{"deploy", "deployment", "rollback"}},
		{Path: "engineering/software/debugging", Keywords: []string{"panic", "stack trace", "debug"}},
	}, "uncategorized")

	res, err := cat.CategorizeWithConfidence("The deployment rolled back after a panic in the worker", CategorizeOptions{Mode: CategorizerModeStorage})
	if err != nil {
		t.Fatalf("CategorizeWithConfidence failed: %v", err)
	}
	if res.Category != "engineering/software/deployment" {
		t.Fatalf("expected deployment category, got %q", res.Category)
	}
	if res.Confidence < 0.5 {
		t.Fatalf("expected confidence above 0.5, got %f", res.Confidence)
	}
}

func TestRuleBasedCategorizer_FallsBackToParentCategoryWhenConfidenceIsLow(t *testing.T) {
	cat := NewRuleBasedCategorizer([]RuleCategory{{Path: "Platform/Operations/Databases/Postgres", Keywords: []string{"postgres", "replication", "failover", "backup"}}}, "uncategorized")

	res, err := cat.CategorizeWithConfidence("postgres", CategorizeOptions{Mode: CategorizerModeStorage})
	if err != nil {
		t.Fatalf("CategorizeWithConfidence failed: %v", err)
	}
	if res.Category != "Platform/Operations/Databases" {
		t.Fatalf("expected parent category fallback, got %q", res.Category)
	}
	if res.Confidence >= 0.7 {
		t.Fatalf("expected reduced confidence for parent fallback, got %f", res.Confidence)
	}
}

func TestHybridCategorizer_FallsBackToLLMWhenRuleConfidenceIsLow(t *testing.T) {
	ruleCat := NewRuleBasedCategorizer([]RuleCategory{{Path: "Platform/Operations/Databases/Postgres", Keywords: []string{"postgres", "replication", "failover", "backup"}}}, "uncategorized")
	hybrid := NewHybridCategorizer(ruleCat, NewLLMCategorizer(&testLLM{category: "Platform/Operations/Monitoring"}))

	res, err := hybrid.CategorizeWithConfidence("postgres", CategorizeOptions{Mode: CategorizerModeRetrieval})
	if err != nil {
		t.Fatalf("CategorizeWithConfidence failed: %v", err)
	}
	if res.Category != "Platform/Operations/Monitoring" {
		t.Fatalf("expected llm fallback category, got %q", res.Category)
	}
}

func TestRetrievalCategoryRouter_RanksMostRecentPathsFirst(t *testing.T) {
	router := NewRetrievalCategoryRouter(4)
	router.NotePath("Platform/Operations/Databases/Postgres")
	router.NotePath("Platform/Operations/Monitoring")
	router.NotePath("Platform/Operations/Databases/Postgres")

	ranked := router.RankedPaths([]string{"Platform/Operations/Security", "Platform/Operations/Databases/Postgres", "Platform/Operations/Monitoring"})
	if len(ranked) != 3 {
		t.Fatalf("expected 3 ranked paths, got %d", len(ranked))
	}
	if ranked[0] != "Platform/Operations/Databases/Postgres" {
		t.Fatalf("expected most recent path first, got %v", ranked)
	}
}

func TestRuleBasedCategorizer_UsesNestedPathScoringAndFuzzyMatches(t *testing.T) {
	cat := NewRuleBasedCategorizer([]RuleCategory{{Path: "Platform/Operations/Databases/Postgres", Keywords: []string{"postgres", "replication", "failover"}}}, "uncategorized")
	res, err := cat.CategorizeWithConfidence("postgres db failover", CategorizeOptions{Mode: CategorizerModeRetrieval})
	if err != nil {
		t.Fatalf("CategorizeWithConfidence failed: %v", err)
	}
	if res.Category != "Platform/Operations/Databases/Postgres" {
		t.Fatalf("expected nested category path, got %q", res.Category)
	}
	if res.Confidence < 0.55 {
		t.Fatalf("expected confidence above threshold, got %f", res.Confidence)
	}
}

func TestRuleBasedCategorizer_CategorizeCandidatesReturnsMultiplePaths(t *testing.T) {
	cat := NewRuleBasedCategorizer([]RuleCategory{
		{Path: "Platform/Operations/Databases/Postgres", Keywords: []string{"postgres", "failover"}},
		{Path: "Platform/Operations/Monitoring", Keywords: []string{"monitoring", "alerting"}},
		{Path: "Platform/Operations/Security", Keywords: []string{"security", "auth", "token"}},
	}, "uncategorized")

	candidates, err := cat.CategorizeCandidates("postgres failover monitoring alerting", CategorizeOptions{Mode: CategorizerModeRetrieval})
	if err != nil {
		t.Fatalf("CategorizeCandidates failed: %v", err)
	}
	if len(candidates) < 2 {
		t.Fatalf("expected at least 2 candidates, got %d", len(candidates))
	}
	paths := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		paths = append(paths, candidate.Category)
	}
	if !containsString(paths, "Platform/Operations/Databases/Postgres") || !containsString(paths, "Platform/Operations/Monitoring") {
		t.Fatalf("expected postgres and monitoring candidates, got %v", paths)
	}
	if candidates[0].Confidence <= 0 || candidates[1].Confidence <= 0 {
		t.Fatalf("expected positive confidence values, got %v", candidates)
	}
	if strings.TrimSpace(candidates[0].Category) == "" {
		t.Fatalf("expected non-empty first candidate, got %#v", candidates[0])
	}
}

func TestRuleBasedCategorizer_PreservesNonLatinTokensForLocalization(t *testing.T) {
	cat := NewRuleBasedCategorizer([]RuleCategory{{Path: "安全/认证", Keywords: []string{"认证", "登录"}}}, "uncategorized")

	res, err := cat.CategorizeWithConfidence("登录认证流程", CategorizeOptions{Mode: CategorizerModeRetrieval})
	if err != nil {
		t.Fatalf("CategorizeWithConfidence failed: %v", err)
	}
	if res.Category != "安全/认证" {
		t.Fatalf("expected localized category to be preserved, got %q", res.Category)
	}
}

func TestMemoryManager_UsesConfidenceAwareCategorizer(t *testing.T) {
	mgr := NewMemoryManager[string](nil, &MockLLM{}, &MockEmbedder{})
	mgr.SetCategorizer(&staticCategorizer{})
	res, err := mgr.CategorizeWithConfidence("hello", CategorizeOptions{Mode: CategorizerModeStorage})
	if err != nil {
		t.Fatalf("CategorizeWithConfidence failed: %v", err)
	}
	if res.Category != "Static/Category" {
		t.Fatalf("expected categorized output from injected categorizer, got %q", res.Category)
	}
}

type domainSpecificCategorizer struct{}

func (c *domainSpecificCategorizer) Name() string { return "domain-specific" }
func (c *domainSpecificCategorizer) Categorize(text string, opts CategorizeOptions) (string, error) {
	return "ops/incident", nil
}
func (c *domainSpecificCategorizer) CategorizeMany(texts []string, opts CategorizeOptions) ([]string, error) {
	out := make([]string, len(texts))
	for i := range texts {
		out[i] = "ops/incident"
	}
	return out, nil
}

func TestCategorizerRegistry_UsesDomainSpecificCategorizer(t *testing.T) {
	defaultCat := &staticCategorizer{}
	registry := NewCategorizerRegistry(defaultCat)
	registry.Register("ops", &domainSpecificCategorizer{})

	mgr := NewMemoryManager[string](nil, &MockLLM{}, &MockEmbedder{})
	mgr.SetCategorizerRegistry(registry)

	cat, err := mgr.Categorize("server is down", CategorizeOptions{Mode: CategorizerModeRetrieval, Domain: "ops"})
	if err != nil {
		t.Fatalf("Categorize(ops) failed: %v", err)
	}
	if cat != "ops/incident" {
		t.Fatalf("expected ops-specific category, got %q", cat)
	}

	fallbackCat, err := mgr.Categorize("server is down", CategorizeOptions{Mode: CategorizerModeRetrieval, Domain: "finance"})
	if err != nil {
		t.Fatalf("Categorize(finance) failed: %v", err)
	}
	if fallbackCat != "Static/Category" {
		t.Fatalf("expected fallback to default categorizer, got %q", fallbackCat)
	}
}

func TestMemoryManager_UsesInjectedCategorizerForStorageAndRetrieval(t *testing.T) {
	cat := &staticCategorizer{}
	mgr := NewMemoryManager[string](nil, &MockLLM{}, &MockEmbedder{})
	mgr.SetCategorizer(cat)

	storageCategory, err := mgr.Categorize("storage item", CategorizeOptions{Mode: CategorizerModeStorage})
	if err != nil {
		t.Fatalf("Categorize(storage) failed: %v", err)
	}
	if storageCategory != "Static/Category" {
		t.Fatalf("expected storage category to come from categorizer, got %q", storageCategory)
	}

	retrievalCategory, err := mgr.Categorize("retrieval query", CategorizeOptions{Mode: CategorizerModeRetrieval})
	if err != nil {
		t.Fatalf("Categorize(retrieval) failed: %v", err)
	}
	if retrievalCategory != "Static/Category" {
		t.Fatalf("expected retrieval category to come from categorizer, got %q", retrievalCategory)
	}
	if len(cat.calls) != 2 {
		t.Fatalf("expected 2 categorizer calls, got %d", len(cat.calls))
	}
	if cat.calls[0].Mode != CategorizerModeStorage || cat.calls[1].Mode != CategorizerModeRetrieval {
		t.Fatalf("expected mode to be preserved in categorizer calls, got %#v", cat.calls)
	}
}

func TestMemoryManager_InjectsKnowledgeBaseIntoCategorizer(t *testing.T) {
	mgr := NewMemoryManager[string](nil, &MockLLM{}, &MockEmbedder{})
	cat := &kbAwareCategorizer{}
	mgr.SetCategorizer(cat)

	kb := &KnowledgeBase[string]{Manager: mgr}
	mgr.SetKnowledgeBase(kb)

	category, err := mgr.Categorize("hello", CategorizeOptions{Mode: CategorizerModeStorage})
	if err != nil {
		t.Fatalf("Categorize failed: %v", err)
	}
	if category != "kb-aware-category" {
		t.Fatalf("expected category from KB-aware categorizer, got %q", category)
	}
}

func TestMemoryManager_PassesTaxonomyContextToLLMHelper(t *testing.T) {
	mgr := NewMemoryManager[string](nil, &testLLM{category: "Platform/Operations/Monitoring"}, &MockEmbedder{})
	ctx := NewStaticTaxonomyContext([]string{"Platform/Operations/Databases/Postgres", "Platform/Operations/Monitoring"})
	category, err := mgr.Categorize("postgres failover", CategorizeOptions{Mode: CategorizerModeStorage, TaxonomyContext: ctx})
	if err != nil {
		t.Fatalf("Categorize failed: %v", err)
	}
	if category != "Platform/Operations/Monitoring" {
		t.Fatalf("expected taxonomy-aware llm category, got %q", category)
	}
}

func TestStaticTaxonomyContext_ExposesItemCounts(t *testing.T) {
	ctx := NewStaticTaxonomyContextWithCounts(
		[]string{"Platform/Operations/Monitoring", "Platform/Operations/Databases/Postgres"},
		map[string]int{
			"Platform/Operations/Monitoring":         42,
			"Platform/Operations/Databases/Postgres": 120,
		},
	)

	if got := ctx.ItemCount("Platform/Operations/Monitoring"); got != 42 {
		t.Fatalf("expected monitoring item count to be 42, got %d", got)
	}
	if got := ctx.ItemCount("Platform/Operations/Databases/Postgres"); got != 120 {
		t.Fatalf("expected postgres item count to be 120, got %d", got)
	}
	if got := ctx.ItemCount("Platform/Operations/Unknown"); got != 0 {
		t.Fatalf("expected unknown item count to be 0, got %d", got)
	}
}

func TestMemoryManager_IngestAndSleep(t *testing.T) {
	ctx := context.Background()

	catTree := inmemory.NewBtree[sop.UUID, *Category](true)
	vecTree := inmemory.NewBtree[VectorKey, Vector](true)
	itemTree := inmemory.NewBtree[ItemKey, Item[string]](true)

	// Pull out the core Btree interface
	store := NewStore[string]("test_kb", nil, catTree.Btree, inmemory.NewBtree[string, sop.UUID](false).Btree, inmemory.NewBtree[DistanceKey, byte](false).Btree, vecTree.Btree, itemTree.Btree, inmemory.NewBtree[sop.UUID, Document](false).Btree)
	mgr := NewMemoryManager[string](store, &MockLLM{}, &MockEmbedder{})

	// 1. Test IngestThought
	kbMgr := &KnowledgeBase[string]{Manager: mgr, Store: store}
	err := kbMgr.IngestThoughts(ctx, []Thought[string]{{Summaries: []string{"this is a brilliant thought about apples"}, CategoryPath: "", Data: "Apples are great"}}, "Nutritionist")
	if err != nil {
		t.Fatalf("IngestThought failed: %v", err)
	}

	cats, err := store.Categories(ctx)
	ok, err := cats.First(ctx)
	if !ok || err != nil {
		t.Fatalf("No categories were auto-generated by Ingest")
	}

	c, _ := cats.GetCurrentValue(ctx)
	if c.Name != "MockCategory" {
		t.Errorf("Expected auto-generated category 'MockCategory', got '%s'", c.Name)
	}

	// 2. Test SleepCycle
	mgr.sleepThreshold = 0
	err = mgr.SleepCycle(ctx)
	if err != nil {
		t.Fatalf("SleepCycle failed: %v", err)
	}
}

// Test edge cases to hit 100% coverage

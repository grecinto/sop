//go:build quality

package memory_test

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	aidb "github.com/sharedcode/sop/ai/database"
	memory "github.com/sharedcode/sop/ai/memory"
)

// qualityKeywordEmbedder is intentionally deterministic so the quality scenarios
// are reproducible and do not depend on networked embedding services.
type qualityKeywordEmbedder struct {
	rules map[string][]float32
	dim   int
}

func newQualityKeywordEmbedder() *qualityKeywordEmbedder {
	return &qualityKeywordEmbedder{
		dim: 6,
		rules: map[string][]float32{
			"platform":    {0.95, 0.10, 0.05, 0.05, 0.05, 0.05},
			"operations":  {0.05, 0.95, 0.10, 0.05, 0.05, 0.05},
			"database":    {0.05, 0.10, 0.95, 0.05, 0.05, 0.05},
			"postgres":    {0.05, 0.10, 0.70, 0.95, 0.05, 0.05},
			"redis":       {0.05, 0.10, 0.70, 0.05, 0.95, 0.05},
			"monitoring":  {0.05, 0.10, 0.75, 0.05, 0.05, 0.95},
			"replication": {0.10, 0.15, 0.80, 0.90, 0.10, 0.10},
			"failover":    {0.10, 0.15, 0.80, 0.95, 0.10, 0.10},
			"cache":       {0.10, 0.15, 0.75, 0.10, 0.90, 0.10},
			"outage":      {0.10, 0.20, 0.70, 0.10, 0.90, 0.20},
			"auth":        {0.10, 0.20, 0.70, 0.05, 0.05, 0.90},
			"security":    {0.10, 0.20, 0.70, 0.05, 0.05, 0.95},
			"token":       {0.10, 0.20, 0.70, 0.05, 0.05, 0.90},
			"rotation":    {0.10, 0.20, 0.70, 0.05, 0.05, 0.90},
			"backup":      {0.05, 0.10, 0.80, 0.90, 0.10, 0.10},
			"restore":     {0.05, 0.10, 0.80, 0.90, 0.10, 0.10},
		},
	}
}

func (e *qualityKeywordEmbedder) Name() string { return "quality-keyword" }
func (e *qualityKeywordEmbedder) Dim() int     { return e.dim }

func (e *qualityKeywordEmbedder) EmbedTexts(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = e.embedOne(text)
	}
	return out, nil
}

func (e *qualityKeywordEmbedder) EmbedCategoryTexts(ctx context.Context, texts []string) ([][]float32, error) {
	return e.EmbedTexts(ctx, texts)
}

func (e *qualityKeywordEmbedder) EmbedDocumentTexts(ctx context.Context, texts []string) ([][]float32, error) {
	return e.EmbedTexts(ctx, texts)
}

func (e *qualityKeywordEmbedder) EmbedQueryTexts(ctx context.Context, texts []string) ([][]float32, error) {
	return e.EmbedTexts(ctx, texts)
}

func (e *qualityKeywordEmbedder) embedOne(text string) []float32 {
	lower := strings.ToLower(text)
	vec := make([]float32, e.dim)
	for kw, v := range e.rules {
		if strings.Contains(lower, kw) {
			for i := 0; i < e.dim; i++ {
				vec[i] += v[i]
			}
		}
	}
	if sumSq := float32(0); sumSq > 0 {
		for i := 0; i < e.dim; i++ {
			sumSq += vec[i] * vec[i]
		}
		if sumSq > 0 {
			norm := float32(1.0 / float64(sumSq))
			for i := 0; i < e.dim; i++ {
				vec[i] *= norm
			}
		}
	}
	return vec
}

var _ ai.Embeddings = (*qualityKeywordEmbedder)(nil)
var _ ai.EmbeddingModeSupport = (*qualityKeywordEmbedder)(nil)

func openStandaloneQualityKB(t *testing.T, embedder ai.Embeddings) (*memory.KnowledgeBase[map[string]any], *aidb.Database, sop.Transaction, func()) {
	t.Helper()

	ctx := context.Background()
	dbDir := t.TempDir()
	db := aidb.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{dbDir}})

	writeTx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("BeginTransaction(write) failed: %v", err)
	}
	kb, err := db.OpenKnowledgeBase(ctx, "quality-kb", writeTx, nil, embedder, false, false)
	if err != nil {
		_ = writeTx.Rollback(ctx)
		t.Fatalf("OpenKnowledgeBase failed: %v", err)
	}

	cleanup := func() {
		_ = writeTx.Rollback(ctx)
	}
	return kb, db, writeTx, cleanup
}

func seedQualityCategories(t *testing.T, ctx context.Context, kb *memory.KnowledgeBase[map[string]any], embedder ai.Embeddings) {
	t.Helper()

	categories := []struct {
		path   string
		name   string
		parent string
		vector []float32
	}{
		{path: "Platform", name: "Platform", vector: embedder.(*qualityKeywordEmbedder).embedOne("platform")},
		{path: "Platform/Operations", name: "Operations", parent: "Platform", vector: embedder.(*qualityKeywordEmbedder).embedOne("operations")},
		{path: "Platform/Operations/Databases", name: "Databases", parent: "Platform/Operations", vector: embedder.(*qualityKeywordEmbedder).embedOne("database")},
		{path: "Platform/Operations/Databases/Postgres", name: "Postgres", parent: "Platform/Operations/Databases", vector: embedder.(*qualityKeywordEmbedder).embedOne("postgres")},
		{path: "Platform/Operations/Databases/Redis", name: "Redis", parent: "Platform/Operations/Databases", vector: embedder.(*qualityKeywordEmbedder).embedOne("redis")},
		{path: "Platform/Operations/Monitoring", name: "Monitoring", parent: "Platform/Operations", vector: embedder.(*qualityKeywordEmbedder).embedOne("monitoring")},
		{path: "Platform/Operations/Security", name: "Security", parent: "Platform/Operations", vector: embedder.(*qualityKeywordEmbedder).embedOne("security")},
	}

	var ids map[string]sop.UUID = make(map[string]sop.UUID)
	for _, c := range categories {
		id := sop.NewUUID()
		ids[c.path] = id
		params := []memory.UpsertCategoryParam{{
			Category: &memory.Category{
				ID:           id,
				Name:         c.name,
				Path:         c.path,
				CenterVector: c.vector,
			},
		}}
		if c.parent != "" {
			if parentID, ok := ids[c.parent]; ok {
				params[0].ParentIDs = []sop.UUID{parentID}
			}
		}
		if err := kb.UpsertCategories(ctx, params); err != nil {
			t.Fatalf("UpsertCategories(%s) failed: %v", c.path, err)
		}
	}
}

func seedQualityItems(t *testing.T, ctx context.Context, kb *memory.KnowledgeBase[map[string]any], embedder ai.Embeddings) {
	t.Helper()

	items := []struct {
		categoryPath string
		text         string
		summary      string
	}{
		{categoryPath: "Platform/Operations/Databases/Postgres", text: "postgres replication failover guide", summary: "postgres replication"},
		{categoryPath: "Platform/Operations/Databases/Postgres", text: "postgres backup restore checklist", summary: "postgres backup"},
		{categoryPath: "Platform/Operations/Databases/Postgres", text: "postgres connection pooling tuning", summary: "postgres connection"},
		{categoryPath: "Platform/Operations/Databases/Redis", text: "redis cache eviction policy", summary: "redis cache"},
		{categoryPath: "Platform/Operations/Databases/Redis", text: "redis outage mitigation playbook", summary: "redis outage"},
		{categoryPath: "Platform/Operations/Databases/Redis", text: "redis cluster memory troubleshooting", summary: "redis memory"},
		{categoryPath: "Platform/Operations/Monitoring", text: "monitoring alerting dashboard recipe", summary: "monitoring alerting"},
		{categoryPath: "Platform/Operations/Monitoring", text: "service health signal thresholds", summary: "monitoring thresholds"},
		{categoryPath: "Platform/Operations/Security", text: "auth token rotation policy", summary: "auth token"},
		{categoryPath: "Platform/Operations/Security", text: "security incident response checklist", summary: "security incident"},
	}

	for _, item := range items {
		vecs, err := embedder.(*qualityKeywordEmbedder).EmbedTexts(ctx, []string{item.text})
		if err != nil {
			t.Fatalf("EmbedTexts failed: %v", err)
		}
		payload := map[string]any{"text": item.text}
		if err := kb.Store.UpsertByCategoryPath(ctx, item.categoryPath, memory.Item[map[string]any]{ID: sop.NewUUID(), Data: payload, Summaries: []string{item.summary}}, vecs); err != nil {
			t.Fatalf("UpsertByCategoryPath(%s) failed: %v", item.categoryPath, err)
		}
	}
}

func traverseQualityCategories(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], queryVec []float32) (*memory.Category, error) {
	cats, err := kb.Store.Categories(ctx)
	if err != nil {
		return nil, err
	}

	var roots []*memory.Category
	ok, err := cats.First(ctx)
	if err != nil {
		return nil, err
	}
	for ok {
		cat, err := cats.GetCurrentValue(ctx)
		if err != nil {
			return nil, err
		}
		if cat != nil && len(cat.ParentIDs) == 0 {
			roots = append(roots, cat)
		}
		ok, err = cats.Next(ctx)
		if err != nil {
			return nil, err
		}
	}

	if len(roots) == 0 {
		return nil, nil
	}

	current := roots[0]
	currentDist := float32(1e9)
	for _, root := range roots {
		if len(root.CenterVector) == 0 {
			continue
		}
		d := memory.Distance(queryVec, root.CenterVector, true)
		if current == nil || d < currentDist {
			current = root
			currentDist = d
		}
	}

	for {
		var bestChild *memory.Category
		bestChildDist := float32(1e9)
		for _, childID := range current.ChildrenIDs {
			found, err := cats.Find(ctx, childID, false)
			if err != nil || !found {
				continue
			}
			child, err := cats.GetCurrentValue(ctx)
			if err != nil || child == nil || len(child.CenterVector) == 0 {
				continue
			}
			d := memory.Distance(queryVec, child.CenterVector, true)
			if d < bestChildDist {
				bestChild = child
				bestChildDist = d
			}
		}
		if bestChild == nil || bestChildDist >= currentDist {
			break
		}
		current = bestChild
		currentDist = bestChildDist
	}

	return current, nil
}

func payloadText(payload any) string {
	if m, ok := payload.(map[string]any); ok {
		if text, ok := m["text"].(string); ok {
			return text
		}
	}
	return fmt.Sprintf("%v", payload)
}

type qualityReportEntry struct {
	Name              string
	Query             string
	CategoryPath      string
	StorageCategories int
	StorageItems      int
	RelevantCount     int
	PrecisionAt1      float64
	PrecisionAt3      float64
	RecallAt3         float64
	MRR               float64
	NDCGAt3           float64
	SuccessAt3        bool
	TopHits           []string
}

func (r qualityReportEntry) String() string {
	return fmt.Sprintf("scenario=%s query=%q category_path=%q storage_categories=%d storage_items=%d relevant_count=%d precision@1=%.2f precision@3=%.2f recall@3=%.2f mrr=%.2f ndcg@3=%.2f success@3=%t top_hits=%q", r.Name, r.Query, r.CategoryPath, r.StorageCategories, r.StorageItems, r.RelevantCount, r.PrecisionAt1, r.PrecisionAt3, r.RecallAt3, r.MRR, r.NDCGAt3, r.SuccessAt3, strings.Join(r.TopHits, " | "))
}

func containsAny(text string, terms []string) bool {
	lower := strings.ToLower(text)
	for _, term := range terms {
		if strings.Contains(lower, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

type qualityBenchmarkScenario struct {
	Name          string
	Query         string
	CategoryPath  string
	ExpectedTerms []string
	RelevantCount int
	Limit         int
}

func buildQualityBenchmarkScenarios() []qualityBenchmarkScenario {
	return []qualityBenchmarkScenario{
		{Name: "known_nested_category", Query: "postgres failover", CategoryPath: "Platform/Operations/Databases/Postgres", ExpectedTerms: []string{"postgres", "failover"}, RelevantCount: 3, Limit: 3},
		{Name: "text_only_leaf_inference", Query: "postgres failover", ExpectedTerms: []string{"postgres", "failover"}, RelevantCount: 3, Limit: 3},
		{Name: "fuzzy_cross_leaf_retrieval", Query: "redis cache outage", ExpectedTerms: []string{"redis", "cache", "outage"}, RelevantCount: 3, Limit: 3},
		{Name: "monitoring_path_retrieval", Query: "monitoring dashboard alerting", CategoryPath: "Platform/Operations/Monitoring", ExpectedTerms: []string{"monitoring", "alerting", "dashboard"}, RelevantCount: 2, Limit: 3},
		{Name: "security_synonym_retrieval", Query: "auth token rotation", CategoryPath: "Platform/Operations/Security", ExpectedTerms: []string{"auth", "token", "rotation"}, RelevantCount: 2, Limit: 3},
		{Name: "backup_restore_cross_leaf", Query: "database backup restore", CategoryPath: "Platform/Operations/Databases/Postgres", ExpectedTerms: []string{"backup", "restore"}, RelevantCount: 1, Limit: 3},
	}
}

func runQualityBenchmark(t *testing.T, kb *memory.KnowledgeBase[map[string]any], scenarios []qualityBenchmarkScenario, storedCategories int, storedItems int) []qualityReportEntry {
	t.Helper()

	reports := make([]qualityReportEntry, 0, len(scenarios))
	for _, scenario := range scenarios {
		query := memory.SearchRequest[map[string]any]{CategoryPath: scenario.CategoryPath, Text: scenario.Query, Limit: scenario.Limit}
		reports = append(reports, evaluateQualityScenario(t, kb, query, scenario.ExpectedTerms, scenario.RelevantCount, scenario.Name, scenario.CategoryPath, storedCategories, storedItems))
	}
	return reports
}

func evaluateQualityScenario(t *testing.T, kb *memory.KnowledgeBase[map[string]any], query memory.SearchRequest[map[string]any], expectedTerms []string, relevantCount int, name string, categoryPath string, storedCategories int, storedItems int) qualityReportEntry {
	t.Helper()

	results, err := kb.Search(context.Background(), []memory.SearchRequest[map[string]any]{query})
	if err != nil {
		t.Fatalf("Search(%s) failed: %v", name, err)
	}
	if len(results) == 0 || len(results[0]) == 0 {
		t.Fatalf("Search(%s) returned no hits", name)
	}

	hits := results[0]
	topHits := make([]string, 0, len(hits))
	matchingTop3 := 0
	firstRelevantRank := 0
	for i, hit := range hits {
		text := payloadText(hit.Payload)
		topHits = append(topHits, text)
		if containsAny(text, expectedTerms) {
			if firstRelevantRank == 0 {
				firstRelevantRank = i + 1
			}
			if i < 3 {
				matchingTop3++
			}
		}
	}
	if len(topHits) > 3 {
		topHits = topHits[:3]
	}
	precisionAt1 := 0.0
	if firstRelevantRank == 1 {
		precisionAt1 = 1.0
	}
	precisionAt3 := 0.0
	if len(hits) > 0 {
		precisionAt3 = float64(matchingTop3) / float64(min(3, len(hits)))
	}
	recallAt3 := 0.0
	if relevantCount > 0 {
		recallAt3 = float64(matchingTop3) / float64(min(3, relevantCount))
	}
	mrr := 0.0
	if firstRelevantRank > 0 {
		mrr = 1.0 / float64(firstRelevantRank)
	}

	dcg := 0.0
	idealDCG := 0.0
	for i, hit := range hits {
		relevance := 0.0
		if containsAny(payloadText(hit.Payload), expectedTerms) {
			relevance = 1.0
		}
		if i < 3 {
			dcg += relevance / math.Pow(2, float64(i))
		}
	}
	idealCount := min(3, relevantCount)
	for i := 0; i < idealCount; i++ {
		idealDCG += 1.0 / math.Pow(2, float64(i))
	}
	ndcgAt3 := 0.0
	if idealDCG > 0 {
		ndcgAt3 = dcg / idealDCG
	}

	return qualityReportEntry{
		Name:              name,
		Query:             query.Text,
		CategoryPath:      categoryPath,
		StorageCategories: storedCategories,
		StorageItems:      storedItems,
		RelevantCount:     relevantCount,
		PrecisionAt1:      precisionAt1,
		PrecisionAt3:      precisionAt3,
		RecallAt3:         recallAt3,
		MRR:               mrr,
		NDCGAt3:           ndcgAt3,
		SuccessAt3:        firstRelevantRank > 0 && firstRelevantRank <= 3,
		TopHits:           topHits,
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestKnowledgeBaseQuality_Standalone_RealWorldReport(t *testing.T) {
	ctx := context.Background()
	embedder := newQualityKeywordEmbedder()
	kb, db, writeTx, cleanup := openStandaloneQualityKB(t, embedder)
	defer cleanup()

	seedQualityCategories(t, ctx, kb, embedder)
	seedQualityItems(t, ctx, kb, embedder)
	// Note: Vectorization is handled by seed functions

	categories, categoryCount, err := kb.ListCategories(ctx, memory.ListCategoriesParam{Limit: 1000})
	if err != nil {
		t.Fatalf("ListCategories failed: %v", err)
	}
	items, itemCount, err := kb.ListItems(ctx, memory.ListItemsParam{Limit: 1000})
	if err != nil {
		t.Fatalf("ListItems failed: %v", err)
	}
	if categoryCount < 7 {
		t.Fatalf("expected at least 7 categories after seeding, got %d (%v)", categoryCount, categories)
	}
	if itemCount < 10 {
		t.Fatalf("expected at least 10 items after seeding, got %d (%v)", itemCount, items)
	}

	if err := writeTx.Commit(ctx); err != nil {
		t.Fatalf("Commit(write) failed: %v", err)
	}

	readTx, err := db.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		t.Fatalf("BeginTransaction(read) failed: %v", err)
	}
	defer readTx.Rollback(ctx)

	reloadedKB, err := db.OpenKnowledgeBase(ctx, "quality-kb", readTx, nil, embedder, false, false)
	if err != nil {
		t.Fatalf("OpenKnowledgeBase(reload) failed: %v", err)
	}

	reports := runQualityBenchmark(t, reloadedKB, buildQualityBenchmarkScenarios(), categoryCount, itemCount)

	var builder strings.Builder
	builder.WriteString("quality report\n")
	builder.WriteString(fmt.Sprintf("storage_categories=%d storage_items=%d\n", categoryCount, itemCount))
	for _, report := range reports {
		builder.WriteString("- ")
		builder.WriteString(report.String())
		builder.WriteString("\n")
	}
	meanPrecisionAt1 := 0.0
	meanPrecisionAt3 := 0.0
	meanRecallAt3 := 0.0
	meanMRR := 0.0
	meanNDCGAt3 := 0.0
	for _, report := range reports {
		meanPrecisionAt1 += report.PrecisionAt1
		meanPrecisionAt3 += report.PrecisionAt3
		meanRecallAt3 += report.RecallAt3
		meanMRR += report.MRR
		meanNDCGAt3 += report.NDCGAt3
	}
	meanPrecisionAt1 /= float64(len(reports))
	meanPrecisionAt3 /= float64(len(reports))
	meanRecallAt3 /= float64(len(reports))
	meanMRR /= float64(len(reports))
	meanNDCGAt3 /= float64(len(reports))
	if meanPrecisionAt1 < 0.5 {
		t.Fatalf("expected mean precision@1 >= 0.5 across scenarios, got %.2f", meanPrecisionAt1)
	}
	builder.WriteString(fmt.Sprintf("aggregate_precision@1=%.2f aggregate_precision@3=%.2f aggregate_recall@3=%.2f aggregate_mrr=%.2f aggregate_ndcg@3=%.2f\n", meanPrecisionAt1, meanPrecisionAt3, meanRecallAt3, meanMRR, meanNDCGAt3))
	t.Log(builder.String())
}

type categorizerBenchmarkCase struct {
	Name        string
	Categorizer memory.Categorizer
}

type benchmarkLLM struct{}

func (m *benchmarkLLM) Name() string                                               { return "benchmark-llm" }
func (m *benchmarkLLM) EstimateCost(inTokens, outTokens int) float64               { return 0 }
func (m *benchmarkLLM) PrewarmCache(ctx context.Context, opts ai.GenOptions) error { return nil }
func (m *benchmarkLLM) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	lower := strings.ToLower(prompt)
	switch {
	case strings.Contains(lower, "postgres"):
		return ai.GenOutput{Text: "Platform/Operations/Databases/Postgres"}, nil
	case strings.Contains(lower, "redis"):
		return ai.GenOutput{Text: "Platform/Operations/Databases/Redis"}, nil
	case strings.Contains(lower, "monitor"):
		return ai.GenOutput{Text: "Platform/Operations/Monitoring"}, nil
	case strings.Contains(lower, "auth") || strings.Contains(lower, "token") || strings.Contains(lower, "security"):
		return ai.GenOutput{Text: "Platform/Operations/Security"}, nil
	default:
		return ai.GenOutput{Text: "uncategorized"}, nil
	}
}

func TestKnowledgeBaseQuality_Standalone_CategorizerStrategyComparison(t *testing.T) {
	reports := benchmarkCategorizerStrategies(t, newQualityKeywordEmbedder())
	if len(reports) == 0 {
		t.Fatalf("expected categorizer benchmark to produce reports")
	}
	var foundRuleBased, foundLLM bool
	for _, report := range reports {
		if strings.HasPrefix(report.Name, "rule-based/") {
			foundRuleBased = true
		}
		if strings.HasPrefix(report.Name, "llm-backed/") {
			foundLLM = true
		}
	}
	if !foundRuleBased || !foundLLM {
		t.Fatalf("expected reports for both strategy prefixes, got %#v", reports)
	}

	strategySummaries := summarizeCategorizerStrategyReports(reports)
	var builder strings.Builder
	builder.WriteString("categorizer strategy comparison\n")
	for _, summary := range strategySummaries {
		builder.WriteString(fmt.Sprintf("- %s precision@1=%.2f precision@3=%.2f recall@3=%.2f mrr=%.2f ndcg@3=%.2f success_rate=%.2f\n", summary.Name, summary.PrecisionAt1, summary.PrecisionAt3, summary.RecallAt3, summary.MRR, summary.NDCGAt3, summary.SuccessRate))
	}
	t.Log(builder.String())
}

func summarizeCategorizerStrategyReports(reports []qualityReportEntry) []categorizerStrategySummary {
	strategyMap := map[string][]qualityReportEntry{}
	for _, report := range reports {
		parts := strings.SplitN(report.Name, "/", 2)
		if len(parts) != 2 {
			continue
		}
		strategyMap[parts[0]] = append(strategyMap[parts[0]], report)
	}

	summaries := make([]categorizerStrategySummary, 0, len(strategyMap))
	for strategy, strategyReports := range strategyMap {
		precisionAt1 := 0.0
		precisionAt3 := 0.0
		recallAt3 := 0.0
		mrr := 0.0
		ndcgAt3 := 0.0
		successCount := 0
		for _, report := range strategyReports {
			precisionAt1 += report.PrecisionAt1
			precisionAt3 += report.PrecisionAt3
			recallAt3 += report.RecallAt3
			mrr += report.MRR
			ndcgAt3 += report.NDCGAt3
			if report.SuccessAt3 {
				successCount++
			}
		}
		count := float64(len(strategyReports))
		summaries = append(summaries, categorizerStrategySummary{
			Name:         strategy,
			PrecisionAt1: precisionAt1 / count,
			PrecisionAt3: precisionAt3 / count,
			RecallAt3:    recallAt3 / count,
			MRR:          mrr / count,
			NDCGAt3:      ndcgAt3 / count,
			SuccessRate:  float64(successCount) / count,
		})
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Name < summaries[j].Name })
	return summaries
}

type categorizerStrategySummary struct {
	Name         string
	PrecisionAt1 float64
	PrecisionAt3 float64
	RecallAt3    float64
	MRR          float64
	NDCGAt3      float64
	SuccessRate  float64
}

func benchmarkCategorizerStrategies(t *testing.T, embedder ai.Embeddings) []qualityReportEntry {
	t.Helper()

	ctx := context.Background()
	var reports []qualityReportEntry
	cases := []categorizerBenchmarkCase{
		{
			Name: "rule-based",
			Categorizer: memory.NewRuleBasedCategorizer([]memory.RuleCategory{
				{Path: "Platform/Operations/Databases/Postgres", Keywords: []string{"postgres", "replication", "failover", "backup", "restore"}},
				{Path: "Platform/Operations/Databases/Redis", Keywords: []string{"redis", "cache", "outage", "cluster"}},
				{Path: "Platform/Operations/Monitoring", Keywords: []string{"monitoring", "alerting", "dashboard"}},
				{Path: "Platform/Operations/Security", Keywords: []string{"auth", "token", "security", "rotation"}},
			}, "uncategorized"),
		},
		{
			Name:        "llm-backed",
			Categorizer: memory.NewLLMCategorizer(&benchmarkLLM{}),
		},
	}

	for _, tc := range cases {
		kb, db, writeTx, cleanup := openStandaloneQualityKB(t, embedder)
		defer cleanup()

		mgr := memory.NewMemoryManager[map[string]any](kb.Store, nil, embedder)
		mgr.SetCategorizer(tc.Categorizer)
		kb.Manager = mgr

		seedQualityCategories(t, ctx, kb, embedder)
		seedQualityItems(t, ctx, kb, embedder)
	// Note: Vectorization is handled by seed functions
		}

		readTx, err := db.BeginTransaction(ctx, sop.ForReading)
		if err != nil {
			t.Fatalf("BeginTransaction(read,%s) failed: %v", tc.Name, err)
		}
		defer readTx.Rollback(ctx)

		reloadedKB, err := db.OpenKnowledgeBase(ctx, "quality-kb", readTx, nil, embedder, false, false)
		if err != nil {
			t.Fatalf("OpenKnowledgeBase(%s) failed: %v", tc.Name, err)
		}
		strategyReports := runQualityBenchmark(t, reloadedKB, buildQualityBenchmarkScenarios(), 7, 10)
		for i := range strategyReports {
			strategyReports[i].Name = fmt.Sprintf("%s/%s", tc.Name, strategyReports[i].Name)
		}
		reports = append(reports, strategyReports...)
	}
	return reports
}

func TestKnowledgeBaseQuality_Standalone_KnownNestedCategorySearch(t *testing.T) {
	ctx := context.Background()
	embedder := newQualityKeywordEmbedder()
	kb, db, writeTx, cleanup := openStandaloneQualityKB(t, embedder)
	defer cleanup()

	seedQualityCategories(t, ctx, kb, embedder)
	seedQualityItems(t, ctx, kb, embedder)
	// Note: Vectorization is handled by seed functions

	if err := writeTx.Commit(ctx); err != nil {
		t.Fatalf("Commit(write) failed: %v", err)
	}

	readTx, err := db.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		t.Fatalf("BeginTransaction(read) failed: %v", err)
	}
	defer readTx.Rollback(ctx)

	reloadedKB, err := db.OpenKnowledgeBase(ctx, "quality-kb", readTx, nil, embedder, false, false)
	if err != nil {
		t.Fatalf("OpenKnowledgeBase(reload) failed: %v", err)
	}

	results, err := reloadedKB.Search(ctx, []memory.SearchRequest[map[string]any]{{
		CategoryPath: "Platform/Operations/Databases/Postgres",
		Text:         "postgres failover",
		Limit:        5,
	}})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || len(results[0]) == 0 {
		t.Fatalf("expected postgres hits, got %#v", results)
	}
	for _, hit := range results[0] {
		text := strings.ToLower(payloadText(hit.Payload))
		if !strings.Contains(text, "postgres") {
			t.Fatalf("expected postgres-specific hit, got %q", payloadText(hit.Payload))
		}
	}
}

func TestKnowledgeBaseQuality_Standalone_HierarchicalTraversalFindsLeafCategory(t *testing.T) {
	ctx := context.Background()
	embedder := newQualityKeywordEmbedder()
	kb, db, writeTx, cleanup := openStandaloneQualityKB(t, embedder)
	defer cleanup()

	seedQualityCategories(t, ctx, kb, embedder)
	seedQualityItems(t, ctx, kb, embedder)
	// Note: Vectorization is handled by seed functions

	if err := writeTx.Commit(ctx); err != nil {
		t.Fatalf("Commit(write) failed: %v", err)
	}

	readTx, err := db.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		t.Fatalf("BeginTransaction(read) failed: %v", err)
	}
	defer readTx.Rollback(ctx)

	reloadedKB, err := db.OpenKnowledgeBase(ctx, "quality-kb", readTx, nil, embedder, false, false)
	if err != nil {
		t.Fatalf("OpenKnowledgeBase(reload) failed: %v", err)
	}

	queryVec := embedder.embedOne("postgres replication failover guide")
	leafCat, err := traverseQualityCategories(ctx, reloadedKB, queryVec)
	if err != nil {
		t.Fatalf("traverseQualityCategories failed: %v", err)
	}
	if leafCat == nil || leafCat.Path != "Platform/Operations/Databases/Postgres" {
		t.Fatalf("expected traversal to resolve Postgres leaf, got %#v", leafCat)
	}

	hits, err := reloadedKB.Store.QueryItems(ctx, queryVec, leafCat, &memory.SearchOptions[map[string]any]{Limit: 5})
	if err != nil {
		t.Fatalf("QueryItems failed: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("expected items from the resolved leaf category, got %#v", hits)
	}
	for _, hit := range hits {
		text := strings.ToLower(payloadText(hit.Payload))
		if !strings.Contains(text, "postgres") {
			t.Fatalf("expected leaf-category hits to stay in postgres, got %q", payloadText(hit.Payload))
		}
	}
}

func TestKnowledgeBaseQuality_Standalone_HighVolumeNestedTraversalStress(t *testing.T) {
	ctx := context.Background()
	embedder := newQualityKeywordEmbedder()
	kb, db, writeTx, cleanup := openStandaloneQualityKB(t, embedder)
	defer cleanup()

	seedQualityCategories(t, ctx, kb, embedder)
	for i := 0; i < 200; i++ {
		categoryPath := "Platform/Operations/Databases/Postgres"
		if i%2 == 0 {
			categoryPath = "Platform/Operations/Databases/Redis"
		}
		text := fmt.Sprintf("%s workload item %d", categoryPath, i)
		vecs, err := embedder.EmbedTexts(ctx, []string{text})
		if err != nil {
			t.Fatalf("EmbedTexts failed: %v", err)
		}
		payload := map[string]any{"text": text}
		if err := kb.Store.UpsertByCategoryPath(ctx, categoryPath, memory.Item[map[string]any]{ID: sop.NewUUID(), Data: payload, Summaries: []string{strings.ToLower(categoryPath)}}, vecs); err != nil {
			t.Fatalf("UpsertByCategoryPath(%s) failed: %v", categoryPath, err)
		}
	}
	// Note: Vectorization is handled during item addition

	if err := writeTx.Commit(ctx); err != nil {
		t.Fatalf("Commit(write) failed: %v", err)
	}

	readTx, err := db.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		t.Fatalf("BeginTransaction(read) failed: %v", err)
	}
	defer readTx.Rollback(ctx)

	reloadedKB, err := db.OpenKnowledgeBase(ctx, "quality-kb", readTx, nil, embedder, false, false)
	if err != nil {
		t.Fatalf("OpenKnowledgeBase(reload) failed: %v", err)
	}

	queryVec := embedder.embedOne("postgres failover")
	leafCat, err := traverseQualityCategories(ctx, reloadedKB, queryVec)
	if err != nil {
		t.Fatalf("traverseQualityCategories failed: %v", err)
	}
	if leafCat == nil || leafCat.Path != "Platform/Operations/Databases/Postgres" {
		t.Fatalf("expected stress traversal to resolve Postgres leaf, got %#v", leafCat)
	}

	hits, err := reloadedKB.Store.QueryItems(ctx, queryVec, leafCat, &memory.SearchOptions[map[string]any]{Limit: 20})
	if err != nil {
		t.Fatalf("QueryItems failed: %v", err)
	}
	if len(hits) < 10 {
		t.Fatalf("expected at least 10 postgres hits under stress load, got %d", len(hits))
	}
}

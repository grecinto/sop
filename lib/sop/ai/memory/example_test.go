package memory

import (
	"context"
	"fmt"

	"github.com/sharedcode/sop/ai"
)

type exampleGenerator struct{}

func (g *exampleGenerator) Name() string                                 { return "example-generator" }
func (g *exampleGenerator) EstimateCost(inTokens, outTokens int) float64 { return 0 }
func (g *exampleGenerator) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
	return nil
}
func (g *exampleGenerator) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	return ai.GenOutput{Text: "Platform/Operations/Monitoring"}, nil
}

func ExampleRuleBasedCategorizer() {
	cat := NewRuleBasedCategorizer([]RuleCategory{
		{Path: "Platform/Operations/Databases/Postgres", Keywords: []string{"postgres", "replication", "failover"}},
		{Path: "Platform/Operations/Databases/Redis", Keywords: []string{"redis", "cache", "outage"}},
	}, "uncategorized")

	res, _ := cat.CategorizeWithConfidence("postgres failover guide", CategorizeOptions{Mode: CategorizerModeRetrieval})
	fmt.Println(res.Category)
	// Output: Platform/Operations/Databases/Postgres
}

func ExampleMemoryManager_ConfigureFromKnowledgeBaseConfig() {
	cfg := &KnowledgeBaseConfig{
		CategorizerConfig: &CategorizerConfig{
			Type: "hybrid",
			Default: map[string]any{
				"categories": []any{
					map[string]any{"path": "Platform/Operations/Databases/Postgres", "keywords": []any{"postgres", "replication", "failover"}},
				},
			},
		},
	}

	mgr := NewMemoryManager[string](nil, &exampleGenerator{}, nil)
	_ = mgr.ConfigureFromKnowledgeBaseConfig(cfg)

	res, _ := mgr.CategorizeWithConfidence("postgres failover", CategorizeOptions{Mode: CategorizerModeStorage})
	fmt.Println(res.Category)
	// Output: Platform/Operations/Databases/Postgres
}

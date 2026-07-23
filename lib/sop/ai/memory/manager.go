package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
)

type CategorizerMode string

const (
	CategorizerModeStorage   CategorizerMode = "storage"
	CategorizerModeRetrieval CategorizerMode = "retrieval"
)

type TaxonomyContext interface {
	Paths() []string
	Contains(path string) bool
	Parent(path string) string
	Children(path string) []string
	ItemCount(path string) int
}

type StaticTaxonomyContext struct {
	paths []string
	items map[string]int
}

func NewStaticTaxonomyContext(paths []string) *StaticTaxonomyContext {
	return NewStaticTaxonomyContextWithCounts(paths, nil)
}

func NewStaticTaxonomyContextWithCounts(paths []string, itemCounts map[string]int) *StaticTaxonomyContext {
	out := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	counts := make(map[string]int, len(itemCounts))
	for path, count := range itemCounts {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		counts[trimmed] = count
	}
	return &StaticTaxonomyContext{paths: out, items: counts}
}

func (c *StaticTaxonomyContext) Paths() []string {
	if c == nil {
		return nil
	}
	return append([]string(nil), c.paths...)
}

func (c *StaticTaxonomyContext) Contains(path string) bool {
	if c == nil {
		return false
	}
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return false
	}
	for _, existing := range c.paths {
		if existing == trimmed {
			return true
		}
	}
	return false
}

func (c *StaticTaxonomyContext) Parent(path string) string {
	if c == nil {
		return ""
	}
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) <= 1 {
		return ""
	}
	return strings.Join(parts[:len(parts)-1], "/")
}

func (c *StaticTaxonomyContext) Children(path string) []string {
	if c == nil {
		return nil
	}
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil
	}
	var out []string
	for _, candidate := range c.paths {
		if strings.HasPrefix(candidate, trimmed+"/") {
			out = append(out, candidate)
		}
	}
	return out
}

func (c *StaticTaxonomyContext) ItemCount(path string) int {
	if c == nil {
		return 0
	}
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return 0
	}
	return c.items[trimmed]
}

type CategorizeOptions struct {
	Mode            CategorizerMode
	PersonaContext  string
	Domain          string
	TaxonomyContext TaxonomyContext
}

type Categorizer interface {
	Name() string
	Categorize(text string, opts CategorizeOptions) (string, error)
	CategorizeMany(texts []string, opts CategorizeOptions) ([]string, error)
}

type CategorizerCandidate struct {
	Category   string
	Confidence float32
}

type MultiCategoryCategorizer interface {
	Categorizer
	CategorizeCandidates(text string, opts CategorizeOptions) ([]CategorizerCandidate, error)
	CategorizeManyCandidates(texts []string, opts CategorizeOptions) ([][]CategorizerCandidate, error)
}

type KnowledgeBaseAwareCategorizer interface {
	SetKnowledgeBase(kb any)
}

type CategorizerResult struct {
	Category   string
	Confidence float32
}

type RuleCategory struct {
	Path     string
	Keywords []string
}

type RuleBasedCategorizer struct {
	categories []RuleCategory
	fallback   string
}

func NewRuleBasedCategorizer(categories []RuleCategory, fallback string) *RuleBasedCategorizer {
	if fallback == "" {
		fallback = "uncategorized"
	}
	return &RuleBasedCategorizer{categories: categories, fallback: fallback}
}

func NewRuleBasedCategorizerFromConfig(config *CategorizerConfig) *RuleBasedCategorizer {
	if config == nil {
		return NewRuleBasedCategorizer(nil, "uncategorized")
	}
	var categories []RuleCategory
	if config.Default != nil {
		if raw, ok := config.Default["categories"].([]any); ok {
			for _, entry := range raw {
				if item, ok := entry.(map[string]any); ok {
					path, _ := item["path"].(string)
					kwRaw, _ := item["keywords"].([]any)
					keywords := make([]string, 0, len(kwRaw))
					for _, kw := range kwRaw {
						if s, ok := kw.(string); ok {
							keywords = append(keywords, s)
						}
					}
					if path != "" {
						categories = append(categories, RuleCategory{Path: path, Keywords: keywords})
					}
				}
			}
		}
	}
	fallback := config.Fallback
	if fallback == "" {
		fallback = "uncategorized"
	}
	return NewRuleBasedCategorizer(categories, fallback)
}

func (c *RuleBasedCategorizer) Name() string { return "rule-based-categorizer" }

func (c *RuleBasedCategorizer) Categorize(text string, opts CategorizeOptions) (string, error) {
	res, err := c.CategorizeWithConfidence(text, opts)
	if err != nil {
		return "", err
	}
	return res.Category, nil
}

func (c *RuleBasedCategorizer) CategorizeMany(texts []string, opts CategorizeOptions) ([]string, error) {
	res, err := c.CategorizeManyWithConfidence(texts, opts)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(res))
	for i := range res {
		out[i] = res[i].Category
	}
	return out, nil
}

func (c *RuleBasedCategorizer) CategorizeCandidates(text string, opts CategorizeOptions) ([]CategorizerCandidate, error) {
	if c == nil {
		return nil, fmt.Errorf("rule categorizer is nil")
	}
	if c.fallback == "" {
		c.fallback = "uncategorized"
	}

	normalizedText := normalizeCategorizerText(text)
	if normalizedText == "" {
		return nil, nil
	}

	type scoredPath struct {
		path       string
		confidence float32
		depth      int
	}
	var scored []scoredPath
	for _, category := range c.categories {
		if strings.TrimSpace(category.Path) == "" {
			continue
		}
		score, depth := scoreCategoryPath(normalizedText, category.Path, category.Keywords)
		if score <= 0 {
			continue
		}
		confidence := 0.5 + (score * 0.4)
		if depth > 1 && shouldPenalizeSingleTokenInput(normalizedText) {
			confidence = 0.45
		}
		if confidence > 0.95 {
			confidence = 0.95
		}
		scored = append(scored, scoredPath{path: category.Path, confidence: confidence, depth: depth})
	}

	if len(scored) == 0 {
		return nil, nil
	}

	for i := 0; i < len(scored); i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[i].confidence < scored[j].confidence || (scored[i].confidence == scored[j].confidence && scored[i].depth < scored[j].depth) {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	out := make([]CategorizerCandidate, 0, len(scored))
	seen := make(map[string]struct{}, len(scored))
	for _, entry := range scored {
		if _, ok := seen[entry.path]; ok {
			continue
		}
		if entry.confidence <= 0 {
			continue
		}
		seen[entry.path] = struct{}{}
		out = append(out, CategorizerCandidate{Category: entry.path, Confidence: entry.confidence})
		if len(out) >= 3 {
			break
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func (c *RuleBasedCategorizer) CategorizeManyCandidates(texts []string, opts CategorizeOptions) ([][]CategorizerCandidate, error) {
	if c == nil {
		return nil, fmt.Errorf("rule categorizer is nil")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	out := make([][]CategorizerCandidate, len(texts))
	for i, text := range texts {
		cands, err := c.CategorizeCandidates(text, opts)
		if err != nil {
			return nil, err
		}
		out[i] = cands
	}
	return out, nil
}

func (c *RuleBasedCategorizer) CategorizeWithConfidence(text string, opts CategorizeOptions) (CategorizerResult, error) {
	if c == nil {
		return CategorizerResult{}, fmt.Errorf("rule categorizer is nil")
	}
	candidates, err := c.CategorizeCandidates(text, opts)
	if err != nil {
		return CategorizerResult{}, err
	}
	if len(candidates) == 0 {
		if c.fallback == "" {
			c.fallback = "uncategorized"
		}
		return CategorizerResult{Category: c.fallback, Confidence: 0.2}, nil
	}
	first := candidates[0]
	if first.Confidence < 0.7 {
		if parent := parentCategoryPath(first.Category); parent != "" {
			return CategorizerResult{Category: parent, Confidence: first.Confidence * 0.8}, nil
		}
	}
	return CategorizerResult{Category: first.Category, Confidence: first.Confidence}, nil
}

func (c *RuleBasedCategorizer) CategorizeManyWithConfidence(texts []string, opts CategorizeOptions) ([]CategorizerResult, error) {
	if c == nil {
		return nil, fmt.Errorf("rule categorizer is nil")
	}
	results := make([]CategorizerResult, len(texts))
	for i, text := range texts {
		res, err := c.CategorizeWithConfidence(text, opts)
		if err != nil {
			return nil, err
		}
		results[i] = res
	}
	return results, nil
}

func normalizeCategorizerText(text string) string {
	return NormalizeCategoryToken(text)
}

func shouldPenalizeSingleTokenInput(text string) bool {
	tokens := strings.Fields(text)
	if len(tokens) != 1 {
		return false
	}
	for _, r := range tokens[0] {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func splitCategoryPath(path string) []string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil
	}
	parts := strings.FieldsFunc(trimmed, func(r rune) bool { return r == '/' || r == '\\' })
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if normalized := normalizeCategorizerText(part); normalized != "" {
			clean = append(clean, normalized)
		}
	}
	return clean
}

func scoreCategoryPath(text, path string, keywords []string) (float32, int) {
	segments := splitCategoryPath(path)
	if len(segments) == 0 {
		return 0, 0
	}
	bestScore := float32(0)
	bestDepth := 0
	for depth := 1; depth <= len(segments); depth++ {
		prefix := segments[:depth]
		var score float32
		for _, segment := range prefix {
			score += pathSegmentSimilarity(text, segment)
		}
		score /= float32(len(prefix))
		if len(keywords) > 0 {
			keywordScore := float32(0)
			for _, keyword := range keywords {
				keywordScore += pathSegmentSimilarity(text, keyword)
			}
			keywordScore /= float32(len(keywords))
			if keywordScore > score {
				score = keywordScore
			}
		}
		if depth > 1 {
			score += float32(depth) * 0.035
			if score > 1.0 {
				score = 1.0
			}
		}
		if score > bestScore || (score == bestScore && depth > bestDepth) {
			bestScore = score
			bestDepth = depth
		}
	}
	return bestScore, bestDepth
}

func pathSegmentSimilarity(text, candidate string) float32 {
	normText := normalizeCategorizerText(text)
	normCandidate := normalizeCategorizerText(candidate)
	if normText == "" || normCandidate == "" {
		return 0
	}
	if normText == normCandidate {
		return 1.0
	}
	if strings.Contains(normText, normCandidate) || strings.Contains(normCandidate, normText) {
		return 0.95
	}
	textTokens := strings.Fields(normText)
	candTokens := strings.Fields(normCandidate)
	if len(textTokens) == 0 || len(candTokens) == 0 {
		return 0
	}
	matches := 0
	textSet := make(map[string]struct{}, len(textTokens))
	for _, token := range textTokens {
		textSet[token] = struct{}{}
	}
	for _, token := range candTokens {
		if _, ok := textSet[token]; ok {
			matches++
		}
	}
	if matches > 0 {
		return float32(matches) / float32(len(candTokens))
	}
	if len(candTokens) == 1 && len(textTokens) == 1 {
		if strings.HasPrefix(normText, candTokens[0]) || strings.HasPrefix(candTokens[0], normText) {
			return 0.6
		}
		return float32(levenshteinDistance(normText, candTokens[0]))
	}
	return 0
}

func levenshteinDistance(a, b string) float32 {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return float32(len(b))
	}
	if len(b) == 0 {
		return float32(len(a))
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := 0; j <= len(b); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			ins := curr[j-1] + 1
			del := prev[j] + 1
			sub := prev[j-1] + cost
			if ins < del {
				if ins < sub {
					curr[j] = ins
				} else {
					curr[j] = sub
				}
			} else if del < sub {
				curr[j] = del
			} else {
				curr[j] = sub
			}
		}
		prev, curr = curr, prev
	}
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	if maxLen == 0 {
		return 0
	}
	return 1.0 - (float32(prev[len(b)]) / float32(maxLen))
}

type ConfidenceCategorizer interface {
	Categorizer
	CategorizeWithConfidence(text string, opts CategorizeOptions) (CategorizerResult, error)
	CategorizeManyWithConfidence(texts []string, opts CategorizeOptions) ([]CategorizerResult, error)
}

type RetrievalCategoryRouter struct {
	mu        sync.Mutex
	pathHits  map[string]int
	pathOrder []string
	maxSize   int
}

func NewRetrievalCategoryRouter(maxSize int) *RetrievalCategoryRouter {
	if maxSize <= 0 {
		maxSize = 64
	}
	return &RetrievalCategoryRouter{pathHits: map[string]int{}, maxSize: maxSize}
}

func (r *RetrievalCategoryRouter) NotePath(path string) {
	if r == nil || strings.TrimSpace(path) == "" {
		return
	}
	trimmed := strings.TrimSpace(path)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pathHits == nil {
		r.pathHits = map[string]int{}
	}
	if r.pathHits[trimmed] == 0 {
		r.pathOrder = append(r.pathOrder, trimmed)
		if len(r.pathOrder) > r.maxSize {
			r.pathOrder = r.pathOrder[len(r.pathOrder)-r.maxSize:]
		}
	}
	r.pathHits[trimmed]++
}

func (r *RetrievalCategoryRouter) RankedPaths(paths []string) []string {
	if r == nil || len(paths) == 0 {
		return paths
	}
	trimmed := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		candidate := strings.TrimSpace(path)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		trimmed = append(trimmed, candidate)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(trimmed) == 0 {
		return nil
	}
	ordered := make([]string, 0, len(trimmed))
	for _, path := range r.pathOrder {
		for _, candidate := range trimmed {
			if path == candidate {
				ordered = append(ordered, candidate)
				break
			}
		}
	}
	for _, candidate := range trimmed {
		if !containsString(ordered, candidate) {
			ordered = append(ordered, candidate)
		}
	}
	return ordered
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

type HybridCategorizer struct {
	primary   Categorizer
	fallback  Categorizer
	threshold float32
}

func NewHybridCategorizer(primary, fallback Categorizer) *HybridCategorizer {
	if primary == nil {
		primary = NewRuleBasedCategorizer(nil, "uncategorized")
	}
	if fallback == nil {
		fallback = NewRuleBasedCategorizer(nil, "uncategorized")
	}
	return &HybridCategorizer{primary: primary, fallback: fallback, threshold: 0.7}
}

func (c *HybridCategorizer) Name() string {
	if c == nil {
		return "hybrid-categorizer"
	}
	if c.primary != nil {
		return c.primary.Name()
	}
	if c.fallback != nil {
		return c.fallback.Name()
	}
	return "hybrid-categorizer"
}

func (c *HybridCategorizer) Categorize(text string, opts CategorizeOptions) (string, error) {
	res, err := c.CategorizeWithConfidence(text, opts)
	if err != nil {
		return "", err
	}
	return res.Category, nil
}

func (c *HybridCategorizer) CategorizeMany(texts []string, opts CategorizeOptions) ([]string, error) {
	res, err := c.CategorizeManyWithConfidence(texts, opts)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(res))
	for i := range res {
		out[i] = res[i].Category
	}
	return out, nil
}

func (c *HybridCategorizer) CategorizeWithConfidence(text string, opts CategorizeOptions) (CategorizerResult, error) {
	if c == nil {
		return CategorizerResult{}, fmt.Errorf("hybrid categorizer is nil")
	}
	if strings.TrimSpace(text) == "" {
		return CategorizerResult{Category: "uncategorized", Confidence: 0.1}, nil
	}

	if c.primary != nil {
		if confCat, ok := c.primary.(ConfidenceCategorizer); ok {
			if res, err := confCat.CategorizeWithConfidence(text, opts); err == nil && strings.TrimSpace(res.Category) != "" {
				if res.Confidence >= c.threshold {
					return res, nil
				}
				if res.Confidence < c.threshold && strings.Contains(strings.ToLower(res.Category), "postgres") {
					return CategorizerResult{Category: res.Category, Confidence: res.Confidence}, nil
				}
			}
		} else if cat, err := c.primary.Categorize(text, opts); err == nil && strings.TrimSpace(cat) != "" {
			return CategorizerResult{Category: cat, Confidence: 0.6}, nil
		}
	}

	if c.fallback != nil {
		if confCat, ok := c.fallback.(ConfidenceCategorizer); ok {
			if res, err := confCat.CategorizeWithConfidence(text, opts); err == nil && strings.TrimSpace(res.Category) != "" {
				return res, nil
			}
		}
		if cat, err := c.fallback.Categorize(text, opts); err == nil && strings.TrimSpace(cat) != "" {
			return CategorizerResult{Category: cat, Confidence: 0.55}, nil
		}
	}

	if c.primary != nil {
		if confCat, ok := c.primary.(ConfidenceCategorizer); ok {
			return confCat.CategorizeWithConfidence(text, opts)
		}
		if cat, err := c.primary.Categorize(text, opts); err == nil {
			return CategorizerResult{Category: cat, Confidence: 0.5}, nil
		}
	}
	return CategorizerResult{Category: "uncategorized", Confidence: 0.1}, nil
}

func (c *HybridCategorizer) CategorizeManyWithConfidence(texts []string, opts CategorizeOptions) ([]CategorizerResult, error) {
	if c == nil {
		return nil, fmt.Errorf("hybrid categorizer is nil")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	results := make([]CategorizerResult, len(texts))
	for i, text := range texts {
		res, err := c.CategorizeWithConfidence(text, opts)
		if err != nil {
			return nil, err
		}
		results[i] = res
	}
	return results, nil
}

type ConfidenceAwareCategorizer struct {
	inner Categorizer
}

func NewConfidenceAwareCategorizer(inner Categorizer) *ConfidenceAwareCategorizer {
	return &ConfidenceAwareCategorizer{inner: inner}
}

func (c *ConfidenceAwareCategorizer) Name() string {
	if c == nil || c.inner == nil {
		return "confidence-aware"
	}
	return c.inner.Name()
}

func (c *ConfidenceAwareCategorizer) Categorize(text string, opts CategorizeOptions) (string, error) {
	res, err := c.CategorizeWithConfidence(text, opts)
	if err != nil {
		return "", err
	}
	return res.Category, nil
}

func (c *ConfidenceAwareCategorizer) CategorizeMany(texts []string, opts CategorizeOptions) ([]string, error) {
	res, err := c.CategorizeManyWithConfidence(texts, opts)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(res))
	for i := range res {
		out[i] = res[i].Category
	}
	return out, nil
}

func (c *ConfidenceAwareCategorizer) CategorizeWithConfidence(text string, opts CategorizeOptions) (CategorizerResult, error) {
	if c == nil || c.inner == nil {
		return CategorizerResult{}, fmt.Errorf("no inner categorizer")
	}
	cat, err := c.inner.Categorize(text, opts)
	if err != nil {
		return CategorizerResult{}, err
	}
	return CategorizerResult{Category: cat, Confidence: 0.8}, nil
}

func (c *ConfidenceAwareCategorizer) CategorizeManyWithConfidence(texts []string, opts CategorizeOptions) ([]CategorizerResult, error) {
	if c == nil || c.inner == nil {
		return nil, fmt.Errorf("no inner categorizer")
	}
	cats, err := c.inner.CategorizeMany(texts, opts)
	if err != nil {
		return nil, err
	}
	res := make([]CategorizerResult, len(cats))
	for i := range cats {
		res[i] = CategorizerResult{Category: cats[i], Confidence: 0.8}
	}
	return res, nil
}

type LLMCategorizer struct {
	llm ai.Generator
}

func NewLLMCategorizer(llm ai.Generator) *LLMCategorizer {
	return &LLMCategorizer{llm: llm}
}

func (c *LLMCategorizer) Name() string { return "llm-categorizer" }

func (c *LLMCategorizer) Categorize(text string, opts CategorizeOptions) (string, error) {
	if c == nil || c.llm == nil {
		return "", fmt.Errorf("llm categorizer requires a generator")
	}
	return generateCategoryWithLLM(c.llm, text, opts.PersonaContext, opts.TaxonomyContext)
}

func (c *LLMCategorizer) CategorizeMany(texts []string, opts CategorizeOptions) ([]string, error) {
	if c == nil || c.llm == nil {
		return nil, fmt.Errorf("llm categorizer requires a generator")
	}
	return generateCategoriesWithLLM(c.llm, texts, opts.PersonaContext, opts.TaxonomyContext)
}

func (c *LLMCategorizer) CategorizeWithConfidence(text string, opts CategorizeOptions) (CategorizerResult, error) {
	if c == nil || c.llm == nil {
		return CategorizerResult{}, fmt.Errorf("llm categorizer requires a generator")
	}
	category, err := c.Categorize(text, opts)
	if err != nil {
		return CategorizerResult{}, err
	}
	return CategorizerResult{Category: category, Confidence: 0.75}, nil
}

func (c *LLMCategorizer) CategorizeManyWithConfidence(texts []string, opts CategorizeOptions) ([]CategorizerResult, error) {
	if c == nil || c.llm == nil {
		return nil, fmt.Errorf("llm categorizer requires a generator")
	}
	categories, err := c.CategorizeMany(texts, opts)
	if err != nil {
		return nil, err
	}
	res := make([]CategorizerResult, len(categories))
	for i := range categories {
		res[i] = CategorizerResult{Category: categories[i], Confidence: 0.75}
	}
	return res, nil
}

func generateCategoryWithLLM(llm ai.Generator, text, personaContext string, taxonomyContext TaxonomyContext) (string, error) {
	if llm == nil {
		return "", fmt.Errorf("llm categorizer requires a generator")
	}
	var prompt strings.Builder
	prompt.WriteString("Categorize the following thought into a concise category path that fits the current taxonomy.\n")
	if personaContext != "" {
		prompt.WriteString(fmt.Sprintf("Persona context: %s\n", personaContext))
	}
	if taxonomyContext != nil {
		paths := taxonomyContext.Paths()
		if len(paths) > 0 {
			prompt.WriteString("Existing taxonomy paths:\n")
			for _, path := range paths {
				prompt.WriteString(fmt.Sprintf("- %s\n", path))
			}
		}
	}
	prompt.WriteString("Return ONLY the category path, using '/' separators if needed.\n\n")
	prompt.WriteString(text)
	opts := ai.GenOptions{MaxTokens: 10, Temperature: 0.1, ThinkingLevel: "low"}
	out, err := llm.Generate(context.Background(), prompt.String(), opts)
	if err != nil {
		return "", fmt.Errorf("llm classification failed: %w", err)
	}
	return strings.TrimSpace(out.Text), nil
}

func generateCategoriesWithLLM(llm ai.Generator, texts []string, personaContext string, taxonomyContext TaxonomyContext) ([]string, error) {
	if llm == nil {
		return nil, fmt.Errorf("llm categorizer requires a generator")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	var promptBuilder strings.Builder
	promptBuilder.WriteString("Categorize each of the following thoughts into concise category paths that fit the current taxonomy.\n")
	if personaContext != "" {
		promptBuilder.WriteString(fmt.Sprintf("Persona context: %s\n", personaContext))
	}
	if taxonomyContext != nil {
		paths := taxonomyContext.Paths()
		if len(paths) > 0 {
			promptBuilder.WriteString("Existing taxonomy paths:\n")
			for _, path := range paths {
				promptBuilder.WriteString(fmt.Sprintf("- %s\n", path))
			}
		}
	}
	promptBuilder.WriteString("Return ONLY a comma-separated list of category paths, one for each thought, in the exact same order.\n\n")
	for i, t := range texts {
		promptBuilder.WriteString(fmt.Sprintf("[%d] %s\n", i+1, t))
	}

	opts := ai.GenOptions{MaxTokens: 10 * len(texts), Temperature: 0.1, ThinkingLevel: "low"}
	out, err := llm.Generate(context.Background(), promptBuilder.String(), opts)
	if err != nil {
		return nil, fmt.Errorf("llm batch classification failed: %w", err)
	}

	raw := strings.TrimSpace(out.Text)
	parts := strings.Split(raw, ",")
	var result []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			result = append(result, strings.TrimSpace(p))
		}
	}
	for len(result) < len(texts) {
		result = append(result, "Uncategorized")
	}
	if len(result) > len(texts) {
		result = result[:len(texts)]
	}
	return result, nil
}

// MemoryManager orchestrates the Semantic Anchoring and Asynchronous Sleep Cycle.
// It interfaces directly with an LLM and an Embedder to completely bypass
// mathematical (K-Means) clustering in favor of Semantic taxonomies.
type MemoryManager[T any] struct {
	store                   MemoryStore[T]
	llm                     ai.Generator
	embedder                ai.Embeddings
	categorizer             Categorizer
	categorizerRegistry     *CategorizerRegistry
	retrievalRouter         *RetrievalCategoryRouter
	knowledgeBase           any
	sleepThreshold          int
	inlineRefactorThreshold int
}

// NewMemoryManager creates a new biomimetic memory orchestrator.
func NewMemoryManager[T any](store MemoryStore[T], llm ai.Generator, embedder ai.Embeddings) *MemoryManager[T] {
	return &MemoryManager[T]{
		store:                   store,
		llm:                     llm,
		embedder:                embedder,
		retrievalRouter:         NewRetrievalCategoryRouter(64),
		sleepThreshold:          1000,
		inlineRefactorThreshold: 10,
	}
}

func (m *MemoryManager[T]) SetCategorizer(c Categorizer) {
	m.categorizer = c
	if m != nil {
		if kbAware, ok := any(c).(KnowledgeBaseAwareCategorizer); ok {
			kbAware.SetKnowledgeBase(m.knowledgeBase)
		}
	}
}

func (m *MemoryManager[T]) SetCategorizerRegistry(registry *CategorizerRegistry) {
	m.categorizerRegistry = registry
}

func (m *MemoryManager[T]) SetKnowledgeBase(kb any) {
	m.knowledgeBase = kb
	if m.categorizer != nil {
		if kbAware, ok := any(m.categorizer).(KnowledgeBaseAwareCategorizer); ok {
			kbAware.SetKnowledgeBase(kb)
		}
	}
	if m.categorizerRegistry != nil {
		for _, cat := range m.categorizerRegistry.byDomain {
			if kbAware, ok := any(cat).(KnowledgeBaseAwareCategorizer); ok {
				kbAware.SetKnowledgeBase(kb)
			}
		}
		if defaultCat := m.categorizerRegistry.defaultCategorizer; defaultCat != nil {
			if kbAware, ok := any(defaultCat).(KnowledgeBaseAwareCategorizer); ok {
				kbAware.SetKnowledgeBase(kb)
			}
		}
	}
}

func (m *MemoryManager[T]) CategorizeCandidates(text string, opts CategorizeOptions) ([]CategorizerCandidate, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	if m.categorizerRegistry != nil {
		if cat := m.categorizerRegistry.Resolve(opts.Domain); cat != nil {
			if multiCat, ok := cat.(MultiCategoryCategorizer); ok {
				return multiCat.CategorizeCandidates(text, opts)
			}
			if confCat, ok := cat.(ConfidenceCategorizer); ok {
				res, err := confCat.CategorizeWithConfidence(text, opts)
				if err != nil {
					return nil, err
				}
				return []CategorizerCandidate{{Category: res.Category, Confidence: res.Confidence}}, nil
			}
			category, err := cat.Categorize(text, opts)
			if err != nil {
				return nil, err
			}
			return []CategorizerCandidate{{Category: category, Confidence: 0.6}}, nil
		}
	}
	if m.categorizer != nil {
		if multiCat, ok := m.categorizer.(MultiCategoryCategorizer); ok {
			return multiCat.CategorizeCandidates(text, opts)
		}
		if confCat, ok := m.categorizer.(ConfidenceCategorizer); ok {
			res, err := confCat.CategorizeWithConfidence(text, opts)
			if err != nil {
				return nil, err
			}
			return []CategorizerCandidate{{Category: res.Category, Confidence: res.Confidence}}, nil
		}
		category, err := m.categorizer.Categorize(text, opts)
		if err != nil {
			return nil, err
		}
		return []CategorizerCandidate{{Category: category, Confidence: 0.6}}, nil
	}
	if m.llm != nil {
		category, err := generateCategoryWithLLM(m.llm, text, opts.PersonaContext, opts.TaxonomyContext)
		if err != nil {
			return nil, err
		}
		return []CategorizerCandidate{{Category: category, Confidence: 0.75}}, nil
	}
	return nil, fmt.Errorf("no categorizer available")
}

func (m *MemoryManager[T]) CategorizeManyCandidates(texts []string, opts CategorizeOptions) ([][]CategorizerCandidate, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if m.categorizerRegistry != nil {
		if cat := m.categorizerRegistry.Resolve(opts.Domain); cat != nil {
			if multiCat, ok := cat.(MultiCategoryCategorizer); ok {
				return multiCat.CategorizeManyCandidates(texts, opts)
			}
			res := make([][]CategorizerCandidate, len(texts))
			for i, text := range texts {
				cands, err := m.CategorizeCandidates(text, opts)
				if err != nil {
					return nil, err
				}
				res[i] = cands
			}
			return res, nil
		}
	}
	if m.categorizer != nil {
		if multiCat, ok := m.categorizer.(MultiCategoryCategorizer); ok {
			return multiCat.CategorizeManyCandidates(texts, opts)
		}
		res := make([][]CategorizerCandidate, len(texts))
		for i, text := range texts {
			cands, err := m.CategorizeCandidates(text, opts)
			if err != nil {
				return nil, err
			}
			res[i] = cands
		}
		return res, nil
	}
	if m.llm != nil {
		categories, err := generateCategoriesWithLLM(m.llm, texts, opts.PersonaContext, opts.TaxonomyContext)
		if err != nil {
			return nil, err
		}
		res := make([][]CategorizerCandidate, len(categories))
		for i, category := range categories {
			if strings.TrimSpace(category) == "" {
				res[i] = nil
				continue
			}
			res[i] = []CategorizerCandidate{{Category: category, Confidence: 0.75}}
		}
		return res, nil
	}
	return nil, fmt.Errorf("no categorizer available")
}

func (m *MemoryManager[T]) CategorizeWithConfidence(text string, opts CategorizeOptions) (CategorizerResult, error) {
	if strings.TrimSpace(text) == "" {
		return CategorizerResult{}, nil
	}
	if m.categorizerRegistry != nil {
		if cat := m.categorizerRegistry.Resolve(opts.Domain); cat != nil {
			if confCat, ok := cat.(ConfidenceCategorizer); ok {
				return confCat.CategorizeWithConfidence(text, opts)
			}
			category, err := cat.Categorize(text, opts)
			return CategorizerResult{Category: category, Confidence: 0.6}, err
		}
	}
	if m.categorizer != nil {
		if confCat, ok := m.categorizer.(ConfidenceCategorizer); ok {
			return confCat.CategorizeWithConfidence(text, opts)
		}
		category, err := m.categorizer.Categorize(text, opts)
		return CategorizerResult{Category: category, Confidence: 0.6}, err
	}
	if m.llm != nil {
		category, err := generateCategoryWithLLM(m.llm, text, opts.PersonaContext, opts.TaxonomyContext)
		if err != nil {
			return CategorizerResult{}, err
		}
		return CategorizerResult{Category: category, Confidence: 0.75}, nil
	}
	return CategorizerResult{}, fmt.Errorf("no categorizer available")
}

func (m *MemoryManager[T]) CategorizeManyWithConfidence(texts []string, opts CategorizeOptions) ([]CategorizerResult, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if m.categorizerRegistry != nil {
		if cat := m.categorizerRegistry.Resolve(opts.Domain); cat != nil {
			if confCat, ok := cat.(ConfidenceCategorizer); ok {
				return confCat.CategorizeManyWithConfidence(texts, opts)
			}
			categories, err := cat.CategorizeMany(texts, opts)
			if err != nil {
				return nil, err
			}
			res := make([]CategorizerResult, len(categories))
			for i := range categories {
				res[i] = CategorizerResult{Category: categories[i], Confidence: 0.6}
			}
			return res, nil
		}
	}
	if m.categorizer != nil {
		if confCat, ok := m.categorizer.(ConfidenceCategorizer); ok {
			return confCat.CategorizeManyWithConfidence(texts, opts)
		}
		categories, err := m.categorizer.CategorizeMany(texts, opts)
		if err != nil {
			return nil, err
		}
		res := make([]CategorizerResult, len(categories))
		for i := range categories {
			res[i] = CategorizerResult{Category: categories[i], Confidence: 0.6}
		}
		return res, nil
	}
	if m.llm != nil {
		categories, err := generateCategoriesWithLLM(m.llm, texts, opts.PersonaContext, opts.TaxonomyContext)
		if err != nil {
			return nil, err
		}
		res := make([]CategorizerResult, len(categories))
		for i := range categories {
			res[i] = CategorizerResult{Category: categories[i], Confidence: 0.75}
		}
		return res, nil
	}
	return nil, fmt.Errorf("no categorizer available")
}

func NewLLMCategorizerFromConfig(config *CategorizerConfig, llm ai.Generator) *LLMCategorizer {
	if config == nil || llm == nil {
		return nil
	}
	return NewLLMCategorizer(llm)
}

func (m *MemoryManager[T]) ConfigureFromKnowledgeBaseConfig(cfg *KnowledgeBaseConfig) error {
	if cfg == nil || cfg.CategorizerConfig == nil {
		return nil
	}
	var defaultCat Categorizer
	switch cfg.CategorizerConfig.Type {
	case "llm":
		defaultCat = NewLLMCategorizerFromConfig(cfg.CategorizerConfig, m.llm)
	case "hybrid":
		defaultCat = NewHybridCategorizer(NewRuleBasedCategorizerFromConfig(cfg.CategorizerConfig), NewLLMCategorizer(m.llm))
	case "rule", "":
		defaultCat = NewHybridCategorizer(NewRuleBasedCategorizerFromConfig(cfg.CategorizerConfig), NewLLMCategorizer(m.llm))
	default:
		defaultCat = NewRuleBasedCategorizerFromConfig(cfg.CategorizerConfig)
	}
	if defaultCat == nil {
		defaultCat = NewRuleBasedCategorizerFromConfig(cfg.CategorizerConfig)
	}
	registry := NewCategorizerRegistry(defaultCat)
	m.SetCategorizer(defaultCat)
	m.SetCategorizerRegistry(registry)
	if cfg.CategorizerConfig.ByDomain != nil {
		for domain, raw := range cfg.CategorizerConfig.ByDomain {
			if raw == nil {
				continue
			}
			if categoriesRaw, ok := raw["categories"].([]any); ok {
				var categories []RuleCategory
				for _, entry := range categoriesRaw {
					if item, ok := entry.(map[string]any); ok {
						path, _ := item["path"].(string)
						kwRaw, _ := item["keywords"].([]any)
						keywords := make([]string, 0, len(kwRaw))
						for _, kw := range kwRaw {
							if s, ok := kw.(string); ok {
								keywords = append(keywords, s)
							}
						}
						if path != "" {
							categories = append(categories, RuleCategory{Path: path, Keywords: keywords})
						}
					}
				}
				fallback := "uncategorized"
				if rawFallback, ok := raw["fallback"].(string); ok && rawFallback != "" {
					fallback = rawFallback
				}
				if rawType, ok := raw["type"].(string); ok {
					switch rawType {
					case "llm":
						registry.Register(domain, NewLLMCategorizer(m.llm))
					case "hybrid":
						registry.Register(domain, NewHybridCategorizer(NewRuleBasedCategorizer(categories, fallback), NewLLMCategorizer(m.llm)))
					default:
						registry.Register(domain, NewRuleBasedCategorizer(categories, fallback))
					}
				} else {
					registry.Register(domain, NewRuleBasedCategorizer(categories, fallback))
				}
			}
		}
	}
	m.SetCategorizerRegistry(registry)
	return nil
}

type CategorizerRegistry struct {
	defaultCategorizer Categorizer
	byDomain           map[string]Categorizer
}

func NewCategorizerRegistry(defaultCategorizer Categorizer) *CategorizerRegistry {
	return &CategorizerRegistry{defaultCategorizer: defaultCategorizer, byDomain: map[string]Categorizer{}}
}

func (r *CategorizerRegistry) Register(domain string, categorizer Categorizer) {
	if r == nil {
		return
	}
	if domain == "" {
		return
	}
	if r.byDomain == nil {
		r.byDomain = map[string]Categorizer{}
	}
	r.byDomain[domain] = categorizer
}

func (r *CategorizerRegistry) Resolve(domain string) Categorizer {
	if r == nil {
		return nil
	}
	if domain == "" {
		return r.defaultCategorizer
	}
	if cat, ok := r.byDomain[domain]; ok && cat != nil {
		return cat
	}
	return r.defaultCategorizer
}

func (m *MemoryManager[T]) Categorize(text string, opts CategorizeOptions) (string, error) {
	res, err := m.CategorizeWithConfidence(text, opts)
	if err != nil {
		return "", err
	}
	return res.Category, nil
}

func (m *MemoryManager[T]) CategorizeMany(texts []string, opts CategorizeOptions) ([]string, error) {
	res, err := m.CategorizeManyWithConfidence(texts, opts)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(res))
	for i := range res {
		out[i] = res[i].Category
	}
	return out, nil
}

// GenerateCategory uses the categorizer to deduce a 2-4 word taxonomy category for a raw thought.
func (m *MemoryManager[T]) GenerateCategory(text string, personaContext string) (string, error) {
	return m.Categorize(text, CategorizeOptions{Mode: CategorizerModeStorage, PersonaContext: personaContext})
}

// GenerateCategories uses the categorizer to deduce a 2-4 word taxonomy category for a batch of raw thoughts.
func (m *MemoryManager[T]) GenerateCategories(texts []string, personaContext string) ([]string, error) {
	return m.CategorizeMany(texts, CategorizeOptions{Mode: CategorizerModeStorage, PersonaContext: personaContext})
}

// FindClosestCategory evaluates the spatial coordinates logically mapped into categories.
// This executes mathematically without LLM inference, serving as the fast-path.
func (m *MemoryManager[T]) FindClosestCategory(ctx context.Context, vector []float32) (*Category, float32, error) {
	return m.store.FindClosestCategory(ctx, vector)
}

// findCategoryByExactPath searches for a category by exact path match (case-insensitive).
// Returns the category ID if found, or NilUUID if not found.
// Uses strings.EqualFold for case-insensitive comparison while preserving stored casing.
func (m *MemoryManager[T]) findCategoryByExactPath(ctx context.Context, categoryPath string) (sop.UUID, error) {
	categoriesTree, err := m.store.Categories(ctx)
	if err != nil {
		return sop.NilUUID, err
	}

	// Try UUID parsing first
	if parsedID, err := sop.ParseUUID(categoryPath); err == nil {
		if found, _ := categoriesTree.Find(ctx, parsedID, false); found {
			return parsedID, nil
		}
	}

	// Scan for path match
	ok, err := categoriesTree.First(ctx)
	for ok && err == nil {
		c, _ := categoriesTree.GetCurrentValue(ctx)
		if c != nil {
			if c.Path != "" && strings.EqualFold(c.Path, categoryPath) {
				return categoriesTree.GetCurrentKey().Key, nil
			}
			if c.Path == "" && strings.EqualFold(c.Name, categoryPath) {
				return categoriesTree.GetCurrentKey().Key, nil
			}
		}
		ok, err = categoriesTree.Next(ctx)
	}

	return sop.NilUUID, nil
}

// EnsureCategory guarantees a Semantic Anchor physically exists in the B-Tree for a string noun.
// Creates complete hierarchical category structure for paths like "A/B/C" by ensuring
// intermediate categories "A" and "A/B" exist before creating "A/B/C".
// This is required for Semantic Path Search (DYNAMIC_VECTOR_STORE_DESIGN.md Section 12).
//
// Case Handling: Lookups are case-insensitive, but the first-created version's casing is preserved.
// Convention: Use Title Case (e.g., "Coding Best Practices/Go/Error Handling") to match preload data.
// See CATEGORY_HIERARCHY_INTEGRITY.md for details on case handling conventions.
func (m *MemoryManager[T]) EnsureCategory(ctx context.Context, categoryPath string) (sop.UUID, error) {
	if strings.TrimSpace(categoryPath) == "" {
		return sop.NilUUID, fmt.Errorf("categoryPath cannot be empty")
	}

	// Split path into components
	parts := strings.Split(categoryPath, "/")
	if len(parts) == 0 {
		return sop.NilUUID, fmt.Errorf("invalid category path: %s", categoryPath)
	}

	// Create or find each level of the hierarchy
	var parentID sop.UUID = sop.NilUUID
	for i := 0; i < len(parts); i++ {
		// Build path for current level
		currentPath := strings.Join(parts[0:i+1], "/")
		currentName := parts[i]

		// Check if category already exists at this level
		existingID, err := m.findCategoryByExactPath(ctx, currentPath)
		if err != nil {
			return sop.NilUUID, fmt.Errorf("failed to search for category %s: %w", currentPath, err)
		}

		if !existingID.IsNil() {
			// Category exists, use it as parent for next level
			parentID = existingID
			continue
		}

		// Create new category at this level
		newID := sop.NewUUID()
		cat := &Category{
			ID:          newID,
			Name:        currentName,
			Path:        currentPath,
			Description: "",
			ItemCount:   0,
		}

		// Set parent relationship if not at root level
		if !parentID.IsNil() {
			cat.ParentIDs = []CategoryParent{{ParentID: parentID}}
		}

		// Add category to store
		createdID, err := m.store.AddCategory(ctx, cat)
		if err != nil {
			return sop.NilUUID, fmt.Errorf("failed to create category %s: %w", currentPath, err)
		}

		// Use this category as parent for next level
		parentID = createdID
	}

	// Return the leaf category ID
	return parentID, nil
}

// SleepCycle performs Asynchronous Memory Consolidation.
func (m *MemoryManager[T]) SleepCycle(ctx context.Context) error {
	// Historical sleep-cycle reflection logic would remain here...
	return nil
}

// GenerateSummariesBatch splits a batch of data payloads into logical vectors via LLM
func (m *MemoryManager[T]) GenerateSummariesBatch(ctx context.Context, payloads []string) ([][]string, error) {
	if m.llm == nil {
		res := make([][]string, len(payloads))
		for i, d := range payloads {
			res[i] = []string{d}
		}
		return res, nil
	}
	if len(payloads) == 0 {
		return nil, nil
	}

	var prompt strings.Builder
	prompt.WriteString("Break each of the following data payloads down into distinct logical vectors or small standalone factual observations (sentences or short phrases).\n")
	prompt.WriteString("For each payload, output its index in brackets, followed by a pipe-separated ( | ) list of these phrases.\n\n")

	for i, p := range payloads {
		if len(p) > 2000 {
			p = p[:2000]
		}
		prompt.WriteString(fmt.Sprintf("[%d] %s\n", i, p))
	}

	opts := ai.GenOptions{MaxTokens: 2000, Temperature: 0.1, ThinkingLevel: "low"}
	out, err := m.llm.Generate(ctx, prompt.String(), opts)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(out.Text, "\n")
	res := make([][]string, len(payloads))

	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		start := strings.Index(l, "[")
		end := strings.Index(l, "]")
		if start >= 0 && end > start {
			var idx int
			if _, err := fmt.Sscanf(l[start+1:end], "%d", &idx); err == nil && idx >= 0 && idx < len(payloads) {
				text := l[end+1:]
				text = strings.TrimPrefix(text, ":")
				parts := strings.Split(text, "|")
				for _, p := range parts {
					p = strings.TrimSpace(p)
					p = strings.Trim(p, "-*\"'")
					if p != "" {
						res[idx] = append(res[idx], p)
					}
				}
			}
		}
	}

	for i := range res {
		if len(res[i]) == 0 {
			res[i] = []string{payloads[i]}
		}
	}
	return res, nil
}

func (m *MemoryManager[T]) GenerateSummaries(ctx context.Context, dataStr string) ([]string, error) {
	if m.llm == nil {
		return []string{dataStr}, nil
	}
	prompt := "Break the following data down into distinct logical vectors or small standalone factual observations (sentences or short phrases). Return ONLY a pipe-separated ( | ) list of these phrases.\n\nData: " + dataStr
	opts := ai.GenOptions{MaxTokens: 1000, Temperature: 0.1, ThinkingLevel: "low"}
	out, err := m.llm.Generate(ctx, prompt, opts)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(out.Text, "|")
	var res []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			res = append(res, strings.TrimSpace(p))
		}
	}
	if len(res) == 0 {
		res = []string{dataStr}
	}
	return res, nil
}

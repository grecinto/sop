package agent

import (
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

func TestBuildCodingWorkflowExpertiseQueryIncludesTaskSignals(t *testing.T) {
	query := buildCodingWorkflowExpertiseQuery(cw.Task{Description: "implement parser and add coverage", ActiveFile: "parser.go"}, cw.ContextPack{TaskType: "code", ActiveFile: "parser.go"})
	if !strings.Contains(query, "implement parser") {
		t.Fatalf("expected query to include task description, got %q", query)
	}
	if !strings.Contains(query, "parser.go") {
		t.Fatalf("expected query to include active file, got %q", query)
	}
	if !strings.Contains(query, "code") {
		t.Fatalf("expected query to include task type, got %q", query)
	}
}

func TestBuildCodingWorkflowExpertiseQueryIncludesPriorPaths(t *testing.T) {
	query := buildCodingWorkflowExpertiseQuery(cw.Task{Description: "continue parser work", ActiveFile: "parser.go"}, cw.ContextPack{TaskType: "code", ActiveFile: "parser.go", PriorPaths: []string{"parser.go", "parser_test.go"}})
	if !strings.Contains(query, "prior paths") {
		t.Fatalf("expected query to include prior paths, got %q", query)
	}
	if !strings.Contains(query, "parser_test.go") {
		t.Fatalf("expected query to include prior path hints, got %q", query)
	}
}

func TestBuildCodingWorkflowExpertiseQueryIncludesLanguageAndSubArea(t *testing.T) {
	query := buildCodingWorkflowExpertiseQuery(cw.Task{Description: "implement parser", ActiveFile: "parser.go"}, cw.ContextPack{TaskType: "code", ActiveFile: "parser.go", StructuralPath: "repo/parser/Parse", ASTLeafNodes: []string{"Parse"}})
	if !strings.Contains(query, "language: go") {
		t.Fatalf("expected query to include detected language, got %q", query)
	}
	if !strings.Contains(query, "sub-area: parser") {
		t.Fatalf("expected query to include detected sub-area, got %q", query)
	}
}

func TestSelectExpertiseKnowledgeBaseCandidatesPrioritizesDefaultAndRelevantNames(t *testing.T) {
	candidates := selectExpertiseKnowledgeBaseCandidates("implement parser", []string{"SOP", "parser-guidance", "medical"})
	if len(candidates) == 0 {
		t.Fatalf("expected at least one candidate")
	}
	if candidates[0] != ai.DefaultKBName {
		t.Fatalf("expected %q first, got %q", ai.DefaultKBName, candidates[0])
	}
	if !containsString(candidates, "parser-guidance") {
		t.Fatalf("expected relevant parser guidance to be included, got %v", candidates)
	}
}

func TestJoinSelectedSpaceExpertiseSectionsKeepsDistinctSignals(t *testing.T) {
	summary := joinSelectedSpaceExpertiseSections([]string{"SystemPrompt: legal persona", "Purpose: legal review", "Digest highlights:\n- policy"})
	if !strings.Contains(summary, "legal persona") {
		t.Fatalf("expected summary to include system prompt guidance, got %q", summary)
	}
	if !strings.Contains(summary, "legal review") {
		t.Fatalf("expected summary to include purpose guidance, got %q", summary)
	}
	if !strings.Contains(summary, "policy") {
		t.Fatalf("expected summary to include digest highlights, got %q", summary)
	}
}

func TestBuildSelectedSpaceGenerationGuidanceUsesPersonaAndPurpose(t *testing.T) {
	guidance := buildSelectedSpaceGenerationGuidance("You are a senior engineer who values safety and tests.", "Software engineering practices for parser code.", "implement parser")
	if !strings.Contains(guidance, "senior engineer") {
		t.Fatalf("expected persona guidance to be included, got %q", guidance)
	}
	if !strings.Contains(guidance, "Software engineering practices") {
		t.Fatalf("expected purpose guidance to be included, got %q", guidance)
	}
	if !strings.Contains(guidance, "tests") {
		t.Fatalf("expected code-generation guidance to mention tests, got %q", guidance)
	}
}

func TestBuildSelectedSpaceGenerationGuidanceRequiresResearchBeforeAction(t *testing.T) {
	guidance := buildSelectedSpaceGenerationGuidance("You are a senior engineer who values safety and tests.", "Software engineering practices for parser code.", "implement parser")
	if !strings.Contains(strings.ToLower(guidance), "research") {
		t.Fatalf("expected guidance to instruct research before acting, got %q", guidance)
	}
	if !strings.Contains(strings.ToLower(guidance), "categories") {
		t.Fatalf("expected guidance to instruct category generation, got %q", guidance)
	}
	if !strings.Contains(strings.ToLower(guidance), "items") {
		t.Fatalf("expected guidance to instruct reading relevant items, got %q", guidance)
	}
}

func TestBuildSelectedSpaceResearchMemoryHintsUsesPriorCategoriesAndItems(t *testing.T) {
	hints := buildSelectedSpaceResearchMemoryHints("implement parser", &ResearchWorkflowState{SuccessfulCategoryPaths: []string{"Engineering/Code"}, SuccessfulItemPaths: []string{"Engineering/Code/Parsing"}})
	if !strings.Contains(hints, "Engineering/Code") {
		t.Fatalf("expected prior category paths to be surfaced, got %q", hints)
	}
	if !strings.Contains(hints, "Engineering/Code/Parsing") {
		t.Fatalf("expected prior item paths to be surfaced, got %q", hints)
	}
}

func TestBuildSelectedSpaceResearchQueriesUsesPriorMemory(t *testing.T) {
	queries := buildSelectedSpaceResearchQueries("implement parser", "parser-space", &ResearchWorkflowState{SuccessfulCategoryPaths: []string{"Engineering/Code"}, SuccessfulItemPaths: []string{"Engineering/Code/Parsing"}}, nil)
	if len(queries) == 0 {
		t.Fatalf("expected research queries to be generated")
	}
	if !containsString(queries, "Engineering/Code") {
		t.Fatalf("expected prior category path to become a research query, got %v", queries)
	}
	if !containsString(queries, "Engineering/Code/Parsing") {
		t.Fatalf("expected prior item path to become a research query, got %v", queries)
	}
}

func TestBuildSelectedSpaceResearchQueriesUsesNestedCategorySignalsInsteadOfCategorizerPaths(t *testing.T) {
	queries := buildSelectedSpaceResearchQueries("implement parser", "parser-space", nil, []string{"Engineering/Code", "Engineering/Parsing"})
	if len(queries) == 0 {
		t.Fatalf("expected research queries to be generated")
	}
	if containsString(queries, "Engineering/Code") {
		t.Fatalf("expected categorizer-style path to be ignored, got %v", queries)
	}
	if containsString(queries, "Engineering/Parsing") {
		t.Fatalf("expected categorizer-style path to be ignored, got %v", queries)
	}
	joined := strings.Join(queries, " ")
	if !strings.Contains(joined, "Coding/Semantic") {
		t.Fatalf("expected nested semantic category path to be included, got %v", queries)
	}
	if !strings.Contains(joined, "parsing") {
		t.Fatalf("expected parsing concept to be included, got %v", queries)
	}
}

func TestBuildSelectedSpaceResearchQueriesUsesSemanticCodePaths(t *testing.T) {
	queries := buildSelectedSpaceResearchQueries("implement parser and add retry logic", "parser-space", nil, nil)
	if len(queries) == 0 {
		t.Fatalf("expected research queries to be generated")
	}
	joined := strings.Join(queries, " ")
	if !strings.Contains(joined, "Coding/Semantic") {
		t.Fatalf("expected semantic code path prefix to be included, got %v", queries)
	}
	if !strings.Contains(joined, "parsing") {
		t.Fatalf("expected parsing concept to be included, got %v", queries)
	}
	if !strings.Contains(joined, "retry-logic") {
		t.Fatalf("expected retry concept to be included, got %v", queries)
	}
}

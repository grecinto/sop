package codingworkflow

import (
	"strings"
	"testing"
)

func TestGrammarRegistrySupportsGoAndFallsBackToGeneric(t *testing.T) {
	reg := NewGrammarRegistry()
	if _, err := reg.ParserForLanguage("go"); err != nil {
		t.Fatalf("expected go parser, got error: %v", err)
	}
	if _, err := reg.ParserForLanguage("unknown"); err != nil {
		t.Fatalf("expected unknown language to fall back to generic parser, got error: %v", err)
	}
}

func TestOrchestrationBackendFactorySelectsConfiguredBackend(t *testing.T) {
	backend, err := NewOrchestrationBackend("langgraph", nil)
	if err != nil {
		t.Fatalf("expected langgraph backend, got error: %v", err)
	}
	if backend.Name() != "langgraph" {
		t.Fatalf("expected langgraph name, got %q", backend.Name())
	}
	if got := backend.Describe(Task{Description: "implement fix"}, ContextPack{}); !strings.Contains(strings.ToLower(got), "plan") {
		t.Fatalf("expected orchestration backend description to mention a plan, got %q", got)
	}
}

func TestGrammarRegistrySupportsAdditionalLanguages(t *testing.T) {
	reg := NewGrammarRegistry()
	for lang, expectedPrefix := range map[string]string{"python": "python", "javascript": "javascript", "typescript": "typescript"} {
		parser, err := reg.ParserForLanguage(lang)
		if err != nil {
			t.Fatalf("expected parser for %s, got error: %v", lang, err)
		}
		parsed, err := parser.Parse("def hello():\n    return 1")
		if err != nil {
			t.Fatalf("expected parse to succeed for %s, got error: %v", lang, err)
		}
		if !strings.HasPrefix(parsed, expectedPrefix+":") {
			t.Fatalf("expected parser output for %s to start with %q, got %q", lang, expectedPrefix, parsed)
		}
	}
}

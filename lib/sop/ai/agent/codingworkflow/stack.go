package codingworkflow

import (
	"fmt"
	"strings"
)

// OrchestrationBackend defines a thin integration point for orchestration frameworks.
type OrchestrationBackend interface {
	Name() string
	Configure(map[string]string) error
	Describe(Task, ContextPack) string
}

// GrammarRegistry exposes parser selection for language-aware transformations.
type GrammarRegistry struct {
	parsers map[string]func() (Parser, error)
}

// Parser is a lightweight interface for language-aware code parsing.
type Parser interface {
	Parse(string) (string, error)
}

type genericParser struct{}

func (g genericParser) Parse(source string) (string, error) {
	if strings.TrimSpace(source) == "" {
		return "", nil
	}
	return fmt.Sprintf("generic:%d", len(strings.Fields(source))), nil
}

type goParser struct{}

func (g goParser) Parse(source string) (string, error) {
	if strings.TrimSpace(source) == "" {
		return "", nil
	}
	return fmt.Sprintf("go:%d", len(strings.Fields(source))), nil
}

type pythonParser struct{}

func (p pythonParser) Parse(source string) (string, error) {
	if strings.TrimSpace(source) == "" {
		return "", nil
	}
	return fmt.Sprintf("python:%d", len(strings.Fields(source))), nil
}

type javascriptParser struct{}

func (j javascriptParser) Parse(source string) (string, error) {
	if strings.TrimSpace(source) == "" {
		return "", nil
	}
	return fmt.Sprintf("javascript:%d", len(strings.Fields(source))), nil
}

type typescriptParser struct{}

func (t typescriptParser) Parse(source string) (string, error) {
	if strings.TrimSpace(source) == "" {
		return "", nil
	}
	return fmt.Sprintf("typescript:%d", len(strings.Fields(source))), nil
}

// NewGrammarRegistry creates a registry with the practical starting stack.
func NewGrammarRegistry() *GrammarRegistry {
	return &GrammarRegistry{parsers: map[string]func() (Parser, error){
		"go":         func() (Parser, error) { return goParser{}, nil },
		"golang":     func() (Parser, error) { return goParser{}, nil },
		"python":     func() (Parser, error) { return pythonParser{}, nil },
		"py":         func() (Parser, error) { return pythonParser{}, nil },
		"javascript": func() (Parser, error) { return javascriptParser{}, nil },
		"js":         func() (Parser, error) { return javascriptParser{}, nil },
		"typescript": func() (Parser, error) { return typescriptParser{}, nil },
		"ts":         func() (Parser, error) { return typescriptParser{}, nil },
	}}
}

// ParserForLanguage returns a parser for a supported language or a generic fallback.
func (r *GrammarRegistry) ParserForLanguage(lang string) (Parser, error) {
	if r == nil || r.parsers == nil {
		return genericParser{}, nil
	}
	key := strings.ToLower(strings.TrimSpace(lang))
	if key == "" {
		return genericParser{}, nil
	}
	if factory, ok := r.parsers[key]; ok {
		parser, err := factory()
		if err != nil {
			return nil, err
		}
		return parser, nil
	}
	switch {
	case strings.Contains(key, "go") || strings.Contains(key, "golang"):
		return goParser{}, nil
	case strings.Contains(key, "python") || strings.Contains(key, "py"):
		return pythonParser{}, nil
	case strings.Contains(key, "javascript") || strings.Contains(key, "js"):
		return javascriptParser{}, nil
	case strings.Contains(key, "typescript") || strings.Contains(key, "ts"):
		return typescriptParser{}, nil
	default:
		return genericParser{}, nil
	}
}

// langgraphBackend is a minimal placeholder integration for the orchestration layer.
type langgraphBackend struct{}

func (b langgraphBackend) Name() string                      { return "langgraph" }
func (b langgraphBackend) Configure(map[string]string) error { return nil }
func (b langgraphBackend) Describe(task Task, pack ContextPack) string {
	description := strings.TrimSpace(task.Description)
	if description != "" {
		return fmt.Sprintf("plan execution for %q using repo context and state", description)
	}
	if strings.TrimSpace(pack.WorkingDir) != "" {
		return fmt.Sprintf("plan execution for repo %s using structured task state", strings.TrimSpace(pack.WorkingDir))
	}
	return "plan execution using structured task state"
}

// openAIAgentsBackend is a minimal placeholder integration for the orchestration layer.
type openAIAgentsBackend struct{}

func (b openAIAgentsBackend) Name() string                      { return "openai_agents" }
func (b openAIAgentsBackend) Configure(map[string]string) error { return nil }
func (b openAIAgentsBackend) Describe(task Task, pack ContextPack) string {
	description := strings.TrimSpace(task.Description)
	if description != "" {
		return fmt.Sprintf("coordinate %q with tool calls and repo context", description)
	}
	return "coordinate tool calls with repo context"
}

// autogenBackend is a minimal placeholder integration for the orchestration layer.
type autogenBackend struct{}

func (b autogenBackend) Name() string                      { return "autogen" }
func (b autogenBackend) Configure(map[string]string) error { return nil }
func (b autogenBackend) Describe(task Task, pack ContextPack) string {
	description := strings.TrimSpace(task.Description)
	if description != "" {
		return fmt.Sprintf("delegate %q through a multi-agent plan", description)
	}
	return "delegate work through a multi-agent plan"
}

// pgvectorBackend is a minimal placeholder integration for retrieval.
type pgvectorBackend struct{}

func (b pgvectorBackend) Name() string                      { return "pgvector" }
func (b pgvectorBackend) Configure(map[string]string) error { return nil }
func (b pgvectorBackend) Describe(task Task, pack ContextPack) string {
	workingDir := strings.TrimSpace(pack.WorkingDir)
	if workingDir == "" {
		workingDir = "."
	}
	if strings.TrimSpace(task.Description) != "" {
		return fmt.Sprintf("retrieval backend for %q from %s", strings.TrimSpace(task.Description), workingDir)
	}
	return fmt.Sprintf("retrieval backend for %s", workingDir)
}

// qdrantBackend is a minimal placeholder integration for retrieval.
type qdrantBackend struct{}

func (b qdrantBackend) Name() string                      { return "qdrant" }
func (b qdrantBackend) Configure(map[string]string) error { return nil }
func (b qdrantBackend) Describe(task Task, pack ContextPack) string {
	workingDir := strings.TrimSpace(pack.WorkingDir)
	if workingDir == "" {
		workingDir = "."
	}
	if strings.TrimSpace(task.Description) != "" {
		return fmt.Sprintf("retrieval backend for %q from %s", strings.TrimSpace(task.Description), workingDir)
	}
	return fmt.Sprintf("retrieval backend for %s", workingDir)
}

// NewOrchestrationBackend selects an orchestration backend by name.
func NewOrchestrationBackend(name string, cfg map[string]string) (OrchestrationBackend, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "langgraph":
		return langgraphBackend{}, nil
	case "openai_agents", "openai-agents", "openaiagents":
		return openAIAgentsBackend{}, nil
	case "autogen":
		return autogenBackend{}, nil
	case "", "default":
		return langgraphBackend{}, nil
	default:
		return nil, fmt.Errorf("unsupported orchestration backend %q", name)
	}
}

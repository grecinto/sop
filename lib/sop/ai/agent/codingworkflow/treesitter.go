package codingworkflow

import (
	"context"
	"strings"

	ts "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

// TreeSitterParser wraps tree-sitter with language-specific parsing logic.
type TreeSitterParser struct {
	language string
}

// NewTreeSitterParser creates a parser for the specified language.
func NewTreeSitterParser(language string) *TreeSitterParser {
	return &TreeSitterParser{language: strings.ToLower(strings.TrimSpace(language))}
}

// ParseSymbols extracts function and type declarations from source code.
func (p *TreeSitterParser) ParseSymbols(content string) ([]string, error) {
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}

	parser := ts.NewParser()
	lang := p.getLanguage()
	if lang == nil {
		return p.fallbackParse(content), nil
	}

	parser.SetLanguage(lang)
	tree, err := parser.ParseCtx(context.Background(), nil, []byte(content))
	if err != nil || tree == nil {
		return p.fallbackParse(content), nil
	}

	root := tree.RootNode()
	if root == nil {
		return p.fallbackParse(content), nil
	}

	symbols := []string{}
	var walk func(node *ts.Node)
	walk = func(node *ts.Node) {
		if node == nil {
			return
		}

		nodeType := node.Type()
		if p.isSymbolNode(nodeType) {
			if name := p.extractSymbolName(node, []byte(content)); name != "" && !contains(symbols, name) {
				symbols = append(symbols, name)
			}
		}

		for i := 0; i < int(node.NamedChildCount()); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(root)

	return symbols, nil
}

// ParseStructure extracts a hierarchical structure summary from source code.
func (p *TreeSitterParser) ParseStructure(content string) (string, error) {
	symbols, err := p.ParseSymbols(content)
	if err != nil {
		return "", err
	}
	if len(symbols) == 0 {
		return "", nil
	}
	return strings.Join(symbols, ", "), nil
}

func (p *TreeSitterParser) getLanguage() *ts.Language {
	switch p.language {
	case "go", "golang":
		return golang.GetLanguage()
	default:
		return nil
	}
}

func (p *TreeSitterParser) isSymbolNode(nodeType string) bool {
	switch p.language {
	case "go", "golang":
		return nodeType == "function_declaration" || nodeType == "method_declaration" || nodeType == "type_declaration"
	case "python", "py":
		return nodeType == "function_definition" || nodeType == "class_definition"
	case "javascript", "js", "typescript", "ts":
		return nodeType == "function_declaration" || nodeType == "class_declaration" || nodeType == "method_definition"
	default:
		return false
	}
}

func (p *TreeSitterParser) extractSymbolName(node *ts.Node, source []byte) string {
	if node == nil {
		return ""
	}

	for _, fieldName := range []string{"name", "identifier"} {
		if child := node.ChildByFieldName(fieldName); child != nil {
			name := strings.TrimSpace(child.Content(source))
			if name != "" {
				return name
			}
		}
	}

	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		if child.Type() == "identifier" || child.Type() == "field_identifier" {
			name := strings.TrimSpace(child.Content(source))
			if name != "" {
				return name
			}
		}
	}

	return ""
}

func (p *TreeSitterParser) fallbackParse(content string) []string {
	var symbols []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		switch p.language {
		case "go", "golang":
			if strings.HasPrefix(trimmed, "func ") || strings.HasPrefix(trimmed, "type ") {
				if name := p.extractGoSymbol(trimmed); name != "" && !contains(symbols, name) {
					symbols = append(symbols, name)
				}
			}
		case "python", "py":
			if strings.HasPrefix(trimmed, "def ") || strings.HasPrefix(trimmed, "class ") {
				if name := p.extractPythonSymbol(trimmed); name != "" && !contains(symbols, name) {
					symbols = append(symbols, name)
				}
			}
		case "javascript", "js", "typescript", "ts":
			if strings.Contains(trimmed, "function ") || strings.HasPrefix(trimmed, "class ") {
				if name := p.extractJSSymbol(trimmed); name != "" && !contains(symbols, name) {
					symbols = append(symbols, name)
				}
			}
		}
	}
	return symbols
}

func (p *TreeSitterParser) extractGoSymbol(line string) string {
	parts := strings.Fields(line)
	if len(parts) > 1 {
		name := strings.TrimSuffix(parts[1], "{")
		name = strings.TrimSuffix(name, "(")
		name = strings.TrimSuffix(name, " struct")
		name = strings.TrimSuffix(name, " interface")
		return name
	}
	return ""
}

func (p *TreeSitterParser) extractPythonSymbol(line string) string {
	parts := strings.Fields(line)
	if len(parts) > 1 {
		name := parts[1]
		if idx := strings.Index(name, "("); idx > 0 {
			name = name[:idx]
		}
		if idx := strings.Index(name, ":"); idx > 0 {
			name = name[:idx]
		}
		return name
	}
	return ""
}

func (p *TreeSitterParser) extractJSSymbol(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "class ") {
		parts := strings.Fields(line)
		if len(parts) > 1 {
			name := parts[1]
			if idx := strings.Index(name, "{"); idx > 0 {
				name = name[:idx]
			}
			return name
		}
	}
	if strings.Contains(line, "function ") {
		idx := strings.Index(line, "function ")
		remainder := line[idx+len("function "):]
		parts := strings.Fields(remainder)
		if len(parts) > 0 {
			name := parts[0]
			if idx := strings.Index(name, "("); idx > 0 {
				name = name[:idx]
			}
			return name
		}
	}
	return ""
}

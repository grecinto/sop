package codingworkflow

import (
	"path/filepath"
	"strings"

	ts "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/csharp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// resolveTreeSitterLanguageForPath resolves a tree-sitter language and canonical
// language alias from a file path.
func resolveTreeSitterLanguageForPath(filePath string) (*ts.Language, string) {
	return resolveTreeSitterLanguageForExtension(filepath.Ext(filePath))
}

// resolveTreeSitterLanguageAliasFromExtension returns only the canonical
// language alias for a file extension.
func resolveTreeSitterLanguageAliasFromExtension(ext string) string {
	_, alias := resolveTreeSitterLanguageForExtension(ext)
	return alias
}

// resolveTreeSitterLanguageForAlias resolves a tree-sitter language from a
// language alias (for example "go", "python", "typescript").
func resolveTreeSitterLanguageForAlias(alias string) (*ts.Language, string) {
	normalized := strings.ToLower(strings.TrimSpace(alias))
	if strings.HasPrefix(normalized, ".") {
		return resolveTreeSitterLanguageForExtension(normalized)
	}

	switch normalized {
	case "go", "golang":
		return golang.GetLanguage(), "go"
	case "python", "py":
		return python.GetLanguage(), "python"
	case "javascript", "js":
		return javascript.GetLanguage(), "javascript"
	case "typescript", "ts":
		return typescript.GetLanguage(), "typescript"
	case "java":
		return java.GetLanguage(), "java"
	case "rust", "rs":
		return rust.GetLanguage(), "rust"
	case "csharp", "c#", "cs":
		return csharp.GetLanguage(), "csharp"
	case "html", "htm":
		return nil, "html"
	case "css", "scss", "sass", "less":
		return nil, "css"
	default:
		return nil, ""
	}
}

// resolveTreeSitterLanguageForExtension resolves a tree-sitter language and
// canonical language alias from a file extension.
func resolveTreeSitterLanguageForExtension(ext string) (*ts.Language, string) {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".go":
		return golang.GetLanguage(), "go"
	case ".py":
		return python.GetLanguage(), "python"
	case ".js", ".jsx", ".mjs", ".cjs":
		return javascript.GetLanguage(), "javascript"
	case ".ts", ".tsx", ".mts", ".cts":
		return typescript.GetLanguage(), "typescript"
	case ".java":
		return java.GetLanguage(), "java"
	case ".rs":
		return rust.GetLanguage(), "rust"
	case ".cs":
		return csharp.GetLanguage(), "csharp"
	case ".html", ".htm":
		return nil, "html"
	case ".css", ".scss", ".sass", ".less":
		return nil, "css"
	default:
		return nil, ""
	}
}

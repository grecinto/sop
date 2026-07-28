package codingworkflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ts "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

// CodeArtifactExtractor extracts code artifacts from modified files using AST analysis.
type CodeArtifactExtractor struct {
	grammarRegistry *GrammarRegistry
}

// NewCodeArtifactExtractor creates a new code artifact extractor.
func NewCodeArtifactExtractor(grammarRegistry *GrammarRegistry) *CodeArtifactExtractor {
	return &CodeArtifactExtractor{
		grammarRegistry: grammarRegistry,
	}
}

// ExtractFromModifiedFiles extracts code artifacts from a list of modified files.
func (cae *CodeArtifactExtractor) ExtractFromModifiedFiles(files []ModifiedFileRecord, workingDir string) ([]CodeArtifact, error) {
	artifacts := []CodeArtifact{}

	for _, file := range files {
		fullPath := filepath.Join(workingDir, file.FilePath)

		// Read file content
		content, err := os.ReadFile(fullPath)
		if err != nil {
			// File might not exist (deleted or created), skip
			continue
		}

		// Extract artifact via AST
		artifact, err := cae.extractFromFile(file.FilePath, content)
		if err != nil {
			// Fallback to path-based inference
			artifact = cae.inferFromPath(file.FilePath)
		}

		if artifact != nil {
			artifacts = append(artifacts, *artifact)
		}
	}

	return artifacts, nil
}

// extractFromFile extracts code artifact from a single file using AST.
func (cae *CodeArtifactExtractor) extractFromFile(filePath string, content []byte) (*CodeArtifact, error) {
	// Determine language from file extension
	lang := cae.getLanguageFromPath(filePath)
	if lang == nil {
		return nil, fmt.Errorf("unsupported file type: %s", filePath)
	}

	// Parse with tree-sitter
	parser := ts.NewParser()
	parser.SetLanguage(lang)

	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return nil, fmt.Errorf("parse failed: %w", err)
	}
	defer tree.Close()

	root := tree.RootNode()

	artifact := &CodeArtifact{
		File:      filePath,
		Functions: []string{},
		Types:     []string{},
	}

	// Extract package/namespace
	artifact.Package = cae.extractPackageName(root, content)
	artifact.Namespace = cae.inferNamespace(filePath)

	// Extract functions/methods
	artifact.Functions = cae.extractFunctions(root, content)

	// Extract types (structs, interfaces, classes)
	artifact.Types = cae.extractTypes(root, content)

	return artifact, nil
}

// getLanguageFromPath returns the tree-sitter language for a file path.
func (cae *CodeArtifactExtractor) getLanguageFromPath(filePath string) *ts.Language {
	ext := filepath.Ext(filePath)

	switch ext {
	case ".go":
		return golang.GetLanguage()
	// TODO: Add more languages
	// case ".js", ".jsx":
	//     return javascript.GetLanguage()
	// case ".ts", ".tsx":
	//     return typescript.GetLanguage()
	// case ".py":
	//     return python.GetLanguage()
	default:
		return nil
	}
}

// extractPackageName extracts the package name from AST.
func (cae *CodeArtifactExtractor) extractPackageName(root *ts.Node, content []byte) string {
	// For Go: look for package declaration
	query := `(package_clause (package_identifier) @package)`

	q, err := ts.NewQuery([]byte(query), golang.GetLanguage())
	if err != nil {
		return ""
	}
	defer q.Close()

	cursor := ts.NewQueryCursor()
	defer cursor.Close()
	cursor.Exec(q, root)

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		for _, capture := range match.Captures {
			return capture.Node.Content(content)
		}
	}

	return ""
}

// extractFunctions extracts function/method names from AST.
func (cae *CodeArtifactExtractor) extractFunctions(root *ts.Node, content []byte) []string {
	functions := []string{}

	// For Go: look for function declarations
	query := `(function_declaration name: (identifier) @function)`

	q, err := ts.NewQuery([]byte(query), golang.GetLanguage())
	if err != nil {
		return functions
	}
	defer q.Close()

	cursor := ts.NewQueryCursor()
	defer cursor.Close()
	cursor.Exec(q, root)

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		for _, capture := range match.Captures {
			name := capture.Node.Content(content)
			functions = append(functions, name)
		}
	}

	// Also look for method declarations
	methodQuery := `(method_declaration name: (field_identifier) @method)`

	mq, err := ts.NewQuery([]byte(methodQuery), golang.GetLanguage())
	if err != nil {
		return functions
	}
	defer mq.Close()

	cursor.Exec(mq, root)

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		for _, capture := range match.Captures {
			name := capture.Node.Content(content)
			functions = append(functions, name)
		}
	}

	return functions
}

// extractTypes extracts type names (structs, interfaces) from AST.
func (cae *CodeArtifactExtractor) extractTypes(root *ts.Node, content []byte) []string {
	types := []string{}

	// For Go: look for type declarations
	queries := []string{
		`(type_declaration (type_spec name: (type_identifier) @type))`,
	}

	for _, queryStr := range queries {
		q, err := ts.NewQuery([]byte(queryStr), golang.GetLanguage())
		if err != nil {
			continue
		}

		cursor := ts.NewQueryCursor()
		cursor.Exec(q, root)

		for {
			match, ok := cursor.NextMatch()
			if !ok {
				break
			}

			for _, capture := range match.Captures {
				name := capture.Node.Content(content)
				types = append(types, name)
			}
		}

		cursor.Close()
		q.Close()
	}

	return types
}

// inferFromPath creates a basic artifact from file path (fallback).
func (cae *CodeArtifactExtractor) inferFromPath(filePath string) *CodeArtifact {
	parts := strings.Split(filePath, "/")

	artifact := &CodeArtifact{
		File:      filePath,
		Functions: []string{},
		Types:     []string{},
	}

	// Infer package from first directory
	if len(parts) > 1 {
		artifact.Package = parts[0]
	}

	artifact.Namespace = cae.inferNamespace(filePath)

	return artifact
}

// inferNamespace infers namespace from file path.
func (cae *CodeArtifactExtractor) inferNamespace(filePath string) string {
	// For Go: extract from path
	// Example: "lib/sop/ai/agent/file.go" -> "github.com/repo/lib/sop/ai/agent"

	parts := strings.Split(filePath, "/")
	if len(parts) == 0 {
		return ""
	}

	// Remove filename
	parts = parts[:len(parts)-1]

	// TODO: Could look at go.mod to determine full module path
	// For now, just use the relative path
	return strings.Join(parts, "/")
}

// GenerateCategoryPath generates a Space category path from code artifacts.
func (cae *CodeArtifactExtractor) GenerateCategoryPath(artifacts []CodeArtifact) []string {
	if len(artifacts) == 0 {
		return []string{"Uncategorized"}
	}

	// Infer application component from package patterns
	component := cae.inferApplicationComponent(artifacts)
	subComponent := cae.inferSubComponent(artifacts)

	if subComponent != "" {
		return []string{component, subComponent}
	}
	return []string{component}
}

// inferApplicationComponent infers the application component from artifacts.
func (cae *CodeArtifactExtractor) inferApplicationComponent(artifacts []CodeArtifact) string {
	// Collect all package names
	packages := []string{}
	for _, a := range artifacts {
		if a.Package != "" {
			packages = append(packages, a.Package)
		}
	}

	if len(packages) == 0 {
		return "Core System"
	}

	// Use first package as primary indicator
	pkg := packages[0]

	// Map common package patterns to components
	componentMappings := map[string]string{
		"auth":       "Authentication System",
		"middleware": "Authentication System",
		"db":         "Database Layer",
		"database":   "Database Layer",
		"parser":     "Parser Engine",
		"tokenizer":  "Parser Engine",
		"ast":        "Parser Engine",
		"api":        "API Routes",
		"handler":    "API Routes",
		"route":      "API Routes",
		"cache":      "Caching Layer",
		"config":     "Configuration",
		"test":       "Testing Infrastructure",
		"util":       "Utilities",
		"helper":     "Utilities",
	}

	// Check for pattern matches
	pkgLower := strings.ToLower(pkg)
	for pattern, component := range componentMappings {
		if strings.Contains(pkgLower, pattern) {
			return component
		}
	}

	// Default: use package name as component
	return titleCasePackage(pkg)
}

// inferSubComponent infers the sub-component from artifacts.
func (cae *CodeArtifactExtractor) inferSubComponent(artifacts []CodeArtifact) string {
	if len(artifacts) == 0 {
		return ""
	}

	// For now, use package name if available
	if artifacts[0].Package != "" {
		pkg := artifacts[0].Package

		// Sub-component mappings
		subMappings := map[string]string{
			"middleware": "Middleware",
			"service":    "Service",
			"handler":    "Handlers",
			"route":      "Routes",
			"model":      "Models",
			"controller": "Controllers",
			"tokenizer":  "Tokenizer",
			"pool":       "Connection Pool",
			"query":      "Query Builder",
		}

		pkgLower := strings.ToLower(pkg)
		for pattern, sub := range subMappings {
			if strings.Contains(pkgLower, pattern) {
				return sub
			}
		}
	}

	return ""
}

// titleCasePackage converts a package name to title case.
func titleCasePackage(pkg string) string {
	words := strings.Split(pkg, "_")
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
		}
	}
	result := strings.Join(words, " ")
	return result + " Component"
}

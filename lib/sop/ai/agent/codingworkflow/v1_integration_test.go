package codingworkflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTreeSitterParserExtractsGoSymbols(t *testing.T) {
	content := `package demo

func Hello() string {
	return "hello"
}

type User struct {
	Name string
}

func (u *User) Greet() string {
	return "hi"
}
`
	parser := NewTreeSitterParser("go")
	symbols, err := parser.ParseSymbols(content)
	if err != nil {
		t.Fatalf("ParseSymbols error: %v", err)
	}

	if len(symbols) == 0 {
		t.Fatal("expected symbols to be extracted")
	}

	if !contains(symbols, "Hello") {
		t.Fatalf("expected Hello symbol, got %v", symbols)
	}
}

func TestTreeSitterParserFallsBackForPython(t *testing.T) {
	content := `def hello():
    return "hello"

class User:
    def greet(self):
        return "hi"
`
	parser := NewTreeSitterParser("python")
	symbols, err := parser.ParseSymbols(content)
	if err != nil {
		t.Fatalf("ParseSymbols error: %v", err)
	}

	if len(symbols) == 0 {
		t.Fatal("expected fallback symbols to be extracted")
	}

	if !contains(symbols, "hello") {
		t.Fatalf("expected hello symbol, got %v", symbols)
	}

	if !contains(symbols, "User") {
		t.Fatalf("expected User symbol, got %v", symbols)
	}
}

func TestTreeSitterParserHandlesJavaScript(t *testing.T) {
	content := `function hello() {
	return "hello";
}

class User {
	greet() {
		return "hi";
	}
}
`
	parser := NewTreeSitterParser("javascript")
	symbols, err := parser.ParseSymbols(content)
	if err != nil {
		t.Fatalf("ParseSymbols error: %v", err)
	}

	if len(symbols) == 0 {
		t.Fatal("expected fallback symbols to be extracted")
	}

	if !contains(symbols, "hello") {
		t.Fatalf("expected hello symbol, got %v", symbols)
	}

	if !contains(symbols, "User") {
		t.Fatalf("expected User symbol, got %v", symbols)
	}
}

func TestFileSearchAdapterFindsRelevantFiles(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "parser.go"), []byte("package demo\nfunc Parse() {}\n"), 0o644); err != nil {
		t.Fatalf("write parser.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "helper.go"), []byte("package demo\nfunc Help() {}\n"), 0o644); err != nil {
		t.Fatalf("write helper.go: %v", err)
	}

	adapter := &FileSearchAdapter{}
	matches, err := adapter.Search(Task{WorkingDir: tmpDir}, RunState{}, "Parse")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	if len(matches) == 0 {
		t.Fatal("expected search to return matches")
	}

	if !contains(matches, "parser.go") {
		t.Fatalf("expected parser.go in matches, got %v", matches)
	}
}

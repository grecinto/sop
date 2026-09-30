package sopbackend

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbedderOptionsTemplateUsesCatalogEntriesWithoutPlaceholder(t *testing.T) {
	templateFile := templatePath("options.html")
	fsys := os.DirFS(filepath.Dir(templateFile))
	tmpl, err := template.ParseFS(fsys, filepath.Base(templateFile))
	if err != nil {
		t.Fatalf("parse embedder options template: %v", err)
	}

	data := map[string]any{
		"ModelCatalog": ModelCatalog{
			Embedder: []ModelCatalogGroup{{
				Label: "Local",
				Options: []ModelCatalogOption{{
					Value: "kelindar:nomic-embed-text-v1.5-q8_0",
					Label: "Embedded library (Nomic Embed Text v1.5 (Q8))",
				}},
			}},
		},
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "embedderOptions", data); err != nil {
		t.Fatalf("execute embedder options template: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "Same as Brain / Default") {
		t.Fatalf("expected template to omit the placeholder default option, got %q", out)
	}
	if !strings.Contains(out, `value="kelindar:nomic-embed-text-v1.5-q8_0"`) {
		t.Fatalf("expected template to include the catalog embedder entry, got %q", out)
	}
}

func TestAIWindowEmbedderToggleHidesButtonForEmbeddedLocalEmbedders(t *testing.T) {
	templateFile := templatePath("scripts_part11.html")
	fsys := os.DirFS(filepath.Dir(templateFile))
	tmpl, err := template.ParseFS(fsys, filepath.Base(templateFile))
	if err != nil {
		t.Fatalf("parse setup wizard script template: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "scripts_part11", map[string]any{}); err != nil {
		t.Fatalf("execute setup wizard script template: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "const embedderTestButton = document.getElementById('chat-settings-embedder-test-button');") {
		t.Fatalf("expected AI window embedder toggle to look up the chat settings embedder test button, got content=%s", out)
	}
	if !strings.Contains(out, "if (embedderTestButton) embedderTestButton.style.display = isLocalEmbedder ? 'none' : 'inline-block';") {
		t.Fatalf("expected AI window embedder toggle to hide the embedder test button for embedded local embedders, got content=%s", out)
	}
}

func TestEmbedderOptionsTemplateRendersActualCatalog(t *testing.T) {
	templateFile := templatePath("options.html")
	fsys := os.DirFS(filepath.Dir(templateFile))
	tmpl, err := template.ParseFS(fsys, filepath.Base(templateFile))
	if err != nil {
		t.Fatalf("parse embedder options template: %v", err)
	}

	// Load the actual model catalog
	if _, err := LoadModelCatalog(""); err != nil {
		t.Fatalf("load model catalog: %v", err)
	}
	catalog := GetModelCatalog()

	if len(catalog.Embedder) == 0 {
		t.Fatal("expected catalog to have embedder entries")
	}

	data := map[string]any{
		"ModelCatalog": catalog,
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "embedderOptions", data); err != nil {
		t.Fatalf("execute embedder options template: %v", err)
	}

	out := buf.String()
	t.Logf("Rendered template:\n%s", out)

	if strings.Contains(out, "Same as Brain / Default") {
		t.Fatal("template should not include the old placeholder option")
	}

	// Check that we have at least one real option
	hasOptions := false
	for _, group := range catalog.Embedder {
		for _, opt := range group.Options {
			if strings.Contains(out, `value="`+opt.Value+`"`) {
				hasOptions = true
				break
			}
		}
	}
	if !hasOptions {
		t.Fatal("template should render at least one catalog embedder option")
	}
}

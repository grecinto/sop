package sopbackend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func templatePath(parts ...string) string {
	return filepath.Join(append([]string{"..", "httpserver", "templates"}, parts...)...)
}

func TestMediaPreviewRendererIsDefinedInTemplate(t *testing.T) {
	path := templatePath("scripts_part01.html")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "window.getItemMediaPreviewHtml = function") {
		t.Fatalf("expected media preview helper to be defined in %s", path)
	}
	if !strings.Contains(content, "<img") || !strings.Contains(content, "<video") {
		t.Fatalf("expected media preview helper to render image and video markup in %s", path)
	}
}

func TestObjectValueFormRendererIsSharedAcrossTemplates(t *testing.T) {
	part03Path := templatePath("scripts_part03.html")
	part03Data, err := os.ReadFile(part03Path)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	part03Content := string(part03Data)
	if !strings.Contains(part03Content, "renderObjectValueFormHtml") {
		t.Fatalf("expected detail editor to reuse the shared object form renderer in %s", part03Path)
	}

	part04Path := templatePath("scripts_part04.html")
	part04Data, err := os.ReadFile(part04Path)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	part04Content := string(part04Data)
	if !strings.Contains(part04Content, "function renderObjectValueFormHtml") {
		t.Fatalf("expected shared object form renderer helper in %s", part04Path)
	}
}

func TestSpaceItemDisplayToggleIsInteractiveInTemplate(t *testing.T) {
	path := templatePath("scripts_part01.html")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "pointer-events: none;") {
		t.Fatalf("expected the icon toggle input to remain interactive in %s", path)
	}
	if !strings.Contains(content, `type="checkbox"`) {
		t.Fatalf("expected the icon toggle to render as a checkbox in %s", path)
	}
	if !strings.Contains(content, "accent-color: var(--link-color);") {
		t.Fatalf("expected the icon toggle to expose a checked-state style in %s", path)
	}
}

func TestEditModalUsesFilenameLabelAndPreviewContainer(t *testing.T) {
	path := templatePath("scripts_part01.html")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Filename:") {
		t.Fatalf("expected edit modal to use Filename label in %s", path)
	}
	if !strings.Contains(content, "preview-image") {
		t.Fatalf("expected edit modal preview container to exist in %s", path)
	}
}

func TestItemCardMarkupCarriesPreviewReferenceAttribute(t *testing.T) {
	path := templatePath("scripts_part01.html")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "data-preview-ref") {
		t.Fatalf("expected item cards to carry a dedicated preview reference attribute in %s", path)
	}
}

func TestReferencePickerExplainsPathFallbackInTemplate(t *testing.T) {
	path := templatePath("scripts_part01.html")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Paste the full path manually") {
		t.Fatalf("expected reference picker fallback guidance in %s", path)
	}
}

func TestSaveSpaceItemRequiresFullReferenceInTemplate(t *testing.T) {
	path := templatePath("scripts_part01.html")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Please enter the full reference/path") {
		t.Fatalf("expected save flow to require a full reference/path in %s", path)
	}
}

func TestAddSpaceItemModalUsesStretchableBodyAndReferenceMetadataToggle(t *testing.T) {
	modalsPath := templatePath("modals.html")
	modalsData, err := os.ReadFile(modalsPath)
	if err != nil {
		t.Fatalf("read modal template: %v", err)
	}
	modalsContent := string(modalsData)
	if !strings.Contains(modalsContent, "new-space-item-form-body") {
		t.Fatalf("expected add-space-item modal to expose a dedicated stretchable form body in %s", modalsPath)
	}
	if !strings.Contains(modalsContent, "new-space-item-cloud-metadata-section") {
		t.Fatalf("expected add-space-item modal to expose a cloud metadata section container in %s", modalsPath)
	}

	scriptsPath := templatePath("scripts_part01.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	scriptsContent := string(scriptsData)
	if !strings.Contains(scriptsContent, "toggleSpaceItemReferenceDetails") {
		t.Fatalf("expected add-space-item modal to toggle reference metadata visibility in %s", scriptsPath)
	}
}

func TestChatWidgetSupportsPinnedFloatingMode(t *testing.T) {
	modalsPath := templatePath("modals.html")
	modalsData, err := os.ReadFile(modalsPath)
	if err != nil {
		t.Fatalf("read chat modal template: %v", err)
	}
	modalsContent := string(modalsData)
	if !strings.Contains(modalsContent, "chat-runtime-controls") {
		t.Fatalf("expected chat runtime controls in %s", modalsPath)
	}
	if !strings.Contains(modalsContent, "chat-runtime-hq") {
		t.Fatalf("expected chat runtime toggle in %s", modalsPath)
	}

	scriptsPath := templatePath("scripts_part05.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read chat script template: %v", err)
	}
	scriptsContent := string(scriptsData)
	if !strings.Contains(scriptsContent, "setChatRuntimeMode") {
		t.Fatalf("expected chat runtime toggle handler in %s", scriptsPath)
	}
	if !strings.Contains(scriptsContent, "sop_chat_runtime") {
		t.Fatalf("expected chat runtime persistence in %s", scriptsPath)
	}
}

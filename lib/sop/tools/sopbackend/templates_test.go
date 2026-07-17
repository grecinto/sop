package sopbackend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func templatePath(parts ...string) string {
	return filepath.Join(append([]string{"templates"}, parts...)...)
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

func TestChatRuntimeSelectionKeepsFormatSelectorVisibleForLocalRuntime(t *testing.T) {
	scriptsPath := templatePath("scripts_part05.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read chat script template: %v", err)
	}
	scriptsContent := string(scriptsData)
	if strings.Contains(scriptsContent, "formatSelect.style.display = runtime === 'hq' ? 'inline-block' : 'none';") {
		t.Fatalf("expected local runtime to keep the format selector visible in %s", scriptsPath)
	}
}

func TestChatLocalCommandOutputUsesTwoColumnLayout(t *testing.T) {
	scriptsPath := templatePath("scripts_part05.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read chat script template: %v", err)
	}
	scriptsContent := string(scriptsData)
	if !strings.Contains(scriptsContent, "buildLocalCommandOutputHtml") {
		t.Fatalf("expected local command renderer to expose a two-column helper in %s", scriptsPath)
	}
	if !strings.Contains(scriptsContent, "chat-command-output-grid") {
		t.Fatalf("expected local command output to use a dedicated grid container in %s", scriptsPath)
	}
}

func TestChatComposerSupportsImageAttachments(t *testing.T) {
	modalPath := templatePath("modals.html")
	modalData, err := os.ReadFile(modalPath)
	if err != nil {
		t.Fatalf("read chat modal template: %v", err)
	}
	modalContent := string(modalData)
	if !strings.Contains(modalContent, "chat-attachment-input") {
		t.Fatalf("expected chat composer to expose an attachment input in %s", modalPath)
	}

	scriptsPath := templatePath("scripts_part05.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read chat script template: %v", err)
	}
	scriptsContent := string(scriptsData)
	if !strings.Contains(scriptsContent, "input_parts") {
		t.Fatalf("expected chat request builder to serialize image attachments as input_parts in %s", scriptsPath)
	}
	if !strings.Contains(scriptsContent, "FileReader") {
		t.Fatalf("expected chat composer to read selected image files in %s", scriptsPath)
	}
}

func TestChatComposerShowsAttachmentPreviewAndFeedback(t *testing.T) {
	modalPath := templatePath("modals.html")
	modalData, err := os.ReadFile(modalPath)
	if err != nil {
		t.Fatalf("read chat modal template: %v", err)
	}
	modalContent := string(modalData)
	if !strings.Contains(modalContent, "chat-attachment-preview") {
		t.Fatalf("expected chat composer preview container in %s", modalPath)
	}
	if !strings.Contains(modalContent, "chat-attachment-feedback") {
		t.Fatalf("expected chat composer feedback container in %s", modalPath)
	}

	scriptsPath := templatePath("scripts_part05.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read chat script template: %v", err)
	}
	scriptsContent := string(scriptsData)
	if !strings.Contains(scriptsContent, "renderChatAttachmentPreview") {
		t.Fatalf("expected attachment preview renderer in %s", scriptsPath)
	}
	if !strings.Contains(scriptsContent, "showChatAttachmentFeedback") {
		t.Fatalf("expected attachment feedback helper in %s", scriptsPath)
	}
}

func TestChatComposerUsesSubtleOverflowTriggerForVisionModels(t *testing.T) {
	modalPath := templatePath("modals.html")
	modalData, err := os.ReadFile(modalPath)
	if err != nil {
		t.Fatalf("read chat modal template: %v", err)
	}
	modalContent := string(modalData)
	if !strings.Contains(modalContent, "chat-attachment-more-btn") {
		t.Fatalf("expected chat composer overflow trigger in %s", modalPath)
	}
	if !strings.Contains(modalContent, "chat-attachment-controls") {
		t.Fatalf("expected chat composer attachment controls container in %s", modalPath)
	}

	scriptsPath := templatePath("scripts_part05.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read chat script template: %v", err)
	}
	scriptsContent := string(scriptsData)
	if !strings.Contains(scriptsContent, "toggleChatAttachmentOverflow") {
		t.Fatalf("expected chat overflow toggle handler in %s", scriptsPath)
	}
	if !strings.Contains(scriptsContent, "shouldShowVisionAttachmentControl") {
		t.Fatalf("expected visual attachment capability check in %s", scriptsPath)
	}
}

func TestRefreshSpacesListHidesDomainSelectWhenEmpty(t *testing.T) {
	scriptsPath := templatePath("scripts_part01.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read dashboard script template: %v", err)
	}
	scriptsContent := string(scriptsData)
	if !strings.Contains(scriptsContent, "if (!kbs || kbs.length === 0) {") {
		t.Fatalf("expected refreshSpacesList to include the empty-space branch in %s", scriptsPath)
	}
	if !strings.Contains(scriptsContent, "domainSelect.style.display = 'none';") {
		t.Fatalf("expected refreshSpacesList to hide the domain select when no spaces are available in %s", scriptsPath)
	}
}

func TestChatRuntimeSwitchHandlesLocalDaemonFallbackInTemplate(t *testing.T) {
	scriptsPath := templatePath("scripts_part05.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read chat script template: %v", err)
	}
	scriptsContent := string(scriptsData)
	if !strings.Contains(scriptsContent, "handleRuntimeSwitchReloadError") {
		t.Fatalf("expected runtime switch to use a resilient reload-error handler in %s", scriptsPath)
	}
	if !strings.Contains(scriptsContent, "Cannot connect to local daemon") {
		t.Fatalf("expected runtime switch to handle local daemon connection failures gracefully in %s", scriptsPath)
	}
}

func TestChatRuntimeWelcomeContextRequestsHQContextForHQRuntime(t *testing.T) {
	scriptsPath := templatePath("scripts_part05.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read chat script template: %v", err)
	}
	scriptsContent := string(scriptsData)
	if strings.Contains(scriptsContent, "if (runtimeMode !== 'local') {\n                return null;\n            }") {
		t.Fatalf("expected HQ runtime welcome-context flow to avoid returning early before the HQ request in %s", scriptsPath)
	}
	if !strings.Contains(scriptsContent, "const response = await authFetch('/api/ai/test-connection'") {
		t.Fatalf("expected HQ runtime welcome-context flow to call the HQ test-connection endpoint in %s", scriptsPath)
	}
}

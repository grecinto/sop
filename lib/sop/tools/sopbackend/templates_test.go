package sopbackend

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func templatePath(parts ...string) string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join(append([]string{"templates"}, parts...)...)
	}
	packageDir := filepath.Dir(filename)
	return filepath.Join(append([]string{packageDir, "templates"}, parts...)...)
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

func TestAddSpaceItemModalExplainsDocumentBackedWorkflow(t *testing.T) {
	modalsPath := templatePath("modals.html")
	modalsData, err := os.ReadFile(modalsPath)
	if err != nil {
		t.Fatalf("read modal template: %v", err)
	}
	modalsContent := string(modalsData)
	for _, token := range []string{"new-space-item-create-category-btn", "document-backed item"} {
		if !strings.Contains(modalsContent, token) {
			t.Fatalf("expected add-space-item modal to expose %s in %s", token, modalsPath)
		}
	}
}

func TestAddSpaceItemModalSupportsWorkflowTemplateAuthoring(t *testing.T) {
	modalsPath := templatePath("modals.html")
	modalsData, err := os.ReadFile(modalsPath)
	if err != nil {
		t.Fatalf("read modal template: %v", err)
	}
	modalsContent := string(modalsData)
	for _, token := range []string{"new-space-item-mode", "new-space-item-workflow-section", "new-space-item-workflow-name", "new-space-item-workflow-steps", "new-space-item-workflow-description", "new-space-item-workflow-protocol-guidance", "new-space-item-workflow-step-metadata"} {
		if !strings.Contains(modalsContent, token) {
			t.Fatalf("expected add-space-item modal to expose %s in %s", token, modalsPath)
		}
	}
}

func TestAddCategoryModalExposesWorkflowToggle(t *testing.T) {
	modalsPath := templatePath("modals.html")
	modalsData, err := os.ReadFile(modalsPath)
	if err != nil {
		t.Fatalf("read modal template: %v", err)
	}
	modalsContent := string(modalsData)
	if !strings.Contains(modalsContent, "new-category-workflow-enabled") {
		t.Fatalf("expected add-category modal to expose workflow toggle in %s", modalsPath)
	}
}

func TestSpaceItemScriptsGateWorkflowModeByCategoryFlag(t *testing.T) {
	scriptsPath := templatePath("scripts_part01.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	scriptsContent := string(scriptsData)
	for _, token := range []string{"getSelectedCategoryWorkflowEnabled", "workflowOption"} {
		if !strings.Contains(scriptsContent, token) {
			t.Fatalf("expected workflow mode helper %s in %s", token, scriptsPath)
		}
	}
}

func TestSpaceItemScriptsRenderWorkflowItemsAsTreeLeaves(t *testing.T) {
	scriptsPath := templatePath("scripts_part01.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	scriptsContent := string(scriptsData)
	for _, token := range []string{"renderWorkflowItemTreeNode", "loadWorkflowItemsForCategoryTree"} {
		if !strings.Contains(scriptsContent, token) {
			t.Fatalf("expected workflow tree helper %s in %s", token, scriptsPath)
		}
	}
}

func TestSpaceItemScriptsSerializeWorkflowStepMetadata(t *testing.T) {
	scriptsPath := templatePath("scripts_part01.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	scriptsContent := string(scriptsData)
	for _, token := range []string{"parseWorkflowStepMetadata", "workflowProtocolGuidance", "workflowMetadataRaw"} {
		if !strings.Contains(scriptsContent, token) {
			t.Fatalf("expected workflow authoring helper %s in %s", token, scriptsPath)
		}
	}
}

func TestSpaceItemScriptsExposeWorkflowPreviewHelpers(t *testing.T) {
	scriptsPath := templatePath("scripts_part01.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	scriptsContent := string(scriptsData)
	for _, token := range []string{"toggleSpaceItemMode", "renderWorkflowDefinitionPreview"} {
		if !strings.Contains(scriptsContent, token) {
			t.Fatalf("expected workflow authoring helper %s in %s", token, scriptsPath)
		}
	}
}

func TestSpaceDocumentRootNodeStaysVisibleAsBuiltInDocumentView(t *testing.T) {
	scriptsPath := templatePath("scripts_part01.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	scriptsContent := string(scriptsData)
	for _, token := range []string{"rootDocumentsContainer.style.display = 'block'", "showSpaceDocumentsRootIfNeeded"} {
		if !strings.Contains(scriptsContent, token) {
			t.Fatalf("expected document tree helper %s in %s", token, scriptsPath)
		}
	}
}

func TestRenderTreeNodesOnlyAddsDocumentsRootAtTopLevel(t *testing.T) {
	scriptsPath := templatePath("scripts_part01.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	scriptsContent := string(scriptsData)
	guardSnippet := "function renderTreeNodes(catList, container, depth=0) {\n            if (depth === 0) {"
	if !strings.Contains(scriptsContent, guardSnippet) {
		t.Fatalf("expected top-level Documents root guard in %s", scriptsPath)
	}
}

func TestSpaceItemScriptsExposeDocumentTreeHelpers(t *testing.T) {
	scriptsPath := templatePath("scripts_part01.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	scriptsContent := string(scriptsData)
	for _, token := range []string{"renderDocumentTreeNode", "loadDocumentsForCategoryTree", "renderSpaceDocumentsRootNode"} {
		if !strings.Contains(scriptsContent, token) {
			t.Fatalf("expected document tree helper %s in %s", token, scriptsPath)
		}
	}
}

func TestSpaceItemScriptsExposeDocumentGalleryHelpers(t *testing.T) {
	scriptsPath := templatePath("scripts_part01.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	scriptsContent := string(scriptsData)
	for _, token := range []string{"renderDocumentCardHtml", "openDocumentDetailView", "fetchDocumentsForCategory"} {
		if !strings.Contains(scriptsContent, token) {
			t.Fatalf("expected document gallery helper %s in %s", token, scriptsPath)
		}
	}
}

func TestSpaceDocumentDeleteActionIsDefinedInTemplate(t *testing.T) {
	scriptsPath := templatePath("scripts_part01.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	scriptsContent := string(scriptsData)
	for _, token := range []string{"deleteSpaceDocument", "Delete Document"} {
		if !strings.Contains(scriptsContent, token) {
			t.Fatalf("expected document delete helper %s in %s", token, scriptsPath)
		}
	}
}

func TestSpaceItemCardRendererSupportsWorkflowStepCards(t *testing.T) {
	scriptsPath := templatePath("scripts_part01.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	scriptsContent := string(scriptsData)
	for _, token := range []string{"workflow-step-card", "workflow-card", "renderWorkflowCardHtml"} {
		if !strings.Contains(scriptsContent, token) {
			t.Fatalf("expected workflow card renderer token %s in %s", token, scriptsPath)
		}
	}
}

func TestWorkflowStudioModalAndRendererAreDefinedInTemplates(t *testing.T) {
	modalsPath := templatePath("modals.html")
	modalsData, err := os.ReadFile(modalsPath)
	if err != nil {
		t.Fatalf("read modal template: %v", err)
	}
	modalsContent := string(modalsData)
	if !strings.Contains(modalsContent, "workflow-studio-modal") {
		t.Fatalf("expected workflow studio modal container in %s", modalsPath)
	}

	scriptsPath := templatePath("scripts_part01.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	scriptsContent := string(scriptsData)
	for _, token := range []string{"renderWorkflowStudioModal", "openWorkflowStudioModal", "workflow-studio-open-btn"} {
		if !strings.Contains(scriptsContent, token) {
			t.Fatalf("expected workflow studio renderer token %s in %s", token, scriptsPath)
		}
	}
}

func TestWorkflowStudioEditorExposesEditableActions(t *testing.T) {
	scriptsPath := templatePath("scripts_part01.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	scriptsContent := string(scriptsData)
	for _, token := range []string{"workflow-studio-save-btn", "workflow-studio-add-step-btn", "workflow-studio-step-list"} {
		if !strings.Contains(scriptsContent, token) {
			t.Fatalf("expected workflow studio editor token %s in %s", token, scriptsPath)
		}
	}
}

func TestWorkflowStudioEditorExposesStepManagementControls(t *testing.T) {
	scriptsPath := templatePath("scripts_part01.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	scriptsContent := string(scriptsData)
	for _, token := range []string{"workflow-studio-step-number", "workflow-studio-delete-step-btn", "workflow-studio-move-step-up-btn", "workflow-studio-move-step-down-btn"} {
		if !strings.Contains(scriptsContent, token) {
			t.Fatalf("expected workflow studio step management token %s in %s", token, scriptsPath)
		}
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

func TestChatCommandPreviewUsesDedicatedStyledSpan(t *testing.T) {
	scriptsPath := templatePath("scripts_part05.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read chat script template: %v", err)
	}
	scriptsContent := string(scriptsData)
	if !strings.Contains(scriptsContent, "chat-command-preview") {
		t.Fatalf("expected chat command preview to use a dedicated styled span in %s", scriptsPath)
	}
	if !strings.Contains(scriptsContent, "activeChatCommandPreview") || !strings.Contains(scriptsContent, "clearActiveChatCommandPreview") {
		t.Fatalf("expected chat command preview to track the active preview element in %s", scriptsPath)
	}

	stylesPath := templatePath("styles.html")
	stylesData, err := os.ReadFile(stylesPath)
	if err != nil {
		t.Fatalf("read chat styles template: %v", err)
	}
	stylesContent := string(stylesData)
	if !strings.Contains(stylesContent, ".chat-command-preview") {
		t.Fatalf("expected chat command preview styling to be defined in %s", stylesPath)
	}
	if !strings.Contains(stylesContent, ".chat-command-preview.is-active::after") {
		t.Fatalf("expected only the active chat command preview to animate in %s", stylesPath)
	}
}

func TestSessionRefreshStoresRefreshedTokens(t *testing.T) {
	scriptsPath := templatePath("scripts_part01.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read chat script template: %v", err)
	}
	scriptsContent := string(scriptsData)
	if !strings.Contains(scriptsContent, "storeSessionTokens(body)") {
		t.Fatalf("expected session refresh to store refreshed tokens in %s", scriptsPath)
	}
}

func TestChatCommandPreviewCleanupTargetsDomElements(t *testing.T) {
	scriptsPath := templatePath("scripts_part05.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read chat script template: %v", err)
	}
	scriptsContent := string(scriptsData)
	if !strings.Contains(scriptsContent, "querySelectorAll('.chat-command-preview.is-active')") {
		t.Fatalf("expected chat command preview cleanup to target active DOM previews in %s", scriptsPath)
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

func TestChatRuntimeSwitchHandlesLocalRuntimeFallbackInTemplate(t *testing.T) {
	scriptsPath := templatePath("scripts_part05.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read chat script template: %v", err)
	}
	scriptsContent := string(scriptsData)
	if !strings.Contains(scriptsContent, "handleRuntimeSwitchReloadError") {
		t.Fatalf("expected runtime switch to use a resilient reload-error handler in %s", scriptsPath)
	}
	if !strings.Contains(scriptsContent, "Cannot connect to local runtime") {
		t.Fatalf("expected runtime switch to handle local runtime connection failures gracefully in %s", scriptsPath)
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

func TestSetupWizardUsesSharedSaveRouteWithoutProvisioningModeState(t *testing.T) {
	scriptsPath := templatePath("scripts_part10.html")
	scriptsData, err := os.ReadFile(scriptsPath)
	if err != nil {
		t.Fatalf("read setup script template: %v", err)
	}
	scriptsContent := string(scriptsData)

	for _, token := range []string{
		"function finishSetup()",
		"submitProvisioningRequest('/api/config/save', finalPayload",
		"function finishDaemonSystemSetup()",
	} {
		if !strings.Contains(scriptsContent, token) {
			t.Fatalf("expected setup wizard routing token %q in %s", token, scriptsPath)
		}
	}

	if strings.Contains(scriptsContent, "submitProvisioningRequest('/api/config/provision/simple'") {
		t.Fatalf("expected setup wizard to stop calling the removed simple-provision endpoint in %s", scriptsPath)
	}
	if strings.Contains(scriptsContent, "currentProvisioningMode") {
		t.Fatalf("expected setup wizard to stop carrying simple-vs-advanced provisioning mode state in %s", scriptsPath)
	}
}

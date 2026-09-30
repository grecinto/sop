package sopbackend

import (
	"os"
	"strings"
	"testing"
)

func TestSpaceQualityShellScriptUsesNativeDataManagerPattern(t *testing.T) {
	body, err := os.ReadFile(templatePath("scripts_kb_quality.html"))
	if err != nil {
		t.Fatalf("failed to read KB quality script template: %v", err)
	}
	if !strings.Contains(string(body), "function selectKBQuality(element)") {
		t.Fatalf("expected shell script to define selectKBQuality function")
	}
	if !strings.Contains(string(body), "renderKBQualityDashboard()") {
		t.Fatalf("expected shell script to render dashboard in the main pane")
	}
	if !strings.Contains(string(body), "data-grid") {
		t.Fatalf("expected shell script to use the data-grid element like other Data Manager views")
	}
}

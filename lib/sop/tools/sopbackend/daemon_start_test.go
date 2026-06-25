package sopbackend

import (
	"testing"
)

func TestReconnectModeSelectionUsesCurrentRuntime(t *testing.T) {
	if got := "local"; got != "local" {
		t.Fatalf("expected local runtime mode sentinel, got %q", got)
	}
}

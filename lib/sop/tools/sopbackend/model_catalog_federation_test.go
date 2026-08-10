package sopbackend

import (
	"testing"
	"time"
)

func TestModelCatalogFederationStateCanFederateWhenRevisionChanges(t *testing.T) {
	state := newModelCatalogFederationState()
	currentTime := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	state.recordCatalogSnapshot("rev-2", currentTime.Add(-30*time.Minute))
	state.markCatalogShared("rev-1", currentTime.Add(-2*time.Hour))

	if !state.canFederate("rev-2", currentTime) {
		t.Fatal("expected federation to be enabled when the current catalog revision is newer")
	}
}

func TestModelCatalogFederationStateBlocksFederationWhenRevisionIsNotNewer(t *testing.T) {
	state := newModelCatalogFederationState()
	currentTime := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	state.recordCatalogSnapshot("rev-1", currentTime.Add(-30*time.Minute))
	state.markCatalogShared("rev-1", currentTime.Add(-10*time.Minute))

	if state.canFederate("rev-1", currentTime) {
		t.Fatal("expected federation to stay disabled when the current catalog revision is not newer")
	}
}

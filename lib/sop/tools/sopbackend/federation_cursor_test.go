package sopbackend

import (
	"testing"
	"time"
)

func TestFederationCursorIncrementsAndTracks(t *testing.T) {
	cursor := &FederationCursor{
		currentVersion: 1,
		lastUpdate:     time.Now(),
		versionHash:    computeVersionHash(1),
	}

	initialHash := cursor.versionHash
	initialVersion := cursor.currentVersion

	// Increment and verify
	newHash := cursor.IncrementVersion()

	if newHash == initialHash {
		t.Errorf("expected hash to change after increment, got same hash: %s", newHash)
	}

	_, currentVersion, _ := cursor.GetCurrentCursor()
	if currentVersion != initialVersion+1 {
		t.Errorf("expected version %d, got %d", initialVersion+1, currentVersion)
	}
}

func TestFederationCursorDetectsDelta(t *testing.T) {
	cursor := &FederationCursor{
		currentVersion: 5,
		lastUpdate:     time.Now(),
		versionHash:    computeVersionHash(5),
	}

	// Empty cursor should indicate delta
	if !cursor.HasDeltaSince("") {
		t.Error("expected delta for empty cursor")
	}

	// Same cursor should not indicate delta
	currentHash, _, _ := cursor.GetCurrentCursor()
	if cursor.HasDeltaSince(currentHash) {
		t.Error("expected no delta for current cursor")
	}

	// Different cursor should indicate delta
	oldHash := computeVersionHash(3)
	if !cursor.HasDeltaSince(oldHash) {
		t.Error("expected delta for old cursor")
	}
}

func TestComputeVersionHashIsDeterministic(t *testing.T) {
	hash1 := computeVersionHash(42)
	hash2 := computeVersionHash(42)

	if hash1 != hash2 {
		t.Errorf("expected deterministic hash, got %s and %s", hash1, hash2)
	}

	hash3 := computeVersionHash(43)
	if hash1 == hash3 {
		t.Error("expected different hashes for different versions")
	}
}

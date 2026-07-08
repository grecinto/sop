package sopbackend

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// FederationCursor tracks the global federation state version.
type FederationCursor struct {
	mu             sync.RWMutex
	currentVersion int64
	lastUpdate     time.Time
	versionHash    string
}

var (
	globalFederationCursor     *FederationCursor
	globalFederationCursorOnce sync.Once
)

// GetGlobalFederationCursor returns the singleton federation cursor.
func GetGlobalFederationCursor() *FederationCursor {
	globalFederationCursorOnce.Do(func() {
		globalFederationCursor = &FederationCursor{
			currentVersion: 1,
			lastUpdate:     time.Now(),
			versionHash:    computeVersionHash(1),
		}
	})
	return globalFederationCursor
}

// IncrementVersion increments the federation cursor when new content is added.
func (fc *FederationCursor) IncrementVersion() string {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	fc.currentVersion++
	fc.lastUpdate = time.Now()
	fc.versionHash = computeVersionHash(fc.currentVersion)

	return fc.versionHash
}

// GetCurrentCursor returns the current cursor hash and version.
func (fc *FederationCursor) GetCurrentCursor() (hash string, version int64, lastUpdate time.Time) {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	return fc.versionHash, fc.currentVersion, fc.lastUpdate
}

// HasDeltaSince checks if there have been changes since the given cursor.
func (fc *FederationCursor) HasDeltaSince(clientCursor string) bool {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	// Empty cursor means client has never synced
	if clientCursor == "" {
		return true
	}

	// Different cursor means changes exist
	return clientCursor != fc.versionHash
}

// computeVersionHash creates a deterministic hash from the version number.
func computeVersionHash(version int64) string {
	data := fmt.Sprintf("federation-v%d", version)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:8]) // Use first 8 bytes for brevity
}

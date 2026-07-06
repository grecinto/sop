package sopbackend

import (
	"context"
	"fmt"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/database"
)

// DaemonRegistration represents a registered daemon in the fleet.
type DaemonRegistration struct {
	DaemonID     string            `json:"daemon_id"`
	DaemonURL    string            `json:"daemon_url"`    // e.g., http://daemon-001.internal:9090
	UserID       string            `json:"user_id"`       // Associated user
	LastSeenAt   time.Time         `json:"last_seen_at"`  // Updated on every sync
	FirstSeenAt  time.Time         `json:"first_seen_at"` // Initial registration time
	Status       string            `json:"status"`        // active, stale, offline (computed)
	Version      string            `json:"version"`       // Daemon version if reported
	Region       string            `json:"region"`        // Geographic region (optional)
	Capabilities []string          `json:"capabilities"`  // Feature flags
	Metadata     map[string]string `json:"metadata"`      // Additional key-value data
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

const (
	daemonRegistryStoreName = "system_daemon_registry"
	daemonStaleTTL          = 5 * time.Minute // Daemon not seen for 5min = stale
)

// GetDaemonRegistry returns the daemon registry (backed by system DB B-tree).
func GetDaemonRegistry() *DaemonRegistry {
	return &DaemonRegistry{staleTTL: daemonStaleTTL}
}

// DaemonRegistry provides access to daemon registrations stored in system DB.
type DaemonRegistry struct {
	staleTTL time.Duration
}

// Register adds or updates a daemon registration in the system DB.
func (r *DaemonRegistry) Register(daemonID, daemonURL, userID string) error {
	if daemonID == "" || daemonURL == "" {
		return nil // Silently ignore empty registrations
	}

	ctx := context.Background()
	db, err := getSystemDB(ctx)
	if err != nil {
		return fmt.Errorf("get system db: %w", err)
	}

	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	options := sop.ConfigureStore(daemonRegistryStoreName, true, 1000, "Daemon Registry", sop.SmallData, "")
	options.IsPrimitiveKey = true

	store, err := database.NewBtree[string, DaemonRegistration](ctx, db.Options(), daemonRegistryStoreName, tx, nil, options)
	if err != nil {
		return fmt.Errorf("open daemon registry store: %w", err)
	}

	now := time.Now().UTC()

	// Check if daemon already exists
	found, err := store.Find(ctx, daemonID, true)
	existingFound := (err == nil && found)
	var existing *DaemonRegistration
	if existingFound {
		val, err := store.GetCurrentValue(ctx)
		if err == nil {
			existing = &val
		}
	}

	reg := DaemonRegistration{
		DaemonID:   daemonID,
		DaemonURL:  daemonURL,
		UserID:     userID,
		LastSeenAt: now,
		UpdatedAt:  now,
		Status:     "active",
	}

	if existingFound {
		// Preserve initial registration time and metadata
		reg.FirstSeenAt = existing.FirstSeenAt
		reg.CreatedAt = existing.CreatedAt
		reg.Version = existing.Version
		reg.Region = existing.Region
		reg.Capabilities = existing.Capabilities
		if existing.Metadata != nil {
			reg.Metadata = existing.Metadata
		}
	} else {
		reg.FirstSeenAt = now
		reg.CreatedAt = now
		reg.Metadata = make(map[string]string)
	}

	if _, err := store.Upsert(ctx, daemonID, reg); err != nil {
		return fmt.Errorf("upsert daemon registration: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// Get retrieves a daemon registration by ID.
func (r *DaemonRegistry) Get(daemonID string) (*DaemonRegistration, bool) {
	if daemonID == "" {
		return nil, false
	}

	ctx := context.Background()
	db, err := getSystemDB(ctx)
	if err != nil {
		return nil, false
	}

	tx, err := db.BeginTransaction(ctx, sop.ForReading, -1)
	if err != nil {
		return nil, false
	}
	defer tx.Rollback(ctx)

	options := sop.ConfigureStore(daemonRegistryStoreName, true, 1000, "Daemon Registry", sop.SmallData, "")
	options.IsPrimitiveKey = true

	store, err := database.NewBtree[string, DaemonRegistration](ctx, db.Options(), daemonRegistryStoreName, tx, nil, options)
	if err != nil {
		return nil, false
	}

	found, err := store.Find(ctx, daemonID, true)
	if err != nil || !found {
		return nil, false
	}

	reg, err := store.GetCurrentValue(ctx)
	if err != nil {
		return nil, false
	}

	// Update status based on last seen time
	if time.Since(reg.LastSeenAt) > r.staleTTL {
		reg.Status = "stale"
	} else {
		reg.Status = "active"
	}

	return &reg, true
}

// GetURL returns the URL for a daemon by ID.
func (r *DaemonRegistry) GetURL(daemonID string) (string, bool) {
	reg, found := r.Get(daemonID)
	if !found {
		return "", false
	}
	return reg.DaemonURL, true
}

// List returns all registered daemons.
func (r *DaemonRegistry) List() []*DaemonRegistration {
	ctx := context.Background()
	db, err := getSystemDB(ctx)
	if err != nil {
		return nil
	}

	tx, err := db.BeginTransaction(ctx, sop.ForReading, -1)
	if err != nil {
		return nil
	}
	defer tx.Rollback(ctx)

	options := sop.ConfigureStore(daemonRegistryStoreName, true, 1000, "Daemon Registry", sop.SmallData, "")
	options.IsPrimitiveKey = true

	store, err := database.NewBtree[string, DaemonRegistration](ctx, db.Options(), daemonRegistryStoreName, tx, nil, options)
	if err != nil {
		return nil
	}

	ok, err := store.First(ctx)
	if err != nil || !ok {
		return nil
	}

	now := time.Now()
	results := make([]*DaemonRegistration, 0)

	for {
		value, err := store.GetCurrentValue(ctx)
		if err != nil {
			break
		}

		// Update status based on staleness
		if now.Sub(value.LastSeenAt) > r.staleTTL {
			value.Status = "stale"
		} else {
			value.Status = "active"
		}

		results = append(results, &value)

		if ok, err := store.Next(ctx); err != nil || !ok {
			break
		}
	}

	return results
}

// ListActive returns only active daemons (recently seen).
func (r *DaemonRegistry) ListActive() []*DaemonRegistration {
	all := r.List()
	active := make([]*DaemonRegistration, 0)

	for _, reg := range all {
		if reg.Status == "active" {
			active = append(active, reg)
		}
	}

	return active
}

// ListByUser returns all daemons for a specific user.
func (r *DaemonRegistry) ListByUser(userID string) []*DaemonRegistration {
	all := r.List()
	userDaemons := make([]*DaemonRegistration, 0)

	for _, reg := range all {
		if reg.UserID == userID {
			userDaemons = append(userDaemons, reg)
		}
	}

	return userDaemons
}

// Remove removes a daemon from the registry.
func (r *DaemonRegistry) Remove(daemonID string) error {
	if daemonID == "" {
		return nil
	}

	ctx := context.Background()
	db, err := getSystemDB(ctx)
	if err != nil {
		return fmt.Errorf("get system db: %w", err)
	}

	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	options := sop.ConfigureStore(daemonRegistryStoreName, true, 1000, "Daemon Registry", sop.SmallData, "")
	options.IsPrimitiveKey = true

	store, err := database.NewBtree[string, DaemonRegistration](ctx, db.Options(), daemonRegistryStoreName, tx, nil, options)
	if err != nil {
		return fmt.Errorf("open daemon registry store: %w", err)
	}

	if _, err := store.Remove(ctx, daemonID); err != nil {
		return fmt.Errorf("remove daemon: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// Cleanup removes stale daemons that haven't been seen for a long time.
func (r *DaemonRegistry) Cleanup(maxAge time.Duration) (int, error) {
	ctx := context.Background()
	db, err := getSystemDB(ctx)
	if err != nil {
		return 0, fmt.Errorf("get system db: %w", err)
	}

	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	options := sop.ConfigureStore(daemonRegistryStoreName, true, 1000, "Daemon Registry", sop.SmallData, "")
	options.IsPrimitiveKey = true

	store, err := database.NewBtree[string, DaemonRegistration](ctx, db.Options(), daemonRegistryStoreName, tx, nil, options)
	if err != nil {
		return 0, fmt.Errorf("open daemon registry store: %w", err)
	}

	ok, err := store.First(ctx)
	if err != nil || !ok {
		return 0, nil
	}

	removed := 0
	now := time.Now()
	toRemove := make([]string, 0)

	for {
		keyItem := store.GetCurrentKey()

		value, err := store.GetCurrentValue(ctx)
		if err != nil {
			break
		}

		if now.Sub(value.LastSeenAt) > maxAge {
			toRemove = append(toRemove, keyItem.Key)
		}

		if ok, err := store.Next(ctx); err != nil || !ok {
			break
		}
	}

	// Remove stale entries
	for _, key := range toRemove {
		if _, err := store.Remove(ctx, key); err == nil {
			removed++
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}

	return removed, nil
}

// Count returns the total number of registered daemons.
func (r *DaemonRegistry) Count() int {
	all := r.List()
	return len(all)
}

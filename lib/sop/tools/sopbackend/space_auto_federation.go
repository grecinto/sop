package sopbackend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	log "log/slog"
	"net/http"
	"strings"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/memory"
)

// autoShareSpacesToDaemon pushes all Spaces marked with AutoShareToDaemons=true to the specified daemon.
// This is called automatically when a daemon registers with HQ during sync.
func autoShareSpacesToDaemon(ctx context.Context, daemonID, daemonURL string) error {
	if daemonID == "" || daemonURL == "" {
		return nil
	}

	log.Info("Auto-sharing Spaces to daemon", "daemon_id", daemonID, "daemon_url", daemonURL)

	// Get list of all databases (typically "aidb")
	databases := []string{"aidb"} // TODO: Make this configurable or discover dynamically

	shareCount := 0
	for _, dbName := range databases {
		opts, err := GetDBOptions(ctx, dbName)
		if err != nil {
			log.Warn("Auto-share: failed to get DB options", "database", dbName, "error", err)
			continue
		}

		db := database.NewDatabase(opts)

		// List all Spaces in this database
		spaceNames, err := db.GetDomains(ctx)
		if err != nil {
			log.Warn("Auto-share: failed to list Spaces", "database", dbName, "error", err)
			continue
		}

		// Check each Space's config for AutoShareToDaemons flag
		for _, spaceName := range spaceNames {
			if shouldAutoShare, err := isSpaceAutoShared(ctx, dbName, spaceName); err == nil && shouldAutoShare {
				log.Info("Auto-sharing Space to daemon", "space_name", spaceName, "daemon_id", daemonID)

				// Export and push the Space
				spaceJSON, err := exportSpaceFromHQ(ctx, dbName, spaceName)
				if err != nil {
					log.Error("Auto-share: failed to export Space", "space_name", spaceName, "error", err)
					continue
				}

				if err := pushSpaceToDaemon(ctx, daemonURL, spaceName, dbName, spaceJSON); err != nil {
					log.Error("Auto-share: failed to push Space to daemon",
						"space_name", spaceName,
						"daemon_id", daemonID,
						"error", err,
					)
					updateSpaceFederationState(spaceName, daemonID, "failed", 0)
					continue
				}

				updateSpaceFederationState(spaceName, daemonID, "delivered", 0)
				shareCount++
				log.Info("Auto-share: successfully pushed Space",
					"space_name", spaceName,
					"daemon_id", daemonID,
				)
			}
		}
	}

	log.Info("Auto-share completed", "daemon_id", daemonID, "spaces_shared", shareCount)
	return nil
}

// isSpaceAutoShared checks if a Space has AutoShareToDaemons=true in its config.
func isSpaceAutoShared(ctx context.Context, dbName, spaceName string) (bool, error) {
	opts, err := GetDBOptions(ctx, dbName)
	if err != nil {
		return false, err
	}

	db := database.NewDatabase(opts)
	tx, err := db.BeginTransaction(ctx, sop.ForReading, -1)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	kb, err := db.OpenKnowledgeBase(ctx, spaceName, tx, nil, nil, false)
	if err != nil {
		return false, err
	}

	config, err := kb.GetConfig(ctx)
	if err != nil {
		return false, err
	}

	if config == nil {
		return false, nil
	}

	return config.AutoShareToDaemons, nil
}

// HandleSetSpaceAutoShare toggles the AutoShareToDaemons flag for a Space.
// POST /api/space/auto-share
// Body: {"space_name": "...", "database_name": "...", "enabled": true/false}
func HandleSetSpaceAutoShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req struct {
		SpaceName    string `json:"space_name"`
		DatabaseName string `json:"database_name"`
		Enabled      bool   `json:"enabled"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if req.SpaceName == "" || req.DatabaseName == "" {
		http.Error(w, "space_name and database_name are required", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	opts, err := GetDBOptions(ctx, req.DatabaseName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get DB config: %v", err), http.StatusInternalServerError)
		return
	}

	db := database.NewDatabase(opts)
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to begin transaction: %v", err), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	kb, err := db.OpenKnowledgeBase(ctx, req.SpaceName, tx, nil, nil, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to open Space: %v", err), http.StatusInternalServerError)
		return
	}

	config, err := kb.GetConfig(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get Space config: %v", err), http.StatusInternalServerError)
		return
	}

	if config == nil {
		config = &memory.KnowledgeBaseConfig{}
	}

	config.AutoShareToDaemons = req.Enabled

	if err := kb.SetConfig(ctx, config); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save Space config: %v", err), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		http.Error(w, fmt.Sprintf("Failed to commit transaction: %v", err), http.StatusInternalServerError)
		return
	}

	action := "disabled"
	if req.Enabled {
		action = "enabled"

		// If enabling, trigger immediate push to all registered daemons
		go func() {
			registry := GetDaemonRegistry()
			activeDaemons := registry.ListActive()

			for _, daemon := range activeDaemons {
				log.Info("Pushing newly auto-shared Space to daemon",
					"space_name", req.SpaceName,
					"daemon_id", daemon.DaemonID,
				)

				spaceJSON, err := exportSpaceFromHQ(context.Background(), req.DatabaseName, req.SpaceName)
				if err != nil {
					log.Error("Failed to export Space for auto-share",
						"space_name", req.SpaceName,
						"error", err,
					)
					continue
				}

				if err := pushSpaceToDaemon(context.Background(), daemon.DaemonURL, req.SpaceName, req.DatabaseName, spaceJSON); err != nil {
					log.Error("Failed to push auto-shared Space to daemon",
						"space_name", req.SpaceName,
						"daemon_id", daemon.DaemonID,
						"error", err,
					)
					updateSpaceFederationState(req.SpaceName, daemon.DaemonID, "failed", 0)
				} else {
					updateSpaceFederationState(req.SpaceName, daemon.DaemonID, "delivered", 0)
				}
			}
		}()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "success",
		"message": fmt.Sprintf("Auto-share %s for Space '%s'", action, req.SpaceName),
		"enabled": req.Enabled,
	})
}

// HandleListAutoSharedSpaces returns all Spaces with AutoShareToDaemons=true.
// GET /api/space/auto-shared
func HandleListAutoSharedSpaces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dbName := r.URL.Query().Get("database")
	if dbName == "" {
		dbName = "aidb" // Default database
	}

	ctx := context.Background()
	opts, err := GetDBOptions(ctx, dbName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get DB config: %v", err), http.StatusInternalServerError)
		return
	}

	db := database.NewDatabase(opts)

	spaceNames, err := db.GetDomains(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list Spaces: %v", err), http.StatusInternalServerError)
		return
	}

	tx, err := db.BeginTransaction(ctx, sop.ForReading, -1)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to begin transaction: %v", err), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	type autoSharedSpace struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}

	autoShared := []autoSharedSpace{}
	for _, spaceName := range spaceNames {
		kb, err := db.OpenKnowledgeBase(ctx, spaceName, tx, nil, nil, false)
		if err != nil {
			continue
		}

		config, err := kb.GetConfig(ctx)
		if err != nil || config == nil {
			continue
		}

		if config.AutoShareToDaemons {
			autoShared = append(autoShared, autoSharedSpace{
				Name:        spaceName,
				Description: strings.TrimSpace(config.Description),
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"database":    dbName,
		"auto_shared": autoShared,
		"count":       len(autoShared),
	})
}

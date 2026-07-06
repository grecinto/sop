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

// listAutoSharedSpacesForDaemon returns all Spaces marked with AutoShareToDaemons=true.
// Daemons use this list to pull Spaces they don't have yet.
func listAutoSharedSpacesForDaemon(ctx context.Context) []map[string]string {
	// Get all configured databases
	cfg := GetConfig()
	databases := make([]string, 0, len(cfg.Databases))
	for _, dbCfg := range cfg.Databases {
		databases = append(databases, dbCfg.Name)
	}

	result := []map[string]string{}
	for _, dbName := range databases {
		opts, err := GetDBOptions(ctx, dbName)
		if err != nil {
			continue
		}

		db := database.NewDatabase(opts)

		// List all Spaces in this database
		spaceNames, err := db.GetDomains(ctx)
		if err != nil {
			continue
		}

		// Check each Space's config for AutoShareToDaemons flag
		for _, spaceName := range spaceNames {
			if shouldAutoShare, err := isSpaceAutoShared(ctx, dbName, spaceName); err == nil && shouldAutoShare {
				result = append(result, map[string]string{
					"space_name":    spaceName,
					"database_name": dbName,
				})
			}
		}
	}

	return result
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

	authCtx := sop.GetAuthFromContext(r.Context())
	isAdmin := false
	for _, role := range authCtx.Roles {
		if sop.IsAdminRole(role) {
			isAdmin = true
			break
		}
	}
	if !isAdmin {
		http.Error(w, "Forbidden: SystemAdmin role required", http.StatusForbidden)
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
		log.Info("Space auto-share enabled - will be pulled by daemons on next sync",
			"space_name", req.SpaceName,
		)
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

// HandleDaemonPullSpace allows a daemon to pull a specific Space from HQ.
// This is the pull-based federation model where daemon requests Space data.
func HandleDaemonPullSpace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request
	var req struct {
		SpaceName    string `json:"space_name"`
		DatabaseName string `json:"database_name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.SpaceName == "" || req.DatabaseName == "" {
		http.Error(w, "space_name and database_name are required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Verify this Space is actually marked for auto-share
	isAutoShared, err := isSpaceAutoShared(ctx, req.DatabaseName, req.SpaceName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to check Space auto-share status: %v", err), http.StatusInternalServerError)
		return
	}

	if !isAutoShared {
		http.Error(w, "Space is not marked for auto-share", http.StatusForbidden)
		return
	}

	// Export the Space
	spaceJSON, err := exportSpaceFromHQ(ctx, req.DatabaseName, req.SpaceName)
	if err != nil {
		log.Error("Failed to export Space for daemon pull",
			"space_name", req.SpaceName,
			"error", err,
		)
		http.Error(w, fmt.Sprintf("Failed to export Space: %v", err), http.StatusInternalServerError)
		return
	}

	log.Info("Daemon pulled Space from HQ",
		"space_name", req.SpaceName,
		"database", req.DatabaseName,
		"size_bytes", len(spaceJSON),
	)

	// Return the Space JSON
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(spaceJSON)
}

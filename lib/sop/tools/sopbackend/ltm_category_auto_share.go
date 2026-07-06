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
)

// HandleSetCategoryAutoShare toggles the AutoShareToDaemons flag for a Category in HQ LTM.
// Only accessible by SystemAdmin role.
// POST /api/ltm/category/auto-share
// Body: {"space_name": "...", "database_name": "...", "category_path": "...", "enabled": true/false}
func HandleSetCategoryAutoShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check authorization - only SystemAdmin can manage category auto-share
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
		CategoryPath string `json:"category_path"`
		Enabled      bool   `json:"enabled"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if req.SpaceName == "" || req.DatabaseName == "" || req.CategoryPath == "" {
		http.Error(w, "space_name, database_name, and category_path are required", http.StatusBadRequest)
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

	// Find the category by path
	categories, err := kb.Store.Categories(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get categories: %v", err), http.StatusInternalServerError)
		return
	}

	categoryPaths, err := kb.Store.CategoriesByPath(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get categories by path: %v", err), http.StatusInternalServerError)
		return
	}

	found, err := categoryPaths.Find(ctx, req.CategoryPath, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to find category: %v", err), http.StatusInternalServerError)
		return
	}

	if !found {
		http.Error(w, fmt.Sprintf("Category not found: %s", req.CategoryPath), http.StatusNotFound)
		return
	}

	categoryID, err := categoryPaths.GetCurrentValue(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get category ID: %v", err), http.StatusInternalServerError)
		return
	}

	foundCat, err := categories.Find(ctx, categoryID, false)
	if err != nil || !foundCat {
		http.Error(w, fmt.Sprintf("Failed to find category by ID: %v", err), http.StatusInternalServerError)
		return
	}

	category, err := categories.GetCurrentValue(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get category: %v", err), http.StatusInternalServerError)
		return
	}

	// Update the auto-share flag
	category.AutoShareToDaemons = &req.Enabled

	if _, err := categories.Update(ctx, categoryID, category); err != nil {
		http.Error(w, fmt.Sprintf("Failed to update category: %v", err), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		http.Error(w, fmt.Sprintf("Failed to commit transaction: %v", err), http.StatusInternalServerError)
		return
	}

	action := "disabled"
	if req.Enabled {
		action = "enabled"
	}

	log.Info("Category auto-share updated",
		"space_name", req.SpaceName,
		"category_path", req.CategoryPath,
		"enabled", req.Enabled,
		"admin_user", authCtx.UserID,
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "success",
		"message": fmt.Sprintf("Auto-share %s for category '%s'", action, req.CategoryPath),
		"enabled": req.Enabled,
	})
}

// HandleListCategoryAutoShare lists all categories and their auto-share settings.
// Only accessible by SystemAdmin role.
// GET /api/ltm/category/auto-share?space_name=...&database_name=...
func HandleListCategoryAutoShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check authorization - only SystemAdmin can view category auto-share settings
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

	spaceName := r.URL.Query().Get("space_name")
	dbName := r.URL.Query().Get("database_name")

	if spaceName == "" || dbName == "" {
		http.Error(w, "space_name and database_name are required", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	opts, err := GetDBOptions(ctx, dbName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get DB config: %v", err), http.StatusInternalServerError)
		return
	}

	db := database.NewDatabase(opts)
	tx, err := db.BeginTransaction(ctx, sop.ForReading, -1)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to begin transaction: %v", err), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	kb, err := db.OpenKnowledgeBase(ctx, spaceName, tx, nil, nil, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to open Space: %v", err), http.StatusInternalServerError)
		return
	}

	categories, err := kb.Store.Categories(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get categories: %v", err), http.StatusInternalServerError)
		return
	}

	type categoryAutoShare struct {
		Path               string `json:"path"`
		Name               string `json:"name"`
		Description        string `json:"description,omitempty"`
		AutoShareToDaemons bool   `json:"auto_share_to_daemons"`
		IsDefault          bool   `json:"is_default"` // True if using default setting (nil)
	}

	result := []categoryAutoShare{}

	ok, err := categories.First(ctx)
	if err != nil || !ok {
		// No categories, return empty list
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"space_name": spaceName,
			"database":   dbName,
			"categories": result,
			"count":      0,
		})
		return
	}

	for {
		cat, err := categories.GetCurrentValue(ctx)
		if err != nil {
			break
		}

		if cat != nil {
			enabled := true   // Default to enabled
			isDefault := true // Assume default unless explicitly set

			if cat.AutoShareToDaemons != nil {
				enabled = *cat.AutoShareToDaemons
				isDefault = false
			}

			result = append(result, categoryAutoShare{
				Path:               cat.Path,
				Name:               cat.Name,
				Description:        strings.TrimSpace(cat.Description),
				AutoShareToDaemons: enabled,
				IsDefault:          isDefault,
			})
		}

		if ok, err := categories.Next(ctx); err != nil || !ok {
			break
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"space_name": spaceName,
		"database":   dbName,
		"categories": result,
		"count":      len(result),
	})
}

// HandleBulkSetCategoryAutoShare allows SA to bulk update multiple categories at once.
// Only accessible by SystemAdmin role.
// POST /api/ltm/category/auto-share/bulk
// Body: {"space_name": "...", "database_name": "...", "updates": [{"category_path": "...", "enabled": true/false}]}
func HandleBulkSetCategoryAutoShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check authorization - only SystemAdmin can manage category auto-share
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
		Updates      []struct {
			CategoryPath string `json:"category_path"`
			Enabled      bool   `json:"enabled"`
		} `json:"updates"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if req.SpaceName == "" || req.DatabaseName == "" || len(req.Updates) == 0 {
		http.Error(w, "space_name, database_name, and updates are required", http.StatusBadRequest)
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

	categories, err := kb.Store.Categories(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get categories: %v", err), http.StatusInternalServerError)
		return
	}

	categoryPaths, err := kb.Store.CategoriesByPath(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get categories by path: %v", err), http.StatusInternalServerError)
		return
	}

	successCount := 0
	failedPaths := []string{}

	for _, update := range req.Updates {
		found, err := categoryPaths.Find(ctx, update.CategoryPath, false)
		if err != nil || !found {
			failedPaths = append(failedPaths, update.CategoryPath)
			continue
		}

		categoryID, err := categoryPaths.GetCurrentValue(ctx)
		if err != nil {
			failedPaths = append(failedPaths, update.CategoryPath)
			continue
		}

		foundCat, err := categories.Find(ctx, categoryID, false)
		if err != nil || !foundCat {
			failedPaths = append(failedPaths, update.CategoryPath)
			continue
		}

		category, err := categories.GetCurrentValue(ctx)
		if err != nil {
			failedPaths = append(failedPaths, update.CategoryPath)
			continue
		}

		// Update the auto-share flag
		category.AutoShareToDaemons = &update.Enabled

		if _, err := categories.Update(ctx, categoryID, category); err != nil {
			failedPaths = append(failedPaths, update.CategoryPath)
			continue
		}

		successCount++
	}

	if err := tx.Commit(ctx); err != nil {
		http.Error(w, fmt.Sprintf("Failed to commit transaction: %v", err), http.StatusInternalServerError)
		return
	}

	log.Info("Bulk category auto-share updated",
		"space_name", req.SpaceName,
		"success_count", successCount,
		"failed_count", len(failedPaths),
		"admin_user", authCtx.UserID,
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":        "success",
		"message":       fmt.Sprintf("Updated %d categories", successCount),
		"success_count": successCount,
		"failed_count":  len(failedPaths),
		"failed_paths":  failedPaths,
	})
}

// getCategoryAutoShareUIVisibility returns whether category auto-share UI should be visible.
// Only visible to SystemAdmin users on HQ.
func getCategoryAutoShareUIVisibility(authCtx sop.AuthContext, isHQ bool) bool {
	if !isHQ {
		return false // Never show on daemon UI
	}
	for _, role := range authCtx.Roles {
		if sop.IsAdminRole(role) {
			return true
		}
	}
	return false
}

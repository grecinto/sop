package sopbackend

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai/memory"

	aidb "github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/database"
)

func handleListSpaceCategories(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	storeName := r.URL.Query().Get("name")
	dbName := r.URL.Query().Get("database")
	parentIDStr := r.URL.Query().Get("parent_id")
	offsetStr := r.URL.Query().Get("offset")
	limitStr := r.URL.Query().Get("limit")

	offset := 0
	if offsetStr != "" {
		fmt.Sscanf(offsetStr, "%d", &offset)
	}
	limit := 50
	if limitStr != "" {
		fmt.Sscanf(limitStr, "%d", &limit)
	}

	if storeName == "" {
		http.Error(w, "Space name is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	dbOpts, err := GetDBOptions(ctx, dbName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForReading)
	if err != nil {
		http.Error(w, "Failed to begin transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer trans.Rollback(ctx)

	db := aidb.NewDatabase(dbOpts)
	dbEmbedder := GetConfiguredEmbedder(r)
	dbLLM := GetConfiguredLLM(r)

	memoryDb, err := db.OpenKnowledgeBase(ctx, storeName, trans, dbLLM, dbEmbedder, false)
	if err != nil {
		json.NewEncoder(w).Encode(make([]map[string]any, 0))
		return
	}

	parentPath := ""
	if parentIDStr == "root" {
		parentPath = "/"
	} else if parentIDStr != "" {
		parentPath = parentIDStr
	}

	var parentCatID sop.UUID
	parentUUID, errP := sop.ParseUUID(parentPath)
	if errP == nil {
		parentCatID = parentUUID
		parentPath = ""
	}

	catList, totalCount, err := memoryDb.ListCategories(ctx, memory.ListCategoriesParam{Limit: limit, Offset: offset, ParentPath: parentPath, ParentID: parentCatID})
	if err != nil {
		json.NewEncoder(w).Encode(make([]map[string]any, 0))
		return
	}

	categories := make([]map[string]any, len(catList))
	authCtx := sop.GetAuthFromContext(ctx)
	for i, cat := range catList {
		children := cat.ChildrenIDs
		if len(children) == 0 {
			children = make([]sop.UUID, 0)
		}
		categoryMap := map[string]any{
			"id":            cat.ID.String(),
			"name":          cat.Name,
			"path":          cat.Path,
			"description":   cat.Description,
			"item_count":    cat.ItemCount,
			"children":      children,
			"parents":       cat.ParentIDs,
			"center_vector": cat.CenterVector,
			"metadata":      cloneDataMap(cat.Metadata),
		}
		annotateCategoryAutoShareVisibility(categoryMap, authCtx, storeName, true)
		categories[i] = categoryMap
	}

	if categories == nil {
		categories = make([]map[string]any, 0)
	}

	rbacMap := sop.ResolveRBACMap(ctx, "space", sop.EntitlementContext{AssetID: storeName, Database: dbName, IsSystemDB: IsSystemDB(dbName)}, nil)

	response := map[string]any{
		"data":   categories,
		"total":  totalCount,
		"offset": offset,
		"limit":  limit,
		// When a Space is about to be displayed, it calls handleListSpaceCategories. Thus we can send the RBAC for the Space here.
		"rbac": rbacMap,
	}
	json.NewEncoder(w).Encode(response)
}

func handleListSpaceItems(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	storeName := r.URL.Query().Get("name")
	dbName := r.URL.Query().Get("database")
	categoryFilter := r.URL.Query().Get("category_id")
	offsetStr := r.URL.Query().Get("offset")
	limitStr := r.URL.Query().Get("limit")

	offset := 0
	if offsetStr != "" {
		fmt.Sscanf(offsetStr, "%d", &offset)
	}
	limit := 50
	if limitStr != "" {
		fmt.Sscanf(limitStr, "%d", &limit)
	}

	if storeName == "" {
		http.Error(w, "Space name is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	dbOpts, err := GetDBOptions(ctx, dbName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForReading)
	if err != nil {
		http.Error(w, "Failed to begin transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer trans.Rollback(ctx)

	var items []SpaceItemView

	// 1. Try NEW Dynamic Store (from today)
	db := aidb.NewDatabase(dbOpts)
	dbEmbedder := GetConfiguredEmbedder(r)
	dbLLM := GetConfiguredLLM(r)

	memoryDb, errDynamic := db.OpenKnowledgeBase(ctx, storeName, trans, dbLLM, dbEmbedder, false)

	totalItemsCount := 0

	if errDynamic == nil && memoryDb != nil {
		var catIDFilter sop.UUID
		if categoryFilter != "" {
			catIDFilter, _ = sop.ParseUUID(categoryFilter)
		}

		rawItems, count, _ := memoryDb.ListItems(ctx, memory.ListItemsParam{CategoryID: catIDFilter, Limit: limit, Offset: offset})
		totalItemsCount = count

		getGoodSample := func(vec []float32) float32 {
			for _, v := range vec {
				if v != 0 {
					return v
				}
			}
			return 0
		}

		for _, val := range rawItems {
			if val.ID.IsNil() {
				continue
			}
			t := SpaceItemView{
				ID:         val.ID.String(),
				CategoryID: val.CategoryID.String(),
				Category:   val.CategoryID.String(),
				Summaries:  val.Summaries,
				DocID:      val.DocID,
			}
			if val.Data != nil {
				payload := val.Data
				if docID, found := payload["doc_id"]; found && len(t.DocID) == 0 {
					switch v := docID.(type) {
					case []string:
						t.DocID = memory.DocIDs(v)
					case []any:
						out := make([]string, 0, len(v))
						for _, item := range v {
							out = append(out, fmt.Sprint(item))
						}
						t.DocID = memory.DocIDs(out)
					default:
						t.DocID = memory.DocIDs{fmt.Sprint(v)}
					}
				}
				if text, found := payload["text"]; found {
					t.Text = fmt.Sprint(text)
				}
				if desc, found := payload["description"]; found {
					t.Description = fmt.Sprint(desc)
				}
				if contentType, found := payload["content_type"]; found {
					t.ContentType = fmt.Sprint(contentType)
				}
				if cat, found := payload["category"]; found {
					t.Category = fmt.Sprint(cat)
				}
				if storageMode, found := payload["storage_mode"]; found {
					t.StorageMode = fmt.Sprint(storageMode)
				}
				if payloadRef, found := payload["payload_ref"]; found {
					t.PayloadRef = fmt.Sprint(payloadRef)
				}
				if t.PayloadRef == "" {
					if filesystemRef, found := payload["filesystem_ref"]; found {
						t.PayloadRef = fmt.Sprint(filesystemRef)
					}
				}
				if cloudProvider, found := payload["cloud_provider"]; found {
					t.CloudProvider = fmt.Sprint(cloudProvider)
				}
				if lifecycleStatus, found := payload["lifecycle_status"]; found {
					t.LifecycleStatus = fmt.Sprint(lifecycleStatus)
				}
				if lifecycleReason, found := payload["lifecycle_reason"]; found {
					t.LifecycleReason = fmt.Sprint(lifecycleReason)
				}
				if archivedAt, found := payload["archived_at"]; found {
					switch v := archivedAt.(type) {
					case int64:
						t.ArchivedAt = v
					case int:
						t.ArchivedAt = int64(v)
					case float64:
						t.ArchivedAt = int64(v)
					}
				}
				if archiveReason, found := payload["archive_reason"]; found {
					t.ArchiveReason = fmt.Sprint(archiveReason)
				}
				if accessCount, found := payload["lifecycle_access_count"]; found {
					switch v := accessCount.(type) {
					case int:
						t.LifecycleAccessCount = v
					case int64:
						t.LifecycleAccessCount = int(v)
					case float64:
						t.LifecycleAccessCount = int(v)
					}
				}
			}
			if len(t.DocID) > 0 {
				if docID, err := sop.ParseUUID(t.DocID.First()); err == nil {
					if doc, err := memoryDb.Store.GetDocument(ctx, docID); err == nil && doc != nil {
						if t.PayloadRef == "" && doc.PayloadRef != "" {
							t.PayloadRef = doc.PayloadRef
						}
						if t.StorageMode == "" && doc.StorageMode != "" {
							t.StorageMode = string(doc.StorageMode)
						}
						if t.CloudProvider == "" && doc.CloudSource.Provider != "" {
							t.CloudProvider = doc.CloudSource.Provider
						}
					}
				}
			}
			if len(val.Positions) > 0 {
				vecTree, errVec := memoryDb.Store.Vectors(ctx)
				if errVec == nil && vecTree != nil {
					found, _ := vecTree.Find(ctx, val.Positions[0], false)
					if found {
						vVal, _ := vecTree.GetCurrentValue(ctx)
						if len(vVal.Data) > 0 {
							t.Vector = []float32{getGoodSample(vVal.Data)}
							t.VectorSize = len(vVal.Data)
						}
					}
				}
			}
			items = append(items, t)
		}

		if items == nil {
			items = make([]SpaceItemView, 0)
		}

		response := map[string]any{
			"data":   items,
			"total":  totalItemsCount,
			"offset": offset,
			"limit":  limit,
		}
		json.NewEncoder(w).Encode(response)
		return
	}
	http.Error(w, "Failed to open KnowledgeBase or items tree not found", http.StatusInternalServerError)
}

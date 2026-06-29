package sopbackend

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	aidb "github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/embed"
	"github.com/sharedcode/sop/ai/memory"
	"github.com/sharedcode/sop/database"
)

func generateDeterministicID(catID sop.UUID, dataStr string) sop.UUID {
	hashBytes := sha256.Sum256([]byte(catID.String() + ":" + dataStr))
	id, _ := sop.ParseUUID(fmt.Sprintf("%x", hashBytes[:16]))
	return id
}

func cloneDataMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}

func ensureDocumentReference(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], item *memory.Item[map[string]any], data map[string]any) error {
	if kb == nil || item == nil || data == nil {
		return nil
	}

	hasDocumentMetadata := false
	if _, ok := data["storage_mode"]; ok {
		hasDocumentMetadata = true
	}
	if _, ok := data["payload_ref"]; ok {
		hasDocumentMetadata = true
	}
	if _, ok := data["cloud_provider"]; ok {
		hasDocumentMetadata = true
	}
	if _, ok := data["title"]; ok {
		hasDocumentMetadata = true
	}
	if _, ok := data["url"]; ok {
		hasDocumentMetadata = true
	}
	if _, ok := data["source"]; ok {
		hasDocumentMetadata = true
	}
	if _, ok := data["content_type"]; ok {
		hasDocumentMetadata = true
	}
	if _, ok := data["content"]; ok {
		hasDocumentMetadata = true
	}
	if _, ok := data["doc_id"]; ok {
		hasDocumentMetadata = true
	}
	if !hasDocumentMetadata && len(item.DocID) == 0 {
		return nil
	}

	docID := sop.NewUUID()
	if len(item.DocID) > 0 {
		if parsed, err := sop.ParseUUID(item.DocID.First()); err == nil {
			docID = parsed
		}
	}
	if explicitDocID, ok := data["doc_id"].(string); ok {
		if parsed, err := sop.ParseUUID(explicitDocID); err == nil {
			docID = parsed
		}
	}

	doc := memory.Document{ID: docID}
	if title, ok := data["title"].(string); ok {
		doc.Title = title
	}
	if url, ok := data["url"].(string); ok {
		doc.URL = url
	}
	if source, ok := data["source"].(string); ok {
		doc.Source = source
	}
	if contentType, ok := data["content_type"].(string); ok {
		doc.ContentType = contentType
	}
	if content, ok := data["content"].(string); ok {
		doc.Content = content
	}
	if rawData, ok := data["data"].([]byte); ok {
		doc.Data = rawData
	}
	if uploadData, ok := data["__uploaded_bytes"].([]byte); ok {
		doc.Data = uploadData
	}
	if content, ok := data["content"].(string); ok {
		doc.Content = content
	}
	if rawData, ok := data["data"].([]byte); ok {
		doc.Data = rawData
	}
	if storageMode, ok := data["storage_mode"].(string); ok && storageMode != "" {
		doc.StorageMode = memory.DocumentStorageMode(storageMode)
	}
	if doc.StorageMode == "" {
		if _, ok := data["filesystem_ref"]; ok {
			doc.StorageMode = memory.DocumentStorageModeFilesystemReference
		}
	}
	if payloadRef, ok := data["payload_ref"].(string); ok && payloadRef != "" {
		doc.PayloadRef = payloadRef
	}
	if uploadData, ok := data["__uploaded_bytes"].([]byte); ok && len(uploadData) > 0 {
		doc.Data = uploadData
		if doc.PayloadRef == "" {
			filename := ""
			if v, ok := data["filename"].(string); ok {
				filename = v
			}
			doc.PayloadRef = "uploaded://" + filepath.Base(filename)
		}
	}
	if filesystemRef, ok := data["filesystem_ref"].(string); ok && filesystemRef != "" {
		if doc.PayloadRef == "" {
			doc.PayloadRef = filesystemRef
		}
		if doc.StorageMode == "" {
			doc.StorageMode = memory.DocumentStorageModeFilesystemReference
		}
	}
	if cloudProvider, ok := data["cloud_provider"].(string); ok {
		doc.CloudSource.Provider = cloudProvider
	}
	if bucket, ok := data["bucket"].(string); ok {
		doc.CloudSource.Bucket = bucket
	}
	if objectKey, ok := data["object_key"].(string); ok {
		doc.CloudSource.ObjectKey = objectKey
	}
	if accessHint, ok := data["access_hint"].(string); ok {
		doc.CloudSource.AccessHint = accessHint
	}
	if region, ok := data["region"].(string); ok {
		doc.CloudSource.Region = region
	}
	if cloudSource, ok := data["cloud_source"].(map[string]any); ok {
		if provider, ok := cloudSource["provider"].(string); ok {
			doc.CloudSource.Provider = provider
		}
		if bucket, ok := cloudSource["bucket"].(string); ok {
			doc.CloudSource.Bucket = bucket
		}
		if objectKey, ok := cloudSource["object_key"].(string); ok {
			doc.CloudSource.ObjectKey = objectKey
		}
		if accessHint, ok := cloudSource["access_hint"].(string); ok {
			doc.CloudSource.AccessHint = accessHint
		}
		if region, ok := cloudSource["region"].(string); ok {
			doc.CloudSource.Region = region
		}
		if url, ok := cloudSource["url"].(string); ok {
			doc.CloudSource.URL = url
		}
	}
	if doc.StorageMode == "" && (doc.PayloadRef != "" || doc.CloudSource.Provider != "" || doc.CloudSource.ObjectKey != "") {
		doc.StorageMode = memory.DocumentStorageModeExternalMetadataOnly
	}

	if err := kb.Store.UpsertDocument(ctx, doc); err != nil {
		return err
	}
	item.DocID = memory.DocIDs{doc.ID.String()}
	if data != nil {
		data["doc_id"] = doc.ID.String()
		if doc.StorageMode != "" {
			data["storage_mode"] = string(doc.StorageMode)
		}
		if doc.PayloadRef != "" {
			data["payload_ref"] = doc.PayloadRef
		}
		if doc.CloudSource.Provider != "" {
			data["cloud_provider"] = doc.CloudSource.Provider
		}
		if doc.CloudSource.ObjectKey != "" {
			data["object_key"] = doc.CloudSource.ObjectKey
		}
		if doc.CloudSource.Bucket != "" {
			data["bucket"] = doc.CloudSource.Bucket
		}
		if doc.CloudSource.AccessHint != "" {
			data["access_hint"] = doc.CloudSource.AccessHint
		}
	}
	return nil
}

type addCategoryResponse struct {
	Success bool   `json:"success"`
	ID      string `json:"id,omitempty"`
	Message string `json:"message,omitempty"`
}

func handleAddSpaceCategory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	storeName := r.URL.Query().Get("name")
	dbName := r.URL.Query().Get("database")
	if storeName == "" {
		http.Error(w, "Knowledge Base name is required", http.StatusBadRequest)
		return
	}

	var req AddSpaceCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	dbOpts, err := GetDBOptions(ctx, dbName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
	if err != nil {
		http.Error(w, "Failed to begin transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer trans.Rollback(ctx)

	db := aidb.NewDatabase(dbOpts)
	dbEmbedder := GetConfiguredEmbedder(r)
	dbLLM := GetConfiguredLLM(r)

	kb, err := db.OpenKnowledgeBase(ctx, storeName, trans, dbLLM, dbEmbedder, false)
	if err != nil {
		http.Error(w, "Failed to open knowledge base: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var vec []float32
	if config.ProductionMode {
		embedder := embed.NewSimple("simple_hash", 384, nil)
		v, err := embed.CategoryTexts(ctx, embedder, []string{req.Name})
		if err != nil || len(v) == 0 {
			http.Error(w, "Failed to embed category", http.StatusInternalServerError)
			return
		}
		vec = v[0]
	} else {
		vec = []float32{0.1, 0.2, 0.3}
	}

	var id sop.UUID
	isUpdate := req.ID != ""

	var cat *memory.Category

	if isUpdate {
		categoriesTree, err := kb.Store.Categories(ctx)
		if err != nil {
			http.Error(w, "Failed to load categories store", http.StatusInternalServerError)
			return
		}

		targetID, err := sop.ParseUUID(req.ID)
		if err != nil {
			http.Error(w, "Invalid ID format format", http.StatusBadRequest)
			return
		}

		found, err := categoriesTree.Find(ctx, targetID, false)
		if err != nil || !found {
			http.Error(w, "Category not found", http.StatusNotFound)
			return
		}

		cat, err = categoriesTree.GetCurrentValue(ctx)
		if err != nil {
			http.Error(w, "Failed to load category details", http.StatusInternalServerError)
			return
		}

		cat.Name = req.Name
		cat.Description = req.Description

		// Only generate a new vector if CenterVector already existed
		if len(cat.CenterVector) > 0 {
			cat.CenterVector = vec
		}

		_, err = categoriesTree.UpdateCurrentItem(ctx, targetID, cat)
		if err != nil {
			http.Error(w, "Failed to update category: "+err.Error(), http.StatusInternalServerError)
			return
		}
		id = targetID
	} else {
		newID := sop.NewUUID()

		pathVal := strings.TrimSpace(req.Path)
		if pathVal == "" {
			pathVal = strings.TrimSpace(req.Name)
		}
		var parentIDs []memory.CategoryParent

		if req.ParentID != "" {
			parsedParent, err := sop.ParseUUID(req.ParentID)
			if err == nil {
				parentIDs = []memory.CategoryParent{{ParentID: parsedParent, UseCase: "Manual Subcategory"}}

				categoriesTree, err := kb.Store.Categories(ctx)
				if err == nil {
					found, _ := categoriesTree.Find(ctx, parsedParent, false)
					if found {
						pCat, _ := categoriesTree.GetCurrentValue(ctx)
						if pCat != nil {
							parentPath := strings.TrimSpace(pCat.Path)
							childName := strings.TrimSpace(req.Name)
							if parentPath != "" && childName != "" {
								pathVal = parentPath + "/" + childName
							} else if childName != "" {
								pathVal = childName
							}
						}
					}
				}
			}
		}

		cat = &memory.Category{
			ID:              newID,
			Name:            req.Name,
			Path:            pathVal,
			Description:     req.Description,
			CenterVector:    vec,
			SummaryMaxCount: MAX_ITEM_SUMMARIES,
			ParentIDs:       parentIDs,
		}

		id, err = kb.Store.AddCategory(ctx, cat)
		if err != nil {
			http.Error(w, "Failed to add category: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if err := trans.Commit(ctx); err != nil {
		http.Error(w, "Failed to commit transaction", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(addCategoryResponse{
		Success: true,
		ID:      id.String(),
		Message: "Category added/updated",
	})
}

type deleteCategoryRequest struct {
	ID string `json:"id"`
}

func handleDeleteSpaceCategory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	storeName := r.URL.Query().Get("name")
	dbName := r.URL.Query().Get("database")
	if storeName == "" {
		http.Error(w, "Knowledge Base name is required", http.StatusBadRequest)
		return
	}

	var req deleteCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	parsedID, err := sop.ParseUUID(req.ID)
	if err != nil {
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	dbOpts, err := GetDBOptions(ctx, dbName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
	if err != nil {
		http.Error(w, "Failed to begin transaction", http.StatusInternalServerError)
		return
	}
	defer trans.Rollback(ctx)

	db := aidb.NewDatabase(dbOpts)
	dbEmbedder := GetConfiguredEmbedder(r)
	dbLLM := GetConfiguredLLM(r)

	kb, err := db.OpenKnowledgeBase(ctx, storeName, trans, dbLLM, dbEmbedder, false)
	if err != nil {
		http.Error(w, "Failed to open knowledge base", http.StatusInternalServerError)
		return
	}

	err = kb.DeleteCategories(ctx, []sop.UUID{parsedID})
	if err != nil {
		http.Error(w, "Category not found or delete fail", http.StatusInternalServerError)
		return
	}

	if err := trans.Commit(ctx); err != nil {
		http.Error(w, "Commit failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(addCategoryResponse{
		Success: true,
		Message: "Category deleted",
	})
}

func handleAddSpaceItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	storeName := r.URL.Query().Get("name")
	dbName := r.URL.Query().Get("database")
	if storeName == "" {
		http.Error(w, "Knowledge Base name is required", http.StatusBadRequest)
		return
	}

	var req AddSpaceItemRequest
	var uploadReq *AddSpaceItemUploadRequest
	if strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, "Failed to parse file upload", http.StatusBadRequest)
			return
		}

		assetMode := strings.ToLower(strings.TrimSpace(r.FormValue("asset_mode")))
		isReferenceMode := assetMode == "reference"
		req.Data = map[string]any{}

		var file multipart.File
		var header *multipart.FileHeader
		var err error
		if isReferenceMode {
			if r.FormValue("payload_ref") != "" {
				req.Data["payload_ref"] = r.FormValue("payload_ref")
			}
			if r.FormValue("storage_mode") != "" {
				req.Data["storage_mode"] = r.FormValue("storage_mode")
			} else {
				req.Data["storage_mode"] = string(memory.DocumentStorageModeExternalMetadataOnly)
			}
			if fileName := strings.TrimSpace(r.FormValue("filename")); fileName != "" {
				req.Data["filename"] = fileName
			}
			if req.Data["payload_ref"] == nil || req.Data["payload_ref"].(string) == "" {
				if fileName := strings.TrimSpace(r.FormValue("filename")); fileName != "" {
					req.Data["payload_ref"] = "uploaded://" + filepath.Base(fileName)
				}
			}
		} else {
			file, header, err = r.FormFile("file")
			if err != nil {
				http.Error(w, "Missing uploaded file", http.StatusBadRequest)
				return
			}
			defer file.Close()
			data, err := io.ReadAll(file)
			if err != nil {
				http.Error(w, "Failed to read uploaded file", http.StatusBadRequest)
				return
			}
			uploadReq = &AddSpaceItemUploadRequest{FileName: header.Filename, Data: data, Size: int64(len(data))}
			if uploadReq != nil && len(uploadReq.Data) > 0 {
				blobRef, err := persistUploadedBlob(storeName, uploadReq.FileName, uploadReq.Data, r.Header.Get("Content-Type"))
				if err != nil {
					http.Error(w, "Failed to persist uploaded file: "+err.Error(), http.StatusBadRequest)
					return
				}
				text, err := extractTextFromUploadedBlob(uploadReq.FileName, uploadReq.Data, r.Header.Get("Content-Type"))
				if err != nil {
					text = ""
				}
				req.Data["payload_ref"] = blobRef.Path
				req.Data["storage_mode"] = "filesystem_reference"
				req.Data["content_type"] = blobRef.ContentType
				req.Data["filename"] = blobRef.FileName
				req.Data["blob_path"] = blobRef.Path
				req.Data["blob_metadata_path"] = blobRef.MetadataPath
				req.Data["blob_checksum"] = blobRef.Checksum
				req.Data["blob_size"] = blobRef.Size
				if text != "" {
					req.Data["content"] = text
				}
				if req.Data["description"] == nil {
					req.Data["description"] = ""
				}
			}
			if uploadReq.FileName != "" {
				req.Data["filename"] = uploadReq.FileName
			}
			if uploadReq != nil && len(uploadReq.Data) > 0 {
				req.Data["__uploaded_bytes"] = uploadReq.Data
			}
		}
		if r.FormValue("description") != "" {
			req.Data["description"] = r.FormValue("description")
		}
		if r.FormValue("text") != "" {
			req.Data["text"] = r.FormValue("text")
		}
		if r.FormValue("category_id") != "" {
			req.CategoryID = r.FormValue("category_id")
		}
		if r.FormValue("summaries") != "" {
			req.Summaries = strings.Split(r.FormValue("summaries"), ".")
		}
		if r.FormValue("storage_mode") != "" {
			req.Data["storage_mode"] = r.FormValue("storage_mode")
		}
		if r.FormValue("payload_ref") != "" {
			req.Data["payload_ref"] = r.FormValue("payload_ref")
		}
		if r.FormValue("cloud_provider") != "" {
			req.Data["cloud_provider"] = r.FormValue("cloud_provider")
		}
		if r.FormValue("filename") != "" {
			req.Data["filename"] = r.FormValue("filename")
		}
		if r.FormValue("content_type") != "" {
			req.Data["content_type"] = r.FormValue("content_type")
		}
	} else {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON body", http.StatusBadRequest)
			return
		}
	}

	parsedCatID, err := sop.ParseUUID(req.CategoryID)
	if err != nil {
		http.Error(w, "Invalid category_id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	dbOpts, err := GetDBOptions(ctx, dbName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
	if err != nil {
		http.Error(w, "Failed to begin transaction", http.StatusInternalServerError)
		return
	}
	defer trans.Rollback(ctx)

	db := aidb.NewDatabase(dbOpts)
	dbEmbedder := GetConfiguredEmbedder(r)
	dbLLM := GetConfiguredLLM(r)

	kb, err := db.OpenKnowledgeBase(ctx, storeName, trans, dbLLM, dbEmbedder, false)
	if err != nil {
		http.Error(w, "Failed to open knowledge base", http.StatusInternalServerError)
		return
	}

	var vecs [][]float32
	chunkStr := ""
	if chunk, ok := req.Data["chunk"].(string); ok {
		chunkStr = chunk
	} else if textVal, ok := req.Data["text"].(string); ok {
		chunkStr = textVal
	} else if textVal, ok := req.Data["Text"].(string); ok {
		chunkStr = textVal
	}

	categoriesTree, _ := kb.Store.Categories(ctx)
	found, cErr := categoriesTree.Find(ctx, parsedCatID, false)
	if cErr != nil || !found {
		http.Error(w, "Category not found", http.StatusNotFound)
		return
	}
	cat, _ := categoriesTree.GetCurrentValue(ctx)

	maxSummaries := cat.SummaryMaxCount
	if maxSummaries <= 0 {
		maxSummaries = MAX_ITEM_SUMMARIES
	}

	dataBytes, _ := json.Marshal(req.Data)
	dataStr := string(dataBytes)

	detID := generateDeterministicID(parsedCatID, dataStr)
	payloadData := cloneDataMap(req.Data)
	newItem := memory.Item[map[string]any]{
		ID:         detID,
		DocID:      req.DocID,
		CategoryID: parsedCatID,
		Data:       payloadData,
	}
	if err := ensureDocumentReference(ctx, kb, &newItem, payloadData); err != nil {
		http.Error(w, "Failed to persist document reference: "+err.Error(), http.StatusInternalServerError)
		return
	}
	itemsTree, err := kb.Store.Items(ctx)
	if err == nil {
		if found, _ := itemsTree.Find(ctx, memory.ItemKey{CategoryID: parsedCatID, ItemID: detID}, false); found {
			// Item already exists. Skip summarization and vector generation.
			if err := trans.Commit(ctx); err != nil {
				http.Error(w, "Commit failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(addCategoryResponse{
				Success: true,
				ID:      detID.String(),
				Message: "Item already exists",
			})
			return
		}
	}

	summarizer := GetSummarizer(config.ProductionMode, GetConfiguredLLMClient(r))
	summaries := DetermineSummaries(ctx, summarizer, req.Summaries, chunkStr, dataStr, maxSummaries)

	if len(req.Positions) > 0 {
		vecs = req.Positions
	}

	newItem.Summaries = summaries
	newItem.Data = payloadData

	err = kb.Store.UpsertByCategoryPath(ctx, cat.Name, newItem, vecs)
	if err != nil {
		http.Error(w, "Failed to insert item: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if cfg, err := kb.GetConfig(ctx); err == nil {
		if cfg == nil {
			cfg = &memory.KnowledgeBaseConfig{}
		}
		cfg.LastModified = time.Now().Unix()
		kb.SetConfig(ctx, cfg)
	}

	if err := trans.Commit(ctx); err != nil {
		http.Error(w, "Commit failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(addCategoryResponse{
		Success: true,
		ID:      newItem.ID.String(),
		Message: "Item added safely",
	})
}

type DigestSourceStoreRequest struct {
	SourceStore       string `json:"source_store"`
	TargetSpace       string `json:"target_space"`
	Template          string `json:"template"`
	TopicHint         string `json:"topic_hint,omitempty"`
	CustomPrompt      string `json:"custom_prompt,omitempty"`
	ReadingStrategy   string `json:"reading_strategy,omitempty"`
	Mode              string `json:"mode,omitempty"`
	Vectorize         bool   `json:"vectorize,omitempty"`
	MaxFilesPerPass   int    `json:"max_files_per_pass,omitempty"`
	MaxChunkChars     int    `json:"max_chunk_chars,omitempty"`
	MaxChunkTokens    int    `json:"max_chunk_tokens,omitempty"`
	ChunkOverlapChars int    `json:"chunk_overlap_chars,omitempty"`
}

type digestFileSummary struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Size        int    `json:"size"`
	Hash        string `json:"hash,omitempty"`
	Content     string `json:"content,omitempty"`
	ModTime     int64  `json:"mod_time,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Preview     string `json:"preview,omitempty"`
}

type digestCuratedItemState struct {
	Path         string   `json:"path"`
	Hash         string   `json:"hash"`
	Size         int      `json:"size,omitempty"`
	ModTime      int64    `json:"mod_time,omitempty"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Body         string   `json:"body"`
	Sources      []string `json:"sources,omitempty"`
	CategoryPath string   `json:"category_path,omitempty"`
}

type digestProcessingEntry struct {
	File       digestFileSummary
	CachedItem *digestCuratedItemState
	NeedsLLM   bool
	SourceText string
}

type deleteItemRequest struct {
	ID         string `json:"id"`
	CategoryID string `json:"category_id"`
}

func resolveSourceStorePath(storagePath, sourceStore string) string {
	trimmed := strings.TrimSpace(sourceStore)
	if trimmed == "" {
		return ""
	}
	if filepath.IsAbs(trimmed) {
		return trimmed
	}
	candidates := []string{
		filepath.Join(storagePath, trimmed),
		filepath.Join(storagePath, sanitizeStoreName(trimmed)),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return candidates[0]
}

func collectDigestSourceText(sourceStorePath string) (string, []digestFileSummary, error) {
	if strings.TrimSpace(sourceStorePath) == "" {
		return "", nil, nil
	}
	var files []digestFileSummary
	var builder strings.Builder
	err := filepath.Walk(sourceStorePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(sourceStorePath, path)
		if err != nil {
			relPath = info.Name()
		}
		if relPath == "." || strings.HasPrefix(relPath, ".") && strings.HasSuffix(relPath, ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			return nil
		}
		hashBytes := sha256.Sum256(data)
		hash := fmt.Sprintf("%x", hashBytes[:])
		contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		text, err := extractTextFromUploadedBlob(info.Name(), data, contentType)
		if err != nil || strings.TrimSpace(text) == "" {
			return nil
		}
		preview := strings.TrimSpace(text)
		if len(preview) > 280 {
			preview = preview[:280] + "..."
		}
		files = append(files, digestFileSummary{
			Path:        filepath.ToSlash(relPath),
			Name:        info.Name(),
			Size:        len(data),
			Hash:        hash,
			Content:     text,
			ModTime:     info.ModTime().Unix(),
			ContentType: contentType,
			Preview:     preview,
		})
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString("File: ")
		builder.WriteString(filepath.ToSlash(relPath))
		builder.WriteString("\n")
		builder.WriteString(text)
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(builder.String()), files, nil
}

func collectDigestSourceTextFromStore(ctx context.Context, db *aidb.Database, sourceStore, storagePath string) (string, []digestFileSummary, error) {
	trimmedStore := strings.TrimSpace(sourceStore)
	if trimmedStore == "" {
		return "", nil, nil
	}
	if db != nil {
		if storePath, err := db.ResolveFolderStorePath(ctx, trimmedStore); err == nil {
			return collectDigestSourceText(storePath)
		} else if storeRoot, err := db.ResolveRepoStoreRoot(ctx, trimmedStore); err == nil {
			return collectDigestSourceText(storeRoot)
		}
	}
	return collectDigestSourceText(resolveSourceStorePath(storagePath, trimmedStore))
}

type digestStructuredContent struct {
	Title        string   `json:"title"`
	Summary      string   `json:"summary"`
	Body         string   `json:"body"`
	Sources      []string `json:"sources"`
	CategoryPath string   `json:"category_path"`
}

type digestInspectionPlan struct {
	Focus        string `json:"focus"`
	CategoryPath string `json:"category_path"`
}

func parseDigestStructuredContent(text string) (digestStructuredContent, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return digestStructuredContent{}, false
	}

	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 3 {
			trimmed = strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
		}
	}
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return digestStructuredContent{}, false
	}

	var parsed digestStructuredContent
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
		return parsed, true
	}

	lines := strings.Split(trimmed, "\n")
	var itemTitle string
	var summary string
	var bodyLines []string
	var sources []string
	var categoryPath string
	inBody := false
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "- item:") {
			itemTitle = strings.TrimSpace(strings.TrimPrefix(line, "- Item:"))
			itemTitle = strings.TrimSpace(strings.TrimPrefix(itemTitle, "- item:"))
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "summary:") {
			summary = strings.TrimSpace(strings.TrimPrefix(line, "Summary:"))
			summary = strings.TrimSpace(strings.TrimPrefix(summary, "summary:"))
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "body:") {
			inBody = true
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "category:") || strings.HasPrefix(strings.ToLower(line), "category_path:") {
			categoryPath = strings.TrimSpace(strings.TrimPrefix(line, "Category:"))
			categoryPath = strings.TrimSpace(strings.TrimPrefix(categoryPath, "category:"))
			categoryPath = strings.TrimSpace(strings.TrimPrefix(categoryPath, "CategoryPath:"))
			categoryPath = strings.TrimSpace(strings.TrimPrefix(categoryPath, "category_path:"))
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "source:") || strings.HasPrefix(strings.ToLower(line), "sources:") {
			inBody = false
			rawSources := strings.TrimSpace(strings.TrimPrefix(line, "Source:"))
			rawSources = strings.TrimSpace(strings.TrimPrefix(rawSources, "Sources:"))
			rawSources = strings.TrimSpace(strings.TrimPrefix(rawSources, "source:"))
			rawSources = strings.TrimSpace(strings.TrimPrefix(rawSources, "sources:"))
			if rawSources != "" {
				sources = append(sources, strings.Split(rawSources, ",")...)
			}
			continue
		}
		if inBody {
			bodyLines = append(bodyLines, line)
		}
	}
	if itemTitle != "" || summary != "" || len(bodyLines) > 0 || len(sources) > 0 || categoryPath != "" {
		parsed := digestStructuredContent{Title: itemTitle, Summary: summary, Body: strings.Join(bodyLines, "\n"), Sources: normalizeDigestSources(sources), CategoryPath: strings.TrimSpace(categoryPath)}
		return parsed, true
	}
	return digestStructuredContent{}, false
}

func normalizeDigestSources(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	seen := map[string]struct{}{}
	for _, candidate := range raw {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func buildDigestPrompt(req DigestSourceStoreRequest, sourceText string, files []digestFileSummary) string {
	var builder strings.Builder
	builder.WriteString("You are curating raw source material into structured knowledge for a target Space.\n")
	builder.WriteString("General curation policy:\n")
	builder.WriteString("- Use the source material as the only grounding. Do not invent facts.\n")
	builder.WriteString("- Produce high-signal, retrieval-friendly knowledge items that are concise, semantically organized, and provenance-aware.\n")
	builder.WriteString("- Prefer concise, high-signal synthesized entries over copied paragraphs. Merge duplicate discussions into one normalized entry whenever possible.\n")
	builder.WriteString("- Reduce duplication, noise, and marketing-style wording. Preserve source provenance for every knowledge entry.\n")
	builder.WriteString("- Use a bounded, retrieval-friendly structure. Group related knowledge under the most relevant semantic category and keep each item focused on a single concept.\n")
	builder.WriteString("- Follow the manifest taxonomy and the bounded semantic taxonomy when choosing where each item belongs.\n")
	builder.WriteString("- Inspect the source store before curating. Prefer using the available store-inspection tools to list relevant files and read the material needed to build the target knowledge base, rather than relying on a single raw dump.\n")
	builder.WriteString("- Curate the source-store material into high-quality knowledge. Let the LLM decide what is important, what is redundant, and what should be dropped while preserving provenance and grounding in the source material.\n")
	builder.WriteString("- Choose the most relevant category path for each item based on the manifest taxonomy and the source material.\n")
	builder.WriteString("- Source inventory is available first: use file metadata such as path, size, and preview to decide what to inspect closely and whether a file should be processed as its own chunk.\n")
	builder.WriteString("- Large files should be processed as separate chunks when metadata indicates they are substantial; do not force huge files into one undifferentiated pass.\n")
	if strings.TrimSpace(req.ReadingStrategy) != "" {
		builder.WriteString("- Reading strategy: ")
		builder.WriteString(req.ReadingStrategy)
		builder.WriteString("\n")
	}
	if req.MaxFilesPerPass > 0 {
		builder.WriteString(fmt.Sprintf("- Limit the inspection pass to %d files per batch.\n", req.MaxFilesPerPass))
	}
	if req.MaxChunkChars > 0 {
		builder.WriteString(fmt.Sprintf("- Keep each curated chunk under %d characters.\n", req.MaxChunkChars))
	}
	if req.MaxChunkTokens > 0 {
		builder.WriteString(fmt.Sprintf("- Keep each curated chunk under %d tokens.\n", req.MaxChunkTokens))
	}
	if req.ChunkOverlapChars > 0 {
		builder.WriteString(fmt.Sprintf("- Preserve %d characters of overlap between adjacent chunks when a file is chunked.\n", req.ChunkOverlapChars))
	}
	builder.WriteString("- Each output item should include: a short title, 1-5 concise summary lines, a fuller body paragraph, source provenance, and a category_path when a suitable category exists.\n")
	builder.WriteString("Target Space: ")
	builder.WriteString(req.TargetSpace)
	builder.WriteString("\n")
	builder.WriteString("Template: ")
	builder.WriteString(req.Template)
	builder.WriteString("\n")
	if strings.TrimSpace(req.TopicHint) != "" {
		builder.WriteString("Topic hint: ")
		builder.WriteString(req.TopicHint)
		builder.WriteString("\n")
	}
	if strings.TrimSpace(req.CustomPrompt) != "" {
		builder.WriteString("Custom prompt: ")
		builder.WriteString(req.CustomPrompt)
		builder.WriteString("\n")
	}
	builder.WriteString("Template-specific curation instructions:\n")
	switch strings.ToLower(strings.TrimSpace(req.Template)) {
	case "courseware":
		builder.WriteString("- Emphasize learning value: highlight core concepts, examples, dependencies, and study-friendly explanations.\n")
		builder.WriteString("- Frame the content as instructional knowledge that helps someone understand the topic quickly and accurately.\n")
	case "code expert", "code-expert", "codeexpert":
		builder.WriteString("- Emphasize engineering value: highlight architecture, tradeoffs, implementation details, testing guidance, and operational considerations.\n")
		builder.WriteString("- Frame the content as actionable expert knowledge for developers and maintainers.\n")
	case "research digest", "research-digest", "researchdigest":
		builder.WriteString("- Emphasize research value: highlight findings, evidence, comparisons, limitations, and open questions.\n")
		builder.WriteString("- Frame the content as a concise digest of what matters most for further investigation or decision-making.\n")
	default:
		builder.WriteString("- Create a concise knowledge summary with a clear title and a short description that captures the main insights and relevance to the target Space.\n")
	}
	builder.WriteString("Respond with a JSON object containing: title, summary, body, sources, and category_path.\n")
	builder.WriteString("- title: a short title for the curated item.\n")
	builder.WriteString("- summary: 1-5 concise summary lines or a single concise summary sentence.\n")
	builder.WriteString("- body: a fuller paragraph that captures the curated knowledge.\n")
	builder.WriteString("- sources: an array of the most relevant source paths or identifiers.\n")
	builder.WriteString("- category_path: a slash-delimited category path following the manifest taxonomy when it is clearly appropriate; otherwise omit it or use an empty string.\n")
	builder.WriteString("Source material:\n")
	builder.WriteString(sourceText)
	if len(files) > 0 {
		builder.WriteString("\n\nSource inventory:\n")
		for _, file := range buildDigestChunkingPlan(files, sourceText, req) {
			builder.WriteString(file.Path)
			builder.WriteString(" | size=")
			builder.WriteString(formatDigestFileSize(file.Size))
			if strings.TrimSpace(file.Preview) != "" {
				builder.WriteString(" | preview=")
				builder.WriteString(strings.TrimSpace(file.Preview))
			}
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func deriveDigestInspectionPlan(ctx context.Context, llm ai.Generator, req DigestSourceStoreRequest, sourceText string, files []digestFileSummary) (digestInspectionPlan, bool) {
	plan := digestInspectionPlan{}
	if llm == nil || strings.TrimSpace(sourceText) == "" {
		return plan, false
	}
	prompt := fmt.Sprintf(`You are planning a source-store curation pass for a Space.
Use the source material as the only grounding.
Target Space: %s
Template: %s
If relevant, recommend a concise inspection focus and a category_path that follows the manifest taxonomy.
Return JSON with fields: focus and category_path.
Source material:
%s`, req.TargetSpace, req.Template, sourceText)
	if len(files) > 0 {
		prompt += "\n\nIncluded files:\n"
		for _, file := range files {
			prompt += file.Path + "\n"
		}
	}
	out, err := llm.Generate(ctx, prompt, ai.GenOptions{MaxTokens: 140, Temperature: 0.2})
	if err != nil || strings.TrimSpace(out.Text) == "" {
		return plan, false
	}
	var parsed digestInspectionPlan
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.Text)), &parsed); err == nil {
		plan.Focus = strings.TrimSpace(parsed.Focus)
		plan.CategoryPath = strings.TrimSpace(parsed.CategoryPath)
		if plan.Focus != "" || plan.CategoryPath != "" {
			return plan, true
		}
	}
	return plan, false
}

func deriveDigestContent(ctx context.Context, llm ai.Generator, req DigestSourceStoreRequest, sourceText string, files []digestFileSummary) (title string, description string) {
	title = "Source Store Digest"
	description = strings.TrimSpace(sourceText)
	if description == "" {
		description = fmt.Sprintf("Digest request for source store %q using template %q.", req.SourceStore, req.Template)
	}
	if len(description) > 900 {
		description = description[:900] + "..."
	}
	if llm != nil && strings.TrimSpace(sourceText) != "" {
		prompt := buildDigestPrompt(req, sourceText, files)
		out, err := llm.Generate(ctx, prompt, ai.GenOptions{MaxTokens: 240, Temperature: 0.2})
		if err == nil && strings.TrimSpace(out.Text) != "" {
			parts := strings.Split(strings.TrimSpace(out.Text), "\n")
			for _, part := range parts {
				trimmed := strings.TrimSpace(part)
				if trimmed == "" {
					continue
				}
				if title == "Source Store Digest" {
					title = trimmed
					continue
				}
				if description == "" || len(trimmed) < len(description) {
					description = trimmed
				}
			}
			if title == "Source Store Digest" {
				title = fmt.Sprintf("%s Digest", strings.TrimSpace(req.Template))
			}
		}
	}
	if strings.TrimSpace(title) == "" || title == "Source Store Digest" {
		title = fmt.Sprintf("%s Digest", strings.TrimSpace(req.Template))
		if strings.TrimSpace(title) == "" {
			title = "Source Store Digest"
		}
	}
	if strings.TrimSpace(description) == "" {
		description = fmt.Sprintf("Digest request for source store %q using template %q.", req.SourceStore, req.Template)
	} else {
		description = fmt.Sprintf("Source store %q · template %q · %s", req.SourceStore, req.Template, description)
	}
	if len(description) > 900 {
		description = description[:900] + "..."
	}
	if strings.TrimSpace(req.TopicHint) != "" && !strings.Contains(strings.ToLower(description), strings.ToLower(req.TopicHint)) {
		description = fmt.Sprintf("%s — %s", req.TopicHint, description)
	}
	return title, description
}

func splitDigestChunks(sourceText string) []string {
	return splitDigestChunksWithRequest(sourceText, DigestSourceStoreRequest{})
}

func splitDigestChunksWithRequest(sourceText string, req DigestSourceStoreRequest) []string {
	trimmed := strings.TrimSpace(sourceText)
	if trimmed == "" {
		return nil
	}
	if strings.Contains(trimmed, "\nFile: ") {
		parts := strings.Split(trimmed, "\nFile: ")
		chunks := make([]string, 0, len(parts))
		for i, part := range parts {
			candidate := strings.TrimSpace(part)
			if candidate == "" {
				continue
			}
			if i > 0 {
				candidate = "File: " + candidate
			}
			chunks = append(chunks, candidate)
		}
		if len(chunks) > 0 {
			return chunks
		}
	}

	chunkSize := req.MaxChunkChars
	if chunkSize <= 0 {
		chunkSize = 0
	}
	overlap := req.ChunkOverlapChars
	if overlap < 0 {
		overlap = 0
	}
	if chunkSize > 0 && overlap >= chunkSize {
		overlap = chunkSize - 1
		if overlap < 0 {
			overlap = 0
		}
	}
	if chunkSize > 0 {
		chunks := make([]string, 0)
		start := 0
		for start < len(trimmed) {
			end := start + chunkSize
			if end > len(trimmed) {
				end = len(trimmed)
			}
			chunk := trimmed[start:end]
			if strings.TrimSpace(chunk) == "" {
				break
			}
			chunks = append(chunks, strings.TrimSpace(chunk))
			if end >= len(trimmed) {
				break
			}
			if overlap > 0 {
				start += chunkSize - overlap
			} else {
				start = end
			}
		}
		if len(chunks) > 0 {
			return chunks
		}
	}

	parts := strings.Split(trimmed, "\n\n")
	chunks := make([]string, 0, len(parts))
	for _, part := range parts {
		candidate := strings.TrimSpace(part)
		if candidate == "" {
			continue
		}
		chunks = append(chunks, candidate)
	}
	if len(chunks) == 0 {
		return []string{trimmed}
	}
	return chunks
}

func formatDigestFileSize(size int) string {
	if size < 0 {
		return "0B"
	}
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%dB", size)
	}
	value := float64(size) / float64(unit)
	if value < unit {
		return fmt.Sprintf("%.1fKB", value)
	}
	value /= float64(unit)
	if value < unit {
		return fmt.Sprintf("%.1fMB", value)
	}
	value /= float64(unit)
	return fmt.Sprintf("%.1fGB", value)
}

func buildDigestChunkingPlan(files []digestFileSummary, sourceText string, req DigestSourceStoreRequest) []digestFileSummary {
	if len(files) == 0 {
		return nil
	}
	limit := req.MaxFilesPerPass
	if limit <= 0 {
		limit = len(files)
	}
	plan := make([]digestFileSummary, 0, len(files))
	for idx, file := range files {
		if limit > 0 && idx >= limit {
			break
		}
		candidate := file
		threshold := 12000
		if req.MaxChunkChars > 0 {
			threshold = req.MaxChunkChars
		}
		if candidate.Size > threshold {
			candidate.Preview = fmt.Sprintf("Large file (%s); process as a separate chunk", formatDigestFileSize(candidate.Size))
		}
		plan = append(plan, candidate)
	}
	return plan
}

func buildDigestProcessingPlan(files []digestFileSummary, previous map[string]digestCuratedItemState, req DigestSourceStoreRequest) []digestProcessingEntry {
	if len(files) == 0 {
		return nil
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	useDelta := mode == "delta"
	plan := make([]digestProcessingEntry, 0, len(files))
	for _, file := range files {
		entry := digestProcessingEntry{File: file, NeedsLLM: true, SourceText: file.Content}
		if entry.SourceText == "" {
			entry.SourceText = file.Preview
		}
		if useDelta && previous != nil {
			if cached, ok := previous[file.Path]; ok {
				matchesMetadata := cached.Body != "" && file.ModTime > 0 && cached.ModTime > 0 && cached.ModTime == file.ModTime
				if matchesMetadata {
					entry.NeedsLLM = false
					entry.CachedItem = &cached
					entry.SourceText = cached.Body
				} else {
					matchesHash := strings.TrimSpace(cached.Hash) != "" && strings.TrimSpace(file.Hash) != "" && strings.EqualFold(strings.TrimSpace(cached.Hash), strings.TrimSpace(file.Hash))
					if matchesHash {
						entry.NeedsLLM = false
						entry.CachedItem = &cached
						entry.SourceText = cached.Body
					}
				}
			}
		}
		plan = append(plan, entry)
	}
	return plan
}

func deriveDigestChunkContentWithDetails(ctx context.Context, llm ai.Generator, req DigestSourceStoreRequest, chunk string, index int, inspectionPlan *digestInspectionPlan) (title string, description string, body string, sources []string, categoryPath string) {
	title = fmt.Sprintf("%s Note %d", strings.TrimSpace(req.Template), index+1)
	if strings.TrimSpace(title) == "" || title == "Note 1" {
		title = fmt.Sprintf("Digest Note %d", index+1)
	}
	description = strings.TrimSpace(chunk)
	if len(description) > 420 {
		description = description[:420] + "..."
	}
	body = ""
	sources = nil
	if llm != nil && strings.TrimSpace(chunk) != "" {
		prompt := fmt.Sprintf("You are curating one knowledge chunk from a source-store digest.\nUse the content as the only grounding.\nTemplate: %s\nTarget Space: %s\nReturn a JSON object with title, summary, body, sources, and category_path that capture the core idea and its usefulness.\nContent:\n%s", req.Template, req.TargetSpace, chunk)
		if inspectionPlan != nil && (strings.TrimSpace(inspectionPlan.Focus) != "" || strings.TrimSpace(inspectionPlan.CategoryPath) != "") {
			prompt += fmt.Sprintf("\n\nInspection plan:\nFocus: %s\nCategory path: %s", strings.TrimSpace(inspectionPlan.Focus), strings.TrimSpace(inspectionPlan.CategoryPath))
		}
		out, err := llm.Generate(ctx, prompt, ai.GenOptions{MaxTokens: 220, Temperature: 0.2})
		if err == nil && strings.TrimSpace(out.Text) != "" {
			if parsed, ok := parseDigestStructuredContent(out.Text); ok {
				title = strings.TrimSpace(parsed.Title)
				description = strings.TrimSpace(parsed.Summary)
				body = strings.TrimSpace(parsed.Body)
				sources = append([]string(nil), parsed.Sources...)
				categoryPath = strings.TrimSpace(parsed.CategoryPath)
			}
			if strings.TrimSpace(title) == "" || strings.HasPrefix(title, "Note") {
				lines := strings.Split(strings.TrimSpace(out.Text), "\n")
				for _, line := range lines {
					trimmed := strings.TrimSpace(line)
					if trimmed == "" {
						continue
					}
					if title == fmt.Sprintf("%s Note %d", strings.TrimSpace(req.Template), index+1) || title == fmt.Sprintf("Digest Note %d", index+1) {
						title = trimmed
						continue
					}
					if description == "" || len(trimmed) < len(description) {
						description = trimmed
					}
				}
			}
		}
	}
	if strings.TrimSpace(title) == "" || strings.HasPrefix(title, "Note") {
		title = fmt.Sprintf("%s Note %d", strings.TrimSpace(req.Template), index+1)
		if strings.TrimSpace(strings.TrimSpace(req.Template)) == "" {
			title = fmt.Sprintf("Digest Note %d", index+1)
		}
	}
	if strings.TrimSpace(body) == "" {
		body = strings.TrimSpace(chunk)
	}
	if strings.TrimSpace(description) == "" {
		description = strings.TrimSpace(chunk)
	}
	if strings.TrimSpace(description) == "" {
		description = fmt.Sprintf("Digest from source store %q using template %q.", req.SourceStore, req.Template)
	} else if strings.TrimSpace(body) == "" || strings.TrimSpace(body) == strings.TrimSpace(chunk) {
		description = fmt.Sprintf("Source store %q · template %q · %s", req.SourceStore, req.Template, description)
	}
	if len(description) > 420 {
		description = description[:420] + "..."
	}
	if strings.TrimSpace(req.TopicHint) != "" && !strings.Contains(strings.ToLower(description), strings.ToLower(req.TopicHint)) {
		description = fmt.Sprintf("%s — %s", req.TopicHint, description)
	}
	if body == "" {
		body = strings.TrimSpace(chunk)
	}
	if len(sources) == 0 {
		sources = []string{req.SourceStore}
	}
	return title, description, body, sources, categoryPath
}

func deriveDigestChunkContent(ctx context.Context, llm ai.Generator, req DigestSourceStoreRequest, chunk string, index int) (title string, description string) {
	title, description, _, _, _ = deriveDigestChunkContentWithDetails(ctx, llm, req, chunk, index, nil)
	return title, description
}

func ensureDigestTargetSpace(ctx context.Context, dbOpts sop.DatabaseOptions, spaceName string, llm ai.Generator, embedder ai.Embeddings) error {
	db := aidb.NewDatabase(dbOpts)
	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
	if err != nil {
		return err
	}
	defer trans.Rollback(ctx)

	if _, err := db.OpenKnowledgeBase(ctx, spaceName, trans, llm, embedder, false); err != nil {
		return err
	}
	return trans.Commit(ctx)
}

func handleDigestSourceStoreUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "Failed to parse upload", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Missing uploaded file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read uploaded file", http.StatusBadRequest)
		return
	}

	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	text, err := extractTextFromUploadedBlob(header.Filename, data, contentType)
	if err != nil {
		http.Error(w, "Unable to read uploaded content: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(text) == "" {
		http.Error(w, "No readable text content found in the uploaded file. Please choose a text-based file with readable content.", http.StatusBadRequest)
		return
	}

	dbName := strings.TrimSpace(r.FormValue("database"))
	spaceName := strings.TrimSpace(r.FormValue("target_space"))
	template := strings.TrimSpace(r.FormValue("template"))
	topicHint := strings.TrimSpace(r.FormValue("topic_hint"))
	customPrompt := strings.TrimSpace(r.FormValue("custom_prompt"))
	readingStrategy := strings.TrimSpace(r.FormValue("reading_strategy"))
	mode := strings.TrimSpace(r.FormValue("mode"))

	if dbName == "" {
		http.Error(w, "Database name is required", http.StatusBadRequest)
		return
	}
	if spaceName == "" {
		http.Error(w, "Target space is required", http.StatusBadRequest)
		return
	}

	dbOpts, err := GetDBOptions(r.Context(), dbName)
	if err != nil {
		http.Error(w, "Invalid database", http.StatusBadRequest)
		return
	}
	if err := ensureDigestTargetSpace(r.Context(), dbOpts, spaceName, GetConfiguredLLM(r), GetConfiguredEmbedder(r)); err != nil {
		http.Error(w, "Failed to prepare target space: "+err.Error(), http.StatusInternalServerError)
		return
	}

	storageRoot := filepath.Join(os.TempDir(), "sop", "digest_uploads")
	if err := os.MkdirAll(storageRoot, 0o755); err != nil {
		http.Error(w, "Failed to prepare upload storage", http.StatusInternalServerError)
		return
	}

	storeName := sanitizeStoreName(header.Filename)
	storeDir := filepath.Join(storageRoot, storeName)
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		http.Error(w, "Failed to prepare digest store", http.StatusInternalServerError)
		return
	}

	storeFilePath := filepath.Join(storeDir, sanitizeFileName(header.Filename))
	if err := os.WriteFile(storeFilePath, data, 0o644); err != nil {
		http.Error(w, "Failed to persist uploaded digest file", http.StatusInternalServerError)
		return
	}

	vectorize := true
	if rawVectorize := strings.TrimSpace(r.FormValue("vectorize")); rawVectorize != "" {
		vectorize = strings.EqualFold(rawVectorize, "true") || strings.EqualFold(rawVectorize, "1") || strings.EqualFold(rawVectorize, "yes") || strings.EqualFold(rawVectorize, "on")
	}

	req := DigestSourceStoreRequest{
		SourceStore:     storeDir,
		TargetSpace:     spaceName,
		Template:        template,
		TopicHint:       topicHint,
		CustomPrompt:    customPrompt,
		ReadingStrategy: readingStrategy,
		Mode:            mode,
		Vectorize:       vectorize,
	}

	task := RegisterTask("DigestSourceStore", 100)
	UpdateTask(task.TaskID, "in_progress", 5, 100, "Preparing digest request...", "")

	go func(taskID string, digestReq DigestSourceStoreRequest, databaseName string) {
		defer func() {
			if rec := recover(); rec != nil {
				UpdateTask(taskID, "error", 0, 0, "", fmt.Sprintf("Digest panic: %v", rec))
			}
		}()

		ctx := context.Background()
		dbOpts, err := GetDBOptions(ctx, databaseName)
		if err != nil {
			UpdateTask(taskID, "error", 0, 0, "", fmt.Sprintf("Invalid database: %v", err))
			return
		}

		storagePath := ""
		if len(dbOpts.StoresFolders) > 0 {
			storagePath = dbOpts.StoresFolders[0]
		}
		if storagePath == "" {
			UpdateTask(taskID, "error", 0, 0, "", "Database storage path is unavailable")
			return
		}
		if err := os.MkdirAll(storagePath, 0o755); err != nil {
			UpdateTask(taskID, "error", 0, 0, "", fmt.Sprintf("Failed to prepare storage path: %v", err))
			return
		}

		UpdateTask(taskID, "in_progress", 20, 100, "Inspecting uploaded content...", "")
		digestReq.SourceStore = storeDir

		db := aidb.NewDatabase(dbOpts)
		sourceStorePath := digestReq.SourceStore
		sourceText, fileSummaries, err := collectDigestSourceText(sourceStorePath)
		if err != nil {
			UpdateTask(taskID, "error", 0, 0, "", fmt.Sprintf("Failed to inspect source store: %v", err))
			return
		}
		if strings.TrimSpace(sourceText) == "" || len(fileSummaries) == 0 {
			UpdateTask(taskID, "error", 0, 0, "", "No readable text content was found in the uploaded source store. Please choose a text-based file with readable content.")
			return
		}

		requestsPath := filepath.Join(storagePath, "source_store_digest_requests.json")
		var requests []DigestSourceStoreRequest
		if data, err := os.ReadFile(requestsPath); err == nil {
			if err := json.Unmarshal(data, &requests); err != nil {
				UpdateTask(taskID, "error", 0, 0, "", fmt.Sprintf("Failed to read digest request backlog: %v", err))
				return
			}
		} else if !os.IsNotExist(err) {
			UpdateTask(taskID, "error", 0, 0, "", fmt.Sprintf("Failed to read digest request backlog: %v", err))
			return
		}

		requests = append(requests, digestReq)
		payload, err := json.MarshalIndent(requests, "", "  ")
		if err != nil {
			UpdateTask(taskID, "error", 0, 0, "", fmt.Sprintf("Failed to encode digest request: %v", err))
			return
		}
		if err := os.WriteFile(requestsPath, append(payload, '\n'), 0o644); err != nil {
			UpdateTask(taskID, "error", 0, 0, "", fmt.Sprintf("Failed to persist digest request: %v", err))
			return
		}

		trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
		if err != nil {
			UpdateTask(taskID, "error", 0, 0, "", fmt.Sprintf("Failed to begin transaction: %v", err))
			return
		}
		defer trans.Rollback(ctx)

		dbEmbedder := GetConfiguredEmbedder(r)
		dbLLM := GetConfiguredLLM(r)
		kb, err := db.OpenKnowledgeBase(ctx, digestReq.TargetSpace, trans, dbLLM, dbEmbedder, false)
		if err != nil {
			UpdateTask(taskID, "error", 0, 0, "", fmt.Sprintf("Failed to open target space: %v", err))
			return
		}

		categoriesTree, err := kb.Store.Categories(ctx)
		if err != nil {
			UpdateTask(taskID, "error", 0, 0, "", fmt.Sprintf("Failed to access space categories: %v", err))
			return
		}

		categoryName := "Digest"
		if strings.TrimSpace(digestReq.Template) != "" {
			categoryName = strings.TrimSpace(digestReq.Template)
			categoryName = strings.ReplaceAll(categoryName, "-", " ")
			categoryName = strings.ToLower(categoryName)
			categoryName = strings.Title(categoryName)
			categoryName += " Digest"
		}
		var categoryID sop.UUID
		var category *memory.Category
		if ok, _ := categoriesTree.First(ctx); ok {
			for {
				current, err := categoriesTree.GetCurrentValue(ctx)
				if err == nil && current != nil && strings.EqualFold(strings.TrimSpace(current.Name), categoryName) {
					category = current
					categoryID = current.ID
					break
				}
				if ok, _ := categoriesTree.Next(ctx); !ok {
					break
				}
			}
		}
		if category == nil {
			newID := sop.NewUUID()
			category = &memory.Category{
				ID:              newID,
				Name:            categoryName,
				Path:            categoryName,
				Description:     "Auto-generated category for digest requests",
				SummaryMaxCount: MAX_ITEM_SUMMARIES,
			}
			if err := kb.UpsertCategories(ctx, []memory.UpsertCategoryParam{{Category: category}}); err != nil {
				UpdateTask(taskID, "error", 0, 0, "", fmt.Sprintf("Failed to create category: %v", err))
				return
			}
			categoryID = newID
		}

		UpdateTask(taskID, "in_progress", 40, 100, "Curating content...", "")

		var itemCount int
		for _, file := range fileSummaries {
			if file.Content == "" && file.Preview == "" {
				continue
			}
			if itemCount >= 5 {
				break
			}
			title, description := deriveDigestChunkContent(ctx, dbLLM, digestReq, file.Preview, itemCount)
			if title == "" {
				title = fmt.Sprintf("%s digest", filepath.Base(file.Path))
			}
			if description == "" {
				description = fmt.Sprintf("Curated from %s for %s", file.Path, digestReq.TargetSpace)
			}

			item := &memory.Item[map[string]any]{
				ID:         sop.NewUUID(),
				CategoryID: categoryID,
				Data: map[string]any{
					"title":        title,
					"description":  description,
					"content":      fmt.Sprintf("Source store: %s\nTemplate: %s\nTopic hint: %s\n%s", digestReq.SourceStore, digestReq.Template, digestReq.TopicHint, file.Preview),
					"source_store": digestReq.SourceStore,
					"target_space": digestReq.TargetSpace,
					"template":     digestReq.Template,
					"path":         file.Path,
				},
			}
			if err := kb.UpsertItems(ctx, []memory.UpsertItemParam[map[string]any]{{CategoryID: categoryID, Item: item}}); err != nil {
				UpdateTask(taskID, "error", 0, 0, "", fmt.Sprintf("Failed to add curated item: %v", err))
				return
			}
			itemCount++
		}

		if itemCount == 0 {
			item := &memory.Item[map[string]any]{
				ID:         sop.NewUUID(),
				CategoryID: categoryID,
				Data: map[string]any{
					"title":        fmt.Sprintf("Digest for %s", strings.TrimSpace(digestReq.TargetSpace)),
					"description":  fmt.Sprintf("Curated from %s using template %s", digestReq.SourceStore, digestReq.Template),
					"content":      sourceText,
					"source_store": digestReq.SourceStore,
					"target_space": digestReq.TargetSpace,
					"template":     digestReq.Template,
				},
			}
			if err := kb.UpsertItems(ctx, []memory.UpsertItemParam[map[string]any]{{CategoryID: categoryID, Item: item}}); err != nil {
				UpdateTask(taskID, "error", 0, 0, "", fmt.Sprintf("Failed to add curated item: %v", err))
				return
			}
		}

		if err := trans.Commit(ctx); err != nil {
			UpdateTask(taskID, "error", 0, 0, "", fmt.Sprintf("Commit failed: %v", err))
			return
		}

		UpdateTask(taskID, "completed", 100, 100, fmt.Sprintf("Digest completed for %s", digestReq.TargetSpace), "")
	}(task.TaskID, req, dbName)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"task_id": task.TaskID, "message": "Digest started in background"})
}

func handleDigestSourceStore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	dbName := r.URL.Query().Get("database")
	if dbName == "" {
		http.Error(w, "Database name is required", http.StatusBadRequest)
		return
	}

	var req DigestSourceStoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.SourceStore) == "" {
		http.Error(w, "Source store is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.TargetSpace) == "" {
		http.Error(w, "Target space is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	dbOpts, err := GetDBOptions(ctx, dbName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	storagePath := ""
	if len(dbOpts.StoresFolders) > 0 {
		storagePath = dbOpts.StoresFolders[0]
	}
	if storagePath == "" {
		http.Error(w, "Database storage path is unavailable", http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(storagePath, 0o755); err != nil {
		http.Error(w, "Failed to prepare storage path", http.StatusInternalServerError)
		return
	}

	db := aidb.NewDatabase(dbOpts)
	sourceStorePath := ""
	sourceText, fileSummaries, err := collectDigestSourceTextFromStore(ctx, db, req.SourceStore, storagePath)
	if strings.TrimSpace(sourceText) == "" {
		sourceStorePath = resolveSourceStorePath(storagePath, req.SourceStore)
		sourceText, fileSummaries, err = collectDigestSourceText(sourceStorePath)
	}
	if err != nil {
		http.Error(w, "Failed to inspect source store: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if strings.TrimSpace(sourceText) == "" || len(fileSummaries) == 0 {
		http.Error(w, "No readable text content found in the source store.", http.StatusBadRequest)
		return
	}

	requestsPath := filepath.Join(storagePath, "source_store_digest_requests.json")
	var requests []DigestSourceStoreRequest
	if data, err := os.ReadFile(requestsPath); err == nil {
		if err := json.Unmarshal(data, &requests); err != nil {
			http.Error(w, "Failed to read digest request backlog", http.StatusInternalServerError)
			return
		}
	} else if !os.IsNotExist(err) {
		http.Error(w, "Failed to read digest request backlog", http.StatusInternalServerError)
		return
	}

	requests = append(requests, req)
	payload, err := json.MarshalIndent(requests, "", "  ")
	if err != nil {
		http.Error(w, "Failed to encode digest request", http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(requestsPath, append(payload, '\n'), 0o644); err != nil {
		http.Error(w, "Failed to persist digest request", http.StatusInternalServerError)
		return
	}

	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
	if err != nil {
		http.Error(w, "Failed to begin transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer trans.Rollback(ctx)

	dbEmbedder := GetConfiguredEmbedder(r)
	dbLLM := GetConfiguredLLM(r)

	kb, err := db.OpenKnowledgeBase(ctx, req.TargetSpace, trans, dbLLM, dbEmbedder, false)
	if err != nil {
		http.Error(w, "Failed to open target space: "+err.Error(), http.StatusInternalServerError)
		return
	}

	categoriesTree, err := kb.Store.Categories(ctx)
	if err != nil {
		http.Error(w, "Failed to access space categories: "+err.Error(), http.StatusInternalServerError)
		return
	}

	categoryName := "Digest"
	if strings.TrimSpace(req.Template) != "" {
		categoryName = strings.TrimSpace(req.Template)
		categoryName = strings.ReplaceAll(categoryName, "-", " ")
		categoryName = strings.ToLower(categoryName)
		categoryName = strings.Title(categoryName)
		categoryName += " Digest"
	}
	var categoryID sop.UUID
	var category *memory.Category
	if ok, _ := categoriesTree.First(ctx); ok {
		for {
			current, err := categoriesTree.GetCurrentValue(ctx)
			if err == nil && current != nil && strings.EqualFold(strings.TrimSpace(current.Name), categoryName) {
				category = current
				categoryID = current.ID
				break
			}
			if ok, _ := categoriesTree.Next(ctx); !ok {
				break
			}
		}
	}
	if category == nil {
		newID := sop.NewUUID()
		category = &memory.Category{
			ID:              newID,
			Name:            categoryName,
			Path:            categoryName,
			Description:     "Auto-generated category for digest requests",
			SummaryMaxCount: MAX_ITEM_SUMMARIES,
		}
		categoryID, err = kb.Store.AddCategory(ctx, category)
		if err != nil {
			http.Error(w, "Failed to add digest category: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	var inspectionPlan *digestInspectionPlan
	if plan, ok := deriveDigestInspectionPlan(ctx, dbLLM, req, sourceText, fileSummaries); ok {
		inspectionPlan = &plan
	}

	prevState := map[string]digestCuratedItemState{}
	if data, err := os.ReadFile(filepath.Join(storagePath, "source_store_digest_state.json")); err == nil {
		if err := json.Unmarshal(data, &prevState); err != nil {
			prevState = map[string]digestCuratedItemState{}
		}
	}
	state := map[string]digestCuratedItemState{}
	shouldUseDelta := strings.EqualFold(strings.TrimSpace(req.Mode), "delta")
	var processingPlan []digestProcessingEntry
	var chunkItems []struct {
		chunk    string
		index    int
		title    string
		desc     string
		body     string
		sources  []string
		category string
	}
	if shouldUseDelta {
		processingPlan = buildDigestProcessingPlan(fileSummaries, prevState, req)
		if len(processingPlan) == 0 {
			processingPlan = []digestProcessingEntry{{File: digestFileSummary{Path: req.SourceStore, Content: sourceText}, NeedsLLM: true, SourceText: sourceText}}
		}
	} else {
		chunks := splitDigestChunksWithRequest(sourceText, req)
		if len(chunks) == 0 {
			chunks = []string{sourceText}
		}
		for idx, chunk := range chunks {
			title, description, body, sources, categoryPath := deriveDigestChunkContentWithDetails(ctx, dbLLM, req, chunk, idx, inspectionPlan)
			chunkItems = append(chunkItems, struct {
				chunk    string
				index    int
				title    string
				desc     string
				body     string
				sources  []string
				category string
			}{chunk: chunk, index: idx, title: title, desc: description, body: body, sources: sources, category: categoryPath})
		}
	}
	if shouldUseDelta {
		for idx, entry := range processingPlan {
			chunk := entry.SourceText
			if entry.CachedItem != nil {
				chunk = entry.CachedItem.Body
			}
			if strings.TrimSpace(chunk) == "" {
				chunk = entry.File.Preview
			}
			if strings.TrimSpace(chunk) == "" {
				chunk = sourceText
			}
			title, description, body, sources, categoryPath := "", "", "", []string(nil), ""
			if entry.NeedsLLM {
				title, description, body, sources, categoryPath = deriveDigestChunkContentWithDetails(ctx, dbLLM, req, chunk, idx, inspectionPlan)
			} else if entry.CachedItem != nil {
				title = entry.CachedItem.Title
				description = entry.CachedItem.Description
				body = entry.CachedItem.Body
				sources = append([]string(nil), entry.CachedItem.Sources...)
				categoryPath = entry.CachedItem.CategoryPath
			}
			if strings.TrimSpace(title) == "" {
				title = fmt.Sprintf("%s Note %d", strings.TrimSpace(req.Template), idx+1)
			}
			if strings.TrimSpace(description) == "" {
				description = fmt.Sprintf("Digest from %s", entry.File.Path)
			}
			itemData := map[string]any{
				"title":             title,
				"description":       description,
				"body":              body,
				"sources":           sources,
				"source_store":      req.SourceStore,
				"target_space":      req.TargetSpace,
				"template":          req.Template,
				"digest_generated":  true,
				"source_text":       chunk,
				"source_file_count": len(fileSummaries),
				"source_files":      fileSummaries,
				"chunk_index":       idx,
				"chunk_count":       len(processingPlan),
			}
			if strings.TrimSpace(req.TopicHint) != "" {
				itemData["topic_hint"] = req.TopicHint
			}
			if strings.TrimSpace(req.CustomPrompt) != "" {
				itemData["custom_prompt"] = req.CustomPrompt
			}
			if strings.TrimSpace(categoryPath) != "" {
				itemData["category_path"] = categoryPath
			}
			dataBytes, _ := json.Marshal(itemData)
			itemID := generateDeterministicID(categoryID, string(dataBytes))
			categoryTarget := categoryName
			if strings.TrimSpace(categoryPath) != "" {
				categoryTarget = categoryPath
			}
			newItem := memory.Item[map[string]any]{
				ID:         itemID,
				CategoryID: categoryID,
				Data:       cloneDataMap(itemData),
				Summaries: []string{
					title,
					fmt.Sprintf("Digest from %s", req.SourceStore),
					fmt.Sprintf("Template: %s", req.Template),
				},
			}
			if err := kb.Store.UpsertByCategoryPath(ctx, categoryTarget, newItem, nil); err != nil {
				http.Error(w, "Failed to insert digest item: "+err.Error(), http.StatusInternalServerError)
				return
			}
			if entry.CachedItem != nil {
				state[entry.File.Path] = *entry.CachedItem
				continue
			}
			state[entry.File.Path] = digestCuratedItemState{
				Path:         entry.File.Path,
				Hash:         entry.File.Hash,
				Size:         entry.File.Size,
				ModTime:      entry.File.ModTime,
				Title:        title,
				Description:  description,
				Body:         body,
				Sources:      sources,
				CategoryPath: categoryPath,
			}
		}
	} else {
		for _, item := range chunkItems {
			itemData := map[string]any{
				"title":             item.title,
				"description":       item.desc,
				"body":              item.body,
				"sources":           item.sources,
				"source_store":      req.SourceStore,
				"target_space":      req.TargetSpace,
				"template":          req.Template,
				"digest_generated":  true,
				"source_text":       item.chunk,
				"source_file_count": len(fileSummaries),
				"source_files":      fileSummaries,
				"chunk_index":       item.index,
				"chunk_count":       len(chunkItems),
			}
			if strings.TrimSpace(req.TopicHint) != "" {
				itemData["topic_hint"] = req.TopicHint
			}
			if strings.TrimSpace(req.CustomPrompt) != "" {
				itemData["custom_prompt"] = req.CustomPrompt
			}
			if strings.TrimSpace(item.category) != "" {
				itemData["category_path"] = item.category
			}
			dataBytes, _ := json.Marshal(itemData)
			itemID := generateDeterministicID(categoryID, string(dataBytes))
			categoryTarget := categoryName
			if strings.TrimSpace(item.category) != "" {
				categoryTarget = item.category
			}
			newItem := memory.Item[map[string]any]{
				ID:         itemID,
				CategoryID: categoryID,
				Data:       cloneDataMap(itemData),
				Summaries: []string{
					item.title,
					fmt.Sprintf("Digest from %s", req.SourceStore),
					fmt.Sprintf("Template: %s", req.Template),
				},
			}
			if err := kb.Store.UpsertByCategoryPath(ctx, categoryTarget, newItem, nil); err != nil {
				http.Error(w, "Failed to insert digest item: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}
	if cfg, err := kb.GetConfig(ctx); err == nil {
		if cfg == nil {
			cfg = &memory.KnowledgeBaseConfig{}
		}
		cfg.LastModified = time.Now().Unix()
		kb.SetConfig(ctx, cfg)
	}
	if err := trans.Commit(ctx); err != nil {
		http.Error(w, "Failed to commit digest transaction", http.StatusInternalServerError)
		return
	}

	stateBytes, err := json.MarshalIndent(state, "", "  ")
	if err == nil {
		_ = os.WriteFile(filepath.Join(storagePath, "source_store_digest_state.json"), append(stateBytes, '\n'), 0o644)
	}

	if req.Vectorize {
		if err := db.Vectorize(ctx, req.TargetSpace, dbLLM, dbEmbedder, 100); err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "Digest curated; vectorization completed with warnings", "warning": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "Digest curated and vectorized"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "Digest curated; vectorization skipped"})
}

func handleDeleteSpaceItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	storeName := r.URL.Query().Get("name")
	dbName := r.URL.Query().Get("database")
	if storeName == "" {
		http.Error(w, "Knowledge Base name is required", http.StatusBadRequest)
		return
	}

	var req deleteItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	parsedID, err := sop.ParseUUID(req.ID)
	if err != nil {
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	dbOpts, err := GetDBOptions(ctx, dbName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
	if err != nil {
		http.Error(w, "Failed to begin transaction", http.StatusInternalServerError)
		return
	}
	defer trans.Rollback(ctx)

	db := aidb.NewDatabase(dbOpts)
	dbEmbedder := GetConfiguredEmbedder(r)
	dbLLM := GetConfiguredLLM(r)

	kb, err := db.OpenKnowledgeBase(ctx, storeName, trans, dbLLM, dbEmbedder, false)
	if err != nil {
		http.Error(w, "Failed to open knowledge base", http.StatusInternalServerError)
		return
	}

	itemsTree, err := kb.Store.Items(ctx)
	if err != nil {
		http.Error(w, "Failed to access items", http.StatusInternalServerError)
		return
	}

	var foundKey memory.ItemKey
	var found bool

	if req.CategoryID != "" {
		parsedCatID, catErr := sop.ParseUUID(req.CategoryID)
		if catErr == nil {
			foundKey = memory.ItemKey{CategoryID: parsedCatID, ItemID: parsedID}
			if ok, _ := itemsTree.Find(ctx, foundKey, false); ok {
				found = true
			}
		}
	}

	// Fallback to sequential scan if CategoryID is missing or not found
	if !found {
		itemsTree.First(ctx)
		for {
			k := itemsTree.GetCurrentKey()
			if k.Key.ItemID == parsedID {
				foundKey = k.Key
				found = true
				break
			}
			if ok, _ := itemsTree.Next(ctx); !ok {
				break
			}
		}
	}

	if !found {
		http.Error(w, "Item not found", http.StatusNotFound)
		return
	}

	err = kb.DeleteItems(ctx, []memory.ItemKey{{CategoryID: foundKey.CategoryID, ItemID: foundKey.ItemID}})
	if err != nil {
		http.Error(w, "Failed to delete item", http.StatusInternalServerError)
		return
	}

	if err := trans.Commit(ctx); err != nil {
		http.Error(w, "Commit failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(addCategoryResponse{
		Success: true,
		Message: "Item deleted successfully",
	})
}

type batchResponse struct {
	Success bool     `json:"success"`
	Count   int      `json:"count"`
	IDs     []string `json:"ids"`
}

func handleAddSpaceItemsBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	storeName := r.URL.Query().Get("name")
	dbName := r.URL.Query().Get("database")
	if storeName == "" {
		http.Error(w, "Knowledge Base name is required", http.StatusBadRequest)
		return
	}

	var req AddSpaceItemsBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if len(req.Items) == 0 {
		http.Error(w, "No items provided", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	dbOpts, err := GetDBOptions(ctx, dbName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
	if err != nil {
		http.Error(w, "Failed to begin transaction", http.StatusInternalServerError)
		return
	}
	defer trans.Rollback(ctx)

	db := aidb.NewDatabase(dbOpts)
	dbEmbedder := GetConfiguredEmbedder(r)
	dbLLM := GetConfiguredLLM(r)

	kb, err := db.OpenKnowledgeBase(ctx, storeName, trans, dbLLM, dbEmbedder, false)
	if err != nil {
		http.Error(w, "Failed to open knowledge base", http.StatusInternalServerError)
		return
	}

	categoriesTree, _ := kb.Store.Categories(ctx)

	type itemState struct {
		OriginalReq  AddSpaceItemRequest
		ParsedCatID  sop.UUID
		CatName      string
		Item         memory.Item[map[string]any]
		ChunkStr     string
		MaxSummaries int
		Skip         bool
	}

	var states []itemState

	summarizer := GetSummarizer(config.ProductionMode, GetConfiguredLLMClient(r))

	for _, itm := range req.Items {
		parsedCatID, err := sop.ParseUUID(itm.CategoryID)
		if err != nil {
			http.Error(w, "Invalid category_id in batch", http.StatusBadRequest)
			return
		}

		found, cErr := categoriesTree.Find(ctx, parsedCatID, false)
		if cErr != nil || !found {
			http.Error(w, "Category not found", http.StatusNotFound)
			return
		}
		cat, _ := categoriesTree.GetCurrentValue(ctx)

		maxSummaries := cat.SummaryMaxCount
		if maxSummaries <= 0 {
			maxSummaries = MAX_ITEM_SUMMARIES
		}

		chunkStr := ""
		if chunk, ok := itm.Data["chunk"].(string); ok {
			chunkStr = chunk
		} else if textVal, ok := itm.Data["Text"].(string); ok {
			chunkStr = textVal
		}

		dataBytes, _ := json.Marshal(itm.Data)
		dataStr := string(dataBytes)

		detID := generateDeterministicID(parsedCatID, dataStr)
		item := memory.Item[map[string]any]{
			ID:         detID,
			DocID:      itm.DocID,
			CategoryID: parsedCatID,
			Data:       itm.Data,
		}
		if err := ensureDocumentReference(ctx, kb, &item, itm.Data); err != nil {
			http.Error(w, "Failed to persist document reference: "+err.Error(), http.StatusInternalServerError)
			return
		}
		itemsTree, err := kb.Store.Items(ctx)
		if err == nil {
			if found, _ := itemsTree.Find(ctx, memory.ItemKey{CategoryID: parsedCatID, ItemID: detID}, false); found {
				state := itemState{
					OriginalReq:  itm,
					ParsedCatID:  parsedCatID,
					CatName:      cat.Name,
					ChunkStr:     chunkStr,
					MaxSummaries: maxSummaries,
					Skip:         true,
					Item:         item,
				}
				states = append(states, state)
				continue
			}
		}

		summaries := DetermineSummaries(ctx, summarizer, itm.Summaries, chunkStr, dataStr, maxSummaries)

		state := itemState{
			OriginalReq:  itm,
			ParsedCatID:  parsedCatID,
			CatName:      cat.Name,
			ChunkStr:     chunkStr,
			MaxSummaries: maxSummaries,
			Skip:         false,
			Item: memory.Item[map[string]any]{
				ID:         detID,
				DocID:      item.DocID,
				CategoryID: parsedCatID,
				Summaries:  summaries,
				Data:       itm.Data,
			},
		}
		states = append(states, state)
	}

	var batchEmbedder *embed.Simple
	if config.ProductionMode {
		batchEmbedder = embed.NewSimple("simple_hash", 384, nil)
	}

	flatSummariesToEmbed := make([]string, 0)
	flatPositionsMap := make(map[int]struct{ stateIdx int })
	flatIdx := 0

	for i, s := range states {
		if len(s.OriginalReq.Positions) == 0 && config.ProductionMode {
			for _, summ := range s.Item.Summaries {
				flatSummariesToEmbed = append(flatSummariesToEmbed, summ)
				flatPositionsMap[flatIdx] = struct{ stateIdx int }{stateIdx: i}
				flatIdx++
			}
		}
	}

	var allEmbeddings [][]float32
	if len(flatSummariesToEmbed) > 0 && config.ProductionMode {
		// High-speed batched Embeddings call
		v, err := embed.DocumentTexts(ctx, batchEmbedder, flatSummariesToEmbed)
		if err != nil || len(v) != len(flatSummariesToEmbed) {
			http.Error(w, "Failed to embed item chunks in batch", http.StatusInternalServerError)
			return
		}
		allEmbeddings = v
	}

	var generatedIDs []string
	for i, s := range states {
		if s.Skip {
			generatedIDs = append(generatedIDs, s.Item.ID.String())
			continue
		}

		var vecs [][]float32
		if len(s.OriginalReq.Positions) > 0 {
			vecs = s.OriginalReq.Positions
		} else if config.ProductionMode {
			for j, emb := range allEmbeddings {
				if mapping, ok := flatPositionsMap[j]; ok && mapping.stateIdx == i {
					vecs = append(vecs, emb)
				}
			}
		} else {
			vecs = make([][]float32, len(s.Item.Summaries))
			for k := range s.Item.Summaries {
				vecs[k] = []float32{0.4, 0.5, 0.6}
			}
		}

		err = kb.Store.UpsertByCategoryPath(ctx, s.CatName, s.Item, vecs)
		if err != nil {
			http.Error(w, "Failed to insert batch item: "+err.Error(), http.StatusInternalServerError)
			return
		}
		generatedIDs = append(generatedIDs, s.Item.ID.String())
	}

	if cfg, err := kb.GetConfig(ctx); err == nil {
		if cfg == nil {
			cfg = &memory.KnowledgeBaseConfig{}
		}
		cfg.LastModified = time.Now().Unix()
		kb.SetConfig(ctx, cfg)
	}

	if err := trans.Commit(ctx); err != nil {
		http.Error(w, "Commit failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(batchResponse{
		Success: true,
		Count:   len(generatedIDs),
		IDs:     generatedIDs,
	})
}

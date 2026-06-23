package main

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	aidb "github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/memory"
	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/database"
)

func TestUpdateSpaceItemPersists(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_update_item"
	spaceName := "test_space_update"

	config = Config{
		RootPassword: "secret_password",
		Databases: []DatabaseConfig{{
			Name: dbName,
			Path: tmpDir,
			Mode: "standalone",
		}},
	}

	sendJSON := func(method, url string, handler http.HandlerFunc, body interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler(rr, req)
		return rr
	}

	rr := sendJSON("POST", "/api/spaces/create", handleCreateSpace, CreateSpaceRequest{DatabaseName: dbName, SpaceName: spaceName})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to create space: %v", rr.Body.String())
	}

	rr = sendJSON("POST", "/api/spaces/category/add?database="+dbName+"&name="+spaceName, handleAddSpaceCategory, AddSpaceCategoryRequest{Name: "Root"})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add category: %v", rr.Body.String())
	}
	var catRes map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&catRes); err != nil {
		t.Fatalf("Failed to decode category response: %v", err)
	}
	categoryID := catRes["id"].(string)

	rr = sendJSON("POST", "/api/spaces/item/add?database="+dbName+"&name="+spaceName, handleAddSpaceItem, AddSpaceItemRequest{
		CategoryID: categoryID,
		Summaries:  []string{"Old summary"},
		Data:       map[string]any{"text": "Old text", "description": "Old text"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add item: %v", rr.Body.String())
	}
	var addRes map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&addRes); err != nil {
		t.Fatalf("Failed to decode add item response: %v", err)
	}
	itemID := addRes["id"].(string)

	rr = sendJSON("POST", "/api/spaces/item/update?database="+dbName+"&name="+spaceName, handleUpdateSpaceItem, UpdateSpaceItemRequest{
		ID:         itemID,
		CategoryID: categoryID,
		Summaries:  []string{"Updated summary"},
		Data:       map[string]any{"chunk": "Updated text", "text": "Updated text", "description": "Updated text"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("Update returned wrong status: got %v body %s", rr.Code, rr.Body.String())
	}

	req, _ := http.NewRequest("GET", "/api/spaces/items?database="+dbName+"&name="+spaceName, nil)
	rrGet := httptest.NewRecorder()
	handleListSpaceItems(rrGet, req)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("List returned wrong status: got %v body %s", rrGet.Code, rrGet.Body.String())
	}

	var listRes map[string]any
	if err := json.NewDecoder(rrGet.Body).Decode(&listRes); err != nil {
		t.Fatalf("Failed to decode list response: %v", err)
	}
	items := listRes["data"].([]any)
	if len(items) != 1 {
		t.Fatalf("Expected 1 item after update, got %d", len(items))
	}
	item := items[0].(map[string]any)
	if got := item["description"]; got != "Updated text" {
		t.Fatalf("Expected updated description to persist, got %v", got)
	}
	if got := item["text"]; got != "Updated text" {
		t.Fatalf("Expected updated text to persist, got %v", got)
	}
	if got := item["summaries"]; got == nil {
		t.Fatalf("Expected updated summaries to persist")
	}
}

func TestListSpaceItemsExposesContentType(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_list_content_type"
	spaceName := "test_space_list_content_type"

	config = Config{
		RootPassword: "secret_password",
		Databases: []DatabaseConfig{{
			Name: dbName,
			Path: tmpDir,
			Mode: "standalone",
		}},
	}

	sendJSON := func(method, url string, handler http.HandlerFunc, body interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler(rr, req)
		return rr
	}

	rr := sendJSON("POST", "/api/spaces/create", handleCreateSpace, CreateSpaceRequest{DatabaseName: dbName, SpaceName: spaceName})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to create space: %v", rr.Body.String())
	}

	rr = sendJSON("POST", "/api/spaces/category/add?database="+dbName+"&name="+spaceName, handleAddSpaceCategory, AddSpaceCategoryRequest{Name: "Root"})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add category: %v", rr.Body.String())
	}
	var catRes map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&catRes); err != nil {
		t.Fatalf("Failed to decode category response: %v", err)
	}
	categoryID := catRes["id"].(string)

	rr = sendJSON("POST", "/api/spaces/item/add?database="+dbName+"&name="+spaceName, handleAddSpaceItem, AddSpaceItemRequest{
		CategoryID: categoryID,
		Summaries:  []string{"Media summary"},
		Data: map[string]any{
			"text":         "Media reference",
			"description":  "A media asset",
			"content_type": "image/png",
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add item: %v", rr.Body.String())
	}

	req, _ := http.NewRequest("GET", "/api/spaces/items?database="+dbName+"&name="+spaceName, nil)
	rrGet := httptest.NewRecorder()
	handleListSpaceItems(rrGet, req)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("List returned wrong status: got %v body %s", rrGet.Code, rrGet.Body.String())
	}

	var listRes map[string]any
	if err := json.NewDecoder(rrGet.Body).Decode(&listRes); err != nil {
		t.Fatalf("Failed to decode list response: %v", err)
	}
	items := listRes["data"].([]any)
	if len(items) != 1 {
		t.Fatalf("Expected 1 item after add, got %d", len(items))
	}
	item := items[0].(map[string]any)
	if got := item["content_type"]; got != "image/png" {
		t.Fatalf("Expected content_type to be surfaced in list response, got %v", got)
	}
}

func TestUpdateSpaceItemRequiresCategoryID(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_update_no_cat"
	spaceName := "test_space_update_no_cat"

	config = Config{
		RootPassword: "secret_password",
		Databases: []DatabaseConfig{{
			Name: dbName,
			Path: tmpDir,
			Mode: "standalone",
		}},
	}

	sendJSON := func(method, url string, handler http.HandlerFunc, body interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler(rr, req)
		return rr
	}

	rr := sendJSON("POST", "/api/spaces/create", handleCreateSpace, CreateSpaceRequest{DatabaseName: dbName, SpaceName: spaceName})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to create space: %v", rr.Body.String())
	}

	rr = sendJSON("POST", "/api/spaces/category/add?database="+dbName+"&name="+spaceName, handleAddSpaceCategory, AddSpaceCategoryRequest{Name: "Root"})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add category: %v", rr.Body.String())
	}
	var catRes map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&catRes); err != nil {
		t.Fatalf("Failed to decode category response: %v", err)
	}
	categoryID := catRes["id"].(string)

	rr = sendJSON("POST", "/api/spaces/item/add?database="+dbName+"&name="+spaceName, handleAddSpaceItem, AddSpaceItemRequest{
		CategoryID: categoryID,
		Summaries:  []string{"Old summary"},
		Data:       map[string]any{"text": "Old text", "description": "Old text"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add item: %v", rr.Body.String())
	}
	var addRes map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&addRes); err != nil {
		t.Fatalf("Failed to decode add item response: %v", err)
	}
	itemID := addRes["id"].(string)

	rr = sendJSON("POST", "/api/spaces/item/update?database="+dbName+"&name="+spaceName, handleUpdateSpaceItem, UpdateSpaceItemRequest{
		ID:        itemID,
		Summaries: []string{"Updated summary"},
		Data:      map[string]any{"chunk": "Updated text", "text": "Updated text", "description": "Updated text"},
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("Expected missing CategoryID to fail with 400, got %v body %s", rr.Code, rr.Body.String())
	}
}

func TestHandleUpdateSpaceItemPersistsAppleCloudDocumentReference(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_update_apple_cloud_doc"
	spaceName := "test_space_update_apple_cloud_doc"

	config = Config{
		RootPassword: "secret_password",
		Databases: []DatabaseConfig{{
			Name: dbName,
			Path: tmpDir,
			Mode: "standalone",
		}},
	}

	sendJSON := func(method, url string, handler http.HandlerFunc, body interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler(rr, req)
		return rr
	}

	rr := sendJSON("POST", "/api/spaces/create", handleCreateSpace, CreateSpaceRequest{DatabaseName: dbName, SpaceName: spaceName})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to create space: %v", rr.Body.String())
	}

	rr = sendJSON("POST", "/api/spaces/category/add?database="+dbName+"&name="+spaceName, handleAddSpaceCategory, AddSpaceCategoryRequest{Name: "Root"})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add category: %v", rr.Body.String())
	}
	var catRes map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&catRes); err != nil {
		t.Fatalf("Failed to decode category response: %v", err)
	}
	categoryID := catRes["id"].(string)

	rr = sendJSON("POST", "/api/spaces/item/add?database="+dbName+"&name="+spaceName, handleAddSpaceItem, AddSpaceItemRequest{
		CategoryID: categoryID,
		Summaries:  []string{"Initial summary"},
		Data:       map[string]any{"text": "Initial text", "description": "Initial description"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add item: %v", rr.Body.String())
	}
	var addRes map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&addRes); err != nil {
		t.Fatalf("failed to decode add-item response: %v", err)
	}
	itemID := addRes["id"].(string)

	rr = sendJSON("POST", "/api/spaces/item/update?database="+dbName+"&name="+spaceName, handleUpdateSpaceItem, UpdateSpaceItemRequest{
		ID:         itemID,
		CategoryID: categoryID,
		Summaries:  []string{"Updated Apple cloud reference"},
		Data: map[string]any{
			"chunk":          "Updated Apple cloud reference",
			"text":           "Updated Apple cloud reference",
			"description":    "A document stored in Apple Cloud",
			"content_type":   "text/plain",
			"storage_mode":   string(memory.DocumentStorageModeExternalMetadataOnly),
			"payload_ref":    "applecloud://container/notes.txt",
			"cloud_provider": "apple_cloud",
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to update Apple Cloud-backed item: %v", rr.Body.String())
	}

	ctx := context.Background()
	dbOpts, err := getDBOptions(ctx, dbName)
	if err != nil {
		t.Fatalf("Failed to get DB options: %v", err)
	}
	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer trans.Rollback(ctx)

	db := aidb.NewDatabase(dbOpts)
	kb, err := db.OpenKnowledgeBase(ctx, spaceName, trans, nil, nil, false)
	if err != nil {
		t.Fatalf("Failed to open knowledge base: %v", err)
	}

	itemsTree, err := kb.Store.Items(ctx)
	if err != nil {
		t.Fatalf("Failed to access items tree: %v", err)
	}
	parsedCatID, err := sop.ParseUUID(categoryID)
	if err != nil {
		t.Fatalf("failed to parse category ID: %v", err)
	}
	itemIDUUID, err := sop.ParseUUID(itemID)
	if err != nil {
		t.Fatalf("failed to parse item ID: %v", err)
	}
	found, err := itemsTree.Find(ctx, memory.ItemKey{CategoryID: parsedCatID, ItemID: itemIDUUID}, false)
	if err != nil {
		t.Fatalf("failed to locate stored item: %v", err)
	}
	if !found {
		t.Fatal("expected the updated item to be stored")
	}
	item, err := itemsTree.GetCurrentValue(ctx)
	if err != nil {
		t.Fatalf("failed to read stored item: %v", err)
	}
	if got, ok := item.Data["doc_id"].(string); !ok || got == "" {
		t.Fatalf("expected updated item payload to include the persisted document reference, got %#v", item.Data["doc_id"])
	}
	uid, err := sop.ParseUUID(item.Data["doc_id"].(string))
	if err != nil {
		t.Fatalf("failed to parse stored doc id: %v", err)
	}
	doc, err := kb.Store.GetDocument(ctx, uid)
	if err != nil {
		t.Fatalf("failed to retrieve document: %v", err)
	}
	if doc == nil {
		t.Fatal("expected a document to be created for the updated Apple Cloud-backed item")
	}
	if doc.StorageMode != memory.DocumentStorageModeExternalMetadataOnly {
		t.Fatalf("expected external metadata-only storage mode, got %q", doc.StorageMode)
	}
	if doc.PayloadRef != "applecloud://container/notes.txt" {
		t.Fatalf("expected Apple Cloud payload reference to be preserved, got %q", doc.PayloadRef)
	}
}

func TestHandleAddSpaceItemPersistsAppleCloudDocumentReference(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_apple_cloud_doc"
	spaceName := "test_space_apple_cloud_doc"

	config = Config{
		RootPassword: "secret_password",
		Databases: []DatabaseConfig{{
			Name: dbName,
			Path: tmpDir,
			Mode: "standalone",
		}},
	}

	sendJSON := func(method, url string, handler http.HandlerFunc, body interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler(rr, req)
		return rr
	}

	rr := sendJSON("POST", "/api/spaces/create", handleCreateSpace, CreateSpaceRequest{DatabaseName: dbName, SpaceName: spaceName})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to create space: %v", rr.Body.String())
	}

	rr = sendJSON("POST", "/api/spaces/category/add?database="+dbName+"&name="+spaceName, handleAddSpaceCategory, AddSpaceCategoryRequest{Name: "Root"})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add category: %v", rr.Body.String())
	}
	var catRes map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&catRes); err != nil {
		t.Fatalf("Failed to decode category response: %v", err)
	}
	categoryID := catRes["id"].(string)

	reqBody := AddSpaceItemRequest{
		CategoryID: categoryID,
		Summaries:  []string{"Apple cloud reference"},
		Data: map[string]any{
			"text":           "Apple cloud reference",
			"description":    "A document stored in Apple Cloud",
			"content_type":   "text/plain",
			"storage_mode":   string(memory.DocumentStorageModeExternalMetadataOnly),
			"payload_ref":    "applecloud://container/notes.txt",
			"cloud_provider": "apple_cloud",
		},
	}

	rr = sendJSON("POST", "/api/spaces/item/add?database="+dbName+"&name="+spaceName, handleAddSpaceItem, reqBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add Apple Cloud-backed item: %v", rr.Body.String())
	}

	ctx := context.Background()
	dbOpts, err := getDBOptions(ctx, dbName)
	if err != nil {
		t.Fatalf("Failed to get DB options: %v", err)
	}
	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer trans.Rollback(ctx)

	db := aidb.NewDatabase(dbOpts)
	kb, err := db.OpenKnowledgeBase(ctx, spaceName, trans, nil, nil, false)
	if err != nil {
		t.Fatalf("Failed to open knowledge base: %v", err)
	}

	itemsTree, err := kb.Store.Items(ctx)
	if err != nil {
		t.Fatalf("Failed to access items tree: %v", err)
	}
	parsedCatID, err := sop.ParseUUID(categoryID)
	if err != nil {
		t.Fatalf("failed to parse category ID: %v", err)
	}
	var addRes map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&addRes); err != nil {
		t.Fatalf("failed to decode add-item response: %v", err)
	}
	itemID, err := sop.ParseUUID(addRes["id"].(string))
	if err != nil {
		t.Fatalf("failed to parse item ID: %v", err)
	}
	found, err := itemsTree.Find(ctx, memory.ItemKey{CategoryID: parsedCatID, ItemID: itemID}, false)
	if err != nil {
		t.Fatalf("failed to locate stored item: %v", err)
	}
	if !found {
		t.Fatal("expected the item to be stored")
	}
	item, err := itemsTree.GetCurrentValue(ctx)
	if err != nil {
		t.Fatalf("failed to read stored item: %v", err)
	}
	if got, ok := item.Data["doc_id"].(string); !ok || got == "" {
		t.Fatalf("expected item payload to include the persisted document reference, got %#v", item.Data["doc_id"])
	}

	uid, err := sop.ParseUUID(item.Data["doc_id"].(string))
	if err != nil {
		t.Fatalf("failed to parse stored doc id: %v", err)
	}
	doc, err := kb.Store.GetDocument(ctx, uid)
	if err != nil {
		t.Fatalf("failed to retrieve document: %v", err)
	}
	if doc == nil {
		t.Fatal("expected a document to be created for the Apple Cloud-backed item")
	}
	if doc.StorageMode != memory.DocumentStorageModeExternalMetadataOnly {
		t.Fatalf("expected external metadata-only storage mode, got %q", doc.StorageMode)
	}
	if doc.PayloadRef != "applecloud://container/notes.txt" {
		t.Fatalf("expected Apple Cloud payload reference to be preserved, got %q", doc.PayloadRef)
	}
	if !doc.IsAppleCloud() {
		t.Fatalf("expected document to be classified as Apple Cloud-backed")
	}
}

func TestHandleAddSpaceItemMultipartUploadPersistsUploadedBytes(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_multipart_upload"
	spaceName := "test_space_multipart_upload"

	config = Config{
		RootPassword: "secret_password",
		Databases: []DatabaseConfig{{
			Name: dbName,
			Path: tmpDir,
			Mode: "standalone",
		}},
	}

	rr := httptest.NewRecorder()
	createReq := httptest.NewRequest("POST", "/api/spaces/create", bytes.NewBufferString(`{"database_name":"`+dbName+`","space_name":"`+spaceName+`"}`))
	createReq.Header.Set("Content-Type", "application/json")
	handleCreateSpace(rr, createReq)
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to create space: %v", rr.Body.String())
	}

	rr = httptest.NewRecorder()
	catReq := httptest.NewRequest("POST", "/api/spaces/category/add?database="+dbName+"&name="+spaceName, bytes.NewBufferString(`{"name":"Root"}`))
	catReq.Header.Set("Content-Type", "application/json")
	handleAddSpaceCategory(rr, catReq)
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add category: %v", rr.Body.String())
	}
	var catRes map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&catRes); err != nil {
		t.Fatalf("Failed to decode category response: %v", err)
	}
	categoryID := catRes["id"].(string)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "sample.txt")
	if err != nil {
		t.Fatalf("failed to create form file part: %v", err)
	}
	if _, err := part.Write([]byte("hello from upload")); err != nil {
		t.Fatalf("failed to write uploaded bytes: %v", err)
	}
	for _, field := range []struct{ key, value string }{
		{key: "category_id", value: categoryID},
		{key: "description", value: "Uploaded file"},
		{key: "summaries", value: "Upload summary"},
		{key: "storage_mode", value: string(memory.DocumentStorageModeFilesystemReference)},
		{key: "cloud_provider", value: "filesystem"},
		{key: "filename", value: "sample.txt"},
	} {
		if err := writer.WriteField(field.key, field.value); err != nil {
			t.Fatalf("failed to write form field %s: %v", field.key, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	rr = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/spaces/item/add?database="+dbName+"&name="+spaceName, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	handleAddSpaceItem(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add uploaded item: %v", rr.Body.String())
	}

	ctx := context.Background()
	dbOpts, err := getDBOptions(ctx, dbName)
	if err != nil {
		t.Fatalf("Failed to get DB options: %v", err)
	}
	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer trans.Rollback(ctx)

	db := aidb.NewDatabase(dbOpts)
	kb, err := db.OpenKnowledgeBase(ctx, spaceName, trans, nil, nil, false)
	if err != nil {
		t.Fatalf("Failed to open knowledge base: %v", err)
	}

	itemsTree, err := kb.Store.Items(ctx)
	if err != nil {
		t.Fatalf("Failed to access items tree: %v", err)
	}
	var addRes map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&addRes); err != nil {
		t.Fatalf("failed to decode add-item response: %v", err)
	}
	itemID, err := sop.ParseUUID(addRes["id"].(string))
	if err != nil {
		t.Fatalf("failed to parse item ID: %v", err)
	}
	parsedCatID, err := sop.ParseUUID(categoryID)
	if err != nil {
		t.Fatalf("failed to parse category ID: %v", err)
	}
	found, err := itemsTree.Find(ctx, memory.ItemKey{CategoryID: parsedCatID, ItemID: itemID}, false)
	if err != nil {
		t.Fatalf("failed to locate stored item: %v", err)
	}
	if !found {
		t.Fatal("expected the item to be stored")
	}
	item, err := itemsTree.GetCurrentValue(ctx)
	if err != nil {
		t.Fatalf("failed to read stored item: %v", err)
	}
	if got, ok := item.Data["doc_id"].(string); !ok || got == "" {
		t.Fatalf("expected item payload to include the persisted document reference, got %#v", item.Data["doc_id"])
	}
	uid, err := sop.ParseUUID(item.Data["doc_id"].(string))
	if err != nil {
		t.Fatalf("failed to parse stored doc id: %v", err)
	}
	doc, err := kb.Store.GetDocument(ctx, uid)
	if err != nil {
		t.Fatalf("failed to retrieve document: %v", err)
	}
	if doc == nil {
		t.Fatal("expected a document to be created for the uploaded item")
	}
	if doc.PayloadRef != "uploaded://sample.txt" {
		t.Fatalf("expected uploaded payload reference to be preserved, got %q", doc.PayloadRef)
	}
	if string(doc.Data) != "hello from upload" {
		t.Fatalf("expected uploaded bytes to be persisted, got %q", string(doc.Data))
	}
}

func TestHandleAddSpaceItemPersistsFilesystemDocumentReference(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_filesystem_doc"
	spaceName := "test_space_filesystem_doc"

	config = Config{
		RootPassword: "secret_password",
		Databases: []DatabaseConfig{{
			Name: dbName,
			Path: tmpDir,
			Mode: "standalone",
		}},
	}

	sendJSON := func(method, url string, handler http.HandlerFunc, body interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler(rr, req)
		return rr
	}

	rr := sendJSON("POST", "/api/spaces/create", handleCreateSpace, CreateSpaceRequest{DatabaseName: dbName, SpaceName: spaceName})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to create space: %v", rr.Body.String())
	}

	rr = sendJSON("POST", "/api/spaces/category/add?database="+dbName+"&name="+spaceName, handleAddSpaceCategory, AddSpaceCategoryRequest{Name: "Root"})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add category: %v", rr.Body.String())
	}
	var catRes map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&catRes); err != nil {
		t.Fatalf("Failed to decode category response: %v", err)
	}
	categoryID := catRes["id"].(string)

	filesystemRef := "/tmp/inbox/report.pdf"
	reqBody := AddSpaceItemRequest{
		CategoryID: categoryID,
		Summaries:  []string{"Filesystem reference"},
		Data: map[string]any{
			"text":           "Filesystem reference",
			"description":    "A document stored on the local filesystem",
			"content_type":   "application/pdf",
			"filesystem_ref": filesystemRef,
		},
	}

	rr = sendJSON("POST", "/api/spaces/item/add?database="+dbName+"&name="+spaceName, handleAddSpaceItem, reqBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add filesystem-backed item: %v", rr.Body.String())
	}

	ctx := context.Background()
	dbOpts, err := getDBOptions(ctx, dbName)
	if err != nil {
		t.Fatalf("Failed to get DB options: %v", err)
	}
	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer trans.Rollback(ctx)

	db := aidb.NewDatabase(dbOpts)
	kb, err := db.OpenKnowledgeBase(ctx, spaceName, trans, nil, nil, false)
	if err != nil {
		t.Fatalf("Failed to open knowledge base: %v", err)
	}

	itemsTree, err := kb.Store.Items(ctx)
	if err != nil {
		t.Fatalf("Failed to access items tree: %v", err)
	}
	var addRes map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&addRes); err != nil {
		t.Fatalf("failed to decode add-item response: %v", err)
	}
	itemID, err := sop.ParseUUID(addRes["id"].(string))
	if err != nil {
		t.Fatalf("failed to parse item ID: %v", err)
	}
	parsedCatID, err := sop.ParseUUID(categoryID)
	if err != nil {
		t.Fatalf("failed to parse category ID: %v", err)
	}
	found, err := itemsTree.Find(ctx, memory.ItemKey{CategoryID: parsedCatID, ItemID: itemID}, false)
	if err != nil {
		t.Fatalf("failed to locate stored item: %v", err)
	}
	if !found {
		t.Fatal("expected the item to be stored")
	}
	item, err := itemsTree.GetCurrentValue(ctx)
	if err != nil {
		t.Fatalf("failed to read stored item: %v", err)
	}
	if got, ok := item.Data["doc_id"].(string); !ok || got == "" {
		t.Fatalf("expected item payload to include the persisted document reference, got %#v", item.Data["doc_id"])
	}
	uid, err := sop.ParseUUID(item.Data["doc_id"].(string))
	if err != nil {
		t.Fatalf("failed to parse stored doc id: %v", err)
	}
	doc, err := kb.Store.GetDocument(ctx, uid)
	if err != nil {
		t.Fatalf("failed to retrieve document: %v", err)
	}
	if doc == nil {
		t.Fatal("expected a document to be created for the filesystem-backed item")
	}
	if doc.StorageMode != memory.DocumentStorageModeFilesystemReference {
		t.Fatalf("expected filesystem-reference storage mode, got %q", doc.StorageMode)
	}
	if doc.PayloadRef != filesystemRef {
		t.Fatalf("expected filesystem payload reference to be preserved, got %q", doc.PayloadRef)
	}
	if !doc.IsFilesystemReference() {
		t.Fatalf("expected document to be classified as a filesystem-backed reference")
	}
}

func TestHandleListSpaceItemsReturnsDocumentReferenceMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_list_document_ref"
	spaceName := "test_space_list_document_ref"

	config = Config{
		RootPassword: "secret_password",
		Databases: []DatabaseConfig{{
			Name: dbName,
			Path: tmpDir,
			Mode: "standalone",
		}},
	}

	sendJSON := func(method, url string, handler http.HandlerFunc, body interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler(rr, req)
		return rr
	}

	rr := sendJSON("POST", "/api/spaces/create", handleCreateSpace, CreateSpaceRequest{DatabaseName: dbName, SpaceName: spaceName})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to create space: %v", rr.Body.String())
	}

	rr = sendJSON("POST", "/api/spaces/category/add?database="+dbName+"&name="+spaceName, handleAddSpaceCategory, AddSpaceCategoryRequest{Name: "Root"})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add category: %v", rr.Body.String())
	}
	var catRes map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&catRes); err != nil {
		t.Fatalf("Failed to decode category response: %v", err)
	}
	categoryID := catRes["id"].(string)

	reqBody := AddSpaceItemRequest{
		CategoryID: categoryID,
		Summaries:  []string{"Filesystem reference"},
		Data: map[string]any{
			"text":           "Filesystem reference",
			"description":    "A document stored on the local filesystem",
			"content_type":   "application/pdf",
			"filesystem_ref": "/tmp/inbox/report.pdf",
		},
	}

	rr = sendJSON("POST", "/api/spaces/item/add?database="+dbName+"&name="+spaceName, handleAddSpaceItem, reqBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add item: %v", rr.Body.String())
	}

	req, _ := http.NewRequest("GET", "/api/spaces/items?database="+dbName+"&name="+spaceName, nil)
	rrGet := httptest.NewRecorder()
	handleListSpaceItems(rrGet, req)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("List returned wrong status: got %v body %s", rrGet.Code, rrGet.Body.String())
	}

	var listRes map[string]any
	if err := json.NewDecoder(rrGet.Body).Decode(&listRes); err != nil {
		t.Fatalf("Failed to decode list response: %v", err)
	}
	items := listRes["data"].([]any)
	if len(items) != 1 {
		t.Fatalf("Expected 1 item after add, got %d", len(items))
	}
	item := items[0].(map[string]any)
	if item["payload_ref"] != "/tmp/inbox/report.pdf" {
		t.Fatalf("expected payload_ref to be exposed in list response, got %#v", item["payload_ref"])
	}
	if item["storage_mode"] != string(memory.DocumentStorageModeFilesystemReference) {
		t.Fatalf("expected storage_mode to be exposed in list response, got %#v", item["storage_mode"])
	}
}

func TestHandleListSpaceItemsFallsBackToFilesystemRef(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_list_filesystem_ref_fallback"
	spaceName := "test_space_list_filesystem_ref_fallback"

	config = Config{
		RootPassword: "secret_password",
		Databases: []DatabaseConfig{{
			Name: dbName,
			Path: tmpDir,
			Mode: "standalone",
		}},
	}

	sendJSON := func(method, url string, handler http.HandlerFunc, body interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler(rr, req)
		return rr
	}

	rr := sendJSON("POST", "/api/spaces/create", handleCreateSpace, CreateSpaceRequest{DatabaseName: dbName, SpaceName: spaceName})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to create space: %v", rr.Body.String())
	}

	rr = sendJSON("POST", "/api/spaces/category/add?database="+dbName+"&name="+spaceName, handleAddSpaceCategory, AddSpaceCategoryRequest{Name: "Root"})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add category: %v", rr.Body.String())
	}
	var catRes map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&catRes); err != nil {
		t.Fatalf("Failed to decode category response: %v", err)
	}
	categoryID := catRes["id"].(string)

	reqBody := AddSpaceItemRequest{
		CategoryID: categoryID,
		Summaries:  []string{"Filesystem fallback"},
		Data: map[string]any{
			"text":           "Filesystem fallback",
			"description":    "A filesystem-backed item without payload_ref",
			"content_type":   "application/pdf",
			"filesystem_ref": "/tmp/inbox/report.pdf",
		},
	}

	rr = sendJSON("POST", "/api/spaces/item/add?database="+dbName+"&name="+spaceName, handleAddSpaceItem, reqBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add item: %v", rr.Body.String())
	}

	req, _ := http.NewRequest("GET", "/api/spaces/items?database="+dbName+"&name="+spaceName, nil)
	rrGet := httptest.NewRecorder()
	handleListSpaceItems(rrGet, req)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("List returned wrong status: got %v body %s", rrGet.Code, rrGet.Body.String())
	}

	var listRes map[string]any
	if err := json.NewDecoder(rrGet.Body).Decode(&listRes); err != nil {
		t.Fatalf("Failed to decode list response: %v", err)
	}
	items := listRes["data"].([]any)
	if len(items) != 1 {
		t.Fatalf("Expected 1 item after add, got %d", len(items))
	}
	item := items[0].(map[string]any)
	if item["payload_ref"] != "/tmp/inbox/report.pdf" {
		t.Fatalf("expected filesystem_ref fallback to be exposed in list response, got %#v", item["payload_ref"])
	}
}

func TestHandleAddSpaceCategory_NormalizesChildPath(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_trim_child_path"
	spaceName := "test_space_trim_child_path"

	config = Config{
		RootPassword: "secret_password",
		Databases: []DatabaseConfig{{
			Name: dbName,
			Path: tmpDir,
			Mode: "standalone",
		}},
	}

	sendJSON := func(method, url string, handler http.HandlerFunc, body interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler(rr, req)
		return rr
	}

	rr := sendJSON("POST", "/api/spaces/create", handleCreateSpace, CreateSpaceRequest{DatabaseName: dbName, SpaceName: spaceName})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to create space: %v", rr.Body.String())
	}

	rr = sendJSON("POST", "/api/spaces/category/add?database="+dbName+"&name="+spaceName, handleAddSpaceCategory, AddSpaceCategoryRequest{Name: "Root A"})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add root category: %v", rr.Body.String())
	}
	var rootRes map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&rootRes); err != nil {
		t.Fatalf("Failed to decode root category response: %v", err)
	}
	rootID := rootRes["id"].(string)

	rr = sendJSON("POST", "/api/spaces/category/add?database="+dbName+"&name="+spaceName, handleAddSpaceCategory, AddSpaceCategoryRequest{
		Name:     " Child A1 ",
		ParentID: rootID,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add child category: %v", rr.Body.String())
	}
	var childRes map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&childRes); err != nil {
		t.Fatalf("Failed to decode child category response: %v", err)
	}
	childID := childRes["id"].(string)

	ctx := context.Background()
	dbOpts, err := getDBOptions(ctx, dbName)
	if err != nil {
		t.Fatalf("getDBOptions failed: %v", err)
	}
	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForReading)
	if err != nil {
		t.Fatalf("BeginTransaction failed: %v", err)
	}
	defer trans.Rollback(ctx)

	db := aidb.NewDatabase(dbOpts)
	kb, err := db.OpenKnowledgeBase(ctx, spaceName, trans, nil, nil, false)
	if err != nil {
		t.Fatalf("OpenKnowledgeBase failed: %v", err)
	}

	cats, err := kb.Store.Categories(ctx)
	if err != nil {
		t.Fatalf("Categories failed: %v", err)
	}

	childUUID, err := sop.ParseUUID(childID)
	if err != nil {
		t.Fatalf("ParseUUID failed: %v", err)
	}

	found, err := cats.Find(ctx, childUUID, false)
	if err != nil {
		t.Fatalf("Find child category failed: %v", err)
	}
	if !found {
		t.Fatal("expected child category to exist")
	}

	cat, err := cats.GetCurrentValue(ctx)
	if err != nil {
		t.Fatalf("GetCurrentValue failed: %v", err)
	}
	if got, want := cat.Path, "Root A / Child A1"; got != want {
		t.Fatalf("child category path = %q, want %q", got, want)
	}
}

func TestListSpaceCategoryAndItems(t *testing.T) {
	// Setup temp dir for DB
	tmpDir := t.TempDir()
	dbName := "testdb_cats"
	spaceName := "test_space"

	// Setup global config
	config = Config{
		RootPassword: "secret_password",
		Databases: []DatabaseConfig{
			{
				Name: dbName,
				Path: tmpDir,
				Mode: "standalone",
			},
		},
	}

	// Helper to send HTTP requests to handlers
	sendJSON := func(method, url string, handler http.HandlerFunc, body interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler(rr, req)
		return rr
	}

	// 1. Create Space
	rr := sendJSON("POST", "/api/spaces/create", handleCreateSpace, CreateSpaceRequest{
		DatabaseName: dbName,
		SpaceName:    spaceName,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to create space: %v", rr.Body.String())
	}

	// 2. Add Root A Category
	rr = sendJSON("POST", "/api/spaces/category/add?database="+dbName+"&name="+spaceName, handleAddSpaceCategory, AddSpaceCategoryRequest{
		Name: "Root A",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add Root A")
	}
	var res map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&res)
	rootAID := res["id"].(string)

	// 3. Add Root B Category
	rr = sendJSON("POST", "/api/spaces/category/add?database="+dbName+"&name="+spaceName, handleAddSpaceCategory, AddSpaceCategoryRequest{
		Name: "Root B",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add Root B")
	}
	rrBody := rr.Body.String()
	res = make(map[string]interface{})
	json.Unmarshal([]byte(rrBody), &res)
	rootBID := res["id"].(string)

	// 4. Add Child A1 Category
	rr = sendJSON("POST", "/api/spaces/category/add?database="+dbName+"&name="+spaceName, handleAddSpaceCategory, AddSpaceCategoryRequest{
		Name:     "Child A1",
		ParentID: rootAID,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add Child A1: %v", rr.Body.String())
	}
	res = make(map[string]interface{})
	json.NewDecoder(rr.Body).Decode(&res)
	childA1ID := res["id"].(string)

	// 5. Add Item 1 to Child A1
	rr = sendJSON("POST", "/api/spaces/item/add?database="+dbName+"&name="+spaceName, handleAddSpaceItem, AddSpaceItemRequest{
		CategoryID: childA1ID,
		Summaries:  []string{"Summary 1"},
		Data:       map[string]interface{}{"text": "Item 1 Text"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add Item 1: %v", rr.Body.String())
	}

	// 6. Add Item 2 to Root B
	rr = sendJSON("POST", "/api/spaces/item/add?database="+dbName+"&name="+spaceName, handleAddSpaceItem, AddSpaceItemRequest{
		CategoryID: rootBID,
		Summaries:  []string{"Summary 2"},
		Data:       map[string]interface{}{"text": "Item 2 Text"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add Item 2: %v", rr.Body.String())
	}

	// --- Phase 2: Queries --- //

	// Test 1: HTTP List Root Categories (parent_id is empty)
	req, _ := http.NewRequest("GET", "/api/spaces/categories?database="+dbName+"&name="+spaceName+"&parent_id=", nil)
	rrGet := httptest.NewRecorder()
	handleListSpaceCategories(rrGet, req)

	if status := rrGet.Code; status != http.StatusOK {
		t.Logf("List categories returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var rootResp map[string]interface{}
	err := json.NewDecoder(rrGet.Body).Decode(&rootResp)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	rootResponse := rootResp["data"].([]interface{})

	// Since we inserted Root A and Root B, we should get exactly 2
	if len(rootResponse) < 2 {
		t.Logf("Expected at least 2 root categories, got %d", len(rootResponse))
	}

	for _, c := range rootResponse {
		cat := c.(map[string]interface{})
		children, ok := cat["children"].([]interface{})

		// The fix in category_items.go ensures "children" is strictly an array instead of null
		if !ok {
			t.Logf("Expected category %s 'children' to be an array, got %v", cat["name"], cat["children"])
		}

		if cat["id"].(string) == rootBID && len(children) != 0 {
			t.Logf("Expected Root B to have an empty array for children, got length %d", len(children))
		}
		if cat["id"].(string) == rootAID && len(children) == 0 {
			t.Logf("Expected Root A to have children, got none.")
		}
	}

	// Test 2: HTTP List Child Categories
	req, _ = http.NewRequest("GET", "/api/spaces/categories?database="+dbName+"&name="+spaceName+"&parent_id="+rootAID, nil)
	rrGet = httptest.NewRecorder()
	handleListSpaceCategories(rrGet, req)

	var childResp map[string]interface{}
	json.NewDecoder(rrGet.Body).Decode(&childResp)
	childResponse := childResp["data"].([]interface{})

	if len(childResponse) < 1 {
		t.Fatalf("Expected at least 1 child category, got %d", len(childResponse))
	}
	if childResponse[0].(map[string]interface{})["id"].(string) != childA1ID {
		t.Logf("Expected Child A1 ID %s, got %v", childA1ID, childResponse[0].(map[string]interface{})["id"])
	}

	// Test 3: HTTP List Items by explicit CategoryID (fixes the UI filtering bug)

	// Items in Root B
	req, _ = http.NewRequest("GET", "/api/spaces/items?database="+dbName+"&name="+spaceName+"&category_id="+rootBID, nil)
	rrGet = httptest.NewRecorder()
	handleListSpaceItems(rrGet, req)

	var itemsResponseB map[string]interface{}
	json.NewDecoder(rrGet.Body).Decode(&itemsResponseB)

	itemsB := itemsResponseB["data"].([]interface{})
	if len(itemsB) != 1 {
		t.Logf("Expected 1 item in Root B, got %d", len(itemsB))
	}
	if item, ok := itemsB[0].(map[string]interface{}); !ok || item["category_id"] == nil || item["category_id"].(string) != rootBID {
		t.Fatalf("Expected items response to include category_id=%s for UI update flow, got %#v", rootBID, itemsB[0])
	}

	// Items in Child A1
	req, _ = http.NewRequest("GET", "/api/spaces/items?database="+dbName+"&name="+spaceName+"&category_id="+childA1ID, nil)
	rrGet = httptest.NewRecorder()
	handleListSpaceItems(rrGet, req)

	var itemsResponseA1 map[string]interface{}
	json.NewDecoder(rrGet.Body).Decode(&itemsResponseA1)

	itemsA1 := itemsResponseA1["data"].([]interface{})
	if len(itemsA1) != 1 {
		t.Logf("Expected 1 item in Child A1, got %d", len(itemsA1))
	}
}

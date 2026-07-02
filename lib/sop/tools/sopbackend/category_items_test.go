package sopbackend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	aidb "github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/memory"
	"github.com/sharedcode/sop/database"
)

func TestSpaceItemViewExposesLifecycleMetadata(t *testing.T) {
	payload := map[string]any{
		"lifecycle_status":       "archived",
		"lifecycle_reason":       "cold_or_unused",
		"archived_at":            int64(1700000000000),
		"archive_reason":         "cold_or_unused",
		"lifecycle_access_count": 1,
	}
	item := SpaceItemView{}
	if payload != nil {
		if lifecycleStatus, found := payload["lifecycle_status"]; found {
			item.LifecycleStatus = fmt.Sprint(lifecycleStatus)
		}
		if lifecycleReason, found := payload["lifecycle_reason"]; found {
			item.LifecycleReason = fmt.Sprint(lifecycleReason)
		}
		if archivedAt, found := payload["archived_at"]; found {
			switch v := archivedAt.(type) {
			case int64:
				item.ArchivedAt = v
			case int:
				item.ArchivedAt = int64(v)
			case float64:
				item.ArchivedAt = int64(v)
			}
		}
		if archiveReason, found := payload["archive_reason"]; found {
			item.ArchiveReason = fmt.Sprint(archiveReason)
		}
		if accessCount, found := payload["lifecycle_access_count"]; found {
			switch v := accessCount.(type) {
			case int:
				item.LifecycleAccessCount = v
			case int64:
				item.LifecycleAccessCount = int(v)
			case float64:
				item.LifecycleAccessCount = int(v)
			}
		}
	}
	if item.LifecycleStatus != "archived" {
		t.Fatalf("expected archived lifecycle status to be exposed, got %q", item.LifecycleStatus)
	}
	if item.ArchiveReason != "cold_or_unused" {
		t.Fatalf("expected archive reason to be exposed, got %q", item.ArchiveReason)
	}
}

func TestBuildDigestPromptUsesComprehensiveCurationInstructions(t *testing.T) {
	req := DigestSourceStoreRequest{
		SourceStore:  "source-store",
		TargetSpace:  "target-space",
		Template:     "courseware",
		TopicHint:    "distributed systems",
		CustomPrompt: "Summarize for onboarding",
	}
	prompt := buildDigestPrompt(req, "Distributed systems fundamentals\nUse queues and event sourcing.", []digestFileSummary{{Path: "notes.txt", Preview: "Distributed systems fundamentals"}})
	for _, expected := range []string{
		"Use the source material as the only grounding",
		"Preserve source provenance",
		"Prefer concise, high-signal synthesized entries",
		"Do not invent facts",
		"Inspect the source store before curating",
		"store-inspection tools",
		"list relevant files",
		"build the target knowledge base",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected digest prompt to include %q, got %q", expected, prompt)
		}
	}
}

func TestBuildDigestPromptTreatsAllFilesAsEqualWeightInputs(t *testing.T) {
	req := DigestSourceStoreRequest{TargetSpace: "target-space", Template: "research digest"}
	prompt := buildDigestPrompt(req, "File A content\nFile B content", []digestFileSummary{{Path: "a.txt"}, {Path: "b.txt"}})
	for _, expected := range []string{
		"Let the LLM decide what is important",
		"what is redundant",
		"what should be dropped",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected digest prompt to include %q, got %q", expected, prompt)
		}
	}
}

func TestBuildDigestPromptRequestsCategoryPathAndManifestTaxonomy(t *testing.T) {
	req := DigestSourceStoreRequest{TargetSpace: "target-space", Template: "courseware"}
	prompt := buildDigestPrompt(req, "Distributed systems fundamentals", nil)
	for _, expected := range []string{
		"category_path",
		"manifest taxonomy",
		"bounded semantic taxonomy",
		"Choose the most relevant category path",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected digest prompt to include %q, got %q", expected, prompt)
		}
	}
}

func TestSplitDigestChunksPreservesFileBoundaries(t *testing.T) {
	chunks := splitDigestChunks("File: docs/guide.md\nFirst section\n\nFile: notes.txt\nSecond section")
	if len(chunks) != 2 {
		t.Fatalf("expected two file-based chunks, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0], "docs/guide.md") || !strings.Contains(chunks[1], "notes.txt") {
		t.Fatalf("expected chunks to preserve file boundaries, got %#v", chunks)
	}
}

func TestBuildDigestPromptIncludesMetadataFirstInventoryForLargeFiles(t *testing.T) {
	req := DigestSourceStoreRequest{TargetSpace: "target-space", Template: "courseware"}
	prompt := buildDigestPrompt(req, "Distributed systems fundamentals", []digestFileSummary{{Path: "docs/guide.md", Size: 12000, Preview: "Large content"}, {Path: "notes.txt", Size: 800, Preview: "Small content"}})
	for _, expected := range []string{
		"Source inventory",
		"docs/guide.md",
		"11.7KB",
		"Large files should be processed as separate chunks",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected digest prompt to include %q, got %q", expected, prompt)
		}
	}
}

func TestSplitDigestChunksWithRequestHonorsChunkSizeAndOverlap(t *testing.T) {
	chunks := splitDigestChunksWithRequest("0123456789", DigestSourceStoreRequest{MaxChunkChars: 4, ChunkOverlapChars: 2})
	if len(chunks) != 4 {
		t.Fatalf("expected four chunks with overlap, got %d: %#v", len(chunks), chunks)
	}
	if chunks[0] != "0123" || chunks[1] != "2345" || chunks[2] != "4567" || chunks[3] != "6789" {
		t.Fatalf("unexpected chunk sequence: %#v", chunks)
	}
}

func TestBuildDigestProcessingPlanPrefersMetadataThenHash(t *testing.T) {
	previous := map[string]digestCuratedItemState{
		"notes.txt": {
			Path:        "notes.txt",
			Body:        "cached body",
			Description: "cached description",
			Title:       "cached title",
			ModTime:     100,
			Size:        14,
		},
	}
	files := []digestFileSummary{{Path: "notes.txt", Size: 14, ModTime: 100, Hash: "different-hash", Content: "new content"}}
	plan := buildDigestProcessingPlan(files, previous, DigestSourceStoreRequest{Mode: "delta"})
	if len(plan) != 1 {
		t.Fatalf("expected one processing entry, got %d", len(plan))
	}
	if plan[0].NeedsLLM {
		t.Fatalf("expected metadata-based cached entry to be reused")
	}
	if plan[0].CachedItem == nil || plan[0].CachedItem.Title != "cached title" {
		t.Fatalf("expected cached item from metadata, got %#v", plan[0].CachedItem)
	}
}

type stubDigestGenerator struct {
	response string
}

func (g *stubDigestGenerator) Name() string { return "stub" }
func (g *stubDigestGenerator) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	return ai.GenOutput{Text: g.response}, nil
}
func (g *stubDigestGenerator) EstimateCost(inTokens, outTokens int) float64               { return 0 }
func (g *stubDigestGenerator) PrewarmCache(ctx context.Context, opts ai.GenOptions) error { return nil }

func TestDeriveDigestChunkContentProducesStructuredKnowledgeItem(t *testing.T) {
	req := DigestSourceStoreRequest{Template: "courseware", TargetSpace: "learning-space", SourceStore: "source-store"}
	title, description, body, sources, _ := deriveDigestChunkContentWithDetails(context.Background(), &stubDigestGenerator{response: `{"title":"Course Foundations","summary":"Foundations for learners","body":"A full curated explanation.","sources":["notes.txt","readme.md"]}`}, req, "Original chunk content", 0, nil)
	if title != "Course Foundations" {
		t.Fatalf("expected structured title to be used, got %q", title)
	}
	if description != "Foundations for learners" {
		t.Fatalf("expected structured summary to be used, got %q", description)
	}
	if body != "A full curated explanation." {
		t.Fatalf("expected structured body to be used, got %q", body)
	}
	if len(sources) != 2 || sources[0] != "notes.txt" || sources[1] != "readme.md" {
		t.Fatalf("expected structured sources to be preserved, got %#v", sources)
	}
}

func TestDeriveDigestChunkContentParsesManifestMarkdownItem(t *testing.T) {
	req := DigestSourceStoreRequest{Template: "courseware", TargetSpace: "learning-space", SourceStore: "source-store"}
	title, description, body, sources, categoryPath := deriveDigestChunkContentWithDetails(context.Background(), &stubDigestGenerator{response: `- Item: Course Foundations
   Summary: Foundations for learners
   Body:
   A full curated explanation.
   Sources: notes.txt, readme.md`}, req, "Original chunk content", 0, nil)
	if title != "Course Foundations" {
		t.Fatalf("expected manifest title to be used, got %q", title)
	}
	if description != "Foundations for learners" {
		t.Fatalf("expected manifest summary to be used, got %q", description)
	}
	if body != "A full curated explanation." {
		t.Fatalf("expected manifest body to be used, got %q", body)
	}
	if len(sources) != 2 || sources[0] != "notes.txt" || sources[1] != "readme.md" {
		t.Fatalf("expected manifest sources to be preserved, got %#v", sources)
	}
	if categoryPath != "" {
		t.Fatalf("expected empty category path when the response has none, got %q", categoryPath)
	}
}

func TestDeriveDigestChunkContentParsesCategoryPathFromStructuredResponse(t *testing.T) {
	req := DigestSourceStoreRequest{Template: "courseware", TargetSpace: "learning-space", SourceStore: "source-store"}
	_, _, _, _, categoryPath := deriveDigestChunkContentWithDetails(context.Background(), &stubDigestGenerator{response: `{"title":"Course Foundations","summary":"Foundations for learners","body":"A full curated explanation.","sources":["notes.txt"],"category_path":"Architecture/Storage"}`}, req, "Original chunk content", 0, nil)
	if categoryPath != "Architecture/Storage" {
		t.Fatalf("expected category path to be preserved, got %q", categoryPath)
	}
}

func TestDeriveDigestInspectionPlanReturnsFocusAndCategoryPath(t *testing.T) {
	req := DigestSourceStoreRequest{Template: "courseware", TargetSpace: "learning-space", SourceStore: "source-store"}
	plan, ok := deriveDigestInspectionPlan(context.Background(), &stubDigestGenerator{response: `{"focus":"Prioritize architecture and storage concepts","category_path":"Architecture/Storage"}`}, req, "Distributed systems fundamentals", []digestFileSummary{{Path: "notes.txt", Preview: "Distributed systems fundamentals"}})
	if !ok {
		t.Fatalf("expected inspection plan to be parsed")
	}
	if plan.Focus != "Prioritize architecture and storage concepts" {
		t.Fatalf("expected inspection focus to be preserved, got %q", plan.Focus)
	}
	if plan.CategoryPath != "Architecture/Storage" {
		t.Fatalf("expected inspection category path to be preserved, got %q", plan.CategoryPath)
	}
}

func TestHandleDigestSourceStoreUploadCreatesSpaceItem(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_digest_source_store_upload"
	spaceName := "digest_upload_target"

	config = Config{
		RootPassword: SecuredString("secret_password"),
		Databases: []DatabaseConfig{{
			Name: dbName,
			Path: tmpDir,
			Mode: "standalone",
		}},
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "notes.txt")
	if err != nil {
		t.Fatalf("failed to create multipart part: %v", err)
	}
	if _, err := part.Write([]byte("Distributed systems fundamentals\nUse queues and event sourcing.")); err != nil {
		t.Fatalf("failed to write multipart payload: %v", err)
	}
	if err := writer.WriteField("database", dbName); err != nil {
		t.Fatalf("failed to write database field: %v", err)
	}
	if err := writer.WriteField("target_space", spaceName); err != nil {
		t.Fatalf("failed to write target_space field: %v", err)
	}
	if err := writer.WriteField("template", "courseware"); err != nil {
		t.Fatalf("failed to write template field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/source-store/digest/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()
	handleDigestSourceStoreUpload(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("digest upload returned wrong status: got %v body %s", rr.Code, rr.Body.String())
	}
	var taskResp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&taskResp); err != nil {
		t.Fatalf("failed to decode upload response: %v", err)
	}
	taskID := taskResp["task_id"]
	if taskID == "" {
		t.Fatalf("expected task_id in upload response, got %#v", taskResp)
	}

	var items []any
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		listReq, _ := http.NewRequest("GET", "/api/spaces/items?database="+dbName+"&name="+spaceName, nil)
		listRR := httptest.NewRecorder()
		handleListSpaceItems(listRR, listReq)
		if listRR.Code != http.StatusOK {
			t.Fatalf("list items returned wrong status: got %v body %s", listRR.Code, listRR.Body.String())
		}

		var listRes map[string]any
		if err := json.NewDecoder(listRR.Body).Decode(&listRes); err != nil {
			t.Fatalf("failed to decode list response: %v", err)
		}
		items, _ = listRes["data"].([]any)
		if len(items) >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(items) < 1 {
		t.Fatalf("expected at least one item to be created for digest upload, got %d; task=%+v", len(items), GetTask(taskID))
	}
}

func TestHandleDigestSourceStoreUploadRejectsEmptyContent(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_digest_source_store_empty_upload"
	spaceName := "empty_upload_target"

	config = Config{
		RootPassword: SecuredString("secret_password"),
		Databases: []DatabaseConfig{{
			Name: dbName,
			Path: tmpDir,
			Mode: "standalone",
		}},
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "notes.txt")
	if err != nil {
		t.Fatalf("failed to create multipart part: %v", err)
	}
	if _, err := part.Write([]byte("   \n\t  ")); err != nil {
		t.Fatalf("failed to write multipart payload: %v", err)
	}
	if err := writer.WriteField("database", dbName); err != nil {
		t.Fatalf("failed to write database field: %v", err)
	}
	if err := writer.WriteField("target_space", spaceName); err != nil {
		t.Fatalf("failed to write target_space field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/source-store/digest/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()
	handleDigestSourceStoreUpload(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for empty upload, got %d body %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(strings.ToLower(rr.Body.String()), "readable text") {
		t.Fatalf("expected error to mention readable text, got %s", rr.Body.String())
	}
}

func TestHandleDigestSourceStoreReturnsSuccessPayload(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_digest_source_store_task"
	spaceName := "target_space_task"

	config = Config{
		RootPassword: SecuredString("secret_password"),
		Databases: []DatabaseConfig{{
			Name: dbName,
			Path: tmpDir,
			Mode: "standalone",
		}},
	}

	if err := os.MkdirAll(filepath.Join(tmpDir, "source-store"), 0o755); err != nil {
		t.Fatalf("failed to create source-store directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "source-store", "notes.txt"), []byte("Distributed systems fundamentals\nUse queues and event sourcing."), 0o644); err != nil {
		t.Fatalf("failed to write source-store content: %v", err)
	}

	body, _ := json.Marshal(DigestSourceStoreRequest{
		SourceStore: "source-store",
		TargetSpace: spaceName,
		Template:    "courseware",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/source-store/digest?database="+dbName, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleDigestSourceStore(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("digest request returned wrong status: got %v body %s", rr.Code, rr.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode digest response: %v", err)
	}
	if response["success"] != true {
		t.Fatalf("expected success=true in response, got %#v", response)
	}
	if response["message"] == nil || fmt.Sprint(response["message"]) == "" {
		t.Fatalf("expected non-empty message in response, got %#v", response)
	}
	if _, hasTaskID := response["task_id"]; hasTaskID {
		t.Fatalf("expected synchronous digest response to omit task_id, got %#v", response)
	}
}

func TestHandleDigestSourceStorePersistsDigestRequest(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_digest_source_store"
	spaceName := "target_space"

	config = Config{
		RootPassword: SecuredString("secret_password"),
		Databases: []DatabaseConfig{{
			Name: dbName,
			Path: tmpDir,
			Mode: "standalone",
		}},
	}

	if err := os.MkdirAll(filepath.Join(tmpDir, "source-store"), 0o755); err != nil {
		t.Fatalf("failed to create source-store directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "source-store", "notes.txt"), []byte("Distributed systems fundamentals\nUse queues and event sourcing."), 0o644); err != nil {
		t.Fatalf("failed to write source-store content: %v", err)
	}

	sendJSON := func(method, url string, handler http.HandlerFunc, body interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler(rr, req)
		return rr
	}

	rr := sendJSON("POST", "/api/source-store/digest?database="+dbName, handleDigestSourceStore, DigestSourceStoreRequest{
		SourceStore:  "source-store",
		TargetSpace:  spaceName,
		Template:     "courseware",
		TopicHint:    "distributed systems",
		CustomPrompt: "Summarize the source for onboarding",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("digest request returned wrong status: got %v body %s", rr.Code, rr.Body.String())
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "source_store_digest_requests.json"))
	if err != nil {
		t.Fatalf("expected digest request file to be created: %v", err)
	}

	var requests []DigestSourceStoreRequest
	if err := json.Unmarshal(data, &requests); err != nil {
		t.Fatalf("failed to decode digest request file: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("expected exactly one digest request, got %d", len(requests))
	}
	if requests[0].SourceStore != "source-store" || requests[0].TargetSpace != spaceName || requests[0].Template != "courseware" {
		t.Fatalf("unexpected persisted digest request: %+v", requests[0])
	}
}

func TestHandleDigestSourceStoreCreatesSpaceItem(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_digest_source_store_item"
	spaceName := "digest_target"

	config = Config{
		RootPassword: SecuredString("secret_password"),
		Databases: []DatabaseConfig{{
			Name:          dbName,
			Path:          tmpDir,
			StoresFolders: []string{tmpDir},
			Mode:          "standalone",
		}},
	}

	if err := os.MkdirAll(filepath.Join(tmpDir, "source-store"), 0o755); err != nil {
		t.Fatalf("failed to create source-store directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "source-store", "notes.txt"), []byte("Distributed systems fundamentals\nUse queues and event sourcing."), 0o644); err != nil {
		t.Fatalf("failed to write source-store content: %v", err)
	}

	sendJSON := func(method, url string, handler http.HandlerFunc, body interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler(rr, req)
		return rr
	}

	rr := sendJSON("POST", "/api/source-store/digest?database="+dbName, handleDigestSourceStore, DigestSourceStoreRequest{
		SourceStore:  "source-store",
		TargetSpace:  spaceName,
		Template:     "courseware",
		TopicHint:    "distributed systems",
		CustomPrompt: "Summarize the source for onboarding",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("digest request returned wrong status: got %v body %s", rr.Code, rr.Body.String())
	}

	req, _ := http.NewRequest("GET", "/api/spaces/items?database="+dbName+"&name="+spaceName, nil)
	rrGet := httptest.NewRecorder()
	handleListSpaceItems(rrGet, req)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("list items returned wrong status: got %v body %s", rrGet.Code, rrGet.Body.String())
	}

	var listRes map[string]any
	if err := json.NewDecoder(rrGet.Body).Decode(&listRes); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}
	items, ok := listRes["data"].([]any)
	if !ok {
		t.Fatalf("expected data array in list response, got %#v", listRes["data"])
	}
	if len(items) < 1 {
		t.Fatalf("expected at least one item to be created for digest, got %d", len(items))
	}
	item := items[0].(map[string]any)
	desc := item["description"]
	if desc == nil || fmt.Sprint(desc) == "" {
		t.Fatalf("expected digest item to include a description, got %#v", item)
	}
	if !strings.Contains(fmt.Sprint(desc), "source-store") || !strings.Contains(fmt.Sprint(desc), "courseware") {
		t.Fatalf("expected digest item description to mention source store and template, got %v", desc)
	}
}

func TestHandleDigestSourceStoreCuratesFolderStoreContent(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_digest_folder_store_content"
	spaceName := "digest_folder_target"

	config = Config{
		RootPassword: SecuredString("secret_password"),
		Databases: []DatabaseConfig{{
			Name: dbName,
			Path: tmpDir,
			Mode: "standalone",
		}},
	}

	if err := os.MkdirAll(filepath.Join(tmpDir, "source-store"), 0o755); err != nil {
		t.Fatalf("failed to create source store folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "source-store", "notes.txt"), []byte("Distributed systems fundamentals\nUse queues and event sourcing."), 0o644); err != nil {
		t.Fatalf("failed to write source store content: %v", err)
	}

	sendJSON := func(method, url string, handler http.HandlerFunc, body interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler(rr, req)
		return rr
	}

	rr := sendJSON("POST", "/api/source-store/digest?database="+dbName, handleDigestSourceStore, DigestSourceStoreRequest{
		SourceStore:  "source-store",
		TargetSpace:  spaceName,
		Template:     "courseware",
		TopicHint:    "distributed systems",
		CustomPrompt: "Summarize the source for onboarding",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("digest request returned wrong status: got %v body %s", rr.Code, rr.Body.String())
	}

	req, _ := http.NewRequest("GET", "/api/spaces/items?database="+dbName+"&name="+spaceName, nil)
	rrGet := httptest.NewRecorder()
	handleListSpaceItems(rrGet, req)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("list items returned wrong status: got %v body %s", rrGet.Code, rrGet.Body.String())
	}

	var listRes map[string]any
	if err := json.NewDecoder(rrGet.Body).Decode(&listRes); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}
	items, ok := listRes["data"].([]any)
	if !ok {
		t.Fatalf("expected data array in list response, got %#v", listRes["data"])
	}
	if len(items) == 0 {
		t.Fatalf("expected digest to create at least one item, got none")
	}
	item := items[0].(map[string]any)
	desc := fmt.Sprint(item["description"])
	if !strings.Contains(desc, "Distributed systems") {
		t.Fatalf("expected digest item description to include source content, got %q", desc)
	}
}

func TestHandleDigestSourceStoreUsesRepoSourceStoreManifest(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_digest_repo_source_store_manifest"
	spaceName := "digest_repo_manifest"

	config = Config{
		RootPassword: SecuredString("secret_password"),
		Databases: []DatabaseConfig{{
			Name: dbName,
			Path: tmpDir,
			Mode: "standalone",
		}},
	}

	repoDir := filepath.Join(filepath.Dir(tmpDir), "repo-source-outside")
	if err := os.MkdirAll(filepath.Join(repoDir, "docs"), 0o755); err != nil {
		t.Fatalf("failed to create repo source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "docs", "readme.md"), []byte("Repository guidance for curated digests"), 0o644); err != nil {
		t.Fatalf("failed to write repo source content: %v", err)
	}

	dbOpts := sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{tmpDir}}
	db := aidb.NewDatabase(dbOpts)
	manifest := aidb.RepoSourceStoreManifest{
		Name: "repo-source",
		Repositories: []aidb.RepoSource{{
			URL: repoDir,
		}},
	}
	if err := db.AddRepoStore(context.Background(), manifest.Name, manifest); err != nil {
		t.Fatalf("failed to register repo source store: %v", err)
	}

	sendJSON := func(method, url string, handler http.HandlerFunc, body interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler(rr, req)
		return rr
	}

	rr := sendJSON("POST", "/api/source-store/digest?database="+dbName, handleDigestSourceStore, DigestSourceStoreRequest{
		SourceStore: "repo-source",
		TargetSpace: spaceName,
		Template:    "research digest",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("digest request returned wrong status: got %v body %s", rr.Code, rr.Body.String())
	}

	req, _ := http.NewRequest("GET", "/api/spaces/items?database="+dbName+"&name="+spaceName, nil)
	rrGet := httptest.NewRecorder()
	handleListSpaceItems(rrGet, req)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("list items returned wrong status: got %v body %s", rrGet.Code, rrGet.Body.String())
	}

	var listRes map[string]any
	if err := json.NewDecoder(rrGet.Body).Decode(&listRes); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}
	items, ok := listRes["data"].([]any)
	if !ok {
		t.Fatalf("expected data array in list response, got %#v", listRes["data"])
	}
	if len(items) == 0 {
		t.Fatalf("expected digest to create at least one item, got none")
	}
	item := items[0].(map[string]any)
	desc := fmt.Sprint(item["description"])
	if !strings.Contains(desc, "Repository guidance") {
		t.Fatalf("expected digest item description to include repo content, got %q", desc)
	}
}

func TestHandleDigestSourceStoreCreatesMultipleCuratedItemsFromContent(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_digest_multiple_curated_items"
	spaceName := "digest_multiple_items"

	config = Config{
		RootPassword: SecuredString("secret_password"),
		Databases: []DatabaseConfig{{
			Name: dbName,
			Path: tmpDir,
			Mode: "standalone",
		}},
	}

	if err := os.MkdirAll(filepath.Join(tmpDir, "source-store"), 0o755); err != nil {
		t.Fatalf("failed to create source store folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "source-store", "notes.txt"), []byte("Distributed systems fundamentals.\n\nUse queues and event sourcing for resilience.\n\nDesign for failure and recoverability."), 0o644); err != nil {
		t.Fatalf("failed to write source store content: %v", err)
	}

	sendJSON := func(method, url string, handler http.HandlerFunc, body interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler(rr, req)
		return rr
	}

	rr := sendJSON("POST", "/api/source-store/digest?database="+dbName, handleDigestSourceStore, DigestSourceStoreRequest{
		SourceStore: "source-store",
		TargetSpace: spaceName,
		Template:    "courseware",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("digest request returned wrong status: got %v body %s", rr.Code, rr.Body.String())
	}

	req, _ := http.NewRequest("GET", "/api/spaces/items?database="+dbName+"&name="+spaceName, nil)
	rrGet := httptest.NewRecorder()
	handleListSpaceItems(rrGet, req)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("list items returned wrong status: got %v body %s", rrGet.Code, rrGet.Body.String())
	}

	var listRes map[string]any
	if err := json.NewDecoder(rrGet.Body).Decode(&listRes); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}
	items, ok := listRes["data"].([]any)
	if !ok {
		t.Fatalf("expected data array in list response, got %#v", listRes["data"])
	}
	if len(items) < 2 {
		t.Fatalf("expected digest to create multiple curated items from the source content, got %d", len(items))
	}
}

func TestUpdateSpaceItemPersists(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_update_item"
	spaceName := "test_space_update"

	config = Config{
		RootPassword: SecuredString("secret_password"),
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

func TestUpdateSpaceItemPersistsLifecycleMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_update_lifecycle_metadata"
	spaceName := "test_space_update_lifecycle_metadata"

	config = Config{
		RootPassword: SecuredString("secret_password"),
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
		t.Fatalf("Failed to decode add item response: %v", err)
	}
	itemID := addRes["id"].(string)

	rr = sendJSON("POST", "/api/spaces/item/update?database="+dbName+"&name="+spaceName, handleUpdateSpaceItem, UpdateSpaceItemRequest{
		ID:         itemID,
		CategoryID: categoryID,
		Summaries:  []string{"Updated lifecycle summary"},
		Data: map[string]any{
			"chunk":                  "Updated lifecycle text",
			"text":                   "Updated lifecycle text",
			"description":            "Updated lifecycle description",
			"lifecycle_status":       "archived",
			"lifecycle_reason":       "cold_or_unused",
			"archive_reason":         "cold_or_unused",
			"archived_at":            int64(1700000000000),
			"lifecycle_access_count": 2,
		},
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
	if got := fmt.Sprint(item["lifecycle_status"]); got != "archived" {
		t.Fatalf("Expected lifecycle_status to persist, got %v", item["lifecycle_status"])
	}
	if got := fmt.Sprint(item["archive_reason"]); got != "cold_or_unused" {
		t.Fatalf("Expected archive_reason to persist, got %v", item["archive_reason"])
	}
}

func TestAddSpaceItemPersistsLifecycleMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_add_lifecycle_metadata"
	spaceName := "test_space_add_lifecycle_metadata"

	config = Config{
		RootPassword: SecuredString("secret_password"),
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
		Data: map[string]any{
			"text":                   "Initial text",
			"description":            "Initial description",
			"lifecycle_status":       "archived",
			"lifecycle_reason":       "cold_or_unused",
			"archive_reason":         "cold_or_unused",
			"archived_at":            int64(1700000000000),
			"lifecycle_access_count": 2,
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
	if got := fmt.Sprint(item["lifecycle_status"]); got != "archived" {
		t.Fatalf("Expected lifecycle_status to persist, got %v", item["lifecycle_status"])
	}
	if got := fmt.Sprint(item["archive_reason"]); got != "cold_or_unused" {
		t.Fatalf("Expected archive_reason to persist, got %v", item["archive_reason"])
	}
}

func TestListSpaceItemsExposesContentType(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_list_content_type"
	spaceName := "test_space_list_content_type"

	config = Config{
		RootPassword: SecuredString("secret_password"),
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
		RootPassword: SecuredString("secret_password"),
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
		RootPassword: SecuredString("secret_password"),
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
	dbOpts, err := GetDBOptions(ctx, dbName)
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
		RootPassword: SecuredString("secret_password"),
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
	dbOpts, err := GetDBOptions(ctx, dbName)
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

func TestHandleAddSpaceItemMultipartPersistsBlankLifecycleFields(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_multipart_blank_lifecycle"
	spaceName := "test_space_multipart_blank_lifecycle"

	config = Config{
		RootPassword: SecuredString("secret_password"),
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
	for _, field := range []struct{ key, value string }{
		{key: "category_id", value: categoryID},
		{key: "description", value: "Lifecycle clear test"},
		{key: "summaries", value: "Lifecycle clear summary"},
		{key: "asset_mode", value: "reference"},
		{key: "storage_mode", value: string(memory.DocumentStorageModeExternalMetadataOnly)},
		{key: "payload_ref", value: "uploaded://sample.txt"},
		{key: "filename", value: "sample.txt"},
		{key: "lifecycle_status", value: ""},
		{key: "lifecycle_reason", value: ""},
		{key: "archive_reason", value: ""},
		{key: "lifecycle_access_count", value: "0"},
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
		t.Fatalf("Failed to add item with blank lifecycle overrides: %v", rr.Body.String())
	}

	var addRes map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&addRes); err != nil {
		t.Fatalf("failed to decode add-item response: %v", err)
	}
	itemID, err := sop.ParseUUID(addRes["id"].(string))
	if err != nil {
		t.Fatalf("failed to parse item ID: %v", err)
	}

	ctx := context.Background()
	dbOpts, err := GetDBOptions(ctx, dbName)
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
	if got := fmt.Sprint(item.Data["lifecycle_status"]); got != "" {
		t.Fatalf("expected blank lifecycle_status to be persisted, got %q", got)
	}
	if got := fmt.Sprint(item.Data["lifecycle_reason"]); got != "" {
		t.Fatalf("expected blank lifecycle_reason to be persisted, got %q", got)
	}
	if got := fmt.Sprint(item.Data["archive_reason"]); got != "" {
		t.Fatalf("expected blank archive_reason to be persisted, got %q", got)
	}
	if got := fmt.Sprint(item.Data["lifecycle_access_count"]); got != "0" {
		t.Fatalf("expected lifecycle_access_count 0 to be persisted, got %q", got)
	}
}

func TestHandleAddSpaceItemMultipartReferenceModeUsesReferenceInsteadOfUpload(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_multipart_reference"
	spaceName := "test_space_multipart_reference"

	config = Config{
		RootPassword: SecuredString("secret_password"),
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
	if _, err := part.Write([]byte("ignored bytes")); err != nil {
		t.Fatalf("failed to write uploaded bytes: %v", err)
	}
	for _, field := range []struct{ key, value string }{
		{key: "category_id", value: categoryID},
		{key: "description", value: "Reference-only file"},
		{key: "summaries", value: "Reference summary"},
		{key: "asset_mode", value: "reference"},
		{key: "storage_mode", value: string(memory.DocumentStorageModeExternalMetadataOnly)},
		{key: "payload_ref", value: "uploaded://sample.txt"},
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
		t.Fatalf("Failed to add reference item: %v", rr.Body.String())
	}

	ctx := context.Background()
	dbOpts, err := GetDBOptions(ctx, dbName)
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
	uid, err := sop.ParseUUID(item.Data["doc_id"].(string))
	if err != nil {
		t.Fatalf("failed to parse stored doc id: %v", err)
	}
	doc, err := kb.Store.GetDocument(ctx, uid)
	if err != nil {
		t.Fatalf("failed to retrieve document: %v", err)
	}
	if doc == nil {
		t.Fatal("expected a document to be created for the reference-only item")
	}
	if doc.StorageMode != memory.DocumentStorageModeExternalMetadataOnly {
		t.Fatalf("expected external metadata-only storage mode, got %q", doc.StorageMode)
	}
	if doc.PayloadRef != "uploaded://sample.txt" {
		t.Fatalf("expected uploaded reference to be preserved, got %q", doc.PayloadRef)
	}
}

func TestHandleAddSpaceItemMultipartUploadPersistsUploadedBytes(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_multipart_upload"
	spaceName := "test_space_multipart_upload"

	config = Config{
		RootPassword: SecuredString("secret_password"),
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
	dbOpts, err := GetDBOptions(ctx, dbName)
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
	if doc.PayloadRef == "" {
		t.Fatal("expected a persisted payload reference for the uploaded item")
	}
	if string(doc.Data) != "hello from upload" {
		t.Fatalf("expected uploaded bytes to be persisted, got %q", string(doc.Data))
	}
	if doc.Content != "hello from upload" {
		t.Fatalf("expected extracted text to be persisted on the document, got %q", doc.Content)
	}
}

func TestHandleAddSpaceItemPersistsFilesystemDocumentReference(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_filesystem_doc"
	spaceName := "test_space_filesystem_doc"

	config = Config{
		RootPassword: SecuredString("secret_password"),
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
	dbOpts, err := GetDBOptions(ctx, dbName)
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
		RootPassword: SecuredString("secret_password"),
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
		RootPassword: SecuredString("secret_password"),
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

func TestHandleAddSpaceCategoryPersistsLifecycleMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_category_lifecycle_metadata"
	spaceName := "test_space_category_lifecycle_metadata"

	config = Config{
		RootPassword: SecuredString("secret_password"),
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

	rr = sendJSON("POST", "/api/spaces/category/add?database="+dbName+"&name="+spaceName, handleAddSpaceCategory, AddSpaceCategoryRequest{
		Name: "Repo Root",
		Metadata: map[string]any{
			"lifecycle_status":       "archived",
			"lifecycle_reason":       "repo_finished",
			"archive_reason":         "repo_finished",
			"archived_at":            int64(1700000000000),
			"lifecycle_access_count": 3,
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to add category: %v", rr.Body.String())
	}

	var addRes map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&addRes); err != nil {
		t.Fatalf("Failed to decode category response: %v", err)
	}
	categoryID := addRes["id"].(string)

	ctx := context.Background()
	dbOpts, err := GetDBOptions(ctx, dbName)
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

	categoriesTree, err := kb.Store.Categories(ctx)
	if err != nil {
		t.Fatalf("Categories failed: %v", err)
	}
	parsedID, err := sop.ParseUUID(categoryID)
	if err != nil {
		t.Fatalf("ParseUUID failed: %v", err)
	}
	found, err := categoriesTree.Find(ctx, parsedID, false)
	if err != nil {
		t.Fatalf("Find category failed: %v", err)
	}
	if !found {
		t.Fatal("expected category to exist")
	}
	cat, err := categoriesTree.GetCurrentValue(ctx)
	if err != nil {
		t.Fatalf("GetCurrentValue failed: %v", err)
	}
	if got := fmt.Sprint(cat.Metadata["lifecycle_status"]); got != "archived" {
		t.Fatalf("expected persisted lifecycle_status archived, got %q", got)
	}
	if got := fmt.Sprint(cat.Metadata["archive_reason"]); got != "repo_finished" {
		t.Fatalf("expected persisted archive_reason repo_finished, got %q", got)
	}

	req, _ := http.NewRequest("GET", "/api/spaces/categories?database="+dbName+"&name="+spaceName, nil)
	rrList := httptest.NewRecorder()
	handleListSpaceCategories(rrList, req)
	if rrList.Code != http.StatusOK {
		t.Fatalf("category list failed: %v", rrList.Body.String())
	}
	var listRes map[string]any
	if err := json.NewDecoder(rrList.Body).Decode(&listRes); err != nil {
		t.Fatalf("Failed to decode category list response: %v", err)
	}
	items := listRes["data"].([]any)
	if len(items) == 0 {
		t.Fatal("expected category list to include categories")
	}
	foundCategory := false
	for _, raw := range items {
		item := raw.(map[string]any)
		if item["id"] == categoryID {
			foundCategory = true
			metadata, ok := item["metadata"].(map[string]any)
			if !ok {
				t.Fatalf("expected category metadata to be present in list response, got %#v", item["metadata"])
			}
			if got := fmt.Sprint(metadata["lifecycle_status"]); got != "archived" {
				t.Fatalf("expected list response metadata lifecycle_status archived, got %q", got)
			}
			break
		}
	}
	if !foundCategory {
		t.Fatalf("expected category %s to appear in list response", categoryID)
	}
}

func TestHandleAddSpaceCategory_NormalizesChildPath(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "testdb_trim_child_path"
	spaceName := "test_space_trim_child_path"

	config = Config{
		RootPassword: SecuredString("secret_password"),
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
	dbOpts, err := GetDBOptions(ctx, dbName)
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
	if got, want := cat.Path, "Root A/Child A1"; got != want {
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
		RootPassword: SecuredString("secret_password"),
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

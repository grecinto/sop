package sopbackend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	log "log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/memory"
)

// validSpaceNamePattern defines allowed characters: letters, numbers, underscore, hyphen
var validSpaceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// normalizeSpaceName converts spaces to underscores and lowercases the name for backend storage
// Example: "Core Playbook" -> "core_playbook"
func normalizeSpaceName(name string) string {
	name = strings.TrimSpace(name)
	// Convert spaces to underscores
	name = strings.ReplaceAll(name, " ", "_")
	// Convert to lowercase for consistency
	name = strings.ToLower(name)
	return name
}

// validateSpaceName checks if a Space name is valid after normalization
func validateSpaceName(name string) error {
	if name == "" {
		return fmt.Errorf("space name cannot be empty")
	}
	if !validSpaceNamePattern.MatchString(name) {
		return fmt.Errorf("space name can only contain letters, numbers, underscores, and hyphens")
	}
	return nil
}

func autoVectorizeBuiltinSpace(ctx context.Context, db *database.Database, spaceName, preloadPath string, emb ai.Embeddings, llm ai.Generator, onProgress func(progress int, total int, msg string)) error {
	if !shouldForceLocalBuiltinEmbedder(spaceName, preloadPath) || emb == nil {
		return nil
	}

	if onProgress != nil {
		onProgress(95, 100, "Calculating Embeddings (Auto Vectorize)...")
	}

	if err := db.Vectorize(ctx, spaceName, llm, emb, 100); err != nil {
		return fmt.Errorf("vectorization failed: %w", err)
	}
	return nil
}

type kbConfigSyncer interface {
	GetConfig(context.Context) (*memory.KnowledgeBaseConfig, error)
	SetConfig(context.Context, *memory.KnowledgeBaseConfig) error
}

func syncKnowledgeBaseEmbedderConfig(ctx context.Context, kb kbConfigSyncer, emb ai.Embeddings) error {
	if kb == nil || emb == nil {
		return nil
	}

	cfg, err := kb.GetConfig(ctx)
	if err != nil || cfg == nil {
		return nil
	}

	needsUpdate := false
	if cfg.EmbedderDimension != emb.Dim() {
		cfg.EmbedderDimension = emb.Dim()
		needsUpdate = true
	}
	if cfg.Embedder != emb.Name() {
		cfg.Embedder = emb.Name()
		needsUpdate = true
	}
	if !needsUpdate {
		return nil
	}

	return kb.SetConfig(ctx, cfg)
}

func runIngestImportSpace(ctx context.Context, request IngestSpaceRequest, emb ai.Embeddings, llm ai.Generator, onProgress func(progress int, total int, msg string), beforeCommit func()) error {
	if onProgress != nil {
		onProgress(10, 100, "Initializing database connection...")
	}

	opts, err := GetDBOptions(ctx, request.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to get DB config: %w", err)
	}

	db := database.NewDatabase(opts)
	trans, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer trans.Rollback(ctx)

	kb, err := db.OpenKnowledgeBase(ctx, request.SpaceName, trans, llm, emb, false)
	if err != nil {
		return fmt.Errorf("failed to open KnowledgeBase '%s': %w", request.SpaceName, err)
	}

	reader, closer, err := ingestImportReader(ctx, request)
	if err != nil {
		return err
	}
	if closer != nil {
		defer closer.Close()
	}

	if reader != nil {
		if onProgress != nil {
			onProgress(30, 100, "Reading Space data stream...")
		}

		enrichCb := func(it *memory.ExportItem[map[string]any]) {
			var textStr, descStr string
			if txt, ok := it.Data["text"].(string); ok {
				textStr = txt
			} else if txt, ok := it.Data["title"].(string); ok {
				textStr = txt
			}

			if desc, ok := it.Data["description"].(string); ok {
				descStr = desc
			} else if cnt, ok := it.Data["content"].(string); ok {
				descStr = cnt
			}

			it.Summaries = extractSummaries(SpaceIngestChunk{
				Summaries:   it.Summaries,
				Text:        textStr,
				Description: descStr,
				Data:        it.Data,
			})
		}

		if err := kb.ImportJSON(ctx, reader, "expert", enrichCb); err != nil {
			return fmt.Errorf("failed to ImportJSON: %w", err)
		}
	}

	if err := syncKnowledgeBaseEmbedderConfig(ctx, kb, emb); err != nil {
		return fmt.Errorf("failed to update KnowledgeBase config: %w", err)
	}

	if onProgress != nil {
		onProgress(90, 100, "Committing changes...")
	}
	if beforeCommit != nil {
		beforeCommit()
	}
	if err := trans.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit ingest import: %w", err)
	}

	if err := autoVectorizeBuiltinSpace(ctx, db, request.SpaceName, request.PreloadFilePath, emb, llm, onProgress); err != nil {
		return fmt.Errorf("import successful, but %w", err)
	}

	return nil
}

func runIngestSpace(ctx context.Context, request IngestSpaceRequest, emb ai.Embeddings, llm ai.Generator, onProgress func(progress int, total int, msg string), beforeCommit func()) error {
	if onProgress != nil {
		onProgress(10, 100, "Initializing database connection...")
	}

	opts, err := GetDBOptions(ctx, request.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to get DB config: %w", err)
	}

	db := database.NewDatabase(opts)
	trans, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer trans.Rollback(ctx)

	kb, err := db.OpenKnowledgeBase(ctx, request.SpaceName, trans, llm, emb, false)
	if err != nil {
		return fmt.Errorf("failed to open KnowledgeBase '%s': %w", request.SpaceName, err)
	}

	if request.Attributes != nil {
		if err := kb.SetConfig(ctx, request.Attributes); err != nil {
			return fmt.Errorf("failed to insert Space attributes: %w", err)
		}
	}

	reader, closer, err := ingestImportReader(ctx, request)
	if err != nil {
		return err
	}
	if closer != nil {
		defer closer.Close()
	}

	if reader != nil {
		if onProgress != nil {
			onProgress(30, 100, "Reading Space data stream...")
		}

		decoder := json.NewDecoder(reader)
		var batch []memory.Thought[map[string]any]
		const batchSize = 500
		totalProcessed := 0

		currentKBConfig, cfgErr := kb.GetConfig(ctx)
		if cfgErr != nil {
			return fmt.Errorf("failed to read KnowledgeBase config: %w", cfgErr)
		}

		documentMode := false
		if currentKBConfig != nil {
			documentMode = currentKBConfig.DocumentMode
		}

		ingestBatch := func(final bool) error {
			if len(batch) == 0 {
				return nil
			}

			progressMsg := fmt.Sprintf("Embedding and ingesting batch (processed %d)...", totalProcessed)
			progressTotal := totalProcessed + 100
			if final {
				progressMsg = fmt.Sprintf("Embedding and ingesting final batch (processed %d)...", totalProcessed)
				progressTotal = totalProcessed
			}
			if onProgress != nil {
				onProgress(50, progressTotal, progressMsg)
			}

			if err := kb.IngestThoughts(ctx, batch, "expert"); err != nil {
				return fmt.Errorf("failed to ingest Space batch: %w", err)
			}
			batch = batch[:0]
			return nil
		}

		t, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("failed to read Space data stream: %w", err)
		}

		delim, ok := t.(json.Delim)
		if !ok {
			return fmt.Errorf("failed to parse Space data stream: expected JSON object or array")
		}

		if delim == '{' {
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return fmt.Errorf("failed to read Space data stream: %w", err)
				}
				keyStr, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("failed to parse Space data stream: expected object key")
				}

				switch keyStr {
				case "config":
					var cfg memory.KnowledgeBaseConfig
					if err := decoder.Decode(&cfg); err != nil {
						return fmt.Errorf("failed to decode Space config: %w", err)
					}
					if err := kb.SetConfig(ctx, &cfg); err != nil {
						return fmt.Errorf("failed to set Space config: %w", err)
					}
					documentMode = cfg.DocumentMode
				case "items":
					t, err := decoder.Token()
					if err != nil {
						return fmt.Errorf("failed to read Space items: %w", err)
					}
					if itemsDelim, ok := t.(json.Delim); !ok || itemsDelim != '[' {
						return fmt.Errorf("failed to parse Space items: expected array")
					}

					for decoder.More() {
						var chunk SpaceIngestChunk
						if err := decoder.Decode(&chunk); err != nil {
							return fmt.Errorf("failed to decode Space item: %w", err)
						}

						cid := chunk.ID
						if cid == "" {
							cid = fmt.Sprintf("custom_%d", totalProcessed)
						}

						batch = append(batch, memory.Thought[map[string]any]{
							DocID:     chunk.DocID,
							Summaries: extractSummaries(chunk), Vectors: chunk.Vectors, CategoryPath: chunk.Category, Data: buildChunkData(cid, chunk, documentMode),
						})
						totalProcessed++

						if len(batch) >= batchSize {
							if err := ingestBatch(false); err != nil {
								return err
							}
						}
					}
				default:
					var skip any
					if err := decoder.Decode(&skip); err != nil {
						return fmt.Errorf("failed to skip Space property %q: %w", keyStr, err)
					}
				}
			}
		} else if delim == '[' {
			for decoder.More() {
				var chunk SpaceIngestChunk
				if err := decoder.Decode(&chunk); err != nil {
					return fmt.Errorf("failed to decode Space item: %w", err)
				}

				cid := chunk.ID
				if cid == "" {
					cid = fmt.Sprintf("custom_%d", totalProcessed)
				}

				batch = append(batch, memory.Thought[map[string]any]{
					DocID:     chunk.DocID,
					Summaries: extractSummaries(chunk), Vectors: chunk.Vectors, CategoryPath: chunk.Category, Data: buildChunkData(cid, chunk, documentMode),
				})
				totalProcessed++

				if len(batch) >= batchSize {
					if err := ingestBatch(false); err != nil {
						return err
					}
				}
			}
		} else {
			return fmt.Errorf("failed to parse Space data stream: expected JSON object or array")
		}

		if err := ingestBatch(true); err != nil {
			return err
		}

		if err := syncKnowledgeBaseEmbedderConfig(ctx, kb, emb); err != nil {
			return fmt.Errorf("failed to update KnowledgeBase config: %w", err)
		}

	}

	if onProgress != nil {
		onProgress(90, 100, "Committing changes...")
	}
	if beforeCommit != nil {
		beforeCommit()
	}
	if err := trans.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit vector store initialization: %w", err)
	}

	if err := autoVectorizeBuiltinSpace(ctx, db, request.SpaceName, request.PreloadFilePath, emb, llm, onProgress); err != nil {
		return err
	}

	return nil
}

func shouldUseImportPathForPreload(templateID, preloadPath string) bool {
	trimmed := strings.TrimSpace(templateID)
	return strings.EqualFold(trimmed, ai.DefaultKBName) || strings.EqualFold(trimmed, "Core Playbook")
}

func builtinPreloadCandidates(templateID string) []string {
	normalized := strings.ToLower(strings.TrimSpace(templateID))
	normalized = strings.ReplaceAll(normalized, "_", " ")
	normalized = strings.ReplaceAll(normalized, "-", " ")
	normalized = strings.Join(strings.Fields(normalized), " ")

	var candidates []string
	if strings.Contains(normalized, "medical") {
		candidates = append(candidates,
			"medical.json",
			"ai/medical.json",
			"../medical.json",
			"../../medical.json",
			"lib/sop/medical.json",
			"../lib/sop/medical.json",
			"../../lib/sop/medical.json",
		)
	}
	if strings.Contains(normalized, "core playbook") {
		candidates = append(candidates,
			"sop_base_knowledge.json",
			"ai/sop_base_knowledge.json",
			"../ai/sop_base_knowledge.json",
			"../../ai/sop_base_knowledge.json",
			"lib/sop/ai/sop_base_knowledge.json",
			"../lib/sop/ai/sop_base_knowledge.json",
			"../../lib/sop/ai/sop_base_knowledge.json",
		)
	}
	return candidates
}

func resolvePreloadFilePath(templateID string, searchRoots []string) string {
	trimmed := strings.TrimSpace(templateID)
	if trimmed == "" {
		return ""
	}

	candidates := builtinPreloadCandidates(trimmed)
	for _, root := range searchRoots {
		if root == "" {
			continue
		}

		current := filepath.Clean(root)
		for {
			for _, candidate := range candidates {
				candidatePath := filepath.Clean(filepath.Join(current, candidate))
				if _, err := os.Stat(candidatePath); err == nil {
					if absPath, err := filepath.Abs(candidatePath); err == nil {
						return absPath
					}
					return candidatePath
				}
			}

			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}

	return ""
}

func buildImportSpaceRequest(ctx context.Context, filePath, databaseName, spaceName string) (*http.Request, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open preload file: %w", err)
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("database", databaseName); err != nil {
		return nil, fmt.Errorf("failed to write database field: %w", err)
	}
	if err := writer.WriteField("name", spaceName); err != nil {
		return nil, fmt.Errorf("failed to write name field: %w", err)
	}

	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("failed to copy preload file contents: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/spaces/import", &body)
	if err != nil {
		return nil, fmt.Errorf("failed to create import request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req, nil
}

func ingestImportReader(ctx context.Context, request IngestSpaceRequest) (io.Reader, io.Closer, error) {
	if request.PreloadFilePath != "" {
		f, err := os.Open(request.PreloadFilePath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open preload file: %w", err)
		}
		return f, f, nil
	}
	if request.URL != "" {
		reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodGet, request.URL, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create import request: %w", err)
		}
		resp, err := http.DefaultClient.Do(reqHTTP)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch import URL: %w", err)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return nil, nil, fmt.Errorf("failed to fetch import URL: %s", resp.Status)
		}
		return resp.Body, resp.Body, nil
	}
	if len(request.CustomData) > 0 {
		return bytes.NewReader(request.CustomData), nil, nil
	}
	return nil, nil, nil
}

func PreloadSpaceFromTemplate(ctx context.Context, templateID, databaseName string, r *http.Request) error {
	if strings.TrimSpace(templateID) == "" || strings.TrimSpace(databaseName) == "" {
		return fmt.Errorf("template_id and database_name are required")
	}

	if config.DisableSOPKBPreload && (strings.EqualFold(templateID, ai.DefaultKBName) || strings.EqualFold(templateID, "Core Playbook")) {
		return fmt.Errorf("SOP KB preload is disabled by configuration")
	}

	var actualPath string
	searchRoots := []string{}
	if cwd, err := os.Getwd(); err == nil {
		searchRoots = append(searchRoots, cwd)
	}
	if exePath, err := os.Executable(); err == nil {
		searchRoots = append(searchRoots, filepath.Dir(exePath))
	}
	if configPath := strings.TrimSpace(os.Getenv("SOP_CONFIG_FILE")); configPath != "" {
		searchRoots = append(searchRoots, filepath.Dir(configPath))
	}
	if repoRoot, err := filepath.Abs(filepath.Join(".", "..", "..")); err == nil {
		searchRoots = append(searchRoots, repoRoot)
	}
	searchRoots = append(searchRoots, ".", "..", "../..", "ai")

	pathsToTry := []string{
		templateID + ".json",
		"../" + templateID + ".json",
		"../../" + templateID + ".json",
		"ai/" + templateID + ".json",
	}
	pathsToTry = append(pathsToTry, builtinPreloadCandidates(templateID)...)

	for _, p := range pathsToTry {
		if _, err := os.Stat(p); err == nil {
			actualPath = p
			log.Debug("SOP preload candidate found directly", "template_id", templateID, "path", p)
			break
		}
		log.Debug("SOP preload candidate missing", "template_id", templateID, "path", p)
	}
	if actualPath == "" {
		if resolved := resolvePreloadFilePath(templateID, searchRoots); resolved != "" {
			actualPath = resolved
			log.Debug("SOP preload candidate found via resolver", "template_id", templateID, "resolved_path", resolved)
		} else {
			log.Warn("SOP preload asset not found", "template_id", templateID, "search_roots", searchRoots, "paths_tried", pathsToTry)
		}
	}

	if actualPath == "" && (strings.EqualFold(templateID, ai.DefaultKBName) || strings.EqualFold(templateID, "Core Playbook")) {
		isGoRun := strings.Contains(os.Args[0], "go-build") || strings.HasPrefix(os.Args[0], os.TempDir())
		if isGoRun {
			fmt.Printf("SOP Knowledge Base JSON not found. Auto-compiling since running in dev mode...\n")
			compilerDir := "./ai/cmd/knowledge_compiler"
			if _, err := os.Stat("../../ai/cmd/knowledge_compiler"); err == nil {
				compilerDir = "../../ai/cmd/knowledge_compiler"
			} else if _, err := os.Stat("../ai/cmd/knowledge_compiler"); err == nil {
				compilerDir = "../ai/cmd/knowledge_compiler"
			}
			cmd := exec.Command("bash", "run.sh")
			cmd.Dir = compilerDir
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err == nil {
				for _, p := range pathsToTry {
					if _, err := os.Stat(p); err == nil {
						actualPath = p
						break
					}
				}
			} else {
				fmt.Printf("Warning: Failed to auto-compile SOP KB: %v\n", err)
			}
		}
	}

	if actualPath == "" {
		return fmt.Errorf("failed to find Space file for template %q", templateID)
	}

	log.Debug("SOP preload asset resolved", "template_id", templateID, "asset_path", actualPath)
	dbEmbedder := GetConfiguredEmbedderForSpace(r, templateID, actualPath)
	dbLLM := GetConfiguredLLM(r)
	if shouldUseImportPathForPreload(templateID, actualPath) {
		return runIngestImportSpace(ctx, IngestSpaceRequest{
			DatabaseName:    databaseName,
			SpaceName:       templateID,
			PreloadFilePath: actualPath,
		}, dbEmbedder, dbLLM, nil, nil)
	}

	return runIngestSpace(ctx, IngestSpaceRequest{
		DatabaseName:    databaseName,
		SpaceName:       templateID,
		PreloadFilePath: actualPath,
	}, dbEmbedder, dbLLM, nil, nil)
}

func handlePreloadSpace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PreloadSpaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("SOP preload request decode failed", "error", err)
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.TemplateID == "" || req.DatabaseName == "" {
		log.Error("SOP preload request missing required fields", "template_id", req.TemplateID, "database_name", req.DatabaseName)
		http.Error(w, "template_id and database_name are required", http.StatusBadRequest)
		return
	}

	if config.DisableSOPKBPreload && (strings.EqualFold(req.TemplateID, ai.DefaultKBName) || strings.EqualFold(req.TemplateID, "SOP") || strings.EqualFold(req.TemplateID, "Core Playbook")) {
		log.Info("SOP preload disabled by configuration", "template_id", req.TemplateID, "database_name", req.DatabaseName)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success":  true,
			"disabled": true,
			"message":  "SOP KB preload is disabled by configuration",
		})
		return
	}

	var actualPath string
	searchRoots := []string{}
	if cwd, err := os.Getwd(); err == nil {
		searchRoots = append(searchRoots, cwd)
	}
	if exePath, err := os.Executable(); err == nil {
		searchRoots = append(searchRoots, filepath.Dir(exePath))
	}
	if configPath := strings.TrimSpace(os.Getenv("SOP_CONFIG_FILE")); configPath != "" {
		searchRoots = append(searchRoots, filepath.Dir(configPath))
	}
	if repoRoot, err := filepath.Abs(filepath.Join(".", "..", "..")); err == nil {
		searchRoots = append(searchRoots, repoRoot)
	}
	searchRoots = append(searchRoots, ".", "..", "../..", "ai")

	pathsToTry := []string{
		req.TemplateID + ".json",
		"../" + req.TemplateID + ".json",
		"../../" + req.TemplateID + ".json",
		"ai/" + req.TemplateID + ".json",
	}
	pathsToTry = append(pathsToTry, builtinPreloadCandidates(req.TemplateID)...)

	for _, p := range pathsToTry {
		if _, err := os.Stat(p); err == nil {
			actualPath = p
			log.Debug("SOP preload candidate found directly", "template_id", req.TemplateID, "path", p)
			break
		}
		log.Debug("SOP preload candidate missing", "template_id", req.TemplateID, "path", p)
	}
	if actualPath == "" {
		if resolved := resolvePreloadFilePath(req.TemplateID, searchRoots); resolved != "" {
			actualPath = resolved
			log.Debug("SOP preload candidate found via resolver", "template_id", req.TemplateID, "resolved_path", resolved)
		} else {
			log.Warn("SOP preload asset not found", "template_id", req.TemplateID, "search_roots", searchRoots, "paths_tried", pathsToTry)
		}
	}

	if actualPath == "" && (strings.EqualFold(req.TemplateID, ai.DefaultKBName) || strings.EqualFold(req.TemplateID, "SOP") || strings.EqualFold(req.TemplateID, "Core Playbook")) {
		// Auto-generate if running in source development (go run)
		isGoRun := strings.Contains(os.Args[0], "go-build") || strings.HasPrefix(os.Args[0], os.TempDir())
		if isGoRun {
			fmt.Printf("SOP Knowledge Base JSON not found. Auto-compiling since running in dev mode...\n")
			compilerDir := "./ai/cmd/knowledge_compiler"
			if _, err := os.Stat("../../ai/cmd/knowledge_compiler"); err == nil {
				compilerDir = "../../ai/cmd/knowledge_compiler"
			} else if _, err := os.Stat("../ai/cmd/knowledge_compiler"); err == nil {
				compilerDir = "../ai/cmd/knowledge_compiler"
			}
			cmd := exec.Command("bash", "run.sh")
			cmd.Dir = compilerDir
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err == nil {
				// Search again
				for _, p := range pathsToTry {
					if _, err := os.Stat(p); err == nil {
						actualPath = p
						break
					}
				}
			} else {
				fmt.Printf("Warning: Failed to auto-compile SOP KB: %v\n", err)
			}
		}
	}

	if actualPath == "" {
		log.Error("SOP preload failed because asset path was empty", "template_id", req.TemplateID)
		http.Error(w, fmt.Sprintf("Failed to find Space file for template '%s'", req.TemplateID), http.StatusBadRequest)
		return
	}

	log.Debug("SOP preload asset resolved", "template_id", req.TemplateID, "asset_path", actualPath)
	dbEmbedder := GetConfiguredEmbedderForSpace(r, req.TemplateID, actualPath)
	dbLLM := GetConfiguredLLM(r)
	task := RegisterTask("SpacePreload", 100)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"task_id": task.TaskID,
		"message": fmt.Sprintf("Preloading %s started in background", req.TemplateID),
	})

	log.Debug("SOP preload task registered", "task_id", task.TaskID, "template_id", req.TemplateID, "database_name", req.DatabaseName, "asset_path", actualPath)

	go func(taskID string, request PreloadSpaceRequest, preloadPath string, emb ai.Embeddings, llm ai.Generator) {
		log.Debug("SOP preload background worker started", "task_id", taskID, "template_id", request.TemplateID, "database_name", request.DatabaseName, "asset_path", preloadPath)
		defer func() {
			if rec := recover(); rec != nil {
				log.Error("Panic during preload", "task_id", taskID, "template", request.TemplateID, "database", request.DatabaseName, "error", rec)
				UpdateTask(taskID, "error", 0, 0, "", fmt.Sprintf("Panic during preload: %v", rec))
			}
		}()

		ctx := context.Background()
		var err error
		log.Debug("SOP preload worker branch selected", "task_id", taskID, "template_id", request.TemplateID, "asset_path", preloadPath, "use_import", shouldUseImportPathForPreload(request.TemplateID, preloadPath))
		if shouldUseImportPathForPreload(request.TemplateID, preloadPath) {
			err = runIngestImportSpace(ctx, IngestSpaceRequest{
				DatabaseName:    request.DatabaseName,
				SpaceName:       request.TemplateID,
				PreloadFilePath: preloadPath,
			}, emb, llm, func(progress int, total int, msg string) {
				UpdateTask(taskID, "in_progress", progress, total, msg, "")
			}, nil)
		} else {
			err = runIngestSpace(ctx, IngestSpaceRequest{
				DatabaseName:    request.DatabaseName,
				SpaceName:       request.TemplateID,
				PreloadFilePath: preloadPath,
			}, emb, llm, func(progress int, total int, msg string) {
				UpdateTask(taskID, "in_progress", progress, total, msg, "")
			}, nil)
		}
		if err != nil {
			log.Error("Space preload failed", "task_id", taskID, "template", request.TemplateID, "database", request.DatabaseName, "error", err)
			UpdateTask(taskID, "error", 0, 0, "", err.Error())
			return
		}

		UpdateTask(taskID, "completed", 100, 100, fmt.Sprintf("Successfully pre-loaded %s", request.TemplateID), "")
	}(task.TaskID, req, actualPath, dbEmbedder, dbLLM)
}

func extractSummaries(chunk SpaceIngestChunk) []string {
	summaries := chunk.Summaries
	var finalParagraphs []string
	seen := make(map[string]bool)

	addParagraph := func(toAdd []string) {
		for _, s := range toAdd {
			s = strings.TrimSpace(s)
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			if len(finalParagraphs) < MAX_ITEM_SUMMARIES {
				finalParagraphs = append(finalParagraphs, s)
			}
		}
	}

	if sArr, ok := summaries.([]interface{}); ok && len(sArr) > 0 {
		var res []string
		for _, s := range sArr {
			if str, ok := s.(string); ok && str != "" {
				res = append(res, str)
			}
		}
		addParagraph(res)
	} else if sArr2, ok := summaries.([]string); ok && len(sArr2) > 0 {
		addParagraph(sArr2)
	} else if sStr, ok := summaries.(string); ok && sStr != "" {
		sStr = strings.ReplaceAll(sStr, "\r\n", "\n")
		parts := strings.Split(sStr, "\n\n")
		var res []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				if len(p) > MAX_PARAGRAPH_LENGTH {
					p = p[:MAX_PARAGRAPH_LENGTH]
				}
				res = append(res, p)
			}
		}
		addParagraph(res)
	}

	if len(finalParagraphs) >= MAX_ITEM_SUMMARIES {
		return finalParagraphs
	}

	// Check if we can apply some heuristics and come up with decent Summaries.
	s := determineSummaries(chunk.Text, chunk.Description, MAX_ITEM_SUMMARIES)
	addParagraph(s)

	if len(finalParagraphs) >= MAX_ITEM_SUMMARIES {
		return finalParagraphs
	}

	// Check if there is Text, Description in Data.
	if chunk.Data != nil {
		var dataText, dataDesc string
		if t, ok := chunk.Data["text"].(string); ok {
			dataText = t
		}
		if d, ok := chunk.Data["description"].(string); ok {
			dataDesc = d
		}
		if dataText != "" || dataDesc != "" {
			s = determineSummaries(dataText, dataDesc, MAX_ITEM_SUMMARIES)
			addParagraph(s)
		}
	}

	return finalParagraphs
}

func handleCreateSpace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CreateSpaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.SpaceName == "" || req.DatabaseName == "" {
		http.Error(w, "space_name and database_name are required", http.StatusBadRequest)
		return
	}

	// Normalize Space name (convert spaces to underscores, lowercase)
	normalizedName := normalizeSpaceName(req.SpaceName)

	// Validate normalized Space name
	if err := validateSpaceName(normalizedName); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Use normalized name for all operations
	req.SpaceName = normalizedName

	ctx := context.Background()
	opts, err := GetDBOptions(ctx, req.DatabaseName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get DB config: %v", err), http.StatusInternalServerError)
		return
	}

	db := database.NewDatabase(opts)
	trans, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to begin transaction: %v", err), http.StatusInternalServerError)
		return
	}
	defer trans.Rollback(ctx)

	dbEmbedder := GetConfiguredEmbedder(r)
	dbLLM := GetConfiguredLLM(r)

	kb, err := db.OpenKnowledgeBase(ctx, req.SpaceName, trans, dbLLM, dbEmbedder, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to open Space '%s': %v", req.SpaceName, err), http.StatusInternalServerError)
		return
	}

	if req.Attributes != nil {
		if err := kb.SetConfig(ctx, req.Attributes); err != nil {
			http.Error(w, fmt.Sprintf("Failed to set Space config: %v", err), http.StatusInternalServerError)
			return
		}
	}

	if err := trans.Commit(ctx); err != nil {
		http.Error(w, fmt.Sprintf("Failed to commit Space creation: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Space '%s' created successfully", req.SpaceName),
	})
}

func handleIngestSpace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req IngestSpaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.DatabaseName == "" {
		http.Error(w, "database_name is required", http.StatusBadRequest)
		return
	}

	if req.SpaceName == "" {
		http.Error(w, "space_name is required", http.StatusBadRequest)
		return
	}

	// Validate Space name - reject spaces and invalid characters
	if err := validateSpaceName(req.SpaceName); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if len(req.CustomData) == 0 && req.URL == "" && req.PreloadFilePath == "" {
		http.Error(w, "Must provide custom_data, url, or preload filepath to ingest", http.StatusBadRequest)
		return
	}

	dbEmbedder := GetConfiguredEmbedderForSpace(r, req.SpaceName, req.PreloadFilePath)
	dbLLM := GetConfiguredLLM(r)

	task := RegisterTask("SpaceIngest", 100)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"task_id": task.TaskID,
		"message": fmt.Sprintf("Preloading and Ingesting %s started in background", req.SpaceName),
	})

	go func(taskId string, request IngestSpaceRequest, emb ai.Embeddings, llm ai.Generator) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error("Panic during preload", "task_id", taskId, "space", request.SpaceName, "database", request.DatabaseName, "error", rec)
				UpdateTask(taskId, "error", 0, 0, "", fmt.Sprintf("Panic during preload: %v", rec))
			}
		}()

		ctx := context.Background()
		err := runIngestSpace(ctx, request, emb, llm, func(progress int, total int, msg string) {
			UpdateTask(taskId, "in_progress", progress, total, msg, "")
		}, nil)
		if err != nil {
			log.Error("Space preload failed", "task_id", taskId, "space", request.SpaceName, "database", request.DatabaseName, "error", err)
			UpdateTask(taskId, "error", 0, 0, "", err.Error())
			return
		}

		UpdateTask(taskId, "completed", 100, 100, fmt.Sprintf("Successfully pre-loaded %s", request.SpaceName), "")
	}(task.TaskID, req, dbEmbedder, dbLLM)
}

func handleIngestImportSpace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req IngestSpaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.DatabaseName == "" || req.SpaceName == "" {
		http.Error(w, "database_name and space_name are required", http.StatusBadRequest)
		return
	}

	if len(req.CustomData) == 0 && req.URL == "" && req.PreloadFilePath == "" {
		http.Error(w, "Must provide custom_data, url, or preload filepath to ingest/import", http.StatusBadRequest)
		return
	}

	dbEmbedder := GetConfiguredEmbedderForSpace(r, req.SpaceName, req.PreloadFilePath)
	dbLLM := GetConfiguredLLM(r)
	embedderName := ""
	if dbEmbedder != nil {
		embedderName = dbEmbedder.Name()
	}
	log.Debug("ingest import embedder selection", "space", req.SpaceName, "preload_path", req.PreloadFilePath, "production_mode", config.ProductionMode, "embedder_name", embedderName, "embedder_nil", dbEmbedder == nil, "llm_nil", dbLLM == nil)

	task := RegisterTask("SpaceIngestImport", 100)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"task_id": task.TaskID,
		"message": fmt.Sprintf("Ingest/Importing %s started in background", req.SpaceName),
	})

	go func(taskId string, request IngestSpaceRequest, emb ai.Embeddings, llm ai.Generator) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error("Panic during ingest import", "task_id", taskId, "space", request.SpaceName, "database", request.DatabaseName, "error", rec)
				UpdateTask(taskId, "error", 0, 0, "", fmt.Sprintf("Panic during ingest import: %v", rec))
			}
		}()

		ctx := context.Background()
		err := runIngestImportSpace(ctx, request, emb, llm, func(progress int, total int, msg string) {
			UpdateTask(taskId, "in_progress", progress, total, msg, "")
		}, nil)
		if err != nil {
			log.Error("Space ingest import failed", "task_id", taskId, "space", request.SpaceName, "database", request.DatabaseName, "error", err)
			UpdateTask(taskId, "error", 0, 0, "", err.Error())
			return
		}

		UpdateTask(taskId, "completed", 100, 100, fmt.Sprintf("Successfully imported and ingested %s", request.SpaceName), "")
	}(task.TaskID, req, dbEmbedder, dbLLM)
}

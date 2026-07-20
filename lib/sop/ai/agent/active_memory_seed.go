package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"time"

	log "log/slog"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai/memory"
)

const semanticBaseKnowledgeBatchSize = 50

type KnowledgeChunk struct {
	Category string `json:"category"`
	Title    string `json:"title"`
	Content  string `json:"content"`
}

func (s *Service) SeedSemanticBaseKnowledge(jsonFilePath string) {
	if s.domain == nil || !s.EnableShortTermMemory {
		log.Warn("SeedSemanticBaseKnowledge aborted: Domain not initialized or Active Memory disabled")
		return
	}

	go func() {
		embedCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		log.Info("SeedSemanticBaseKnowledge: Starting async RAG documentation ingestion", "file", jsonFilePath)

		data, err := os.ReadFile(jsonFilePath)
		if err != nil {
			log.Warn("SeedSemanticBaseKnowledge failed to read file", "error", err)
			return
		}

		var chunks []KnowledgeChunk
		if err := json.Unmarshal(data, &chunks); err != nil {
			log.Warn("SeedSemanticBaseKnowledge failed to unmarshal json", "error", err)
			return
		}

		txtx, err := s.domain.BeginTransaction(embedCtx, sop.ForReading)
		if err != nil {
			log.Warn("SeedSemanticBaseKnowledge: Failed to start transaction", "error", err)
			return
		}

		memAny, err := s.domain.Memory(embedCtx, txtx)
		if err != nil {
			txtx.Rollback(embedCtx)
			log.Warn("SeedSemanticBaseKnowledge: Failed to get MemoryBase", "error", err)
			return
		}

		_, ok := memAny.(*memory.KnowledgeBase[map[string]any])
		if !ok {
			txtx.Rollback(embedCtx)
			log.Warn("SeedSemanticBaseKnowledge: Invalid MemoryBase interface")
			return
		}
		txtx.Rollback(embedCtx)

		log.Info("SeedSemanticBaseKnowledge: Ingesting chunks...", "count", len(chunks))

		thoughts := buildSemanticBaseKnowledgeThoughts(chunks)
		for _, batch := range batchSemanticBaseKnowledgeThoughts(thoughts, semanticBaseKnowledgeBatchSize) {
			tx, err := s.domain.BeginTransaction(embedCtx, sop.ForWriting)
			if err != nil {
				log.Warn("SeedSemanticBaseKnowledge: Failed to start batch transaction", "error", err)
				continue
			}
			kbTx, _ := s.domain.Memory(embedCtx, tx)
			kbW := kbTx.(*memory.KnowledgeBase[map[string]any])

			if err := kbW.IngestThoughts(embedCtx, batch, "System"); err != nil {
				tx.Rollback(embedCtx)
				log.Warn("SeedSemanticBaseKnowledge: Failed to ingest thought batch", "error", err)
				continue
			}
			if err := tx.Commit(embedCtx); err != nil {
				log.Warn("SeedSemanticBaseKnowledge: Failed to commit thought batch", "error", err)
			}
		}

		log.Info("SeedSemanticBaseKnowledge: Documentation RAG ingestion complete!", "chunks", len(chunks))
	}()
}

func buildSemanticBaseKnowledgeThoughts(chunks []KnowledgeChunk) []memory.Thought[map[string]any] {
	thoughts := make([]memory.Thought[map[string]any], 0, len(chunks))
	for i, chunk := range chunks {
		chunkID := fmt.Sprintf("core_knowledge_%d_%x", i, sha256.Sum256([]byte(chunk.Title+chunk.Content)))
		batchText := fmt.Sprintf("Base Knowledge -> %s -> %s\n%s", chunk.Category, chunk.Title, chunk.Content)

		payload := map[string]any{
			"id":       chunkID,
			"category": chunk.Category,
			"title":    chunk.Title,
			"ts":       time.Now().UnixMilli(),
			"type":     "semantic_base",
			"content":  chunk.Content,
		}

		thoughts = append(thoughts, memory.Thought[map[string]any]{
			Summaries:    []string{batchText},
			CategoryPath: chunk.Category,
			Data:         payload,
		})
	}
	return thoughts
}

func batchSemanticBaseKnowledgeThoughts(thoughts []memory.Thought[map[string]any], batchSize int) [][]memory.Thought[map[string]any] {
	if batchSize <= 0 {
		batchSize = len(thoughts)
	}
	if len(thoughts) == 0 {
		return nil
	}

	batches := make([][]memory.Thought[map[string]any], 0, (len(thoughts)+batchSize-1)/batchSize)
	for start := 0; start < len(thoughts); start += batchSize {
		end := start + batchSize
		if end > len(thoughts) {
			end = len(thoughts)
		}
		batches = append(batches, thoughts[start:end])
	}
	return batches
}

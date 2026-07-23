package sopbackend

import (
	"encoding/json"
)

func parseIngestPayload(data []byte) ([]SpaceIngestChunk, error) {
	var env struct {
		Items []SpaceIngestChunk `json:"items"`
	}
	if err := json.Unmarshal(data, &env); err == nil && len(env.Items) > 0 {
		return env.Items, nil
	}
	var chunks []SpaceIngestChunk
	err := json.Unmarshal(data, &chunks)
	return chunks, err
}

func getVectorsToUse(chunk SpaceIngestChunk) [][]float32 {
	if len(chunk.SummariesVectors) > 0 {
		return chunk.SummariesVectors
	}
	return chunk.Vectors
}

func buildChunkData(cid string, chunk SpaceIngestChunk, documentMode bool) map[string]any {
	if chunk.Data != nil && len(chunk.Data) > 0 {
		if _, exists := chunk.Data["original_id"]; !exists && cid != "" {
			chunk.Data["original_id"] = cid
		}
		if len(chunk.DocID) > 0 {
			chunk.Data["doc_id"] = chunk.DocID
		}
		return chunk.Data
	}

	text := chunk.Text
	desc := chunk.Description

	res := map[string]any{
		"text":        text,
		"description": desc,
		"category":    chunk.Category,
		"original_id": cid,
	}

	if len(chunk.DocID) > 0 {
		res["doc_id"] = chunk.DocID
	}

	return res
}

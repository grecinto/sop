package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	aidb "github.com/sharedcode/sop/ai/database"
	coredb "github.com/sharedcode/sop/database"
)

func TestPromoteAndRetrieveSharedMemoryArtifact(t *testing.T) {
	ctx := context.Background()
	storagePath := t.TempDir()

	opts := sop.DatabaseOptions{
		Type:          sop.Standalone,
		StoresFolders: []string{storagePath},
	}
	if _, err := coredb.Setup(ctx, opts); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	sysDB := aidb.NewDatabase(opts)
	if err := PromoteSharedMemoryArtifact(ctx, sysDB, nil, nil, "Retry auth failures with a short backoff before escalating", "core_playbook", "Fleet Learning/general", "scope", "shared", map[string]any{"source": "test"}); err != nil {
		t.Fatalf("PromoteSharedMemoryArtifact() error = %v", err)
	}

	hits, err := RetrieveSharedMemory(ctx, sysDB, nil, nil, "auth failures backoff", "core_playbook", 5)
	if err != nil {
		t.Fatalf("RetrieveSharedMemory() error = %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least one shared-memory hit")
	}

	found := false
	for _, hit := range hits {
		if txt, ok := hit.Payload["text"].(string); ok && strings.Contains(txt, "backoff") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected shared-memory hit payload to include the promoted text, got %#v", hits[0].Payload)
	}
}

func TestPromoteKnowledgeArtifactCreatesNamedKnowledgeBaseEntry(t *testing.T) {
	ctx := context.Background()
	storagePath := t.TempDir()

	opts := sop.DatabaseOptions{
		Type:          sop.Standalone,
		StoresFolders: []string{storagePath},
	}
	if _, err := coredb.Setup(ctx, opts); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	sysDB := aidb.NewDatabase(opts)
	reference := ai.ArtifactReference{Name: "go-best-practices", Type: ai.ArtifactTypeSpace, DatabaseName: "system"}
	if err := PromoteKnowledgeArtifact(ctx, sysDB, nil, nil, "Use short backoff retries for transient auth failures", reference, "", "shared_memory", "scope", "shared", map[string]any{"source": "hq_sync"}); err != nil {
		t.Fatalf("PromoteKnowledgeArtifact() error = %v", err)
	}

	hits, err := RetrieveSharedMemory(ctx, sysDB, nil, nil, "transient auth failures", "go-best-practices", 5)
	if err != nil {
		t.Fatalf("RetrieveSharedMemory() error = %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least one named-artifact hit")
	}

	if got := ExtractArtifactReference(map[string]any{"artifact_ref": map[string]any{"name": "go-best-practices", "type": "Space", "db_name": "system"}}); got.Name != reference.Name || got.DatabaseName != reference.DatabaseName {
		t.Fatalf("expected artifact reference to round-trip, got %#v", got)
	}
}

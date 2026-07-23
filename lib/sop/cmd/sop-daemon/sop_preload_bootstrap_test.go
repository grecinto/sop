package main

import (
	"context"
	"testing"
)

func TestBootstrapSOPKnowledgeBaseIfNeededSkipsExistingSpace(t *testing.T) {
	var preloadCalls int

	err := bootstrapSOPKnowledgeBaseIfNeeded(
		context.Background(),
		"system",
		func(context.Context, string) (bool, error) { return true, nil },
		func(context.Context, string) error {
			preloadCalls++
			return nil
		},
	)
	if err != nil {
		t.Fatalf("bootstrapSOPKnowledgeBaseIfNeeded() error = %v", err)
	}
	if preloadCalls != 0 {
		t.Fatalf("expected preload to be skipped for existing space, got %d calls", preloadCalls)
	}
}

func TestBootstrapSOPKnowledgeBaseIfNeededTriggersPreloadWhenMissing(t *testing.T) {
	var preloadCalls int
	var preloadDB string

	err := bootstrapSOPKnowledgeBaseIfNeeded(
		context.Background(),
		"system",
		func(context.Context, string) (bool, error) { return false, nil },
		func(_ context.Context, db string) error {
			preloadCalls++
			preloadDB = db
			return nil
		},
	)
	if err != nil {
		t.Fatalf("bootstrapSOPKnowledgeBaseIfNeeded() error = %v", err)
	}
	if preloadCalls != 1 {
		t.Fatalf("expected preload to run once, got %d calls", preloadCalls)
	}
	if preloadDB != "system" {
		t.Fatalf("expected preload for database %q, got %q", "system", preloadDB)
	}
}

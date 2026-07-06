package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCollectSharedMemoryArtifactsSkipsEmptyPayload(t *testing.T) {
	withIsolatedSystemDB(t)

	artifacts, err := collectSharedMemoryArtifacts(context.Background())
	if err != nil {
		t.Fatalf("collectSharedMemoryArtifacts returned error: %v", err)
	}
	if artifacts == nil {
		return
	}
	if len(artifacts) > 0 {
		t.Fatalf("expected no artifacts for empty local store, got %d", len(artifacts))
	}
}

func TestDaemonAuthHandlerSyncWithHQSkipsWhenNoSessionStore(t *testing.T) {
	h := &DaemonAuthHandler{}
	if err := h.syncWithHQ(context.Background()); err != nil {
		t.Fatalf("expected no-op sync to succeed, got %v", err)
	}
}

func TestDaemonAuthHandlerStoreRetrievedArtifactsNoop(t *testing.T) {
	h := &DaemonAuthHandler{}
	if err := h.storeRetrievedArtifacts(context.Background(), nil); err != nil {
		t.Fatalf("expected no-op artifact storage to succeed, got %v", err)
	}
}

func TestDaemonAuthHandlerHandleBootstrapRejectsEmptyBody(t *testing.T) {
	h := &DaemonAuthHandler{}
	req := httptest.NewRequest(http.MethodPost, "/bootstrap", nil)
	rrw := httptest.NewRecorder()

	h.HandleBootstrap(rrw, req)
	if rrw.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %d", rrw.Code)
	}
}

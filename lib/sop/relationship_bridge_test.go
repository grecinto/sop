package sop

import (
	"reflect"
	"testing"
)

func TestConfigureBridgeStore_AddsRelationAndAIHints(t *testing.T) {
	relation := Relation{
		SourceFields: []string{"user_id"},
		TargetStore:  "users",
		TargetFields: []string{"id"},
	}

	opts := ConfigureStore("users_subscriptions", false, 8, "Bridge store for user subscriptions", SmallData, "")
	opts.SetRelation(relation, "Bridge store for user subscriptions")

	if opts.Name != "users_subscriptions" {
		t.Fatalf("expected store name users_subscriptions, got %q", opts.Name)
	}
	if opts.Description != "Bridge store for user subscriptions" {
		t.Fatalf("unexpected description: %q", opts.Description)
	}
	if len(opts.Relations) != 1 {
		t.Fatalf("expected one relation, got %d", len(opts.Relations))
	}
	if !reflect.DeepEqual(opts.Relations[0], relation) {
		t.Fatalf("expected relation %+v, got %+v", relation, opts.Relations[0])
	}
	if got, ok := opts.CustomData["relationship_bridge"]; !ok || got != true {
		t.Fatalf("expected relationship_bridge metadata, got %#v", got)
	}
	if got, ok := opts.CustomData["relationship_target_store"]; !ok || got != "users" {
		t.Fatalf("expected target store metadata, got %#v", got)
	}
	aiSemantics, ok := opts.CustomData["ai_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("expected ai_semantics metadata, got %#v", opts.CustomData["ai_semantics"])
	}
	if aiSemantics["kind"] != "bridge" {
		t.Fatalf("expected bridge ai semantic kind, got %#v", aiSemantics["kind"])
	}
}

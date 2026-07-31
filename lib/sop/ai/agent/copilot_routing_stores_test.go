package agent

import (
	"context"
	"testing"

	"github.com/sharedcode/sop/ai"
)

func TestClassifyTaskContextV2_MapsStoreQueriesToStoreDomains(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		response   string
		wantDomain string
		wantIntent string
		wantPhase  string
	}{
		{
			name:       "stores bug fix",
			query:      "fix the users table in Stores",
			response:   `{"domains":["stores"],"phase":"execution","phase_plan":["investigation","execution"],"intent":"generate_code","confidence":"high","reasoning":"store bug fix"}`,
			wantDomain: "stores",
			wantIntent: "generate_code",
			wantPhase:  "execution",
		},
		{
			name:       "stores read query",
			query:      "show me the store users",
			response:   `{"domains":["stores"],"phase":"investigation","phase_plan":["investigation"],"intent":"read_data","confidence":"high","reasoning":"read data"}`,
			wantDomain: "stores",
			wantIntent: "read_data",
			wantPhase:  "investigation",
		},
		{
			name:       "desktop automation over stores",
			query:      "run shell command to query Stores",
			response:   `{"domains":["desktop","stores"],"phase":"execution","phase_plan":["execution"],"intent":"read_data","confidence":"high","reasoning":"desktop command"}`,
			wantDomain: "desktop",
			wantIntent: "read_data",
			wantPhase:  "execution",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ag := &CopilotAgent{}
			classification, err := ag.ClassifyTaskContextV2(context.Background(), tt.query, &v2RoutingTestGenerator{Response: tt.response})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(classification.Domains) == 0 {
				t.Fatal("expected classification domains")
			}
			if !containsDomain(classification.Domains, tt.wantDomain) {
				t.Fatalf("expected domains to include %q, got %v", tt.wantDomain, classification.Domains)
			}
			if classification.Intent != tt.wantIntent {
				t.Fatalf("expected intent %q, got %q", tt.wantIntent, classification.Intent)
			}
			if classification.Phase != tt.wantPhase {
				t.Fatalf("expected phase %q, got %q", tt.wantPhase, classification.Phase)
			}
		})
	}
}

func TestEvaluateRoutingGatesV2_UsesDiscoveryGateForStoreColdStart(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{Variables: make(map[string]any)})

	taskCtx, err := ag.evaluateRoutingGatesV2(ctx, "fix the users table in Stores", &v2RoutingTestGenerator{Response: `{"domains":["stores"],"phase":"investigation","phase_plan":["investigation"],"intent":"read_data","confidence":"high","reasoning":"read data"}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskCtx == nil {
		t.Fatal("expected a routing classification")
	}
	if taskCtx.RoutingGate != RoutingGateDiscovery {
		t.Fatalf("expected discovery gate, got %q", taskCtx.RoutingGate)
	}
	if taskCtx.Domain != "stores" {
		t.Fatalf("expected stores domain, got %q", taskCtx.Domain)
	}
}

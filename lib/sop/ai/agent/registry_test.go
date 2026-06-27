package agent

import "testing"

func TestRegistryListPreservesRegistrationOrder(t *testing.T) {
	r := NewRegistry()

	r.Register("zeta", "zeta tool", "{}", nil)
	r.Register("alpha", "alpha tool", "{}", nil)
	r.Register("middle", "middle tool", "{}", nil)

	tools := r.List()
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}

	want := []string{"zeta", "alpha", "middle"}
	for i, name := range want {
		if tools[i].Name != name {
			t.Fatalf("tool %d = %q, want %q", i, tools[i].Name, name)
		}
	}
}

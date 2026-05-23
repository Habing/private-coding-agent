package tools

import (
	"testing"

	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// TestMutatingMarkers locks the v1 contract for Workflow Engine Dry-Run: only
// the side-effecting tools advertise IsMutating()==true; everything else
// stays non-mutating (either by not implementing the interface or by
// returning false). Failing this test would let Dry-Run silently execute
// mutating ops, which is exactly what the test protects against.
func TestMutatingMarkers(t *testing.T) {
	mustMutate := []toolbus.Tool{
		NewFSWrite(nil),
		NewShellExec(nil),
		NewMemorySave(nil),
		NewMemoryDelete(nil),
	}
	for _, tool := range mustMutate {
		m, ok := tool.(toolbus.Mutating)
		if !ok {
			t.Fatalf("%s missing Mutating interface", tool.Name())
		}
		if !m.IsMutating() {
			t.Fatalf("%s expected IsMutating()=true", tool.Name())
		}
	}

	mustNotMutate := []toolbus.Tool{
		NewFSRead(nil),
		NewFSList(nil),
		NewFSGlob(nil),
		NewMemoryList(nil),
		NewMemorySearch(nil),
		NewHTTPFetch(HTTPFetchConfig{Enabled: true, AllowHosts: []string{"example.com"}}),
	}
	for _, tool := range mustNotMutate {
		if m, ok := tool.(toolbus.Mutating); ok && m.IsMutating() {
			t.Fatalf("%s unexpectedly mutating", tool.Name())
		}
	}
}

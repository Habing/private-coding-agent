package toolbus

import (
	"fmt"
	"sort"
	"sync"
)

// Registry holds the active in-process tools by name.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

// Register adds a tool. Returns an error on duplicate name.
func (r *Registry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := t.Name()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("toolbus: duplicate tool name %q", name)
	}
	r.tools[name] = t
	return nil
}

// Unregister removes a tool by name. Returns an error if it was not present
// so the caller can distinguish "did nothing" from "removed". Workflow
// Engine's Publish/Unpublish path relies on this to swap a refreshed DSL into
// the live ToolBus without restarting the server.
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; !exists {
		return fmt.Errorf("toolbus: tool %q not registered", name)
	}
	delete(r.tools, name)
	return nil
}

// Get returns the tool by name (ok=false if missing).
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// List returns all tools sorted by name.
func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

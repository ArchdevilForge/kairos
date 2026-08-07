package playbook

import "sync"

// Registry holds enabled playbooks.
type Registry struct {
	mu   sync.RWMutex
	byID map[string]Playbook
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byID: make(map[string]Playbook)}
}

// DefaultRegistry enables stage-1 leader_pullback only.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(&LeaderPullback{})
	return r
}

// Register adds or replaces a playbook by ID.
func (r *Registry) Register(p Playbook) {
	r.mu.Lock()
	r.byID[p.ID()] = p
	r.mu.Unlock()
}

// Get returns a playbook by id.
func (r *Registry) Get(id string) (Playbook, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byID[id]
	return p, ok
}

// All returns a snapshot of registered playbooks.
func (r *Registry) All() []Playbook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Playbook, 0, len(r.byID))
	for _, p := range r.byID {
		out = append(out, p)
	}
	return out
}

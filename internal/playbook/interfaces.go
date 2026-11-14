package playbook

// Playbook matches a structured opportunity definition against a context.
type Playbook interface {
	ID() string
}

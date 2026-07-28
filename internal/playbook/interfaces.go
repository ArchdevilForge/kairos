package playbook

import "github.com/ArchdevilForge/kairos/internal/types"

// Playbook matches a structured opportunity definition against a context.
type Playbook interface {
	ID() string
	Match(types.PlaybookContext) types.PlaybookMatch
}

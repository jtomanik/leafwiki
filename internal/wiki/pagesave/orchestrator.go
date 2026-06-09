package pagesave

// PageSideEffect is implemented by any component that reacts to a page mutation.
// Apply is always called synchronously and errors are handled internally (best-effort).
type PageSideEffect interface {
	Apply(event PageSaveEvent)
}

// RequiredPageSideEffect can veto the remaining side effects when the mutation
// must not be treated as fully processed after a failure.
type RequiredPageSideEffect interface {
	ApplyRequired(event PageSaveEvent) error
}

// PageSaveOrchestrator fans out a PageSaveEvent to all registered side effects.
type PageSaveOrchestrator struct {
	sideEffects []PageSideEffect
}

// NewPageSaveOrchestrator creates an orchestrator with the given side effects.
func NewPageSaveOrchestrator(effects ...PageSideEffect) *PageSaveOrchestrator {
	return &PageSaveOrchestrator{sideEffects: effects}
}

// Run delivers the event to each side effect in registration order.
func (o *PageSaveOrchestrator) Run(event PageSaveEvent) error {
	for _, se := range o.sideEffects {
		if required, ok := se.(RequiredPageSideEffect); ok {
			if err := required.ApplyRequired(event); err != nil {
				return err
			}
			continue
		}
		se.Apply(event)
	}
	return nil
}

package notify

import "context"

// Event is a future notification payload emitted by core workflows.
type Event struct {
	Topic   string
	Summary string
	Details map[string]string
}

// Notifier is the seam for future notification implementations.
type Notifier interface {
	Notify(ctx context.Context, event Event) error
}

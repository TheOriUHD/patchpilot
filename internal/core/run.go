package core

// RunStatus describes the lifecycle state of an update run.
type RunStatus string

const (
	// RunStatusQueued is a durable manual run waiting behind an active run.
	RunStatusQueued RunStatus = "queued"
	// RunStatusRunning is a run currently being executed by the daemon.
	RunStatusRunning RunStatus = "running"
	// RunStatusSuccess is a run where every host completed successfully.
	RunStatusSuccess RunStatus = "success"
	// RunStatusPartialFailure is a run where at least one host failed.
	RunStatusPartialFailure RunStatus = "partial_failure"
	// RunStatusFailed is a run that failed before it could complete normally.
	RunStatusFailed RunStatus = "failed"
	// RunStatusCancelled is a run cancelled by an operator.
	RunStatusCancelled RunStatus = "cancelled"
)

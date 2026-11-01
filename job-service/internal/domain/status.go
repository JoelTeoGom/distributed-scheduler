package domain

// There is no failed status: a failed attempt goes back to pending (retry) or to dead (retries exhausted).
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusDead      Status = "dead"
)

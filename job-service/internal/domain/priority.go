package domain

// Priority is numeric so the claim query can ORDER BY it: lower = more urgent.
type Priority uint8

const (
	PriorityCritical Priority = iota
	PriorityHigh
	PriorityNormal
	PriorityLow
)

package domain

import "errors"

var (
	// The message is generic on purpose: the tenant reads it, and it must not reveal internal addresses.
	ErrDestinationBlocked = errors.New("destination not allowed")
	// The reconciler took the job back after its lease expired, so this executor no longer owns it.
	ErrLeaseLost = errors.New("lease lost")
)

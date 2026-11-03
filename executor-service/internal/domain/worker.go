package domain

import "time"

// Worker is the executor that holds the job while it runs.
type Worker struct {
	Name           string
	LeaseExpiresAt time.Time // if it passes, the reconciler takes the job back
}

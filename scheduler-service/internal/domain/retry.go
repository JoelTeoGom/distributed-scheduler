package domain

import "time"

const (
	maxAttemptsLimit = 255
	maxBackoffBase   = 24 * time.Hour
)

type RetryPolicy struct {
	MaxAttempts int
	BackoffBase time.Duration // delay before retry n = BackoffBase * 2^(n-1)
}

func (r RetryPolicy) validate() error {
	if r.MaxAttempts < 1 || r.MaxAttempts > maxAttemptsLimit {
		return invalid("max_attempts must be between 1 and 255")
	}
	if r.BackoffBase <= 0 || r.BackoffBase > maxBackoffBase || r.BackoffBase%time.Second != 0 {
		return invalid("backoff base must be a whole number of seconds, up to one day")
	}
	return nil
}

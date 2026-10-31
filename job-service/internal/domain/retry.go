package domain

import "time"

type RetryPolicy struct {
	MaxAttempts int
	BackoffBase time.Duration
}

package domain

import "time"

type RetryPolicy struct {
	MaxAttempts int
	BackoffBase time.Duration // delay before retry n = BackoffBase * 2^(n-1)
}

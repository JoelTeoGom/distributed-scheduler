package domain

import "time"

// Config is copied from the task when the job is created, so editing the task does not change existing jobs.
type Config struct {
	Priority Priority
	Request  Request
	Timeout  time.Duration
	Retry    RetryPolicy
}

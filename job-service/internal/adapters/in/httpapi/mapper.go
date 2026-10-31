package httpapi

import (
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/ports"
)

var priorityNames = map[domain.Priority]string{
	domain.PriorityCritical: "critical",
	domain.PriorityHigh:     "high",
	domain.PriorityNormal:   "normal",
	domain.PriorityLow:      "low",
}

func toJobResponse(job domain.Job) JobResponse {
	return JobResponse{
		ID:          job.ID.String(),
		TaskID:      job.TaskID.String(),
		ScheduledAt: job.ScheduledAt,
		Priority:    priorityNames[job.Config.Priority],
		Request: HTTPRequestDTO{
			Method:    job.Config.Request.Method,
			URL:       job.Config.Request.URL,
			Headers:   job.Config.Request.Headers,
			Body:      string(job.Config.Request.Body),
			SecretRef: job.Config.Request.SecretRef,
		},
		TimeoutSeconds: durationToSeconds(job.Config.Timeout),
		Retry: RetryDTO{
			MaxAttempts:        job.Config.Retry.MaxAttempts,
			BackoffBaseSeconds: durationToSeconds(job.Config.Retry.BackoffBase),
		},
		Status:        string(job.Status),
		Attempt:       job.Attempt,
		NextAttemptAt: job.NextAttemptAt,
		Result: ResultDTO{
			StartedAt:    job.Result.StartedAt,
			FinishedAt:   job.Result.FinishedAt,
			HTTPStatus:   job.Result.HTTPStatus,
			Error:        job.Result.Error,
			ResponseBody: string(job.Result.ResponseBody),
		},
		CreatedAt: job.CreatedAt,
		UpdatedAt: job.UpdatedAt,
	}
}

func toJobPageResponse(page ports.JobPage) JobPageResponse {
	jobs := make([]JobResponse, 0, len(page.Jobs))
	for _, job := range page.Jobs {
		jobs = append(jobs, toJobResponse(job))
	}
	return JobPageResponse{Jobs: jobs, NextCursor: page.NextCursor}
}

func durationToSeconds(duration time.Duration) uint32 {
	return uint32(duration / time.Second)
}

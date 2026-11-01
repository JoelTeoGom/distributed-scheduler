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

func toJobResponse(job *domain.Job) JobResponse {
	response := JobResponse{
		ID:          job.ID.String(),
		TaskID:      job.TaskID.String(),
		ScheduledAt: job.ScheduledAt,
		Priority:    priorityNames[job.Config.Priority],
		Status:      string(job.Status),
		Attempt:     job.Attempt,
		Result: ResultDTO{
			StartedAt:    job.Result.StartedAt,
			FinishedAt:   job.Result.FinishedAt,
			HTTPStatus:   job.Result.HTTPStatus,
			Error:        job.Result.Error,
			ResponseBody: string(job.Result.ResponseBody),
		},
	}

	// Archived jobs have a reduced view: Cassandra does not store their config.
	if job.Config.Request.Method == "" {
		return response
	}

	timeoutSeconds := durationToSeconds(job.Config.Timeout)
	response.Request = &HTTPRequestDTO{
		Method:    job.Config.Request.Method,
		URL:       job.Config.Request.URL,
		Headers:   job.Config.Request.Headers,
		Body:      string(job.Config.Request.Body),
		SecretRef: job.Config.Request.SecretRef,
	}
	response.TimeoutSeconds = &timeoutSeconds
	response.Retry = &RetryDTO{
		MaxAttempts:        job.Config.Retry.MaxAttempts,
		BackoffBaseSeconds: durationToSeconds(job.Config.Retry.BackoffBase),
	}
	response.NextAttemptAt = &job.NextAttemptAt
	response.CreatedAt = &job.CreatedAt
	response.UpdatedAt = &job.UpdatedAt
	return response
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

package httpapi

import (
	"context"
	"net/http"

	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/domain"
	"github.com/google/uuid"
)

type jobOperation func(ctx context.Context, tenantID, jobID uuid.UUID) (domain.Job, error)

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:  "ok",
		Service: s.serviceName,
		Version: s.serviceVersion,
	})
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) error {
	return s.applyToJob(w, r, s.jobService.Get)
}

func (s *Server) retryJob(w http.ResponseWriter, r *http.Request) error {
	return s.applyToJob(w, r, s.jobService.Retry)
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) error {
	if err := requireDeadStatus(r); err != nil {
		return err
	}

	page, err := parsePageRequest(r)
	if err != nil {
		return err
	}

	jobPage, err := s.jobService.ListDead(r.Context(), tenantIDFrom(r.Context()), page)
	if err != nil {
		return err
	}

	writeJSON(w, http.StatusOK, toJobPageResponse(jobPage))
	return nil
}

func (s *Server) listTaskJobs(w http.ResponseWriter, r *http.Request) error {
	taskID, err := parseID(r)
	if err != nil {
		return err
	}

	page, err := parsePageRequest(r)
	if err != nil {
		return err
	}

	jobPage, err := s.jobService.ListByTask(r.Context(), tenantIDFrom(r.Context()), taskID, page)
	if err != nil {
		return err
	}

	writeJSON(w, http.StatusOK, toJobPageResponse(jobPage))
	return nil
}

func (s *Server) applyToJob(w http.ResponseWriter, r *http.Request, operation jobOperation) error {
	jobID, err := parseID(r)
	if err != nil {
		return err
	}

	job, err := operation(r.Context(), tenantIDFrom(r.Context()), jobID)
	if err != nil {
		return err
	}

	writeJSON(w, http.StatusOK, toJobResponse(job))
	return nil
}

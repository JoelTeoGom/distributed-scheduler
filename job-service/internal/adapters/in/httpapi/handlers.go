package httpapi

import "net/http"

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:  "ok",
		Service: s.serviceName,
		Version: s.serviceVersion,
	})
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) error {
	jobID, err := parseID(r)
	if err != nil {
		return err
	}

	job, err := s.jobService.Get(r.Context(), tenantIDFrom(r.Context()), jobID)
	if err != nil {
		return err
	}

	writeJSON(w, http.StatusOK, toJobResponse(job))
	return nil
}

func (s *Server) retryJob(w http.ResponseWriter, r *http.Request) error {
	jobID, err := parseID(r)
	if err != nil {
		return err
	}

	if err := s.jobService.Retry(r.Context(), tenantIDFrom(r.Context()), jobID); err != nil {
		return err
	}

	w.WriteHeader(http.StatusAccepted)
	return nil
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) error {
	query, err := parseDeadQuery(r)
	if err != nil {
		return err
	}

	jobPage, err := s.jobService.ListDead(r.Context(), query)
	if err != nil {
		return err
	}

	writeJSON(w, http.StatusOK, toJobPageResponse(jobPage))
	return nil
}

func (s *Server) listTaskJobs(w http.ResponseWriter, r *http.Request) error {
	query, err := parseHistoryQuery(r)
	if err != nil {
		return err
	}

	jobPage, err := s.jobService.History(r.Context(), query)
	if err != nil {
		return err
	}

	writeJSON(w, http.StatusOK, toJobPageResponse(jobPage))
	return nil
}

package httpapi

import (
	"context"
	"net/http"

	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/domain"
	"github.com/google/uuid"
)

type taskOperation func(ctx context.Context, tenantID, taskID uuid.UUID) (domain.Task, error)

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:  "ok",
		Service: s.serviceName,
		Version: s.serviceVersion,
	})
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) error {
	var request CreateTaskRequest
	body, err := decodeJSON(w, r, &request)
	if err != nil {
		return err
	}

	input, err := request.toInput()
	if err != nil {
		return err
	}

	idempotency, err := parseIdempotencyKey(r.Header.Get(idempotencyKeyHeader), body)
	if err != nil {
		return err
	}

	task, alreadyCreated, err := s.taskService.Create(r.Context(), tenantIDFrom(r.Context()), idempotency, input)
	if err != nil {
		return err
	}

	status := http.StatusCreated
	if alreadyCreated {
		status = http.StatusOK
	}
	w.Header().Set("Location", "/tasks/"+task.ID.String())
	writeTask(w, status, task)
	return nil
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) error {
	page, err := parsePageRequest(r)
	if err != nil {
		return err
	}

	taskPage, err := s.taskService.List(r.Context(), tenantIDFrom(r.Context()), page)
	if err != nil {
		return err
	}

	writeJSON(w, http.StatusOK, toTaskPageResponse(taskPage))
	return nil
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) error {
	return s.applyToTask(w, r, s.taskService.Get)
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) error {
	taskID, err := parseTaskID(r)
	if err != nil {
		return err
	}

	expectedVersion, err := parseExpectedVersion(r.Header.Get(ifMatchHeader))
	if err != nil {
		return err
	}

	var request UpdateTaskRequest
	if _, err := decodeJSON(w, r, &request); err != nil {
		return err
	}

	input, err := request.toInput()
	if err != nil {
		return err
	}

	task, err := s.taskService.Update(r.Context(), tenantIDFrom(r.Context()), taskID, expectedVersion, input)
	if err != nil {
		return err
	}

	writeTask(w, http.StatusOK, task)
	return nil
}

func (s *Server) pauseTask(w http.ResponseWriter, r *http.Request) error {
	return s.applyToTask(w, r, s.taskService.Pause)
}

func (s *Server) resumeTask(w http.ResponseWriter, r *http.Request) error {
	return s.applyToTask(w, r, s.taskService.Resume)
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) error {
	taskID, err := parseTaskID(r)
	if err != nil {
		return err
	}

	if err := s.taskService.Delete(r.Context(), tenantIDFrom(r.Context()), taskID); err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) applyToTask(w http.ResponseWriter, r *http.Request, operation taskOperation) error {
	taskID, err := parseTaskID(r)
	if err != nil {
		return err
	}

	task, err := operation(r.Context(), tenantIDFrom(r.Context()), taskID)
	if err != nil {
		return err
	}

	writeTask(w, http.StatusOK, task)
	return nil
}

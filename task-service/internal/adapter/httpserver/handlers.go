package httpserver

import (
	"context"
	"errors"
	"net/http"

	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/adapter/httpserver/middleware"
	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/ports"
	"github.com/google/uuid"
)

type taskOperation func(ctx context.Context, tenantID, taskID uuid.UUID) (domain.Task, error)

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:  "ok",
		Service: s.serviceName,
		Version: s.serviceVersion,
	})
}

func (s *Server) createTaskHandler(w http.ResponseWriter, r *http.Request) {
	var request CreateTaskRequest
	body, ok := readJSON(w, r, &request)
	if !ok {
		return
	}

	input, err := request.toInput()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	idempotency, err := parseIdempotencyKey(r.Header.Get(idempotencyKeyHeader), body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	task, err := s.taskService.Create(r.Context(), middleware.TenantID(r.Context()), idempotency, input)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	w.Header().Set("Location", "/tasks/"+task.ID.String())
	writeTask(w, http.StatusCreated, task)
}

func (s *Server) listTasksHandler(w http.ResponseWriter, r *http.Request) {
	page, err := parsePageRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	taskPage, err := s.taskService.List(r.Context(), middleware.TenantID(r.Context()), page)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, toTaskPageResponse(taskPage))
}

func (s *Server) getTaskHandler(w http.ResponseWriter, r *http.Request) {
	s.applyToTask(w, r, s.taskService.Get)
}

func (s *Server) updateTaskHandler(w http.ResponseWriter, r *http.Request) {
	taskID, err := parseTaskID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ifMatch := r.Header.Get("If-Match")
	if ifMatch == "" {
		writeError(w, http.StatusPreconditionRequired, "If-Match header is required")
		return
	}
	expectedVersion, err := parseExpectedVersion(ifMatch)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var request UpdateTaskRequest
	if _, ok := readJSON(w, r, &request); !ok {
		return
	}

	input, err := request.toInput()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	task, err := s.taskService.Update(r.Context(), middleware.TenantID(r.Context()), taskID, expectedVersion, input)
	if errors.Is(err, ports.ErrVersionConflict) {
		writeError(w, http.StatusPreconditionFailed, err.Error())
		return
	}
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	writeTask(w, http.StatusOK, task)
}

func (s *Server) pauseTaskHandler(w http.ResponseWriter, r *http.Request) {
	s.applyToTask(w, r, s.taskService.Pause)
}

func (s *Server) resumeTaskHandler(w http.ResponseWriter, r *http.Request) {
	s.applyToTask(w, r, s.taskService.Resume)
}

func (s *Server) deleteTaskHandler(w http.ResponseWriter, r *http.Request) {
	taskID, err := parseTaskID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.taskService.Delete(r.Context(), middleware.TenantID(r.Context()), taskID); err != nil {
		writeServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) applyToTask(w http.ResponseWriter, r *http.Request, operation taskOperation) {
	taskID, err := parseTaskID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	task, err := operation(r.Context(), middleware.TenantID(r.Context()), taskID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	writeTask(w, http.StatusOK, task)
}

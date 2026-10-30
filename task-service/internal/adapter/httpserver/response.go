package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/ports"
)

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("write response", "error", err)
	}
}

func writeTask(w http.ResponseWriter, status int, task domain.Task) {
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, task.Version))
	writeJSON(w, status, toTaskResponse(task))
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{Error: message})
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ports.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ports.ErrVersionConflict), errors.Is(err, ports.ErrDuplicateKey):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ports.ErrIdempotencyMismatch):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

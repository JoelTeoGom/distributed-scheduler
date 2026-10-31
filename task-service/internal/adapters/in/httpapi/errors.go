package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/domain"
)

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{Error: message})
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrVersionConflict), errors.Is(err, domain.ErrDuplicateKey):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrIdempotencyMismatch):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

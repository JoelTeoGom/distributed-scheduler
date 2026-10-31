package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/domain"
)

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, message := http.StatusInternalServerError, "internal server error"

	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		status, message = http.StatusBadRequest, err.Error()
	case errors.Is(err, domain.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", "Bearer")
		status, message = http.StatusUnauthorized, domain.ErrUnauthorized.Error()
	case errors.Is(err, domain.ErrNotFound):
		status, message = http.StatusNotFound, err.Error()
	case errors.Is(err, domain.ErrNotRetryable):
		status, message = http.StatusConflict, err.Error()
	default:
		slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	}

	writeJSON(w, status, ErrorResponse{Error: message})
}

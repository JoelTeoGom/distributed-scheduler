package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/domain"
)

var errMissingIfMatch = errors.New("If-Match header is required")

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, message := http.StatusInternalServerError, "internal server error"

	var bodyTooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &bodyTooLarge):
		status, message = http.StatusRequestEntityTooLarge, "request body is too large"
	case errors.Is(err, domain.ErrInvalidInput):
		status, message = http.StatusBadRequest, err.Error()
	case errors.Is(err, domain.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", "Bearer")
		status, message = http.StatusUnauthorized, domain.ErrUnauthorized.Error()
	case errors.Is(err, domain.ErrNotFound):
		status, message = http.StatusNotFound, err.Error()
	case errors.Is(err, errMissingIfMatch):
		status, message = http.StatusPreconditionRequired, err.Error()
	case errors.Is(err, domain.ErrVersionConflict):
		status, message = versionConflictStatus(r), err.Error()
	case errors.Is(err, domain.ErrDuplicateKey):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, domain.ErrIdempotencyMismatch):
		status, message = http.StatusUnprocessableEntity, err.Error()
	default:
		slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	}

	writeJSON(w, status, ErrorResponse{Error: message})
}

func versionConflictStatus(r *http.Request) int {
	if r.Header.Get(ifMatchHeader) != "" {
		return http.StatusPreconditionFailed
	}
	return http.StatusConflict
}

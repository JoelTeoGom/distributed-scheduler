package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/domain"
)

const maxRequestBodyBytes = 1 << 20

// decodeJSON also returns the raw body, which the idempotency hash needs.
func decodeJSON(w http.ResponseWriter, r *http.Request, target any) ([]byte, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrInvalidInput, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrInvalidInput, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: unexpected data after JSON value", domain.ErrInvalidInput)
	}
	return body, nil
}

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

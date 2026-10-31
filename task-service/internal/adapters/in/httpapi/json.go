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

// readJSON returns the raw body too, which the idempotency hash needs.
// When it returns false the error response has already been written.
func readJSON(w http.ResponseWriter, r *http.Request, target any) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	var bodyTooLarge *http.MaxBytesError
	if errors.As(err, &bodyTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
		return nil, false
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read request body")
		return nil, false
	}

	if err := decodeStrictJSON(body, target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return nil, false
	}
	return body, true
}

func decodeStrictJSON(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("unexpected data after JSON value")
	}
	return nil
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

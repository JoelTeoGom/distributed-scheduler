package httpserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/ports"
	"github.com/google/uuid"
)

const (
	idempotencyKeyHeader    = "Idempotency-Key"
	maxIdempotencyKeyLength = 255
	maxRequestBodyBytes     = 1 << 20

	defaultPageLimit = 50
	maxPageLimit     = 100
)

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

func parseTaskID(r *http.Request) (uuid.UUID, error) {
	taskID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, errors.New("invalid task id")
	}
	return taskID, nil
}

func parseIdempotencyKey(key string, body []byte) (*ports.IdempotencyKey, error) {
	if key == "" {
		return nil, nil
	}
	if len(key) > maxIdempotencyKeyLength {
		return nil, fmt.Errorf("%s must be at most %d characters", idempotencyKeyHeader, maxIdempotencyKeyLength)
	}
	for i := 0; i < len(key); i++ {
		if key[i] < ' ' || key[i] > '~' {
			return nil, fmt.Errorf("%s must be printable ASCII", idempotencyKeyHeader)
		}
	}
	return &ports.IdempotencyKey{Key: key, RequestHash: sha256.Sum256(body)}, nil
}

func parseExpectedVersion(ifMatch string) (uint32, error) {
	version, err := strconv.ParseUint(strings.Trim(ifMatch, `"`), 10, 32)
	if err != nil {
		return 0, errors.New("If-Match must be the task version")
	}
	return uint32(version), nil
}

func parsePageRequest(r *http.Request) (ports.PageRequest, error) {
	query := r.URL.Query()
	page := ports.PageRequest{Cursor: query.Get("cursor"), Limit: defaultPageLimit}

	if limitParameter := query.Get("limit"); limitParameter != "" {
		limit, err := strconv.Atoi(limitParameter)
		if err != nil || limit < 1 || limit > maxPageLimit {
			return ports.PageRequest{}, fmt.Errorf("limit must be between 1 and %d", maxPageLimit)
		}
		page.Limit = limit
	}
	return page, nil
}

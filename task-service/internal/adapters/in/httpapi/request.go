package httpapi

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/ports"
	"github.com/google/uuid"
)

const (
	idempotencyKeyHeader    = "Idempotency-Key"
	maxIdempotencyKeyLength = 255

	defaultPageLimit = 50
	maxPageLimit     = 100
)

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

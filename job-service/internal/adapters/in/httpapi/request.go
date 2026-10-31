package httpapi

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/ports"
)

const (
	defaultPageLimit = 50
	maxPageLimit     = 100
)

func parseID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: invalid id", domain.ErrInvalidInput)
	}
	return id, nil
}

func requireDeadStatus(r *http.Request) error {
	if r.URL.Query().Get("status") != string(domain.StatusDead) {
		return fmt.Errorf("%w: status must be dead", domain.ErrInvalidInput)
	}
	return nil
}

func parsePageRequest(r *http.Request) (ports.PageRequest, error) {
	query := r.URL.Query()
	page := ports.PageRequest{Cursor: query.Get("cursor"), Limit: defaultPageLimit}

	if limitParameter := query.Get("limit"); limitParameter != "" {
		limit, err := strconv.Atoi(limitParameter)
		if err != nil || limit < 1 || limit > maxPageLimit {
			return ports.PageRequest{}, fmt.Errorf("%w: limit must be between 1 and %d", domain.ErrInvalidInput, maxPageLimit)
		}
		page.Limit = limit
	}
	return page, nil
}

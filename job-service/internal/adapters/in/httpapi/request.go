package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

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

func parseDeadQuery(r *http.Request) (ports.DeadQuery, error) {
	if r.URL.Query().Get("status") != string(domain.StatusDead) {
		return ports.DeadQuery{}, fmt.Errorf("%w: status must be dead", domain.ErrInvalidInput)
	}

	limit, err := parseLimit(r)
	if err != nil {
		return ports.DeadQuery{}, err
	}

	query := ports.DeadQuery{TenantID: tenantIDFrom(r.Context()), Limit: limit}
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		after, err := uuid.Parse(cursor)
		if err != nil {
			return ports.DeadQuery{}, fmt.Errorf("%w: invalid cursor", domain.ErrInvalidInput)
		}
		query.After = &after
	}
	return query, nil
}

func parseHistoryQuery(r *http.Request) (ports.HistoryQuery, error) {
	taskID, err := parseID(r)
	if err != nil {
		return ports.HistoryQuery{}, err
	}

	limit, err := parseLimit(r)
	if err != nil {
		return ports.HistoryQuery{}, err
	}

	query := ports.HistoryQuery{TenantID: tenantIDFrom(r.Context()), TaskID: taskID, Limit: limit}
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		before, err := time.Parse(time.RFC3339Nano, cursor)
		if err != nil {
			return ports.HistoryQuery{}, fmt.Errorf("%w: invalid cursor", domain.ErrInvalidInput)
		}
		query.Before = &before
	}
	return query, nil
}

func parseLimit(r *http.Request) (int, error) {
	limitParameter := r.URL.Query().Get("limit")
	if limitParameter == "" {
		return defaultPageLimit, nil
	}

	limit, err := strconv.Atoi(limitParameter)
	if err != nil || limit < 1 || limit > maxPageLimit {
		return 0, fmt.Errorf("%w: limit must be between 1 and %d", domain.ErrInvalidInput, maxPageLimit)
	}
	return limit, nil
}

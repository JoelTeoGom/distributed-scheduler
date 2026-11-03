package httpclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/ports"
)

const maxResponseBodySize = 4 << 10

type Executor struct {
	client *http.Client
}

var _ ports.RequestExecutor = (*Executor)(nil)

func NewExecutor() *Executor {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		// Commented out for now so local tests can call endpoints on localhost. Enable it before deploying.
		// Control: checkDestination,
	}

	transport := &http.Transport{
		// Only http.DefaultTransport reads HTTP_PROXY and HTTPS_PROXY, so this custom Transport already ignores them.
		// nil just makes it explicit: a proxy would make the dialer check the proxy address instead of the endpoint.
		Proxy:               nil,
		DialContext:         dialer.DialContext,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	return &Executor{client: &http.Client{
		Transport: transport,
		// A redirect could take the request away from the URL the task-service validated.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func (e *Executor) Execute(ctx context.Context, job *domain.Job) (domain.Result, error) {
	startedAt := time.Now()
	httpStatus, responseBody, err := e.send(ctx, job)
	finishedAt := time.Now()

	result := domain.Result{
		StartedAt:    &startedAt,
		FinishedAt:   &finishedAt,
		ResponseBody: responseBody,
	}
	if httpStatus != 0 {
		result.HTTPStatus = &httpStatus
	}
	if err != nil {
		result.Error = describeError(err)
	}
	if errors.Is(err, domain.ErrDestinationBlocked) {
		return result, domain.ErrDestinationBlocked
	}
	return result, nil
}

func (e *Executor) send(ctx context.Context, job *domain.Job) (int, []byte, error) {
	requestContext, cancel := context.WithTimeout(ctx, job.Config.Timeout)
	defer cancel()

	jobRequest := job.Config.Request
	request, err := http.NewRequestWithContext(requestContext, jobRequest.Method, jobRequest.URL, bytes.NewReader(jobRequest.Body))
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}

	for name, value := range jobRequest.Headers {
		request.Header.Set(name, value)
	}
	request.Header.Set("Idempotency-Key", job.ID.String())
	request.Header.Set("X-Scheduled-At", job.ScheduledAt.UTC().Format(time.RFC3339))
	request.Header.Set("X-Attempt", strconv.Itoa(job.Attempt))
	// TODO: resolve jobRequest.SecretRef into the credentials header once there is a secret store.

	response, err := e.client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBodySize))
	if err != nil {
		return response.StatusCode, responseBody, fmt.Errorf("read response: %w", err)
	}
	return response.StatusCode, responseBody, nil
}

// The error is visible to the tenant, so a blocked dial never shows the resolved address.
func describeError(err error) string {
	switch {
	case errors.Is(err, domain.ErrDestinationBlocked):
		return domain.ErrDestinationBlocked.Error()
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	return err.Error()
}

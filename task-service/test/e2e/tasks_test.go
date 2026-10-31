//go:build e2e

// Needs a running service: E2E_BASE_URL and the JWT_SECRET it was started with.
package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const concurrentRequests = 20

type taskResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Schedule struct {
		Type     string     `json:"type"`
		RunAt    *time.Time `json:"run_at"`
		CronExpr string     `json:"cron_expr"`
		Timezone string     `json:"timezone"`
	} `json:"schedule"`
	Priority string `json:"priority"`
	Request  struct {
		Method    string            `json:"method"`
		URL       string            `json:"url"`
		Headers   map[string]string `json:"headers"`
		Body      string            `json:"body"`
		SecretRef string            `json:"secret_ref"`
	} `json:"request"`
	TimeoutSeconds int `json:"timeout_seconds"`
	Retry          struct {
		MaxAttempts        int `json:"max_attempts"`
		BackoffBaseSeconds int `json:"backoff_base_seconds"`
	} `json:"retry"`
	Status    string     `json:"status"`
	NextRunAt *time.Time `json:"next_run_at"`
	Version   uint32     `json:"version"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type pageResponse struct {
	Tasks      []taskResponse `json:"tasks"`
	NextCursor string         `json:"next_cursor"`
}

type response struct {
	status  int
	headers http.Header
	body    []byte
}

type client struct {
	t     *testing.T
	token string
}

func baseURL() string {
	if value := os.Getenv("E2E_BASE_URL"); value != "" {
		return value
	}
	return "http://127.0.0.1:8080"
}

func jwtSecret(t *testing.T) string {
	t.Helper()
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		t.Fatal("JWT_SECRET is required")
	}
	return secret
}

func signToken(t *testing.T, secret string, claims jwt.MapClaims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func newClient(t *testing.T) client {
	t.Helper()
	return client{t: t, token: signToken(t, jwtSecret(t), jwt.MapClaims{
		"tenant_id": uuid.NewString(),
		"exp":       time.Now().Add(time.Hour).Unix(),
	})}
}

func (c client) attempt(method, target string, payload any, headers map[string]string) (response, error) {
	var reader io.Reader
	switch value := payload.(type) {
	case nil:
	case string:
		reader = strings.NewReader(value)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return response{}, err
		}
		reader = bytes.NewReader(encoded)
	}

	request, err := http.NewRequest(method, baseURL()+target, reader)
	if err != nil {
		return response{}, err
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}

	result, err := http.DefaultClient.Do(request)
	if err != nil {
		return response{}, err
	}
	defer result.Body.Close()

	body, err := io.ReadAll(result.Body)
	if err != nil {
		return response{}, err
	}
	return response{status: result.StatusCode, headers: result.Header, body: body}, nil
}

func (c client) send(wantStatus int, method, target string, payload any, headers map[string]string) response {
	c.t.Helper()
	result, err := c.attempt(method, target, payload, headers)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, target, err)
	}
	if result.status != wantStatus {
		c.t.Fatalf("%s %s: status %d, want %d: %s", method, target, result.status, wantStatus, result.body)
	}
	return result
}

func decode[T any](t *testing.T, result response) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(result.body, &value); err != nil {
		t.Fatalf("decode %s: %v", result.body, err)
	}
	return value
}

func cronPayload() map[string]any {
	return map[string]any{
		"name":     "nightly report",
		"schedule": map[string]any{"type": "cron", "cron_expr": "0 9 * * *", "timezone": "Europe/Madrid"},
		"priority": "high",
		"request": map[string]any{
			"method":     "POST",
			"url":        "https://api.example.com/hook",
			"headers":    map[string]string{"X-Trace": "1"},
			"body":       `{"report":"daily"}`,
			"secret_ref": "secrets/report",
		},
		"timeout_seconds": 30,
		"retry":           map[string]any{"max_attempts": 3, "backoff_base_seconds": 10},
	}
}

func atPayload(runAt time.Time) map[string]any {
	payload := cronPayload()
	payload["schedule"] = map[string]any{"type": "at", "run_at": runAt.Format(time.RFC3339)}
	return payload
}

func (c client) createTask(payload map[string]any) taskResponse {
	c.t.Helper()
	return decode[taskResponse](c.t, c.send(http.StatusCreated, http.MethodPost, "/tasks", payload, nil))
}

func taskPath(taskID string) string {
	return "/tasks/" + taskID
}

func ifMatch(version uint32) map[string]string {
	return map[string]string{"If-Match": fmt.Sprintf(`"%d"`, version)}
}

func TestHealthIsPublic(t *testing.T) {
	client{t: t}.send(http.StatusOK, http.MethodGet, "/health", nil, nil)
}

func TestAuthentication(t *testing.T) {
	secret := jwtSecret(t)
	tenant := uuid.NewString()
	validUntil := time.Now().Add(time.Hour).Unix()

	testCases := map[string]string{
		"no token":     "",
		"garbage":      "not-a-token",
		"tampered":     signToken(t, secret, jwt.MapClaims{"tenant_id": tenant, "exp": validUntil}) + "x",
		"wrong secret": signToken(t, "another-secret-another-secret-xxx", jwt.MapClaims{"tenant_id": tenant, "exp": validUntil}),
		"expired":      signToken(t, secret, jwt.MapClaims{"tenant_id": tenant, "exp": time.Now().Add(-time.Hour).Unix()}),
		"without exp":  signToken(t, secret, jwt.MapClaims{"tenant_id": tenant}),
		"no tenant":    signToken(t, secret, jwt.MapClaims{"exp": validUntil}),
	}

	for name, token := range testCases {
		t.Run(name, func(t *testing.T) {
			result := client{t: t, token: token}.send(http.StatusUnauthorized, http.MethodGet, "/tasks", nil, nil)
			if result.headers.Get("WWW-Authenticate") != "Bearer" {
				t.Errorf("missing WWW-Authenticate: %v", result.headers)
			}
		})
	}
}

func TestEveryProtectedRouteRequiresAToken(t *testing.T) {
	taskID := uuid.NewString()
	routes := [][2]string{
		{http.MethodPost, "/tasks"},
		{http.MethodGet, "/tasks"},
		{http.MethodGet, taskPath(taskID)},
		{http.MethodPatch, taskPath(taskID)},
		{http.MethodPost, taskPath(taskID) + "/pause"},
		{http.MethodPost, taskPath(taskID) + "/resume"},
		{http.MethodDelete, taskPath(taskID)},
	}

	for _, route := range routes {
		t.Run(route[0]+" "+route[1], func(t *testing.T) {
			client{t: t}.send(http.StatusUnauthorized, route[0], route[1], nil, nil)
		})
	}
}

func TestCreateCronTask(t *testing.T) {
	api := newClient(t)
	before := time.Now()

	result := api.send(http.StatusCreated, http.MethodPost, "/tasks", cronPayload(), nil)
	task := decode[taskResponse](t, result)

	if result.headers.Get("Location") != taskPath(task.ID) || result.headers.Get("ETag") != `"1"` {
		t.Errorf("headers: %v", result.headers)
	}
	if task.Status != "active" || task.Version != 1 || task.Priority != "high" || task.Name != "nightly report" {
		t.Errorf("task: %+v", task)
	}
	if task.Request.Body != `{"report":"daily"}` || task.Request.Headers["X-Trace"] != "1" || task.Request.SecretRef != "secrets/report" {
		t.Errorf("request: %+v", task.Request)
	}
	if task.TimeoutSeconds != 30 || task.Retry.MaxAttempts != 3 || task.Retry.BackoffBaseSeconds != 10 {
		t.Errorf("limits: %+v", task)
	}
	if strings.Contains(string(result.body), "tenant_id") {
		t.Errorf("the tenant must not be exposed: %s", result.body)
	}

	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	nextRun := task.NextRunAt.In(madrid)
	if nextRun.Hour() != 9 || nextRun.Minute() != 0 || !nextRun.After(before) {
		t.Errorf("next run in Madrid: %v", nextRun)
	}
}

func TestCreateAtTask(t *testing.T) {
	runAt := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)

	task := newClient(t).createTask(atPayload(runAt))

	if task.Schedule.Type != "at" || task.Schedule.Timezone != "UTC" || !task.NextRunAt.Equal(runAt) {
		t.Errorf("task: %+v", task.Schedule)
	}
}

func TestCreateRejectsInvalidRequests(t *testing.T) {
	testCases := map[string]func(payload map[string]any){
		"empty name":       func(payload map[string]any) { payload["name"] = " " },
		"missing priority": func(payload map[string]any) { delete(payload, "priority") },
		"unknown priority": func(payload map[string]any) { payload["priority"] = "urgent" },
		"unknown field":    func(payload map[string]any) { payload["tenant_id"] = uuid.NewString() },
		"timeout overflow": func(payload map[string]any) { payload["timeout_seconds"] = int64(36028797018963969) },
		"negative timeout": func(payload map[string]any) { payload["timeout_seconds"] = -1 },
		"timeout too long": func(payload map[string]any) { payload["timeout_seconds"] = 7200 },
		"invalid cron": func(payload map[string]any) {
			payload["schedule"] = map[string]any{"type": "cron", "cron_expr": "99 * * * *"}
		},
		"cron never fires": func(payload map[string]any) {
			payload["schedule"] = map[string]any{"type": "cron", "cron_expr": "0 0 31 2 *"}
		},
		"unknown timezone": func(payload map[string]any) {
			payload["schedule"] = map[string]any{"type": "cron", "cron_expr": "* * * * *", "timezone": "Mars/Olympus"}
		},
		"at in the past": func(payload map[string]any) {
			payload["schedule"] = map[string]any{"type": "at", "run_at": time.Now().Add(-time.Hour).Format(time.RFC3339)}
		},
		"http url": func(payload map[string]any) {
			payload["request"] = map[string]any{"method": "GET", "url": "http://api.example.com"}
		},
		"aws metadata": func(payload map[string]any) {
			payload["request"] = map[string]any{"method": "GET", "url": "https://169.254.169.254/latest/meta-data"}
		},
		"private network": func(payload map[string]any) {
			payload["request"] = map[string]any{"method": "GET", "url": "https://10.0.0.5/internal"}
		},
		"sensitive header": func(payload map[string]any) {
			payload["request"] = map[string]any{"method": "GET", "url": "https://api.example.com", "headers": map[string]string{"Authorization": "Bearer x"}}
		},
		"zero attempts": func(payload map[string]any) {
			payload["retry"] = map[string]any{"max_attempts": 0, "backoff_base_seconds": 1}
		},
	}

	for name, mutate := range testCases {
		t.Run(name, func(t *testing.T) {
			payload := cronPayload()
			mutate(payload)
			newClient(t).send(http.StatusBadRequest, http.MethodPost, "/tasks", payload, nil)
		})
	}
}

func TestCreateRejectsMalformedBodies(t *testing.T) {
	api := newClient(t)

	api.send(http.StatusBadRequest, http.MethodPost, "/tasks", "not json", nil)
	api.send(http.StatusBadRequest, http.MethodPost, "/tasks", "", nil)
	api.send(http.StatusBadRequest, http.MethodPost, "/tasks", `{"name":"a"} {"name":"b"}`, nil)
	api.send(http.StatusRequestEntityTooLarge, http.MethodPost, "/tasks", strings.Repeat(" ", 1<<20+1), nil)
}

func TestIdempotency(t *testing.T) {
	api := newClient(t)
	key := map[string]string{"Idempotency-Key": "request-1"}

	first := decode[taskResponse](t, api.send(http.StatusCreated, http.MethodPost, "/tasks", cronPayload(), key))

	existing := decode[taskResponse](t, api.send(http.StatusOK, http.MethodPost, "/tasks", cronPayload(), key))
	if existing.ID != first.ID {
		t.Errorf("the retry created another task: %s vs %s", existing.ID, first.ID)
	}

	other := cronPayload()
	other["name"] = "another body"
	api.send(http.StatusUnprocessableEntity, http.MethodPost, "/tasks", other, key)

	api.send(http.StatusCreated, http.MethodPost, "/tasks", cronPayload(), map[string]string{"Idempotency-Key": "REQUEST-1"})
	newClient(t).send(http.StatusCreated, http.MethodPost, "/tasks", cronPayload(), key)

	api.send(http.StatusNoContent, http.MethodDelete, taskPath(first.ID), nil, nil)
	deleted := decode[taskResponse](t, api.send(http.StatusOK, http.MethodPost, "/tasks", cronPayload(), key))
	if deleted.ID != first.ID || deleted.Status != "deleted" {
		t.Errorf("retry of a deleted task: %+v", deleted)
	}

	api.send(http.StatusBadRequest, http.MethodPost, "/tasks", cronPayload(), map[string]string{"Idempotency-Key": "clé"})
	api.send(http.StatusBadRequest, http.MethodPost, "/tasks", cronPayload(), map[string]string{"Idempotency-Key": strings.Repeat("a", 256)})
}

func TestConcurrentCreatesWithTheSameKeyStoreOneTask(t *testing.T) {
	api := newClient(t)
	key := map[string]string{"Idempotency-Key": "concurrent"}

	results := make([]response, concurrentRequests)
	failures := make([]error, concurrentRequests)
	var group sync.WaitGroup
	for i := range concurrentRequests {
		group.Go(func() {
			results[i], failures[i] = api.attempt(http.MethodPost, "/tasks", cronPayload(), key)
		})
	}
	group.Wait()

	created := 0
	ids := map[string]bool{}
	for i, result := range results {
		if failures[i] != nil {
			t.Fatal(failures[i])
		}
		if result.status == http.StatusCreated {
			created++
		} else if result.status != http.StatusOK {
			t.Fatalf("status %d: %s", result.status, result.body)
		}
		ids[decode[taskResponse](t, result).ID] = true
	}

	if created != 1 || len(ids) != 1 {
		t.Errorf("created %d, distinct ids %d", created, len(ids))
	}
	page := decode[pageResponse](t, api.send(http.StatusOK, http.MethodGet, "/tasks", nil, nil))
	if len(page.Tasks) != 1 {
		t.Errorf("stored %d tasks", len(page.Tasks))
	}
}

func TestGetTask(t *testing.T) {
	api := newClient(t)
	task := api.createTask(cronPayload())

	result := api.send(http.StatusOK, http.MethodGet, taskPath(task.ID), nil, nil)

	if got := decode[taskResponse](t, result); got.ID != task.ID || got.Name != task.Name {
		t.Errorf("got %+v", got)
	}
	if result.headers.Get("ETag") != `"1"` {
		t.Errorf("etag: %q", result.headers.Get("ETag"))
	}
	api.send(http.StatusNotFound, http.MethodGet, taskPath(uuid.NewString()), nil, nil)
	api.send(http.StatusBadRequest, http.MethodGet, "/tasks/not-a-uuid", nil, nil)
}

func TestListPaginatesInCreationOrder(t *testing.T) {
	api := newClient(t)
	var created []string
	for i := range 5 {
		payload := cronPayload()
		payload["name"] = fmt.Sprintf("task-%d", i)
		created = append(created, api.createTask(payload).ID)
		time.Sleep(2 * time.Millisecond)
	}
	api.send(http.StatusNoContent, http.MethodDelete, taskPath(api.createTask(cronPayload()).ID), nil, nil)

	var seen []string
	cursor := ""
	for pages := 1; ; pages++ {
		page := decode[pageResponse](t, api.send(http.StatusOK, http.MethodGet, "/tasks?limit=2&cursor="+cursor, nil, nil))
		for _, task := range page.Tasks {
			seen = append(seen, task.ID)
		}
		if page.NextCursor == "" {
			if pages != 3 {
				t.Errorf("pages: %d", pages)
			}
			break
		}
		cursor = page.NextCursor
	}

	if strings.Join(seen, ",") != strings.Join(created, ",") {
		t.Errorf("seen %v\nwant %v", seen, created)
	}
}

func TestListRejectsInvalidParameters(t *testing.T) {
	api := newClient(t)

	for _, query := range []string{"limit=0", "limit=101", "limit=x", "cursor=not-a-uuid"} {
		api.send(http.StatusBadRequest, http.MethodGet, "/tasks?"+query, nil, nil)
	}
}

func TestListOfANewTenantIsAnEmptyArray(t *testing.T) {
	result := newClient(t).send(http.StatusOK, http.MethodGet, "/tasks", nil, nil)

	if !strings.Contains(string(result.body), `"tasks":[]`) {
		t.Errorf("body: %s", result.body)
	}
}

func TestPatchTask(t *testing.T) {
	api := newClient(t)
	task := api.createTask(cronPayload())
	target := taskPath(task.ID)

	api.send(http.StatusPreconditionRequired, http.MethodPatch, target, map[string]any{"name": "renamed"}, nil)
	api.send(http.StatusBadRequest, http.MethodPatch, target, map[string]any{"name": "renamed"}, map[string]string{"If-Match": "abc"})

	result := api.send(http.StatusOK, http.MethodPatch, target, map[string]any{"name": "renamed"}, ifMatch(1))
	renamed := decode[taskResponse](t, result)
	if renamed.Name != "renamed" || renamed.Version != 2 || result.headers.Get("ETag") != `"2"` {
		t.Errorf("renamed: %+v %v", renamed, result.headers)
	}
	if renamed.Priority != task.Priority || !renamed.NextRunAt.Equal(*task.NextRunAt) {
		t.Errorf("untouched fields changed: %+v", renamed)
	}

	api.send(http.StatusPreconditionFailed, http.MethodPatch, target, map[string]any{"name": "stale"}, ifMatch(1))
	api.send(http.StatusBadRequest, http.MethodPatch, target, map[string]any{"timeout_seconds": 0}, ifMatch(2))
	api.send(http.StatusBadRequest, http.MethodPatch, target, map[string]any{"priority": "urgent"}, ifMatch(2))

	stored := decode[taskResponse](t, api.send(http.StatusOK, http.MethodGet, target, nil, nil))
	if stored.Version != 2 || stored.Name != "renamed" {
		t.Errorf("a rejected PATCH changed the task: %+v", stored)
	}

	runAt := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	rescheduled := decode[taskResponse](t, api.send(http.StatusOK, http.MethodPatch, target, map[string]any{
		"schedule": map[string]any{"type": "at", "run_at": runAt.Format(time.RFC3339)},
	}, ifMatch(2)))
	if rescheduled.Schedule.Type != "at" || !rescheduled.NextRunAt.Equal(runAt) || rescheduled.Version != 3 {
		t.Errorf("rescheduled: %+v", rescheduled)
	}
}

func TestConcurrentPatchesWithTheSameVersionLetOneWin(t *testing.T) {
	api := newClient(t)
	task := api.createTask(cronPayload())

	results := make([]response, concurrentRequests)
	failures := make([]error, concurrentRequests)
	var group sync.WaitGroup
	for i := range concurrentRequests {
		group.Go(func() {
			results[i], failures[i] = api.attempt(http.MethodPatch, taskPath(task.ID), map[string]any{"name": fmt.Sprintf("writer-%d", i)}, ifMatch(1))
		})
	}
	group.Wait()

	winners := 0
	for i, result := range results {
		if failures[i] != nil {
			t.Fatal(failures[i])
		}
		switch result.status {
		case http.StatusOK:
			winners++
		case http.StatusPreconditionFailed:
		default:
			t.Fatalf("status %d: %s", result.status, result.body)
		}
	}

	if winners != 1 {
		t.Errorf("winners: %d", winners)
	}
	stored := decode[taskResponse](t, api.send(http.StatusOK, http.MethodGet, taskPath(task.ID), nil, nil))
	if stored.Version != 2 {
		t.Errorf("version: %d", stored.Version)
	}
}

func TestPauseAndResumeACronTask(t *testing.T) {
	api := newClient(t)
	task := api.createTask(cronPayload())

	paused := decode[taskResponse](t, api.send(http.StatusOK, http.MethodPost, taskPath(task.ID)+"/pause", nil, nil))
	if paused.Status != "paused" || paused.Version != 2 {
		t.Errorf("paused: %s v%d", paused.Status, paused.Version)
	}

	again := decode[taskResponse](t, api.send(http.StatusOK, http.MethodPost, taskPath(task.ID)+"/pause", nil, nil))
	if again.Version != 2 {
		t.Errorf("a repeated pause must be a no-op: v%d", again.Version)
	}

	resumed := decode[taskResponse](t, api.send(http.StatusOK, http.MethodPost, taskPath(task.ID)+"/resume", nil, nil))
	if resumed.Status != "active" || resumed.Version != 3 || !resumed.NextRunAt.After(time.Now()) {
		t.Errorf("resumed: %+v", resumed)
	}

	stored := decode[taskResponse](t, api.send(http.StatusOK, http.MethodGet, taskPath(task.ID), nil, nil))
	if stored.Status != "active" || stored.Version != 3 {
		t.Errorf("stored: %s v%d", stored.Status, stored.Version)
	}
}

func TestResumeKeepsThePendingRunOfAnAtTask(t *testing.T) {
	api := newClient(t)
	runAt := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	task := api.createTask(atPayload(runAt))

	api.send(http.StatusOK, http.MethodPost, taskPath(task.ID)+"/pause", nil, nil)
	resumed := decode[taskResponse](t, api.send(http.StatusOK, http.MethodPost, taskPath(task.ID)+"/resume", nil, nil))

	if resumed.NextRunAt == nil || !resumed.NextRunAt.Equal(runAt) {
		t.Errorf("next run: %v", resumed.NextRunAt)
	}
}

func TestDeleteHidesTheTask(t *testing.T) {
	api := newClient(t)
	task := api.createTask(cronPayload())
	target := taskPath(task.ID)

	api.send(http.StatusNoContent, http.MethodDelete, target, nil, nil)

	api.send(http.StatusNotFound, http.MethodGet, target, nil, nil)
	api.send(http.StatusNotFound, http.MethodPatch, target, map[string]any{"name": "x"}, ifMatch(2))
	api.send(http.StatusNotFound, http.MethodPost, target+"/pause", nil, nil)
	api.send(http.StatusNotFound, http.MethodPost, target+"/resume", nil, nil)
	api.send(http.StatusNotFound, http.MethodDelete, target, nil, nil)

	page := decode[pageResponse](t, api.send(http.StatusOK, http.MethodGet, "/tasks", nil, nil))
	if len(page.Tasks) != 0 {
		t.Errorf("listed %d tasks", len(page.Tasks))
	}
}

func TestTenantsAreIsolated(t *testing.T) {
	owner := newClient(t)
	stranger := newClient(t)
	task := owner.createTask(cronPayload())
	target := taskPath(task.ID)

	stranger.send(http.StatusNotFound, http.MethodGet, target, nil, nil)
	stranger.send(http.StatusNotFound, http.MethodPatch, target, map[string]any{"name": "stolen"}, ifMatch(1))
	stranger.send(http.StatusNotFound, http.MethodPost, target+"/pause", nil, nil)
	stranger.send(http.StatusNotFound, http.MethodPost, target+"/resume", nil, nil)
	stranger.send(http.StatusNotFound, http.MethodDelete, target, nil, nil)

	page := decode[pageResponse](t, stranger.send(http.StatusOK, http.MethodGet, "/tasks", nil, nil))
	if len(page.Tasks) != 0 {
		t.Errorf("listed %d foreign tasks", len(page.Tasks))
	}

	stored := decode[taskResponse](t, owner.send(http.StatusOK, http.MethodGet, target, nil, nil))
	if stored.Version != 1 || stored.Name != task.Name || stored.Status != "active" {
		t.Errorf("the owner's task changed: %+v", stored)
	}
}

func TestUnknownMethodIsRejected(t *testing.T) {
	newClient(t).send(http.StatusMethodNotAllowed, http.MethodPut, "/tasks", nil, nil)
}

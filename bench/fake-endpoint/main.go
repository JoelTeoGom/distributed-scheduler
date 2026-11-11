// Fake tenant endpoint for the benchmarks.
//
//	/t?lat=50&dist=lognormal&err=0.05&p=0   sleeps lat ms (fixed, or lognormal around it), answers 503 with probability err, else 200
//	/t?dist=uniform&min=10000&max=30000     sleeps a uniform random time between min and max ms
//	   &t=42&k=slow                         tenant and endpoint profile, only recorded, for per-tenant results
//	GET  /stats                             counters and start lag percentiles per priority, as JSON;
//	                                        a call the client gave up on is recorded as 499, not 200
//	GET  /events                            one CSV row per request received, for timelines
//	POST /reset                             clears everything between runs
//
// The start lag is measured when the request arrives, before sleeping: now - X-Scheduled-At.
// Only first attempts count for the lag, so retries don't mix in their backoff.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"
)

const priorities = 4

// statusClientGone is nginx's 499: the client closed the connection before the answer.
const statusClientGone = 499

// event is one request as the endpoint saw it.
type event struct {
	arrived     time.Time
	scheduledAt time.Time // zero when the header is missing or invalid
	priority    int
	tenant      string
	profile     string
	attempt     string
	status      int
	key         string
}

type recorder struct {
	mu        sync.Mutex
	requests  int
	byStatus  map[int]int
	succeeded map[string]int // Idempotency-Key -> 200s sent
	lags      [priorities][]float64
	badHeader int
	events    []event
}

func newRecorder() *recorder {
	return &recorder{byStatus: map[int]int{}, succeeded: map[string]int{}}
}

func (r *recorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests, r.badHeader = 0, 0
	r.byStatus, r.succeeded = map[int]int{}, map[string]int{}
	r.lags = [priorities][]float64{}
	r.events = nil
}

func (r *recorder) record(e event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests++
	r.byStatus[e.status]++
	r.events = append(r.events, e)
	if e.status == http.StatusOK {
		r.succeeded[e.key]++
	}
	if e.scheduledAt.IsZero() {
		r.badHeader++
		return
	}
	if e.attempt == "1" && e.priority >= 0 && e.priority < priorities {
		r.lags[e.priority] = append(r.lags[e.priority], e.arrived.Sub(e.scheduledAt).Seconds())
	}
}

// writeEvents writes every request as CSV: arrival in Unix milliseconds, due time in Unix seconds, start lag in seconds.
func (r *recorder) writeEvents(w http.ResponseWriter) {
	r.mu.Lock()
	events := slices.Clone(r.events)
	r.mu.Unlock()
	fmt.Fprintln(w, "arrived_ms,scheduled_s,lag_s,priority,tenant,profile,attempt,status,key")
	for _, e := range events {
		scheduled, lag := "", ""
		if !e.scheduledAt.IsZero() {
			scheduled = strconv.FormatInt(e.scheduledAt.Unix(), 10)
			lag = strconv.FormatFloat(e.arrived.Sub(e.scheduledAt).Seconds(), 'f', 4, 64)
		}
		fmt.Fprintf(w, "%d,%s,%s,%d,%s,%s,%s,%d,%s\n", e.arrived.UnixMilli(), scheduled, lag, e.priority, e.tenant, e.profile, e.attempt, e.status, e.key)
	}
}

type lagStats struct {
	Jobs int     `json:"jobs"`
	P50  float64 `json:"p50_s"`
	P90  float64 `json:"p90_s"`
	P95  float64 `json:"p95_s"`
	P99  float64 `json:"p99_s"`
	Max  float64 `json:"max_s"`
}

type snapshot struct {
	Requests         int              `json:"requests"`
	ByStatus         map[string]int   `json:"by_status"`
	UniqueSucceeded  int              `json:"unique_jobs_succeeded"`
	DuplicateSuccess int              `json:"duplicate_successes"`
	BadScheduledAt   int              `json:"bad_x_scheduled_at"`
	StartLagPriority map[int]lagStats `json:"start_lag_by_priority"`
	StartLagAll      lagStats         `json:"start_lag_all"`
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(math.Ceil(p*float64(len(sorted)))) - 1
	return sorted[max(i, 0)]
}

func summarize(values []float64) lagStats {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	s := lagStats{Jobs: len(sorted)}
	if len(sorted) > 0 {
		s.P50, s.P90, s.P95 = percentile(sorted, .50), percentile(sorted, .90), percentile(sorted, .95)
		s.P99, s.Max = percentile(sorted, .99), sorted[len(sorted)-1]
	}
	return s
}

func (r *recorder) snapshot() snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := snapshot{
		Requests:         r.requests,
		ByStatus:         map[string]int{},
		BadScheduledAt:   r.badHeader,
		StartLagPriority: map[int]lagStats{},
	}
	for code, n := range r.byStatus {
		s.ByStatus[strconv.Itoa(code)] = n
	}
	for _, n := range r.succeeded {
		s.UniqueSucceeded++
		s.DuplicateSuccess += n - 1
	}
	var all []float64
	for p := range priorities {
		s.StartLagPriority[p] = summarize(r.lags[p])
		all = append(all, r.lags[p]...)
	}
	s.StartLagAll = summarize(all)
	return s
}

func queryFloat(q map[string][]string, key string, def float64) float64 {
	if v, ok := q[key]; ok && len(v) > 0 {
		if f, err := strconv.ParseFloat(v[0], 64); err == nil {
			return f
		}
	}
	return def
}

// delay returns how long to sleep: fixed, lognormal with median lat and sigma 0.5 (a long tail, like real endpoints),
// or uniform between min and max.
func delay(q map[string][]string) time.Duration {
	latMs := queryFloat(q, "lat", 0)
	switch firstValue(q, "dist") {
	case "lognormal":
		latMs = latMs * math.Exp(0.5*rand.NormFloat64())
	case "uniform":
		low, high := queryFloat(q, "min", 0), queryFloat(q, "max", 0)
		latMs = low + rand.Float64()*max(high-low, 0)
	}
	if latMs <= 0 {
		return 0
	}
	return time.Duration(latMs * float64(time.Millisecond))
}

func firstValue(q map[string][]string, key string) string {
	if v := q[key]; len(v) > 0 {
		return v[0]
	}
	return ""
}

func main() {
	rec := newRecorder()
	mux := http.NewServeMux()

	mux.HandleFunc("/t", func(w http.ResponseWriter, req *http.Request) {
		e := event{arrived: time.Now(), attempt: req.Header.Get("X-Attempt"), key: req.Header.Get("Idempotency-Key")}
		q := req.URL.Query()
		e.priority = int(queryFloat(q, "p", -1))
		e.tenant, e.profile = firstValue(q, "t"), firstValue(q, "k")
		if scheduledAt, err := time.Parse(time.RFC3339, req.Header.Get("X-Scheduled-At")); err == nil {
			e.scheduledAt = scheduledAt
		}

		time.Sleep(delay(q))

		e.status = http.StatusOK
		if rand.Float64() < queryFloat(q, "err", 0) {
			e.status = http.StatusServiceUnavailable
		}
		if req.Context().Err() != nil {
			// The executor gave up (timeout) before the answer: the work was done, but nobody heard about it.
			e.status = statusClientGone
		}
		rec.record(e)
		w.WriteHeader(e.status)
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("GET /events", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		rec.writeEvents(w)
	})

	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rec.snapshot())
	})

	mux.HandleFunc("POST /reset", func(w http.ResponseWriter, _ *http.Request) {
		rec.reset()
		w.WriteHeader(http.StatusNoContent)
	})

	addr := ":8080"
	if v := os.Getenv("ADDR"); v != "" {
		addr = v
	}
	log.Printf("fake endpoint listening on %s", addr)
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}

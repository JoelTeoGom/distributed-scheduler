// Fake tenant endpoint for the benchmarks.
//
//	/t?lat=50&dist=lognormal&err=0.05&p=0   sleeps lat ms (fixed, or lognormal around it), answers 503 with probability err, else 200
//	GET  /stats                             counters and start lag percentiles per priority, as JSON
//	POST /reset                             clears everything between runs
//
// The start lag is measured when the request arrives, before sleeping: now - X-Scheduled-At.
// Only first attempts count for the lag, so retries don't mix in their backoff.
package main

import (
	"encoding/json"
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

type recorder struct {
	mu        sync.Mutex
	requests  int
	byStatus  map[int]int
	succeeded map[string]int // Idempotency-Key -> 200s sent
	lags      [priorities][]float64
	badHeader int
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
}

func (r *recorder) record(priority int, lag float64, lagOK bool, firstAttempt bool, key string, status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests++
	r.byStatus[status]++
	if status == http.StatusOK {
		r.succeeded[key]++
	}
	if !lagOK {
		r.badHeader++
		return
	}
	if firstAttempt && priority >= 0 && priority < priorities {
		r.lags[priority] = append(r.lags[priority], lag)
	}
}

type lagStats struct {
	Jobs int     `json:"jobs"`
	P50  float64 `json:"p50_s"`
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
		s.P50, s.P95, s.P99, s.Max = percentile(sorted, .50), percentile(sorted, .95), percentile(sorted, .99), sorted[len(sorted)-1]
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

// delay returns how long to sleep: fixed, or lognormal with median lat and sigma 0.5 (a long tail, like real endpoints).
func delay(latMs float64, dist string) time.Duration {
	if latMs <= 0 {
		return 0
	}
	if dist == "lognormal" {
		latMs = latMs * math.Exp(0.5*rand.NormFloat64())
	}
	return time.Duration(latMs * float64(time.Millisecond))
}

func main() {
	rec := newRecorder()
	mux := http.NewServeMux()

	mux.HandleFunc("/t", func(w http.ResponseWriter, req *http.Request) {
		arrived := time.Now()
		q := req.URL.Query()
		priority := int(queryFloat(q, "p", -1))

		scheduledAt, err := time.Parse(time.RFC3339, req.Header.Get("X-Scheduled-At"))
		lag := arrived.Sub(scheduledAt).Seconds()
		firstAttempt := req.Header.Get("X-Attempt") == "1"

		time.Sleep(delay(queryFloat(q, "lat", 0), q.Get("dist")))

		status := http.StatusOK
		if rand.Float64() < queryFloat(q, "err", 0) {
			status = http.StatusServiceUnavailable
		}
		rec.record(priority, lag, err == nil, firstAttempt, req.Header.Get("Idempotency-Key"), status)
		w.WriteHeader(status)
		_, _ = w.Write([]byte("ok\n"))
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

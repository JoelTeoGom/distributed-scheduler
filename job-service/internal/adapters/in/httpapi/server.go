package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/ports"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
)

type Options struct {
	Address        string
	ServiceName    string
	ServiceVersion string
}

type Server struct {
	serviceName    string
	serviceVersion string
	jobService     ports.JobService
	tokens         ports.TokenVerifier
	httpServer     *http.Server
}

// handlerFunc returns its error so the status mapping lives only in writeError.
type handlerFunc func(w http.ResponseWriter, r *http.Request) error

func NewServer(options Options, jobService ports.JobService, tokens ports.TokenVerifier) *Server {
	server := &Server{
		serviceName:    options.ServiceName,
		serviceVersion: options.ServiceVersion,
		jobService:     jobService,
		tokens:         tokens,
	}
	server.httpServer = &http.Server{
		Addr:              options.Address,
		Handler:           server.routes(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
	return server
}

func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

// Shutdown stops accepting connections and waits for in-flight requests until ctx expires.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	protected := func(pattern string, handler handlerFunc) {
		mux.Handle(pattern, s.requireAuth(handle(handler)))
	}

	mux.HandleFunc("GET /health", s.health)

	protected("GET /jobs", s.listJobs)
	protected("GET /jobs/{id}", s.getJob)
	protected("POST /jobs/{id}/retry", s.retryJob)
	protected("GET /tasks/{id}/jobs", s.listTaskJobs)

	return mux
}

func handle(handler handlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := handler(w, r); err != nil {
			writeError(w, r, err)
		}
	})
}

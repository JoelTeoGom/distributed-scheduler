package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/config"
	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/ports"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
)

type Server struct {
	serviceName    string
	serviceVersion string
	taskService    ports.TaskService
	httpServer     *http.Server
}

func NewServer(config config.Config, taskService ports.TaskService) *Server {
	server := &Server{
		serviceName:    config.ServiceName,
		serviceVersion: config.ServiceVersion,
		taskService:    taskService,
	}
	server.httpServer = &http.Server{
		Addr:              config.ServerAddress,
		Handler:           server.routes([]byte(config.JWTSecret)),
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

func (s *Server) routes(jwtSecret []byte) http.Handler {
	mux := http.NewServeMux()
	authenticate := Authenticate(jwtSecret)

	handleAuthenticated := func(pattern string, handler http.HandlerFunc) {
		mux.Handle(pattern, authenticate(handler))
	}

	mux.HandleFunc("GET /health", s.healthHandler)

	handleAuthenticated("POST /tasks", s.createTaskHandler)
	handleAuthenticated("GET /tasks", s.listTasksHandler)
	handleAuthenticated("GET /tasks/{id}", s.getTaskHandler)
	handleAuthenticated("PATCH /tasks/{id}", s.updateTaskHandler)
	handleAuthenticated("POST /tasks/{id}/pause", s.pauseTaskHandler)
	handleAuthenticated("POST /tasks/{id}/resume", s.resumeTaskHandler)
	handleAuthenticated("DELETE /tasks/{id}", s.deleteTaskHandler)

	return mux
}

package httpserver

import (
	"net/http"

	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/adapter/httpserver/middleware"
)

func (s *Server) routes(jwtSecret []byte) http.Handler {
	mux := http.NewServeMux()
	authenticate := middleware.Authenticate(jwtSecret)

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

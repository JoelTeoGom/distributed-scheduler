package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/domain"
)

type tenantIDKey struct{}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, hasBearerPrefix := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !hasBearerPrefix {
			writeError(w, r, domain.ErrUnauthorized)
			return
		}

		tenantID, err := s.tokens.Verify(token)
		if err != nil {
			writeError(w, r, err)
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tenantIDKey{}, tenantID)))
	})
}

// tenantIDFrom panics outside requireAuth, so a route that forgot the middleware fails closed.
func tenantIDFrom(ctx context.Context) uuid.UUID {
	return ctx.Value(tenantIDKey{}).(uuid.UUID)
}

package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type tenantIDKey struct{}

type tenantClaims struct {
	TenantID uuid.UUID `json:"tenant_id"`
	jwt.RegisteredClaims
}

// Authenticate only accepts unexpired HS256 tokens that carry a tenant_id claim.
func Authenticate(secret []byte) func(http.Handler) http.Handler {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	secretLookup := func(*jwt.Token) (any, error) { return secret, nil }

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, hasBearerPrefix := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !hasBearerPrefix {
				writeUnauthorized(w)
				return
			}

			var claims tenantClaims
			if _, err := parser.ParseWithClaims(token, &claims, secretLookup); err != nil || claims.TenantID == uuid.Nil {
				writeUnauthorized(w)
				return
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tenantIDKey{}, claims.TenantID)))
		})
	}
}

// TenantID panics outside Authenticate, so a route that forgot the middleware fails closed.
func TenantID(ctx context.Context) uuid.UUID {
	return ctx.Value(tenantIDKey{}).(uuid.UUID)
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", "Bearer")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"unauthorized"}` + "\n"))
}

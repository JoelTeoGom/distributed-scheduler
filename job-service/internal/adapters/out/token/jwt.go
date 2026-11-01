package token

import (
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/ports"
)

type tenantClaims struct {
	TenantID uuid.UUID `json:"tenant_id"`
	jwt.RegisteredClaims
}

type JWT struct {
	secret []byte
	parser *jwt.Parser
}

var _ ports.TokenVerifier = (*JWT)(nil)

func NewJWT(secret string) *JWT {
	return &JWT{
		secret: []byte(secret),
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			jwt.WithExpirationRequired(),
		),
	}
}

func (j *JWT) Verify(token string) (uuid.UUID, error) {
	var claims tenantClaims
	_, err := j.parser.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return j.secret, nil })
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", domain.ErrUnauthorized, err)
	}
	if claims.TenantID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: missing tenant_id", domain.ErrUnauthorized)
	}
	return claims.TenantID, nil
}

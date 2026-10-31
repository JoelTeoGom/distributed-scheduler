package ports

import "github.com/google/uuid"

type TokenVerifier interface {
	Verify(token string) (tenantID uuid.UUID, err error)
}

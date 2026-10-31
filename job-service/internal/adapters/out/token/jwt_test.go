package token_test

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/adapters/out/token"
	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/domain"
)

const testSecret = "0123456789abcdef0123456789abcdef"

var testTenant = uuid.MustParse("11111111-1111-1111-1111-111111111111")

func sign(t *testing.T, method jwt.SigningMethod, key any, claims jwt.MapClaims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestVerifyReturnsTheTenant(t *testing.T) {
	signed := sign(t, jwt.SigningMethodHS256, []byte(testSecret), jwt.MapClaims{
		"tenant_id": testTenant.String(),
		"exp":       time.Now().Add(time.Hour).Unix(),
	})

	tenantID, err := token.NewJWT(testSecret).Verify(signed)

	if err != nil || tenantID != testTenant {
		t.Errorf("tenant %v err %v", tenantID, err)
	}
}

func TestVerifyRejectsInvalidTokens(t *testing.T) {
	valid := time.Now().Add(time.Hour).Unix()
	secret := []byte(testSecret)
	tenant := testTenant.String()

	testCases := map[string]string{
		"garbage":        "not-a-token",
		"empty":          "",
		"wrong secret":   sign(t, jwt.SigningMethodHS256, []byte("another-secret-another-secret-xx"), jwt.MapClaims{"tenant_id": tenant, "exp": valid}),
		"expired":        sign(t, jwt.SigningMethodHS256, secret, jwt.MapClaims{"tenant_id": tenant, "exp": time.Now().Add(-time.Hour).Unix()}),
		"without exp":    sign(t, jwt.SigningMethodHS256, secret, jwt.MapClaims{"tenant_id": tenant}),
		"without tenant": sign(t, jwt.SigningMethodHS256, secret, jwt.MapClaims{"exp": valid}),
		"invalid tenant": sign(t, jwt.SigningMethodHS256, secret, jwt.MapClaims{"tenant_id": "nope", "exp": valid}),
		"nil tenant":     sign(t, jwt.SigningMethodHS256, secret, jwt.MapClaims{"tenant_id": uuid.Nil.String(), "exp": valid}),
		"alg none":       sign(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, jwt.MapClaims{"tenant_id": tenant, "exp": valid}),
		"another alg":    sign(t, jwt.SigningMethodHS512, secret, jwt.MapClaims{"tenant_id": tenant, "exp": valid}),
	}

	for name, signed := range testCases {
		t.Run(name, func(t *testing.T) {
			if _, err := token.NewJWT(testSecret).Verify(signed); !errors.Is(err, domain.ErrUnauthorized) {
				t.Errorf("want ErrUnauthorized, got %v", err)
			}
		})
	}
}

package ports

import "crypto/sha256"

type IdempotencyKey struct {
	Key         string
	RequestHash [sha256.Size]byte
}

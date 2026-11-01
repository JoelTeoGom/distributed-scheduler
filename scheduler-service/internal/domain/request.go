package domain

// Request is the HTTP call every job of the task makes.
type Request struct {
	Method    string
	URL       string
	Headers   map[string]string // non-sensitive only
	Body      []byte
	SecretRef string // reference to a secret, never the secret itself
}

package domain

import "strings"

const maxSecretRefLength = 255

var allowedMethods = map[string]bool{
	"GET":    true,
	"POST":   true,
	"PUT":    true,
	"PATCH":  true,
	"DELETE": true,
}

var sensitiveHeaders = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"cookie":              true,
	"x-api-key":           true,
}

// Request is the HTTP call every job of the task makes.
type Request struct {
	Method    string
	URL       string
	Headers   map[string]string // non-sensitive only
	Body      []byte
	SecretRef string // reference to a secret, never the secret itself
}

func (r Request) validate() error {
	if !allowedMethods[r.Method] {
		return invalid("method must be GET, POST, PUT, PATCH or DELETE")
	}
	if err := validateEndpoint(r.URL); err != nil {
		return err
	}
	for name := range r.Headers {
		if sensitiveHeaders[strings.ToLower(name)] {
			return invalid("sensitive headers are not allowed, use secret_ref")
		}
	}
	if len(r.SecretRef) > maxSecretRefLength {
		return invalid("secret_ref must be at most 255 characters")
	}
	return nil
}

package domain

import (
	"net/netip"
	"net/url"
	"strings"
)

const (
	maxEndpointLength  = 2048
	maxSecretRefLength = 255
)

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

// DNS can change after this check, so executors must validate the resolved address too.
func validateEndpoint(rawURL string) error {
	if len(rawURL) > maxEndpointLength {
		return invalid("url must be at most 2048 characters")
	}

	endpoint, err := url.Parse(rawURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" {
		return invalid("url must be a valid https URL")
	}
	if endpoint.User != nil {
		return invalid("url cannot contain credentials, use secret_ref")
	}
	if isInternalHost(endpoint.Hostname()) {
		return invalid("url cannot point to an internal address")
	}
	return nil
}

func isInternalHost(hostname string) bool {
	host := strings.ToLower(strings.TrimSuffix(hostname, "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".internal") {
		return true
	}

	address, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	address = address.Unmap()
	return !address.IsGlobalUnicast() || address.IsPrivate()
}

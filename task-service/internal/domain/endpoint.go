package domain

import (
	"net/netip"
	"net/url"
	"strings"
)

const maxEndpointLength = 2048

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

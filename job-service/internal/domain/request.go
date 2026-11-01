package domain

// Request is the HTTP call the job makes.
type Request struct {
	Method    string
	URL       string
	Headers   map[string]string
	Body      []byte
	SecretRef string
}

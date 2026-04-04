package mikrotik

import "net/http"

//go:generate mockgen -source=deps.go -destination=mock_test.go -package=mikrotik_test

// HTTPClient is the interface used by Client to send HTTP requests.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

package utils

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// RetryTransport wraps an http.RoundTripper and retries requests that
// receive transient server errors (HTTP 500, 502, 503, 504).
// It uses exponential backoff between retries.
type RetryTransport struct {
	Base       http.RoundTripper
	MaxRetries int
	BaseDelay  time.Duration
}

func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error

	for attempt := 0; attempt <= t.MaxRetries; attempt++ {
		// Clone the request body for retries (body is consumed on each attempt)
		if req.Body != nil && req.GetBody != nil {
			bodyClone, _ := req.GetBody()
			req.Body = bodyClone
		}

		resp, err = t.Base.RoundTrip(req)
		if err != nil {
			return resp, err
		}

		if resp.StatusCode < 500 || attempt == t.MaxRetries {
			return resp, nil
		}

		// Transient 5xx -- drain body and retry with exponential backoff
		if resp.Body != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}

		delay := t.BaseDelay * (1 << attempt) // 2s, 4s, 8s
		fmt.Printf("[http-retry] %s %s: attempt %d/%d got HTTP %d, retrying in %s\n",
			req.Method, req.URL.Path, attempt+1, t.MaxRetries, resp.StatusCode, delay)
		time.Sleep(delay)
	}

	return resp, err
}

// NewRetryTransport creates a RetryTransport that wraps the given base transport
// with 5 retries and 2s base delay (exponential: 2s, 4s, 8s, 16s, 32s).
func NewRetryTransport(base http.RoundTripper) *RetryTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &RetryTransport{
		Base:       base,
		MaxRetries: 5,
		BaseDelay:  2 * time.Second,
	}
}

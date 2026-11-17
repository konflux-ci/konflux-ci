package go_tests

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func Test_proxyCookieGETRetry_recovers_from_502(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	status, _, body, err := proxyCookieGETRetry(context.Background(), client, srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	if string(body) != "recovered" {
		t.Fatalf("unexpected body: %q", body)
	}
	if n := calls.Load(); n < 4 {
		t.Fatalf("expected at least 4 requests, got %d", n)
	}
}

func Test_proxyCookieGETRetry_connection_refused_recovery(t *testing.T) {
	// Pick a listener, grab its address, then close it so the first
	// requests get "connection refused".
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	srvURL := "http://" + addr

	// Re-open a server on the same address after a short delay.
	go func() {
		time.Sleep(3 * time.Second)
		replacement, listenErr := net.Listen("tcp", addr)
		if listenErr != nil {
			return
		}
		s := &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("back"))
			}),
		}
		t.Cleanup(func() { s.Close() })
		_ = s.Serve(replacement)
	}()

	client := &http.Client{Timeout: 2 * time.Second}
	status, _, body, err := proxyCookieGETRetry(context.Background(), client, srvURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	if string(body) != "back" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func Test_proxyCookieGETRetry_gives_up_after_timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	// Use a short context so the test does not wait the full 20 s.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := &http.Client{Timeout: 2 * time.Second}
	status, _, _, err := proxyCookieGETRetry(ctx, client, srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d", status)
	}
}

func Test_proxyCookieGETRetry_non_retriable_error_returned_immediately(t *testing.T) {
	client := &http.Client{Timeout: 2 * time.Second}
	// An invalid URL scheme produces a non-retriable error.
	start := time.Now()
	_, _, _, err := proxyCookieGETRetry(context.Background(), client, "://bad")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
	// Should return immediately, not after retry delay.
	if elapsed > 3*time.Second {
		t.Fatalf("non-retriable error took %s, expected immediate return", elapsed)
	}
}

func Test_proxyCookieGETRetry_does_not_retry_non_502_status(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	status, _, _, err := proxyCookieGETRetry(context.Background(), client, srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", status)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("expected exactly 1 request (no retry), got %d", n)
	}
}

func Test_isTransientLoginError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"connection refused", fmt.Errorf("dial tcp 10.0.0.1:9443: connect: connection refused"), true},
		{"connection reset", fmt.Errorf("read tcp 10.0.0.1:9443: read: connection reset by peer"), true},
		{"timeout", fmt.Errorf("context deadline exceeded"), false},
		{"other", fmt.Errorf("something else"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientLoginError(tc.err); got != tc.want {
				t.Errorf("isTransientLoginError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

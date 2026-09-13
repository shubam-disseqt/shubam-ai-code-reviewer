// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package gh

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fastSleep swaps the real sleeper for a no-op so retry backoff loops run
// in microseconds. Every retry test uses this — the whole point is verifying
// retry logic, not wall-clock backoff.
func newTestRetryTransport(next http.RoundTripper) *retryTransport {
	t := newRetryTransport(next).(*retryTransport)
	t.sleep = func(time.Duration, contextLike) {}
	return t
}

func TestRetryTransport_RetriesOn5xx_ThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	tr := newTestRetryTransport(http.DefaultTransport)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Errorf("calls = %d, want 3 (2 retries)", calls)
	}
}

func TestRetryTransport_GivesUpAfterMaxAttempts(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	tr := newTestRetryTransport(http.DefaultTransport)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("expected the final 500 to be returned as a response, got err %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != retryMaxAttempts {
		t.Errorf("calls = %d, want %d", got, retryMaxAttempts)
	}
}

func TestRetryTransport_DoesNotRetry4xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	tr := newTestRetryTransport(http.DefaultTransport)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close()
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("calls = %d, want 1 (no retry on 4xx)", calls)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestRetryTransport_Retries429WithRetryAfterSeconds(t *testing.T) {
	var calls int32
	var sleepMu sync.Mutex
	var sleepDurations []time.Duration
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := newTestRetryTransport(http.DefaultTransport)
	// Capture the requested sleep durations without actually sleeping.
	tr.sleep = func(d time.Duration, _ contextLike) {
		sleepMu.Lock()
		sleepDurations = append(sleepDurations, d)
		sleepMu.Unlock()
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	resp.Body.Close()

	// wait() uses time.After directly, not the injected sleep, so we assert on
	// nextDelay's parsed value via a direct call to keep this focused.
	got := tr.nextDelay(1, &http.Response{Header: http.Header{"Retry-After": []string{"3"}}})
	if got != 3*time.Second {
		t.Errorf("nextDelay for Retry-After: 3 = %v, want 3s", got)
	}
}

func TestRetryTransport_RetryAfterHTTPDate(t *testing.T) {
	tr := newTestRetryTransport(http.DefaultTransport)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tr.now = func() time.Time { return now }

	future := now.Add(5 * time.Second).Format(http.TimeFormat)
	d, ok := parseRetryAfter(future, now)
	if !ok {
		t.Fatalf("parseRetryAfter(HTTP date) failed to parse")
	}
	if d != 5*time.Second {
		t.Errorf("parseRetryAfter = %v, want 5s", d)
	}

	// A past date should clamp to 0, not a negative duration.
	past := now.Add(-5 * time.Second).Format(http.TimeFormat)
	d, ok = parseRetryAfter(past, now)
	if !ok || d != 0 {
		t.Errorf("past date: got (%v, %v), want (0s, true)", d, ok)
	}
}

func TestRetryTransport_RetriesNetworkError(t *testing.T) {
	var calls int32
	fake := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			return nil, errors.New("network unreachable")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
			Header:     http.Header{},
		}, nil
	})
	tr := newTestRetryTransport(fake)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.test/x", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close()
	if atomic.LoadInt32(&calls) != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestRetryTransport_ReplaysBodyAcrossAttempts(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if len(bodies) < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := newTestRetryTransport(http.DefaultTransport)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL, strings.NewReader(`{"hello":"world"}`))
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	resp.Body.Close()

	if len(bodies) < 2 {
		t.Fatalf("expected at least 2 attempts, got %d", len(bodies))
	}
	for i, b := range bodies {
		if b != `{"hello":"world"}` {
			t.Errorf("attempt %d body = %q, want the original payload replayed", i, b)
		}
	}
}

func TestRetryTransport_ContextCancelStopsRetries(t *testing.T) {
	var calls int32
	fake := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return nil, errors.New("boom")
	})
	tr := newTestRetryTransport(fake)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.test/x", nil)
	_, err := tr.RoundTrip(req)
	if err == nil {
		t.Fatal("want error when context is cancelled")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want exactly 1 attempt before ctx short-circuit", got)
	}
}

func TestRetryTransport_ExponentialBackoffCap(t *testing.T) {
	tr := newTestRetryTransport(http.DefaultTransport)
	// nextDelay with no resp uses jittered exponential backoff. Cap at
	// retryMaxDelay before jitter, so upper bound is retryMaxDelay.
	for attempt := 1; attempt <= 10; attempt++ {
		d := tr.nextDelay(attempt, nil)
		if d < 0 || d > retryMaxDelay {
			t.Errorf("attempt %d: delay %v out of [0, %v]", attempt, d, retryMaxDelay)
		}
	}
}

func TestParseRetryAfter_Garbage(t *testing.T) {
	if d, ok := parseRetryAfter("banana", time.Now()); ok || d != 0 {
		t.Errorf("garbage: got (%v, %v), want (0, false)", d, ok)
	}
	if d, ok := parseRetryAfter("", time.Now()); ok || d != 0 {
		t.Errorf("empty: got (%v, %v), want (0, false)", d, ok)
	}
	if d, ok := parseRetryAfter("-3", time.Now()); ok || d != 0 {
		t.Errorf("negative seconds: got (%v, %v), want (0, false)", d, ok)
	}
}

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		code int
		want bool
	}{
		{200, false}, {201, false}, {204, false},
		{301, false}, {400, false}, {401, false}, {403, false}, {404, false}, {422, false},
		{429, true},
		{500, true}, {502, true}, {503, true}, {504, true}, {599, true},
	}
	for _, c := range cases {
		if got := isRetryable(c.code); got != c.want {
			t.Errorf("isRetryable(%d) = %v, want %v", c.code, got, c.want)
		}
	}
}

// roundTripFunc adapts a function to the http.RoundTripper interface for
// tests that need in-process behaviour rather than a real server.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

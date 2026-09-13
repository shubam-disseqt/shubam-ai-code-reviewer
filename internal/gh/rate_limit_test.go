// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package gh

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestRateLimitedTransport_BlocksBeyondBurst(t *testing.T) {
	// 100rps + burst=3 means the 4th back-to-back request must wait ~10ms.
	// We measure whether the wallclock elapsed exceeds a modest floor so the
	// test isn't sensitive to scheduler noise.
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := &rateLimitedTransport{
		limiter: rate.NewLimiter(rate.Limit(100), 3),
		next:    http.DefaultTransport,
	}
	client := &http.Client{Transport: tr}

	// Consume the burst.
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("burst req %d: %v", i, err)
		}
		resp.Body.Close()
	}

	// The next request must wait for a token.
	start := time.Now()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("throttled req: %v", err)
	}
	resp.Body.Close()
	elapsed := time.Since(start)

	// At 100rps we expect ~10ms of wait. Assert a lower floor of 5ms so this
	// test isn't flaky on a busy machine — the point is "it waited", not the
	// exact latency.
	if elapsed < 5*time.Millisecond {
		t.Errorf("throttled request returned in %v; expected >5ms wait", elapsed)
	}
	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Errorf("server saw %d calls, want 4", got)
	}
}

func TestRateLimitedTransport_HonoursContextCancel(t *testing.T) {
	// Tight limiter so Wait blocks; cancel the context and verify the error
	// surfaces without hitting the network.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("network should not have been reached")
	}))
	defer srv.Close()

	tr := &rateLimitedTransport{
		limiter: rate.NewLimiter(rate.Limit(0.001), 0), // effectively never
		next:    http.DefaultTransport,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	_, err := tr.RoundTrip(req)
	if err == nil {
		t.Fatal("want error from cancelled context")
	}
}

func TestRateLimitedTransport_ConcurrentSafe(t *testing.T) {
	// Race-detector regression: multiple goroutines sharing one limiter must
	// not race. rate.Limiter itself is safe, but our wrapper still needs to
	// pass -race with concurrent RoundTrips.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := &rateLimitedTransport{
		limiter: rate.NewLimiter(rate.Limit(1000), 50),
		next:    http.DefaultTransport,
	}
	client := &http.Client{Transport: tr}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
			resp, err := client.Do(req)
			if err != nil {
				t.Errorf("concurrent req: %v", err)
				return
			}
			resp.Body.Close()
		}()
	}
	wg.Wait()
}

func TestEnvFloat_And_EnvInt(t *testing.T) {
	t.Setenv("ZR_TEST_FLOAT", "3.5")
	if got := envFloat("ZR_TEST_FLOAT", 1.0); got != 3.5 {
		t.Errorf("envFloat = %v, want 3.5", got)
	}
	if got := envFloat("ZR_TEST_MISSING", 1.5); got != 1.5 {
		t.Errorf("envFloat default = %v, want 1.5", got)
	}
	t.Setenv("ZR_TEST_FLOAT_BAD", "not-a-number")
	if got := envFloat("ZR_TEST_FLOAT_BAD", 2.0); got != 2.0 {
		t.Errorf("envFloat garbage should fall back to default, got %v", got)
	}

	t.Setenv("ZR_TEST_INT", "42")
	if got := envInt("ZR_TEST_INT", 1); got != 42 {
		t.Errorf("envInt = %d, want 42", got)
	}
	if got := envInt("ZR_TEST_INT_MISSING", 7); got != 7 {
		t.Errorf("envInt default = %d, want 7", got)
	}
}

func TestNewRateLimitedTransport_UsesEnvOverrides(t *testing.T) {
	t.Setenv("ZREVIEW_GH_RATE_LIMIT_RPS", "50")
	t.Setenv("ZREVIEW_GH_RATE_LIMIT_BURST", "7")
	rt := newRateLimitedTransport(http.DefaultTransport)
	rlt, ok := rt.(*rateLimitedTransport)
	if !ok {
		t.Fatalf("wrong wrapper type: %T", rt)
	}
	if rlt.limiter.Burst() != 7 {
		t.Errorf("burst = %d, want 7", rlt.limiter.Burst())
	}
	if float64(rlt.limiter.Limit()) != 50 {
		t.Errorf("rps = %v, want 50", rlt.limiter.Limit())
	}
}

func TestNewRateLimitedTransport_InvalidEnvFallsBackToDefaults(t *testing.T) {
	t.Setenv("ZREVIEW_GH_RATE_LIMIT_RPS", "0")
	t.Setenv("ZREVIEW_GH_RATE_LIMIT_BURST", "-1")
	rt := newRateLimitedTransport(http.DefaultTransport)
	rlt := rt.(*rateLimitedTransport)
	if rlt.limiter.Burst() != defaultRateBurst {
		t.Errorf("burst = %d, want default %d", rlt.limiter.Burst(), defaultRateBurst)
	}
	if float64(rlt.limiter.Limit()) != defaultRateRPS {
		t.Errorf("rps = %v, want default %v", rlt.limiter.Limit(), defaultRateRPS)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package gh

import (
	"bytes"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Retry policy constants. Tuned for GitHub REST: base=500ms doubles to
// 500/1000/2000/4000/8000ms, capped so a persistent 5xx aborts after ~15s
// of waiting rather than pinning the review process indefinitely.
const (
	retryMaxAttempts = 5
	retryBaseDelay   = 500 * time.Millisecond
	retryMaxDelay    = 8 * time.Second
	retryMultiplier  = 2.0
)

// retryTransport wraps next with retry-on-transient-failure logic. Retries
// fire for network errors and for 5xx / 429 responses. Any 4xx other than 429
// is returned as-is: go-github surfaces those as ErrorResponse so the caller
// can classify.
type retryTransport struct {
	next     http.RoundTripper
	sleep    func(time.Duration, contextLike) // injectable for tests
	now      func() time.Time                 // injectable for tests
	rand     *rand.Rand
	randMu   sync.Mutex // rand.Rand is not safe for concurrent use
	attempts int        // 0 => use retryMaxAttempts
}

func newRetryTransport(next http.RoundTripper) http.RoundTripper {
	return &retryTransport{
		next:  next,
		sleep: defaultSleep,
		now:   time.Now,
		// Seed off wall clock — jitter needs randomness, not cryptographic
		// unpredictability, so math/rand is fine.
		//nolint:gosec // non-security use
		rand: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	maxAttempts := t.attempts
	if maxAttempts <= 0 {
		maxAttempts = retryMaxAttempts
	}
	// Buffer the request body once so we can replay it across attempts.
	// go-github sets GetBody for us on JSON bodies, but we can't rely on
	// every code path having it wired — do it defensively.
	body, err := snapshotBody(req)
	if err != nil {
		return nil, err
	}

	var lastResp *http.Response
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Fresh body clone per attempt.
		if body != nil {
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
		resp, err := t.next.RoundTrip(req)
		// Network-level failure: retry unless ctx is done.
		if err != nil {
			lastErr = err
			if req.Context().Err() != nil {
				return nil, err
			}
			if attempt == maxAttempts {
				return nil, err
			}
			t.wait(req, attempt, nil)
			continue
		}
		// Not retryable → return the response as-is.
		if !isRetryable(resp.StatusCode) {
			return resp, nil
		}
		// Retryable status. Drain body so connection can be reused, then
		// back off.
		lastResp = resp
		if attempt == maxAttempts {
			return resp, nil
		}
		drain(resp)
		t.wait(req, attempt, resp)
	}
	// Loop always returns inside, but the compiler wants a trailing path.
	if lastResp != nil {
		return lastResp, nil
	}
	return nil, lastErr
}

// wait sleeps for the next backoff interval. If resp carries a valid
// Retry-After header (429 or 503), that value is used verbatim (bounded by
// retryMaxDelay * 4 so a hostile server can't stall us forever). Otherwise
// exponential backoff with full jitter is applied. Context cancellation
// short-circuits the sleep so a cancelled review doesn't hang here.
func (t *retryTransport) wait(req *http.Request, attempt int, resp *http.Response) {
	d := t.nextDelay(attempt, resp)
	if d <= 0 {
		return
	}
	t.sleep(d, req.Context())
}

// defaultSleep is the production sleeper: honours ctx cancellation via
// time.After. Tests replace it with a no-op to avoid multi-second delays.
func defaultSleep(d time.Duration, ctx contextLike) {
	if d <= 0 {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// contextLike is the subset of context.Context we need — decouples the
// sleep helper from importing context in the signature (keeps the retry
// package's public surface at zero — this is internal only).
type contextLike interface {
	Done() <-chan struct{}
}

func (t *retryTransport) nextDelay(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if d, ok := parseRetryAfter(resp.Header.Get("Retry-After"), t.now()); ok {
			// Cap so a pathological server can't stall the review indefinitely.
			cap := retryMaxDelay * 4
			if d > cap {
				d = cap
			}
			return d
		}
	}
	// Exponential backoff with full jitter.
	base := float64(retryBaseDelay)
	for i := 1; i < attempt; i++ {
		base *= retryMultiplier
	}
	if base > float64(retryMaxDelay) {
		base = float64(retryMaxDelay)
	}
	t.randMu.Lock()
	jittered := t.rand.Float64() * base
	t.randMu.Unlock()
	return time.Duration(jittered)
}

// isRetryable reports whether an HTTP status code warrants another attempt.
// 5xx are always transient; 429 is rate-limit throttling and honours
// Retry-After. Any 4xx other than 429 is a client bug and won't fix itself
// on retry.
func isRetryable(status int) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	return status >= 500 && status <= 599
}

// parseRetryAfter accepts both integer-seconds and HTTP-date forms per RFC
// 7231 § 7.1.3. Returns (0, false) for unparseable values so the caller can
// fall back to exponential backoff.
func parseRetryAfter(hdr string, now time.Time) (time.Duration, bool) {
	if hdr == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(hdr); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}
	if ts, err := http.ParseTime(hdr); err == nil {
		d := ts.Sub(now)
		if d < 0 {
			d = 0
		}
		return d, true
	}
	return 0, false
}

// snapshotBody reads req.Body once so retries can replay it. Nil body
// returns (nil, nil). We deliberately buffer in memory — GitHub REST payloads
// are small (SARIF is the largest at ~MB after gzip). If that ever changes,
// swap in an io.ReadSeeker-based approach.
func snapshotBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	b, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(b))
	return b, nil
}

// drain consumes and closes a response body so the underlying TCP connection
// is returned to the pool for the next attempt.
func drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

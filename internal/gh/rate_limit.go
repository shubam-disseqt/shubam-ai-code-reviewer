// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package gh

import (
	"net/http"
	"os"
	"strconv"

	"golang.org/x/time/rate"
)

// GitHub authenticated REST limits are 5000 req/hr = ~1.39 rps. Unauthenticated
// callers get 60 req/hr, but we treat that as user error — no point pacing to
// a limit that only kicks in for misconfigured tokens.
const (
	defaultRateRPS   = 1.39
	defaultRateBurst = 20
)

// rateLimitedTransport paces every outbound request through a token-bucket
// limiter before delegating to next. limiter.Wait blocks until a token is
// available or ctx is cancelled — the SDK's context deadlines propagate
// through untouched.
type rateLimitedTransport struct {
	limiter *rate.Limiter
	next    http.RoundTripper
}

func (t *rateLimitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.limiter.Wait(req.Context()); err != nil {
		return nil, err
	}
	return t.next.RoundTrip(req)
}

// newRateLimitedTransport wraps next with a token-bucket limiter sized by env
// or falling back to defaults. rps <= 0 or burst <= 0 falls back to defaults —
// callers who want "off" should skip the wrapper entirely.
func newRateLimitedTransport(next http.RoundTripper) http.RoundTripper {
	rps := envFloat("SACR_GH_RATE_LIMIT_RPS", defaultRateRPS)
	burst := envInt("SACR_GH_RATE_LIMIT_BURST", defaultRateBurst)
	if rps <= 0 {
		rps = defaultRateRPS
	}
	if burst <= 0 {
		burst = defaultRateBurst
	}
	return &rateLimitedTransport{
		limiter: rate.NewLimiter(rate.Limit(rps), burst),
		next:    next,
	}
}

func envFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return n
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

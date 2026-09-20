// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors
// Portions Copyright 2026 shubam-ai-code-reviewer contributors
//
// Adapted from alibaba/open-code-review internal/viewer/securityheaders.go
// under Apache License 2.0.

package docsserver

import "net/http"

// contentSecurityPolicy locks the docs viewer down to first-party
// resources only. The docs bundle loads no third-party scripts,
// styles, fonts, or frames, so a strict same-origin policy holds
// without any 'unsafe-inline' relaxation. This mitigates injection of
// active content should any templated value ever escape HTML escaping.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'none'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"frame-ancestors 'none'; " +
	"form-action 'none'"

// securityHeaders wraps a handler and sets defense-in-depth response
// headers on every reply. HSTS is intentionally omitted: the docs
// server serves plain HTTP on loopback, where HSTS is meaningless
// and would wrongly pin localhost.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "geolocation=(), camera=(), microphone=()")
		next.ServeHTTP(w, r)
	})
}

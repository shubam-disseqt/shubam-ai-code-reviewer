// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package docsserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHostOnly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"host only", "localhost", "localhost"},
		{"host and port", "localhost:8080", "localhost"},
		{"ipv4 with port", "127.0.0.1:8080", "127.0.0.1"},
		{"ipv6 bracketed with port", "[::1]:8080", "::1"},
		{"ipv6 bare bracketed", "[::1]", "::1"},
		{"upper case", "LOCALHOST", "localhost"},
		{"unbracketed ipv6 rejected", "fe80::1", ""},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := hostOnly(tt.in)
			if got != tt.want {
				t.Errorf("hostOnly(%q) = %q; want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsLoopbackHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		host string
		want bool
	}{
		{"localhost", true},
		{"127.0.0.1", true},
		{"127.0.0.42", true},
		{"::1", true},
		{"example.com", false},
		{"192.168.1.1", false},
		{"", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.host, func(t *testing.T) {
			t.Parallel()
			if got := isLoopbackHost(tt.host); got != tt.want {
				t.Errorf("isLoopbackHost(%q) = %v; want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestBuildAllowedHosts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		bindHost   string
		envVal     string
		wantHosts  []string
		wantAbsent []string
	}{
		{
			name:      "defaults only",
			bindHost:  "",
			envVal:    "",
			wantHosts: []string{"localhost", "127.0.0.1", "::1"},
		},
		{
			name:      "concrete bind added",
			bindHost:  "192.168.1.10",
			envVal:    "",
			wantHosts: []string{"192.168.1.10", "localhost"},
		},
		{
			name:       "wildcard bind not auto-added",
			bindHost:   "0.0.0.0",
			envVal:     "",
			wantAbsent: []string{"0.0.0.0"},
		},
		{
			name:      "env value merged",
			bindHost:  "127.0.0.1",
			envVal:    "docs.example.com, other.example.com",
			wantHosts: []string{"docs.example.com", "other.example.com"},
		},
		{
			name:      "bracketed ipv6 bind normalized",
			bindHost:  "[fe80::1]",
			envVal:    "",
			wantHosts: []string{"fe80::1"},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildAllowedHosts(tt.bindHost, tt.envVal)
			for _, h := range tt.wantHosts {
				if _, ok := got[h]; !ok {
					t.Errorf("allowed hosts missing %q; got %v", h, got)
				}
			}
			for _, h := range tt.wantAbsent {
				if _, ok := got[h]; ok {
					t.Errorf("allowed hosts should not contain %q; got %v", h, got)
				}
			}
		})
	}
}

func TestHostGuard(t *testing.T) {
	t.Parallel()
	allowed := buildAllowedHosts("127.0.0.1", "docs.example.com")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := hostGuard(allowed, next)

	tests := []struct {
		name       string
		host       string
		wantStatus int
	}{
		{"loopback name", "localhost:8080", http.StatusOK},
		{"loopback ipv4", "127.0.0.1:8080", http.StatusOK},
		{"loopback ipv6", "[::1]:8080", http.StatusOK},
		{"allowlisted", "docs.example.com", http.StatusOK},
		{"forbidden", "attacker.example.com", http.StatusForbidden},
		{"empty host", "", http.StatusForbidden},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "http://ignored/", nil)
			req.Host = tt.host
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("Host=%q got status %d; want %d", tt.host, w.Code, tt.wantStatus)
			}
		})
	}
}

func TestDisplayAddr(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"127.0.0.1:8080", "127.0.0.1:8080"},
		{"0.0.0.0:8080", "localhost:8080"},
		{":8080", "localhost:8080"},
		{"[::]:8080", "localhost:8080"},
		{"not-an-address", "not-an-address"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := displayAddr(tt.in); got != tt.want {
				t.Errorf("displayAddr(%q) = %q; want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := securityHeaders(next)
	req := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	wantHeaders := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	}
	for k, want := range wantHeaders {
		if got := w.Header().Get(k); got != want {
			t.Errorf("header %s = %q; want %q", k, got, want)
		}
	}
	if got := w.Header().Get("Content-Security-Policy"); got == "" {
		t.Error("missing Content-Security-Policy header")
	}
}

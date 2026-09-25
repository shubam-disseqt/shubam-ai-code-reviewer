// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckDBURLMemory(t *testing.T) {
	t.Setenv("SACR_DB_URL", "sqlite:///:memory:")
	got := checkDBURL(context.Background())
	if got.Status != "ok" {
		t.Fatalf("want ok, got %q (%s)", got.Status, got.Detail)
	}
}

func TestCheckDBURLBadDSN(t *testing.T) {
	t.Setenv("SACR_DB_URL", "mysql://x")
	got := checkDBURL(context.Background())
	if got.Status != "fail" {
		t.Fatalf("want fail, got %q", got.Status)
	}
	if got.Suggestion == "" {
		t.Error("expected a suggestion")
	}
}

// Unset SACR_DB_URL is not a skip any more: doctor opens the per-repo
// default under $HOME/.sacr/index/ and pings it.
func TestCheckDBURLDefault(t *testing.T) {
	t.Setenv("SACR_DB_URL", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	got := checkDBURL(context.Background())
	if got.Status != "ok" {
		t.Fatalf("want ok, got %q (%s)", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, ".sacr") {
		t.Errorf("detail should name the default path, got %q", got.Detail)
	}
}

func TestCheckOrgRulesRepoSet(t *testing.T) {
	t.Setenv("SACR_ORG_RULES_REPO", "owner/repo")
	got := checkOrgRulesRepo()
	if got.Status != "ok" {
		t.Fatalf("want ok, got %q", got.Status)
	}
	if !strings.Contains(got.Detail, "owner/repo") {
		t.Errorf("detail: %q", got.Detail)
	}
}

func TestCheckGithubTokenNoToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	got := checkGithubToken(context.Background())
	if got.Status != "skip" {
		t.Fatalf("want skip, got %q", got.Status)
	}
}

// checkGithubToken uses http.DefaultClient with a hardcoded URL, so we can't
// intercept it without exposing a seam. Assert the non-2xx path via a fake
// transport swap on DefaultClient — safe because tests own the process.
func TestCheckGithubTokenBadResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	origTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = &redirTransport{target: srv.URL}
	defer func() { http.DefaultClient.Transport = origTransport }()

	t.Setenv("GITHUB_TOKEN", "fake")
	got := checkGithubToken(context.Background())
	if got.Status != "fail" {
		t.Fatalf("want fail, got %q (%s)", got.Status, got.Detail)
	}
}

// redirTransport rewrites the request URL to point at target's host, so the
// real https://api.github.com call gets served by our httptest server.
type redirTransport struct{ target string }

func (r *redirTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	// Replace only the scheme+host; keep the path/query.
	// httptest server URL is http://127.0.0.1:xxxx
	// Parse manually to avoid another import.
	scheme, hostPort := "http", strings.TrimPrefix(r.target, "http://")
	req.URL.Scheme = scheme
	req.URL.Host = hostPort
	req.Host = hostPort
	return http.DefaultTransport.RoundTrip(req)
}

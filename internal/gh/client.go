// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/core/overlap.py
// under Apache License 2.0.

// Package gh is a minimal GitHub REST client — just what cross-PR overlap
// detection and inline review-comment posting need. It wraps
// google/go-github/v63 rather than re-implementing HTTP, and deliberately
// exposes a narrow surface (three methods) so the overlap package doesn't
// depend on the whole SDK type graph.
package gh

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-github/v63/github"
	"golang.org/x/oauth2"
)

// Client is a thin wrapper around *github.Client. The overlap package accepts
// a *Client rather than an interface — tests hit a real *github.Client backed
// by httptest.
type Client struct {
	sdk *github.Client
	// authed is true when a non-empty token was provided. Purely diagnostic;
	// used to log at DEBUG when an unauthenticated call may fail.
	authed bool
}

// Options configures NewClient. All fields are optional.
type Options struct {
	// Token is a GitHub personal access token or App installation token.
	// Empty is allowed for public-repo read paths; auth-required calls will
	// log at DEBUG and let GitHub return 401/403.
	Token string
	// BaseURL points at a GitHub Enterprise Server. Empty means github.com.
	// The value should be the API root, e.g. "https://ghe.example.com/api/v3/".
	// Trailing slash is added automatically by go-github.
	BaseURL string
	// HTTPClient overrides the underlying transport. Tests use httptest here.
	HTTPClient *http.Client
}

// NewClient builds a *Client. Returns an error only if BaseURL is provided
// but unparseable.
//
// Two HTTP middlewares are installed on the transport chain, in this
// order (outer to inner): rate limit -> retry -> transport. Rate limit paces
// user-facing requests to stay under GitHub's authenticated hourly quota;
// retry hides transient 5xx / 429 with exponential backoff + Retry-After.
// See docs/reliability.html.
func NewClient(opts Options) (*Client, error) {
	base := opts.HTTPClient
	if opts.Token != "" {
		// oauth2.NewClient wraps whatever transport is on the context's
		// http.Client (or DefaultClient), so we thread HTTPClient through it.
		ctx := context.Background()
		if base != nil {
			ctx = context.WithValue(ctx, oauth2.HTTPClient, base)
		}
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: opts.Token})
		base = oauth2.NewClient(ctx, ts)
	}
	base = withReliabilityMiddleware(base)
	sdk := github.NewClient(base)
	if opts.BaseURL != "" {
		u := opts.BaseURL
		if !strings.HasSuffix(u, "/") {
			u += "/"
		}
		var err error
		sdk, err = sdk.WithEnterpriseURLs(u, u)
		if err != nil {
			return nil, fmt.Errorf("gh: parse enterprise URL: %w", err)
		}
	}
	return &Client{sdk: sdk, authed: opts.Token != ""}, nil
}

// OpenPRRef mirrors mira.models.OpenPRRef — a lightweight PR handle without
// the diff body. Times are UTC.
type OpenPRRef struct {
	Number    int
	Title     string
	Body      string
	HeadRef   string
	BaseRef   string
	HeadSHA   string
	HTMLURL   string
	UserLogin string
	IsDraft   bool
	UpdatedAt time.Time
}

// ReviewComment describes a single inline review comment.
//
// GitHub's inline-comment API has two shapes: single-line (Line only) and
// multi-line (StartLine..Line inclusive). StartLine == 0 selects single-line.
// Side is "RIGHT" for post-change lines (default when empty), "LEFT" for
// pre-change lines.
type ReviewComment struct {
	Path      string
	Body      string
	Line      int
	StartLine int
	Side      string
	CommitSHA string
}

// ListOpenPRs returns up to `limit` open PRs on owner/repo, most-recently
// updated first. Wraps go-github's paginated Pulls.List.
func (c *Client) ListOpenPRs(ctx context.Context, owner, repo string, limit int) ([]OpenPRRef, error) {
	if limit <= 0 {
		return nil, nil
	}
	c.warnIfUnauth()
	// GitHub's per-page cap is 100; if a caller asks for more we paginate.
	perPage := limit
	if perPage > 100 {
		perPage = 100
	}
	opt := &github.PullRequestListOptions{
		State:       "open",
		Sort:        "updated",
		Direction:   "desc",
		ListOptions: github.ListOptions{PerPage: perPage},
	}
	var out []OpenPRRef
	for {
		pulls, resp, err := c.sdk.PullRequests.List(ctx, owner, repo, opt)
		if err != nil {
			return nil, fmt.Errorf("gh: list open PRs: %w", err)
		}
		for _, pr := range pulls {
			out = append(out, prToRef(pr))
			if len(out) >= limit {
				return out, nil
			}
		}
		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}
		opt.Page = resp.NextPage
	}
}

// GetPRFiles returns up to `limit` filenames touched by PR `number`. A 404
// (stale/deleted PR) returns (nil, nil) so an overlap batch isn't derailed by
// one candidate vanishing mid-run.
func (c *Client) GetPRFiles(ctx context.Context, owner, repo string, number, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	c.warnIfUnauth()
	perPage := limit
	if perPage > 100 {
		perPage = 100
	}
	opt := &github.ListOptions{PerPage: perPage}
	var out []string
	for {
		files, resp, err := c.sdk.PullRequests.ListFiles(ctx, owner, repo, number, opt)
		if err != nil {
			if isNotFound(resp, err) {
				return nil, nil
			}
			return nil, fmt.Errorf("gh: get PR files: %w", err)
		}
		for _, f := range files {
			if f == nil {
				continue
			}
			out = append(out, f.GetFilename())
			if len(out) >= limit {
				return out, nil
			}
		}
		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}
		opt.Page = resp.NextPage
	}
}

// PostReviewComment posts an inline PR review comment. Used by --format github
// to drop a single suggestion at a specific line/range.
func (c *Client) PostReviewComment(ctx context.Context, owner, repo string, number int, comment ReviewComment) error {
	c.warnIfUnauth()
	side := comment.Side
	if side == "" {
		side = "RIGHT"
	}
	in := &github.PullRequestComment{
		Body:     github.String(comment.Body),
		Path:     github.String(comment.Path),
		CommitID: github.String(comment.CommitSHA),
		Line:     github.Int(comment.Line),
		Side:     github.String(side),
	}
	if comment.StartLine > 0 && comment.StartLine != comment.Line {
		in.StartLine = github.Int(comment.StartLine)
		in.StartSide = github.String(side)
	}
	_, _, err := c.sdk.PullRequests.CreateComment(ctx, owner, repo, number, in)
	if err != nil {
		return fmt.Errorf("gh: post review comment: %w", err)
	}
	return nil
}

// prToRef flattens go-github's PullRequest into our lightweight ref.
// Every getter tolerates nil, so we do too.
func prToRef(pr *github.PullRequest) OpenPRRef {
	if pr == nil {
		return OpenPRRef{}
	}
	return OpenPRRef{
		Number:    pr.GetNumber(),
		Title:     pr.GetTitle(),
		Body:      pr.GetBody(),
		HeadRef:   pr.GetHead().GetRef(),
		BaseRef:   pr.GetBase().GetRef(),
		HeadSHA:   pr.GetHead().GetSHA(),
		HTMLURL:   pr.GetHTMLURL(),
		UserLogin: pr.GetUser().GetLogin(),
		IsDraft:   pr.GetDraft(),
		UpdatedAt: pr.GetUpdatedAt().Time,
	}
}

// isNotFound reports whether a response/error pair is a 404. We check the
// HTTP status directly so we don't need to unwrap through go-github's
// ErrorResponse structure — either path lands on 404 the same way.
func isNotFound(resp *github.Response, err error) bool {
	if resp != nil && resp.StatusCode == http.StatusNotFound {
		return true
	}
	var gherr *github.ErrorResponse
	if errors.As(err, &gherr) && gherr.Response != nil && gherr.Response.StatusCode == http.StatusNotFound {
		return true
	}
	return false
}

// warnIfUnauth logs once (via the default logger, DEBUG-level intent) when a
// call runs without a token. Users on public repos can ignore this; anything
// else will surface as 401/403 from GitHub. Kept dead-simple — a real logger
// would be nice, but wiring one just for this line is not lazy.
func (c *Client) warnIfUnauth() {
	if !c.authed {
		log.Printf("gh: unauthenticated request (set GITHUB_TOKEN to raise rate limits)")
	}
}

// withReliabilityMiddleware installs the rate-limit + retry chain on the
// HTTP client that go-github (via oauth2) wraps. Nil in returns a fresh
// client so callers don't have to special-case the "no options" path.
func withReliabilityMiddleware(in *http.Client) *http.Client {
	if in == nil {
		in = &http.Client{}
	}
	base := in.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	// Order (outer -> inner): rate limit -> retry -> transport.
	// A single logical GitHub call passes the limiter once and may spend
	// several attempts inside retry — the limiter caps steady-state
	// throughput; retry handles bursts of transient failure.
	out := *in
	out.Transport = newRateLimitedTransport(newRetryTransport(base))
	return &out
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package gh

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/google/go-github/v63/github"
)

// GetPRBody returns the current markdown body of PR `number`. Empty body is
// returned as ("", nil) — GitHub's default for un-authored PRs.
func (c *Client) GetPRBody(ctx context.Context, owner, repo string, number int) (string, error) {
	c.warnIfUnauth()
	pr, _, err := c.sdk.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return "", fmt.Errorf("gh: get PR body: %w", err)
	}
	if pr == nil {
		return "", nil
	}
	return pr.GetBody(), nil
}

// UpdatePRBody replaces the PR description with `body`. Only the body field
// is sent — go-github's Edit merges into the existing PR, so other fields
// (title, base, state) are untouched.
func (c *Client) UpdatePRBody(ctx context.Context, owner, repo string, number int, body string) error {
	c.warnIfUnauth()
	patch := &github.PullRequest{Body: github.String(body)}
	if _, _, err := c.sdk.PullRequests.Edit(ctx, owner, repo, number, patch); err != nil {
		return fmt.Errorf("gh: update PR body: %w", err)
	}
	return nil
}

// AddLabels adds `labels` to PR (issue) `number`. GitHub deduplicates by
// name server-side, so this call is idempotent per label. Empty input is a
// no-op — matches Phase 15 labeler semantics (zero label output on failure).
func (c *Client) AddLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	c.warnIfUnauth()
	if _, _, err := c.sdk.Issues.AddLabelsToIssue(ctx, owner, repo, number, labels); err != nil {
		return fmt.Errorf("gh: add labels: %w", err)
	}
	return nil
}

// UploadSARIF POSTs a SARIF blob to /repos/:owner/:repo/code-scanning/sarifs.
// The `sarif` field is base64(gzip(json)) per the GitHub REST spec:
// https://docs.github.com/en/rest/code-scanning/code-scanning#upload-an-analysis-as-sarif-data
//
// v1 is fire-and-forget: the POST returns a sarif id which is discarded.
// The 30s context timeout guards against a hung endpoint hijacking the
// review's exit. Polling GET /code-scanning/sarifs/{id} is reserved for a
// future --wait-sarif flag.
func (c *Client) UploadSARIF(ctx context.Context, owner, repo, commitSHA, ref string, sarifBytes []byte) error {
	c.warnIfUnauth()
	encoded, err := gzipBase64(sarifBytes)
	if err != nil {
		return fmt.Errorf("gh: encode SARIF: %w", err)
	}
	analysis := &github.SarifAnalysis{
		CommitSHA: github.String(commitSHA),
		Ref:       github.String(ref),
		Sarif:     github.String(encoded),
		StartedAt: &github.Timestamp{Time: time.Now().UTC()},
		ToolName:  github.String("zreview"),
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, _, err := c.sdk.CodeScanning.UploadSarif(ctx, owner, repo, analysis); err != nil {
		return fmt.Errorf("gh: upload SARIF: %w", err)
	}
	return nil
}

// gzipBase64 gzip-then-base64-encodes `raw` for the SARIF upload API. The
// gzip level uses the default balanced setting — small SARIF payloads don't
// benefit from tuning.
func gzipBase64(raw []byte) (string, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(raw); err != nil {
		return "", err
	}
	if err := gw.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

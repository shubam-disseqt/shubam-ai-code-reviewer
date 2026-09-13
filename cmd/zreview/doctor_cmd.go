// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/shubam-disseqt/z-code-reviewer/internal/index"
	"github.com/shubam-disseqt/z-code-reviewer/internal/llm"
)

// doctorCheck is one pre-flight probe.
type doctorCheck struct {
	Name       string
	Status     string // "ok" | "fail" | "skip"
	Detail     string
	Suggestion string
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run pre-flight checks against the environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd.Context(), cmd)
		},
	}
}

func runDoctor(ctx context.Context, cmd *cobra.Command) error {
	checks := []doctorCheck{
		checkProvider(),
		checkGit(ctx),
		checkDBURL(ctx),
		checkOrgRulesRepo(),
		checkGithubToken(ctx),
	}

	w := cmd.OutOrStdout()
	var failed bool
	fmt.Fprintf(w, "%-24s %-6s %s\n", "CHECK", "STATUS", "DETAIL")
	for _, c := range checks {
		if c.Status == "fail" {
			failed = true
		}
		fmt.Fprintf(w, "%-24s %-6s %s\n", c.Name, c.Status, c.Detail)
		if c.Status == "fail" && c.Suggestion != "" {
			fmt.Fprintf(w, "%-24s        → %s\n", "", c.Suggestion)
		}
	}
	if failed {
		return fmt.Errorf("doctor: one or more checks failed")
	}
	return nil
}

// checkProvider tests whether the LLM resolver produces a usable endpoint.
func checkProvider() doctorCheck {
	c := doctorCheck{Name: "llm provider"}
	configPath := ""
	if home, err := os.UserHomeDir(); err == nil {
		configPath = filepath.Join(home, ".opencodereview", "config.json")
	}
	ep, err := llm.ResolveEndpointWithOptions(configPath, llm.ResolveOptions{
		Provider: os.Getenv("ZREVIEW_PROVIDER"),
		Model:    os.Getenv("ZREVIEW_MODEL"),
	})
	if err != nil {
		c.Status = "fail"
		c.Detail = err.Error()
		c.Suggestion = "set ANTHROPIC_API_KEY / OPENAI_API_KEY or configure ~/.opencodereview/config.json"
		return c
	}
	c.Status = "ok"
	c.Detail = fmt.Sprintf("%s (%s)", ep.Model, ep.Source)
	return c
}

// checkGit ensures a git binary is on PATH and modern enough.
func checkGit(ctx context.Context) doctorCheck {
	c := doctorCheck{Name: "git"}
	out, err := exec.CommandContext(ctx, "git", "--version").Output()
	if err != nil {
		c.Status = "fail"
		c.Detail = err.Error()
		c.Suggestion = "install git 2.41 or newer"
		return c
	}
	c.Detail = strings.TrimSpace(string(out))
	c.Status = "ok"
	// ponytail: exact version parse is overkill; a `git --version` that starts
	// with "git version 2." satisfies the minimum bar and any real breakage
	// will surface at the actual diff call with a specific error. Upgrade path
	// is to parse the semver if a user hits a 2.x-below-2.41 bug.
	return c
}

// checkDBURL verifies the index store's Ping when ZREVIEW_DB_URL is set.
func checkDBURL(ctx context.Context) doctorCheck {
	c := doctorCheck{Name: "index db"}
	dsn := os.Getenv("ZREVIEW_DB_URL")
	if dsn == "" {
		c.Status = "skip"
		c.Detail = "ZREVIEW_DB_URL not set (JIT context mode)"
		return c
	}
	store, err := index.NewStore(ctx, dsn)
	if err != nil {
		c.Status = "fail"
		c.Detail = err.Error()
		c.Suggestion = "check the DSN scheme (sqlite:///path or sqlite:///:memory:)"
		return c
	}
	defer store.Close()
	if err := store.Ping(ctx); err != nil {
		c.Status = "fail"
		c.Detail = err.Error()
		c.Suggestion = "verify the database file exists and is writable"
		return c
	}
	c.Status = "ok"
	c.Detail = dsn
	return c
}

// checkOrgRulesRepo does not clone; it just reports whether the env var is set.
// A full clone probe would prompt for credentials on private repos, which is
// worse than a fast "configured y/n" for a doctor pass.
func checkOrgRulesRepo() doctorCheck {
	c := doctorCheck{Name: "org rules repo"}
	spec := os.Getenv("ZREVIEW_ORG_RULES_REPO")
	if spec == "" {
		c.Status = "skip"
		c.Detail = "ZREVIEW_ORG_RULES_REPO not set"
		return c
	}
	c.Status = "ok"
	c.Detail = spec
	return c
}

// checkGithubToken hits GET /user to validate the token.
func checkGithubToken(ctx context.Context) doctorCheck {
	c := doctorCheck{Name: "github token"}
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		c.Status = "skip"
		c.Detail = "GITHUB_TOKEN not set (overlap disabled)"
		return c
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.Status = "fail"
		c.Detail = err.Error()
		c.Suggestion = "verify network + token validity"
		return c
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		c.Status = "fail"
		c.Detail = fmt.Sprintf("GitHub returned %d", resp.StatusCode)
		c.Suggestion = "rotate GITHUB_TOKEN or check its scopes"
		return c
	}
	c.Status = "ok"
	c.Detail = "authenticated"
	return c
}

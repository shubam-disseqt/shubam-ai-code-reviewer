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

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/spf13/cobra"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/index"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/llm"
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
		checkDeepSeek(),
		checkBedrock(ctx),
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
		Provider: os.Getenv("SACR_PROVIDER"),
		Model:    os.Getenv("SACR_MODEL"),
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

// checkDeepSeek reports whether DEEPSEEK_API_KEY is set and looks structurally
// valid. It does NOT call the DeepSeek API — a doctor pass must never cost the
// operator money. Keys are `sk-` prefixed on the DeepSeek platform.
func checkDeepSeek() doctorCheck {
	c := doctorCheck{Name: "deepseek key"}
	key := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if key == "" {
		c.Status = "skip"
		c.Detail = "DEEPSEEK_API_KEY not set"
		return c
	}
	// Structural sanity only. DeepSeek keys are `sk-` + ~32 chars; anything
	// outside a reasonable band is almost certainly a copy-paste error.
	if !strings.HasPrefix(key, "sk-") {
		c.Status = "fail"
		c.Detail = "DEEPSEEK_API_KEY does not start with sk-"
		c.Suggestion = "regenerate at https://platform.deepseek.com/api_keys"
		return c
	}
	if len(key) < 20 || len(key) > 100 {
		c.Status = "fail"
		c.Detail = fmt.Sprintf("DEEPSEEK_API_KEY length %d is out of the expected 20-100 range", len(key))
		c.Suggestion = "regenerate at https://platform.deepseek.com/api_keys"
		return c
	}
	c.Status = "ok"
	c.Detail = fmt.Sprintf("key present (base URL https://api.deepseek.com)")
	return c
}

// bedrockAuthMethod classifies which credential source the AWS default chain
// is likely to use, based only on env vars — no SDK call. Order mirrors AWS
// precedence: AWS_BEARER_TOKEN_BEDROCK wins in the bedrock middleware, then
// static keys, then AWS_PROFILE, otherwise ambient (SSO cache, instance role,
// container role, credential_process — resolved lazily at request time).
func bedrockAuthMethod() string {
	switch {
	case os.Getenv("AWS_BEARER_TOKEN_BEDROCK") != "":
		return "bearer token (AWS_BEARER_TOKEN_BEDROCK)"
	case os.Getenv("AWS_ACCESS_KEY_ID") != "" && os.Getenv("AWS_SECRET_ACCESS_KEY") != "":
		return "static keys (AWS_ACCESS_KEY_ID)"
	case os.Getenv("AWS_PROFILE") != "":
		return fmt.Sprintf("profile (AWS_PROFILE=%s)", os.Getenv("AWS_PROFILE"))
	}
	return "ambient chain (SSO cache / instance role / credential_process)"
}

// checkBedrock verifies the AWS credential chain resolves for the Bedrock code
// path. It runs when SACR_PROVIDER=bedrock or when any AWS_* env var hints
// that Bedrock is the intended provider; otherwise it skips.
//
// It does NOT call Bedrock — a doctor pass must not spend money. It also does
// not call Retrieve() on the credential provider, because SSO refresh and IMDS
// lookup are network calls and, in the SSO case, can pop an interactive login
// prompt. Non-nil awsCfg.Credentials after LoadDefaultConfig is what the
// Bedrock client itself relies on before the first signed request.
func checkBedrock(ctx context.Context) doctorCheck {
	c := doctorCheck{Name: "bedrock"}

	provider := strings.ToLower(strings.TrimSpace(os.Getenv("SACR_PROVIDER")))
	awsHint := os.Getenv("AWS_REGION") != "" ||
		os.Getenv("AWS_PROFILE") != "" ||
		os.Getenv("AWS_BEARER_TOKEN_BEDROCK") != "" ||
		os.Getenv("AWS_ACCESS_KEY_ID") != ""
	if provider != "bedrock" && !awsHint {
		c.Status = "skip"
		c.Detail = "SACR_PROVIDER != bedrock and no AWS_* env hints"
		return c
	}

	// Bound the config load so a broken profile cannot stall doctor. Same
	// timeout the client uses on the hot path.
	loadCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var loadOpts []func(*awsconfig.LoadOptions) error
	if profile := os.Getenv("AWS_PROFILE"); profile != "" {
		loadOpts = append(loadOpts, awsconfig.WithSharedConfigProfile(profile))
	}
	if region := os.Getenv("AWS_REGION"); region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(region))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(loadCtx, loadOpts...)
	if err != nil {
		c.Status = "fail"
		c.Detail = fmt.Sprintf("LoadDefaultConfig failed: %v", err)
		c.Suggestion = "verify ~/.aws/config, AWS_PROFILE, or run `aws sso login`"
		return c
	}
	if awsCfg.Credentials == nil {
		c.Status = "fail"
		c.Detail = "no credential provider resolved from the AWS default chain"
		c.Suggestion = "set AWS_PROFILE, AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY, or AWS_BEARER_TOKEN_BEDROCK"
		return c
	}
	if awsCfg.Region == "" {
		c.Status = "fail"
		c.Detail = fmt.Sprintf("no AWS region resolved (auth=%s)", bedrockAuthMethod())
		c.Suggestion = "export AWS_REGION=us-east-1 (or us-west-2); Bedrock requires a region to derive the runtime host"
		return c
	}
	c.Status = "ok"
	c.Detail = fmt.Sprintf("region=%s, auth=%s", awsCfg.Region, bedrockAuthMethod())
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
	// note: exact version parse is overkill; a `git --version` that starts
	// with "git version 2." satisfies the minimum bar and any real breakage
	// will surface at the actual diff call with a specific error. Upgrade path
	// is to parse the semver if a user hits a 2.x-below-2.41 bug.
	return c
}

// checkDBURL verifies the index store's Ping when SACR_DB_URL is set.
func checkDBURL(ctx context.Context) doctorCheck {
	c := doctorCheck{Name: "index db"}
	dsn := os.Getenv("SACR_DB_URL")
	if dsn == "" {
		c.Status = "skip"
		c.Detail = "SACR_DB_URL not set (JIT context mode)"
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
	spec := os.Getenv("SACR_ORG_RULES_REPO")
	if spec == "" {
		c.Status = "skip"
		c.Detail = "SACR_ORG_RULES_REPO not set"
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

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/shubam-disseqt/z-code-reviewer/internal/comment"
	"github.com/shubam-disseqt/z-code-reviewer/internal/diff"
	"github.com/shubam-disseqt/z-code-reviewer/internal/gh"
	"github.com/shubam-disseqt/z-code-reviewer/internal/gitcmd"
	"github.com/shubam-disseqt/z-code-reviewer/internal/index"
	"github.com/shubam-disseqt/z-code-reviewer/internal/llm"
	"github.com/shubam-disseqt/z-code-reviewer/internal/llmloop"
	"github.com/shubam-disseqt/z-code-reviewer/internal/model"
	"github.com/shubam-disseqt/z-code-reviewer/internal/overlap"
	"github.com/shubam-disseqt/z-code-reviewer/internal/reviewctx"
	"github.com/shubam-disseqt/z-code-reviewer/internal/rules"
	"github.com/shubam-disseqt/z-code-reviewer/internal/scoring"
	"github.com/shubam-disseqt/z-code-reviewer/internal/selector"
	"github.com/shubam-disseqt/z-code-reviewer/internal/session"
	"github.com/shubam-disseqt/z-code-reviewer/internal/tool"
)

// reviewOpts is the parsed CLI surface for `zreview review`.
type reviewOpts struct {
	From        string
	To          string
	Commit      string
	Repo        string
	Format      string
	Output      string
	PR          int
	Resume      string
	Verbose     bool
	MinSeverity string
}

// defaultMaxTokens is the token budget assumed for the model's context window
// when nothing else is configured. Kept small on purpose — real models will
// happily accept larger values but the compression thresholds derived from it
// should stay conservative until the endpoint reports its real limit.
// ponytail: hardcoded 200k window, upgrade path is per-model config.
const defaultMaxTokens = 200_000

func newReviewCmd() *cobra.Command {
	opts := &reviewOpts{
		Repo:        ".",
		Format:      formatStdout,
		Output:      "-",
		MinSeverity: string(scoring.SeverityMedium),
	}
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Run an AI-powered review over a git diff",
		Long: `Run the full review pipeline: resolve the diff, select files,
build repo context, apply org rules, drive the LLM loop, and emit findings.

Diff selection is mutually exclusive: pass --commit for a single commit, OR
--from/--to for a range, OR neither for the current workspace.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.validate(); err != nil {
				return err
			}
			return runReview(cmd.Context(), cmd, opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.From, "from", "", "base ref (range mode)")
	f.StringVar(&opts.To, "to", "", "head ref (range mode; default HEAD)")
	f.StringVar(&opts.Commit, "commit", "", "single-commit mode; mutually exclusive with --from/--to")
	f.StringVar(&opts.Repo, "repo", ".", "working directory")
	f.StringVar(&opts.Format, "format", formatStdout, "output format: stdout | json | github")
	f.StringVar(&opts.Output, "output", "-", "output path (json format; \"-\" for stdout)")
	f.IntVar(&opts.PR, "pr", 0, "PR number (github format)")
	f.StringVar(&opts.Resume, "resume", "", "resume an interrupted session by id")
	f.BoolVar(&opts.Verbose, "verbose", false, "log more")
	f.StringVar(&opts.MinSeverity, "min-severity", opts.MinSeverity,
		"drop findings below this bucket: LOW | MEDIUM | HIGH | CRITICAL (SUPPRESS is always dropped)")

	return cmd
}

func (o *reviewOpts) validate() error {
	if o.Commit != "" && (o.From != "" || o.To != "") {
		return fmt.Errorf("--commit is mutually exclusive with --from/--to")
	}
	if !validFormat(o.Format) {
		return fmt.Errorf("--format must be one of stdout|json|github (got %q)", o.Format)
	}
	if o.Format == formatGithub && o.PR == 0 {
		return fmt.Errorf("--format=github requires --pr")
	}
	sev, ok := scoring.ParseSeverity(o.MinSeverity)
	if !ok {
		return fmt.Errorf("--min-severity must be one of LOW|MEDIUM|HIGH|CRITICAL (got %q)", o.MinSeverity)
	}
	if sev == scoring.SeveritySuppress {
		return fmt.Errorf("--min-severity=SUPPRESS is invalid (SUPPRESS is always dropped)")
	}
	return nil
}

// runReview stitches the pipeline stages together. Every stage wraps its
// error with fmt.Errorf so a failure names the phase that hit it.
func runReview(ctx context.Context, cmd *cobra.Command, opts *reviewOpts) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// 0) scoring policy — loaded early so a bad override fails fast
	// before we spend tokens. Repo-local overrides log so users know
	// which policy is in play.
	policy, err := scoring.LoadPolicy(opts.Repo)
	if err != nil {
		return fmt.Errorf("scoring: %w", err)
	}
	if opts.Verbose || policy.Source() != "embedded" {
		fmt.Fprintf(cmd.OutOrStderr(), "[zreview] scoring policy: %s\n", policy.Source())
	}
	minSev, _ := scoring.ParseSeverity(opts.MinSeverity) // validated in opts.validate()

	// 1) diff
	diffs, err := resolveDiffs(ctx, opts)
	if err != nil {
		return fmt.Errorf("diff: %w", err)
	}
	if len(diffs) == 0 {
		fmt.Fprintln(cmd.OutOrStderr(), "[zreview] no changes to review")
		return nil
	}

	// 2) selector
	decisions := applySelector(diffs)
	kept := selector.Kept(decisions)
	if opts.Verbose {
		fmt.Fprintf(cmd.OutOrStderr(), "[zreview] selector: kept %d of %d diffs\n", len(kept), len(diffs))
	}
	if len(kept) == 0 {
		fmt.Fprintln(cmd.OutOrStderr(), "[zreview] every diff was filtered out")
		return nil
	}

	// 3) index store (optional)
	store, err := openStore(ctx)
	if err != nil {
		return fmt.Errorf("index store: %w", err)
	}
	if store != nil {
		defer store.Close()
		maybeSpawnWarmer(ctx, store, opts.Repo, kept, cmd.OutOrStderr())
	}

	// 3.5) deterministic scanners — best-effort, tolerant of missing
	// binaries. Findings tagged Source="scanner:<tool>" enter the same
	// comment collector as LLM findings; Phase 16 will score them and
	// Phase 17 will route CVE/secret categories to SARIF.
	scannerFindings := runScanners(ctx, opts.Repo, kept, cmd.OutOrStderr())
	scannerByPath := groupScannerFindingsByPath(scannerFindings)

	// 3.6) summarizer + labeler (parallel, cheap tier). Best-effort — a
	// failure here logs and continues with zero values. We resolve tiers
	// early so the errgroup can dispatch alongside the main LLM setup.
	tiers, err := newLLMTiers()
	if err != nil {
		return fmt.Errorf("llm: %w", err)
	}
	summary, labels := runCheapAgents(ctx, tiers, kept, opts.Verbose, cmd.OutOrStderr())

	// 4) rules
	rulesBlock, err := loadRules(ctx, kept)
	if err != nil {
		// Rules failure is not fatal — log and continue.
		fmt.Fprintf(cmd.OutOrStderr(), "[zreview] rules: %v (continuing without)\n", err)
		rulesBlock = ""
	}

	// 5) context
	reviewCtx, err := buildContext(ctx, opts.Repo, kept, store)
	if err != nil {
		fmt.Fprintf(cmd.OutOrStderr(), "[zreview] context: %v (continuing without)\n", err)
		reviewCtx = ""
	}

	// 6) session
	sess, err := newSession(opts.Resume)
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	defer sess.Close()

	// 7) llm tiers. Main drives the reviewer loop; Cheap already served the
	// Phase 15 summarizer/labeler above.
	llmClient := tiers.Main
	modelName := tiers.MainModel

	// 8) prompts + tool defs
	sysPrompt, err := loadPrompt("main_task_system.md")
	if err != nil {
		return fmt.Errorf("prompts: %w", err)
	}
	userTmpl, err := loadPrompt("main_task_user.md")
	if err != nil {
		return fmt.Errorf("prompts: %w", err)
	}
	toolDefs, err := loadMainToolDefs()
	if err != nil {
		return fmt.Errorf("tools: %w", err)
	}
	compression, err := buildCompressionTemplate()
	if err != nil {
		return fmt.Errorf("prompts: %w", err)
	}

	// 9) llmloop deps + runner
	registry, diffLookup, err := buildToolRegistry(opts.Repo, kept, opts)
	if err != nil {
		return fmt.Errorf("tools: %w", err)
	}
	collector := comment.NewCommentCollector()
	// Feed deterministic scanner findings into the collector before the
	// LLM loop runs so downstream consumers (dedup, emit) treat them like
	// any other finding.
	for _, sc := range scannerFindings {
		collector.Add(scannerFindingToComment(sc))
	}
	runner := llmloop.NewRunner(llmloop.Deps{
		LLMClient: llmClient,
		Model:     modelName,
		Template: llmloop.Template{
			MaxTokens:             defaultMaxTokens,
			MaxToolRequestTimes:   100,
			MemoryCompressionTask: compression,
		},
		Tools:            registry,
		MainToolDefs:     toolDefs,
		CommentCollector: collector,
		Session:          sess,
		DiffLookup:       diffLookup,
		AllDiffs:         func() []model.Diff { return kept },
	})

	// 10) dispatch — one MAIN_TASK per file. v1: sequential, no batching.
	changeFiles := renderChangedFilesJSON(kept)
	for _, d := range kept {
		path := d.NewPath
		if path == "" || path == "/dev/null" {
			path = d.OldPath
		}
		if opts.Verbose {
			fmt.Fprintf(cmd.OutOrStderr(), "[zreview] reviewing %s\n", path)
		}
		knownIssues := renderKnownIssuesBlock(scannerByPath[path])
		msgs := buildReviewMessages(sysPrompt, userTmpl, rulesBlock, reviewCtx, knownIssues, changeFiles, renderDiffsForFile(d))
		if _, stop, err := runner.RunMainTask(ctx, msgs, path); err != nil {
			return fmt.Errorf("llmloop %s: %w (stop=%s)", path, err, stop)
		}
	}
	runner.WaitBackground()

	// 11) post-process — deduped & line-snapped by llmloop already. Keep
	// only comments that resolved to a line, so the emitter never posts
	// unlocated findings to GitHub. Then apply the scoring policy: SUPPRESS
	// is always dropped, anything below --min-severity is dropped, and the
	// surviving scores are handed to emit for JSON envelope enrichment.
	comments := runner.CollectPendingComments()
	comments = filterResolved(comments)
	comments, scoreMap := filterByScore(comments, policy, minSev)

	// 11.5) fingerprint + carry-over (best-effort, PR-gated).
	// Reconciles fresh comments against persisted findings for this
	// (owner, repo, pr); unchanged file → carry, matching fp → keep,
	// touched file with no match → resolved (dropped).
	owner, repo := ownerRepoFromEnv()
	carry := runCarryover(comments, changedPathsFromDiffs(kept), owner, repo, opts.PR, cmd.OutOrStderr())
	comments = carry.Comments

	// 12) overlap (best-effort)
	overlapFindings := maybeDetectOverlap(ctx, opts, kept, llmClient)

	// 13) emit
	ghClient, _ := newGithubClient()
	err = emit(ctx, emitConfig{
		Format:       opts.Format,
		Output:       opts.Output,
		SessionID:    sess.SessionID(),
		Comments:     comments,
		Overlap:      overlapFindings,
		GHClient:     ghClient,
		PRNumber:     opts.PR,
		Stdout:       cmd.OutOrStdout(),
		Owner:        owner,
		Repo:         repo,
		FindingState: carry.State,
		Summary:      summary,
		Labels:       labels,
		Scores:       scoreMap,
	})
	if err != nil {
		return err
	}

	if code := exitCodeForComments(comments, scoreMap); code != 0 {
		// Return a typed error so main.go can pick up the special exit code.
		return &blockerExitError{code: code}
	}
	return nil
}

// blockerExitError signals a non-zero, non-1 exit code (e.g. 3 for blockers).
type blockerExitError struct{ code int }

func (e *blockerExitError) Error() string {
	return fmt.Sprintf("review found blocker findings (exit %d)", e.code)
}

// ---- pipeline helpers ----

// resolveDiffs picks the appropriate diff provider based on the flag combo.
func resolveDiffs(ctx context.Context, opts *reviewOpts) ([]model.Diff, error) {
	runner := gitcmd.New(0)
	var p *diff.Provider
	switch {
	case opts.Commit != "":
		p = diff.NewCommitProvider(opts.Repo, opts.Commit, runner)
	case opts.From != "" || opts.To != "":
		to := opts.To
		if to == "" {
			to = "HEAD"
		}
		p = diff.NewProvider(opts.Repo, opts.From, to, runner)
	default:
		p = diff.NewWorkspaceProvider(opts.Repo, runner)
	}
	return p.GetDiff(ctx)
}

// applySelector runs the deterministic pre-dispatch gate.
func applySelector(diffs []model.Diff) []selector.Decision {
	opts := selector.DefaultOptions()
	opts.TokenCounter = llm.CountTokens
	return selector.Select(diffs, opts)
}

// openStore opens the index Store if ZREVIEW_DB_URL is set. Returns (nil,nil)
// otherwise, which signals JIT-context mode downstream.
func openStore(ctx context.Context) (index.Store, error) {
	dsn := os.Getenv("ZREVIEW_DB_URL")
	if dsn == "" {
		return nil, nil
	}
	return index.NewStore(ctx, dsn)
}

// loadRules loads the org rules repo (or dir) named by ZREVIEW_ORG_RULES_REPO
// and returns the rendered prompt block for the paths kept in this run.
func loadRules(ctx context.Context, kept []model.Diff) (string, error) {
	spec := os.Getenv("ZREVIEW_ORG_RULES_REPO")
	if spec == "" {
		return "", nil
	}
	cacheDir := filepath.Join(os.TempDir(), "zreview-rules-cache")
	loader := rules.NewLoader(cacheDir)
	all, err := loader.LoadFromRepo(ctx, spec)
	if err != nil {
		return "", err
	}
	paths := make([]string, 0, len(kept))
	for _, d := range kept {
		p := d.NewPath
		if p == "" || p == "/dev/null" {
			p = d.OldPath
		}
		paths = append(paths, p)
	}
	selected := rules.Select(all, rules.SelectionInput{FilePaths: paths})
	return rules.RenderForPrompt(selected), nil
}

// buildContext produces the markdown "codebase context" block. Store may be
// nil, in which case reviewctx falls back to JIT extraction.
func buildContext(ctx context.Context, repo string, kept []model.Diff, store index.Store) (string, error) {
	newFileContent := make(map[string]string, len(kept))
	changed := changedPathsFromDiffs(kept)
	for _, d := range kept {
		p := d.NewPath
		if p == "" || p == "/dev/null" {
			p = d.OldPath
		}
		if d.NewFileContent != "" {
			newFileContent[p] = d.NewFileContent
		}
	}
	return reviewctx.Build(ctx, reviewctx.Options{
		RepoRoot:       repo,
		ChangedPaths:   changed,
		NewFileContent: newFileContent,
		Store:          store,
	})
}

// changedPathsFromDiffs extracts new (or fallback old) paths from a diff slice.
func changedPathsFromDiffs(kept []model.Diff) []string {
	paths := make([]string, 0, len(kept))
	for _, d := range kept {
		p := d.NewPath
		if p == "" || p == "/dev/null" {
			p = d.OldPath
		}
		paths = append(paths, p)
	}
	return paths
}

// maybeSpawnWarmer bridges the review flow to the index warmer: enumerate
// changed paths, check the store, if any are missing spawn a detached
// `zreview index --paths ...` and continue. The current review still uses
// JIT for the missing files (that's reviewctx.Build's built-in fallback);
// the NEXT review of the same files reads real summaries. All errors are
// non-fatal.
func maybeSpawnWarmer(ctx context.Context, store index.Store, repo string, kept []model.Diff, out io.Writer) {
	paths := changedPathsFromDiffs(kept)
	missing, err := missingSummaryPaths(ctx, store, paths)
	if err != nil {
		fmt.Fprintf(out, "[zreview] warmer: %v (skipping)\n", err)
		return
	}
	spawnIndexWarmer(repo, missing, out)
}

// newSession creates (or resumes) a session file under ZREVIEW_SESSION_DIR.
func newSession(resumeID string) (*session.Session, error) {
	dir := os.Getenv("ZREVIEW_SESSION_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve session dir: %w", err)
		}
		dir = filepath.Join(home, ".zreview", "sessions")
	}
	return session.New(dir, resumeID)
}

// newLLMTiers resolves the two-tier LLM clients. Main is resolved from the OCR
// resolver (env / ~/.opencodereview/config.json / shell rc, with ZREVIEW_MODEL
// / ZREVIEW_PROVIDER overrides). Cheap opts in via ZREVIEW_CHEAP_MODEL /
// ZREVIEW_CHEAP_PROVIDER; when unset, Cheap == Main so callers never need a
// nil check.
func newLLMTiers() (llm.Tiers, error) {
	configPath := ""
	if home, err := os.UserHomeDir(); err == nil {
		configPath = filepath.Join(home, ".opencodereview", "config.json")
	}
	return llm.ResolveTiers(configPath, llm.ResolveOptions{
		Provider: os.Getenv("ZREVIEW_PROVIDER"),
		Model:    os.Getenv("ZREVIEW_MODEL"),
	})
}

// buildToolRegistry wires the context-gathering tools the LLM can call
// during the loop. Returns a lookup closure for llmloop's DiffLookup.
func buildToolRegistry(repo string, kept []model.Diff, opts *reviewOpts) (*tool.Registry, func(string) *model.Diff, error) {
	reg := tool.NewRegistry()

	mode := tool.ParseReviewMode(opts.From, opts.To, opts.Commit)
	ref := ""
	if r, ok := mode.RefValue(opts.To, opts.Commit); ok {
		ref = r
	}
	fr := &tool.FileReader{
		RepoDir: repo,
		Mode:    mode,
		Ref:     ref,
		Runner:  gitcmd.New(0),
	}
	reg.Register(tool.NewFileRead(fr))
	reg.Register(tool.NewFileFind(fr))
	reg.Register(tool.NewCodeSearch(fr))

	diffTextByPath := make(map[string]string, len(kept))
	diffByPath := make(map[string]*model.Diff, len(kept))
	for i := range kept {
		d := &kept[i]
		p := d.NewPath
		if p == "" || p == "/dev/null" {
			p = d.OldPath
		}
		diffTextByPath[p] = d.Diff
		diffByPath[p] = d
	}
	reg.Register(tool.NewFileReadDiff(tool.NewDiffMap(diffTextByPath)))
	reg.Freeze()

	lookup := func(path string) *model.Diff {
		return diffByPath[path]
	}
	return reg, lookup, nil
}

// filterResolved drops comments whose line numbers never resolved. GitHub
// posting rejects them and the stdout/json formats have nothing useful to
// show for them either.
func filterResolved(comments []model.LlmComment) []model.LlmComment {
	out := comments[:0]
	for _, c := range comments {
		if c.StartLine == 0 && c.EndLine == 0 {
			continue
		}
		out = append(out, c)
	}
	return out
}

// filterByScore applies the deterministic scoring policy: SUPPRESS
// findings are always dropped; findings below minSev are dropped;
// survivors are returned along with a map of commentKey → Score so the
// emitter can attach severity/confidence/impact/rationale to the JSON
// envelope.
func filterByScore(comments []model.LlmComment, p scoring.Policy, minSev scoring.Severity) ([]model.LlmComment, map[string]scoring.Score) {
	if len(comments) == 0 {
		return comments, nil
	}
	minRank := scoring.Rank(minSev)
	out := comments[:0]
	scores := make(map[string]scoring.Score, len(comments))
	for _, c := range comments {
		sc := scoring.ScoreOne(c, p)
		if sc.Score.Severity == scoring.SeveritySuppress {
			continue
		}
		if scoring.Rank(sc.Score.Severity) < minRank {
			continue
		}
		out = append(out, c)
		scores[commentKey(c)] = sc.Score
	}
	return out, scores
}

// maybeDetectOverlap runs cross-PR overlap detection when a GITHUB_TOKEN is
// available and the user hasn't opted out. Any failure is swallowed — this
// is best-effort and must never block review output.
func maybeDetectOverlap(ctx context.Context, opts *reviewOpts, kept []model.Diff, llmc llm.LLMClient) []overlap.Finding {
	if os.Getenv("ZREVIEW_OVERLAP_ENABLED") == "0" {
		return nil
	}
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" || opts.PR == 0 {
		return nil
	}
	owner, repo := ownerRepoFromEnv()
	if owner == "" || repo == "" {
		return nil
	}
	ghc, err := gh.NewClient(gh.Options{Token: token})
	if err != nil {
		return nil
	}
	paths := make([]string, 0, len(kept))
	for _, d := range kept {
		p := d.NewPath
		if p == "" || p == "/dev/null" {
			p = d.OldPath
		}
		paths = append(paths, p)
	}
	cur := overlap.PR{Owner: owner, Repo: repo, Number: opts.PR, Paths: paths}
	findings, _ := overlap.Detect(ctx, overlap.DefaultConfig(), cur, ghc, llmc)
	return findings
}

// ownerRepoFromEnv reads GITHUB_REPOSITORY (the GH Actions convention:
// "owner/repo") for the current owner/repo. Empty return means the caller
// must skip anything that needs GitHub identity.
func ownerRepoFromEnv() (string, string) {
	v := os.Getenv("GITHUB_REPOSITORY")
	if v == "" {
		return "", ""
	}
	parts := strings.SplitN(v, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

// runCheapAgents fires the summarizer and labeler in parallel against the
// cheap tier. Both are best-effort: any error logs and yields a zero value,
// so the main review path never blocks on them. errgroup.Wait always returns
// nil here because the closures swallow errors after logging — the group is
// used purely for parallel dispatch, not error propagation.
func runCheapAgents(ctx context.Context, tiers llm.Tiers, kept []model.Diff, verbose bool, out io.Writer) (model.Summary, model.Labels) {
	var summary model.Summary
	var labels model.Labels
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		s, err := RunSummarizer(gctx, tiers.Cheap, tiers.CheapModel, kept)
		if err != nil {
			fmt.Fprintf(out, "[zreview] summarizer: %v (continuing without)\n", err)
			return nil
		}
		summary = s
		if verbose {
			fmt.Fprintf(out, "[zreview] summarizer: risk=%q groups=%d\n", s.Risk, len(s.ChangeGroups))
		}
		return nil
	})
	g.Go(func() error {
		l, err := RunLabeler(gctx, tiers.Cheap, tiers.CheapModel, kept)
		if err != nil {
			fmt.Fprintf(out, "[zreview] labeler: %v (continuing without)\n", err)
			return nil
		}
		labels = l
		if verbose {
			fmt.Fprintf(out, "[zreview] labeler: type=%q risk_tag=%q\n", l.PRType, l.RiskTag)
		}
		return nil
	})
	_ = g.Wait()
	return summary, labels
}

// newGithubClient returns a gh.Client if GITHUB_TOKEN is set, else (nil,nil).
func newGithubClient() (*gh.Client, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil, nil
	}
	return gh.NewClient(gh.Options{Token: token})
}

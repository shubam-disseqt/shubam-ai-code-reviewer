// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/shubam-disseqt/z-code-reviewer/internal/comment"
	"github.com/shubam-disseqt/z-code-reviewer/internal/depgraph"
	"github.com/shubam-disseqt/z-code-reviewer/internal/diff"
	"github.com/shubam-disseqt/z-code-reviewer/internal/effort"
	"github.com/shubam-disseqt/z-code-reviewer/internal/gh"
	"github.com/shubam-disseqt/z-code-reviewer/internal/gitcmd"
	"github.com/shubam-disseqt/z-code-reviewer/internal/index"
	"github.com/shubam-disseqt/z-code-reviewer/internal/llm"
	"github.com/shubam-disseqt/z-code-reviewer/internal/llmloop"
	"github.com/shubam-disseqt/z-code-reviewer/internal/logutil"
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
	// WaitSARIF blocks after upload until GitHub reports "complete" or
	// "failed" (5-min ceiling inside the emitter). Off by default so
	// review latency isn't dominated by Code Scanning processing time.
	WaitSARIF bool
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
	f.StringVar(&opts.Format, "format", formatStdout, "output format: stdout | json | github | sarif")
	f.StringVar(&opts.Output, "output", "-", "output path (json/sarif formats; \"-\" for stdout)")
	f.IntVar(&opts.PR, "pr", 0, "PR number (github format)")
	f.StringVar(&opts.Resume, "resume", "", "resume an interrupted session by id")
	f.BoolVar(&opts.Verbose, "verbose", false, "log more")
	f.StringVar(&opts.MinSeverity, "min-severity", opts.MinSeverity,
		"drop findings below this bucket: LOW | MEDIUM | HIGH | CRITICAL (SUPPRESS is always dropped)")
	f.BoolVar(&opts.WaitSARIF, "wait-sarif", false,
		"poll GitHub until SARIF processing is complete/failed before returning (github format only)")

	return cmd
}

func (o *reviewOpts) validate() error {
	if o.Commit != "" && (o.From != "" || o.To != "") {
		return fmt.Errorf("--commit is mutually exclusive with --from/--to")
	}
	if !validFormat(o.Format) {
		return fmt.Errorf("--format must be one of stdout|json|github|sarif (got %q)", o.Format)
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
	// One logger per review, configured from env. Passed down explicitly —
	// no package-level state. Text mode is default; JSON mode via
	// ZREVIEW_LOG_FORMAT=json for CI/observability.
	logger := logutil.FromEnv(cmd.OutOrStderr())
	started := time.Now()
	metrics := Metrics{}

	// 0) scoring policy — loaded early so a bad override fails fast
	// before we spend tokens. Repo-local overrides log so users know
	// which policy is in play.
	policy, err := scoring.LoadPolicy(opts.Repo)
	if err != nil {
		return fmt.Errorf("scoring: %w", err)
	}
	if opts.Verbose || policy.Source() != "embedded" {
		logutil.WithStage(logger, "scoring").Info("policy loaded", "source", policy.Source())
	}
	minSev, _ := scoring.ParseSeverity(opts.MinSeverity) // validated in opts.validate()

	// 1) diff
	diffs, err := resolveDiffs(ctx, opts)
	if err != nil {
		return fmt.Errorf("diff: %w", err)
	}
	if len(diffs) == 0 {
		logutil.WithStage(logger, "diff").Info("no changes to review")
		return nil
	}

	// 2) selector
	decisions := applySelector(diffs)
	kept := selector.Kept(decisions)
	if opts.Verbose {
		logutil.WithStage(logger, "selector").Info("kept diffs", "kept", len(kept), "total", len(diffs))
	}
	if len(kept) == 0 {
		logutil.WithStage(logger, "selector").Info("every diff was filtered out")
		return nil
	}
	metrics.FilesReviewed = len(kept)

	// 3) index store (optional)
	store, err := openStore(ctx)
	if err != nil {
		return fmt.Errorf("index store: %w", err)
	}
	if store != nil {
		defer store.Close()
		maybeSpawnWarmer(ctx, store, opts.Repo, kept, logger)
	}

	// 3.5) deterministic scanners — best-effort, tolerant of missing
	// binaries. Findings tagged Source="scanner:<tool>" enter the same
	// comment collector as LLM findings; Phase 16 will score them and
	// Phase 17 will route CVE/secret categories to SARIF.
	scannerFindings := runScanners(ctx, opts.Repo, kept, logger)
	scannerByPath := groupScannerFindingsByPath(scannerFindings)
	metrics.ScannerFindings = len(scannerFindings)

	// 3.6) summarizer + labeler (parallel, cheap tier). Best-effort — a
	// failure here logs and continues with zero values. We resolve tiers
	// early so the errgroup can dispatch alongside the main LLM setup.
	tiers, err := newLLMTiers()
	if err != nil {
		return fmt.Errorf("llm: %w", err)
	}
	tiersLogger := logutil.WithStage(logger, "tiers")
	for _, note := range tiers.Notes {
		tiersLogger.Info(note)
	}
	summary, labels := runCheapAgents(ctx, tiers, kept, opts.Verbose, logger)

	// 4) rules
	rulesBlock, err := loadRules(ctx, kept)
	if err != nil {
		// Rules failure is not fatal — log and continue.
		logutil.WithStage(logger, "rules").Warn("continuing without rules", "err", err.Error())
		rulesBlock = ""
	}

	// 5) context
	reviewCtx, err := buildContext(ctx, opts.Repo, kept, store)
	if err != nil {
		logutil.WithStage(logger, "context").Warn("continuing without context", "err", err.Error())
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
	reviewLogger := logutil.WithStage(logger, "review")
	for _, d := range kept {
		path := d.NewPath
		if path == "" || path == "/dev/null" {
			path = d.OldPath
		}
		if opts.Verbose {
			reviewLogger.Info("reviewing file", "path", path)
		}
		knownIssues := renderKnownIssuesBlock(scannerByPath[path])
		msgs := buildReviewMessages(sysPrompt, userTmpl, rulesBlock, reviewCtx, knownIssues, changeFiles, renderDiffsForFile(d))
		if _, stop, err := runner.RunMainTask(ctx, msgs, path); err != nil {
			return fmt.Errorf("llmloop %s: %w (stop=%s)", path, err, stop)
		}
	}
	runner.WaitBackground()
	metrics.PromptTokens = runner.TotalInputTokens()
	metrics.CompletionTokens = runner.TotalOutputTokens()

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
	carry := runCarryover(comments, changedPathsFromDiffs(kept), owner, repo, opts.PR, logger)
	comments = carry.Comments
	metrics.CarriedFindings = carry.Counts.Carried
	metrics.ResolvedFindings = carry.Counts.Resolved
	metrics.NewFindings = carry.Counts.New

	// 12) overlap (best-effort)
	overlapFindings := maybeDetectOverlap(ctx, opts, kept, llmClient)

	// 12.5) reviewer effort score — deterministic 0-10 from the diff, the
	// scored findings, and cross-PR overlap. Best-effort: a policy error
	// logs and continues with a zero-valued Score (empty audit).
	reviewerEffort := computeReviewerEffort(opts.Repo, kept, scoreMap, len(overlapFindings), logger)

	// 12.6) deterministic Go import graph. Parses actual imports from the
	// changed files' source so every edge in the rendered diagram is
	// grounded in the code, not inferred from filenames by an LLM.
	// Empty string when the diff has no cross-package imports — the
	// description block just omits the section in that case.
	pkgDiagram := computeDepGraph(opts.Repo, kept)

	// 13) emit
	ghClient, _ := newGithubClient()
	// GitHub's PR-comment API rejects an empty commit_id. Resolve the head
	// SHA locally from the range's --to ref (or HEAD if unset). This
	// assumes the operator pushed the same commit to the PR head — true
	// for CI runs and for the doc-recommended local workflow.
	commitSHA := resolveHeadSHA(opts)
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
		CommitSHA:    commitSHA,
		Ref:          os.Getenv("GITHUB_REF"),
		WaitSARIF:    opts.WaitSARIF,
		FindingState: carry.State,
		Summary:      summary,
		Labels:       labels,
		Scores:       scoreMap,
		Effort:       reviewerEffort,
		PkgDiagram:   pkgDiagram,
	})
	if err != nil {
		return err
	}
	metrics.CommentsPosted = len(comments)
	metrics.DurationMs = time.Since(started).Milliseconds()
	emitMetrics(logger, metrics)

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
func maybeSpawnWarmer(ctx context.Context, store index.Store, repo string, kept []model.Diff, logger *slog.Logger) {
	paths := changedPathsFromDiffs(kept)
	missing, err := missingSummaryPaths(ctx, store, paths)
	if err != nil {
		logutil.WithStage(logger, "warmer").Warn("skipping", "err", err.Error())
		return
	}
	spawnIndexWarmer(repo, missing, logger)
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

// resolveHeadSHA returns the git SHA of the "to" ref for --format=github
// posting. Falls back to HEAD when --to is empty (workspace / commit modes).
// Best-effort: an unresolvable ref returns "" and the poster surfaces a
// clearer error at the API boundary than an empty commit_id would.
func resolveHeadSHA(opts *reviewOpts) string {
	ref := "HEAD"
	if opts.To != "" {
		ref = opts.To
	} else if opts.Commit != "" {
		ref = opts.Commit
	}
	out, err := gitcmd.New(0).Run(context.Background(), opts.Repo, "rev-parse", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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

	// code_comment and task_done need to pass the registry lookup in
	// llmloop.executeToolCall so the loop enters their special-cased
	// branches (comment collection / task termination). Their Execute
	// methods never run — the loop returns before the fallthrough — but
	// without a registration the loop returns "not available" and every
	// LLM finding gets silently dropped.
	reg.Register(tool.NewStub(tool.CodeComment))
	reg.Register(tool.NewStub(tool.TaskDone))
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
func runCheapAgents(ctx context.Context, tiers llm.Tiers, kept []model.Diff, verbose bool, logger *slog.Logger) (model.Summary, model.Labels) {
	var summary model.Summary
	var labels model.Labels
	sumLog := logutil.WithStage(logger, "summarizer")
	labLog := logutil.WithStage(logger, "labeler")
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		s, err := RunSummarizer(gctx, tiers.Cheap, tiers.CheapModel, kept)
		if err != nil {
			sumLog.Warn("continuing without summary", "err", err.Error())
			return nil
		}
		summary = s
		if verbose {
			sumLog.Info("summarized", "risk", s.Risk, "groups", len(s.ChangeGroups))
		}
		return nil
	})
	g.Go(func() error {
		l, err := RunLabeler(gctx, tiers.Cheap, tiers.CheapModel, kept)
		if err != nil {
			labLog.Warn("continuing without labels", "err", err.Error())
			return nil
		}
		labels = l
		if verbose {
			labLog.Info("labeled", "type", l.PRType, "risk_tag", l.RiskTag)
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

// computeReviewerEffort turns the diff, findings, and overlap counts into a
// deterministic 0-10 score. Best-effort: a policy-load failure logs and
// falls back to the embedded default; a total failure returns a zero Score
// so the description block just omits the section.
func computeReviewerEffort(repo string, kept []model.Diff, scoreMap map[string]scoring.Score, overlappingPRs int, logger *slog.Logger) effort.Score {
	pol, err := effort.LoadPolicy(repo)
	if err != nil {
		logutil.WithStage(logger, "effort").Warn("policy load failed, using zero score", "err", err.Error())
		return effort.Score{}
	}
	if pol.Source() != "embedded" {
		logutil.WithStage(logger, "effort").Info("policy loaded", "source", pol.Source())
	}

	files := make([]effort.FileDelta, 0, len(kept))
	for _, d := range kept {
		path := d.NewPath
		if path == "" || path == "/dev/null" {
			path = d.OldPath
		}
		added, deleted := countAddedDeleted(d.Diff)
		files = append(files, effort.FileDelta{
			Path:         path,
			LinesAdded:   added,
			LinesDeleted: deleted,
			IsNew:        d.IsNew,
			IsDeleted:    d.IsDeleted,
			IsRenamed:    d.IsRenamed,
			IsTest:       effort.IsTestFile(path),
		})
	}

	sev := map[string]int{}
	for _, s := range scoreMap {
		sev[string(s.Severity)]++
	}

	return effort.Compute(effort.Inputs{
		Files:          files,
		FindingsBySev:  sev,
		OverlappingPRs: overlappingPRs,
	}, pol)
}

// countAddedDeleted counts + and - lines in a unified diff body, skipping
// the `+++`/`---` file headers and `@@` hunk headers.
func countAddedDeleted(unifiedDiff string) (added, deleted int) {
	for _, line := range strings.Split(unifiedDiff, "\n") {
		if len(line) == 0 {
			continue
		}
		switch line[0] {
		case '+':
			if strings.HasPrefix(line, "+++") {
				continue
			}
			added++
		case '-':
			if strings.HasPrefix(line, "---") {
				continue
			}
			deleted++
		}
	}
	return added, deleted
}

// computeDepGraph parses the changed Go files' actual imports and returns
// a Mermaid flowchart body. Empty string on any error, or when no
// cross-package edges exist — the description-block renderer omits the
// section in that case.
func computeDepGraph(repo string, kept []model.Diff) string {
	modulePrefix := readModulePath(repo)
	if modulePrefix == "" {
		return ""
	}
	files := make([]depgraph.File, 0, len(kept))
	for _, d := range kept {
		p := d.NewPath
		if p == "" || p == "/dev/null" {
			p = d.OldPath
		}
		if d.NewFileContent == "" {
			continue // deleted file or content unavailable
		}
		files = append(files, depgraph.File{Path: p, Content: []byte(d.NewFileContent)})
	}
	return depgraph.Render(files, depgraph.DefaultOptions(modulePrefix))
}

// readModulePath returns the module path from <repo>/go.mod, or "" if the
// file is missing / malformed. Best-effort — a repo without go.mod just
// gets no depgraph diagram.
func readModulePath(repo string) string {
	data, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

# Bundled semgrep presets

zreview ships a small, curated semgrep ruleset per language so
`zreview review` finds real issues out-of-the-box, without asking users
to configure semgrep themselves. The rules are embedded into the binary
at build time via `go:embed`.

## What's here

| File              | Language(s)          | Rules | Focus                                                  |
|-------------------|----------------------|-------|--------------------------------------------------------|
| `javascript.yml`  | JavaScript, TypeScript | 12    | RCE, XSS, SQLi, weak crypto, CORS, JWT, path traversal |
| `python.yml`      | Python               | 13    | RCE, SQLi, pickle/yaml unsafe load, weak crypto, CSRF  |
| `ruby.yml`        | Ruby / Rails         | 11    | eval, command injection, mass assignment, weak crypto  |

Rules are intentionally scoped to high-signal, low-false-positive
patterns — think Snyk / Bandit / Brakeman defaults, not the full
semgrep-registry.

## Selection

The runner inspects the changed file paths and loads only the presets
whose language actually appears in the diff:

- `.js` / `.jsx` / `.ts` / `.tsx` / `.mjs` / `.cjs` → `javascript.yml`
- `.py` / `.pyi` → `python.yml`
- `.rb` / `.rake` / `Gemfile` → `ruby.yml`

If no supported language is touched, semgrep is skipped — we already
run govulncheck for Go.

## Overriding

Two env vars, no config file needed:

- `ZREVIEW_SEMGREP_CONFIG=<path>` — use your own semgrep config
  (file, directory, or `p/<pack>` reference) instead of the bundled
  presets. Passed through to `semgrep --config` verbatim.
- `ZREVIEW_DISABLE_SEMGREP_PRESETS=1` — disable bundled rules. Combined
  with an unset `ZREVIEW_SEMGREP_CONFIG`, semgrep is skipped entirely.

## Adding a rule

1. Edit the language file. Keep the rule shape from the existing
   entries: `id`, `message`, `severity` (`ERROR` / `WARNING` / `INFO`),
   `languages`, `pattern-either`, and a `metadata` block with `category`
   and CWE.
2. Test locally against a positive and a negative fixture:
   `semgrep --config internal/scanner/rules/python.yml some/file.py`.
3. If it fires on the codebase itself, either fix the code or narrow
   the rule — false positives erode trust fast.

Prefer additive PRs. If a rule turns out to be noisy in the wild, we
delete it rather than adding suppressions everywhere.

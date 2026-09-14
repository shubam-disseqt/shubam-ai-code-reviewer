# Running zreview on Windows

zreview is developed primarily on Linux and macOS. Windows support is
best-effort: `ci-windows.yml` runs build + race tests on `windows-latest`,
currently as an informational (non-blocking) job while we triage the first
wave of platform-specific failures.

## Supported environments

| Environment                | Status                                              |
| -------------------------- | --------------------------------------------------- |
| WSL2 (Ubuntu, Debian)      | First-class. Treat as Linux.                        |
| Native Windows + PowerShell 7 | Supported target. All tests should pass here.    |
| Native Windows + cmd.exe   | Should work; only PowerShell is smoke-tested in CI. |
| Git Bash / MSYS2           | Works but path translation can bite — see below.    |

If you have a choice, use WSL2. Everything else in this doc is for the
native-Windows case.

## Prerequisites

- Go, matching the version pinned in `go.mod` (currently 1.26.x). Get it
  from https://go.dev/dl/ or `winget install GoLang.Go`.
- Git for Windows. `winget install Git.Git`.
- A C toolchain is **not** required — zreview uses `modernc.org/sqlite`
  (pure Go), so `CGO_ENABLED=0` builds fine.

## Line endings

Windows Git defaults to `core.autocrlf=true`. That silently rewrites LF to
CRLF on checkout, which breaks:

- gofmt (fails, then complains about line-ending diffs)
- testdata golden files
- embedded documentation in `docs/embed.go`

Set this globally before cloning:

```powershell
git config --global core.autocrlf false
git config --global core.eol lf
```

If you already cloned with autocrlf on, refresh the working tree:

```powershell
git rm --cached -r .
git reset --hard
```

## Path separators

Anywhere zreview stores or compares paths internally, it uses
`filepath.Join` and `filepath.Rel`, which do the right thing on both
platforms. When paths cross a serialization boundary — session log JSON,
SQLite index rows, SARIF output — they are normalized to forward slashes
via `filepath.ToSlash`. If you see a Windows session log with mixed
separators, that is a bug — file it.

Places to watch when editing code:

- `internal/pathutil/` — `WithinBase` uses `os.PathSeparator`.
- `internal/index/indexer.go` — normalizes with `filepath.ToSlash` before
  storing.
- `internal/gitcmd/` — spawns `git`; path arguments must be OS-native.
- `internal/session/` — writes newline-delimited JSON; the newline is `\n`
  regardless of platform, and paths inside the JSON are forward-slash.
- `cmd/zreview/review_cmd.go` — `--repo` accepts either separator; it's
  canonicalized via `pathutil.CanonicalPath`.

Avoid string-joining paths by hand. `"a" + "/" + "b"` will bite you on
Windows even though it happens to work most of the time.

## Case sensitivity

NTFS is case-insensitive by default but case-preserving. Two gotchas:

1. `filepath.EvalSymlinks` returns the on-disk case, which may not match
   what the user typed. String-compare paths after canonicalizing both
   ends, and only with `strings.EqualFold` if you know both came from the
   filesystem.
2. Git on Windows will happily let you commit `Foo.go` and `foo.go` as
   two entries. zreview's file walkers see one; the index may see the
   other. If you produce or consume file lists, run them through
   `filepath.Clean` and stick to the case Git returns.

## External tools

zreview shells out to `git`, and optionally `semgrep`, `gitleaks`, and
`govulncheck`. On Windows these must be on `%PATH%`. If your project uses
a repo-local `.venv` or `node_modules/.bin` for scanners, add it to PATH
in the same shell before invoking `zreview`.

`internal/llm/keycmd_windows.go` already handles the API-key subprocess
via `cmd.exe /c`; `keycmd_unix.go` uses `sh -c`. If you add another
shell-out path, split it the same way — do not assume `/bin/sh`.

## Running the test suite

```powershell
go build ./...
go test -race -count=1 -timeout 10m ./...
```

Windows tests are noticeably slower than Linux, mostly from filesystem
syscalls and Defender scanning `AppData\Local\Temp`. Exclude the repo
and the Go build cache from real-time protection if you're doing
significant local iteration.

## Known workarounds

- **`gofmt` diffs with only whitespace**: line endings. Fix per the
  autocrlf section above.
- **`too many open files` under load**: rare, but Windows tightens the
  per-process handle limit under `-race`. Reduce `-parallel` or run
  a package at a time.
- **SQLite index locked**: the `modernc.org/sqlite` driver opens the DB
  file with default sharing. Antivirus that holds a scan lock will
  produce transient `SQLITE_BUSY`. Retry, or exclude the index path.
- **`git worktree` in a UNC / mapped drive path**: `--repo` accepts UNC
  paths, but `filepath.EvalSymlinks` behaves inconsistently across
  drive-letter mappings. Prefer a local drive path.

## Filing a Windows-only bug

Include:

- `go version` output
- `git --version`
- Windows edition + build (`winver`)
- Whether you're in PowerShell 7, cmd, Git Bash, or MSYS2
- The full command line and the failing test/package

`ci-windows.yml`'s uploaded `windows-test-logs` artifact is a good source
of comparison output.

# sacr

`sacr` is the CLI for [shubam-ai-code-reviewer](https://github.com/shubam-disseqt/shubam-ai-code-reviewer),
a Go-based AI code review tool.

## Install

```sh
npm install -g sacr
```

Then:

```sh
sacr --help
```

## How this package works

This is a thin launcher. The native `sacr` binary lives in a
platform-specific optional dependency:

| Platform / arch | npm package |
|---|---|
| linux / x64 | `sacr-linux-x64` |
| linux / arm64 | `sacr-linux-arm64` |
| darwin / x64 | `sacr-darwin-x64` |
| darwin / arm64 | `sacr-darwin-arm64` |
| win32 / x64 | `sacr-win32-x64` |
| win32 / arm64 | `sacr-win32-arm64` |

`npm` installs only the sub-package that matches your platform. The
`bin/sacr.js` launcher resolves that sub-package at runtime and execs
its native binary, propagating exit codes and signals.

If your platform is not in the table, install a prebuilt binary
directly from [GitHub Releases](https://github.com/shubam-disseqt/shubam-ai-code-reviewer/releases)
via the `install.sh` or `install.ps1` script — the npm package is a
convenience wrapper, not the source of truth.

## Verifying the binary

Each release publishes a `sha256sum.txt` and a SLSA build-provenance
attestation. See [SECURITY.md](https://github.com/shubam-disseqt/shubam-ai-code-reviewer/blob/main/SECURITY.md)
for verification instructions.

## License

Apache-2.0. See [LICENSE](https://github.com/shubam-disseqt/shubam-ai-code-reviewer/blob/main/LICENSE).

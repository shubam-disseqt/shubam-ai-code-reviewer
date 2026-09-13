# zreview

`zreview` is the CLI for [z-code-reviewer](https://github.com/shubam-disseqt/z-code-reviewer),
a Go-based AI code review tool.

## Install

```sh
npm install -g zreview
```

Then:

```sh
zreview --help
```

## How this package works

This is a thin launcher. The native `zreview` binary lives in a
platform-specific optional dependency:

| Platform / arch | npm package |
|---|---|
| linux / x64 | `zreview-linux-x64` |
| linux / arm64 | `zreview-linux-arm64` |
| darwin / x64 | `zreview-darwin-x64` |
| darwin / arm64 | `zreview-darwin-arm64` |
| win32 / x64 | `zreview-win32-x64` |
| win32 / arm64 | `zreview-win32-arm64` |

`npm` installs only the sub-package that matches your platform. The
`bin/zreview.js` launcher resolves that sub-package at runtime and execs
its native binary, propagating exit codes and signals.

If your platform is not in the table, install a prebuilt binary
directly from [GitHub Releases](https://github.com/shubam-disseqt/z-code-reviewer/releases)
via the `install.sh` or `install.ps1` script — the npm package is a
convenience wrapper, not the source of truth.

## Verifying the binary

Each release publishes a `sha256sum.txt` and a SLSA build-provenance
attestation. See [SECURITY.md](https://github.com/shubam-disseqt/z-code-reviewer/blob/main/SECURITY.md)
for verification instructions.

## License

Apache-2.0. See [LICENSE](https://github.com/shubam-disseqt/z-code-reviewer/blob/main/LICENSE).

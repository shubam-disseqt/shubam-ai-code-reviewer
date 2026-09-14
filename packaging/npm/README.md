# zreview (npm)

Deterministic, LLM-free code review CLI. This package is the npm distribution
channel for [`zreview`](https://github.com/shubam-disseqt/z-code-reviewer).

## Install

```sh
npm i -g zreview
```

The `postinstall` step downloads the matching prebuilt binary from GitHub
Releases and drops it next to a small Node wrapper on your `PATH`.

## Use

```sh
zreview review
zreview version
zreview --help
```

## Supported platforms

| OS      | Arch  |
| ------- | ----- |
| macOS   | arm64 |
| macOS   | x64   |
| Linux   | arm64 |
| Linux   | x64   |
| Windows | arm64 |
| Windows | x64   |

Unsupported platforms fall back to building from source — see the main
[README](https://github.com/shubam-disseqt/z-code-reviewer#build-from-source).

## Environment overrides

- `ZREVIEW_VERSION` — force a specific release version.
- `ZREVIEW_DOWNLOAD_BASE` — mirror or proxy URL prefix for the archives.
- `ZREVIEW_SKIP_DOWNLOAD=1` — skip the postinstall download (offline installs,
  air-gapped mirroring workflows).

## What ships in this package

- `install.js` — postinstall downloader that fetches the binary tarball.
- `bin/zreview.js` — Node wrapper that execs the downloaded binary.
- Downloaded artifacts land in `bin/` at install time and are ignored by git.

## License

Apache-2.0. See the repo root for full license and NOTICE.

# zreview — VS Code extension

Runs [zreview](https://github.com/shubam-disseqt/z-code-reviewer) against your working copy and renders findings in the Problems panel and editor gutter.

## Prerequisites

- The `zreview` CLI on your `PATH` (or configure `zreview.binaryPath`).
- A git repository open as your workspace root.
- Whatever env vars your zreview config needs (`OPENAI_API_KEY`, etc.) — the extension inherits your VS Code process env.

## Install (VSIX)

```bash
cd ide/vscode
npm install
npm run build
npx vsce package
code --install-extension zreview-0.1.0.vsix
```

## Usage

Command palette (`Cmd+Shift+P` / `Ctrl+Shift+P`):

- **zreview: Review Current Diff** — runs `zreview review --from HEAD~1 --to HEAD --format json`
- **zreview: Review Current File** — runs `zreview review --format json` in the workspace and surfaces findings for the active file

Findings appear in the Problems panel with severity mapped as:

| zreview severity | VS Code severity |
| ---------------- | ---------------- |
| critical, high   | Error            |
| medium           | Warning          |
| low              | Information      |
| (other)          | Hint             |

## Settings

| Setting             | Default    | Description                                       |
| ------------------- | ---------- | ------------------------------------------------- |
| `zreview.binaryPath`| `zreview`  | Path to the zreview binary.                       |
| `zreview.format`    | `json`     | Output format. Only `json` is consumed today.     |

## Status

Skeleton — no tests inside VS Code yet. Contributions welcome.

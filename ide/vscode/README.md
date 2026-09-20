# sacr — VS Code extension

Runs [sacr](https://github.com/shubam-disseqt/shubam-ai-code-reviewer) against your working copy and renders findings in the Problems panel and editor gutter.

## Prerequisites

- The `sacr` CLI on your `PATH` (or configure `sacr.binaryPath`).
- A git repository open as your workspace root.
- Whatever env vars your sacr config needs (`OPENAI_API_KEY`, etc.) — the extension inherits your VS Code process env.

## Install (VSIX)

```bash
cd ide/vscode
npm install
npm run build
npx vsce package
code --install-extension sacr-0.1.0.vsix
```

## Usage

Command palette (`Cmd+Shift+P` / `Ctrl+Shift+P`):

- **sacr: Review Current Diff** — runs `sacr review --from HEAD~1 --to HEAD --format json`
- **sacr: Review Current File** — runs `sacr review --format json` in the workspace and surfaces findings for the active file

Findings appear in the Problems panel with severity mapped as:

| sacr severity | VS Code severity |
| ---------------- | ---------------- |
| critical, high   | Error            |
| medium           | Warning          |
| low              | Information      |
| (other)          | Hint             |

## Settings

| Setting             | Default    | Description                                       |
| ------------------- | ---------- | ------------------------------------------------- |
| `sacr.binaryPath`| `sacr`  | Path to the sacr binary.                       |
| `sacr.format`    | `json`     | Output format. Only `json` is consumed today.     |

## Status

Skeleton — no tests inside VS Code yet. Contributions welcome.

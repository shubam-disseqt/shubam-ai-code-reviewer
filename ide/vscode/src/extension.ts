// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// VS Code extension skeleton for zreview. Shells out to the CLI, parses
// `--format json`, and renders findings in the Problems panel.

import { spawn } from "child_process";
import * as path from "path";
import * as vscode from "vscode";

// Shape of one comment in `zreview review --format json`. Mirrors
// emittedComment in cmd/zreview/emit.go (LlmComment fields flattened).
interface ZreviewComment {
  path: string;
  content: string;
  start_line: number;
  end_line?: number;
  severity?: string;
  category?: string;
  source?: string;
}

interface ZreviewResult {
  session_id?: string;
  comments?: ZreviewComment[];
}

let diagnostics: vscode.DiagnosticCollection;

export function activate(context: vscode.ExtensionContext): void {
  diagnostics = vscode.languages.createDiagnosticCollection("zreview");
  context.subscriptions.push(
    diagnostics,
    vscode.commands.registerCommand("zreview.reviewCurrentDiff", () =>
      runReview(["review", "--from", "HEAD~1", "--to", "HEAD", "--format", "json"], "Reviewing HEAD~1..HEAD"),
    ),
    vscode.commands.registerCommand("zreview.reviewFile", () => {
      const editor = vscode.window.activeTextEditor;
      if (!editor) {
        vscode.window.showWarningMessage("zreview: open a file first.");
        return;
      }
      // No dedicated --file flag yet; review uncommitted changes and let
      // the Problems panel filter by file. ponytail: swap for --file once
      // the CLI grows a single-file mode.
      return runReview(["review", "--format", "json"], `Reviewing ${path.basename(editor.document.fileName)}`);
    }),
  );
}

export function deactivate(): void {
  diagnostics?.dispose();
}

async function runReview(args: string[], title: string): Promise<void> {
  const cfg = vscode.workspace.getConfiguration("zreview");
  const bin = cfg.get<string>("binaryPath", "zreview");
  const cwd = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
  if (!cwd) {
    vscode.window.showErrorMessage("zreview: open a workspace folder first.");
    return;
  }

  await vscode.window.withProgress(
    { location: vscode.ProgressLocation.Notification, title, cancellable: false },
    async () => {
      try {
        const stdout = await execCapture(bin, args, cwd);
        const parsed: ZreviewResult = JSON.parse(stdout);
        renderFindings(parsed.comments ?? [], cwd);
        const n = parsed.comments?.length ?? 0;
        vscode.window.showInformationMessage(`zreview: ${n} finding${n === 1 ? "" : "s"}.`);
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        vscode.window.showErrorMessage(`zreview failed: ${msg}`);
      }
    },
  );
}

function renderFindings(comments: ZreviewComment[], cwd: string): void {
  diagnostics.clear();
  const grouped = new Map<string, vscode.Diagnostic[]>();
  for (const c of comments) {
    const startLine = Math.max(0, (c.start_line || 1) - 1);
    const endLine = Math.max(startLine, (c.end_line || c.start_line || 1) - 1);
    const range = new vscode.Range(startLine, 0, endLine, Number.MAX_SAFE_INTEGER);
    const diag = new vscode.Diagnostic(range, c.content, severityFor(c.severity));
    diag.source = c.source ? `zreview:${c.source}` : "zreview";
    if (c.category) {
      diag.code = c.category;
    }
    const abs = path.isAbsolute(c.path) ? c.path : path.join(cwd, c.path);
    const bucket = grouped.get(abs) ?? [];
    bucket.push(diag);
    grouped.set(abs, bucket);
  }
  for (const [file, diags] of grouped) {
    diagnostics.set(vscode.Uri.file(file), diags);
  }
}

function severityFor(raw: string | undefined): vscode.DiagnosticSeverity {
  switch ((raw ?? "").toLowerCase()) {
    case "critical":
    case "high":
      return vscode.DiagnosticSeverity.Error;
    case "medium":
      return vscode.DiagnosticSeverity.Warning;
    case "low":
      return vscode.DiagnosticSeverity.Information;
    default:
      return vscode.DiagnosticSeverity.Hint;
  }
}

function execCapture(bin: string, args: string[], cwd: string): Promise<string> {
  return new Promise((resolve, reject) => {
    const proc = spawn(bin, args, { cwd });
    let stdout = "";
    let stderr = "";
    proc.stdout.on("data", (d) => (stdout += d.toString()));
    proc.stderr.on("data", (d) => (stderr += d.toString()));
    proc.on("error", (e) => reject(new Error(`spawn ${bin}: ${e.message}`)));
    proc.on("close", (code) => {
      if (code === 0) {
        resolve(stdout);
      } else {
        reject(new Error(`exit ${code}: ${stderr.trim() || "(no stderr)"}`));
      }
    });
  });
}

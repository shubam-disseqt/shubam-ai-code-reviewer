#!/usr/bin/env node
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Launcher for the zreview native binary. The real executable ships in a
// platform-specific optional dependency (e.g. zreview-linux-x64). This file
// resolves the correct sub-package, execs the binary, and propagates the
// child's exit code and signals.

"use strict";

const { spawnSync } = require("node:child_process");
const path = require("node:path");

// Map Node's platform+arch to the npm sub-package we publish. Keep this in
// lockstep with the matrix in .github/workflows/release.yml.
const SUPPORTED = {
  "linux-x64": "zreview-linux-x64",
  "linux-arm64": "zreview-linux-arm64",
  "darwin-x64": "zreview-darwin-x64",
  "darwin-arm64": "zreview-darwin-arm64",
  "win32-x64": "zreview-win32-x64",
  "win32-arm64": "zreview-win32-arm64",
};

function die(msg) {
  process.stderr.write(`zreview: ${msg}\n`);
  process.exit(1);
}

function resolveBinary() {
  const key = `${process.platform}-${process.arch}`;
  const pkg = SUPPORTED[key];
  if (!pkg) {
    die(
      `unsupported platform ${key}. Supported: ${Object.keys(SUPPORTED).join(", ")}.\n` +
        `See https://github.com/shubam-disseqt/z-code-reviewer/releases for prebuilt binaries.`,
    );
  }
  const binName = process.platform === "win32" ? "zreview.exe" : "zreview";
  // require.resolve on the package's package.json gives us the install root
  // regardless of hoisting layout (npm, pnpm, yarn).
  let pkgRoot;
  try {
    pkgRoot = path.dirname(require.resolve(`${pkg}/package.json`));
  } catch (err) {
    die(
      `missing optional dependency ${pkg}. This usually means npm skipped ` +
        `it (--no-optional, --omit=optional, or a lockfile without the ` +
        `platform entry). Reinstall with optional dependencies enabled.`,
    );
  }
  return path.join(pkgRoot, "bin", binName);
}

function main() {
  const bin = resolveBinary();
  const result = spawnSync(bin, process.argv.slice(2), {
    stdio: "inherit",
    // Windows needs shell:false + the .exe extension already on `bin`.
    windowsHide: false,
  });
  if (result.error) {
    if (result.error.code === "ENOENT") {
      die(`binary not found at ${bin}. Try reinstalling zreview.`);
    }
    die(`failed to launch ${bin}: ${result.error.message}`);
  }
  if (result.signal) {
    // Re-raise the signal so callers observe the same termination cause.
    process.kill(process.pid, result.signal);
    return;
  }
  process.exit(result.status ?? 1);
}

main();

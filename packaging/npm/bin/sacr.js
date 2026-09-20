#!/usr/bin/env node
// SPDX-License-Identifier: Apache-2.0
// Thin launcher that hands off to the platform-specific sacr binary
// dropped in by install.js.

"use strict";

const path = require("path");
const fs = require("fs");
const { spawn } = require("child_process");

const binName = process.platform === "win32" ? "sacr.exe" : "sacr";
const binPath = path.join(__dirname, binName);

if (!fs.existsSync(binPath)) {
  console.error(`[sacr] binary not found at ${binPath}`);
  console.error(`[sacr] postinstall did not run or failed. Try:`);
  console.error(`[sacr]   npm rebuild sacr`);
  console.error(`[sacr] or reinstall: npm i -g sacr`);
  process.exit(1);
}

const child = spawn(binPath, process.argv.slice(2), { stdio: "inherit" });
child.on("error", (err) => {
  console.error(`[sacr] failed to launch ${binPath}: ${err.message}`);
  process.exit(1);
});
child.on("exit", (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
    return;
  }
  process.exit(code ?? 1);
});

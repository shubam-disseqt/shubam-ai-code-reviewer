#!/usr/bin/env node
// SPDX-License-Identifier: Apache-2.0
// Downloads the zreview binary matching the host platform from GitHub Releases
// and drops it into ./bin. Runs as npm's postinstall hook.
//
// Environment overrides:
//   ZREVIEW_VERSION           override version (default: package.json version)
//   ZREVIEW_DOWNLOAD_BASE     override release URL prefix
//   ZREVIEW_SKIP_DOWNLOAD=1   skip download (for CI mirroring, offline installs)

"use strict";

const fs = require("fs");
const os = require("os");
const path = require("path");
const https = require("https");
const zlib = require("zlib");
const { pipeline } = require("stream");
const { execFileSync } = require("child_process");

if (process.env.ZREVIEW_SKIP_DOWNLOAD === "1") {
  console.log("[zreview] ZREVIEW_SKIP_DOWNLOAD=1 set; skipping binary download.");
  process.exit(0);
}

const PLATFORMS = {
  "darwin-arm64": { os: "darwin", arch: "arm64", ext: "tar.gz", bin: "zreview" },
  "darwin-x64":   { os: "darwin", arch: "amd64", ext: "tar.gz", bin: "zreview" },
  "linux-arm64":  { os: "linux",  arch: "arm64", ext: "tar.gz", bin: "zreview" },
  "linux-x64":    { os: "linux",  arch: "amd64", ext: "tar.gz", bin: "zreview" },
  "win32-arm64":  { os: "windows", arch: "arm64", ext: "zip",   bin: "zreview.exe" },
  "win32-x64":    { os: "windows", arch: "amd64", ext: "zip",   bin: "zreview.exe" },
};

const pkg = JSON.parse(fs.readFileSync(path.join(__dirname, "package.json"), "utf8"));
const version = process.env.ZREVIEW_VERSION || pkg.version;
const base = process.env.ZREVIEW_DOWNLOAD_BASE ||
  `https://github.com/shubam-disseqt/z-code-reviewer/releases/download/v${version}`;

const key = `${process.platform}-${process.arch}`;
const target = PLATFORMS[key];
if (!target) {
  const supported = Object.keys(PLATFORMS).join(", ");
  console.error(`[zreview] Unsupported platform: ${key}`);
  console.error(`[zreview] Supported: ${supported}`);
  console.error(`[zreview] Build from source: https://github.com/shubam-disseqt/z-code-reviewer#build-from-source`);
  process.exit(1);
}

const archive = `zreview_${version}_${target.os}_${target.arch}.${target.ext}`;
const url = `${base}/${archive}`;
const binDir = path.join(__dirname, "bin");
const binPath = path.join(binDir, target.bin);
const archivePath = path.join(binDir, archive);

fs.mkdirSync(binDir, { recursive: true });

console.log(`[zreview] Downloading ${url}`);

download(url, archivePath)
  .then(() => extract(archivePath, binDir, target))
  .then(() => {
    if (target.os !== "windows") {
      fs.chmodSync(binPath, 0o755);
    }
    fs.unlinkSync(archivePath);
    console.log(`[zreview] Installed ${binPath}`);
  })
  .catch((err) => {
    console.error(`[zreview] Install failed: ${err.message}`);
    // ponytail: exit 0 so a network hiccup during postinstall doesn't wedge
    // the entire npm install; user gets a clear error when they invoke zreview.
    // Upgrade to exit 1 if we start caring more about install-time signal than
    // best-effort resilience.
    process.exit(0);
  });

function download(from, to, redirects = 0) {
  if (redirects > 5) return Promise.reject(new Error("too many redirects"));
  return new Promise((resolve, reject) => {
    https.get(from, { headers: { "User-Agent": "zreview-npm-installer" } }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        res.resume();
        resolve(download(res.headers.location, to, redirects + 1));
        return;
      }
      if (res.statusCode !== 200) {
        res.resume();
        reject(new Error(`GET ${from} -> HTTP ${res.statusCode}`));
        return;
      }
      const out = fs.createWriteStream(to);
      pipeline(res, out, (err) => (err ? reject(err) : resolve()));
    }).on("error", reject);
  });
}

function extract(archivePath, destDir, target) {
  if (target.ext === "tar.gz") {
    return extractTarGz(archivePath, destDir);
  }
  return extractZip(archivePath, destDir);
}

function extractTarGz(archivePath, destDir) {
  return new Promise((resolve, reject) => {
    // ponytail: relying on system `tar` avoids a runtime dep on tar-stream;
    // every macOS/Linux node runtime has tar. Swap in tar-stream if we ever
    // need to support alpine-minimal images without tar in $PATH.
    try {
      execFileSync("tar", ["-xzf", archivePath, "-C", destDir], { stdio: "inherit" });
      resolve();
    } catch (err) {
      reject(new Error(`tar extraction failed: ${err.message}`));
    }
  });
}

function extractZip(archivePath, destDir) {
  return new Promise((resolve, reject) => {
    // Windows ships PowerShell; use Expand-Archive to avoid bundling unzip.
    try {
      execFileSync(
        "powershell.exe",
        [
          "-NoProfile",
          "-NonInteractive",
          "-Command",
          `Expand-Archive -Force -Path '${archivePath}' -DestinationPath '${destDir}'`,
        ],
        { stdio: "inherit" }
      );
      resolve();
    } catch (err) {
      reject(new Error(`zip extraction failed: ${err.message}`));
    }
  });
}

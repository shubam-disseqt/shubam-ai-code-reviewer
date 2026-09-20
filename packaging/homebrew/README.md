# Homebrew tap for sacr

The `brew install shubam-disseqt/tap/sacr` command in the root README depends
on a separate GitHub repository named `shubam-disseqt/homebrew-tap`. Homebrew
resolves `brew tap <user>/<name>` to `github.com/<user>/homebrew-<name>`, so the
tap repository name is fixed.

## One-time bootstrap (manual)

Do this once, before the first release that should be installable via brew.

1. Create a new **public** GitHub repository named exactly
   `shubam-disseqt/homebrew-tap`. No description or template required.
2. Copy `Formula/sacr.rb` from this directory into the root of that repo
   under `Formula/sacr.rb`.
3. Commit and push to `main`. The tap is now live.
4. Add a repository secret to `shubam-disseqt/shubam-ai-code-reviewer` called
   `HOMEBREW_TAP_TOKEN` — a fine-grained personal access token with
   `contents: read/write` on `shubam-disseqt/homebrew-tap`. The release
   workflow uses it to open PRs that bump the formula's version and SHA256s.

## What users run

```sh
brew tap shubam-disseqt/tap
brew install sacr
```

Or in one shot:

```sh
brew install shubam-disseqt/tap/sacr
```

## What the release workflow does

Every tag push (`v*`) triggers `.github/workflows/release.yml`, which:

1. Builds `sacr` for macOS/Linux on arm64 and amd64.
2. Uploads `sacr_<version>_<os>_<arch>.tar.gz` archives to the GitHub
   release.
3. Opens a PR against `shubam-disseqt/homebrew-tap` that updates
   `Formula/sacr.rb`:
   - `version` bumps to the new tag (minus the leading `v`).
   - Each `sha256` placeholder is replaced with the archive's real digest.

## Placeholders in the formula

The template ships with these strings so the formula is syntactically valid
and easy to update by hand or via `sed`:

- `RELEASE_VERSION` — the release version, e.g. `0.2.0` (no leading `v`).
- `RELEASE_SHA256_DARWIN_ARM64`
- `RELEASE_SHA256_DARWIN_AMD64`
- `RELEASE_SHA256_LINUX_ARM64`
- `RELEASE_SHA256_LINUX_AMD64`

The release workflow substitutes all five before opening the tap PR.

## Testing the formula locally

From inside a clone of the tap repo:

```sh
brew install --build-from-source ./Formula/sacr.rb
brew test sacr
brew audit --strict --online sacr
```

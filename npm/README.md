# npm packaging for zreview

This directory is the source of the npm distribution. It is not
consumed by Go and does not participate in `make check`.

## Layout

```
npm/
├── zreview/                 # meta package users install; only bin/zreview.js ships
│   ├── package.json         # declares optionalDependencies on every platform stub
│   ├── bin/zreview.js       # Node launcher — resolves + execs the platform binary
│   └── README.md
├── zreview-linux-x64/       # one leaf per (os, cpu) pair
│   ├── package.json         # os + cpu constraints; only this platform's npm install pulls it
│   ├── bin/                 # binary is populated at release time (see release.yml)
│   └── README.md
├── zreview-linux-arm64/
├── zreview-darwin-x64/
├── zreview-darwin-arm64/
├── zreview-win32-x64/
└── zreview-win32-arm64/
```

The `bin/` directories in the leaf packages are intentionally empty in
git — CI drops the matching matrix binary in before running
`npm publish`. Everything under `npm/*/bin/` is gitignored, so the
tree stays clean between releases.

## How the launcher works

`bin/zreview.js` in the meta package uses `require.resolve` to find
the correct `zreview-<platform>-<arch>` sub-package installed as an
optional dependency, then execs its binary via `spawnSync` with
`stdio: 'inherit'`. Exit codes and signals propagate to the parent.

We deliberately do NOT use a `postinstall` hook:

- npm's `optionalDependencies` + `os` + `cpu` fields already restrict
  install to the correct binary. No manual download logic is needed.
- A `postinstall` that hits the network re-introduces the trust
  problem that shipping SLSA-attested release assets is meant to
  solve. If the launcher can find the binary, we're done; if it
  can't, that is a `--no-optional` / `--omit=optional` config problem
  the user must fix.

The same trade-off is used by esbuild, swc, and other Go/Rust CLIs
on npm.

## Publishing (CI-only, do not run locally)

`.github/workflows/release.yml` publishes all seven packages after
the GitHub Release is created:

1. Download matrix artifacts into `dist/`
2. Copy each `zreview-<os>-<arch>[.exe]` into
   `npm/zreview-<npm-platform>-<npm-cpu>/bin/`
3. `npm version <tag> --no-git-tag-version --allow-same-version`
   inside each sub-package and the meta package
4. `npm publish --provenance --access public` in dependency order:
   platform stubs first, then the meta package (so `optionalDependencies`
   resolve on the registry)

`NPM_TOKEN` is required. `id-token: write` is required for `--provenance`.

## Platform / arch mapping

Go's `GOOS` / `GOARCH` differs from npm's `os` / `cpu`. The release
workflow handles this translation:

| Go GOOS/GOARCH | npm os/cpu | npm package |
|---|---|---|
| linux/amd64 | linux/x64 | zreview-linux-x64 |
| linux/arm64 | linux/arm64 | zreview-linux-arm64 |
| darwin/amd64 | darwin/x64 | zreview-darwin-x64 |
| darwin/arm64 | darwin/arm64 | zreview-darwin-arm64 |
| windows/amd64 | win32/x64 | zreview-win32-x64 |
| windows/arm64 | win32/arm64 | zreview-win32-arm64 |

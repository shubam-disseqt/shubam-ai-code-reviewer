# Contributing to shubam-ai-code-reviewer

Thanks for wanting to contribute. Please read this file, [AGENTS.md](AGENTS.md),
and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) before opening a PR.

## Before you start

- The scope of this project is fixed — see [docs/ARCHITECTURE.md § 9. Non-goals](docs/ARCHITECTURE.md#9-non-goals).
  If your contribution expands scope, open an issue for discussion first.
- Read [docs/PORTING.md](docs/PORTING.md) if you are lifting code from
  `alibaba/open-code-review` or `miracodeai/mira`. Attribution rules are
  non-negotiable.
- Big new features go through the roadmap process — see [ROADMAP.md](ROADMAP.md)
  and [GOVERNANCE.md](GOVERNANCE.md).

## Development setup

Requirements:

- Go 1.24+
- Git 2.41+ (matches upstream OCR)
- Node 20+ (for the docs site under `docs/`, only when authoring)

```sh
git clone https://github.com/shubam-disseqt/shubam-ai-code-reviewer.git
cd shubam-ai-code-reviewer
make build
./bin/sacr version
./bin/sacr docs
```

## Common commands

| Command | What it does |
|---|---|
| `make build` | Build `bin/sacr` for the host platform |
| `make test` | Run all Go unit tests |
| `make coverage` | Run tests with coverage; enforces 80% floor |
| `make lint` | `gofmt` + `go vet` + `staticcheck` |
| `make check` | Full CI-equivalent battery: format + lint + test + coverage + license + english-only |
| `make docs` | Build the docs site into `docs/dist/` (for embedding) |
| `make clean` | Delete `bin/`, `dist/`, `coverage.out` |

## Line endings

LF only. Enforced via `.gitattributes`. If Git ever produces CRLF
files on your machine, run:

```sh
git add --renormalize .
```

## Commit messages

Conventional Commits format:

```
<type>: <description>

<optional body>

<optional footer>
```

Types: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`, `perf`, `ci`.

Keep the subject under 72 chars. Put the important details in the PR
body, not the commit message.

**Do not include AI attribution trailers** (`Co-Authored-By: <AI>`,
`Assisted-by:`, etc.). Attribution goes in the PR body when material.
See [AGENTS.md](AGENTS.md).

## PR checklist

Before requesting review, confirm:

- [ ] `make check` passes locally
- [ ] Test coverage for changed packages is ≥ 80% (or unchanged)
- [ ] New Go source files have SPDX headers
- [ ] If any file was lifted or ported from OCR or Mira, attribution
      header is in place per [NOTICE](NOTICE) and [docs/PORTING.md § 2](docs/PORTING.md#2-attribution-model-per-file-header)
- [ ] Docs updated when behavior or config changes
- [ ] AI use disclosed in the PR body if applicable (see AGENTS.md)

## Branching

- `main` is the release-tracking branch. All work goes via PR.
- Feature branches: `feat/<short-slug>`, `fix/<short-slug>`.
- No direct pushes to `main`.

## Design changes

Anything touching:

- The scope of the tool
- The list of upstream sources
- The threat model
- Public flag surface (`sacr <cmd> --...`)

goes through a design issue before code. Small refactors and bug fixes
do not.

## Questions

Open a Discussion under the repo — issues are for bug reports and
tracked work, not open questions.
